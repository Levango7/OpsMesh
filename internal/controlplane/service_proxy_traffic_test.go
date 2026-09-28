// service_proxy_traffic_test.go — 逐域真实流量端点（TD-60 §5.3「最终裁决待真实流量」的取数出口）。
//
// 锁定的契约：域集合来自转发路由表（新增域不会缺席）、有流量的域计数正确、
// 零流量域显式返回 0（"没人用"是裁决证据）、鉴权与 /api/v1/admin/* 其余只读端点同档。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/metrics"
)

// trafficResponse 端点响应结构（与 handleAdminServiceTraffic 的载荷一致）。
type trafficResponse struct {
	Domains      []metrics.TrafficBucket `json:"domains"`
	Window       string                  `json:"window"`
	CrossRestart string                  `json:"crossRestart"`
	Note         string                  `json:"note"`
}

func decodeTraffic(t *testing.T, body string) trafficResponse {
	t.Helper()
	var out trafficResponse
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("响应解析失败: %v; body=%s", err, body)
	}
	return out
}

// TestAdminServiceTrafficReportsPerDomain 按域汇总：有流量的域计数正确，
// 路由表内其余域显式出现且 requests=0。
func TestAdminServiceTrafficReportsPerDomain(t *testing.T) {
	s := newServiceProxyTestServer()
	s.metrics = metrics.New()
	s.metrics.RecordHTTP(http.MethodGet, "/api/v1/gpu/nodes", "200", 0.01)
	s.metrics.RecordHTTP(http.MethodGet, "/api/v1/gpu/nodes", "200", 0.02)
	s.metrics.RecordHTTP(http.MethodPost, "/api/v1/device-svc/devices", "201", 0.03)
	// 非代理域流量不得混入。
	s.metrics.RecordHTTP(http.MethodGet, "/api/v1/devices", "200", 0.01)

	w := httptest.NewRecorder()
	s.handleAdminServiceTraffic(w, adminReq(t, s, http.MethodGet, "/api/v1/admin/service-traffic"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	out := decodeTraffic(t, w.Body.String())

	byDomain := make(map[string]metrics.TrafficBucket, len(out.Domains))
	for _, b := range out.Domains {
		byDomain[b.Domain] = b
	}
	if got := byDomain["gpu"].Requests; got != 2 {
		t.Errorf("gpu requests = %d, want 2", got)
	}
	if got := byDomain["device"].Requests; got != 1 {
		t.Errorf("device requests = %d, want 1", got)
	}
	// 域集合 = 路由表全部域（当前 7 个：gpu/runbook/incident/autoscaler/portal/device/task）。
	for _, d := range proxyDomainNames() {
		b, ok := byDomain[d]
		if !ok {
			t.Fatalf("结果缺域 %q（域必须取自转发路由表）: %+v", d, out.Domains)
		}
		if d != "gpu" && d != "device" && b.Requests != 0 {
			t.Errorf("域 %s requests = %d, want 0（零流量域应显式报 0）", d, b.Requests)
		}
	}
	if out.Window == "" || out.CrossRestart == "" {
		t.Errorf("响应应说明计数窗口与跨重启取数方式: %+v", out)
	}
}

// TestAdminServiceTrafficAuthAndMethod 鉴权与方法闸：
// viewer 无 diagnostics:execute → 403；无身份 → 401；POST → 405。
func TestAdminServiceTrafficAuthAndMethod(t *testing.T) {
	s := newServiceProxyTestServer()
	s.metrics = metrics.New()
	s.metrics.RecordHTTP(http.MethodGet, "/api/v1/gpu/nodes", "200", 0.01)

	w := httptest.NewRecorder()
	s.handleAdminServiceTraffic(w, adminReq(t, s, http.MethodPost, "/api/v1/admin/service-traffic"))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405; body=%s", w.Code, w.Body.String())
	}

	viewerReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-traffic", nil)
	viewerReq.Header.Set("Authorization", loginAsViewer(t, s))
	viewerReq.Header.Set("X-Tenant-ID", "default")
	w = httptest.NewRecorder()
	s.handleAdminServiceTraffic(w, viewerReq)
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), levelPermission) {
		t.Errorf("403 应指明缺失的权限点 %s: %s", levelPermission, w.Body.String())
	}

	// 无凭证（仅租户头）：requireAuth 下 401，不返回任何流量数据。
	bareReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-traffic", nil)
	bareReq.Header.Set("X-Tenant-ID", "default")
	w = httptest.NewRecorder()
	s.requireAuth = true
	s.handleAdminServiceTraffic(w, bareReq)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无凭证 status = %d, want 401; body=%s", w.Code, w.Body.String())
	}
}

// TestGroupProxyDomainsCoversRoutingTable 域→前缀派生只有一份实现：
// service-traffic 与 service-routing 必须看到同一组域与前缀。
func TestGroupProxyDomainsCoversRoutingTable(t *testing.T) {
	groups := groupProxyDomains()
	seen := map[string][]string{}
	for _, g := range groups {
		seen[g.domain] = g.prefixes
	}
	rules := allProxyRules()
	if len(seen) == 0 || len(rules) < len(seen) {
		t.Fatalf("聚合结果异常: groups=%d rules=%d", len(seen), len(rules))
	}
	for _, r := range rules {
		found := false
		for _, p := range seen[r.domain] {
			if p == r.publicPrefix {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("域 %q 缺少前缀 %s（聚合漏规则）: %v", r.domain, r.publicPrefix, seen[r.domain])
		}
	}
}
