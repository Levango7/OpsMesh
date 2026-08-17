package controlplane

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/k8s"
	"opsmesh/internal/store"
)

// 本文件补全 k8s_cluster.go 中 0% 覆盖的 handleTestK8sCluster 和 decryptKubeconfig。

func newK8sClusterTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-k8s-cluster-extra-32!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
		clusterMgr:   k8s.NewClusterManager(),
	}
}

// =============================================================================
// handleTestK8sCluster
// =============================================================================

func TestHandleTestK8sCluster_MethodNotAllowed_Extra(t *testing.T) {
	s := newK8sClusterTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/test", nil)
	rec := httptest.NewRecorder()
	s.handleTestK8sCluster(rec, req, "c1")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

func TestHandleTestK8sCluster_NoAuth(t *testing.T) {
	s := newK8sClusterTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/test", nil)
	rec := httptest.NewRecorder()
	s.handleTestK8sCluster(rec, req, "c1")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

func TestHandleTestK8sCluster_NotFound_Extra(t *testing.T) {
	s := newK8sClusterTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/nope/test", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleTestK8sCluster(rec, req, "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestHandleTestK8sCluster_TenantMismatch_Extra(t *testing.T) {
	s := newK8sClusterTestServer()
	auth := loginAsAdmin(t, s)
	// 保存一个 default 租户的集群
	s.store.SaveK8sCluster(&store.K8sCluster{ID: "c1", TenantID: "default", Name: "test", Status: "unknown"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/test", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "other")
	rec := httptest.NewRecorder()
	s.handleTestK8sCluster(rec, req, "c1")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("tenant mismatch: %d, want 404", rec.Code)
	}
}

func TestHandleTestK8sCluster_Offline_Extra(t *testing.T) {
	s := newK8sClusterTestServer()
	auth := loginAsAdmin(t, s)
	// 保存一个集群，kubeconfig 为空（TestCluster 会失败）
	s.store.SaveK8sCluster(&store.K8sCluster{ID: "c1", TenantID: "default", Name: "test", Status: "unknown", Kubeconfig: "invalid"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/test", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleTestK8sCluster(rec, req, "c1")
	// TestCluster 会因 invalid kubeconfig 失败，返回 offline
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleTestK8sCluster_NilClusterMgr_Extra(t *testing.T) {
	s := newK8sClusterTestServer()
	s.clusterMgr = nil
	auth := loginAsAdmin(t, s)
	s.store.SaveK8sCluster(&store.K8sCluster{ID: "c1", TenantID: "default", Name: "test", Status: "unknown"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/test", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleTestK8sCluster(rec, req, "c1")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", rec.Code)
	}
}

// =============================================================================
// decryptKubeconfig
// =============================================================================

func TestDecryptKubeconfig_Empty_Extra(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	out, err := s.decryptKubeconfig("")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if out != "" {
		t.Errorf("empty input: got %q, want empty", out)
	}
}

func TestDecryptKubeconfig_NoEncryptionKey_Extra(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	out, err := s.decryptKubeconfig("plain-kubeconfig")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if out != "plain-kubeconfig" {
		t.Errorf("no key: got %q, want plain", out)
	}
}

// =============================================================================
// encryptKubeconfig
// =============================================================================

func TestEncryptKubeconfig_Empty_Extra(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	out, err := s.encryptKubeconfig("")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if out != "" {
		t.Errorf("empty: got %q", out)
	}
}

func TestEncryptKubeconfig_NoKey_Extra(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	out, err := s.encryptKubeconfig("plain")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if out != "plain" {
		t.Errorf("no key: got %q, want plain", out)
	}
}
