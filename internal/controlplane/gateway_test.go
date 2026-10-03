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
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/extension"
	"github.com/Levango7/OpsMesh/internal/store"
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

// =============================================================================
// G2 修复：API 网关最小数据面（handleGatewayProxy）+ 统计真实计数
// =============================================================================

// addGatewayRouteForTest 直接向网关状态注入一条路由（绕过 HTTP handler，聚焦数据面）。
func addGatewayRouteForTest(s *Server, rule *extension.RouteRule) {
	gw := s.ensureGateway()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.routes[rule.TenantID] == nil {
		gw.routes[rule.TenantID] = make(map[string]*gatewayRouteEntry)
	}
	gw.routes[rule.TenantID][rule.ID] = &gatewayRouteEntry{
		rule:    rule,
		limiter: extension.NewRateLimiter(rule.RateLimitPerSec),
	}
}

// TestHandleGatewayProxy_ForwardsAndStats 验证 /gw/ 剥前缀转发到后端 + 统计真实计数。
func TestHandleGatewayProxy_ForwardsAndStats(t *testing.T) {
	s := newGatewayRouteTestServer()
	// 真实后端：回显收到的路径。
	var gotPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		// 睡 2ms：本机回环转发可在时钟粒度内完成，导致实测延迟恰为 0，
		// 使下方 AvgLatencyMs>0 断言随机失败（Windows 单调时钟粒度粗）。真实后端必有耗时。
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	addGatewayRouteForTest(s, &extension.RouteRule{
		ID:            "r1",
		TenantID:      "default",
		Name:          "echo",
		PathPrefix:    "/api/echo/",
		TargetBackend: backend.URL,
		Methods:       []string{"GET"},
		Enabled:       true,
	})

	req := httptest.NewRequest(http.MethodGet, "/gw/api/echo/hello", nil)
	req.Header.Set("Authorization", loginAsAdmin(t, s))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayProxy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	if gotPath != "/api/echo/hello" {
		t.Fatalf("backend got path=%q, want /api/echo/hello（/gw 前缀应被剥除）", gotPath)
	}
	// 统计：TotalRequests=1、无错误、AvgLatencyMs>0。
	gw := s.ensureGateway()
	gw.mu.RLock()
	stats := gw.stats
	gw.mu.RUnlock()
	if stats.TotalRequests != 1 {
		t.Fatalf("TotalRequests=%d, want 1", stats.TotalRequests)
	}
	if stats.TotalErrors != 0 {
		t.Fatalf("TotalErrors=%d, want 0", stats.TotalErrors)
	}
	if stats.AvgLatencyMs <= 0 {
		t.Fatalf("AvgLatencyMs=%v, want > 0", stats.AvgLatencyMs)
	}
}

// =============================================================================
// P0 回归：/gw/ 数据面鉴权（此前无任何鉴权 + 跨租户聚合路由）
// =============================================================================

// TestHandleGatewayProxy_RequiresAuth 验证无凭证请求被拒（401）。
//
// 回归背景：handleGatewayProxy 曾是全仓唯一不做鉴权的反向代理——任何能访问 8080 端口的
// 主体，无需登录即可经管理员配置的路由访问内部后端，绕过全部 RBAC。
// 本用例把该行为固化为"必须 401"，防止后续重构再次摘掉鉴权。
func TestHandleGatewayProxy_RequiresAuth(t *testing.T) {
	s := newGatewayRouteTestServer()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("未鉴权请求不应触达后端")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	addGatewayRouteForTest(s, &extension.RouteRule{
		ID: "r-auth", TenantID: "default", Name: "echo",
		PathPrefix: "/api/echo/", TargetBackend: backend.URL,
		Methods: []string{"GET"}, Enabled: true,
	})

	// 无 Authorization / 无 Cookie / 无网关注入角色 → requireProd 走不到任何身份分支。
	req := httptest.NewRequest(http.MethodGet, "/gw/api/echo/hello", nil)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayProxy(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401（/gw/ 必须鉴权）; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleGatewayProxy_TenantIsolation 验证 A 租户凭证无法命中 B 租户的路由。
//
// 回归背景：原实现遍历 gw.routes 全量聚合，路由池天然跨租户——任一租户的合法凭证
// 都能命中其他租户管理员配置的 TargetBackend。现只取 actx.TenantID 名下路由。
func TestHandleGatewayProxy_TenantIsolation(t *testing.T) {
	s := newGatewayRouteTestServer()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("跨租户请求不应触达后端")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	// 路由属于 victim 租户。
	addGatewayRouteForTest(s, &extension.RouteRule{
		ID: "r-victim", TenantID: "victim", Name: "echo",
		PathPrefix: "/api/echo/", TargetBackend: backend.URL,
		Methods: []string{"GET"}, Enabled: true,
	})

	// 攻击者以 default 租户合法身份请求同一路径。
	req := httptest.NewRequest(http.MethodGet, "/gw/api/echo/hello", nil)
	req.Header.Set("Authorization", loginAsAdmin(t, s))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayProxy(w, req)

	// 路由池为空 → 与"无匹配路由"同义，404。
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404（跨租户路由不可见）; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleGatewayProxy_RejectsMetadataTarget 验证 targetBackend 指向云元数据段被拒。
//
// 网关后端本就是内网微服务，故走 allowPrivate=true（放行私网/环回）；
// 但该模式仍硬拒链路本地/云元数据/0.0.0.0/8——169.254.169.254 是 SSRF 的头号目标。
func TestHandleGatewayProxy_RejectsMetadataTarget(t *testing.T) {
	s := newGatewayRouteTestServer()
	addGatewayRouteForTest(s, &extension.RouteRule{
		ID: "r-imds", TenantID: "default", Name: "imds",
		PathPrefix: "/api/imds/", TargetBackend: "http://169.254.169.254:80",
		Methods: []string{"GET"}, Enabled: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/gw/api/imds/latest/meta-data/", nil)
	req.Header.Set("Authorization", loginAsAdmin(t, s))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayProxy(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status=%d, want 502（元数据段应被拒）; body=%s", w.Code, w.Body.String())
	}
	// 拒绝原因不得回吐：ValidateWebhookURL 的错误含解析出的 IP 与拒绝类别，
	// 放进响应体等于把内网地址段与网关出网策略送给调用方。
	if body := w.Body.String(); strings.Contains(body, "169.254") || strings.Contains(body, "link-local") ||
		strings.Contains(body, "metadata") {
		t.Fatalf("502 响应体泄露了拒绝细节/内网地址；body=%s", body)
	}
}

// TestHandleGatewayProxy_NoRoute404 验证无匹配路由返回 404 且计入 TotalErrors。
func TestHandleGatewayProxy_NoRoute404(t *testing.T) {
	s := newGatewayRouteTestServer()
	req := httptest.NewRequest(http.MethodGet, "/gw/api/nomatch/x", nil)
	req.Header.Set("Authorization", loginAsAdmin(t, s))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayProxy(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
	gw := s.ensureGateway()
	gw.mu.RLock()
	stats := gw.stats
	gw.mu.RUnlock()
	if stats.TotalRequests != 1 {
		t.Fatalf("TotalRequests=%d, want 1", stats.TotalRequests)
	}
	if stats.TotalErrors != 1 {
		t.Fatalf("TotalErrors=%d, want 1（404 计入错误）", stats.TotalErrors)
	}
}

// TestHandleGatewayProxy_BackendError500 验证后端 >=500 响应计入 TotalErrors。
func TestHandleGatewayProxy_BackendError500(t *testing.T) {
	s := newGatewayRouteTestServer()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer backend.Close()

	addGatewayRouteForTest(s, &extension.RouteRule{
		ID:            "r-err",
		TenantID:      "default",
		Name:          "err",
		PathPrefix:    "/api/err/",
		TargetBackend: backend.URL,
		Enabled:       true,
	})

	req := httptest.NewRequest(http.MethodGet, "/gw/api/err/x", nil)
	req.Header.Set("Authorization", loginAsAdmin(t, s))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayProxy(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500（透传后端错误）", w.Code)
	}
	gw := s.ensureGateway()
	gw.mu.RLock()
	stats := gw.stats
	gw.mu.RUnlock()
	if stats.TotalErrors != 1 {
		t.Fatalf("TotalErrors=%d, want 1（后端 500 计入错误）", stats.TotalErrors)
	}
}

// TestHandleGatewayStats_RealCounts 验证 /gateway/stats 返回真实计数（不再恒为 0）。
func TestHandleGatewayStats_RealCounts(t *testing.T) {
	s := newGatewayRouteTestServer()
	auth := loginAsAdmin(t, s)

	// 先打一次 404 请求产生错误计数。
	// 必须带凭证：/gw/ 数据面自 P0 修复起有鉴权（原先零鉴权是全仓唯一裸奔的反向代理），
	// 无凭证请求会被 401 拦下且不计入 stats——那样这个测试就测不到统计逻辑了。
	req := httptest.NewRequest(http.MethodGet, "/gw/nope", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayProxy(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("前置请求应得 404（无匹配路由）；got=%d body=%s", w.Code, w.Body.String())
	}

	// 查 /gateway/stats。
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/gateway/stats", nil)
	req2.Header.Set("Authorization", auth)
	req2.Header.Set("X-Tenant-ID", "default")
	w2 := httptest.NewRecorder()
	s.handleGatewayStats(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w2.Code, w2.Body.String())
	}
	var stats extension.GatewayStats
	if err := json.Unmarshal(w2.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stats.TotalRequests == 0 {
		t.Fatal("TotalRequests=0, want >0（stats 不再恒为 0）")
	}
	if stats.TotalErrors == 0 {
		t.Fatal("TotalErrors=0, want >0（404 已计入）")
	}
}
