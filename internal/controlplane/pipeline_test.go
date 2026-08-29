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
	"opsmesh/internal/proto"
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
// G2 修复：任务认领（AgentID/ParentID）+ run 状态流转（reconcileRunStatus）
// =============================================================================

// TestHandleRunPipelineTemplate_WithAgentID 验证模板带 AgentID 时：
// run 推进后派生任务写入 AgentID（认领）+ ParentID（血缘），run 保持 running（不再立即 succeeded）。
func TestHandleRunPipelineTemplate_WithAgentID(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateTemplate("default", &store.PipelineTemplate{
		Name:    "claim-test",
		Type:    "tekton",
		YAML:    "echo hello",
		AgentID: "agent-claim-01",
	})
	if created == nil {
		t.Fatal("CreateTemplate returned nil")
	}

	body := `{}`
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

	// 推进 executor：pending → running，创建派生任务。
	s.processPendingPipelineRuns()

	// 派生任务应写入 AgentID=模板 AgentID + ParentID=run.ID。
	tasks := s.store.TasksByParent(run.ID)
	if len(tasks) != 1 {
		t.Fatalf("TasksByParent(run=%s) = %d, want 1", run.ID, len(tasks))
	}
	task := tasks[0]
	if task.AgentID != "agent-claim-01" {
		t.Fatalf("task.AgentID=%q, want agent-claim-01", task.AgentID)
	}
	if task.ParentID != run.ID {
		t.Fatalf("task.ParentID=%q, want %q", task.ParentID, run.ID)
	}

	// run 应保持 running（不再立即 succeeded）。
	stored, ok := s.store.GetRun("default", run.ID)
	if !ok {
		t.Fatal("run not found in store")
	}
	if stored.Status != "running" {
		t.Fatalf("stored run.Status=%q, want running", stored.Status)
	}
	// 查询详情时 reconcile 派生出 running（子任务未 done）。
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline/runs/"+run.ID, nil)
	req2.Header.Set("Authorization", auth)
	w2 := httptest.NewRecorder()
	s.handlePipelineRun(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("detail status=%d", w2.Code)
	}
	var gotRun store.PipelineRun
	_ = json.Unmarshal(w2.Body.Bytes(), &gotRun)
	if gotRun.Status != "running" {
		t.Fatalf("detail run.Status=%q, want running（子任务未 done）", gotRun.Status)
	}
}

// TestRunPipeline_AgentIDMissing 验证模板未指定 AgentID 时 run 置 failed。
func TestRunPipeline_AgentIDMissing(t *testing.T) {
	s := newPipelineTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateTemplate("default", &store.PipelineTemplate{
		Name: "no-agent-test",
		Type: "tekton",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipeline/templates/"+created.ID+"/run", strings.NewReader(`{}`))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handlePipelineTemplate(w, req)
	var run store.PipelineRun
	_ = json.Unmarshal(w.Body.Bytes(), &run)

	s.processPendingPipelineRuns()

	stored, ok := s.store.GetRun("default", run.ID)
	if !ok {
		t.Fatal("run not found in store")
	}
	if stored.Status != "failed" {
		t.Fatalf("stored run.Status=%q, want failed（模板无 AgentID 任务不可下发）", stored.Status)
	}
	if !strings.Contains(stored.Logs, "agentID not set") {
		t.Fatalf("run.Logs=%q, want contain 'agentID not set'", stored.Logs)
	}
}

// TestReconcileRunStatus 验证 run 终态推导规则。
func TestReconcileRunStatus(t *testing.T) {
	s := newPipelineTestServer()

	newRun := func(status string) *store.PipelineRun {
		r := s.store.CreateRun("default", &store.PipelineRun{Status: "running"})
		return r
	}

	// 1) 子任务全部 done → succeeded。
	runOK := newRun("running")
	s.store.CreateTask(&proto.Task{TaskID: "t-ok-1", AgentID: "a", ParentID: runOK.ID, Status: "done"})
	if got := s.reconcileRunStatus(runOK.ID); got != "succeeded" {
		t.Fatalf("全部 done 应 succeeded，got=%q", got)
	}

	// 2) 任一子任务 failed → failed。
	runFail := newRun("running")
	s.store.CreateTask(&proto.Task{TaskID: "t-fail-1", AgentID: "a", ParentID: runFail.ID, Status: "done"})
	s.store.CreateTask(&proto.Task{TaskID: "t-fail-2", AgentID: "a", ParentID: runFail.ID, Status: "failed"})
	if got := s.reconcileRunStatus(runFail.ID); got != "failed" {
		t.Fatalf("含 failed 子任务应 failed，got=%q", got)
	}

	// 3) 子任务未全部 done → running。
	runRun := newRun("running")
	s.store.CreateTask(&proto.Task{TaskID: "t-run-1", AgentID: "a", ParentID: runRun.ID, Status: "pending"})
	if got := s.reconcileRunStatus(runRun.ID); got != "running" {
		t.Fatalf("子任务 pending 应 running，got=%q", got)
	}

	// 4) 无子任务 → running。
	runEmpty := newRun("running")
	if got := s.reconcileRunStatus(runEmpty.ID); got != "running" {
		t.Fatalf("无子任务应 running，got=%q", got)
	}

	// 5) 空 ID → running（兜底）。
	if got := s.reconcileRunStatus(""); got != "running" {
		t.Fatalf("空 runID 应 running，got=%q", got)
	}
}

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
