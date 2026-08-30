// ticket.go 实现 Phase 1 工单管理 HTTP handler。
//
// API 端点：
//   - GET    /api/v1/tickets           列出工单（支持 ?status=&priority=&category=&assigneeID= 查询参数）
//   - POST   /api/v1/tickets           创建工单 {title, description, priority, category, ...}
//   - GET    /api/v1/tickets/{id}      获取工单详情
//   - PUT    /api/v1/tickets/{id}      更新工单
//   - POST   /api/v1/tickets/{id}/close 关闭工单
//
// 设计要点（与 k8s_cluster.go 风格一致）：
//   - 用 s.requireTenantContext(w, r) 提取租户（复用现有方法，统一租户隔离行为）；
//   - 错误响应统一 {"error": "message"} 格式，HTTP 状态码 400/404/500；
//   - 用 decodeJSONBody 解析请求体（防 DoS 限制大小）；
//   - 鉴权：需 ticket:read/ticket:write 权限（与现有 RBAC 一致）。
package controlplane

import (
	"net/http"
	"strings"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// handleTickets 统一处理 /api/v1/tickets：
//   - GET：列出工单（支持 ?status=&priority=&category=&assigneeID= 查询参数）
//   - POST：创建工单
func (s *Server) handleTickets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListTickets(w, r)
	case http.MethodPost:
		s.handleCreateTicket(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListTickets 处理 GET /api/v1/tickets：列出工单（支持过滤参数）。
func (s *Server) handleListTickets(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "ticket:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	// 解析过滤参数（空串表示不过滤）。
	filter := store.TicketFilter{
		Status:     r.URL.Query().Get("status"),
		Priority:   r.URL.Query().Get("priority"),
		Category:   r.URL.Query().Get("category"),
		AssigneeID: r.URL.Query().Get("assigneeID"),
	}
	tickets := s.store.ListTickets(actx.TenantID, filter)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"tickets": tickets})
}

// handleCreateTicket 处理 POST /api/v1/tickets：创建工单。
// 请求体：{title, description, priority, category, assigneeID, creatorID,
// relatedDevice, relatedTask, tags}；title 必填。
func (s *Server) handleCreateTicket(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "ticket:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	var body struct {
		Title         string   `json:"title"`
		Description   string   `json:"description"`
		Priority      string   `json:"priority"`
		Category      string   `json:"category"`
		AssigneeID    string   `json:"assigneeID"`
		CreatorID     string   `json:"creatorID"`
		RelatedDevice string   `json:"relatedDevice"`
		RelatedTask   string   `json:"relatedTask"`
		Tags          []string `json:"tags"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Title == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
		return
	}
	// CreatorID 默认填充为当前调用者（防伪造创建人）。
	creatorID := body.CreatorID
	if creatorID == "" {
		creatorID = caller.ID
	}
	t := &store.Ticket{
		Title:         body.Title,
		Description:   body.Description,
		Priority:      body.Priority,
		Category:      body.Category,
		AssigneeID:    body.AssigneeID,
		CreatorID:     creatorID,
		RelatedDevice: body.RelatedDevice,
		RelatedTask:   body.RelatedTask,
		Tags:          body.Tags,
	}
	created := s.store.CreateTicket(actx.TenantID, t)
	if created == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "create ticket failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "ticket_create", Target: created.ID, Detail: sanitizeAuditDetail("title=" + created.Title),
	})
	paginate.WriteJSON(w, http.StatusCreated, created)
}

// handleTicketRouting 分派 /api/v1/tickets/{id} 子路径：
//   - GET    /api/v1/tickets/{id}       获取工单详情
//   - PUT    /api/v1/tickets/{id}       更新工单
//   - POST   /api/v1/tickets/{id}/close 关闭工单
func (s *Server) handleTicketRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/tickets/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "ticket id required"})
		return
	}
	// 按 / 切分：[id] / [id, action]。
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "ticket id required"})
		return
	}
	// 仅 /{id}：工单本身管理（GET/PUT）。
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetTicket(w, r, id)
		case http.MethodPut:
			s.handleUpdateTicket(w, r, id)
		default:
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	// /{id}/{action}。
	action := parts[1]
	switch action {
	case "close":
		s.handleCloseTicket(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetTicket 处理 GET /api/v1/tickets/{id}：获取工单详情。
func (s *Server) handleGetTicket(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "ticket:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	t, ok := s.store.GetTicket(actx.TenantID, id)
	if !ok || t == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, t)
}

// handleUpdateTicket 处理 PUT /api/v1/tickets/{id}：更新工单。
// 请求体：{title, description, status, priority, category, assigneeID,
// relatedDevice, relatedTask, tags}。
func (s *Server) handleUpdateTicket(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "ticket:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	var body struct {
		Title         string   `json:"title"`
		Description   string   `json:"description"`
		Status        string   `json:"status"`
		Priority      string   `json:"priority"`
		Category      string   `json:"category"`
		AssigneeID    string   `json:"assigneeID"`
		RelatedDevice string   `json:"relatedDevice"`
		RelatedTask   string   `json:"relatedTask"`
		Tags          []string `json:"tags"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	t := &store.Ticket{
		ID:            id,
		Title:         body.Title,
		Description:   body.Description,
		Status:        body.Status,
		Priority:      body.Priority,
		Category:      body.Category,
		AssigneeID:    body.AssigneeID,
		RelatedDevice: body.RelatedDevice,
		RelatedTask:   body.RelatedTask,
		Tags:          body.Tags,
	}
	updated, ok := s.store.UpdateTicket(actx.TenantID, t)
	if !ok || updated == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "ticket_update", Target: id, Detail: sanitizeAuditDetail("title=" + updated.Title),
	})
	paginate.WriteJSON(w, http.StatusOK, updated)
}

// handleCloseTicket 处理 POST /api/v1/tickets/{id}/close：关闭工单。
func (s *Server) handleCloseTicket(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "ticket:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	closed, ok := s.store.CloseTicket(actx.TenantID, id)
	if !ok || closed == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "ticket_close", Target: id, Detail: sanitizeAuditDetail("title=" + closed.Title),
	})
	paginate.WriteJSON(w, http.StatusOK, closed)
}
