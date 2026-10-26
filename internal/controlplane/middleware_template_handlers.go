// Package controlplane: middleware_template_handlers.go 实现中间件模板 CRUD HTTP handler。
//
// 从 middleware_deploy.go 拆分而来，包含模板的 list/create/update/delete/detail handler
// 及 handleMiddlewareTemplateDetail 路由分派。
package controlplane

import (
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/controlplane/presets"
	"github.com/Levango7/OpsMesh/internal/events"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// handleMiddlewareTemplates 处理 /api/v1/middleware-templates：
//   - GET：列出所有模板（从 store 读取，store 为空回退预置；可选 category/risk 过滤）
//   - POST：创建新模板（CRUD，需 middleware:write 权限）
//
// 该路由注册在精确路径 /api/v1/middleware-templates（无尾斜杠）。
func (s *Server) handleMiddlewareTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListMiddlewareTemplatesGet(w, r)
	case http.MethodPost:
		s.handleCreateMiddlewareTemplate(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListMiddlewareTemplatesGet 处理 GET /api/v1/middleware-templates：列出所有模板（从 store 读取，含回退）。
// 可选查询参数 category 过滤、risk 过滤。
func (s *Server) handleListMiddlewareTemplatesGet(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "middleware:read"); !ok {
		return
	}
	q := r.URL.Query()
	category := q.Get("category")
	risk := q.Get("risk")
	all := s.listMiddlewareTemplatesFromStore(actx.TenantID)
	out := make([]MiddlewareTemplate, 0, len(all))
	for _, t := range all {
		if category != "" && t.Category != category {
			continue
		}
		if risk != "" && t.Risk != risk {
			continue
		}
		out = append(out, t)
	}
	paginate.WriteJSON(w, http.StatusOK, out)
}

// handleCreateMiddlewareTemplate 处理 POST /api/v1/middleware-templates：创建新中间件模板（CRUD）。
// 请求体即 MiddlewareTemplate JSON；ID 为空时由 store 分配随机 ID。
// 需 middleware:write 权限；创建后审计 + 事件总线 + SSE 通知。
func (s *Server) handleCreateMiddlewareTemplate(w http.ResponseWriter, r *http.Request) {
	if err := s.verifyFederationRequest(r); err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	caller, ok := s.requireProd(w, r, "middleware:write")
	if !ok {
		return
	}
	var tpl MiddlewareTemplate
	if err := decodeJSONBody(w, r, &tpl); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if tpl.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if len(tpl.Scripts) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "scripts is required"})
		return
	}
	// 租户归属闸（P0 跨租户命令注入）：客户端可自带 ID，而 store 以 ID 为主键、
	// 写入不校验归属 ⇒ 自带一个已属他租户的 ID（如 default 租户的共享预置行 mysql）
	// 会直接覆盖对方模板，被所有租户部署执行。属他租户一律 404（不泄露存在性）。
	if tpl.ID != "" {
		if st := s.store.GetMiddlewareTemplate(tpl.ID); st != nil && st.TenantID != actx.TenantID {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
	}
	tpl.Risk = presets.NormalizeRisk(tpl.Risk)
	st := middlewareTemplateToStore(&tpl, actx.TenantID)
	if err := s.store.SaveMiddlewareTemplate(st); err != nil {
		writeInternalError(r.Context(), w, "middleware.saveTemplate", err)
		return
	}
	saved := middlewareTemplateFromStore(s.store.GetMiddlewareTemplate(st.ID))
	userID := ""
	if caller != nil {
		userID = caller.ID
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: userID, Action: "mw_template_create", Target: st.ID, Detail: sanitizeAuditDetail("name=" + tpl.Name),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: userID,
			Action: "mw_template_create", Target: st.ID, Detail: sanitizeAuditDetail("name=" + tpl.Name), Level: events.LevelInfo,
		})
	}
	s.publishEvent(r.Context(), "mw_template_changed", actx.TenantID, map[string]string{"templateID": st.ID, "action": "create"})
	paginate.WriteJSON(w, http.StatusCreated, saved)
}

// handleUpdateMiddlewareTemplate 处理 PUT /api/v1/middleware-templates/{id}：更新中间件模板（CRUD）。
// 需 middleware:write 权限；不存在返回 404。
func (s *Server) handleUpdateMiddlewareTemplate(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.verifyFederationRequest(r); err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	caller, ok := s.requireProd(w, r, "middleware:write")
	if !ok {
		return
	}
	var tpl MiddlewareTemplate
	if err := decodeJSONBody(w, r, &tpl); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if tpl.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if len(tpl.Scripts) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "scripts is required"})
		return
	}
	// 租户归属闸（P0 跨租户命令注入）：模板行属他租户时按 404 拒绝（不泄露存在性）。
	// store 以 ID 为主键、写入不校验归属，故此前任何持 middleware:write 的租户都能
	// 把恶意 scripts 写进 default 租户的共享预置行（如 mysql），被所有租户部署执行。
	existing := s.getMiddlewareTemplateScoped(actx.TenantID, id)
	if existing == nil {
		if s.store.GetMiddlewareTemplate(id) != nil {
			// 行存在但属他租户 → 404。
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		// 回退检查：若为预置模板 ID 且尚未 seed，允许 upsert。
		if preset := middlewareTemplateByID(id); preset == nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
	}
	tpl.ID = id
	tpl.Risk = presets.NormalizeRisk(tpl.Risk)
	st := middlewareTemplateToStore(&tpl, actx.TenantID)
	if err := s.store.SaveMiddlewareTemplate(st); err != nil {
		writeInternalError(r.Context(), w, "middleware.saveTemplate", err)
		return
	}
	saved := middlewareTemplateFromStore(s.store.GetMiddlewareTemplate(id))
	userID := ""
	if caller != nil {
		userID = caller.ID
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: userID, Action: "mw_template_update", Target: id, Detail: sanitizeAuditDetail("name=" + tpl.Name),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: userID,
			Action: "mw_template_update", Target: id, Detail: sanitizeAuditDetail("name=" + tpl.Name), Level: events.LevelInfo,
		})
	}
	s.publishEvent(r.Context(), "mw_template_changed", actx.TenantID, map[string]string{"templateID": id, "action": "update"})
	paginate.WriteJSON(w, http.StatusOK, saved)
}

// handleDeleteMiddlewareTemplate 处理 DELETE /api/v1/middleware-templates/{id}：删除中间件模板（CRUD）。
// 需 middleware:write 权限；不存在返回 404；删除成功返回 204。
func (s *Server) handleDeleteMiddlewareTemplate(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	caller, ok := s.requireProd(w, r, "middleware:write")
	if !ok {
		return
	}
	// 租户归属闸（P0 跨租户命令注入）：模板行属他租户时按 404 拒绝（不泄露存在性），
	// 防止任一租户删除 default 租户的共享预置行（删除后他人部署即断链）。
	existing := s.getMiddlewareTemplateScoped(actx.TenantID, id)
	if existing == nil {
		if s.store.GetMiddlewareTemplate(id) != nil {
			// 行存在但属他租户 → 404。
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		if middlewareTemplateByID(id) == nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		// 预置模板未 seed，store 中本就不存在，直接返回 204。
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.store.DeleteMiddlewareTemplate(id) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	userID := ""
	if caller != nil {
		userID = caller.ID
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: userID, Action: "mw_template_delete", Target: id, Detail: sanitizeAuditDetail("name=" + existing.Name),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: userID,
			Action: "mw_template_delete", Target: id, Detail: sanitizeAuditDetail("name=" + existing.Name), Level: events.LevelInfo,
		})
	}
	s.publishEvent(r.Context(), "mw_template_changed", actx.TenantID, map[string]string{"templateID": id, "action": "delete"})
	w.WriteHeader(http.StatusNoContent)
}

// handleMiddlewareTemplateByID 处理 GET /api/v1/middleware-templates/{id}：返回模板详情（从 store 读取，含回退）。
func (s *Server) handleMiddlewareTemplateByID(w http.ResponseWriter, r *http.Request, id string) {
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
	// 租户归属闸：先按本租户取（属他租户的行与不存在的行都返回 nil）。
	t := s.getMiddlewareTemplateScoped(actx.TenantID, id)
	if t == nil {
		if st := s.store.GetMiddlewareTemplate(id); st != nil {
			// 行存在但属他租户 → 404（不泄露存在性）。
			// 例外：default 租户的**共享预置模板行**对所有租户只读可见——列表接口本就按
			// 「当前租户 + default 租户」合并展示预置模板，详情接口须保持同一语义，
			// 否则列表里点开任一预置模板必然 404（前端 middleware.js 依赖该详情接口）。
			if st.TenantID != "default" || middlewareTemplateByID(id) == nil {
				paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
				return
			}
			t = middlewareTemplateFromStore(st)
		} else {
			// store 尚未 seed：回退到内存预置模板（与 getMiddlewareTemplateByID 同语义）。
			t = middlewareTemplateByID(id)
		}
	}
	if t == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, t)
}

// handleDeployMiddlewareTemplate 处理 POST /api/v1/middleware-templates/{id}/deploy：
// 在指定 agent 上部署中间件。
// 请求体: { "agentID": "...", "deployType": "docker|systemd", "params": {"name":"...","port":"..."}, "tenantID": "..." }
// 实现：根据 deployType 取对应脚本，将 params 替换占位符后作为 shell task 下发，复用 store.CreateTask。

// handleMiddlewareTemplateDetail 统一分派 /api/v1/middleware-templates/{id}... 子路径：
//   - GET    /api/v1/middleware-templates/{id}：模板详情
//   - PUT    /api/v1/middleware-templates/{id}：更新模板（CRUD）
//   - DELETE /api/v1/middleware-templates/{id}：删除模板（CRUD）
//   - POST   /api/v1/middleware-templates/{id}/deploy：在指定 agent 上部署
//
// 注意：/api/v1/middleware-templates（无尾斜杠）由 handleMiddlewareTemplates 处理；
// /api/v1/middleware-templates/（带尾斜杠但无 id）此处转给 list handler 兜底。
func (s *Server) handleMiddlewareTemplateDetail(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/middleware-templates/")
	if idAndRest == "" {
		// 兜底：/api/v1/middleware-templates/（带尾斜杠）转给 list handler 处理 GET/POST。
		s.handleMiddlewareTemplates(w, r)
		return
	}
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "template id required"})
		return
	}
	switch {
	case len(parts) == 1:
		// /api/v1/middleware-templates/{id}
		switch r.Method {
		case http.MethodGet:
			s.handleMiddlewareTemplateByID(w, r, id)
		case http.MethodPut:
			s.handleUpdateMiddlewareTemplate(w, r, id)
		case http.MethodDelete:
			s.handleDeleteMiddlewareTemplate(w, r, id)
		default:
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	case len(parts) == 2 && parts[1] == "deploy":
		// POST /api/v1/middleware-templates/{id}/deploy
		s.handleDeployMiddlewareTemplate(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}
