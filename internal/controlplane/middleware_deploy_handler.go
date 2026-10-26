// Package controlplane: middleware_deploy_handler.go 实现中间件部署/卸载 HTTP handler。
//
// 从 middleware_deploy.go 拆分而来，包含 handleDeployMiddlewareTemplate（部署）、
// handleMiddlewareInstances（实例列表）、handleUninstallMiddlewareInstance（卸载）。
package controlplane

import (
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/controlplane/presets"
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
		Version    string            `json:"version"`
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
	// 版本选择：请求体 version（或 params.version）决定实际使用的镜像与脚本；
	// 未指定时回落到模板默认版本（Versions[0]/Default）。
	selectedVersion := selectedMiddlewareVersion(body.Version, body.Params)
	if v := resolveMiddlewareVersion(tpl, selectedVersion); v != nil {
		selectedVersion = v.Version
	}
	script, ok := resolveMiddlewareScript(tpl, body.DeployType, selectedVersion)
	if !ok {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "script not found for deployType: " + body.DeployType})
		return
	}
	tplParams := resolveMiddlewareParams(tpl, selectedVersion)
	resolvedVersion := selectedVersion
	// 校验必填参数。
	for _, p := range tplParams {
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
	for _, p := range tplParams {
		if _, ok := body.Params[p.Name]; !ok && p.Default != "" {
			if body.Params == nil {
				body.Params = map[string]string{}
			}
			body.Params[p.Name] = p.Default
		}
	}
	// 参数类型与语义验证（端口范围/路径/非空）。
	if err := validateMiddlewareParams(tplParams, body.Params); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// shell 元字符校验：占位符替换前拒绝含元字符的值，防命令注入。
	if err := presets.ValidateShellSafeValues(body.Params); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	// 统一走 requireTenantAgent 防线（P0-6 多租户隔离）。
	targetTenant := actx.TenantID
	if _, ok := s.requireTenantAgent(w, body.AgentID, actx.TenantID); !ok {
		return
	}
	// 拼接最终 command：将脚本中 {name}/{port}/... 占位符替换为 params 实际值。
	// 多行脚本（如 mongodb 的 systemd 部署脚本含 heredoc）不能留在内联命令通道——
	// agent 端对内联命令的换行是硬拦截（"ls\nrm -rf /" 拼接注入防护），
	// 留在那里 100% 被拒。故拆成两段：脚本体进 ScriptBody 通道（agent 落临时文件
	// 后以 `sh <file>` 执行），Command 退化成单行调用（`sh {script}`）。
	// Quota gate: middleware deploy creates a task.
	if s.quotaMgr != nil {
		if err := s.quotaMgr.CheckTask(actx.TenantID); err != nil {
			paginate.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
			return
		}
	}
	rendered := renderMiddlewareScript(script.Deploy, body.Params)
	scriptBody, command := presets.SplitScriptCommand(rendered)
	// instance key：复合键 <templateID>@<agentID>:<deployType>:<version>
	instKey := instanceKey(id, body.AgentID, body.DeployType, resolvedVersion)
	task, err := s.store.CreateTask(&proto.Task{
		AgentID:    body.AgentID,
		TenantID:   targetTenant,
		Type:       proto.TaskTypeShell,
		Command:    command,
		ScriptBody: scriptBody,
		MaxRetries: s.cfg.TaskMaxRetries,
	})
	if err != nil {
		// 缺口 3：中间件部署任务没落库 = 什么都没装，不能返回 201 让前端以为装上了。
		writeInternalError(r.Context(), w, "middleware_deploy.create_task", err)
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: targetTenant,
		UserID:   actx.UserID,
		Action:   "deploy_middleware",
		Target:   task.TaskID,
		Detail:   sanitizeAuditDetail("template=" + id + " deployType=" + body.DeployType + " agent=" + body.AgentID + " version=" + resolvedVersion + " instance=" + instKey),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: targetTenant, UserID: actx.UserID,
			Action: "deploy_middleware", Target: task.TaskID,
			Detail: sanitizeAuditDetail("template=" + id + " deployType=" + body.DeployType + " agent=" + body.AgentID + " version=" + resolvedVersion + " instance=" + instKey), Level: events.LevelInfo,
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
		"version":      resolvedVersion,
		"instanceID":   instKey,
	})
}

// handleMiddlewareInstances 处理 GET /api/v1/middleware-instances：查询已部署中间件实例。
// 从审计日志推导实例清单（action=deploy_middleware / uninstall_middleware）。
func (s *Server) handleMiddlewareInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "middleware:read"); !ok {
		return
	}

	q := r.URL.Query()
	agentID := q.Get("agentID")
	category := q.Get("category")
	templateID := q.Get("templateID")
	includeRemoved := q.Get("includeRemoved") == "1"

	instances := s.deriveMiddlewareInstances(actx.TenantID, agentID, category, templateID, includeRemoved)
	paginate.WriteJSON(w, http.StatusOK, instances)
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
		Version    string            `json:"version"`
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
	// 实例复合键：优先使用 URL 路径中的 instance id，缺失时按复合键推导。
	instKey := instanceID
	if instKey == "" {
		instKey = instanceKey(body.TemplateID, body.AgentID, body.DeployType, resolveMiddlewareVersionStr(tpl, body.Version))
	}
	// 版本选择：请求体 version（或 params.version）决定实际使用的镜像与脚本。
	selectedVersion := selectedMiddlewareVersion(body.Version, body.Params)
	if v := resolveMiddlewareVersion(tpl, selectedVersion); v != nil {
		selectedVersion = v.Version
	}
	resolvedVersion := selectedVersion
	script, ok := resolveMiddlewareScript(tpl, body.DeployType, selectedVersion)
	if !ok {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "script not found for deployType: " + body.DeployType})
		return
	}
	if script.Uninstall == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "uninstall script not defined for template: " + body.TemplateID})
		return
	}
	tplParams := resolveMiddlewareParams(tpl, selectedVersion)
	// 填充默认参数（与 deploy 一致，确保占位符能被替换）。
	if body.Params == nil {
		body.Params = map[string]string{}
	}
	for _, p := range tplParams {
		if _, ok := body.Params[p.Name]; !ok && p.Default != "" {
			body.Params[p.Name] = p.Default
		}
	}
	// 类型语义校验 + shell 元字符校验：与 deploy 同规则，防卸载路径命令注入。
	if err := validateMiddlewareParams(tplParams, body.Params); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := presets.ValidateShellSafeValues(body.Params); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	// 统一走 requireTenantAgent 防线（P0-6 多租户隔离）。
	targetTenant := actx.TenantID
	if _, ok := s.requireTenantAgent(w, body.AgentID, actx.TenantID); !ok {
		return
	}
	// 拼接最终 command：将 Uninstall 脚本中占位符替换为 params 实际值。
	// 与 deploy 同款拆分：多行脚本走 ScriptBody 通道，Command 保持单行。
	// Quota gate: middleware uninstall creates a task.
	if s.quotaMgr != nil {
		if err := s.quotaMgr.CheckTask(actx.TenantID); err != nil {
			paginate.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
			return
		}
	}
	rendered := renderMiddlewareScript(script.Uninstall, body.Params)
	scriptBody, command := presets.SplitScriptCommand(rendered)
	task, err := s.store.CreateTask(&proto.Task{
		AgentID:    body.AgentID,
		TenantID:   targetTenant,
		Type:       proto.TaskTypeShell,
		Command:    command,
		ScriptBody: scriptBody,
		MaxRetries: s.cfg.TaskMaxRetries,
	})
	if err != nil {
		// 缺口 3：卸载任务没落库 = 中间件还装着，不能返回 201。
		writeInternalError(r.Context(), w, "middleware_uninstall.create_task", err)
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: targetTenant,
		UserID:   actx.UserID,
		Action:   "uninstall_middleware",
		Target:   task.TaskID,
		Detail:   sanitizeAuditDetail("instance=" + instKey + " template=" + body.TemplateID + " deployType=" + body.DeployType + " agent=" + body.AgentID + " version=" + resolvedVersion),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: targetTenant, UserID: actx.UserID,
			Action: "uninstall_middleware", Target: task.TaskID,
			Detail: sanitizeAuditDetail("instance=" + instKey + " template=" + body.TemplateID + " deployType=" + body.DeployType + " agent=" + body.AgentID + " version=" + resolvedVersion), Level: events.LevelInfo,
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
		"instanceID":   instKey,
		"templateID":   body.TemplateID,
		"templateName": tpl.Name,
		"deployType":   body.DeployType,
		"version":      resolvedVersion,
	})
}
