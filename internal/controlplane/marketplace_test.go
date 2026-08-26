// marketplace_test.go 测试 Phase 6 插件市场 HTTP handler（marketplace.go）。
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

// newMarketplaceTestServer 构造插件市场 API 测试用 Server。
func newMarketplaceTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-marketplace-test-32!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// TestHandleListPlugins_Empty 验证空列表返回 200。
func TestHandleListPlugins_Empty(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/marketplace/plugins", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleMarketplacePlugins(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreatePlugin 验证正常创建返回 201。
func TestHandleCreatePlugin(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"my-plugin","version":"1.0.0","type":"data","description":"test plugin"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleMarketplacePlugins(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var p store.Plugin
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.ID == "" {
		t.Fatal("ID is empty")
	}
}

// TestHandleCreatePlugin_MissingName 验证缺 name 返回 400。
func TestHandleCreatePlugin_MissingName(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"version":"1.0.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleMarketplacePlugins(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleGetPlugin 验证正常获取插件详情。
func TestHandleGetPlugin(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreatePlugin(&store.Plugin{Name: "my-plugin", Version: "1.0.0"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/marketplace/plugins/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleMarketplacePluginRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleDeletePlugin 验证正常删除插件。
func TestHandleDeletePlugin(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreatePlugin(&store.Plugin{Name: "my-plugin", Version: "1.0.0"})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/marketplace/plugins/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleMarketplacePluginRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleInstallPlugin 验证安装插件。
func TestHandleInstallPlugin(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreatePlugin(&store.Plugin{Name: "my-plugin", Version: "1.0.0"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins/"+created.ID+"/install", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleMarketplacePluginRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var p store.Plugin
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !p.Installed {
		t.Fatal("Installed=false, want true")
	}
}

// TestHandleUninstallPlugin 验证卸载插件。
func TestHandleUninstallPlugin(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreatePlugin(&store.Plugin{Name: "my-plugin", Version: "1.0.0", Installed: true, Enabled: true})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins/"+created.ID+"/uninstall", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleMarketplacePluginRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var p store.Plugin
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Installed {
		t.Fatal("Installed=true, want false")
	}
}

// TestHandleEnablePlugin 验证启用插件。
func TestHandleEnablePlugin(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreatePlugin(&store.Plugin{Name: "my-plugin", Version: "1.0.0", Enabled: false})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins/"+created.ID+"/enable", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleMarketplacePluginRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleMarketplacePlugins_NoAuth 验证无 Authorization 头返回 401。
func TestHandleMarketplacePlugins_NoAuth(t *testing.T) {
	s := newMarketplaceTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/marketplace/plugins", nil)
	w := httptest.NewRecorder()
	s.handleMarketplacePlugins(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// TestHandleMarketplacePluginRouting_EmptyID 验证空 id 返回 400。
func TestHandleMarketplacePluginRouting_EmptyID(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/marketplace/plugins/", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleMarketplacePluginRouting(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleCreatePlugin_InvalidType 验证非法 pluginType 返回 400（L1 白名单）。
func TestHandleCreatePlugin_InvalidType(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"p1","version":"1.0.0","type":"agent"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleMarketplacePlugins(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreatePlugin_MissingType 验证缺 type 返回 400（L1 白名单）。
func TestHandleCreatePlugin_MissingType(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"p1","version":"1.0.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleMarketplacePlugins(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreatePlugin_BadDownloadURLScheme 验证 downloadURL 非 http/https scheme 返回 400（L1 scheme 校验）。
func TestHandleCreatePlugin_BadDownloadURLScheme(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"p1","version":"1.0.0","type":"data","downloadURL":"file:///etc/passwd"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleMarketplacePlugins(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreatePlugin_ValidDownloadURL 验证 downloadURL=https 通过（L1 scheme 校验）。
func TestHandleCreatePlugin_ValidDownloadURL(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"p1","version":"1.0.0","type":"logic","downloadURL":"https://example.com/p.tar.gz"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleMarketplacePlugins(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
}

// ============================================================================
// M10 错误路径补强：404（插件不存在）/405（方法不允许）/400（参数缺失）
// ============================================================================

// TestHandleGetPlugin_NotFound 验证获取不存在插件返回 404。
func TestHandleGetPlugin_NotFound(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/marketplace/plugins/nonexistent-id", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleMarketplacePluginRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleDeletePlugin_NotFound 验证删除不存在插件返回 404。
func TestHandleDeletePlugin_NotFound(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/marketplace/plugins/nonexistent-id", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleMarketplacePluginRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleInstallPlugin_NotFound 验证安装不存在插件返回 404。
func TestHandleInstallPlugin_NotFound(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins/nonexistent-id/install", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleMarketplacePluginRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleMarketplacePlugins_MethodNotAllowed 验证 PUT /plugins 返回 405。
func TestHandleMarketplacePlugins_MethodNotAllowed(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/marketplace/plugins", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleMarketplacePlugins(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleMarketplacePluginRouting_MethodNotAllowed 验证 PUT /plugins/{id} 返回 405。
func TestHandleMarketplacePluginRouting_MethodNotAllowed(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreatePlugin(&store.Plugin{Name: "p1", Version: "1.0.0"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/marketplace/plugins/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleMarketplacePluginRouting(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreatePlugin_MissingVersion 验证缺 version 返回 400。
func TestHandleCreatePlugin_MissingVersion(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"p1","type":"data"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleMarketplacePlugins(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreatePlugin_InvalidJSON 验证非法 JSON body 返回 400。
func TestHandleCreatePlugin_InvalidJSON(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"p1","version":"1.0.0","type":"data"` // 缺右大括号
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleMarketplacePlugins(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleMarketplacePluginRouting_UnknownSubPath 验证未知子路径返回 404。
func TestHandleMarketplacePluginRouting_UnknownSubPath(t *testing.T) {
	s := newMarketplaceTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreatePlugin(&store.Plugin{Name: "p1", Version: "1.0.0"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins/"+created.ID+"/frobnicate", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleMarketplacePluginRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}
