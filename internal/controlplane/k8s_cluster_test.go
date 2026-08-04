// k8s_cluster_test.go 测试 Phase 3 K8s 集群管理 HTTP handler（k8s_cluster.go）。
//
// 覆盖范围：
//   - handleListK8sClusters：空列表、创建后列表、kubeconfig 脱敏
//   - handleCreateK8sCluster：正常创建、缺必填字段、无效 JSON
//   - handleDeleteK8sCluster：正常删除、删除不存在、返回 204
//   - handleK8sClusters：method not allowed 分派
//   - handleK8sClusterRouting：{id} 路由分派、空 id、test 子路径
//   - 鉴权：无 token 返回 401
//
// 测试策略：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore, jwtSecret: 固定}；
//   - 鉴权用例通过 admin 登录获取 token（requirePermission 校验 user:read/write/delete）；
//   - clusterMgr 设为 nil 跳过 client-go 连接尝试（避免依赖真实 K8s 集群），
//     此时创建的集群 Status 保持 "unknown"；
//   - 用 httptest.NewRequest + httptest.NewRecorder 直接调用 handler，断言 status code 与响应体。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// newK8sTestServer 构造 K8s 集群 API 测试用 Server：
//   - memory store（NewMemoryStore 已 seedRBAC，预置 admin/admin123）；
//   - 固定 jwtSecret（避免随机性）；
//   - clusterMgr = nil（跳过 client-go 连接，创建集群时 Status 保持 "unknown"）。
func newK8sTestServer() *Server {
	st := store.NewMemoryStore()
	return &Server{
		store:      st,
		cfg:        &config.Config{TaskMaxRetries: 3},
		jwtSecret:  []byte("test-jwt-secret-for-k8s-cluster-test-32b!"),
		loginGuard: newLoginGuard(),
		// clusterMgr 故意留 nil：测试不依赖真实 K8s 集群，handleCreateK8sCluster 检测 nil 后跳过连接。
	}
}

// =============================================================================
// handleListK8sClusters（GET /api/v1/k8s/clusters）
// =============================================================================

// TestHandleListK8sClusters_Empty 验证空列表返回 200 + clusters:[]。
func TestHandleListK8sClusters_Empty(t *testing.T) {
	s := newK8sTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleK8sClusters(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Clusters []*store.K8sCluster `json:"clusters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Clusters) != 0 {
		t.Fatalf("clusters=%d, want 0", len(resp.Clusters))
	}
}

// TestHandleListK8sClusters_AfterCreate 验证创建后列表含 1 个集群且 kubeconfig 脱敏。
func TestHandleListK8sClusters_AfterCreate(t *testing.T) {
	s := newK8sTestServer()
	auth := loginAsAdmin(t, s)

	// 先直接写一条集群到 store（绕过 handler，避免 clusterMgr 干扰）
	s.store.SaveK8sCluster(&store.K8sCluster{
		ID:         "cluster-list-1",
		Name:       "list-test",
		Server:     "https://1.2.3.4:6443",
		Kubeconfig: "apiVersion: v1\nclusters: []",
		Status:     "online",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleK8sClusters(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Clusters []*store.K8sCluster `json:"clusters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Clusters) != 1 {
		t.Fatalf("clusters=%d, want 1", len(resp.Clusters))
	}
	c := resp.Clusters[0]
	if c.Name != "list-test" {
		t.Fatalf("Name=%q, want list-test", c.Name)
	}
	// kubeconfig 必须脱敏
	if c.Kubeconfig != k8sClusterKubeconfigMasked {
		t.Fatalf("Kubeconfig=%q, want %q (masked)", c.Kubeconfig, k8sClusterKubeconfigMasked)
	}
}

// TestHandleListK8sClusters_NoAuth 验证无 Authorization 头返回 401。
func TestHandleListK8sClusters_NoAuth(t *testing.T) {
	s := newK8sTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters", nil)
	w := httptest.NewRecorder()
	s.handleK8sClusters(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// =============================================================================
// handleCreateK8sCluster（POST /api/v1/k8s/clusters）
// =============================================================================

// TestHandleCreateK8sCluster 验证正常创建返回 201 + 脱敏后的集群（含 ID）。
func TestHandleCreateK8sCluster(t *testing.T) {
	s := newK8sTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"test","server":"https://1.2.3.4:6443","kubeconfig":"apiVersion: v1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleK8sClusters(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var c store.K8sCluster
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if c.ID == "" {
		t.Fatal("ID is empty, want server-assigned")
	}
	if c.Name != "test" {
		t.Fatalf("Name=%q, want test", c.Name)
	}
	if c.Server != "https://1.2.3.4:6443" {
		t.Fatalf("Server=%q, want https://1.2.3.4:6443", c.Server)
	}
	// kubeconfig 必须脱敏
	if c.Kubeconfig != k8sClusterKubeconfigMasked {
		t.Fatalf("Kubeconfig=%q, want %q (masked in response)", c.Kubeconfig, k8sClusterKubeconfigMasked)
	}
	// clusterMgr 为 nil，跳过连接尝试，Status 保持 "unknown"
	if c.Status != "unknown" {
		t.Fatalf("Status=%q, want unknown (clusterMgr nil)", c.Status)
	}

	// 确认集群已持久化到 store
	got := s.store.GetK8sCluster(c.ID)
	if got == nil {
		t.Fatal("GetK8sCluster returned nil after create")
	}
	// store 层保留原始 kubeconfig（未脱敏）
	if got.Kubeconfig != "apiVersion: v1" {
		t.Fatalf("stored Kubeconfig=%q, want original", got.Kubeconfig)
	}
}

// TestHandleCreateK8sCluster_MissingFields 验证缺 name 或 kubeconfig 返回 400。
func TestHandleCreateK8sCluster_MissingFields(t *testing.T) {
	s := newK8sTestServer()
	auth := loginAsAdmin(t, s)

	cases := []struct {
		name string
		body string
	}{
		{"missing name", `{"server":"https://x","kubeconfig":"cfg"}`},
		{"missing kubeconfig", `{"name":"x","server":"https://x"}`},
		{"both missing", `{"server":"https://x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters", strings.NewReader(tc.body))
			req.Header.Set("Authorization", auth)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.handleK8sClusters(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// TestHandleCreateK8sCluster_InvalidJSON 验证无效 JSON 返回 400。
func TestHandleCreateK8sCluster_InvalidJSON(t *testing.T) {
	s := newK8sTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters", strings.NewReader(`{not json`))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleK8sClusters(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateK8sCluster_NoAuth 验证无 Authorization 头返回 401。
func TestHandleCreateK8sCluster_NoAuth(t *testing.T) {
	s := newK8sTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters",
		strings.NewReader(`{"name":"x","kubeconfig":"y"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleK8sClusters(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// =============================================================================
// handleK8sClusters method 分派
// =============================================================================

// TestHandleK8sClusters_MethodNotAllowed 验证非 GET/POST 方法返回 405。
func TestHandleK8sClusters_MethodNotAllowed(t *testing.T) {
	s := newK8sTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/k8s/clusters", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleK8sClusters(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
}

// =============================================================================
// handleDeleteK8sCluster（DELETE /api/v1/k8s/clusters/{id}）
// =============================================================================

// TestHandleDeleteK8sCluster 验证删除已存在集群返回 204。
func TestHandleDeleteK8sCluster(t *testing.T) {
	s := newK8sTestServer()
	auth := loginAsAdmin(t, s)

	s.store.SaveK8sCluster(&store.K8sCluster{
		ID:         "cluster-del",
		Name:       "to-be-deleted",
		Kubeconfig: "cfg",
		Status:     "online",
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/k8s/clusters/cluster-del", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleK8sClusterRouting(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body=%s", w.Code, w.Body.String())
	}
	// 确认已从 store 移除
	if s.store.GetK8sCluster("cluster-del") != nil {
		t.Fatal("cluster still exists after delete")
	}
}

// TestHandleDeleteK8sCluster_NotFound 验证删除不存在的集群返回 404。
func TestHandleDeleteK8sCluster_NotFound(t *testing.T) {
	s := newK8sTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/k8s/clusters/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleK8sClusterRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleDeleteK8sCluster_NoAuth 验证无 Authorization 头返回 401。
func TestHandleDeleteK8sCluster_NoAuth(t *testing.T) {
	s := newK8sTestServer()
	s.store.SaveK8sCluster(&store.K8sCluster{ID: "c", Name: "c", Kubeconfig: "k"})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/k8s/clusters/c", nil)
	w := httptest.NewRecorder()
	s.handleK8sClusterRouting(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// =============================================================================
// handleK8sClusterRouting 路由分派
// =============================================================================

// TestHandleK8sClusterRouting_EmptyID 验证 /api/v1/k8s/clusters/（空 id）返回 400。
func TestHandleK8sClusterRouting_EmptyID(t *testing.T) {
	s := newK8sTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/k8s/clusters/", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleK8sClusterRouting(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleK8sClusterRouting_MethodNotAllowed 验证 /{id} 非 DELETE 方法返回 405。
func TestHandleK8sClusterRouting_MethodNotAllowed(t *testing.T) {
	s := newK8sTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/some-id", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleK8sClusterRouting(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
}

// =============================================================================
// kubeconfig 脱敏
// =============================================================================

// TestHandleK8sCluster_KubeconfigMasked 验证空 kubeconfig 不脱敏（保持空串），非空脱敏为 ***。
// 覆盖 maskK8sCluster / maskK8sClusters 的边界。
func TestHandleK8sCluster_KubeconfigMasked(t *testing.T) {
	s := newK8sTestServer()
	auth := loginAsAdmin(t, s)

	// 集群 A：非空 kubeconfig → 脱敏
	s.store.SaveK8sCluster(&store.K8sCluster{
		ID:         "c-mask",
		Name:       "masked",
		Kubeconfig: "secret-content",
		Status:     "online",
	})
	// 集群 B：空 kubeconfig → 保持空串
	s.store.SaveK8sCluster(&store.K8sCluster{
		ID:         "c-empty",
		Name:       "empty",
		Kubeconfig: "",
		Status:     "online",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleK8sClusters(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Clusters []*store.K8sCluster `json:"clusters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Clusters) != 2 {
		t.Fatalf("clusters=%d, want 2", len(resp.Clusters))
	}
	byID := map[string]*store.K8sCluster{}
	for _, c := range resp.Clusters {
		byID[c.ID] = c
	}
	if byID["c-mask"].Kubeconfig != k8sClusterKubeconfigMasked {
		t.Fatalf("c-mask Kubeconfig=%q, want %q", byID["c-mask"].Kubeconfig, k8sClusterKubeconfigMasked)
	}
	if byID["c-empty"].Kubeconfig != "" {
		t.Fatalf("c-empty Kubeconfig=%q, want empty (not masked)", byID["c-empty"].Kubeconfig)
	}
}

// TestMaskK8sCluster_Nil 验证 maskK8sCluster(nil) 返回 nil（边界保护）。
func TestMaskK8sCluster_Nil(t *testing.T) {
	if got := maskK8sCluster(nil); got != nil {
		t.Fatalf("maskK8sCluster(nil) = %+v, want nil", got)
	}
}

// TestMaskK8sClusters_Empty 验证 maskK8sClusters 空输入返回非 nil 空切片。
func TestMaskK8sClusters_Empty(t *testing.T) {
	got := maskK8sClusters(nil)
	if got == nil {
		t.Fatal("maskK8sClusters(nil) = nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}