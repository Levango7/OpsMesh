// gateway_circuit_test.go TD-66 P2 数据面熔断的行为守卫。
//
// 立项文档 §4 验收标准第二条：「模拟后端连续返回 500 × 5 次 ⇒ 第 6 次请求直接 503
// （不再打到后端）；等 30s 后半开试探——后端恢复则关闭断路器。」
//
// 半开的冷却等待用「直接拨快 openedAt」的方式跳过真实 30s（与 circuit_test.go 同法），
// 但**前 5 次失败与第 6 次 fast-fail 完全走真实 HTTP 路径**。
package controlplane

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/extension"
)

// failBackend 构造一个"始终 500"的后端，并返回一个计数器（验证熔断后不再被打）。
func failBackend(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestGatewayCircuitOpensAfterFiveFailures 验收标准第二条前半：第 6 次 fast-fail 503 不再打后端。
func TestGatewayCircuitOpensAfterFiveFailures(t *testing.T) {
	backend, hits := failBackend(t)
	s := newGatewayRouteTestServer()
	auth := loginAsAdmin(t, s)

	addGatewayRouteForTest(s, &extension.RouteRule{ID: "r-circ", TenantID: "default", Name: "circ",
		PathPrefix: "/api/circ/", TargetBackend: backend.URL, Methods: []string{"GET"}, Enabled: true})

	// 前 5 次：每次真的打到后端（后端必然 500）。
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/gw/api/circ/x", nil)
		req.Header.Set("Authorization", auth)
		req.Header.Set("X-Tenant-ID", "default")
		w := httptest.NewRecorder()
		s.handleGatewayProxy(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("第 %d 次 status=%d, want 500（应透传后端失败）", i+1, w.Code)
		}
	}
	if got := atomic.LoadInt32(hits); got != 5 {
		t.Fatalf("熔断前应真实打后端 5 次, got=%d", got)
	}

	// 第 6 次：熔断 ⇒ 503 且后端不再被打（fast-fail 的意义）。
	before := atomic.LoadInt32(hits)
	req := httptest.NewRequest(http.MethodGet, "/gw/api/circ/x", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayProxy(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("熔断后 status=%d, want 503（fast-fail）", w.Code)
	}
	if got := atomic.LoadInt32(hits); got != before {
		t.Errorf("熔断后后端仍被打（%d→%d）——fast-fail 失效", before, got)
	}
}

// TestGatewayCircuitHalfOpenProbeRecovers 验收标准第二条后半：冷却+半开试探+后端恢复 ⇒ 关闭。
func TestGatewayCircuitHalfOpenProbeRecovers(t *testing.T) {
	// 可翻盘后端：前 5 次 500，之后 200。
	var mode int32 = 1 // 1=坏，0=好
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&mode) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	s := newGatewayRouteTestServer()
	auth := loginAsAdmin(t, s)
	created := &extension.RouteRule{ID: "r-circ2", TenantID: "default", Name: "circ2",
		PathPrefix: "/api/circ2/", TargetBackend: backend.URL, Methods: []string{"GET"}, Enabled: true}
	addGatewayRouteForTest(s, created)

	// 打到熔断。
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/gw/api/circ2/x", nil)
		req.Header.Set("Authorization", auth)
		req.Header.Set("X-Tenant-ID", "default")
		w := httptest.NewRecorder()
		s.handleGatewayProxy(w, req)
	}
	req6 := httptest.NewRequest(http.MethodGet, "/gw/api/circ2/x", nil)
	req6.Header.Set("Authorization", auth)
	req6.Header.Set("X-Tenant-ID", "default")
	w6 := httptest.NewRecorder()
	s.handleGatewayProxy(w6, req6)
	if w6.Code != http.StatusServiceUnavailable {
		t.Fatalf("前置失败：未熔断（status=%d）", w6.Code)
	}

	// 把冷却拨到结束 + 后端翻好。
	gw := s.ensureGateway()
	gw.mu.Lock()
	if e := gw.routes["default"][created.ID]; e != nil && e.breaker != nil {
		e.breaker.OpenedAtForTest()
	}
	gw.mu.Unlock()
	atomic.StoreInt32(&mode, 0)

	// 半开试探：应放行并成功 ⇒ 断路器关闭。
	req7 := httptest.NewRequest(http.MethodGet, "/gw/api/circ2/x", nil)
	req7.Header.Set("Authorization", auth)
	req7.Header.Set("X-Tenant-ID", "default")
	w7 := httptest.NewRecorder()
	s.handleGatewayProxy(w7, req7)
	if w7.Code != http.StatusOK {
		t.Fatalf("半开试探应被放行且成功（status=%d, body=%s）", w7.Code, w7.Body.String())
	}
	// 断路器已关 ⇒ 再打也应 200（不再被 503 拦）。
	req8 := httptest.NewRequest(http.MethodGet, "/gw/api/circ2/x", nil)
	req8.Header.Set("Authorization", auth)
	req8.Header.Set("X-Tenant-ID", "default")
	w8 := httptest.NewRecorder()
	s.handleGatewayProxy(w8, req8)
	if w8.Code != http.StatusOK {
		t.Fatalf("恢复后 status=%d, want 200", w8.Code)
	}
}

// TestGatewayCircuitNotAffectedByClientErrors 4xx 是业务应答不是后端故障 ⇒ 不熔断。
func TestGatewayCircuitNotAffectedByClientErrors(t *testing.T) {
	var hits int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer backend.Close()

	s := newGatewayRouteTestServer()
	auth := loginAsAdmin(t, s)
	created := &extension.RouteRule{ID: "r-circ3", TenantID: "default", Name: "circ3",
		PathPrefix: "/api/circ3/", TargetBackend: backend.URL, Methods: []string{"GET"}, Enabled: true}
	addGatewayRouteForTest(s, created)

	// 打 20 次全 4xx：一次都不应熔断（否则"用户传错参数"会把整条路由打挂）。
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/gw/api/circ3/x", nil)
		req.Header.Set("Authorization", auth)
		req.Header.Set("X-Tenant-ID", "default")
		w := httptest.NewRecorder()
		s.handleGatewayProxy(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("第 %d 次 status=%d, want 400（透传，不应熔断）", i+1, w.Code)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 20 {
		t.Fatalf("20 次 4xx 应全部真实透传, got=%d", got)
	}
	gw := s.ensureGateway()
	gw.mu.RLock()
	entry := gw.routes["default"][created.ID]
	gw.mu.RUnlock()
	if entry != nil && entry.breaker != nil && entry.breaker.State() != "closed" {
		t.Errorf("4xx 不应导致熔断, state=%s", entry.breaker.State())
	}
}

// TestGatewayRouteTimeoutIsApplied P4：路由超时从 TimeoutMs 读取并生效
// （验收标准第四条：配 timeoutMs=1000 的路由，后端 sleep 2s ⇒ 请求 504）。
func TestGatewayRouteTimeoutIsApplied(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // 超过路由超时
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	s := newGatewayRouteTestServer()
	auth := loginAsAdmin(t, s)
	createTestGatewayRoute(t, s, auth, fmt.Sprintf(
		`{"name":"slow","pathPrefix":"/slow/","targetBackend":"%s","timeoutMs":300,"methods":["GET"],"enabled":true}`, backend.URL))

	req := httptest.NewRequest(http.MethodGet, "/gw/slow/x", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayProxy(w, req)
	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("超时未生效：status=%d want 504; body=%s", w.Code, w.Body.String())
	}
}

// TestGatewayRouteTimeoutPersisted P1+P4 联动：timeoutMs 必须落 store（重启后仍生效）。
func TestGatewayRouteTimeoutPersisted(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	s := newGatewayRouteTestServer()
	auth := loginAsAdmin(t, s)
	created := createTestGatewayRoute(t, s, auth, fmt.Sprintf(
		`{"name":"t1","pathPrefix":"/t/","targetBackend":"%s","timeoutMs":300,"methods":["GET"],"enabled":true}`, backend.URL))
	if created.TimeoutMs != 300 {
		t.Fatalf("创建响应应带回 timeoutMs=300, got=%d", created.TimeoutMs)
	}

	// 重启后从 store 读回，超时配置仍在（数据面重启后保持语义）。
	s2 := &Server{
		store:        s.store,
		cfg:          s.cfg,
		jwtSecret:    s.jwtSecret,
		sessionStore: s.sessionStore,
		loginGuard:   s.loginGuard,
	}
	req := httptest.NewRequest(http.MethodGet, "/gw/t/x", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s2.handleGatewayProxy(w, req)
	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("重启后超时配置未生效：status=%d want 504; body=%s", w.Code, w.Body.String())
	}
}

// TestGatewayRouteDefaultTimeout 未配 timeoutMs ⇒ 用默认 30s（不被 0 值误杀）。
func TestGatewayRouteDefaultTimeout(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // 快响应，不触发超时
	}))
	defer backend.Close()

	s := newGatewayRouteTestServer()
	auth := loginAsAdmin(t, s)
	createTestGatewayRoute(t, s, auth, fmt.Sprintf(
		`{"name":"fast","pathPrefix":"/fast/","targetBackend":"%s","methods":["GET"],"enabled":true}`, backend.URL))

	req := httptest.NewRequest(http.MethodGet, "/gw/fast/x", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleGatewayProxy(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("默认超时不应影响正常快响应: status=%d body=%s", w.Code, w.Body.String())
	}
}
