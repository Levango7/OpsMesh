// middleware_deploy_test.go 测试中间件部署模板 API（middleware_deploy.go）。
//
// 覆盖范围：
//   - handleMiddlewareTemplates：列表（含 category/risk 过滤）、空 store 回退预置、创建
//   - handleMiddlewareTemplateDetail：详情、更新、删除、路由分派
//   - handleDeployMiddlewareTemplate：部署（含参数验证、shell 元字符拒绝、agent 不存在、租户不匹配）
//   - handleMiddlewareInstances：实例列表
//   - handleUninstallMiddlewareInstance：卸载实例
//   - handleMiddlewareInstanceRouting：实例路由分派
//   - 纯函数：middlewareTemplateByID / renderMiddlewareScript / validateMiddlewareParams /
//     middlewareTemplateToStore / middlewareTemplateFromStore / seedPresetMiddlewareTemplates /
//     listMiddlewareTemplatesFromStore / getMiddlewareTemplateByID
//
// 测试策略：白盒（package controlplane），直接装配 Server{store: MemoryStore, cfg: Demo=true}。
package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// newMWTestServer 构造中间件模板 API 测试用 Server。
func newMWTestServer() *Server {
	st := store.NewMemoryStore()
	return &Server{
		store:       st,
		cfg:         &config.Config{TaskMaxRetries: 3, Demo: true},
		requireAuth: false,
	}
}

// =============================================================================
// handleMiddlewareTemplates（GET /api/v1/middleware-templates）
// =============================================================================

func TestHandleListMiddlewareTemplates_Empty(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-templates", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var list []MiddlewareTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("空 store 应回退预置模板，但返回空列表")
	}
	ids := make(map[string]bool, len(list))
	for _, tpl := range list {
		ids[tpl.ID] = true
	}
	if !ids["mysql"] {
		t.Fatal("预置模板 mysql 缺失")
	}
}

func TestHandleListMiddlewareTemplates_CategoryFilter(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-templates?category=database", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var list []MiddlewareTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, tpl := range list {
		if tpl.Category != "database" {
			t.Fatalf("category=%q, want database", tpl.Category)
		}
	}
	if len(list) == 0 {
		t.Fatal("database 分类应有预置模板")
	}
}

func TestHandleListMiddlewareTemplates_RiskFilter(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-templates?risk=medium", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var list []MiddlewareTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, tpl := range list {
		if tpl.Risk != "medium" {
			t.Fatalf("risk=%q, want medium", tpl.Risk)
		}
	}
}

func TestHandleListMiddlewareTemplates_MethodNotAllowed(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/middleware-templates", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleMiddlewareTemplateDetail（GET /api/v1/middleware-templates/{id}）
// =============================================================================

func TestHandleMiddlewareTemplateByID_Found(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-templates/mysql", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var tpl MiddlewareTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &tpl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tpl.ID != "mysql" {
		t.Fatalf("ID=%q, want mysql", tpl.ID)
	}
	if tpl.Name == "" {
		t.Fatal("Name is empty")
	}
	if len(tpl.Scripts) == 0 {
		t.Fatal("Scripts is empty")
	}
}

func TestHandleMiddlewareTemplateByID_NotFound(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-templates/non-existent-id", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleMiddlewareTemplateByID_MethodNotAllowed(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/middleware-templates/mysql", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleCreateMiddlewareTemplate（POST /api/v1/middleware-templates）
// =============================================================================

func TestHandleCreateMiddlewareTemplate_Happy(t *testing.T) {
	s := newMWTestServer()
	body := `{"name":"custom-mw","category":"database","scripts":{"docker":{"deploy":"echo hi"}},"risk":"low"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var tpl MiddlewareTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &tpl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tpl.ID == "" {
		t.Fatal("ID is empty, want store-assigned")
	}
	if tpl.Name != "custom-mw" {
		t.Fatalf("Name=%q, want custom-mw", tpl.Name)
	}
}

func TestHandleCreateMiddlewareTemplate_MissingName(t *testing.T) {
	s := newMWTestServer()
	body := `{"scripts":{"docker":{"deploy":"echo hi"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateMiddlewareTemplate_MissingScripts(t *testing.T) {
	s := newMWTestServer()
	body := `{"name":"x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateMiddlewareTemplate_InvalidJSON(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateMiddlewareTemplate_RiskNormalized(t *testing.T) {
	s := newMWTestServer()
	body := `{"name":"x","scripts":{"docker":{"deploy":"echo hi"}},"risk":"bogus"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var tpl MiddlewareTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &tpl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tpl.Risk != "low" {
		t.Fatalf("Risk=%q, want low (normalized)", tpl.Risk)
	}
}

// =============================================================================
// handleUpdateMiddlewareTemplate（PUT /api/v1/middleware-templates/{id}）
// =============================================================================

func TestHandleUpdateMiddlewareTemplate_Happy(t *testing.T) {
	s := newMWTestServer()
	s.store.SaveMiddlewareTemplate(&store.MiddlewareTemplate{ID: "upd-1", TenantID: "default", Name: "old", Config: `{"id":"upd-1","name":"old","scripts":{"docker":{"deploy":"old"}}}`})

	body := `{"name":"new-name","scripts":{"docker":{"deploy":"new-cmd"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/middleware-templates/upd-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var tpl MiddlewareTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &tpl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tpl.Name != "new-name" {
		t.Fatalf("Name=%q, want new-name", tpl.Name)
	}
}

func TestHandleUpdateMiddlewareTemplate_NotFound(t *testing.T) {
	s := newMWTestServer()
	body := `{"name":"x","scripts":{"docker":{"deploy":"y"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/middleware-templates/no-such-id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpdateMiddlewareTemplate_PresetUpsert(t *testing.T) {
	s := newMWTestServer()
	body := `{"name":"updated","scripts":{"docker":{"deploy":"new-cmd"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/middleware-templates/mysql", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpdateMiddlewareTemplate_MissingName(t *testing.T) {
	s := newMWTestServer()
	body := `{"scripts":{"docker":{"deploy":"x"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/middleware-templates/mysql", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// handleDeleteMiddlewareTemplate（DELETE /api/v1/middleware-templates/{id}）
// =============================================================================

func TestHandleDeleteMiddlewareTemplate_Happy(t *testing.T) {
	s := newMWTestServer()
	s.store.SaveMiddlewareTemplate(&store.MiddlewareTemplate{ID: "del-1", TenantID: "default", Name: "x", Config: `{"id":"del-1","name":"x","scripts":{"docker":{"deploy":"y"}}}`})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/middleware-templates/del-1", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if s.store.GetMiddlewareTemplate("del-1") != nil {
		t.Fatal("template still exists after delete")
	}
}

func TestHandleDeleteMiddlewareTemplate_NotFound(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/middleware-templates/no-such", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeleteMiddlewareTemplate_PresetUnseeded(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/middleware-templates/mysql", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// handleDeployMiddlewareTemplate（POST /api/v1/middleware-templates/{id}/deploy）
// =============================================================================

func TestHandleDeployMiddlewareTemplate_Happy(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})

	body := `{"agentID":"` + a.AgentID + `","deployType":"docker","params":{"name":"mydb","port":"3306","password":"secret123","datadir":"/data/mysql"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["templateID"] != "mysql" {
		t.Fatalf("templateID=%v, want mysql", resp["templateID"])
	}
	if resp["task"] == nil {
		t.Fatal("task is nil")
	}
}

func TestHandleDeployMiddlewareTemplate_NotFound(t *testing.T) {
	s := newMWTestServer()
	body := `{"agentID":"a1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/no-such/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeployMiddlewareTemplate_MissingAgentID(t *testing.T) {
	s := newMWTestServer()
	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeployMiddlewareTemplate_UnsupportedDeployType(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"agentID":"` + a.AgentID + `","deployType":"bogus"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeployMiddlewareTemplate_DefaultParamFilled(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	// mysql: name/port/datadir 有默认值，仅 password 必填且无默认值。
	body := `{"agentID":"` + a.AgentID + `","params":{"password":"pw123"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201 (defaults filled); body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeployMiddlewareTemplate_RequiredParamMissing(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	// mysql: password 必填且无默认值，缺失应 400。
	body := `{"agentID":"` + a.AgentID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 (required param missing); body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeployMiddlewareTemplate_ShellUnsafeParam(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"agentID":"` + a.AgentID + `","params":{"name":"db;reboot","port":"3306","password":"pw","datadir":"/data/mysql"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 (shell metachar); body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeployMiddlewareTemplate_InvalidPort(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"agentID":"` + a.AgentID + `","params":{"port":"99999","password":"pw"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 (invalid port); body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeployMiddlewareTemplate_AgentNotFound(t *testing.T) {
	s := newMWTestServer()
	body := `{"agentID":"ghost-agent","params":{"password":"pw"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeployMiddlewareTemplate_TenantMismatch(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	body := `{"agentID":"` + a.AgentID + `","params":{"password":"pw"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "t2")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeployMiddlewareTemplate_InvalidJSON(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(`{bad`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeployMiddlewareTemplate_MethodNotAllowed(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-templates/mysql/deploy", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleMiddlewareInstances（GET /api/v1/middleware-instances）
// =============================================================================

func TestHandleMiddlewareInstances_Empty(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-instances", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareInstances(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var list []interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("want empty list, got %d items", len(list))
	}
}

func TestHandleMiddlewareInstances_MethodNotAllowed(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-instances", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareInstances(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleUninstallMiddlewareInstance（POST /api/v1/middleware-instances/{id}/uninstall）
// =============================================================================

func TestHandleUninstallMiddlewareInstance_Happy(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"agentID":"` + a.AgentID + `","templateID":"mysql","deployType":"docker","params":{"password":"pw"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-instances/inst-1/uninstall", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareInstanceRouting(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["instanceID"] != "inst-1" {
		t.Fatalf("instanceID=%v, want inst-1", resp["instanceID"])
	}
}

func TestHandleUninstallMiddlewareInstance_MissingAgentID(t *testing.T) {
	s := newMWTestServer()
	body := `{"templateID":"mysql"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-instances/inst-1/uninstall", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareInstanceRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleUninstallMiddlewareInstance_MissingTemplateID(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"agentID":"` + a.AgentID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-instances/inst-1/uninstall", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareInstanceRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleUninstallMiddlewareInstance_TemplateNotFound(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"agentID":"` + a.AgentID + `","templateID":"no-such"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-instances/inst-1/uninstall", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareInstanceRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleUninstallMiddlewareInstance_MethodNotAllowed(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-instances/inst-1/uninstall", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareInstanceRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleMiddlewareTemplateDetail / handleMiddlewareInstanceRouting 路由分派
// =============================================================================

func TestHandleMiddlewareTemplateDetail_EmptyID(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-templates/", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 (list fallback); body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleMiddlewareTemplateDetail_UnknownSubPath(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-templates/mysql/unknown", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestHandleMiddlewareInstanceRouting_EmptyID(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-instances/", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareInstanceRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 (list fallback); body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleMiddlewareInstanceRouting_UnknownSubPath(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-instances/inst-1/unknown", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareInstanceRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

// =============================================================================
// 纯函数测试
// =============================================================================

func TestMiddlewareTemplateByID_Found(t *testing.T) {
	tpl := middlewareTemplateByID("mysql")
	if tpl == nil {
		t.Fatal("mysql not found")
	}
	if tpl.Name == "" {
		t.Fatal("Name is empty")
	}
}

func TestMiddlewareTemplateByID_NotFound(t *testing.T) {
	if tpl := middlewareTemplateByID("no-such"); tpl != nil {
		t.Fatalf("want nil, got %+v", tpl)
	}
}

func TestRenderMiddlewareScript(t *testing.T) {
	got := renderMiddlewareScript("docker run --name {name} -p {port}", map[string]string{"name": "db", "port": "3306"})
	if got != "docker run --name db -p 3306" {
		t.Fatalf("got=%q, want 'docker run --name db -p 3306'", got)
	}
	// 未提供 key 保留原占位符。
	got = renderMiddlewareScript("hello {name}", map[string]string{})
	if got != "hello {name}" {
		t.Fatalf("got=%q, want 'hello {name}'", got)
	}
}

func TestValidateMiddlewareParams(t *testing.T) {
	// int 类型合法。
	params := []MiddlewareParam{{Name: "port", Type: "int"}}
	if err := validateMiddlewareParams(params, map[string]string{"port": "3306"}); err != nil {
		t.Fatalf("port=3306 should pass: %v", err)
	}
	// int 类型非法。
	if err := validateMiddlewareParams(params, map[string]string{"port": "abc"}); err == nil {
		t.Fatal("port=abc should fail")
	}
	// port 越界。
	if err := validateMiddlewareParams(params, map[string]string{"port": "70000"}); err == nil {
		t.Fatal("port=70000 should fail")
	}
	// string 类型路径校验。
	params = []MiddlewareParam{{Name: "datadir", Type: "string"}}
	if err := validateMiddlewareParams(params, map[string]string{"datadir": "/data/db"}); err != nil {
		t.Fatalf("datadir=/data/db should pass: %v", err)
	}
	if err := validateMiddlewareParams(params, map[string]string{"datadir": "relative"}); err == nil {
		t.Fatal("datadir=relative should fail")
	}
	// 缺失值跳过。
	if err := validateMiddlewareParams(params, map[string]string{}); err != nil {
		t.Fatalf("missing value should skip: %v", err)
	}
}

func TestMiddlewareTemplateToStore_RoundTrip(t *testing.T) {
	tpl := &MiddlewareTemplate{ID: "x", Name: "n", Category: "database", Version: "8.0", Risk: "low"}
	st := middlewareTemplateToStore(tpl, "default")
	if st == nil {
		t.Fatal("middlewareTemplateToStore returned nil")
	}
	if st.ID != "x" || st.Name != "n" || st.Type != "database" {
		t.Fatalf("store fields wrong: %+v", st)
	}
	got := middlewareTemplateFromStore(st)
	if got == nil || got.ID != "x" || got.Name != "n" || got.Category != "database" {
		t.Fatalf("round-trip failed: %+v", got)
	}
}

func TestMiddlewareTemplateToStore_Nil(t *testing.T) {
	if got := middlewareTemplateToStore(nil, "default"); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestMiddlewareTemplateFromStore_Nil(t *testing.T) {
	if got := middlewareTemplateFromStore(nil); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestMiddlewareTemplateFromStore_EmptyConfig(t *testing.T) {
	st := &store.MiddlewareTemplate{ID: "x", Name: "n", Type: "database", Version: "8.0"}
	got := middlewareTemplateFromStore(st)
	if got == nil || got.ID != "x" || got.Name != "n" || got.Category != "database" {
		t.Fatalf("minimal fallback failed: %+v", got)
	}
}

func TestMiddlewareTemplateFromStore_BadConfig(t *testing.T) {
	st := &store.MiddlewareTemplate{ID: "x", Name: "n", Type: "database", Version: "8.0", Config: "{bad json"}
	got := middlewareTemplateFromStore(st)
	if got == nil || got.ID != "x" || got.Name != "n" {
		t.Fatalf("bad config fallback failed: %+v", got)
	}
}

func TestSeedPresetMiddlewareTemplates(t *testing.T) {
	s := newMWTestServer()
	s.seedPresetMiddlewareTemplates()
	if got := s.store.GetMiddlewareTemplate("mysql"); got == nil {
		t.Fatal("mysql not seeded")
	}
	// 再次 seed 不覆盖（幂等）。
	s.seedPresetMiddlewareTemplates()
	if got := s.store.GetMiddlewareTemplate("mysql"); got == nil {
		t.Fatal("mysql disappeared after re-seed")
	}
}

func TestListMiddlewareTemplatesFromStore_TenantMerge(t *testing.T) {
	s := newMWTestServer()
	s.store.SaveMiddlewareTemplate(&store.MiddlewareTemplate{ID: "preset-1", TenantID: "default", Name: "p", Config: `{"id":"preset-1","name":"p","scripts":{"docker":{"deploy":"x"}}}`})
	s.store.SaveMiddlewareTemplate(&store.MiddlewareTemplate{ID: "custom-1", TenantID: "t1", Name: "c", Config: `{"id":"custom-1","name":"c","scripts":{"docker":{"deploy":"y"}}}`})

	got := s.listMiddlewareTemplatesFromStore("t1")
	ids := make(map[string]bool, len(got))
	for _, tpl := range got {
		ids[tpl.ID] = true
	}
	if !ids["preset-1"] {
		t.Fatal("preset-1 missing from t1 view")
	}
	if !ids["custom-1"] {
		t.Fatal("custom-1 missing from t1 view")
	}
}

func TestGetMiddlewareTemplateByID_Fallback(t *testing.T) {
	s := newMWTestServer()
	got := s.getMiddlewareTemplateByID("mysql")
	if got == nil || got.ID != "mysql" {
		t.Fatalf("fallback to preset failed: %+v", got)
	}
}

func TestGetMiddlewareTemplateByID_FromStore(t *testing.T) {
	s := newMWTestServer()
	s.store.SaveMiddlewareTemplate(&store.MiddlewareTemplate{ID: "x", TenantID: "default", Name: "stored", Config: `{"id":"x","name":"stored","scripts":{"docker":{"deploy":"y"}}}`})
	got := s.getMiddlewareTemplateByID("x")
	if got == nil || got.Name != "stored" {
		t.Fatalf("from store failed: %+v", got)
	}
}

// =============================================================================
// 认证与审计
// =============================================================================

func TestHandleListMiddlewareTemplates_NoAuth(t *testing.T) {
	s := newMWTestServer()
	s.requireAuth = true
	s.cfg.Demo = false
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-templates", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

func TestHandleListMiddlewareTemplates_BadTenant(t *testing.T) {
	s := newMWTestServer()
	s.cfg.Demo = false
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-templates", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateMiddlewareTemplate_AuditLogged(t *testing.T) {
	s := newMWTestServer()
	body := `{"name":"audit-test","scripts":{"docker":{"deploy":"echo hi"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	audits := s.store.Audits()
	found := false
	for _, a := range audits {
		if a.Action == "mw_template_create" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("audit event mw_template_create not logged")
	}
}

func TestHandleDeleteMiddlewareTemplate_AuditLogged(t *testing.T) {
	s := newMWTestServer()
	s.store.SaveMiddlewareTemplate(&store.MiddlewareTemplate{ID: "aud-del", TenantID: "default", Name: "x", Config: `{"id":"aud-del","name":"x","scripts":{"docker":{"deploy":"y"}}}`})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/middleware-templates/aud-del", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	audits := s.store.Audits()
	found := false
	for _, a := range audits {
		if a.Action == "mw_template_delete" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("audit event mw_template_delete not logged")
	}
}

func TestHandleUpdateMiddlewareTemplate_AuditLogged(t *testing.T) {
	s := newMWTestServer()
	s.store.SaveMiddlewareTemplate(&store.MiddlewareTemplate{ID: "aud-upd", TenantID: "default", Name: "old", Config: `{"id":"aud-upd","name":"old","scripts":{"docker":{"deploy":"x"}}}`})

	body := `{"name":"new","scripts":{"docker":{"deploy":"y"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/middleware-templates/aud-upd", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	audits := s.store.Audits()
	found := false
	for _, a := range audits {
		if a.Action == "mw_template_update" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("audit event mw_template_update not logged")
	}
}

func TestHandleDeployMiddlewareTemplate_AuditLogged(t *testing.T) {
	s := newMWTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"agentID":"` + a.AgentID + `","params":{"password":"pw"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates/mysql/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplateDetail(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	audits := s.store.Audits()
	found := false
	for _, a := range audits {
		if a.Action == "deploy_middleware" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("audit event deploy_middleware not logged")
	}
}

func TestHandleListMiddlewareTemplates_WithSeed(t *testing.T) {
	s := newMWTestServer()
	s.seedPresetMiddlewareTemplates()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/middleware-templates", nil)
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var list []MiddlewareTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("seeded store should return non-empty list")
	}
}

func TestHandleListMiddlewareTemplates_BadJSONBody(t *testing.T) {
	s := newMWTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/middleware-templates", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiddlewareTemplates(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}