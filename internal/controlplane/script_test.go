// script_test.go 测试 Phase 5 自定义脚本 HTTP handler（script.go）。
//
// 覆盖范围：
//   - handleListScripts：空列表、创建后列表
//   - handleCreateScript：正常创建、缺必填字段、无效 language、无效 JSON
//   - handleGetScript：正常获取、不存在
//   - handleUpdateScript：正常更新、不存在
//   - handleDeleteScript：正常删除、不存在
//   - handleScriptExecute：正常执行、缺 deviceID、不存在
//   - handleScriptExecutions：执行记录列表
//   - handleScripts：method not allowed 分派
//   - handleScriptRouting：{id} 路由分派、空 id、未知子路径
//   - 鉴权：无 token 返回 401
//
// 测试策略（与 webhook_test.go 风格一致）：
//   - 白盒（package controlplane），直接装配 Server{store: MemoryStore, jwtSecret: 固定}；
//   - 鉴权用例通过 admin 登录获取 token（requirePermission 校验 script:read/write）；
//   - 用 httptest.NewRequest + httptest.NewRecorder 直接调用 handler，断言 status code 与响应体。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// newScriptTestServer 构造脚本 API 测试用 Server。
func newScriptTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{TaskMaxRetries: 3},
		jwtSecret:    []byte("test-jwt-secret-for-script-test-32bytes!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

// =============================================================================
// handleListScripts（GET /api/v1/scripts）
// ============================================================================

// TestHandleListScripts_Empty 验证空列表返回 200 + scripts:[]。
func TestHandleListScripts_Empty(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scripts", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleScripts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Scripts []*store.Script `json:"scripts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Scripts) != 0 {
		t.Fatalf("scripts=%d, want 0", len(resp.Scripts))
	}
}

// TestHandleListScripts_AfterCreate 验证创建后列表含 1 个脚本。
func TestHandleListScripts_AfterCreate(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	s.store.CreateScript("default", &store.Script{
		Name:     "list-test",
		Language: "shell",
		Content:  "echo hello",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scripts", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleScripts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Scripts []*store.Script `json:"scripts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Scripts) != 1 {
		t.Fatalf("scripts=%d, want 1", len(resp.Scripts))
	}
	if resp.Scripts[0].Name != "list-test" {
		t.Fatalf("Name=%q, want list-test", resp.Scripts[0].Name)
	}
}

// TestHandleListScripts_NoAuth 验证无 Authorization 头返回 401。
func TestHandleListScripts_NoAuth(t *testing.T) {
	s := newScriptTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/scripts", nil)
	w := httptest.NewRecorder()
	s.handleScripts(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

// =============================================================================
// handleCreateScript（POST /api/v1/scripts）
// ============================================================================

// TestHandleCreateScript 验证正常创建返回 201 + 脚本（含 ID）。
func TestHandleCreateScript(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"test-script","language":"shell","content":"echo hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScripts(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var sc store.Script
	if err := json.Unmarshal(w.Body.Bytes(), &sc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sc.ID == "" {
		t.Fatal("ID is empty, want server-assigned")
	}
	if sc.Name != "test-script" {
		t.Fatalf("Name=%q, want test-script", sc.Name)
	}
	if sc.Language != "shell" {
		t.Fatalf("Language=%q, want shell", sc.Language)
	}
	// 确认已持久化到 store
	got, ok := s.store.GetScript("default", sc.ID)
	if !ok || got == nil {
		t.Fatal("GetScript returned nil after create")
	}
}

// TestHandleCreateScript_DefaultLanguage 验证 language 为空时默认 shell。
func TestHandleCreateScript_DefaultLanguage(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"test-script","content":"echo hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScripts(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var sc store.Script
	if err := json.Unmarshal(w.Body.Bytes(), &sc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sc.Language != "shell" {
		t.Fatalf("Language=%q, want shell (default)", sc.Language)
	}
}

// TestHandleCreateScript_MissingName 验证缺 name 返回 400。
func TestHandleCreateScript_MissingName(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"content":"echo hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScripts(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateScript_MissingContent 验证缺 content 返回 400。
func TestHandleCreateScript_MissingContent(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"test-script"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScripts(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateScript_InvalidLanguage 验证无效 language 返回 400。
func TestHandleCreateScript_InvalidLanguage(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"test-script","language":"ruby","content":"puts hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScripts(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleCreateScript_InvalidJSON 验证无效 JSON 返回 400。
func TestHandleCreateScript_InvalidJSON(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":invalid`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScripts(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleGetScript（GET /api/v1/scripts/{id}）
// ============================================================================

// TestHandleGetScript 验证正常获取脚本详情。
func TestHandleGetScript(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateScript("default", &store.Script{Name: "get-test", Content: "echo hello"})
	if created == nil {
		t.Fatal("CreateScript returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scripts/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var sc store.Script
	if err := json.Unmarshal(w.Body.Bytes(), &sc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sc.ID != created.ID {
		t.Fatalf("ID=%q, want %q", sc.ID, created.ID)
	}
}

// TestHandleGetScript_NotFound 验证获取不存在的脚本返回 404。
func TestHandleGetScript_NotFound(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scripts/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleUpdateScript（PUT /api/v1/scripts/{id}）
// ============================================================================

// TestHandleUpdateScript 验证正常更新脚本。
func TestHandleUpdateScript(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateScript("default", &store.Script{Name: "update-test", Content: "echo hello"})
	if created == nil {
		t.Fatal("CreateScript returned nil")
	}

	body := `{"name":"updated-name","content":"echo world"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/scripts/"+created.ID, strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var sc store.Script
	if err := json.Unmarshal(w.Body.Bytes(), &sc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sc.Name != "updated-name" {
		t.Fatalf("Name=%q, want updated-name", sc.Name)
	}
	if sc.Content != "echo world" {
		t.Fatalf("Content=%q, want echo world", sc.Content)
	}
}

// TestHandleUpdateScript_NotFound 验证更新不存在的脚本返回 404。
func TestHandleUpdateScript_NotFound(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"name":"updated-name","content":"echo hello"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/scripts/nonexistent", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleDeleteScript（DELETE /api/v1/scripts/{id}）
// ============================================================================

// TestHandleDeleteScript 验证正常删除脚本。
func TestHandleDeleteScript(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateScript("default", &store.Script{Name: "delete-test", Content: "echo hello"})
	if created == nil {
		t.Fatal("CreateScript returned nil")
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/scripts/"+created.ID, nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	// 确认已删除
	if _, ok := s.store.GetScript("default", created.ID); ok {
		t.Fatal("GetScript returned ok after delete")
	}
}

// TestHandleDeleteScript_NotFound 验证删除不存在的脚本返回 404。
func TestHandleDeleteScript_NotFound(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/scripts/nonexistent", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleScriptExecute（POST /api/v1/scripts/{id}/execute）
// ============================================================================

// TestHandleScriptExecute 验证正常执行返回 202 + 执行记录（真实执行：创建 task 下发）。
func TestHandleScriptExecute(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateScript("default", &store.Script{Name: "exec-test", Content: "echo hello"})
	if created == nil {
		t.Fatal("CreateScript returned nil")
	}

	body := `{"deviceID":"dev-01"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts/"+created.ID+"/execute", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d, want 202; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		ExecutionID string `json:"executionID"`
		TaskID      string `json:"taskID"`
		ScriptID    string `json:"scriptID"`
		DeviceID    string `json:"deviceID"`
		Status      string `json:"status"`
		Message     string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ScriptID != created.ID {
		t.Fatalf("ScriptID=%q, want %q", resp.ScriptID, created.ID)
	}
	if resp.DeviceID != "dev-01" {
		t.Fatalf("DeviceID=%q, want dev-01", resp.DeviceID)
	}
	if resp.Status != "pending" {
		t.Fatalf("Status=%q, want pending", resp.Status)
	}
	if resp.TaskID == "" {
		t.Fatal("TaskID should not be empty")
	}
}

// TestHandleScriptExecute_MissingDeviceID 验证缺 deviceID 返回 400。
func TestHandleScriptExecute_MissingDeviceID(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateScript("default", &store.Script{Name: "exec-test", Content: "echo hello"})
	if created == nil {
		t.Fatal("CreateScript returned nil")
	}

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts/"+created.ID+"/execute", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleScriptExecute_NotFound 验证执行不存在的脚本返回 404。
func TestHandleScriptExecute_NotFound(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	body := `{"deviceID":"dev-01"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts/nonexistent/execute", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// =============================================================================
// handleScriptExecutions（GET /api/v1/scripts/{id}/executions）
// ============================================================================

// TestHandleScriptExecutions 验证执行记录列表。
func TestHandleScriptExecutions(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateScript("default", &store.Script{Name: "exec-list-test", Content: "echo hello"})
	if created == nil {
		t.Fatal("CreateScript returned nil")
	}
	// 先记录一条执行
	// M3：直接经 Store 接口调用，消除对 *MemoryStore 的类型断言。
	now := time.Now()
	finishedAt := now
	s.store.RecordScriptExecution("default", created.ID, "dev-01", "succeeded", "ok", "", now, &finishedAt)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scripts/"+created.ID+"/executions", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Executions []*store.ScriptExecution `json:"executions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Executions) != 1 {
		t.Fatalf("executions=%d, want 1", len(resp.Executions))
	}
}

// TestHandleScriptExecutions_Empty 验证空执行记录返回 200 + executions:[]。
func TestHandleScriptExecutions_Empty(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	created := s.store.CreateScript("default", &store.Script{Name: "exec-empty", Content: "echo hello"})
	if created == nil {
		t.Fatal("CreateScript returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scripts/"+created.ID+"/executions", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Executions []*store.ScriptExecution `json:"executions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Executions) != 0 {
		t.Fatalf("executions=%d, want 0", len(resp.Executions))
	}
}

// =============================================================================
// handleScriptRouting 路由分派
// ============================================================================

// TestHandleScriptRouting_EmptyID 验证空 id 返回 400。
func TestHandleScriptRouting_EmptyID(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scripts/", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// TestHandleScriptRouting_UnknownSubPath 验证未知子路径返回 404。
func TestHandleScriptRouting_UnknownSubPath(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts/some-id/unknown", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleScriptRouting(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// TestHandleScripts_MethodNotAllowed 验证不支持的方法返回 405。
func TestHandleScripts_MethodNotAllowed(t *testing.T) {
	s := newScriptTestServer()
	auth := loginAsAdmin(t, s)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/scripts", nil)
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleScripts(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", w.Code)
	}
}
