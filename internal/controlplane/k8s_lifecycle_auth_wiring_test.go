package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/k8slifecycle"
)

// 回归：k8s 生命周期 9 条路由必须由 NewServer 注入鉴权回调。
// 此前该包的路由直接挂在 mux 上且没有任何鉴权闸，匿名即可触发升级 /
// etcd 恢复 / 节点扩缩容（见 docs/semantic-rulings-2026-10-30.md #1）。
func TestK8sLifecycleRoutesAreAuthorized(t *testing.T) {
	cfg := &config.Config{
		Mode:                "controlplane",
		Store:               "memory",
		EventBus:            "noop",
		RequireAuth:         true,
		TrustGatewayHeaders: false,
		GRPCPort:            0,
		HTTPPort:            0,
		MetricsPort:         0,
		TaskMaxRetries:      3,
	}
	s, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if s.k8sLifecycleHandler == nil {
		t.Fatal("k8sLifecycleHandler 未构造")
	}
	if s.k8sLifecycleHandler.Authorize == nil {
		t.Fatal("k8s 生命周期未注入鉴权回调——路由将匿名可达")
	}

	mux := http.NewServeMux()
	k8slifecycle.RegisterK8sLifecycleRoutes(mux, s.k8sLifecycleHandler)

	for _, path := range []string{
		"/api/v1/k8s/lifecycle/upgrade",
		"/api/v1/k8s/lifecycle/etcd-restore",
		"/api/v1/k8s/lifecycle/node-scale",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"clusterId":"c1","targetVersion":"1.30.0"}`))
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s 匿名请求 = %d, want 401", path, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/k8s/lifecycle/etcd-backups?cpNode=n1", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("etcd-backups 匿名请求 = %d, want 401", rec.Code)
	}
}
