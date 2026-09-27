// service_proxy_routing_test.go —— TD-60 切流基建的可回归用例（2026-09-26）。
//
// 覆盖三件事，对应三个真实缺陷/缺失：
//  1. 自环拦截：出厂配置把 autoscaler/portal 的后端指向控制面自己的 8080，
//     /api/v1/portal/quotas 被静默转发回自己并返回单体数据。本文件用与
//     出厂配置一致的参数复现该场景，断言它现在返回 503。
//  2. 路由开关 OPSMESH_SERVICE_PROXY：让切流第一次成为可逆操作。
//  3. 路由状态端点：让「这个域现在由谁处理」成为可查询事实。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// mustAtoi 解析端口字符串，失败即测试失败。
func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("解析端口 %q 失败: %v", s, err)
	}
	return n
}

// disabledProxyDomainsFrom 在给定开关取值下解析停用域集合。
func disabledProxyDomainsFrom(t *testing.T, raw string) (map[string]bool, error) {
	t.Helper()
	t.Setenv(serviceProxyEnvKey, raw)
	return disabledProxyDomains()
}

// adminReq 构造一个带 admin 鉴权头的请求。
func adminReq(t *testing.T, s *Server, method, path string) *http.Request {
	t.Helper()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	return req
}

// clearBackendEnv 清空全部转发后端环境变量，让规则回落到出厂默认值。
func clearBackendEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"GPU_SVC_URL", "RUNBOOK_SVC_URL", "INCIDENT_SVC_URL",
		"AUTOSCALER_SVC_URL", "PORTAL_SVC_URL", "DEVICE_SVC_URL",
	} {
		t.Setenv(k, "")
	}
}

// ── 1. 自环拦截 ────────────────────────────────────────────────────

// TestIsLoopbackHost 回环主机判定。
func TestIsLoopbackHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"127.0.0.53", true},
		{"localhost", true},
		{"LOCALHOST", true},
		{"::1", true},
		{"[::1]", true},
		{"gpu-svc", false},
		{"controlplane", false},
		{"10.0.0.5", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isLoopbackHost(tc.host); got != tc.want {
			t.Errorf("isLoopbackHost(%q) = %v，期望 %v", tc.host, got, tc.want)
		}
	}
}

// TestProxySelfLoopTarget 出厂事故的判定核心：后端端口与控制面自身监听端口重合。
func TestProxySelfLoopTarget(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		httpPort int
		grpcPort int
		metrics  int
		wantSelf bool
	}{
		{"出厂事故：portal 指向控制面自身", "http://127.0.0.1:8080", 8080, 9090, 9091, true},
		{"localhost 同形", "http://localhost:8080", 8080, 9090, 9091, true},
		{"撞 gRPC 端口也算", "http://127.0.0.1:9090", 8080, 9090, 9091, true},
		{"撞 metrics 端口也算", "http://127.0.0.1:9091", 8080, 9090, 9091, true},
		{"容器服务名不构成自环", "http://gpu-svc:8107", 8080, 9090, 9091, false},
		{"异机同端口不构成自环", "http://10.0.0.9:8080", 8080, 9090, 9091, false},
		{"同机但端口不同", "http://127.0.0.1:8090", 8080, 9090, 9091, false},
		{"无显式端口不判定", "http://127.0.0.1", 8080, 9090, 9091, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatalf("url.Parse(%q) 失败: %v", tc.raw, err)
			}
			_, self := proxySelfLoopTarget(u, tc.httpPort, tc.grpcPort, tc.metrics)
			if self != tc.wantSelf {
				t.Errorf("proxySelfLoopTarget(%q) 自环=%v，期望 %v", tc.raw, self, tc.wantSelf)
			}
		})
	}
}

// TestProxySelfLoopTargetMalformedPort 覆盖 url.Parse 拦不住的畸形端口：
// upstreamBase 走的是 url.Parse，非数字端口在那里就被拒了，但 proxySelfLoopTarget
// 仍必须自己站得住（防御式解析不得依赖调用方已过滤）。
func TestProxySelfLoopTargetMalformedPort(t *testing.T) {
	for _, host := range []string{"127.0.0.1:abc", "127.0.0.1:", "127.0.0.1:8080x", "127.0.0.1:-1"} {
		u := &url.URL{Scheme: "http", Host: host}
		if p, self := proxySelfLoopTarget(u, 8080, 9090, 9091); self {
			t.Errorf("host=%q 被误判为自环（端口 %d）", host, p)
		}
	}
}

// TestServiceProxySelfLoopRejected 本文件最重要的一条：逐字复现出厂配置下的
// 静默错误数据路径，断言现在被 503 拦下、且不再把单体的数据透传给调用方。
func TestServiceProxySelfLoopRejected(t *testing.T) {
	s := newServiceProxyTestServer()
	// 显式给端口——零值端口不会触发自环判定，必须模拟真实启动时的 8080。
	s.httpPort, s.grpcPort, s.metricsPort = 8080, 9090, 9091

	// 用一个真实监听的测试服务模拟「控制面自己」：它会返回单体口径的数据。
	back := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"quota":"来自单体的错误数据"}`))
	}))
	defer back.Close()
	_, p, _ := strings.Cut(strings.TrimPrefix(back.URL, "http://"), ":")
	t.Setenv("PORTAL_SVC_URL", "http://127.0.0.1:"+p)
	s.httpPort = mustAtoi(t, p) // 制造真实自环

	rec := httptest.NewRecorder()
	s.handleServiceProxy(rec, adminReq(t, s, http.MethodGet, "/api/v1/portal/quotas"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("自环后端应返回 503，实际 %d；body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "来自单体的错误数据") {
		t.Fatalf("把单体的数据透传给了调用方：%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "PORTAL_SVC_URL") {
		t.Errorf("503 响应应指明修复方式（提到 PORTAL_SVC_URL），实际：%s", rec.Body.String())
	}
}

// TestValidateServiceProxyTargetsDetectsShippedDefaults 出厂默认值必须被自检点名。
// 断言「至少能报出 portal 与 autoscaler」——它们正是默认值撞 8080 的两域。
func TestValidateServiceProxyTargetsDetectsShippedDefaults(t *testing.T) {
	clearBackendEnv(t)

	joined := strings.Join(validateServiceProxyTargets(8080, 9090, 9091), "\n")
	for _, want := range []string{`"portal"`, `"autoscaler"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("自检未点名域 %s；实际报出：\n%s", want, joined)
		}
	}

	// 设了正确地址后不应再报——这是「修好之后」的验收条件。
	t.Setenv("PORTAL_SVC_URL", "http://portal-svc:8109")
	t.Setenv("AUTOSCALER_SVC_URL", "http://autoscaler-svc:8080")
	after := strings.Join(validateServiceProxyTargets(8080, 9090, 9091), "\n")
	if strings.Contains(after, `"portal"`) {
		t.Errorf("已设置正确后端后仍报 portal 自环：%s", after)
	}
}

// ── 2. 路由开关 ────────────────────────────────────────────────────

// TestParseDisabledProxyDomains 开关的全部取值形态。
func TestParseDisabledProxyDomains(t *testing.T) {
	all := len(proxyDomainNames())
	if all == 0 {
		t.Fatal("proxyDomainNames 为空，规则表疑似丢失")
	}
	cases := []struct {
		raw         string
		wantErr     bool
		wantCount   int
		wantContain []string
	}{
		{raw: "", wantCount: 0},
		{raw: "on", wantCount: 0},
		{raw: "  ALL  ", wantCount: 0},
		{raw: "off", wantCount: all},
		{raw: "NONE", wantCount: all},
		{raw: "gpu", wantCount: 1, wantContain: []string{"gpu"}},
		{raw: "gpu,portal", wantCount: 2, wantContain: []string{"gpu", "portal"}},
		{raw: " gpu , portal ", wantCount: 2, wantContain: []string{"gpu", "portal"}},
		{raw: "gpu,,portal", wantCount: 2}, // 空段容忍
		{raw: "不存在的域", wantErr: true},      //
		{raw: "gpu,不存在的域", wantErr: true},  // 整体拒绝，不做部分生效
	}
	for _, tc := range cases {
		t.Run("raw="+tc.raw, func(t *testing.T) {
			got, err := parseDisabledProxyDomains(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际 nil，得到 %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if len(got) != tc.wantCount {
				t.Errorf("停用域数 = %d，期望 %d（得到 %v）", len(got), tc.wantCount, got)
			}
			for _, d := range tc.wantContain {
				if !got[d] {
					t.Errorf("期望停用 %q，实际停用集合 %v", d, got)
				}
			}
		})
	}
}

// TestProxySwitchInvalidValueDoesNotCutover 开关写错时**不得**部分切流。
// 误切生产流量的代价远大于「没切」，因此非法取值必须整体拒绝。
func TestProxySwitchInvalidValueDoesNotCutover(t *testing.T) {
	if _, err := parseDisabledProxyDomains("gpu,typo域"); err == nil {
		t.Fatal("部分非法取值被接受，会造成部分域被静默切走")
	}
}

// TestDisabledProxyDomainIsNotRegistered 停用的域**不注册**路由，请求自然落回单体
// 自己的 handler（真正的回退），而不是被代理 handler 拦成 503。
func TestDisabledProxyDomainIsNotRegistered(t *testing.T) {
	disabled, err := disabledProxyDomainsFrom(t, "gpu")
	if err != nil {
		t.Fatalf("解析开关失败: %v", err)
	}
	if !disabled["gpu"] {
		t.Fatal("gpu 未被标记为停用")
	}

	// 复刻 server_lifecycle.go 的注册条件。
	registered := false
	for i := range serviceProxyRules {
		if serviceProxyRules[i].publicPrefix == "" || disabled[serviceProxyRules[i].domain] {
			continue
		}
		if serviceProxyRules[i].domain == "gpu" {
			registered = true
		}
	}
	if registered {
		t.Error("停用域 gpu 仍会被注册为代理路由，回退语义不成立")
	}
}

// ── 3. 路由状态端点 ────────────────────────────────────────────────

// TestServiceRoutingEndpoint 端点必须把自环如实报成 self-loop，而不是 ok。
func TestServiceRoutingEndpoint(t *testing.T) {
	s := newServiceProxyTestServer()
	s.httpPort, s.grpcPort, s.metricsPort = 8080, 9090, 9091
	clearBackendEnv(t)
	t.Setenv(serviceProxyEnvKey, "runbook")

	rec := httptest.NewRecorder()
	s.handleServiceRouting(rec, adminReq(t, s, http.MethodGet, "/api/v1/admin/service-routing"))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200；body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Switch struct {
			Value    string          `json:"value"`
			Disabled map[string]bool `json:"disabled"`
		} `json:"switch"`
		Domains []struct {
			Domain    string `json:"domain"`
			Backend   string `json:"backend"`
			Forwarded bool   `json:"forwarded"`
			Status    string `json:"status"`
		} `json:"domains"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %v；body=%s", err, rec.Body.String())
	}
	if !out.Switch.Disabled["runbook"] {
		t.Errorf("开关状态未反映到响应：%+v", out.Switch)
	}

	byDomain := map[string]int{}
	for i, d := range out.Domains {
		byDomain[d.Domain] = i
	}
	for _, want := range []string{"gpu", "runbook", "incident", "autoscaler", "portal", "device"} {
		if _, ok := byDomain[want]; !ok {
			t.Fatalf("响应缺少域 %q；实际 %+v", want, out.Domains)
		}
	}

	// 出厂默认值下 portal 撞 8080 → 必须报 self-loop，不能报 ok。
	pi := byDomain["portal"]
	if got := out.Domains[pi].Status; got != "self-loop" {
		t.Errorf("portal 状态 = %q，期望 self-loop（默认值撞控制面 8080）", got)
	}
	if out.Domains[pi].Forwarded {
		t.Error("self-loop 域不应标记为 forwarded")
	}

	// 被开关停用的域必须是 disabled。
	ri := byDomain["runbook"]
	if got := out.Domains[ri].Status; got != "disabled" {
		t.Errorf("runbook 状态 = %q，期望 disabled", got)
	}
}

// TestServiceRoutingMethodNotAllowed 只读端点不接受写。
func TestServiceRoutingMethodNotAllowed(t *testing.T) {
	s := newServiceProxyTestServer()
	rec := httptest.NewRecorder()
	s.handleServiceRouting(rec, adminReq(t, s, http.MethodPost, "/api/v1/admin/service-routing"))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST 状态码 = %d，期望 405", rec.Code)
	}
}
