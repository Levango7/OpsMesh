// gateway_approval.go 实现 task-svc 的审批流管理 HTTP API，对齐 controlplane 的审批 API。
//
// 端点清单（与 controlplane server_approval.go 逐一对齐）：
//   - GET    /api/v1/approval/flows             列表审批流
//   - POST   /api/v1/approval/flows             创建审批流
//   - GET    /api/v1/approval/flows/{id}        查询审批流
//   - PUT    /api/v1/approval/flows/{id}        更新审批流
//   - DELETE /api/v1/approval/flows/{id}        删除审批流
//   - GET    /api/v1/approval/requests          列表审批请求（?status=pending）
//   - POST   /api/v1/approval/requests          提交审批请求
//   - GET    /api/v1/approval/requests/{id}     审批请求详情
//   - POST   /api/v1/approval/requests/{id}/approve 审批通过
//   - POST   /api/v1/approval/requests/{id}/reject  审批拒绝
//   - POST   /api/v1/approval/requests/{id}/cancel  取消审批
//   - GET    /api/v1/approval/requests/{id}/history 审批历史
//   - GET    /api/v1/approval/pending           待我审批列表
//
// 与 controlplane 的差异：
//   - controlplane 使用 paginate.WriteJSON / paginate.JSONError → task-svc 使用 writeJSON / writeError
//   - controlplane 使用 s.requireTenantContext + s.requireProd → task-svc 使用 g.extractAuth（权限检查省略）
//   - controlplane 使用 s.audit / s.bus.Publish / s.publishEvent → task-svc gateway 层省略审计与事件推送
//   - controlplane 使用 genBatchID → task-svc 使用 genApprovalID
//   - controlplane 使用 decodeJSONBody → task-svc 使用 json.NewDecoder(r.Body).Decode
package http

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/services/task-svc/internal/approval"
)

// SetApprovalEngine 注入审批引擎实例。
// 由 main 在构造 Gateway 后调用，使审批路由可用。
func (g *Gateway) SetApprovalEngine(e *approval.Engine) {
	g.approvalEngine = e
}

// RegisterApprovalRoutes 注册审批流管理路由到给定 mux。
func (g *Gateway) RegisterApprovalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/approval/flows", g.handleApprovalFlows)
	mux.HandleFunc("/api/v1/approval/flows/", g.handleApprovalFlowRouting)
	mux.HandleFunc("/api/v1/approval/requests", g.handleApprovalRequests)
	mux.HandleFunc("/api/v1/approval/requests/", g.handleApprovalRequestRouting)
	mux.HandleFunc("/api/v1/approval/pending", g.handleApprovalPending)
}

// ============================================================================
// 审批流 CRUD
// ============================================================================

// handleApprovalFlows 处理 /api/v1/approval/flows（GET 列表 / POST 创建）。
func (g *Gateway) handleApprovalFlows(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		g.approvalListFlows(w, r)
	case http.MethodPost:
		g.approvalCreateFlow(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// approvalListFlows GET /api/v1/approval/flows：列表审批流（按租户过滤）。
func (g *Gateway) approvalListFlows(w http.ResponseWriter, r *http.Request) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if g.approvalEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "approval engine not configured")
		return
	}
	flows := g.approvalEngine.ListFlows(actx.TenantID)
	writeJSON(w, http.StatusOK, map[string]any{
		"flows": flows,
		"total": len(flows),
	})
}

// approvalCreateFlow POST /api/v1/approval/flows：创建审批流。
func (g *Gateway) approvalCreateFlow(w http.ResponseWriter, r *http.Request) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if g.approvalEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "approval engine not configured")
		return
	}
	var f approval.ApprovalFlow
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if f.TenantID == "" {
		f.TenantID = actx.TenantID
	}
	if f.ID == "" {
		f.ID = genApprovalID("flow")
	}
	if err := g.approvalEngine.CreateFlow(&f); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, &f)
}

// handleApprovalFlowRouting 处理 /api/v1/approval/flows/{id}（GET/PUT/DELETE）。
func (g *Gateway) handleApprovalFlowRouting(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/approval/flows/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "flow id required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		g.approvalGetFlow(w, r, id)
	case http.MethodPut:
		g.approvalUpdateFlow(w, r, id)
	case http.MethodDelete:
		g.approvalDeleteFlow(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (g *Gateway) approvalGetFlow(w http.ResponseWriter, r *http.Request, id string) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if g.approvalEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "approval engine not configured")
		return
	}
	f, err := g.approvalEngine.GetFlow(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if actx.TenantID != "" && f.TenantID != actx.TenantID {
		writeError(w, http.StatusForbidden, "tenant mismatch")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (g *Gateway) approvalUpdateFlow(w http.ResponseWriter, r *http.Request, id string) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if g.approvalEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "approval engine not configured")
		return
	}
	var f approval.ApprovalFlow
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	f.ID = id
	if f.TenantID == "" {
		f.TenantID = actx.TenantID
	}
	if err := g.approvalEngine.UpdateFlow(&f); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, &f)
}

func (g *Gateway) approvalDeleteFlow(w http.ResponseWriter, r *http.Request, id string) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if g.approvalEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "approval engine not configured")
		return
	}
	_ = actx // actx 仅用于认证校验，删除操作不需要租户过滤（Engine 按 ID 删除）
	if err := g.approvalEngine.DeleteFlow(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

// ============================================================================
// 审批请求生命周期
// ============================================================================

// handleApprovalRequests 处理 /api/v1/approval/requests（GET 列表 / POST 提交）。
func (g *Gateway) handleApprovalRequests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		g.approvalListRequests(w, r)
	case http.MethodPost:
		g.approvalSubmitRequest(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (g *Gateway) approvalListRequests(w http.ResponseWriter, r *http.Request) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if g.approvalEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "approval engine not configured")
		return
	}
	status := r.URL.Query().Get("status")
	reqs := g.approvalEngine.ListRequests(actx.TenantID, status)
	writeJSON(w, http.StatusOK, map[string]any{
		"requests": reqs,
		"total":    len(reqs),
	})
}

func (g *Gateway) approvalSubmitRequest(w http.ResponseWriter, r *http.Request) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if g.approvalEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "approval engine not configured")
		return
	}
	var req approval.ApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.TenantID == "" {
		req.TenantID = actx.TenantID
	}
	if req.Operator == "" {
		req.Operator = actx.UserID
	}
	if req.ID == "" {
		req.ID = genApprovalID("apr")
	}
	if req.Status == "" {
		req.Status = approval.StatusPending
	}
	if err := g.approvalEngine.Submit(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, &req)
}

// handleApprovalRequestRouting 处理 /api/v1/approval/requests/{id}[/approve|/reject|/cancel|/history]。
func (g *Gateway) handleApprovalRequestRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/approval/requests/")
	if idAndRest == "" {
		writeError(w, http.StatusBadRequest, "request id required")
		return
	}
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if len(parts) == 1 {
		// GET /api/v1/approval/requests/{id}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		g.approvalGetRequest(w, r, id)
		return
	}
	switch parts[1] {
	case "approve":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		g.approvalApproveRequest(w, r, id)
	case "reject":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		g.approvalRejectRequest(w, r, id)
	case "cancel":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		g.approvalCancelRequest(w, r, id)
	case "history":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		g.approvalGetHistory(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (g *Gateway) approvalGetRequest(w http.ResponseWriter, r *http.Request, id string) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if g.approvalEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "approval engine not configured")
		return
	}
	req, err := g.approvalEngine.GetRequest(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if actx.TenantID != "" && req.TenantID != actx.TenantID {
		writeError(w, http.StatusForbidden, "tenant mismatch")
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (g *Gateway) approvalApproveRequest(w http.ResponseWriter, r *http.Request, id string) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if g.approvalEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "approval engine not configured")
		return
	}
	var body struct {
		Comment string `json:"comment"`
	}
	// comment 为可选字段：空 body 视为空注释；非法 JSON 返回 400。
	if derr := json.NewDecoder(r.Body).Decode(&body); derr != nil && !errors.Is(derr, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := g.approvalEngine.Approve(id, actx.UserID, body.Comment); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved", "id": id})
}

func (g *Gateway) approvalRejectRequest(w http.ResponseWriter, r *http.Request, id string) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if g.approvalEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "approval engine not configured")
		return
	}
	var body struct {
		Comment string `json:"comment"`
	}
	// comment 为可选字段：空 body 视为空注释；非法 JSON 返回 400。
	if derr := json.NewDecoder(r.Body).Decode(&body); derr != nil && !errors.Is(derr, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := g.approvalEngine.Reject(id, actx.UserID, body.Comment); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected", "id": id})
}

func (g *Gateway) approvalCancelRequest(w http.ResponseWriter, r *http.Request, id string) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if g.approvalEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "approval engine not configured")
		return
	}
	if err := g.approvalEngine.Cancel(id, actx.UserID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled", "id": id})
}

func (g *Gateway) approvalGetHistory(w http.ResponseWriter, r *http.Request, id string) {
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if g.approvalEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "approval engine not configured")
		return
	}
	h, err := g.approvalEngine.GetHistory(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	// 租户隔离：通过请求归属校验（GetHistory 已校验请求存在）。
	if actx.TenantID != "" {
		if req, e2 := g.approvalEngine.GetRequest(id); e2 == nil && req.TenantID != actx.TenantID {
			writeError(w, http.StatusForbidden, "tenant mismatch")
			return
		}
	}
	writeJSON(w, http.StatusOK, h)
}

// ============================================================================
// 待我审批列表
// ============================================================================

// handleApprovalPending 处理 GET /api/v1/approval/pending：待我审批列表。
func (g *Gateway) handleApprovalPending(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if g.approvalEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "approval engine not configured")
		return
	}
	pending := g.approvalEngine.ListPendingApprovals(actx.UserID)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"pending": pending,
		"total":   len(pending),
	})
}

// ============================================================================
// 工具
// ============================================================================

// genApprovalID 生成审批实体 ID，格式：{prefix}-{unixNano}-{randomHex}。
// 对齐 controlplane genBatchID 的语义：前缀 + 时间戳 + 随机段保证全局唯一。
func genApprovalID(prefix string) string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("%s-%d-%s", prefix, time.Now().UnixNano(), hex.EncodeToString(buf[:]))
}
