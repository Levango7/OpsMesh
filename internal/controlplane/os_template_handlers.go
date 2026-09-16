// Package controlplane: os_template_handlers.go 实现 OS 模板的 CRUD/execute HTTP handler。
//
// 从 os_optimize.go 拆分而来，仅包含 HTTP handler 函数；类型定义、常量、预置模板、
// 验证函数、store 适配分别位于 os_optimize.go / os_template_validate.go / os_template_store.go。
package controlplane

import (
	"net/http"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

// handleListOSTemplates 处理 /api/v1/os-templates：
//   - GET：列出所有模板（从 store 读取，store 为空回退预置；可选 category/risk/os 过滤）
//   - POST：创建新模板（CRUD，需 os:write 权限）
func (s *Server) handleListOSTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListOSTemplatesGet(w, r)
	case http.MethodPost:
		s.handleCreateOSTemplate(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListOSTemplatesGet 处理 GET /api/v1/os-templates：列出所有模板（从 store 读取，含回退）。
// 可选查询参数 category 过滤；可选 risk 过滤；可选 os 过滤。
func (s *Server) handleListOSTemplatesGet(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "os:read"); !ok {
		return
	}
	q := r.URL.Query()
	category := q.Get("category")
	risk := q.Get("risk")
	osFilter := q.Get("os")
	all := s.listOSTemplatesFromStore(actx.TenantID)
	out := make([]OSTemplate, 0, len(all))
	for _, t := range all {
		if category != "" && t.Category != category {
			continue
		}
		if risk != "" && t.Risk != risk {
			continue
		}
		if osFilter != "" && t.OS != osFilter && t.OS != "all" {
			continue
		}
		out = append(out, t)
	}
	paginate.WriteJSON(w, http.StatusOK, out)
}

// handleCreateOSTemplate 处理 POST /api/v1/os-templates：创建新 OS 模板（CRUD）。
// 请求体即 OSTemplate JSON；ID 为空时由 store 分配随机 ID。
// 需 os:write 权限；创建后审计 + 事件总线 + SSE 通知。
func (s *Server) handleCreateOSTemplate(w http.ResponseWriter, r *http.Request) {
	if err := s.verifyFederationRequest(r); err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	caller, ok := s.requireProd(w, r, "os:write")
	if !ok {
		return
	}
	var tpl OSTemplate
	if err := decodeJSONBody(w, r, &tpl); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if tpl.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if tpl.Commands == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "commands is required"})
		return
	}
	// 基本字段校验：risk 必须为 low/medium/high（空则归一为 low）。
	tpl.Risk = normalizeRisk(tpl.Risk)
	st := osTemplateToStore(&tpl, actx.TenantID)
	if err := s.store.SaveOSTemplate(st); err != nil {
		writeInternalError(r.Context(), w, "osOptimize.saveTemplate", err)
		return
	}
	// 回读以获取 store 分配的 ID/时间戳。
	saved := osTemplateFromStore(s.store.GetOSTemplate(st.ID))
	userID := ""
	if caller != nil {
		userID = caller.ID
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: userID, Action: "os_template_create", Target: st.ID, Detail: sanitizeAuditDetail("name=" + tpl.Name),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: userID,
			Action: "os_template_create", Target: st.ID, Detail: sanitizeAuditDetail("name=" + tpl.Name), Level: events.LevelInfo,
		})
	}
	s.publishEvent(r.Context(), "os_template_changed", actx.TenantID, map[string]string{"templateID": st.ID, "action": "create"})
	paginate.WriteJSON(w, http.StatusCreated, saved)
}

// handleUpdateOSTemplate 处理 PUT /api/v1/os-templates/{id}：更新 OS 模板（CRUD）。
// 请求体为 OSTemplate JSON；ID 路径参数与 body.ID 不一致时以路径为准。
// 需 os:write 权限；不存在返回 404。
func (s *Server) handleUpdateOSTemplate(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.verifyFederationRequest(r); err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	caller, ok := s.requireProd(w, r, "os:write")
	if !ok {
		return
	}
	var tpl OSTemplate
	if err := decodeJSONBody(w, r, &tpl); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if tpl.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if tpl.Commands == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "commands is required"})
		return
	}
	// 检查存在性（含回退预置模板：预置模板在 store 中已 seed，此处能查到）。
	existing := s.store.GetOSTemplate(id)
	if existing == nil {
		// 回退检查：若为预置模板 ID 且尚未 seed，允许"upsert"（首次写入 store）。
		if preset := osTemplateByID(id); preset == nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
	}
	tpl.ID = id
	tpl.Risk = normalizeRisk(tpl.Risk)
	st := osTemplateToStore(&tpl, actx.TenantID)
	if err := s.store.SaveOSTemplate(st); err != nil {
		writeInternalError(r.Context(), w, "osOptimize.saveTemplate", err)
		return
	}
	saved := osTemplateFromStore(s.store.GetOSTemplate(id))
	userID := ""
	if caller != nil {
		userID = caller.ID
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: userID, Action: "os_template_update", Target: id, Detail: sanitizeAuditDetail("name=" + tpl.Name),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: userID,
			Action: "os_template_update", Target: id, Detail: sanitizeAuditDetail("name=" + tpl.Name), Level: events.LevelInfo,
		})
	}
	s.publishEvent(r.Context(), "os_template_changed", actx.TenantID, map[string]string{"templateID": id, "action": "update"})
	paginate.WriteJSON(w, http.StatusOK, saved)
}

// handleDeleteOSTemplate 处理 DELETE /api/v1/os-templates/{id}：删除 OS 模板（CRUD）。
// 需 os:write 权限；不存在返回 404；删除成功返回 204。
func (s *Server) handleDeleteOSTemplate(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	caller, ok := s.requireProd(w, r, "os:write")
	if !ok {
		return
	}
	existing := s.store.GetOSTemplate(id)
	if existing == nil {
		// 回退检查：预置模板 ID 但未 seed → 视为不存在。
		if osTemplateByID(id) == nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		// 预置模板未 seed，直接返回 204（内存中无法删除，但 store 中本就不存在）。
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.store.DeleteOSTemplate(id) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	userID := ""
	if caller != nil {
		userID = caller.ID
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: userID, Action: "os_template_delete", Target: id, Detail: sanitizeAuditDetail("name=" + existing.Name),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: userID,
			Action: "os_template_delete", Target: id, Detail: sanitizeAuditDetail("name=" + existing.Name), Level: events.LevelInfo,
		})
	}
	s.publishEvent(r.Context(), "os_template_changed", actx.TenantID, map[string]string{"templateID": id, "action": "delete"})
	w.WriteHeader(http.StatusNoContent)
}

// handleOSTemplateByID 处理 GET /api/v1/os-templates/{id}：返回模板详情（从 store 读取，含回退）。
func (s *Server) handleOSTemplateByID(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	_, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "os:read"); !ok {
		return
	}
	t := s.getOSTemplateByID(id)
	if t == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, t)
}

// handleExecuteOSTemplate 处理 POST /api/v1/os-templates/{id}/execute：在指定 agent 上执行模板。
// 请求体: { "agentID": "...", "params": ["arg1", "arg2"] }
// 实现：将模板 Commands 通过 `set --` 注入位置参数后作为 shell task 下发，复用 store.CreateTask。
func (s *Server) handleExecuteOSTemplate(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if err := s.verifyFederationRequest(r); err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "os:execute"); !ok {
		return
	}
	tpl := s.getOSTemplateByID(id)
	if tpl == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	var body struct {
		AgentID   string            `json:"agentID"`
		Params    []string          `json:"params"`
		ParamsMap map[string]string `json:"paramsMap"`
		TenantID  string            `json:"tenantID"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.AgentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}
	// 参数验证与 command 拼接：
	// - 新模式（模板有 Params 定义）：用 paramsMap 做占位符替换 + 验证；
	// - 旧模式（无 Params 定义）：用 params []string 通过 `set --` 注入位置参数。
	var command string
	if len(tpl.Params) > 0 {
		paramsMap := body.ParamsMap
		if paramsMap == nil {
			paramsMap = map[string]string{}
		}
		// 填充默认值 + 校验必填。
		for _, p := range tpl.Params {
			val, ok := paramsMap[p.Name]
			if !ok || val == "" {
				if p.Default != "" {
					paramsMap[p.Name] = p.Default
					continue
				}
				if p.Required {
					paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "param required: " + p.Name})
					return
				}
			}
			_ = val
		}
		// 类型与语义验证。
		if err := validateOSParams(tpl.Params, paramsMap); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// shell 元字符校验：占位符替换前拒绝含元字符的值，防命令注入。
		if err := validateShellSafeValues(paramsMap); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		command = renderOSScript(tpl.Commands, paramsMap)
	} else {
		command = buildOSExecuteCommand(tpl.Commands, body.Params)
	}
	// 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	targetTenant := actx.TenantID
	agent := s.lookupAgent(body.AgentID)
	if agent == nil || (targetTenant != "" && agent.TenantID != targetTenant) {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "agent not found or tenant mismatch"})
		return
	}
	// 拼接最终 command：通过 `set --` 注入位置参数，使脚本内 $1/$2/... 可用。

	task := s.store.CreateTask(&proto.Task{
		AgentID:    body.AgentID,
		TenantID:   targetTenant,
		Type:       proto.TaskTypeShell,
		Command:    command,
		MaxRetries: s.cfg.TaskMaxRetries,
	})
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: targetTenant,
		UserID:   actx.UserID,
		Action:   "execute_os_template",
		Target:   task.TaskID,
		Detail:   sanitizeAuditDetail("template=" + id + " agent=" + body.AgentID),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: targetTenant, UserID: actx.UserID,
			Action: "execute_os_template", Target: task.TaskID,
			Detail: sanitizeAuditDetail("template=" + id + " agent=" + body.AgentID), Level: events.LevelInfo,
		})
	}
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.store.PendingDepth())
	}
	// SSE：通知前端 OS 模板执行任务已创建。
	// 租户隔离：携带 targetTenant，仅同租户订阅者收到。
	// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	s.publishEvent(r.Context(), "task_status", targetTenant, map[string]string{
		"taskID":  task.TaskID,
		"status":  task.Status,
		"agentID": body.AgentID,
	})
	paginate.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"task":         task,
		"templateID":   id,
		"templateName": tpl.Name,
	})
}
