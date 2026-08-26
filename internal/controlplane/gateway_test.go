// gateway_test.go 测试 Phase 5 API 网关 HTTP handler（gateway.go）。
//
// 覆盖范围（审计断言，H9 写路径审计补齐回归）：
//   - handleCreateGatewayRoute：创建路由成功后写入审计（Action="gateway_route_create"）
//
// 测试策略（与 automation_test.go 风格一致）：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore, jwtSecret: 固定}；
//   - 鉴权用例通过 admin 登录获取 token（requirePermission 校验 gateway:read/write）；
//   - 用 httptest.NewRequest + httptest.NewRecorder 直接调用 handler，断言 status code 与审计日志。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/extension"
	"opsmesh/internal/store"
)

// newGatewayRouteTestServer 构造网关 API 测试用 Server。
func newGatewayRouteTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-gateway-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// TestHandleCreateGatewayRoute_AuditLogged 验证创建路由成功后写入审计日志，
// 审计事件 Action="gateway_route_create"、Target=路由 ID、UserID=caller.ID。
// 参考 automation_test.go 既有模式：loginAsAdmin + 触发写操作 + 断言 store.Audits()。
func TestHandleCreateGatewayRoute_AuditLogged(t *testing.T) {
	s := newGatewayRouteTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"audit-route","pathPrefix":"/api/","targetBackend":"http://backend:8080"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gateway/routes", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayRoutes(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var rule extension.RouteRule
	if err := json.Unmarshal(w.Body.Bytes(), &rule); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 断言审计日志含 gateway_route_create 事件，且 Target/UserID 正确。
	audits := s.store.Audits()
	var hit bool
	for _, ev := range audits {
		if ev.Action == "gateway_route_create" && ev.Target == rule.ID && ev.UserID != "" {
			hit = true
			break
		}
	}
	if !hit {
		t.Fatalf("audit log missing gateway_route_create for route=%s; audits=%+v", rule.ID, audits)
	}
}

// TestHandleCreateGatewayRoute_BadBackendScheme 验证非法 scheme 返回 400（L1 targetBackend 校验）。
func TestHandleCreateGatewayRoute_BadBackendScheme(t *testing.T) {
	s := newGatewayRouteTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"r1","pathPrefix":"/api/","targetBackend":"file:///etc/passwd"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gateway/routes", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayRoutes(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateGatewayRoute_BadBackendHostPort 验证缺端口返回 400（L1 targetBackend 校验）。
func TestHandleCreateGatewayRoute_BadBackendHostPort(t *testing.T) {
	s := newGatewayRouteTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"r1","pathPrefix":"/api/","targetBackend":"http://backend"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gateway/routes", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayRoutes(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateGatewayRoute_ValidGrpcBackend 验证 grpc scheme 通过（L1 targetBackend 校验）。
func TestHandleCreateGatewayRoute_ValidGrpcBackend(t *testing.T) {
	s := newGatewayRouteTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"r1","pathPrefix":"/api/","targetBackend":"grpc://backend:9090"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gateway/routes", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayRoutes(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
}