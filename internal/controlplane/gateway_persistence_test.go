// gateway_persistence_test.go TD-66 P1 路由持久化的行为守卫。
//
// 验收标准第一条（立项文档 §4）：「重启控制面后 GET /api/v1/gateway/routes 返回重启前的规则
// （MySQL 后端）」——用「同一 store、两个 gatewayState」模拟进程重启：旧 state 丢弃、
// 新 state 首次读时从 store 恢复。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/extension"
	"github.com/Levango7/OpsMesh/internal/store"
)

// newGatewayPersistenceServer 构造带 memory store 的 Server（memory 后端也走同一套
// store 方法——与 MySQL 唯一的差别是进程退出后数据是否还在，不影响本用例的跨实例语义）。
func newGatewayPersistenceServer(t *testing.T) *Server {
	t.Helper()
	s := newGatewayRouteTestServer()
	return s
}

// TestGatewayRoutesSurviveStateReset 重启语义：同一份 store 下，
// 旧 gatewayState 里的路由必须能在一个全新 gatewayState 上恢复。
func TestGatewayRoutesSurviveStateReset(t *testing.T) {
	// 造一个干净的 store 并配好路由 CRUD（复用 handler，走真实写路径——含 store 落库）。
	s := newGatewayPersistenceServer(t)
	auth := loginAsAdmin(t, s)

	created := createTestGatewayRoute(t, s, auth, `{"name":"p1","pathPrefix":"/api/","targetBackend":"http://127.0.0.1:9","enabled":true}`)
	if created.ID == "" {
		t.Fatal("前置条件失败：路由未创建成功")
	}

	// 模拟进程重启：换一个全新 Server（新 gatewayState/新 gatewayOnce），但共享同一份 store。
	// 这比 Reset sync.Once 更接近真实重启语义（连 Once 都是新的）。
	s = &Server{
		store:        s.store,
		cfg:          s.cfg,
		jwtSecret:    s.jwtSecret,
		sessionStore: s.sessionStore,
		loginGuard:   s.loginGuard,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gateway/routes", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleListGatewayRoutes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), created.ID) {
		t.Errorf("重启后未恢复路由（验收标准第一条）: body=%s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"name":"p1"`) {
		t.Errorf("重启后路由字段不完整: body=%s", w.Body.String())
	}
}

// TestGatewayRouteDeletePersists 删除必须真的落 store——否则重启后路由会复活。
func TestGatewayRouteDeletePersists(t *testing.T) {
	s := newGatewayPersistenceServer(t)
	auth := loginAsAdmin(t, s)
	created := createTestGatewayRoute(t, s, auth, `{"name":"del1","pathPrefix":"/api/del/","targetBackend":"http://127.0.0.1:9"}`)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/gateway/routes/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayRouteRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("删除 status=%d want 200; body=%s", w.Code, w.Body.String())
	}

	// 丢弃缓存重新读——若删除只在内存生效，路由会"复活"。
	s = &Server{
		store:        s.store,
		cfg:          s.cfg,
		jwtSecret:    s.jwtSecret,
		sessionStore: s.sessionStore,
		loginGuard:   s.loginGuard,
	}
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/gateway/routes", nil)
	req2.Header.Set("Authorization", auth)
	req2.Header.Set("X-Tenant-ID", "default")
	w2 := httptest.NewRecorder()
	s.handleListGatewayRoutes(w2, req2)
	if strings.Contains(w2.Body.String(), created.ID) {
		t.Errorf("删除未落 store（重启后路由复活）: %s", w2.Body.String())
	}
}

// TestGatewayRouteUpdatePersists 更新的字段必须落 store（尤其 Enabled=false 的启停）。
func TestGatewayRouteUpdatePersists(t *testing.T) {
	s := newGatewayPersistenceServer(t)
	auth := loginAsAdmin(t, s)
	created := createTestGatewayRoute(t, s, auth, `{"name":"upd1","pathPrefix":"/api/upd/","targetBackend":"http://127.0.0.1:9","timeoutMs":1500}`)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/gateway/routes/"+created.ID,
		strings.NewReader(`{"name":"upd1-renamed","pathPrefix":"/api/upd/","targetBackend":"http://127.0.0.1:8","timeoutMs":2000,"enabled":false}`))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayRouteRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("更新 status=%d want 200; body=%s", w.Code, w.Body.String())
	}

	s = &Server{
		store:        s.store,
		cfg:          s.cfg,
		jwtSecret:    s.jwtSecret,
		sessionStore: s.sessionStore,
		loginGuard:   s.loginGuard,
	}
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/gateway/routes", nil)
	req2.Header.Set("Authorization", auth)
	req2.Header.Set("X-Tenant-ID", "default")
	w2 := httptest.NewRecorder()
	s.handleListGatewayRoutes(w2, req2)
	body := w2.Body.String()
	for _, want := range []string{"upd1-renamed", "127.0.0.1:8", `"timeoutMs":2000`, `"enabled":false`} {
		if !strings.Contains(body, want) {
			t.Errorf("更新未落 store（缺 %s）: %s", want, body)
		}
	}
}

// TestGatewayRoutesTenantIsolatedInStore 两租户的路由互不可见（persistence 层的隔离键）。
func TestGatewayRoutesTenantIsolatedInStore(t *testing.T) {
	st := store.NewMemoryStore()
	if err := st.SaveGatewayRoute(&store.GatewayRoute{
		ID: "r-a", TenantID: "tenant-a", Name: "a", PathPrefix: "/a/", TargetBackend: "http://127.0.0.1:9",
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(st.ListGatewayRoutes("tenant-a")); got != 1 {
		t.Fatalf("tenant-a 应看到 1 条, got=%d", got)
	}
	if got := len(st.ListGatewayRoutes("tenant-b")); got != 0 {
		t.Fatalf("tenant-b 应看到 0 条（隔离）, got=%d", got)
	}
	if st.GetGatewayRoute("tenant-b", "r-a") != nil {
		t.Error("跨租户 Get 不应命中")
	}
	// upsert 幂等：同 (tenant,id) 再保存仍是 1 条
	if err := st.SaveGatewayRoute(&store.GatewayRoute{
		ID: "r-a", TenantID: "tenant-a", Name: "a2", PathPrefix: "/a2/", TargetBackend: "http://127.0.0.1:10",
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(st.ListGatewayRoutes("tenant-a")); got != 1 {
		t.Fatalf("upsert 后应仍 1 条, got=%d", got)
	}
	if got := st.GetGatewayRoute("tenant-a", "r-a"); got == nil || got.Name != "a2" {
		t.Fatalf("upsert 未更新字段: %+v", got)
	}
}

// TestRouteRuleGatewayRoundTrip 转换层零丢失（域形态 → 持久化 → 域形态）。
func TestRouteRuleGatewayRoundTrip(t *testing.T) {
	rule := &extension.RouteRule{
		ID: "r-1", TenantID: "t", Name: "n", PathPrefix: "/p/", TargetBackend: "http://h:1",
		Methods: []string{"GET", "POST"}, RateLimitPerSec: 5, TimeoutMs: 1234, Enabled: true,
	}
	back := gatewayToRouteRule(routeRuleToGateway(rule))
	if back.ID != rule.ID || back.TimeoutMs != rule.TimeoutMs || back.RateLimitPerSec != rule.RateLimitPerSec {
		t.Errorf("转换丢失字段: %+v", back)
	}
	if len(back.Methods) != 2 || back.Methods[1] != "POST" {
		t.Errorf("Methods 转换丢失: %v", back.Methods)
	}
}

// createTestGatewayRoute 调 handler 建一条路由，返回 rule（含后端生成的 ID）。
func createTestGatewayRoute(t *testing.T, s *Server, auth, bodyJSON string) *extension.RouteRule {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gateway/routes", strings.NewReader(bodyJSON))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayRoutes(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建路由 status=%d want 201; body=%s", w.Code, w.Body.String())
	}
	var rule extension.RouteRule
	if err := decodeJSONBodyTo(w.Body.Bytes(), &rule); err != nil {
		t.Fatalf("解析创建响应失败: %v", err)
	}
	return &rule
}

// decodeJSONBodyTo json 反序列化小助手（响应体已完整读入内存）。
func decodeJSONBodyTo(b []byte, v any) error {
	return json.Unmarshal(b, v)
}
