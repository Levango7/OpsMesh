// Package controlplane: middleware_deploy_handler.go 实现中间件部署/卸载 HTTP handler。
//
// 从 middleware_deploy.go 拆分而来，包含 handleDeployMiddlewareTemplate（部署）、
// handleMiddlewareInstances（实例列表）、handleUninstallMiddlewareInstance（卸载）。
package controlplane

import (
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/events"
	"github.com/Levango7/OpsMesh/internal/proto"
)

func (s *Server) handleDeployMiddlewareTemplate(w http.ResponseWriter, r *http.Request, id string) {
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
	if _, ok := s.requireProd(w, r, "middleware:execute"); !ok {
		return
	}
	tpl := s.getMiddlewareTemplateByID(id)
	if tpl == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	var body struct {
		AgentID    string            `json:"agentID"`
		DeployType string            `json:"deployType"`
		Params     map[string]string `json:"params"`
		TenantID   string            `json:"tenantID"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.AgentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}
	if body.DeployType == "" {
		body.DeployType = "docker" // 默认 docker 部署
	}
	// 校验 deployType 是否在模板支持列表内。
	supported := false
	for _, dt := range tpl.DeployTypes {
		if dt == body.DeployType {
			supported = true
			break
		}
	}
	if !supported {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported deployType: " + body.DeployType})
		return
	}
	script, ok := tpl.Scripts[body.DeployType]
	if !ok {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "script not found for deployType: " + body.DeployType})
		return
	}
	// 校验必填参数。
	for _, p := range tpl.Params {
		if p.Required {
			val, ok := body.Params[p.Name]
			if !ok || val == "" {
				// 未提供则尝试用默认值。
				if p.Default != "" {
					if body.Params == nil {
						body.Params = map[string]string{}
					}
					body.Params[p.Name] = p.Default
					continue
				}
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "param required: " + p.Name})
				return
			}
		}
	}
	// 未提供的可选参数填充默认值。
	for _, p := range tpl.Params {
		if _, ok := body.Params[p.Name]; !ok && p.Default != "" {
			if body.Params == nil {
				body.Params = map[string]string{}
			}
			body.Params[p.Name] = p.Default
		}
	}
	// 参数类型与语义验证（端口范围/路径/非空）。
	if err := validateMiddlewareParams(tpl.Params, body.Params); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// shell 元字符校验：占位符替换前拒绝含元字符的值，防命令注入。
	if err := validateShellSafeValues(body.Params); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	targetTenant := actx.TenantID
	agent := s.lookupAgent(body.AgentID)
	if agent == nil || (targetTenant != "" && agent.TenantID != targetTenant) {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "agent not found or tenant mismatch"})
		return
	}
	// 拼接最终 command：将脚本中 {name}/{port}/... 占位符替换为 params 实际值。
	command := renderMiddlewareScript(script.Deploy, body.Params)
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
		Action:   "deploy_middleware",
		Target:   task.TaskID,
		Detail:   sanitizeAuditDetail("template=" + id + " deployType=" + body.DeployType + " agent=" + body.AgentID),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: targetTenant, UserID: actx.UserID,
			Action: "deploy_middleware", Target: task.TaskID,
			Detail: sanitizeAuditDetail("template=" + id + " deployType=" + body.DeployType + " agent=" + body.AgentID), Level: events.LevelInfo,
		})
	}
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.store.PendingDepth())
	}
	// SSE：通知前端新部署任务已创建。
	// 租户隔离：携带 targetTenant，仅同租户订阅者收到。
	// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	s.publishEvent(r.Context(), "task_status", targetTenant, map[string]string{
		"taskID":  task.TaskID,
		"status":  task.Status,
		"agentID": body.AgentID,
	})
	paginate.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"task":         task,
		"taskID":       task.TaskID,
		"templateID":   id,
		"templateName": tpl.Name,
		"deployType":   body.DeployType,
	})
}

// handleMiddlewareInstances 处理 GET /api/v1/middleware-instances：查询已部署中间件实例。
// MVP 实现：返回空数组（后续可从任务历史按 action=deploy_middleware 推导实例清单）。
// 可选查询参数 agentID 过滤、category 过滤。
func (s *Server) handleMiddlewareInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	_, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "middleware:read"); !ok {
		return
	}
	// MVP：返回空数组。后续可从审计/任务历史推导已部署实例。
	// 保留 query 参数解析以兼容前端调用，避免后续扩展时改签名。
	_ = r.URL.Query().Get("agentID")
	_ = r.URL.Query().Get("category")
	paginate.WriteJSON(w, http.StatusOK, []interface{}{})
}

// handleUninstallMiddlewareInstance 处理 POST /api/v1/middleware-instances/{id}/uninstall：
// 卸载已部署的中间件实例。
// 请求体: { "agentID": "...", "templateID": "...", "deployType": "docker|systemd", "params": {...}, "tenantID": "..." }
// 实现：根据 templateID 取对应 deployType 的 Uninstall 脚本，占位符替换后作为 shell task 下发，
// 复用 store.CreateTask + Audit + 事件总线 + SSE，与 deploy 同款逻辑。
// 响应: { "task": ..., "taskID": "...", "instanceID": "...", "templateID": "...", "templateName": "...", "deployType": "..." }
func (s *Server) handleUninstallMiddlewareInstance(w http.ResponseWriter, r *http.Request, instanceID string) {
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
	if _, ok := s.requireProd(w, r, "middleware:execute"); !ok {
		return
	}
	var body struct {
		AgentID    string            `json:"agentID"`
		TemplateID string            `json:"templateID"`
		DeployType string            `json:"deployType"`
		Params     map[string]string `json:"params"`
		TenantID   string            `json:"tenantID"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.AgentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}
	if body.TemplateID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "templateID is required"})
		return
	}
	if body.DeployType == "" {
		body.DeployType = "docker" // 默认 docker 部署
	}
	tpl := s.getMiddlewareTemplateByID(body.TemplateID)
	if tpl == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found: " + body.TemplateID})
		return
	}
	script, ok := tpl.Scripts[body.DeployType]
	if !ok {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "script not found for deployType: " + body.DeployType})
		return
	}
	if script.Uninstall == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "uninstall script not defined for template: " + body.TemplateID})
		return
	}
	// 填充默认参数（与 deploy 一致，确保占位符能被替换）。
	if body.Params == nil {
		body.Params = map[string]string{}
	}
	for _, p := range tpl.Params {
		if _, ok := body.Params[p.Name]; !ok && p.Default != "" {
			body.Params[p.Name] = p.Default
		}
	}
	// 类型语义校验 + shell 元字符校验：与 deploy 同规则，防卸载路径命令注入。
	if err := validateMiddlewareParams(tpl.Params, body.Params); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateShellSafeValues(body.Params); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	targetTenant := actx.TenantID
	agent := s.lookupAgent(body.AgentID)
	if agent == nil || (targetTenant != "" && agent.TenantID != targetTenant) {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "agent not found or tenant mismatch"})
		return
	}
	// 拼接最终 command：将 Uninstall 脚本中占位符替换为 params 实际值。
	command := renderMiddlewareScript(script.Uninstall, body.Params)
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
		Action:   "uninstall_middleware",
		Target:   task.TaskID,
		Detail:   sanitizeAuditDetail("instance=" + instanceID + " template=" + body.TemplateID + " deployType=" + body.DeployType + " agent=" + body.AgentID),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: targetTenant, UserID: actx.UserID,
			Action: "uninstall_middleware", Target: task.TaskID,
			Detail: sanitizeAuditDetail("instance=" + instanceID + " template=" + body.TemplateID + " deployType=" + body.DeployType + " agent=" + body.AgentID), Level: events.LevelInfo,
		})
	}
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.store.PendingDepth())
	}
	// SSE：通知前端卸载任务已创建。
	// 租户隔离：携带 targetTenant，仅同租户订阅者收到。
	// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	s.publishEvent(r.Context(), "task_status", targetTenant, map[string]string{
		"taskID":  task.TaskID,
		"status":  task.Status,
		"agentID": body.AgentID,
	})
	paginate.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"task":         task,
		"taskID":       task.TaskID,
		"instanceID":   instanceID,
		"templateID":   body.TemplateID,
		"templateName": tpl.Name,
		"deployType":   body.DeployType,
	})
}
