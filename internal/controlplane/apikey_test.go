// apikey_test.go 测试 Phase 6 API Key 管理 HTTP handler（apikey.go）。
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