// k8s_manage_test.go 测试 K8s 资源管理路由与纯函数（k8s_manage.go）。
//
// 覆盖范围：
//   - handleK8sResourceRouting：clusterMgr 未初始化、集群不存在、租户不匹配、集群未连接、未知资源
//   - routePods：方法校验、参数缺失、未知子路径
//   - routeDeployments：方法校验、参数缺失、未知子路径
//   - formatAge：纯函数（零值/秒/分/时/天/未来时间）
//
// 测试策略：白盒（package controlplane），直接调用路由函数。
// 实际 K8s API 调用需要真实集群连接，此处聚焦路由逻辑与错误处理。
package controlplane

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/k8s"
	"opsmesh/internal/store"
)

// newK8sManageTestServer 构造 K8s 资源管理测试用 Server。
func newK8sManageTestServer() *Server {
	st := store.NewMemoryStore()
	return &Server{
		store:       st,
		cfg:         &config.Config{TaskMaxRetries: 3, Demo: true},
		requireAuth: false,
		clusterMgr:  k8s.NewClusterManager(),
	}
}

// =============================================================================
// formatAge 纯函数
// =============================================================================

func TestFormatAge_Zero(t *testing.T) {
	if got := formatAge(time.Time{}); got != "" {
		t.Fatalf("zero time should return empty, got %q", got)
	}
}

func TestFormatAge_Seconds(t *testing.T) {
	now := time.Now()
	got := formatAge(now.Add(-30 * time.Second))
	if got != "30s" {
		t.Fatalf("got=%q, want 30s", got)
	}
}

func TestFormatAge_Minutes(t *testing.T) {
	now := time.Now()
	got := formatAge(now.Add(-5 * time.Minute))
	if got != "5m" {
		t.Fatalf("got=%q, want 5m", got)
	}
}

func TestFormatAge_Hours(t *testing.T) {
	now := time.Now()
	got := formatAge(now.Add(-3 * time.Hour))
	if got != "3h" {
		t.Fatalf("got=%q, want 3h", got)
	}
}

func TestFormatAge_Days(t *testing.T) {
	now := time.Now()
	got := formatAge(now.Add(-7 * 24 * time.Hour))
	if got != "7d" {
		t.Fatalf("got=%q, want 7d", got)
	}
}

func TestFormatAge_FutureTime(t *testing.T) {
	// 未来时间取绝对值，应返回正数秒。
	now := time.Now()
	got := formatAge(now.Add(10 * time.Second))
	if got != "10s" {
		t.Fatalf("got=%q, want 10s", got)
	}
}

// =============================================================================
// handleK8sResourceRouting 错误路径
// =============================================================================

func TestHandleK8sResourceRouting_NilClusterMgr(t *testing.T) {
	s := newK8sManageTestServer()
	s.clusterMgr = nil
	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/namespaces", nil)
	rec := httptest.NewRecorder()
	s.handleK8sResourceRouting(rec, req, "c1", "namespaces", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleK8sResourceRouting_ClusterNotFound(t *testing.T) {
	s := newK8sManageTestServer()
	// 集群不在 store 中。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/no-such/namespaces", nil)
	rec := httptest.NewRecorder()
	s.handleK8sResourceRouting(rec, req, "no-such", "namespaces", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleK8sResourceRouting_TenantMismatch(t *testing.T) {
	s := newK8sManageTestServer()
	// 集群属于 t1 租户，但请求头声明 t2。
	s.store.SaveK8sCluster(&store.K8sCluster{ID: "c1", TenantID: "t1", Name: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/namespaces", nil)
	req.Header.Set("X-Tenant-ID", "t2")
	rec := httptest.NewRecorder()
	s.handleK8sResourceRouting(rec, req, "c1", "namespaces", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 (tenant mismatch); body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleK8sResourceRouting_NotConnected(t *testing.T) {
	s := newK8sManageTestServer()
	// 集群在 store 中但未连接（clusterMgr 为空 manager）。
	s.store.SaveK8sCluster(&store.K8sCluster{ID: "c1", TenantID: "default", Name: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/namespaces", nil)
	rec := httptest.NewRecorder()
	s.handleK8sResourceRouting(rec, req, "c1", "namespaces", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 (not connected); body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// routePods 方法校验与错误路径
// =============================================================================

func TestRoutePods_ListMethodNotAllowed(t *testing.T) {
	s := newK8sManageTestServer()
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/pods", nil)
	rec := httptest.NewRecorder()
	s.routePods(rec, req, client, "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestRoutePods_LogsMethodNotAllowed(t *testing.T) {
	s := newK8sManageTestServer()
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/k8s/clusters/c1/pods/ns/pod/logs", nil)
	rec := httptest.NewRecorder()
	s.routePods(rec, req, client, "ns/pod/logs")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestRoutePods_DeleteMethodNotAllowed(t *testing.T) {
	s := newK8sManageTestServer()
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/pods/ns/pod", nil)
	rec := httptest.NewRecorder()
	s.routePods(rec, req, client, "ns/pod")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestRoutePods_MissingNamespace(t *testing.T) {
	s := newK8sManageTestServer()
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/k8s/clusters/c1/pods//pod", nil)
	rec := httptest.NewRecorder()
	s.routePods(rec, req, client, "/pod")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRoutePods_MissingName(t *testing.T) {
	s := newK8sManageTestServer()
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/k8s/clusters/c1/pods/ns/", nil)
	rec := httptest.NewRecorder()
	s.routePods(rec, req, client, "ns/")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRoutePods_UnknownSubPath(t *testing.T) {
	s := newK8sManageTestServer()
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/pods/ns/pod/unknown", nil)
	rec := httptest.NewRecorder()
	s.routePods(rec, req, client, "ns/pod/unknown")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// routeDeployments 方法校验与错误路径
// =============================================================================

func TestRouteDeployments_ListMethodNotAllowed(t *testing.T) {
	s := newK8sManageTestServer()
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/deployments", nil)
	rec := httptest.NewRecorder()
	s.routeDeployments(rec, req, client, "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestRouteDeployments_ScaleMethodNotAllowed(t *testing.T) {
	s := newK8sManageTestServer()
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/deployments/ns/dep/scale", nil)
	rec := httptest.NewRecorder()
	s.routeDeployments(rec, req, client, "ns/dep/scale")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestRouteDeployments_RestartMethodNotAllowed(t *testing.T) {
	s := newK8sManageTestServer()
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/deployments/ns/dep/restart", nil)
	rec := httptest.NewRecorder()
	s.routeDeployments(rec, req, client, "ns/dep/restart")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestRouteDeployments_RollbackMethodNotAllowed(t *testing.T) {
	s := newK8sManageTestServer()
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/deployments/ns/dep/rollback", nil)
	rec := httptest.NewRecorder()
	s.routeDeployments(rec, req, client, "ns/dep/rollback")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestRouteDeployments_MissingNamespace(t *testing.T) {
	s := newK8sManageTestServer()
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/deployments//dep/scale", nil)
	rec := httptest.NewRecorder()
	s.routeDeployments(rec, req, client, "/dep/scale")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRouteDeployments_MissingName(t *testing.T) {
	s := newK8sManageTestServer()
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/deployments/ns//scale", nil)
	rec := httptest.NewRecorder()
	s.routeDeployments(rec, req, client, "ns//scale")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRouteDeployments_UnknownSubPath(t *testing.T) {
	s := newK8sManageTestServer()
	client := &k8s.K8sClient{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/deployments/ns/dep/unknown", nil)
	rec := httptest.NewRecorder()
	s.routeDeployments(rec, req, client, "ns/dep/unknown")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}