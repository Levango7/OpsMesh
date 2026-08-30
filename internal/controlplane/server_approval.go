// server_approval.go 实现 M5 审批 API：
//   - GET    /api/v1/approval/flows             列表审批流
//   - POST   /api/v1/approval/flows             创建审批流
//   - PUT    /api/v1/approval/flows/{id}        更新审批流
//   - DELETE /api/v1/approval/flows/{id}        删除审批流
//   - GET    /api/v1/approval/requests          列表审批请求（?status=pending）
//   - POST   /api/v1/approval/requests          提交审批请求
//   - GET    /api/v1/approval/requests/{id}     审批请求详情
//   - POST   /api/v1/approval/requests/{id}/approve 审批通过
//   - POST   /api/v1/approval/requests/{id}/reject  审批拒绝
//   - POST   /api/v1/approval/requests/{id}/cancel  取消审批
//   - GET    /api/v1/approval/pending           待我审批列表
//   - GET    /api/v1/approval/requests/{id}/history 审批历史
//
// 依赖 internal/approval 包（Engine/Flow/Request/History）。
// Server 持有 *approval.Engine 实例（在 NewServer 中构造）。
package controlplane

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/approval"
	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

// ============================================================================
// 审批流 CRUD
// ============================================================================

// handleApprovalFlows 处理 /api/v1/approval/flows（GET 列表 / POST 创建）。
func (s *Server) handleApprovalFlows(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.approvalListFlows(w, r)
	case http.MethodPost:
		s.approvalCreateFlow(w, r)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// approvalListFlows GET /api/v1/approval/flows：列表审批流（按租户过滤）。
func (s *Server) approvalListFlows(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "approval:read"); !ok {
		return
	}
	flows := s.approvalEngine.ListFlows(actx.TenantID)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"flows": flows,
		"total": len(flows),
	})
}

// approvalCreateFlow POST /api/v1/approval/flows：创建审批流。
func (s *Server) approvalCreateFlow(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "approval:write"); !ok {
		return
	}
	var f approval.ApprovalFlow
	if err := decodeJSONBody(w, r, &f); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if f.TenantID == "" {
		f.TenantID = actx.TenantID
	}
	if f.ID == "" {
		f.ID = genBatchID("flow")
	}
	if err := s.approvalEngine.CreateFlow(&f); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "approval_flow_create", Target: f.ID,
		Detail: "create approval flow",
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: actx.UserID,
			Action: "approval_flow_create", Target: f.ID, Level: events.LevelInfo,
		})
	}
	paginate.WriteJSON(w, http.StatusCreated, &f)
}

// handleApprovalFlowRouting 处理 /api/v1/approval/flows/{id}（GET/PUT/DELETE）。
func (s *Server) handleApprovalFlowRouting(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/approval/flows/")
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "flow id required"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.approvalGetFlow(w, r, id)
	case http.MethodPut:
		s.approvalUpdateFlow(w, r, id)
	case http.MethodDelete:
		s.approvalDeleteFlow(w, r, id)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) approvalGetFlow(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "approval:read"); !ok {
		return
	}
	f, err := s.approvalEngine.GetFlow(id)
	if err != nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if actx.TenantID != "" && f.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, f)
}

func (s *Server) approvalUpdateFlow(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "approval:write"); !ok {
		return
	}
	var f approval.ApprovalFlow
	if err := decodeJSONBody(w, r, &f); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	f.ID = id
	if f.TenantID == "" {
		f.TenantID = actx.TenantID
	}
	if err := s.approvalEngine.UpdateFlow(&f); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "approval_flow_update", Target: id,
		Detail: "update approval flow",
	})
	paginate.WriteJSON(w, http.StatusOK, &f)
}

func (s *Server) approvalDeleteFlow(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "approval:write"); !ok {
		return
	}
	if err := s.approvalEngine.DeleteFlow(id); err != nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "approval_flow_delete", Target: id,
		Detail: "delete approval flow",
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: actx.UserID,
			Action: "approval_flow_delete", Target: id, Level: events.LevelInfo,
		})
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

// ============================================================================
// 审批请求生命周期
// ============================================================================

// handleApprovalRequests 处理 /api/v1/approval/requests（GET 列表 / POST 提交）。
func (s *Server) handleApprovalRequests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.approvalListRequests(w, r)
	case http.MethodPost:
		s.approvalSubmitRequest(w, r)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) approvalListRequests(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "approval:read"); !ok {
		return
	}
	status := r.URL.Query().Get("status")
	reqs := s.approvalEngine.ListRequests(actx.TenantID, status)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"requests": reqs,
		"total":    len(reqs),
	})
}

func (s *Server) approvalSubmitRequest(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "approval:write"); !ok {
		return
	}
	var req approval.ApprovalRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.TenantID == "" {
		req.TenantID = actx.TenantID
	}
	if req.Operator == "" {
		req.Operator = actx.UserID
	}
	if req.ID == "" {
		req.ID = genBatchID("apr")
	}
	if req.Status == "" {
		req.Status = approval.StatusPending
	}
	if err := s.approvalEngine.Submit(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "approval_submit", Target: req.ID,
		Detail: sanitizeAuditDetail("submit approval: " + req.TriggerType + " " + req.Target),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: actx.UserID,
			Action: "approval_submit", Target: req.ID, Level: events.LevelInfo,
			Detail: sanitizeAuditDetail("approval: " + req.TriggerType),
		})
	}
	paginate.WriteJSON(w, http.StatusCreated, &req)
}

// handleApprovalRequestRouting 处理 /api/v1/approval/requests/{id}[/approve|/reject|/cancel|/history]。
func (s *Server) handleApprovalRequestRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/approval/requests/")
	if idAndRest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "request id required"})
		return
	}
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if len(parts) == 1 {
		// GET /api/v1/approval/requests/{id}
		if r.Method != http.MethodGet {
			paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.approvalGetRequest(w, r, id)
		return
	}
	switch parts[1] {
	case "approve":
		if r.Method != http.MethodPost {
			paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.approvalApproveRequest(w, r, id)
	case "reject":
		if r.Method != http.MethodPost {
			paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.approvalRejectRequest(w, r, id)
	case "cancel":
		if r.Method != http.MethodPost {
			paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.approvalCancelRequest(w, r, id)
	case "history":
		if r.Method != http.MethodGet {
			paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.approvalGetHistory(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found", "path": r.URL.Path})
	}
}

func (s *Server) approvalGetRequest(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "approval:read"); !ok {
		return
	}
	req, err := s.approvalEngine.GetRequest(id)
	if err != nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if actx.TenantID != "" && req.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, req)
}

func (s *Server) approvalApproveRequest(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "approval:approve"); !ok {
		return
	}
	var body struct {
		Comment string `json:"comment"`
	}
	// comment 为可选字段：空 body 视为空注释；非法 JSON 返回 400。
	if derr := decodeJSONBody(w, r, &body); derr != nil && !errors.Is(derr, io.EOF) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if err := s.approvalEngine.Approve(id, actx.UserID, body.Comment); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "approval_approve", Target: id,
		Detail: sanitizeAuditDetail("approve: " + body.Comment),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: actx.UserID,
			Action: "approval_approve", Target: id, Level: events.LevelInfo,
		})
	}
	s.publishEvent(r.Context(), "approval_status", actx.TenantID, map[string]string{
		"requestID": id, "status": "approved",
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "approved", "id": id})
}

func (s *Server) approvalRejectRequest(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "approval:approve"); !ok {
		return
	}
	var body struct {
		Comment string `json:"comment"`
	}
	// comment 为可选字段：空 body 视为空注释；非法 JSON 返回 400。
	if derr := decodeJSONBody(w, r, &body); derr != nil && !errors.Is(derr, io.EOF) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if err := s.approvalEngine.Reject(id, actx.UserID, body.Comment); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "approval_reject", Target: id,
		Detail: sanitizeAuditDetail("reject: " + body.Comment),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: actx.UserID,
			Action: "approval_reject", Target: id, Level: events.LevelInfo,
		})
	}
	s.publishEvent(r.Context(), "approval_status", actx.TenantID, map[string]string{
		"requestID": id, "status": "rejected",
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "rejected", "id": id})
}

func (s *Server) approvalCancelRequest(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "approval:write"); !ok {
		return
	}
	if err := s.approvalEngine.Cancel(id, actx.UserID); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "approval_cancel", Target: id,
		Detail: "cancel approval request",
	})
	s.publishEvent(r.Context(), "approval_status", actx.TenantID, map[string]string{
		"requestID": id, "status": "cancelled",
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "cancelled", "id": id})
}

func (s *Server) approvalGetHistory(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "approval:read"); !ok {
		return
	}
	h, err := s.approvalEngine.GetHistory(id)
	if err != nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	// 租户隔离：通过请求归属校验（GetHistory 已校验请求存在）。
	if actx.TenantID != "" {
		if req, e2 := s.approvalEngine.GetRequest(id); e2 == nil && req.TenantID != actx.TenantID {
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
			return
		}
	}
	paginate.WriteJSON(w, http.StatusOK, h)
}

// ============================================================================
// 待我审批列表
// ============================================================================

// handleApprovalPending 处理 GET /api/v1/approval/pending：待我审批列表。
func (s *Server) handleApprovalPending(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "approval:read"); !ok {
		return
	}
	pending := s.approvalEngine.ListPendingApprovals(actx.UserID)
	// 按租户过滤（ListPendingApprovals 已按用户过滤，但跨租户场景下再过滤一次）。
	if actx.TenantID != "" {
		filtered := make([]*approval.ApprovalRequest, 0, len(pending))
		for _, p := range pending {
			if p.TenantID == actx.TenantID {
				filtered = append(filtered, p)
			}
		}
		pending = filtered
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"pending": pending,
		"total":   len(pending),
	})
}

// suppress unused import warnings (json used in case future expansion).
var _ = json.Marshal
var _ = time.Now
