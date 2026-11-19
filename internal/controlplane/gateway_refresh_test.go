// gateway_refresh_test.go 网关两处残余缺陷的行为守卫：
//
//  1. 缓存重载必须与首次加载走同一构造路径——重载后 breaker 非 nil，
//     连续 5 次 5xx 后第 6 次 fast-fail 503；
//  2. 多副本路由缓存必须收敛——副本 A 的 CRUD 在刷新周期内对副本 B 可见；
//  3. 重载与请求并发时无数据竞争/panic（并发读路径不持有可变 rule）。
package controlplane

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

// newCountingOKBackend 返回始终 200 的后端 + 命中计数器。
func newCountingOKBackend(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestGatewayReloadWiresCircuitBreaker 缺陷 ①：经 loadTenantRoutesLocked（重载路径）
// 构造的条目必须带熔断器，且熔断语义与首次加载一致。
func TestGatewayReloadWiresCircuitBreaker(t *testing.T) {
	backend, hits := failBackend(t)
	st := store.NewMemoryStore()
	// 直接落 store，模拟「重启后缓存为空、首个 /gw/ 请求触发按需加载」。
	if err := st.SaveGatewayRoute(&store.GatewayRoute{
		ID: "r-reload", TenantID: "default", Name: "reload",
		PathPrefix: "/api/reload/", TargetBackend: backend.URL,
		Methods: []string{"GET"}, Enabled: true,
	}); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	s := &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-gateway-test-32bytes!"),
		sessionStore: store.NewInProcessSessionStore(),
	}
	s.loginGuard = newLoginGuard(s.sessionStore)
	auth := loginAsAdmin(t, s)

	// 走重载路径（不是创建路径）。
	gw := s.ensureGateway()
	gw.mu.Lock()
	if err := s.loadTenantRoutesLocked(gw, "default"); err != nil {
		gw.mu.Unlock()
		t.Fatalf("reload: %v", err)
	}
	entry := gw.routes["default"]["r-reload"]
	gw.mu.Unlock()
	if entry == nil {
		t.Fatal("重载后条目缺失")
	}
	if entry.breaker == nil {
		t.Fatal("缺陷①：重载路径未接熔断器（breaker == nil）")
	}
	if entry.limiter == nil {
		t.Fatal("重载路径未接限流器（limiter == nil）")
	}

	// 5 次真实 5xx 后，第 6 次必须 503 且不再打后端。
	for i := 0; i < 5; i++ {
		w := proxyGatewayRequest(s, auth, "default", "/gw/api/reload/x")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("第 %d 次 status=%d want 500; body=%s", i+1, w.Code, w.Body.String())
		}
	}
	before := atomic.LoadInt32(hits)
	w := proxyGatewayRequest(s, auth, "default", "/gw/api/reload/x")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("熔断后 status=%d want 503; body=%s", w.Code, w.Body.String())
	}
	if got := atomic.LoadInt32(hits); got != before {
		t.Errorf("熔断后后端仍被打（%d→%d）——重载条目的 breaker 未生效", before, got)
	}
}

// TestGatewayRoutePropagatesAcrossReplicas 缺陷 ②：副本 A 建/删路由后，
// 副本 B 在刷新周期内收敛（不再永久陈旧）。
func TestGatewayRoutePropagatesAcrossReplicas(t *testing.T) {
	shared := store.NewMemoryStore()
	backend, _ := newCountingOKBackend(t)

	replicaA := newGatewayReplica(shared)
	replicaB := newGatewayReplica(shared)
	authA := loginAsAdmin(t, replicaA)
	authB := loginAsAdmin(t, replicaB)

	// B 先服务过一次（缓存已建立），模拟真实运行中的副本。
	if w := proxyGatewayRequest(replicaB, authB, "default", "/gw/api/repl/x"); w.Code != http.StatusNotFound {
		t.Fatalf("前置：B 初始无路由应 404, got=%d", w.Code)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	replicaB.startGatewayRouteRefresh(ctx, 10*time.Millisecond)

	// A 创建路由（写 store + A 的缓存）。
	created := createTestGatewayRoute(t, replicaA, authA, fmt.Sprintf(
		`{"name":"repl","pathPrefix":"/api/repl/","targetBackend":"%s","methods":["GET"],"enabled":true}`,
		backend.URL))

	deadline := time.Now().Add(time.Second)
	for {
		w := proxyGatewayRequest(replicaB, authB, "default", "/gw/api/repl/x")
		if w.Code == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("副本 B 在刷新周期内未看到 A 创建的路由（status=%d）", w.Code)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// A 删除路由后，B 也必须收敛（缓存不能只增不减）。
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/gateway/routes/"+created.ID, nil)
	delReq.Header.Set("Authorization", authA)
	delReq.Header.Set("X-Tenant-ID", "default")
	delW := httptest.NewRecorder()
	replicaA.handleGatewayRouteRouting(delW, delReq)
	if delW.Code != http.StatusOK {
		t.Fatalf("删除 status=%d; body=%s", delW.Code, delW.Body.String())
	}

	deadline = time.Now().Add(time.Second)
	for {
		w := proxyGatewayRequest(replicaB, authB, "default", "/gw/api/repl/x")
		if w.Code == http.StatusNotFound {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("副本 B 未收敛删除（status=%d）", w.Code)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestGatewayRouteConcurrentReadersDuringReload 重载与并发请求同时发生时不 panic、
// 不丢路由（map 读写均在锁内；rule 以写时复制方式替换）。
func TestGatewayRouteConcurrentReadersDuringReload(t *testing.T) {
	backend, hits := newCountingOKBackend(t)
	s := newGatewayRouteTestServer()
	auth := loginAsAdmin(t, s)
	created := createTestGatewayRoute(t, s, auth, fmt.Sprintf(
		`{"name":"race","pathPrefix":"/api/race/","targetBackend":"%s","methods":["GET"],"enabled":true}`,
		backend.URL))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var wg sync.WaitGroup
	errCh := make(chan string, 8)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				w := proxyGatewayRequest(s, auth, "default", "/gw/api/race/x")
				if w.Code != http.StatusOK {
					select {
					case errCh <- fmt.Sprintf("并发读 status=%d body=%s", w.Code, w.Body.String()):
					default:
					}
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 200; j++ {
			s.refreshGatewayRoutes(ctx)
		}
	}()
	wg.Wait()
	close(errCh)
	for msg := range errCh {
		t.Error(msg)
	}
	if got := atomic.LoadInt32(hits); got == 0 {
		t.Fatal("请求未打到后端")
	}

	// 并发风暴后路由仍在且可用（重载没有把缓存清空/写坏）。
	gw := s.ensureGateway()
	gw.mu.RLock()
	_, ok := gw.routes["default"][created.ID]
	gw.mu.RUnlock()
	if !ok {
		t.Fatal("并发重载后路由丢失")
	}
	if w := proxyGatewayRequest(s, auth, "default", "/gw/api/race/x"); w.Code != http.StatusOK {
		t.Fatalf("并发重载后 status=%d; body=%s", w.Code, w.Body.String())
	}
}

// newGatewayReplica 构造共享同一 store 的独立副本（独立 gatewayState/Once）。
func newGatewayReplica(st store.Store) *Server {
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-gateway-test-32bytes!"),
		sessionStore: store.NewInProcessSessionStore(),
		loginGuard:   newLoginGuard(store.NewInProcessSessionStore()),
	}
}

// proxyGatewayRequest 向数据面发一次带鉴权的请求。
func proxyGatewayRequest(s *Server, auth, tenant, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", tenant)
	w := httptest.NewRecorder()
	s.handleGatewayProxy(w, req)
	return w
}
