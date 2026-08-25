// pipeline_test.go 测试 Phase 2 CI/CD 流水线 HTTP handler（pipeline.go）。
//
// 覆盖范围：
//   - handleListPipelineTemplates：空列表、创建后列表
//   - handleCreatePipelineTemplate：正常创建、缺必填字段、无效 JSON
//   - handleGetPipelineTemplate：正常获取、不存在
//   - handleUpdatePipelineTemplate：正常更新、不存在
//   - handleDeletePipelineTemplate：正常删除、不存在
//   - handleRunPipelineTemplate：触发运行
//   - handleListPipelineRuns / handlePipelineRun
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 ticket_test.go 风格一致）：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore}；
//   - 鉴权用例通过 admin 登录获取 token（requirePermission 校验 pipeline:read/write）。
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

// newPipelineTestServer 构造流水线 API 测试用 Server。
func newPipelineTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-pipeline-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// =============================================================================
// handleListPipelineTemplates（GET /api/v1/pipeline/templates）
// =============================================================================

// TestHandleListPipelineTemplates_Empty 验证空列表返回 200 + templates:[]。
func TestHandleListPipelineTemplates_Empty(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline/templates", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePipelineTemplates(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Templates []*store.PipelineTemplate `json:"templates"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Templates) != 0 {
		t.Fatalf("templates=%d, want 0", len(resp.Templates))
	}
}

// TestHandleListPipelineTemplates_AfterCreate 验证创建后列表含 1 个模板。
func TestHandleListPipelineTemplates_AfterCreate(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	s.store.CreateTemplate("default", &store.PipelineTemplate{
		Name: "build-pipeline",
		Type: "tekton",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline/templates", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePipelineTemplates(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Templates []*store.PipelineTemplate `json:"templates"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Templates) != 1 {
		t.Fatalf("templates=%d, want 1", len(resp.Templates))
	}
	if resp.Templates[0].Name != "build-pipeline" {
		t.Fatalf("Name=%q, want build-pipeline", resp.Templates[0].Name)
	}
}

// TestHandleListPipelineTemplates_NoAuth 验证无 Authorization 头返回 401。
func TestHandleListPipelineTemplates_NoAuth(t *testing.T) {
	s := newPipelineTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline/templates", nil)
	w := httptest.NewRecorder()
	s.handlePipelineTemplates(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// =============================================================================
// handleCreatePipelineTemplate（POST /api/v1/pipeline/templates）
// =============================================================================

// TestHandleCreatePipelineTemplate 验证正常创建返回 201 + 模板（含 ID）。
func TestHandleCreatePipelineTemplate(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"ci-build","description":"CI build pipeline","type":"tekton","yaml":"steps: []"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipeline/templates", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handlePipelineTemplates(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var tpl store.PipelineTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &tpl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tpl.ID == "" {
		t.Fatal("ID is empty, want server-assigned")
	}
	if tpl.Name != "ci-build" {
		t.Fatalf("Name=%q, want ci-build", tpl.Name)
	}
	if tpl.Type != "tekton" {
		t.Fatalf("Type=%q, want tekton", tpl.Type)
	}
}

// TestHandleCreatePipelineTemplate_MissingName 验证缺 name 返回 400。
func TestHandleCreatePipelineTemplate_MissingName(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"type":"tekton"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipeline/templates", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handlePipelineTemplates(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreatePipelineTemplate_InvalidJSON 验证无效 JSON 返回 400。
func TestHandleCreatePipelineTemplate_InvalidJSON(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":invalid`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipeline/templates", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handlePipelineTemplates(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleGetPipelineTemplate（GET /api/v1/pipeline/templates/{id}）
// =============================================================================

// TestHandleGetPipelineTemplate 验证正常获取模板详情。
func TestHandleGetPipelineTemplate(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateTemplate("default", &store.PipelineTemplate{Name: "get-test", Type: "jenkins"})
	if created == nil {
		t.Fatal("CreateTemplate returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline/templates/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePipelineTemplate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var tpl store.PipelineTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &tpl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tpl.ID != created.ID {
		t.Fatalf("ID=%q, want %q", tpl.ID, created.ID)
	}
	if tpl.Name != "get-test" {
		t.Fatalf("Name=%q, want get-test", tpl.Name)
	}
}

// TestHandleGetPipelineTemplate_NotFound 验证获取不存在的模板返回 404。
func TestHandleGetPipelineTemplate_NotFound(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline/templates/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePipelineTemplate(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleUpdatePipelineTemplate（PUT /api/v1/pipeline/templates/{id}）
// =============================================================================

// TestHandleUpdatePipelineTemplate 验证正常更新模板。
func TestHandleUpdatePipelineTemplate(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateTemplate("default", &store.PipelineTemplate{Name: "update-test", Type: "tekton"})
	if created == nil {
		t.Fatal("CreateTemplate returned nil")
	}

	body := `{"name":"updated-name","type":"jenkins","yaml":"steps: [build]"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/pipeline/templates/"+created.ID, strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handlePipelineTemplate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var tpl store.PipelineTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &tpl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tpl.Name != "updated-name" {
		t.Fatalf("Name=%q, want updated-name", tpl.Name)
	}
	if tpl.Type != "jenkins" {
		t.Fatalf("Type=%q, want jenkins", tpl.Type)
	}
}

// TestHandleUpdatePipelineTemplate_NotFound 验证更新不存在的模板返回 404。
func TestHandleUpdatePipelineTemplate_NotFound(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"updated-name"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/pipeline/templates/nonexistent", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handlePipelineTemplate(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleDeletePipelineTemplate（DELETE /api/v1/pipeline/templates/{id}）
// =============================================================================

// TestHandleDeletePipelineTemplate 验证正常删除模板。
func TestHandleDeletePipelineTemplate(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateTemplate("default", &store.PipelineTemplate{Name: "delete-test"})
	if created == nil {
		t.Fatal("CreateTemplate returned nil")
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pipeline/templates/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePipelineTemplate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	if _, ok := s.store.GetTemplate("default", created.ID); ok {
		t.Fatal("template still exists after delete")
	}
}

// TestHandleDeletePipelineTemplate_NotFound 验证删除不存在的模板返回 404。
func TestHandleDeletePipelineTemplate_NotFound(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pipeline/templates/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePipelineTemplate(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleRunPipelineTemplate（POST /api/v1/pipeline/templates/{id}/run）
// =============================================================================

// TestHandleRunPipelineTemplate 验证触发流水线运行。
func TestHandleRunPipelineTemplate(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateTemplate("default", &store.PipelineTemplate{Name: "run-test", Type: "tekton"})
	if created == nil {
		t.Fatal("CreateTemplate returned nil")
	}

	body := `{"parameters":{"branch":"main"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipeline/templates/"+created.ID+"/run", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handlePipelineTemplate(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var run store.PipelineRun
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if run.ID == "" {
		t.Fatal("run ID is empty")
	}
	if run.TemplateID != created.ID {
		t.Fatalf("TemplateID=%q, want %q", run.TemplateID, created.ID)
	}
	if run.Status != "pending" {
		t.Fatalf("Status=%q, want pending", run.Status)
	}
}

// TestHandleRunPipelineTemplate_NotFound 验证触发不存在的模板返回 404。
func TestHandleRunPipelineTemplate_NotFound(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipeline/templates/nonexistent/run", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePipelineTemplate(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleListPipelineRuns / handlePipelineRun
// =============================================================================

// TestHandleListPipelineRuns 验证列出运行记录。
func TestHandleListPipelineRuns(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	tpl := s.store.CreateTemplate("default", &store.PipelineTemplate{Name: "list-run-test"})
	s.store.CreateRun("default", &store.PipelineRun{TemplateID: tpl.ID, TemplateName: tpl.Name, Status: "running"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline/runs", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePipelineRuns(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Runs []*store.PipelineRun `json:"runs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Runs) != 1 {
		t.Fatalf("runs=%d, want 1", len(resp.Runs))
	}
}

// TestHandleGetPipelineRun 验证获取运行详情。
func TestHandleGetPipelineRun(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateRun("default", &store.PipelineRun{Status: "succeeded"})
	if created == nil {
		t.Fatal("CreateRun returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline/runs/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePipelineRun(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var run store.PipelineRun
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if run.ID != created.ID {
		t.Fatalf("ID=%q, want %q", run.ID, created.ID)
	}
}

// TestHandleGetPipelineRun_NotFound 验证获取不存在的运行返回 404。
func TestHandleGetPipelineRun_NotFound(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline/runs/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePipelineRun(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// 路由分派
// =============================================================================

// TestHandlePipelineTemplates_MethodNotAllowed 验证不支持的方法返回 405。
func TestHandlePipelineTemplates_MethodNotAllowed(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pipeline/templates", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePipelineTemplates(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
}

// TestHandlePipelineTemplate_EmptyID 验证空 id 返回 400。
func TestHandlePipelineTemplate_EmptyID(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline/templates/", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePipelineTemplate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandlePipelineTemplate_UnknownSubPath 验证未知子路径返回 404。
func TestHandlePipelineTemplate_UnknownSubPath(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipeline/templates/some-id/unknown", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePipelineTemplate(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}