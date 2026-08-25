// tenant_test.go 测试 Phase 6 租户管理 HTTP handler（tenant.go）。
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

// newTenantTestServer 构造租户 API 测试用 Server。
func newTenantTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-tenant-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// TestHandleListTenants_Empty 验证空列表返回 200 + tenants:[]。
func TestHandleListTenants_Empty(t *testing.T) {
	s := newTenantTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTenants(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Tenants []*store.Tenant `json:"tenants"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tenants) != 0 {
		t.Fatalf("tenants=%d, want 0", len(resp.Tenants))
	}
}

// TestHandleCreateTenant 验证正常创建返回 201。
func TestHandleCreateTenant(t *testing.T) {
	s := newTenantTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"name":"acme","displayName":"ACME Corp"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTenants(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var tenant store.Tenant
	if err := json.Unmarshal(w.Body.Bytes(), &tenant); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tenant.ID == "" {
		t.Fatal("ID is empty")
	}
	if tenant.Name != "acme" {
		t.Fatalf("Name=%q, want acme", tenant.Name)
	}
}

// TestHandleCreateTenant_MissingName 验证缺 name 返回 400。
func TestHandleCreateTenant_MissingName(t *testing.T) {
	s := newTenantTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"displayName":"ACME Corp"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTenants(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleGetTenant 验证正常获取租户详情。
func TestHandleGetTenant(t *testing.T) {
	s := newTenantTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateTenant(&store.Tenant{Name: "acme", DisplayName: "ACME"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTenantRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var tenant store.Tenant
	if err := json.Unmarshal(w.Body.Bytes(), &tenant); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tenant.ID != created.ID {
		t.Fatalf("ID=%q, want %q", tenant.ID, created.ID)
	}
}

// TestHandleGetTenant_NotFound 验证获取不存在租户返回 404。
func TestHandleGetTenant_NotFound(t *testing.T) {
	s := newTenantTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTenantRouting(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// TestHandleUpdateTenant 验证正常更新租户。
func TestHandleUpdateTenant(t *testing.T) {
	s := newTenantTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateTenant(&store.Tenant{Name: "acme"})
	body := `{"name":"acme-updated","displayName":"ACME Updated"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/"+created.ID, strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTenantRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var tenant store.Tenant
	if err := json.Unmarshal(w.Body.Bytes(), &tenant); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tenant.Name != "acme-updated" {
		t.Fatalf("Name=%q, want acme-updated", tenant.Name)
	}
}

// TestHandleDeleteTenant 验证正常删除租户。
func TestHandleDeleteTenant(t *testing.T) {
	s := newTenantTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateTenant(&store.Tenant{Name: "acme"})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTenantRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	if _, ok := s.store.GetTenant(created.ID); ok {
		t.Fatal("GetTenant returned ok after delete")
	}
}

// TestHandleSuspendTenant 验证暂停租户。
func TestHandleSuspendTenant(t *testing.T) {
	s := newTenantTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateTenant(&store.Tenant{Name: "acme", Status: store.TenantStatusActive})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/"+created.ID+"/suspend", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTenantRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var tenant store.Tenant
	if err := json.Unmarshal(w.Body.Bytes(), &tenant); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tenant.Status != store.TenantStatusSuspended {
		t.Fatalf("Status=%q, want suspended", tenant.Status)
	}
}

// TestHandleActivateTenant 验证激活租户。
func TestHandleActivateTenant(t *testing.T) {
	s := newTenantTestServer()
	auth := loginAsAdmin(t, s)
	created := s.store.CreateTenant(&store.Tenant{Name: "acme", Status: store.TenantStatusSuspended})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/"+created.ID+"/activate", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTenantRouting(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var tenant store.Tenant
	if err := json.Unmarshal(w.Body.Bytes(), &tenant); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tenant.Status != store.TenantStatusActive {
		t.Fatalf("Status=%q, want active", tenant.Status)
	}
}

// TestHandleTenants_NoAuth 验证无 Authorization 头返回 401。
func TestHandleTenants_NoAuth(t *testing.T) {
	s := newTenantTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	w := httptest.NewRecorder()
	s.handleTenants(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// TestHandleTenantRouting_EmptyID 验证空 id 返回 400。
func TestHandleTenantRouting_EmptyID(t *testing.T) {
	s := newTenantTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleTenantRouting(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}