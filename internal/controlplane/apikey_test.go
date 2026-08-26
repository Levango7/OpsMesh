// apikey_test.go 测试 Phase 6 API Key 管理 HTTP handler（apikey.go）。
package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// loginAsViewer 用预定义 viewer 账号登录，返回 Authorization 头值。
// viewer 角色仅含 *:read 权限，用于权限不足（403）测试。
// 复用 auth_test.go 中的 clearMustChangeFlag 清除首登改密标记。
func loginAsViewer(t *testing.T, s *Server) string {
	t.Helper()
	clearMustChangeFlag(s, "viewer")
	body, _ := json.Marshal(map[string]string{"username": "viewer", "password": "viewer123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleAuthLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer login = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp authResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode login resp: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("login token is empty")
	}
	return "Bearer " + resp.Token
}

// newAPIKeyTestServer 构造 API Key API 测试用 Server。
func newAPIKeyTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-apikey-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// TestHandleListAPIKeys_Empty 验证空列表返回 200。
func TestHandleListAPIKeys_Empty(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apikeys", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeys(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateAPIKey 验证正常创建返回 201 + 明文 key。
func TestHandleCreateAPIKey(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"test-key","scopes":["device:read"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apikeys", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeys(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		APIKey   *store.APIKey `json:"apiKey"`
		PlainKey string        `json:"plainKey"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.PlainKey == "" {
		t.Fatal("plainKey is empty")
	}
	if !strings.HasPrefix(resp.PlainKey, "om_") {
		t.Fatalf("plainKey=%q, want om_ prefix", resp.PlainKey)
	}
	if resp.APIKey == nil || resp.APIKey.ID == "" {
		t.Fatal("apiKey ID is empty")
	}
}

// TestHandleCreateAPIKey_MissingName 验证缺 name 返回 400。
func TestHandleCreateAPIKey_MissingName(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"scopes":["device:read"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apikeys", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeys(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleGetAPIKey 验证正常获取 API Key 详情。
func TestHandleGetAPIKey(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateAPIKey("default", &store.APIKey{Name: "test-key", Key: "hash", Enabled: true})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apikeys/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleGetAPIKey_NotFound 验证获取不存在 API Key 返回 404。
func TestHandleGetAPIKey_NotFound(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apikeys/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// TestHandleDeleteAPIKey 验证正常删除 API Key。
func TestHandleDeleteAPIKey(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateAPIKey("default", &store.APIKey{Name: "test-key", Key: "hash", Enabled: true})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/apikeys/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleDisableAPIKey 验证禁用 API Key。
func TestHandleDisableAPIKey(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateAPIKey("default", &store.APIKey{Name: "test-key", Key: "hash", Enabled: true})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apikeys/"+created.ID+"/disable", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var k store.APIKey
	if err := json.Unmarshal(w.Body.Bytes(), &k); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if k.Enabled {
		t.Fatal("Enabled=true, want false")
	}
}

// TestHandleEnableAPIKey 验证启用 API Key。
func TestHandleEnableAPIKey(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateAPIKey("default", &store.APIKey{Name: "test-key", Key: "hash", Enabled: false})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apikeys/"+created.ID+"/enable", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var k store.APIKey
	if err := json.Unmarshal(w.Body.Bytes(), &k); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !k.Enabled {
		t.Fatal("Enabled=false, want true")
	}
}

// TestHandleAPIKeys_NoAuth 验证无 Authorization 头返回 401。
func TestHandleAPIKeys_NoAuth(t *testing.T) {
	s := newAPIKeyTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apikeys", nil)
	w := httptest.NewRecorder()
	s.handleAPIKeys(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// TestHandleAPIKeyRouting_EmptyID 验证空 id 返回 400。
func TestHandleAPIKeyRouting_EmptyID(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apikeys/", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleUpdateAPIKey_EnabledNotChanged 防回归：PUT 携带 enabled=false 不能改变启停状态。
// 启停必须走 /enable|/disable 端点，避免客户端通过 PUT 提权绕过审计。
func TestHandleUpdateAPIKey_EnabledNotChanged(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateAPIKey("default", &store.APIKey{
		Name: "orig-key", Key: "orig-hash", Enabled: true, Scopes: []string{"device:read"},
	})
	// 客户端尝试 PUT {"enabled":false} 提权禁用绕过审计。
	body := `{"name":"renamed","scopes":["device:read"],"enabled":false}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/apikeys/"+created.ID, strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var k store.APIKey
	if err := json.Unmarshal(w.Body.Bytes(), &k); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !k.Enabled {
		t.Fatal("PUT enabled=false took effect; Enabled=false, want true (whitelist must ignore enabled)")
	}
	if k.Name != "renamed" {
		t.Fatalf("Name=%q, want \"renamed\"", k.Name)
	}
}

// TestHandleUpdateAPIKey_EmptyScopesRejected 防回归：PUT 清空 scopes 必须返 400。
// 防止客户端把权限范围置空绕过 scope 校验提权。
func TestHandleUpdateAPIKey_EmptyScopesRejected(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateAPIKey("default", &store.APIKey{
		Name: "orig-key", Key: "orig-hash", Enabled: true, Scopes: []string{"device:read"},
	})
	body := `{"name":"renamed","scopes":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/apikeys/"+created.ID, strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleUpdateAPIKey_KeyHashPreserved 防回归：PUT 后 KeyHash（Key 字段）必须保持不变。
// Key 字段 json:"-" 客户端无法提交，但后端必须显式保留 existing.Key，防止被空值覆盖。
func TestHandleUpdateAPIKey_KeyHashPreserved(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	const origHash = "secret-sha256-hash-xyz"
	created := s.store.CreateAPIKey("default", &store.APIKey{
		Name: "orig-key", Key: origHash, Enabled: true, Scopes: []string{"device:read"},
	})
	body := `{"name":"renamed","scopes":["device:read","task:write"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/apikeys/"+created.ID, strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	// 直接从 store 取，校验底层 KeyHash 未被覆盖。
	k, ok := s.store.GetAPIKey("default", created.ID)
	if !ok || k == nil {
		t.Fatal("GetAPIKey returned not found after update")
	}
	if k.Key != origHash {
		t.Fatalf("Key=%q, want %q (KeyHash must be preserved)", k.Key, origHash)
	}
	// 顺便校验 scopes 已更新为合并后的值。
	if len(k.Scopes) != 2 || k.Scopes[0] != "device:read" || k.Scopes[1] != "task:write" {
		t.Fatalf("Scopes=%v, want [device:read task:write]", k.Scopes)
	}
}

// TestHandleUpdateAPIKey_NotFound 防回归：PUT 不存在 ID 返 404。
func TestHandleUpdateAPIKey_NotFound(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"renamed","scopes":["device:read"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/apikeys/nonexistent", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// ============================================================================
// M10 错误路径补强：无效 key（404）/权限不足（403）/租户隔离（403）/缺租户（401）/405/400
// ============================================================================

// TestHandleDeleteAPIKey_NotFound 验证删除不存在 API Key 返回 404。
func TestHandleDeleteAPIKey_NotFound(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/apikeys/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleEnableAPIKey_NotFound 验证启用不存在 API Key 返回 404。
func TestHandleEnableAPIKey_NotFound(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apikeys/nonexistent/enable", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleDisableAPIKey_NotFound 验证禁用不存在 API Key 返回 404。
func TestHandleDisableAPIKey_NotFound(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apikeys/nonexistent/disable", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleAPIKeys_MethodNotAllowed 验证 PATCH /apikeys 返回 405。
func TestHandleAPIKeys_MethodNotAllowed(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/apikeys", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeys(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleAPIKeyRouting_UnknownSubPath 验证未知子路径返回 404。
func TestHandleAPIKeyRouting_UnknownSubPath(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateAPIKey("default", &store.APIKey{Name: "k1", Key: "h", Enabled: true})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apikeys/"+created.ID+"/frobnicate", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateAPIKey_InvalidJSON 验证非法 JSON body 返回 400。
func TestHandleCreateAPIKey_InvalidJSON(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"k1","scopes":["device:read"]` // 缺右大括号
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apikeys", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeys(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleListAPIKeys_TokenFallbackNoHeader 验证缺 X-Tenant-ID 头时 token 回退到 default。
// admin token 携带 tenant_id=default，requireTenantContext 头空时回退到 token tenant → 200。
// 锁定 token 回退契约：无网关直连场景下 token 携带租户身份即可访问，不强制 X-Tenant-ID 头。
func TestHandleListAPIKeys_TokenFallbackNoHeader(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apikeys", nil)
	req.Header.Set("Authorization", auth)
	// 故意不设 X-Tenant-ID：requireTenantContext 回退到 token tenant_id=default
	w := httptest.NewRecorder()
	s.handleAPIKeys(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 (token 回退 default); body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateAPIKey_TokenFallbackNoHeader 验证缺 X-Tenant-ID 头时创建走 token 回退。
func TestHandleCreateAPIKey_TokenFallbackNoHeader(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"k1","scopes":["device:read"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apikeys", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	// 故意不设 X-Tenant-ID：requireTenantContext 回退到 token tenant_id=default
	w := httptest.NewRecorder()
	s.handleAPIKeys(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201 (token 回退 default 创建成功); body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateAPIKey_ViewerForbidden 验证 viewer（无 apikey:write）创建返回 403。
func TestHandleCreateAPIKey_ViewerForbidden(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsViewer(t, s)
	body := `{"name":"k1","scopes":["device:read"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apikeys", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleAPIKeys(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleGetAPIKey_TenantIsolation 验证跨租户访问 API Key 返回 403。
// admin token 携带 tenant_id=default，带 X-Tenant-ID:other-tenant 访问，
// requireTenantContext 交叉校验 token tenant ≠ header tenant → 403。
func TestHandleGetAPIKey_TenantIsolation(t *testing.T) {
	s := newAPIKeyTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateAPIKey("default", &store.APIKey{Name: "k1", Key: "h", Enabled: true})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apikeys/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "other-tenant") // 跨租户头
	w := httptest.NewRecorder()
	s.handleAPIKeyRouting(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%s", w.Code, w.Body.String())
	}
}
