// argocd_test.go 测试 Phase 2 ArgoCD 应用管理 HTTP handler（argocd.go）。
//
// 覆盖范围：
//   - handleListArgoCDApps：空列表、创建后列表
//   - handleCreateArgoCDApp：正常创建、缺必填字段、无效 JSON
//   - handleGetArgoCDApp：正常获取、不存在
//   - handleUpdateArgoCDApp：正常更新、不存在
//   - handleDeleteArgoCDApp：正常删除、不存在
//   - handleSyncArgoCDApp：同步应用
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 ticket_test.go 风格一致）：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore}；
//   - 鉴权用例通过 admin 登录获取 token（requirePermission 校验 argocd:read/write）。
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

// newArgoCDTestServer 构造 ArgoCD API 测试用 Server。
func newArgoCDTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-argocd-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// =============================================================================
// handleListArgoCDApps（GET /api/v1/argocd/apps）
// =============================================================================

// TestHandleListArgoCDApps_Empty 验证空列表返回 200 + apps:[]。
func TestHandleListArgoCDApps_Empty(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/argocd/apps", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleArgoCDApps(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Apps []*store.ArgoCDApp `json:"apps"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Apps) != 0 {
		t.Fatalf("apps=%d, want 0", len(resp.Apps))
	}
}

// TestHandleListArgoCDApps_AfterCreate 验证创建后列表含 1 个应用。
func TestHandleListArgoCDApps_AfterCreate(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	s.store.CreateApp("default", &store.ArgoCDApp{
		Name:      "my-app",
		Namespace: "production",
		RepoURL:   "https://github.com/org/repo",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/argocd/apps", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleArgoCDApps(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Apps []*store.ArgoCDApp `json:"apps"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Apps) != 1 {
		t.Fatalf("apps=%d, want 1", len(resp.Apps))
	}
	if resp.Apps[0].Name != "my-app" {
		t.Fatalf("Name=%q, want my-app", resp.Apps[0].Name)
	}
}

// TestHandleListArgoCDApps_NoAuth 验证无 Authorization 头返回 401。
func TestHandleListArgoCDApps_NoAuth(t *testing.T) {
	s := newArgoCDTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/argocd/apps", nil)
	w := httptest.NewRecorder()
	s.handleArgoCDApps(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// =============================================================================
// handleCreateArgoCDApp（POST /api/v1/argocd/apps）
// =============================================================================

// TestHandleCreateArgoCDApp 验证正常创建返回 201 + 应用（含 ID）。
func TestHandleCreateArgoCDApp(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"frontend","namespace":"default","repoURL":"https://github.com/org/repo","path":"k8s","targetRevision":"main","syncPolicy":"auto"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/argocd/apps", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleArgoCDApps(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var app store.ArgoCDApp
	if err := json.Unmarshal(w.Body.Bytes(), &app); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if app.ID == "" {
		t.Fatal("ID is empty, want server-assigned")
	}
	if app.Name != "frontend" {
		t.Fatalf("Name=%q, want frontend", app.Name)
	}
	if app.Namespace != "default" {
		t.Fatalf("Namespace=%q, want default", app.Namespace)
	}
}

// TestHandleCreateArgoCDApp_MissingName 验证缺 name 返回 400。
func TestHandleCreateArgoCDApp_MissingName(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"namespace":"default"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/argocd/apps", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleArgoCDApps(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateArgoCDApp_InvalidJSON 验证无效 JSON 返回 400。
func TestHandleCreateArgoCDApp_InvalidJSON(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":invalid`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/argocd/apps", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleArgoCDApps(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleGetArgoCDApp（GET /api/v1/argocd/apps/{id}）
// =============================================================================

// TestHandleGetArgoCDApp 验证正常获取应用详情。
func TestHandleGetArgoCDApp(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateApp("default", &store.ArgoCDApp{Name: "get-test", Namespace: "prod"})
	if created == nil {
		t.Fatal("CreateApp returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/argocd/apps/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleArgoCDApp(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var app store.ArgoCDApp
	if err := json.Unmarshal(w.Body.Bytes(), &app); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if app.ID != created.ID {
		t.Fatalf("ID=%q, want %q", app.ID, created.ID)
	}
	if app.Name != "get-test" {
		t.Fatalf("Name=%q, want get-test", app.Name)
	}
}

// TestHandleGetArgoCDApp_NotFound 验证获取不存在的应用返回 404。
func TestHandleGetArgoCDApp_NotFound(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/argocd/apps/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleArgoCDApp(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleUpdateArgoCDApp（PUT /api/v1/argocd/apps/{id}）
// =============================================================================

// TestHandleUpdateArgoCDApp 验证正常更新应用。
func TestHandleUpdateArgoCDApp(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateApp("default", &store.ArgoCDApp{Name: "update-test", Namespace: "dev"})
	if created == nil {
		t.Fatal("CreateApp returned nil")
	}

	body := `{"name":"updated-name","namespace":"prod","targetRevision":"v2"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/argocd/apps/"+created.ID, strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleArgoCDApp(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var app store.ArgoCDApp
	if err := json.Unmarshal(w.Body.Bytes(), &app); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if app.Name != "updated-name" {
		t.Fatalf("Name=%q, want updated-name", app.Name)
	}
	if app.Namespace != "prod" {
		t.Fatalf("Namespace=%q, want prod", app.Namespace)
	}
}

// TestHandleUpdateArgoCDApp_NotFound 验证更新不存在的应用返回 404。
func TestHandleUpdateArgoCDApp_NotFound(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"updated-name"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/argocd/apps/nonexistent", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleArgoCDApp(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleDeleteArgoCDApp（DELETE /api/v1/argocd/apps/{id}）
// =============================================================================

// TestHandleDeleteArgoCDApp 验证正常删除应用。
func TestHandleDeleteArgoCDApp(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateApp("default", &store.ArgoCDApp{Name: "delete-test"})
	if created == nil {
		t.Fatal("CreateApp returned nil")
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/argocd/apps/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleArgoCDApp(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	if _, ok := s.store.GetApp("default", created.ID); ok {
		t.Fatal("app still exists after delete")
	}
}

// TestHandleDeleteArgoCDApp_NotFound 验证删除不存在的应用返回 404。
func TestHandleDeleteArgoCDApp_NotFound(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/argocd/apps/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleArgoCDApp(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleSyncArgoCDApp（POST /api/v1/argocd/apps/{id}/sync）
// =============================================================================

// TestHandleSyncArgoCDApp 验证同步应用。
func TestHandleSyncArgoCDApp(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateApp("default", &store.ArgoCDApp{Name: "sync-test", Status: "outofsync"})
	if created == nil {
		t.Fatal("CreateApp returned nil")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/argocd/apps/"+created.ID+"/sync", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleArgoCDApp(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var app store.ArgoCDApp
	if err := json.Unmarshal(w.Body.Bytes(), &app); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if app.Status != "synced" {
		t.Fatalf("Status=%q, want synced", app.Status)
	}
	if app.HealthStatus != "healthy" {
		t.Fatalf("HealthStatus=%q, want healthy", app.HealthStatus)
	}
}

// TestHandleSyncArgoCDApp_NotFound 验证同步不存在的应用返回 404。
func TestHandleSyncArgoCDApp_NotFound(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/argocd/apps/nonexistent/sync", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleArgoCDApp(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// 路由分派
// =============================================================================

// TestHandleArgoCDApps_MethodNotAllowed 验证不支持的方法返回 405。
func TestHandleArgoCDApps_MethodNotAllowed(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/argocd/apps", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleArgoCDApps(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
}

// TestHandleArgoCDApp_EmptyID 验证空 id 返回 400。
func TestHandleArgoCDApp_EmptyID(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/argocd/apps/", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleArgoCDApp(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleArgoCDApp_UnknownSubPath 验证未知子路径返回 404。
func TestHandleArgoCDApp_UnknownSubPath(t *testing.T) {
	s := newArgoCDTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/argocd/apps/some-id/unknown", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleArgoCDApp(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}