// os_optimize_test.go 测试 OS 优化模板 API（os_optimize.go）。
//
// 覆盖范围：
//   - handleListOSTemplatesGet：列表（含 category/risk/os 过滤）、空列表回退、store 为空回退预置
//   - handleOSTemplateByID：详情、404、method not allowed
//   - handleCreateOSTemplate：正常创建、缺 name/commands、无效 JSON
//   - handleUpdateOSTemplate：更新存在/不存在、预置模板 upsert
//   - handleDeleteOSTemplate：删除存在/不存在/预置未 seed
//   - handleExecuteOSTemplate：执行模板（含参数验证、shell 元字符拒绝、agent 不存在）
//   - handleOSTemplateRouting：路由分派（list 兜底、空 id、未知子路径）
//   - 纯函数：osTemplateByID / buildOSExecuteCommand / renderOSScript / validateOSParams /
//     validatePort / validateNonEmpty / validatePath / validateShellSafeValues / normalizeRisk /
//     osTemplateToStore / osTemplateFromStore / seedPresetOSTemplates / listOSTemplatesFromStore /
//     getOSTemplateByID
//
// 测试策略：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore, cfg: Demo=true}；
//   - Demo=true 放宽认证（未携带 X-Tenant-ID 头时自动填充 default 租户），便于精确断言；
//   - 用 httptest.NewRequest + httptest.NewRecorder 直接调用 handler，断言 status code 与响应体。
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

// newOSTestServer 构造 OS 模板 API 测试用 Server：
//   - memory store；
//   - Demo=true 放宽认证（未携带 X-Tenant-ID 头时自动填充 default 租户）；
//   - requireAuth=false。
func newOSTestServer() *Server {
	st := store.NewMemoryStore()
	return &Server{
		store:       st,
		cfg:         &config.Config{TaskMaxRetries: 3, Demo: true},
		requireAuth: false,
	}
}

// =============================================================================
// handleListOSTemplatesGet（GET /api/v1/os-templates）
// =============================================================================

// TestHandleListOSTemplates_Empty 验证空 store 回退到内存常量预置模板，返回非空列表。
func TestHandleListOSTemplates_Empty(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/os-templates", nil)
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var list []OSTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("空 store 应回退预置模板，但返回空列表")
	}
	// 验证含已知预置 ID。
	ids := make(map[string]bool, len(list))
	for _, tpl := range list {
		ids[tpl.ID] = true
	}
	if !ids["kernel-tune"] {
		t.Fatal("预置模板 kernel-tune 缺失")
	}
}

// TestHandleListOSTemplates_CategoryFilter 验证 category 过滤生效。
func TestHandleListOSTemplates_CategoryFilter(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/os-templates?category=kernel", nil)
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var list []OSTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, tpl := range list {
		if tpl.Category != "kernel" {
			t.Fatalf("category=%q, want kernel", tpl.Category)
		}
	}
	if len(list) == 0 {
		t.Fatal("kernel 分类应有预置模板")
	}
}

// TestHandleListOSTemplates_RiskFilter 验证 risk 过滤生效。
func TestHandleListOSTemplates_RiskFilter(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/os-templates?risk=high", nil)
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var list []OSTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, tpl := range list {
		if tpl.Risk != "high" {
			t.Fatalf("risk=%q, want high", tpl.Risk)
		}
	}
}

// TestHandleListOSTemplates_OSFilter 验证 os 过滤生效（centos 仅返回 centos 专属 + all）。
func TestHandleListOSTemplates_OSFilter(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/os-templates?os=centos", nil)
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var list []OSTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, tpl := range list {
		if tpl.OS != "centos" && tpl.OS != "all" {
			t.Fatalf("os=%q, want centos or all", tpl.OS)
		}
	}
}

// TestHandleListOSTemplates_MethodNotAllowed 验证非 GET/POST 方法返回 405。
func TestHandleListOSTemplates_MethodNotAllowed(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/os-templates", nil)
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleOSTemplateByID（GET /api/v1/os-templates/{id}）
// =============================================================================

// TestHandleOSTemplateByID_Found 验证存在的预置模板返回详情。
func TestHandleOSTemplateByID_Found(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/os-templates/kernel-tune", nil)
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var tpl OSTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &tpl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tpl.ID != "kernel-tune" {
		t.Fatalf("ID=%q, want kernel-tune", tpl.ID)
	}
	if tpl.Name == "" {
		t.Fatal("Name is empty")
	}
	if tpl.Commands == "" {
		t.Fatal("Commands is empty")
	}
}

// TestHandleOSTemplateByID_NotFound 验证不存在的模板返回 404。
func TestHandleOSTemplateByID_NotFound(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/os-templates/non-existent-id", nil)
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleOSTemplateByID_MethodNotAllowed 验证非 GET 方法返回 405。
func TestHandleOSTemplateByID_MethodNotAllowed(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/os-templates/kernel-tune", nil)
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleCreateOSTemplate（POST /api/v1/os-templates）
// =============================================================================

// TestHandleCreateOSTemplate_Happy 验证正常创建返回 201 + 含 ID。
func TestHandleCreateOSTemplate_Happy(t *testing.T) {
	s := newOSTestServer()
	body := `{"name":"custom-tune","category":"kernel","commands":"echo hi","risk":"low"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var tpl OSTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &tpl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tpl.ID == "" {
		t.Fatal("ID is empty, want store-assigned")
	}
	if tpl.Name != "custom-tune" {
		t.Fatalf("Name=%q, want custom-tune", tpl.Name)
	}
}

// TestHandleCreateOSTemplate_MissingName 验证缺 name 返回 400。
func TestHandleCreateOSTemplate_MissingName(t *testing.T) {
	s := newOSTestServer()
	body := `{"commands":"echo hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleCreateOSTemplate_MissingCommands 验证缺 commands 返回 400。
func TestHandleCreateOSTemplate_MissingCommands(t *testing.T) {
	s := newOSTestServer()
	body := `{"name":"x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleCreateOSTemplate_InvalidJSON 验证无效 JSON 返回 400。
func TestHandleCreateOSTemplate_InvalidJSON(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleCreateOSTemplate_RiskNormalized 验证非法 risk 归一为 low。
func TestHandleCreateOSTemplate_RiskNormalized(t *testing.T) {
	s := newOSTestServer()
	body := `{"name":"x","commands":"echo hi","risk":"bogus"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var tpl OSTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &tpl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tpl.Risk != "low" {
		t.Fatalf("Risk=%q, want low (normalized)", tpl.Risk)
	}
}

// =============================================================================
// handleUpdateOSTemplate（PUT /api/v1/os-templates/{id}）
// =============================================================================

// TestHandleUpdateOSTemplate_Happy 验证更新已存在模板成功。
func TestHandleUpdateOSTemplate_Happy(t *testing.T) {
	s := newOSTestServer()
	// 先创建一个模板。
	s.store.SaveOSTemplate(&store.OSTemplate{ID: "upd-1", TenantID: "default", Name: "old", Config: `{"id":"upd-1","name":"old","commands":"old"}`})

	body := `{"name":"new-name","commands":"new-cmd"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/os-templates/upd-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var tpl OSTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &tpl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tpl.Name != "new-name" {
		t.Fatalf("Name=%q, want new-name", tpl.Name)
	}
}

// TestHandleUpdateOSTemplate_NotFound 验证更新不存在的模板返回 404。
func TestHandleUpdateOSTemplate_NotFound(t *testing.T) {
	s := newOSTestServer()
	body := `{"name":"x","commands":"y"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/os-templates/no-such-id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleUpdateOSTemplate_PresetUpsert 验证更新预置模板 ID（未 seed）允许 upsert。
func TestHandleUpdateOSTemplate_PresetUpsert(t *testing.T) {
	s := newOSTestServer()
	body := `{"name":"updated","commands":"new-cmd"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/os-templates/kernel-tune", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleUpdateOSTemplate_MissingName 验证缺 name 返回 400。
func TestHandleUpdateOSTemplate_MissingName(t *testing.T) {
	s := newOSTestServer()
	body := `{"commands":"x"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/os-templates/kernel-tune", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// handleDeleteOSTemplate（DELETE /api/v1/os-templates/{id}）
// =============================================================================

// TestHandleDeleteOSTemplate_Happy 验证删除已存在模板返回 204。
func TestHandleDeleteOSTemplate_Happy(t *testing.T) {
	s := newOSTestServer()
	s.store.SaveOSTemplate(&store.OSTemplate{ID: "del-1", TenantID: "default", Name: "x", Config: `{"id":"del-1","name":"x","commands":"y"}`})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/os-templates/del-1", nil)
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if s.store.GetOSTemplate("del-1") != nil {
		t.Fatal("template still exists after delete")
	}
}

// TestHandleDeleteOSTemplate_NotFound 验证删除不存在的模板返回 404。
func TestHandleDeleteOSTemplate_NotFound(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/os-templates/no-such", nil)
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleDeleteOSTemplate_PresetUnseeded 验证删除预置模板（未 seed）返回 204。
func TestHandleDeleteOSTemplate_PresetUnseeded(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/os-templates/kernel-tune", nil)
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// handleExecuteOSTemplate（POST /api/v1/os-templates/{id}/execute）
// =============================================================================

// TestHandleExecuteOSTemplate_Happy 验证执行预置模板（无 Params，旧模式）成功创建任务。
func TestHandleExecuteOSTemplate_Happy(t *testing.T) {
	s := newOSTestServer()
	// 注册一个 agent 供执行。
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})

	body := `{"agentID":"` + a.AgentID + `","params":["myhost"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates/hostname-set/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["templateID"] != "hostname-set" {
		t.Fatalf("templateID=%v, want hostname-set", resp["templateID"])
	}
	if resp["task"] == nil {
		t.Fatal("task is nil")
	}
}

// TestHandleExecuteOSTemplate_NotFound 验证执行不存在的模板返回 404。
func TestHandleExecuteOSTemplate_NotFound(t *testing.T) {
	s := newOSTestServer()
	body := `{"agentID":"a1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates/no-such/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleExecuteOSTemplate_MissingAgentID 验证缺 agentID 返回 400。
func TestHandleExecuteOSTemplate_MissingAgentID(t *testing.T) {
	s := newOSTestServer()
	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates/kernel-tune/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleExecuteOSTemplate_AgentNotFound 验证 agent 不存在返回 403。
func TestHandleExecuteOSTemplate_AgentNotFound(t *testing.T) {
	s := newOSTestServer()
	body := `{"agentID":"ghost-agent"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates/kernel-tune/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleExecuteOSTemplate_DefaultParamFilled 验证新模式必填参数缺失时用默认值填充。
func TestHandleExecuteOSTemplate_DefaultParamFilled(t *testing.T) {
	s := newOSTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	// disk-io-tune：device 必填且有 Default="sda"。
	body := `{"agentID":"` + a.AgentID + `","paramsMap":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates/disk-io-tune/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	// 应成功（用默认值）。
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201 (default filled); body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleExecuteOSTemplate_ShellUnsafeParam 验证参数含 shell 元字符被拒绝。
func TestHandleExecuteOSTemplate_ShellUnsafeParam(t *testing.T) {
	s := newOSTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	// dns-config 的 dns1 参数为 string 类型；注入分号。
	body := `{"agentID":"` + a.AgentID + `","paramsMap":{"dns1":"8.8.8.8;reboot","dns2":"114.114.114.114"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates/dns-config/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 (shell metachar); body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleExecuteOSTemplate_MethodNotAllowed 验证非 POST 方法返回 405。
func TestHandleExecuteOSTemplate_MethodNotAllowed(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/os-templates/kernel-tune/execute", nil)
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// handleOSTemplateRouting 路由分派
// =============================================================================

// TestHandleOSTemplateRouting_EmptyID 验证 /api/v1/os-templates/（空 id）转给 list handler。
func TestHandleOSTemplateRouting_EmptyID(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/os-templates/", nil)
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	// 兜底转给 list handler，应返回 200 + 列表。
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 (list fallback); body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleOSTemplateRouting_UnknownSubPath 验证未知子路径返回 404。
func TestHandleOSTemplateRouting_UnknownSubPath(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/os-templates/kernel-tune/unknown", nil)
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

// =============================================================================
// 纯函数测试
// =============================================================================

// TestOSTemplateByID_Found 验证 osTemplateByID 找到预置模板。
func TestOSTemplateByID_Found(t *testing.T) {
	tpl := osTemplateByID("kernel-tune")
	if tpl == nil {
		t.Fatal("kernel-tune not found")
	}
	if tpl.Name == "" {
		t.Fatal("Name is empty")
	}
}

// TestOSTemplateByID_NotFound 验证 osTemplateByID 未找到返回 nil。
func TestOSTemplateByID_NotFound(t *testing.T) {
	if tpl := osTemplateByID("no-such"); tpl != nil {
		t.Fatalf("want nil, got %+v", tpl)
	}
}

// TestBuildOSExecuteCommand 验证 buildOSExecuteCommand 拼接位置参数。
func TestBuildOSExecuteCommand(t *testing.T) {
	// 无参数：直接返回原脚本。
	got := buildOSExecuteCommand("echo hi", nil)
	if got != "echo hi" {
		t.Fatalf("got=%q, want 'echo hi'", got)
	}
	// 有参数：注入 set --。
	got = buildOSExecuteCommand("echo hi", []string{"a", "b"})
	if !strings.HasPrefix(got, "set -- 'a' 'b'\n") {
		t.Fatalf("got=%q, want prefix \"set -- 'a' 'b'\\n\"", got)
	}
	if !strings.HasSuffix(got, "echo hi") {
		t.Fatalf("got=%q, want suffix 'echo hi'", got)
	}
	// 单引号转义。
	got = buildOSExecuteCommand("echo", []string{"it's"})
	if !strings.Contains(got, `'it'\''s'`) {
		t.Fatalf("got=%q, want escaped single quote", got)
	}
}

// TestRenderOSScript 验证 renderOSScript 占位符替换。
func TestRenderOSScript(t *testing.T) {
	got := renderOSScript("hello {name} {port}", map[string]string{"name": "x", "port": "8080"})
	if got != "hello x 8080" {
		t.Fatalf("got=%q, want 'hello x 8080'", got)
	}
	// 未提供 key 保留原占位符。
	got = renderOSScript("hello {name}", map[string]string{})
	if got != "hello {name}" {
		t.Fatalf("got=%q, want 'hello {name}'", got)
	}
}

// TestValidateOSParams 验证 validateOSParams 类型与语义校验。
func TestValidateOSParams(t *testing.T) {
	// int 类型合法。
	if err := validateOSParams([]OSParam{{Name: "port", Type: "int"}}, map[string]string{"port": "8080"}); err != nil {
		t.Fatalf("port=8080 should pass: %v", err)
	}
	// int 类型非法。
	if err := validateOSParams([]OSParam{{Name: "port", Type: "int"}}, map[string]string{"port": "abc"}); err == nil {
		t.Fatal("port=abc should fail")
	}
	// port 越界。
	if err := validateOSParams([]OSParam{{Name: "port", Type: "int"}}, map[string]string{"port": "70000"}); err == nil {
		t.Fatal("port=70000 should fail")
	}
	// string 类型非空校验。
	if err := validateOSParams([]OSParam{{Name: "name", Type: "string"}}, map[string]string{"name": "   "}); err == nil {
		t.Fatal("name='   ' should fail (empty after trim)")
	}
	// 缺失值跳过。
	if err := validateOSParams([]OSParam{{Name: "port", Type: "int"}}, map[string]string{}); err != nil {
		t.Fatalf("missing value should skip: %v", err)
	}
}

// TestValidatePort 验证 validatePort 端口范围。
func TestValidatePort(t *testing.T) {
	cases := []struct {
		port int
		ok   bool
	}{
		{1, true}, {65535, true}, {8080, true},
		{0, false}, {-1, false}, {65536, false},
	}
	for _, c := range cases {
		err := validatePort(c.port)
		if (err == nil) != c.ok {
			t.Fatalf("port=%d ok=%v, err=%v", c.port, c.ok, err)
		}
	}
}

// TestValidateNonEmpty 验证 validateNonEmpty。
func TestValidateNonEmpty(t *testing.T) {
	if err := validateNonEmpty("name", "x"); err != nil {
		t.Fatalf("non-empty should pass: %v", err)
	}
	if err := validateNonEmpty("name", "  "); err == nil {
		t.Fatal("whitespace-only should fail")
	}
	if err := validateNonEmpty("name", ""); err == nil {
		t.Fatal("empty should fail")
	}
}

// TestValidatePath 验证 validatePath。
func TestValidatePath(t *testing.T) {
	if err := validatePath("/etc/foo"); err != nil {
		t.Fatalf("absolute path should pass: %v", err)
	}
	if err := validatePath("relative"); err == nil {
		t.Fatal("relative path should fail")
	}
}

// TestValidateShellSafeValues 验证 validateShellSafeValues。
func TestValidateShellSafeValues(t *testing.T) {
	// 安全值。
	if err := validateShellSafeValues(map[string]string{"a": "value", "b": "123"}); err != nil {
		t.Fatalf("safe values should pass: %v", err)
	}
	// 含分号。
	if err := validateShellSafeValues(map[string]string{"a": "v;reboot"}); err == nil {
		t.Fatal("semicolon should be rejected")
	}
	// 含空格。
	if err := validateShellSafeValues(map[string]string{"a": "v w"}); err == nil {
		t.Fatal("space should be rejected")
	}
	// 含反引号。
	if err := validateShellSafeValues(map[string]string{"a": "v`x`"}); err == nil {
		t.Fatal("backtick should be rejected")
	}
}

// TestNormalizeRisk 验证 normalizeRisk。
func TestNormalizeRisk(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"low", "low"}, {"medium", "medium"}, {"high", "high"},
		{"", "low"}, {"bogus", "low"}, {"critical", "low"},
	}
	for _, c := range cases {
		if got := normalizeRisk(c.in); got != c.want {
			t.Fatalf("normalizeRisk(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

// TestOSTemplateToStore_RoundTrip 验证 osTemplateToStore / osTemplateFromStore 往返。
func TestOSTemplateToStore_RoundTrip(t *testing.T) {
	tpl := &OSTemplate{ID: "x", Name: "n", Category: "kernel", Commands: "echo hi", Risk: "low", OS: "all"}
	st := osTemplateToStore(tpl, "default")
	if st == nil {
		t.Fatal("osTemplateToStore returned nil")
	}
	if st.ID != "x" || st.Name != "n" || st.OS != "all" {
		t.Fatalf("store fields wrong: %+v", st)
	}
	// 往返。
	got := osTemplateFromStore(st)
	if got == nil || got.ID != "x" || got.Name != "n" || got.Commands != "echo hi" {
		t.Fatalf("round-trip failed: %+v", got)
	}
}

// TestOSTemplateToStore_Nil 验证 nil 输入返回 nil。
func TestOSTemplateToStore_Nil(t *testing.T) {
	if got := osTemplateToStore(nil, "default"); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

// TestOSTemplateFromStore_Nil 验证 nil 输入返回 nil。
func TestOSTemplateFromStore_Nil(t *testing.T) {
	if got := osTemplateFromStore(nil); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

// TestOSTemplateFromStore_EmptyConfig 验证空 Config 回退最小模板。
func TestOSTemplateFromStore_EmptyConfig(t *testing.T) {
	st := &store.OSTemplate{ID: "x", Name: "n", OS: "all"}
	got := osTemplateFromStore(st)
	if got == nil || got.ID != "x" || got.Name != "n" || got.OS != "all" {
		t.Fatalf("minimal fallback failed: %+v", got)
	}
}

// TestOSTemplateFromStore_BadConfig 验证坏 Config 回退最小模板。
func TestOSTemplateFromStore_BadConfig(t *testing.T) {
	st := &store.OSTemplate{ID: "x", Name: "n", OS: "all", Config: "{bad json"}
	got := osTemplateFromStore(st)
	if got == nil || got.ID != "x" || got.Name != "n" {
		t.Fatalf("bad config fallback failed: %+v", got)
	}
}

// TestSeedPresetOSTemplates 验证 seedPresetOSTemplates 幂等写入预置模板。
func TestSeedPresetOSTemplates(t *testing.T) {
	s := newOSTestServer()
	s.seedPresetOSTemplates()
	// 应能从 store 读到预置模板。
	if got := s.store.GetOSTemplate("kernel-tune"); got == nil {
		t.Fatal("kernel-tune not seeded")
	}
	// 再次 seed 不覆盖（幂等）。
	s.seedPresetOSTemplates()
	if got := s.store.GetOSTemplate("kernel-tune"); got == nil {
		t.Fatal("kernel-tune disappeared after re-seed")
	}
}

// TestListOSTemplatesFromStore_TenantMerge 验证 listOSTemplatesFromStore 合并当前租户与 default 租户模板。
func TestListOSTemplatesFromStore_TenantMerge(t *testing.T) {
	s := newOSTestServer()
	// 写入 default 租户预置 + t1 租户自定义。
	s.store.SaveOSTemplate(&store.OSTemplate{ID: "preset-1", TenantID: "default", Name: "p", Config: `{"id":"preset-1","name":"p","commands":"x"}`})
	s.store.SaveOSTemplate(&store.OSTemplate{ID: "custom-1", TenantID: "t1", Name: "c", Config: `{"id":"custom-1","name":"c","commands":"y"}`})

	got := s.listOSTemplatesFromStore("t1")
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

// TestGetOSTemplateByID_Fallback 验证 getOSTemplateByID 回退到预置模板。
func TestGetOSTemplateByID_Fallback(t *testing.T) {
	s := newOSTestServer()
	got := s.getOSTemplateByID("kernel-tune")
	if got == nil || got.ID != "kernel-tune" {
		t.Fatalf("fallback to preset failed: %+v", got)
	}
}

// TestGetOSTemplateByID_FromStore 验证 getOSTemplateByID 优先从 store 读取。
func TestGetOSTemplateByID_FromStore(t *testing.T) {
	s := newOSTestServer()
	s.store.SaveOSTemplate(&store.OSTemplate{ID: "x", TenantID: "default", Name: "stored", Config: `{"id":"x","name":"stored","commands":"y"}`})
	got := s.getOSTemplateByID("x")
	if got == nil || got.Name != "stored" {
		t.Fatalf("from store failed: %+v", got)
	}
}

// TestHandleListOSTemplates_NoAuth 验证 requireAuth=true 且无身份头返回 401。
func TestHandleListOSTemplates_NoAuth(t *testing.T) {
	s := newOSTestServer()
	s.requireAuth = true
	s.cfg.Demo = false
	req := httptest.NewRequest(http.MethodGet, "/api/v1/os-templates", nil)
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

// TestHandleListOSTemplates_BadTenant 验证非 demo 非 requireAuth 模式下空租户头返回 400。
func TestHandleListOSTemplates_BadTenant(t *testing.T) {
	s := newOSTestServer()
	s.cfg.Demo = false
	req := httptest.NewRequest(http.MethodGet, "/api/v1/os-templates", nil)
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleExecuteOSTemplate_InvalidJSON 验证无效 JSON 返回 400。
func TestHandleExecuteOSTemplate_InvalidJSON(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates/kernel-tune/execute", strings.NewReader(`{bad`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleExecuteOSTemplate_NewModeIntParam 验证新模式 int 参数类型校验。
func TestHandleExecuteOSTemplate_NewModeIntParam(t *testing.T) {
	s := newOSTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	// limits-config 的 nofile 为 int 类型，提供非整数应失败。
	body := `{"agentID":"` + a.AgentID + `","paramsMap":{"nofile":"not-int"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates/limits-config/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 (int parse); body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleExecuteOSTemplate_NewModeHappy 验证新模式（带 Params 定义）成功渲染并执行。
func TestHandleExecuteOSTemplate_NewModeHappy(t *testing.T) {
	s := newOSTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"agentID":"` + a.AgentID + `","paramsMap":{"ntpserver":"pool.ntp.org"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates/ntp-setup/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleExecuteOSTemplate_TenantMismatch 验证 agent 租户与头租户不一致返回 403。
func TestHandleExecuteOSTemplate_TenantMismatch(t *testing.T) {
	s := newOSTestServer()
	// 注册一个 t1 租户的 agent。
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	body := `{"agentID":"` + a.AgentID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates/kernel-tune/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "t2") // 冒充他租户
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleCreateOSTemplate_AuditLogged 验证创建模板记录审计事件。
func TestHandleCreateOSTemplate_AuditLogged(t *testing.T) {
	s := newOSTestServer()
	body := `{"name":"audit-test","commands":"echo hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	audits := s.store.Audits()
	found := false
	for _, a := range audits {
		if a.Action == "os_template_create" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("audit event os_template_create not logged")
	}
}

// TestHandleDeleteOSTemplate_AuditLogged 验证删除模板记录审计事件。
func TestHandleDeleteOSTemplate_AuditLogged(t *testing.T) {
	s := newOSTestServer()
	s.store.SaveOSTemplate(&store.OSTemplate{ID: "aud-del", TenantID: "default", Name: "x", Config: `{"id":"aud-del","name":"x","commands":"y"}`})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/os-templates/aud-del", nil)
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	audits := s.store.Audits()
	found := false
	for _, a := range audits {
		if a.Action == "os_template_delete" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("audit event os_template_delete not logged")
	}
}

// TestHandleUpdateOSTemplate_AuditLogged 验证更新模板记录审计事件。
func TestHandleUpdateOSTemplate_AuditLogged(t *testing.T) {
	s := newOSTestServer()
	s.store.SaveOSTemplate(&store.OSTemplate{ID: "aud-upd", TenantID: "default", Name: "old", Config: `{"id":"aud-upd","name":"old","commands":"x"}`})

	body := `{"name":"new","commands":"y"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/os-templates/aud-upd", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	audits := s.store.Audits()
	found := false
	for _, a := range audits {
		if a.Action == "os_template_update" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("audit event os_template_update not logged")
	}
}

// TestHandleExecuteOSTemplate_AuditLogged 验证执行模板记录审计事件。
func TestHandleExecuteOSTemplate_AuditLogged(t *testing.T) {
	s := newOSTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := `{"agentID":"` + a.AgentID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates/kernel-tune/execute", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOSTemplateRouting(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	audits := s.store.Audits()
	found := false
	for _, a := range audits {
		if a.Action == "execute_os_template" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("audit event execute_os_template not logged")
	}
}

// TestHandleListOSTemplates_WithSeed 验证 seed 后列表从 store 读取（含预置）。
func TestHandleListOSTemplates_WithSeed(t *testing.T) {
	s := newOSTestServer()
	s.seedPresetOSTemplates()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/os-templates", nil)
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var list []OSTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("seeded store should return non-empty list")
	}
}

// TestHandleListOSTemplates_BadJSONBody 验证 POST 空 body 返回 400（JSON 解码失败）。
func TestHandleListOSTemplates_BadJSONBody(t *testing.T) {
	s := newOSTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/os-templates", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleListOSTemplates(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}
