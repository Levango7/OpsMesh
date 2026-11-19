// abac_enforce_test.go ABAC 定向强制（批次53）回归。
//
// 钉住的产品边界：
//  1. 空白名单 = kill switch：任何路由都不会出现 403；
//  2. 命中白名单且无匹配策略（default-deny）⇒ 403 + 审计 + 指标，handler 不执行；
//  3. 命中白名单且有 allow 策略 ⇒ handler 正常执行；
//  4. 未命中白名单的路由即使 default-deny 也不受影响；
//  5. `{id}` 单段占位匹配；非法条目/预置名在 Validate 阶段被拒绝；
//  6. high-risk 预置展开为仓库真实路由；
//  7. 同时启用 shadow 时，命中强制白名单的请求只评估一次（shadow 让位）。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/metrics"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

func newABACEnforceTestServer(allowlist ...string) (*Server, *store.MemoryStore) {
	st := store.NewMemoryStore()
	return &Server{
		cfg:     &config.Config{ABACEnforce: allowlist},
		metrics: metrics.New(),
		store:   st,
	}, st
}

func newABACEnforceRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "10.0.0.9:54321"
	req.Header.Set("User-Agent", "opsmesh-enforce-test")
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-Id", "alice")
	req.Header.Set("X-User-Roles", "operator")
	return req
}

const abacEnforceNextBody = `{"ok":true}`

func serveABACEnforce(t *testing.T, s *Server, req *http.Request) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
		if _, err := w.Write([]byte(abacEnforceNextBody)); err != nil {
			t.Fatalf("写响应失败: %v", err)
		}
	})
	rec := httptest.NewRecorder()
	s.abacEnforceMiddleware(next).ServeHTTP(rec, req)
	return rec, called
}

func findABACEnforceAudit(events []*proto.AuditEvent, action string) *proto.AuditEvent {
	for _, e := range events {
		if e != nil && e.Action == action {
			return e
		}
	}
	return nil
}

func TestABACEnforce_EmptyAllowlistIsKillSwitch(t *testing.T) {
	beforeDeny := ABACEnforceDeniedTotal.Load()
	s, st := newABACEnforceTestServer() // 空白名单
	rec, called := serveABACEnforce(t, s, newABACEnforceRequest(http.MethodDelete, "/api/v1/devices/dev-1"))

	if !called || rec.Code != http.StatusAccepted {
		t.Fatalf("空白名单时 handler 应照常执行（called=%v code=%d）", called, rec.Code)
	}
	if got := ABACEnforceDeniedTotal.Load() - beforeDeny; got != 0 {
		t.Fatalf("空白名单不得拒绝，拒绝计数增量=%d", got)
	}
	if findABACEnforceAudit(st.Audits(), "abac_enforce_denied") != nil {
		t.Fatal("空白名单不得写 abac_enforce_denied 审计")
	}
	if !strings.Contains(s.metrics.Render(), `opsmesh_abac_enforce_decisions_total{decision="deny",method="DELETE"} 0`) {
		t.Fatal("未强制时 Prometheus 序列仍应存在且为 0")
	}
}

func TestABACEnforce_DefaultDenyReturns403AuditAndMetric(t *testing.T) {
	beforeDeny := ABACEnforceDeniedTotal.Load()
	s, st := newABACEnforceTestServer("DELETE /api/v1/devices/{id}")
	// 不注册任何策略 ⇒ 引擎 default-deny。
	rec, called := serveABACEnforce(t, s, newABACEnforceRequest(http.MethodDelete, "/api/v1/devices/dev-1"))

	if called {
		t.Fatal("被拒绝的请求不得进入 handler")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Error           string `json:"error"`
		Reason          string `json:"reason"`
		MatchedPolicyID string `json:"matchedPolicyID"`
		Enforced        bool   `json:"enforced"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("403 响应不是合法 JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Error != "abac denied" || !body.Enforced || body.Reason == "" || body.MatchedPolicyID != "" {
		t.Fatalf("403 字段不符: %+v", body)
	}
	if !strings.Contains(body.Reason, "no matching policy") {
		t.Fatalf("reason 应说明 default-deny，得到 %q", body.Reason)
	}
	if got := ABACEnforceDeniedTotal.Load() - beforeDeny; got != 1 {
		t.Fatalf("拒绝计数=%d, want 1", got)
	}

	ev := findABACEnforceAudit(st.Audits(), "abac_enforce_denied")
	if ev == nil {
		t.Fatal("缺少 abac_enforce_denied 审计事件")
	}
	if ev.TenantID != "t1" || ev.UserID != "alice" {
		t.Fatalf("审计身份不符: tenant=%q user=%q", ev.TenantID, ev.UserID)
	}
	if ev.Target != "/api/v1/devices/dev-1" || ev.Detail == "" {
		t.Fatalf("审计 target/detail 不符: target=%q detail=%q", ev.Target, ev.Detail)
	}
	if !strings.Contains(ev.Detail, "no matching policy") {
		t.Fatalf("审计 detail 应包含决策原因，得到 %q", ev.Detail)
	}

	if !strings.Contains(s.metrics.Render(), `opsmesh_abac_enforce_decisions_total{decision="deny",method="DELETE"} 1`) {
		t.Fatalf("Prometheus deny/DELETE 计数未记录:\n%s", s.metrics.Render())
	}
}

func TestABACEnforce_AllowPolicyPassesThrough(t *testing.T) {
	s, st := newABACEnforceTestServer("DELETE /api/v1/devices/{id}")
	if err := s.addABACPolicy(abacPolicy{ID: "allow-high-risk", TenantID: "t1", Subject: "*", Resource: "*", Action: "*", Effect: "allow"}); err != nil {
		t.Fatalf("注册策略失败: %v", err)
	}
	rec, called := serveABACEnforce(t, s, newABACEnforceRequest(http.MethodDelete, "/api/v1/devices/dev-1"))

	if !called || rec.Code != http.StatusAccepted {
		t.Fatalf("allow 时 handler 应执行（called=%v code=%d body=%s）", called, rec.Code, rec.Body.String())
	}
	if rec.Body.String() != abacEnforceNextBody {
		t.Fatalf("allow 路径不得改写响应体: %q", rec.Body.String())
	}
	if findABACEnforceAudit(st.Audits(), "abac_enforce_denied") != nil {
		t.Fatal("allow 决策不得写拒绝审计")
	}
	if !strings.Contains(s.metrics.Render(), `opsmesh_abac_enforce_decisions_total{decision="allow",method="DELETE"} 1`) {
		t.Fatalf("Prometheus allow/DELETE 计数未记录:\n%s", s.metrics.Render())
	}
}

func TestABACEnforce_UnmatchedRouteUntouched(t *testing.T) {
	beforeDeny := ABACEnforceDeniedTotal.Load()
	s, _ := newABACEnforceTestServer("DELETE /api/v1/devices/{id}")
	// POST /api/v1/tasks 不在白名单；即使 default-deny 也不得被强制中间件拦截。
	rec, called := serveABACEnforce(t, s, newABACEnforceRequest(http.MethodPost, "/api/v1/tasks"))

	if !called || rec.Code != http.StatusAccepted {
		t.Fatalf("未命中白名单的路由不得被拦截（called=%v code=%d）", called, rec.Code)
	}
	if got := ABACEnforceDeniedTotal.Load() - beforeDeny; got != 0 {
		t.Fatalf("未命中路由不得记拒绝，增量=%d", got)
	}
	if !strings.Contains(s.metrics.Render(), `opsmesh_abac_enforce_decisions_total{decision="deny",method="POST"} 0`) {
		t.Fatal("未命中路由不应产生 POST deny 计数")
	}
}

func TestABACEnforce_PlaceholderMatchesSingleSegmentOnly(t *testing.T) {
	beforeDeny := ABACEnforceDeniedTotal.Load()
	s, _ := newABACEnforceTestServer("DELETE /api/v1/devices/{id}")

	// 单段 ID 命中白名单 ⇒ 403。
	rec, called := serveABACEnforce(t, s, newABACEnforceRequest(http.MethodDelete, "/api/v1/devices/dev-abc"))
	if called || rec.Code != http.StatusForbidden {
		t.Fatalf("{id} 应匹配单段：called=%v code=%d", called, rec.Code)
	}
	// 多一层子路径 ⇒ 分段数不符，不命中。
	rec, called = serveABACEnforce(t, s, newABACEnforceRequest(http.MethodDelete, "/api/v1/devices/dev-abc/reboot"))
	if !called || rec.Code != http.StatusAccepted {
		t.Fatalf("{id} 不得跨段匹配：called=%v code=%d", called, rec.Code)
	}
	// 方法不匹配 ⇒ 不命中。
	rec, called = serveABACEnforce(t, s, newABACEnforceRequest(http.MethodPost, "/api/v1/devices/dev-abc"))
	if !called || rec.Code != http.StatusAccepted {
		t.Fatalf("方法不匹配时不得强制：called=%v code=%d", called, rec.Code)
	}
	if got := ABACEnforceDeniedTotal.Load() - beforeDeny; got != 1 {
		t.Fatalf("只应有 1 次拒绝，增量=%d", got)
	}
}

func TestABACEnforce_InvalidConfigRejectedAtValidate(t *testing.T) {
	base := func() *config.Config {
		return &config.Config{
			Mode:              "controlplane",
			HTTPPort:          8080,
			GRPCPort:          9090,
			MetricsPort:       9091,
			Store:             "memory",
			TaskTimeout:       120 * time.Second,
			ShutdownTimeout:   15 * time.Second,
			WorkerConcurrency: 4,
			TaskLeaseSec:      300,
			Replicas:          1,
			TaskMaxRetries:    3,
			LogBackend:        "memory",
		}
	}
	for _, entry := range []string{
		"GET /api/v1/devices",
		"DELETE api/v1/devices",
		"DELETE /api/v1/devices?x=1",
		"DELETE /api/v1/devices/{id",
		"DELETE /api/v1/devices/*",
		"DELETE /api/v1//devices",
	} {
		c := base()
		c.ABACEnforce = []string{entry}
		if err := c.Validate(); err == nil {
			t.Fatalf("非法条目 %q 应被 Validate 拒绝", entry)
		}
	}
	c := base()
	c.ABACEnforcePreset = "no-such-preset"
	if err := c.Validate(); err == nil {
		t.Fatal("未知 --abac-enforce-preset 应被 Validate 拒绝")
	}
	// 合法条目 + 合法预置必须通过。
	ok := base()
	ok.ABACEnforcePreset = config.ABACEnforcePresetHighRisk
	ok.ABACEnforce = []string{"PATCH /api/v1/devices/{id}"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("合法配置不应报错: %v", err)
	}
}

func TestABACEnforce_PresetHighRiskExpandsDocumentedRoutes(t *testing.T) {
	routes, err := config.ExpandABACEnforcePreset(config.ABACEnforcePresetHighRisk)
	if err != nil {
		t.Fatalf("预置展开失败: %v", err)
	}
	want := []string{
		"DELETE /api/v1/devices/{id}",
		"DELETE /api/v1/system/users",
		"POST /api/v1/system/firewall",
		"DELETE /api/v1/system/firewall",
		"DELETE /api/v1/tenants/{id}",
		"POST /api/v1/fileshare/mount",
	}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("high-risk 预置不符:\n got=%v\nwant=%v", routes, want)
	}

	rules := compileABACEnforceRules(&config.Config{ABACEnforce: routes})
	samples := []struct {
		method string
		path   string
	}{
		{http.MethodDelete, "/api/v1/devices/dev-1"},
		{http.MethodDelete, "/api/v1/system/users"},
		{http.MethodPost, "/api/v1/system/firewall"},
		{http.MethodDelete, "/api/v1/system/firewall"},
		{http.MethodDelete, "/api/v1/tenants/t1"},
		{http.MethodPost, "/api/v1/fileshare/mount"},
	}
	for _, sample := range samples {
		if !abacEnforceCovers(rules, newABACEnforceRequest(sample.method, sample.path)) {
			t.Fatalf("预置路由应覆盖 %s %s", sample.method, sample.path)
		}
	}
}

func TestABACEnforce_ExcludedPathsNeverEnforced(t *testing.T) {
	s, _ := newABACEnforceTestServer(
		"POST /api/v1/abac",
		"POST /api/v1/federation/forward/task",
		"POST /healthz",
		"POST /readyz",
		"POST /metrics",
		"POST /api/v1/events/stream",
	)
	for _, path := range []string{
		"/api/v1/abac",
		"/api/v1/abac/evaluate",
		"/api/v1/federation/forward/task",
		"/healthz",
		"/readyz",
		"/metrics",
		"/api/v1/events/stream",
	} {
		rec, called := serveABACEnforce(t, s, newABACEnforceRequest(http.MethodPost, path))
		if !called || rec.Code != http.StatusAccepted {
			t.Fatalf("排除路径 %s 不得被强制（called=%v code=%d）", path, called, rec.Code)
		}
	}
}

func TestABACEnforce_ShadowDoesNotDoubleEvaluateMatchedRoutes(t *testing.T) {
	st := store.NewMemoryStore()
	s := &Server{
		cfg: &config.Config{
			ABACShadow:  true,
			ABACEnforce: []string{"DELETE /api/v1/devices/{id}"},
		},
		metrics: metrics.New(),
		store:   st,
	}
	if err := s.addABACPolicy(abacPolicy{ID: "allow-high-risk", TenantID: "t1", Subject: "*", Resource: "*", Action: "*", Effect: "allow"}); err != nil {
		t.Fatalf("注册策略失败: %v", err)
	}

	var shadowEvals atomic.Int64
	abacShadowEvalHook = func(_ *Server, _, _, _, _, _ string) (bool, string, string) {
		shadowEvals.Add(1)
		return true, "shadow-policy", "allowed by shadow hook"
	}
	defer func() { abacShadowEvalHook = nil }()

	// 命中强制白名单：enforce 决策一次，shadow 必须让位。
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	})
	rec := httptest.NewRecorder()
	s.abacEnforceMiddleware(s.abacShadowMiddleware(next)).ServeHTTP(rec, newABACEnforceRequest(http.MethodDelete, "/api/v1/devices/dev-1"))
	if !called || rec.Code != http.StatusAccepted {
		t.Fatalf("命中白名单且 allow 时应进入 handler（called=%v code=%d）", called, rec.Code)
	}
	if got := shadowEvals.Load(); got != 0 {
		t.Fatalf("命中强制白名单时 shadow 不得重复评估，实际评估 %d 次", got)
	}

	// 未命中强制白名单：shadow 仍应观察（证明让位只针对命中路由）。
	called = false
	rec = httptest.NewRecorder()
	s.abacEnforceMiddleware(s.abacShadowMiddleware(next)).ServeHTTP(rec, newABACEnforceRequest(http.MethodPost, "/api/v1/tasks"))
	if !called || rec.Code != http.StatusAccepted {
		t.Fatalf("未命中白名单时 handler 应执行（called=%v code=%d）", called, rec.Code)
	}
	if got := shadowEvals.Load(); got != 1 {
		t.Fatalf("未命中白名单时 shadow 应评估 1 次，实际 %d 次", got)
	}
}

func TestABACEnforce_EvaluatorPanicFailsClosed(t *testing.T) {
	beforePanic := ABACEnforcePanicTotal.Load()
	s, _ := newABACEnforceTestServer("DELETE /api/v1/devices/{id}")
	abacEnforceEvalHook = func(_ *Server, _, _, _, _, _ string) (bool, string, string) {
		panic("simulated evaluator failure")
	}
	defer func() { abacEnforceEvalHook = nil }()

	rec, called := serveABACEnforce(t, s, newABACEnforceRequest(http.MethodDelete, "/api/v1/devices/dev-1"))
	if called {
		t.Fatal("评估器 panic 后应 fail-closed，不得进入 handler")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("panic 应返回受控 403，实际 %d", rec.Code)
	}
	if got := ABACEnforcePanicTotal.Load() - beforePanic; got != 1 {
		t.Fatalf("panic 计数=%d, want 1", got)
	}
	if !strings.Contains(s.metrics.Render(), `opsmesh_abac_enforce_decisions_total{decision="error",method="DELETE"} 1`) {
		t.Fatalf("panic 应记 error 决策:\n%s", s.metrics.Render())
	}
}
