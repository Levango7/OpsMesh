package controlplane

// script.go 实现 Phase 5 自定义脚本 HTTP handler（CRUD + 执行 + 执行记录）。
//
// API 端点：
//   - GET    /api/v1/scripts           列出脚本
//   - POST   /api/v1/scripts           创建脚本
//   - GET    /api/v1/scripts/{id}      脚本详情
//   - PUT    /api/v1/scripts/{id}      更新脚本
//   - DELETE /api/v1/scripts/{id}      删除脚本
//   - POST   /api/v1/scripts/{id}/execute  执行脚本（下发到指定设备）
//   - GET    /api/v1/scripts/{id}/executions  执行记录
//
// 设计要点（与 webhook.go 风格一致）：
//   - 用 s.requireTenantContext(w, r) 提取租户；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 用 decodeJSONBody 解析请求体；
//   - 鉴权：需 script:read/script:write 权限。
//   - execute 端点：下发 shell/python task 到指定 agent（复用现有任务机制），
//     同时记录一条 ScriptExecution（pending 状态）；agent 上报后由控制面更新状态。
//     MVP 实现仅记录执行记录（不下发实际任务，避免无 agent 时报错）。

import (
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// clampScriptTimeout 将 timeoutSec clamp 至 [1,600] 秒（L1 输入校验）。
// <1 视为 1 秒（最小执行窗口）；>600 截断为 600 秒（防 DoS 长时间占用 agent）。
func clampScriptTimeout(sec int) int {
	const (
		minTimeoutSec = 1
		maxTimeoutSec = 600
	)
	if sec < minTimeoutSec {
		return minTimeoutSec
	}
	if sec > maxTimeoutSec {
		return maxTimeoutSec
	}
	return sec
}

// handleScripts 统一处理 /api/v1/scripts：
//   - GET：列出脚本
//   - POST：创建脚本
func (s *Server) handleScripts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListScripts(w, r)
	case http.MethodPost:
		s.handleCreateScript(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListScripts 处理 GET /api/v1/scripts：列出脚本。
func (s *Server) handleListScripts(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "script:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	scripts := s.store.ListScripts(actx.TenantID)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"scripts": scripts})
}

// handleCreateScript 处理 POST /api/v1/scripts：创建脚本。
func (s *Server) handleCreateScript(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "script:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	var body store.Script
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.Content == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}
	if body.Language != "" && body.Language != "shell" && body.Language != "python" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "language must be shell or python"})
		return
	}
	// L1 输入校验：timeoutSec clamp 至 [1,600] 秒。
	// <1 视为默认 1 秒；>600 截断为 600 秒（防 DoS 长时间占用 agent）。
	body.TimeoutSec = clampScriptTimeout(body.TimeoutSec)
	// 新建脚本默认启用（Enabled=true）：用户创建脚本即为可执行，
	// 禁用需显式 PUT 更新 Enabled=false。避免零值 false 导致 execute 全 409。
	body.Enabled = true
	created := s.store.CreateScript(actx.TenantID, &body)
	if created == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "create script failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "script_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	paginate.WriteJSON(w, http.StatusCreated, created)
}

// handleScriptRouting 分派 /api/v1/scripts/{id} 子路径：
//   - GET    /api/v1/scripts/{id}        脚本详情
//   - PUT    /api/v1/scripts/{id}        更新脚本
//   - DELETE /api/v1/scripts/{id}        删除脚本
//   - POST   /api/v1/scripts/{id}/execute    执行脚本
//   - GET    /api/v1/scripts/{id}/executions 执行记录
func (s *Server) handleScriptRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/scripts/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "script id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "script id required"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetScript(w, r, id)
		case http.MethodPut:
			s.handleUpdateScript(w, r, id)
		case http.MethodDelete:
			s.handleDeleteScript(w, r, id)
		default:
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "execute":
		s.handleScriptExecute(w, r, id)
	case "executions":
		s.handleScriptExecutions(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetScript 处理 GET /api/v1/scripts/{id}：脚本详情。
func (s *Server) handleGetScript(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "script:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	sc, ok := s.store.GetScript(actx.TenantID, id)
	if !ok || sc == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "script not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, sc)
}

// handleUpdateScript 处理 PUT /api/v1/scripts/{id}：更新脚本。
func (s *Server) handleUpdateScript(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "script:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	var body store.Script
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	body.ID = id
	// L1 输入校验：timeoutSec clamp 至 [1,600] 秒（与 create 路径一致）。
	body.TimeoutSec = clampScriptTimeout(body.TimeoutSec)
	updated, ok := s.store.UpdateScript(actx.TenantID, &body)
	if !ok || updated == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "script not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "script_update", Target: id, Detail: sanitizeAuditDetail("name=" + updated.Name),
	})
	paginate.WriteJSON(w, http.StatusOK, updated)
}

// handleDeleteScript 处理 DELETE /api/v1/scripts/{id}：删除脚本。
func (s *Server) handleDeleteScript(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "script:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	if !s.store.DeleteScript(actx.TenantID, id) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "script not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "script_delete", Target: id, Detail: "",
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleScriptExecute 处理 POST /api/v1/scripts/{id}/execute：执行脚本。
// 请求体：{"deviceID": "...", "params": "..."}；deviceID 必填。
// 真实执行：创建 Task(type=shell)→agent 领取→执行→回写结果。
func (s *Server) handleScriptExecute(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "script:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	sc, ok := s.store.GetScript(actx.TenantID, id)
	if !ok || sc == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "script not found"})
		return
	}
	// L1 输入校验：禁用脚本拒绝执行，返回 409 Conflict。
	// 已禁用脚本不应被下发到 agent，避免误触发；启用需先 POST /enable。
	if !sc.Enabled {
		paginate.WriteJSON(w, http.StatusConflict, map[string]string{"error": "script is disabled, enable it before execution"})
		return
	}
	var body struct {
		DeviceID string `json:"deviceID"`
		Params   string `json:"params"`
	}
	// 请求体可选（GET 也允许，但 POST 推荐 JSON 体）。
	_ = decodeJSONBody(w, r, &body)
	if body.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}

	// 真实执行：创建 shell 任务下发到指定 agent。
	task := &proto.Task{
		Type:     "shell",
		Command:  sc.Content,
		AgentID:  body.DeviceID,
		TenantID: actx.TenantID,
		Status:   "pending",
	}
	created := s.store.CreateTask(task)
	if created == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create execution task"})
		return
	}

	// 记录执行记录（初始状态 pending，agent 执行完毕后回写）。
	now := time.Now()
	exec := s.store.RecordScriptExecution(actx.TenantID, id, body.DeviceID, "pending",
		"task created: "+created.TaskID, "", now, nil)
	if exec == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to record execution"})
		return
	}

	// 审计：记录执行人。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "script_execute", Target: id,
		Detail: sanitizeAuditDetail("deviceID=" + body.DeviceID + " taskID=" + created.TaskID),
	})

	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"executionID": exec.ID,
		"taskID":      created.TaskID,
		"scriptID":    id,
		"deviceID":    body.DeviceID,
		"status":      "pending",
		"message":     "script execution task created, waiting for agent to execute",
		"startedAt":   now,
	})
}

// handleScriptExecutions 处理 GET /api/v1/scripts/{id}/executions：执行记录。
func (s *Server) handleScriptExecutions(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "script:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	executions := s.store.ListScriptExecutions(actx.TenantID, id)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"executions": executions})
}
