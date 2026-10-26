// Package controlplane: os_template_handlers.go 实现 OS 模板的 CRUD/execute HTTP handler。
//
// 从 os_optimize.go 拆分而来，仅包含 HTTP handler 函数；类型定义、常量、预置模板、
// 验证函数、store 适配分别位于 os_optimize.go / os_template_validate.go / os_template_store.go。
package controlplane

import (
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/controlplane/presets"
	"github.com/Levango7/OpsMesh/internal/events"
	"github.com/Levango7/OpsMesh/internal/proto"
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
	// 租户归属闸（P0 跨租户命令注入）：客户端可自带 ID，而 store 以 ID 为主键、
	// 写入不校验归属 ⇒ 自带一个已属他租户的 ID（如 default 租户的共享预置行）会
	// 直接覆盖对方模板，被所有租户执行。属他租户一律 404（不泄露存在性）。
	if tpl.ID != "" {
		if st := s.store.GetOSTemplate(tpl.ID); st != nil && st.TenantID != actx.TenantID {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
	}
	// 基本字段校验：risk 必须为 low/medium/high（空则归一为 low）。
	tpl.Risk = presets.NormalizeRisk(tpl.Risk)
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
	// 租户归属闸（P0 跨租户命令注入）：模板行属他租户时按 404 拒绝（不泄露存在性）。
	// store 以 ID 为主键、写入不校验归属，故此前任何持 os:write 的租户都能把恶意
	// commands 写进 default 租户的共享预置行，被所有租户执行。
	existing := s.getOSTemplateScoped(actx.TenantID, id)
	if existing == nil {
		if s.store.GetOSTemplate(id) != nil {
			// 行存在但属他租户 → 404。
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		// 回退检查：若为预置模板 ID 且尚未 seed，允许"upsert"（首次写入 store）。
		if preset := osTemplateByID(id); preset == nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
	}
	tpl.ID = id
	tpl.Risk = presets.NormalizeRisk(tpl.Risk)
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
	// 租户归属闸（P0 跨租户命令注入）：模板行属他租户时按 404 拒绝（不泄露存在性），
	// 防止任一租户删除 default 租户的共享预置行（删除后他人执行即断链）。
	existing := s.getOSTemplateScoped(actx.TenantID, id)
	if existing == nil {
		if s.store.GetOSTemplate(id) != nil {
			// 行存在但属他租户 → 404。
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
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
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "os:read"); !ok {
		return
	}
	// 租户归属闸：先按本租户取（属他租户的行与不存在的行都返回 nil）。
	t := s.getOSTemplateScoped(actx.TenantID, id)
	if t == nil {
		if st := s.store.GetOSTemplate(id); st != nil {
			// 行存在但属他租户 → 404（不泄露存在性）。
			// 例外：default 租户的**共享预置模板行**对所有租户只读可见——列表接口本就按
			// 「当前租户 + default 租户」合并展示预置模板，详情接口须保持同一语义。
			if st.TenantID != "default" || osTemplateByID(id) == nil {
				paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
				return
			}
			t = osTemplateFromStore(st)
		} else {
			// store 尚未 seed：回退到内存预置模板（与 getOSTemplateByID 同语义）。
			t = osTemplateByID(id)
		}
	}
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
	//
	// 两条分支最终都经 presets.SplitScriptCommand 拆分：
	//   - 脚本体多行（预置模板全是多行 bash）→ 脚本体进 ScriptBody 通道，
	//     Command 退化成单行调用（`sh {script}`）；
	//   - 脚本体单行 → 留在内联 Command 通道，行为与改造前一致。
	// 内联命令通道对换行是硬拦截（agent 端 checkShellMetachars），多行内容留在那里必然被拒。
	var (
		command    string
		scriptBody string
	)
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
		if err := presets.ValidateOSParams(tpl.Params, paramsMap); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// shell 元字符校验：占位符替换前拒绝含元字符的值，防命令注入。
		if err := presets.ValidateShellSafeValues(paramsMap); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		scriptBody, command = presets.SplitScriptCommand(presets.RenderOSScript(tpl.Commands, paramsMap))
	} else {
		// 旧模式：位置参数同样要过 shell 元字符校验（值会被注入脚本体）。
		if err := presets.ValidateShellSafeParamList(body.Params); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		scriptBody, command = presets.BuildOSExecuteCommand(tpl.Commands, body.Params)
	}
	// 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	// 统一走 requireTenantAgent 防线（P0-6 多租户隔离）。
	targetTenant := actx.TenantID
	if _, ok := s.requireTenantAgent(w, body.AgentID, actx.TenantID); !ok {
		return
	}
	// 下发任务：多行脚本体走 ScriptBody 通道（agent 落临时文件后以 sh <file> 执行），
	// Command 保持单行（内联通道的元字符/白名单校验对它照常生效）。

	// Quota gate: OS template execute creates a task.
	if s.quotaMgr != nil {
		if err := s.quotaMgr.CheckTask(actx.TenantID); err != nil {
			paginate.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
			return
		}
	}
	task, err := s.store.CreateTask(&proto.Task{
		AgentID:    body.AgentID,
		TenantID:   targetTenant,
		Type:       proto.TaskTypeShell,
		Command:    command,
		ScriptBody: scriptBody,
		MaxRetries: s.cfg.TaskMaxRetries,
	})
	if err != nil {
		// 缺口 3：模板任务没落库 = 脚本不会被执行，不能返回 201。
		writeInternalError(r.Context(), w, "os_template_execute.create_task", err)
		return
	}
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
