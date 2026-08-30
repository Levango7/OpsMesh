// cmdb_approval.go 实现 CMDB 配置项变更审批流：
//   - POST   /api/v1/cmdb/changes            提交变更申请
//   - GET    /api/v1/cmdb/changes            列出变更申请（?status=pending 过滤）
//   - GET    /api/v1/cmdb/changes/{id}       获取变更详情
//   - POST   /api/v1/cmdb/changes/{id}/approve 审批通过（执行变更）
//   - POST   /api/v1/cmdb/changes/{id}/reject  驳回变更
//
// 设计要点：
//   - 创建/修改/删除 CI 需审批通过才生效，审批人可批准或驳回；
//   - 复用现有 ApproveTask/RejectTask 模式（pending → approved/rejected 状态机）；
//   - 审批通过后调用 cmdb.CiStore 的 CRUD 方法执行实际变更；
//   - 全程记录审计日志（等保三级留痕）。
//
// 与 cmdb/handler.go 中已有的单 CI 审批端点（/api/v1/cmdb/ci/{id}/approve）的区别：
//   - handler.go 的审批是单 CI 实例级别的审批状态翻转（CI 已落库，仅改 approvalStatus 字段）；
//   - 本文件实现的是变更申请级别的审批流（CI 尚未落库，审批通过后才执行 create/update/delete）。
package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/cmdb"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// ============================================================================
// 变更申请状态常量
// ============================================================================

const (
	// cmdbChangeStatusPending 待审批。
	cmdbChangeStatusPending = "pending"
	// cmdbChangeStatusApproved 已通过。
	cmdbChangeStatusApproved = "approved"
	// cmdbChangeStatusRejected 已驳回。
	cmdbChangeStatusRejected = "rejected"
)

// 支持的变更动作。
const (
	cmdbActionCreate = "create"
	cmdbActionUpdate = "update"
	cmdbActionDelete = "delete"
)

// ============================================================================
// 数据模型
// ============================================================================

// CMDBChangeRequest CMDB 变更申请。
type CMDBChangeRequest struct {
	ID         string                 `json:"id"`         // 变更申请 ID（chg-<8 字节 hex>）
	TenantID   string                 `json:"tenantID"`   // 租户隔离
	Action     string                 `json:"action"`     // "create" | "update" | "delete"
	CIType     string                 `json:"ciType"`     // CI 类型（machine/os/service/app/cluster/自定义）
	CIID       string                 `json:"ciID"`       // CI ID（update/delete 时有值）
	Changes    map[string]interface{} `json:"changes"`    // 变更内容（create/update 时含 CiItem 字段）
	Status     string                 `json:"status"`     // "pending" | "approved" | "rejected"
	Requester  string                 `json:"requester"`  // 申请人
	Approver   string                 `json:"approver"`   // 审批人（审批后填充）
	Reason     string                 `json:"reason"`     // 申请理由
	Comment    string                 `json:"comment"`    // 审批备注
	CreatedAt  time.Time              `json:"createdAt"`  // 提交时间
	ReviewedAt *time.Time             `json:"reviewedAt"` // 审批时间（审批后填充）
}

// ============================================================================
// 审批管理器
// ============================================================================

// changeApplyError 包装"变更执行阶段"的底层错误（ciStore 的 CreateCI/UpdateCI/DeleteCI）。
//
// 底层 ciStore 可能返回 SQL 错误（含表名、列名、SQL 片段、DSN），直接回吐客户端即信息泄露。
// 用该类型标记后，HTTP 层可据此脱敏：响应体只给固定文案，原始 err 进服务端日志与审计。
// Cause 仍保留在错误链中（Unwrap），服务端日志/审计可拿到完整信息。
type changeApplyError struct{ cause error }

func (e *changeApplyError) Error() string { return "apply change failed: " + e.cause.Error() }
func (e *changeApplyError) Unwrap() error { return e.cause }

// CMDBApprovalManager CMDB 变更审批管理器。
//
// 持有 store.Store（记录审计日志）与 cmdb.CiStore（执行实际 CI CRUD），
// 内存索引 pending/all 两张表（重启后丢失，MVP 实现；生产可改为 store 持久化）。
//
// 线程安全：mu 保护 pending/all 两张 map 的并发读写。
type CMDBApprovalManager struct {
	store   store.Store  // 审计日志
	ciStore cmdb.CiStore // CI CRUD
	mu      sync.RWMutex
	pending map[string]*CMDBChangeRequest // key=changeID，待审批
	all     map[string]*CMDBChangeRequest // key=changeID，全部变更（含已审批）
}

// NewCMDBApprovalManager 构造 CMDB 变更审批管理器。
func NewCMDBApprovalManager(st store.Store, ci cmdb.CiStore) *CMDBApprovalManager {
	return &CMDBApprovalManager{
		store:   st,
		ciStore: ci,
		pending: make(map[string]*CMDBChangeRequest),
		all:     make(map[string]*CMDBChangeRequest),
	}
}

// SubmitChange 提交变更申请（返回 changeID，等待审批）。
//
// 校验 action 合法性，生成 changeID，置 status=pending，加入 pending/all 索引。
// 不执行实际变更（需审批通过后由 ApproveChange 执行）。
func (m *CMDBApprovalManager) SubmitChange(req *CMDBChangeRequest) (string, error) {
	if req == nil {
		return "", fmt.Errorf("change request is nil")
	}
	// 校验动作合法性。
	switch req.Action {
	case cmdbActionCreate, cmdbActionUpdate, cmdbActionDelete:
	default:
		return "", fmt.Errorf("invalid action: %q (must be create/update/delete)", req.Action)
	}
	// update/delete 必须指定 CI ID。
	if (req.Action == cmdbActionUpdate || req.Action == cmdbActionDelete) && req.CIID == "" {
		return "", fmt.Errorf("ciID required for %s action", req.Action)
	}
	// create 必须指定 CI 类型。
	if req.Action == cmdbActionCreate && req.CIType == "" {
		return "", fmt.Errorf("ciType required for create action")
	}
	// 生成变更 ID。
	if req.ID == "" {
		req.ID = genBatchID("chg")
	}
	req.Status = cmdbChangeStatusPending
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now()
	}

	m.mu.Lock()
	m.pending[req.ID] = req
	m.all[req.ID] = req
	m.mu.Unlock()

	// 记录审计日志（提交申请）。
	m.audit(context.Background(), &proto.AuditEvent{
		TenantID: req.TenantID, UserID: req.Requester,
		Action: "cmdb_change_submit", Target: req.ID,
		Detail: sanitizeAuditDetail(fmt.Sprintf("submit %s on %s/%s: %s", req.Action, req.CIType, req.CIID, req.Reason)),
	})

	return req.ID, nil
}

// ApproveChange 审批通过（执行变更）。
//
// 逻辑：
//  1. 检查变更存在且状态为 pending；
//  2. 执行实际变更（调用 ciStore 的 CreateCI/UpdateCI/DeleteCI）；
//  3. 更新状态为 approved，记录审批人和时间；
//  4. 从 pending 索引移除；
//  5. 记录审计日志。
//
// 若执行实际变更失败，状态保持 pending，调用方可重试。
func (m *CMDBApprovalManager) ApproveChange(changeID, approver, comment string) error {
	m.mu.Lock()
	req, ok := m.all[changeID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("change request %s not found", changeID)
	}
	if req.Status != cmdbChangeStatusPending {
		m.mu.Unlock()
		return fmt.Errorf("change request %s is not pending (current status: %s)", changeID, req.Status)
	}
	m.mu.Unlock()

	// 执行实际变更（在锁外执行，避免长时间持锁）。
	ctx := context.Background()
	if err := m.applyChange(ctx, req); err != nil {
		// 变更执行失败，状态保持 pending，返回错误供调用方重试。
		m.audit(ctx, &proto.AuditEvent{
			TenantID: req.TenantID, UserID: approver,
			Action: "cmdb_change_approve_failed", Target: changeID,
			Detail: sanitizeAuditDetail(fmt.Sprintf("approve failed: %v", err)),
		})
		// 用 changeApplyError 标记：调用方据此脱敏，避免 SQL 细节回吐客户端。
		return &changeApplyError{cause: err}
	}

	// 变更执行成功，更新状态。
	now := time.Now()
	m.mu.Lock()
	req.Status = cmdbChangeStatusApproved
	req.Approver = approver
	req.Comment = comment
	req.ReviewedAt = &now
	delete(m.pending, changeID)
	m.mu.Unlock()

	// 记录审计日志（审批通过）。
	m.audit(ctx, &proto.AuditEvent{
		TenantID: req.TenantID, UserID: approver,
		Action: "cmdb_change_approve", Target: changeID,
		Detail: sanitizeAuditDetail(fmt.Sprintf("approve %s on %s/%s: %s", req.Action, req.CIType, req.CIID, comment)),
	})

	return nil
}

// RejectChange 驳回变更。
//
// 逻辑：
//  1. 检查变更存在且状态为 pending；
//  2. 更新状态为 rejected，记录审批人和时间；
//  3. 从 pending 索引移除；
//  4. 记录审计日志。
//
// 驳回不执行实际变更。
func (m *CMDBApprovalManager) RejectChange(changeID, approver, comment string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	req, ok := m.all[changeID]
	if !ok {
		return fmt.Errorf("change request %s not found", changeID)
	}
	if req.Status != cmdbChangeStatusPending {
		return fmt.Errorf("change request %s is not pending (current status: %s)", changeID, req.Status)
	}

	now := time.Now()
	req.Status = cmdbChangeStatusRejected
	req.Approver = approver
	req.Comment = comment
	req.ReviewedAt = &now
	delete(m.pending, changeID)

	// 记录审计日志（驳回）。
	m.audit(context.Background(), &proto.AuditEvent{
		TenantID: req.TenantID, UserID: approver,
		Action: "cmdb_change_reject", Target: changeID,
		Detail: sanitizeAuditDetail(fmt.Sprintf("reject %s on %s/%s: %s", req.Action, req.CIType, req.CIID, comment)),
	})

	return nil
}

// ListPending 列出待审批变更（按租户过滤）。
func (m *CMDBApprovalManager) ListPending(tenantID string) ([]*CMDBChangeRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*CMDBChangeRequest, 0, len(m.pending))
	for _, req := range m.pending {
		if tenantID != "" && req.TenantID != tenantID {
			continue
		}
		out = append(out, req)
	}
	return out, nil
}

// ListAll 列出全部变更（按租户 + 状态过滤，status 空串=全部）。
func (m *CMDBApprovalManager) ListAll(tenantID, status string) ([]*CMDBChangeRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*CMDBChangeRequest, 0, len(m.all))
	for _, req := range m.all {
		if tenantID != "" && req.TenantID != tenantID {
			continue
		}
		if status != "" && req.Status != status {
			continue
		}
		out = append(out, req)
	}
	return out, nil
}

// GetChange 获取变更详情。
func (m *CMDBApprovalManager) GetChange(changeID string) (*CMDBChangeRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	req, ok := m.all[changeID]
	if !ok {
		return nil, fmt.Errorf("change request %s not found", changeID)
	}
	return req, nil
}

// ============================================================================
// 内部方法
// ============================================================================

// applyChange 执行实际变更（审批通过后调用）。
//
// 根据 action 分发到 ciStore 的 CreateCI/UpdateCI/DeleteCI。
// Changes 字段通过 JSON 反序列化构造 CiItem（create/update 时）。
func (m *CMDBApprovalManager) applyChange(ctx context.Context, req *CMDBChangeRequest) error {
	switch req.Action {
	case cmdbActionCreate:
		ci, err := changeToCiItem(req)
		if err != nil {
			return err
		}
		ci.TenantID = req.TenantID
		ci.CiType = req.CIType
		if ci.ID == "" {
			ci.ID = fmt.Sprintf("ci-%d", time.Now().UnixNano())
		}
		if ci.Status == "" {
			ci.Status = "active"
		}
		if ci.Source == "" {
			ci.Source = "api"
		}
		// 审批通过的 CI 直接为 approved 状态。
		ci.ApprovalStatus = cmdb.ApprovalApproved
		now := time.Now()
		if ci.CreatedAt.IsZero() {
			ci.CreatedAt = now
		}
		ci.UpdatedAt = now
		return m.ciStore.CreateCI(ctx, ci)

	case cmdbActionUpdate:
		ci, err := changeToCiItem(req)
		if err != nil {
			return err
		}
		ci.ID = req.CIID
		ci.TenantID = req.TenantID
		if req.CIType != "" {
			ci.CiType = req.CIType
		}
		// 审批通过的更新保持 approved 状态。
		ci.ApprovalStatus = cmdb.ApprovalApproved
		return m.ciStore.UpdateCI(ctx, ci)

	case cmdbActionDelete:
		return m.ciStore.DeleteCI(ctx, req.CIID, req.TenantID)

	default:
		return fmt.Errorf("unknown action: %s", req.Action)
	}
}

// changeToCiItem 从变更申请的 Changes 字段构造 CiItem。
//
// Changes 是 map[string]interface{}，通过 JSON marshal/unmarshal 转成 CiItem。
// Changes 为空时返回空 CiItem（delete 动作不需要 Changes）。
func changeToCiItem(req *CMDBChangeRequest) (*cmdb.CiItem, error) {
	if len(req.Changes) == 0 {
		return &cmdb.CiItem{}, nil
	}
	b, err := json.Marshal(req.Changes)
	if err != nil {
		return nil, fmt.Errorf("marshal changes: %w", err)
	}
	var ci cmdb.CiItem
	if err := json.Unmarshal(b, &ci); err != nil {
		return nil, fmt.Errorf("unmarshal changes to CiItem: %w", err)
	}
	return &ci, nil
}

// audit 记录审计事件（store 为 nil 时跳过，便于测试）。
func (m *CMDBApprovalManager) audit(ctx context.Context, e *proto.AuditEvent) {
	if m.store == nil || e == nil {
		return
	}
	m.store.Audit(e)
}

// ============================================================================
// HTTP API handler
// ============================================================================

// handleCMDBChanges 处理 /api/v1/cmdb/changes（GET 列表 / POST 提交）。
func (s *Server) handleCMDBChanges(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.cmdbChangeList(w, r)
	case http.MethodPost:
		s.cmdbChangeSubmit(w, r)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// cmdbChangeList GET /api/v1/cmdb/changes：列出变更申请（?status=pending 过滤）。
func (s *Server) cmdbChangeList(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cmdb:read"); !ok {
		return
	}
	status := r.URL.Query().Get("status")
	var (
		reqs []*CMDBChangeRequest
		err  error
	)
	if status == cmdbChangeStatusPending {
		reqs, err = s.cmdbApprovalMgr.ListPending(actx.TenantID)
	} else {
		reqs, err = s.cmdbApprovalMgr.ListAll(actx.TenantID, status)
	}
	if err != nil {
		writeInternalError(r.Context(), w, "cmdbApproval.listChanges", err)
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"changes": reqs,
		"total":   len(reqs),
	})
}

// cmdbChangeSubmit POST /api/v1/cmdb/changes：提交变更申请。
func (s *Server) cmdbChangeSubmit(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cmdb:write"); !ok {
		return
	}
	var req CMDBChangeRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
		return
	}
	if req.TenantID == "" {
		req.TenantID = actx.TenantID
	}
	if req.Requester == "" {
		req.Requester = actx.UserID
	}
	id, err := s.cmdbApprovalMgr.SubmitChange(&req)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	paginate.WriteJSON(w, http.StatusCreated, &req)
	_ = id
}

// handleCMDBChangeRouting 处理 /api/v1/cmdb/changes/{id}[/approve|/reject]。
func (s *Server) handleCMDBChangeRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/cmdb/changes/")
	if idAndRest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "change id required"})
		return
	}
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if len(parts) == 1 {
		// GET /api/v1/cmdb/changes/{id}
		if r.Method != http.MethodGet {
			paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.cmdbChangeGet(w, r, id)
		return
	}
	switch parts[1] {
	case "approve":
		if r.Method != http.MethodPost {
			paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.cmdbChangeApprove(w, r, id)
	case "reject":
		if r.Method != http.MethodPost {
			paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.cmdbChangeReject(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found", "path": r.URL.Path})
	}
}

// cmdbChangeGet GET /api/v1/cmdb/changes/{id}：获取变更详情。
func (s *Server) cmdbChangeGet(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cmdb:read"); !ok {
		return
	}
	req, err := s.cmdbApprovalMgr.GetChange(id)
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

// cmdbChangeApprove POST /api/v1/cmdb/changes/{id}/approve：审批通过。
func (s *Server) cmdbChangeApprove(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cmdb:approve"); !ok {
		return
	}
	// 租户归属校验：审批前确认变更属于当前租户。
	req, err := s.cmdbApprovalMgr.GetChange(id)
	if err != nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if actx.TenantID != "" && req.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
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
	if err := s.cmdbApprovalMgr.ApproveChange(id, actx.UserID, body.Comment); err != nil {
		// 变更执行阶段的错误来自 ciStore（可能含 SQL 细节）→ 脱敏后返回，原始 err 仅记日志。
		var apErr *changeApplyError
		if errors.As(err, &apErr) {
			writeSanitizedError(r.Context(), w, http.StatusBadRequest, "cmdbApproval.approveChange", "apply change failed", err)
			return
		}
		// 其余为状态机/校验错误（not found / not pending），语义安全，按原契约回吐。
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "approved", "id": id})
}

// cmdbChangeReject POST /api/v1/cmdb/changes/{id}/reject：驳回变更。
func (s *Server) cmdbChangeReject(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "cmdb:approve"); !ok {
		return
	}
	// 租户归属校验：驳回前确认变更属于当前租户。
	req, err := s.cmdbApprovalMgr.GetChange(id)
	if err != nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if actx.TenantID != "" && req.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
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
	if err := s.cmdbApprovalMgr.RejectChange(id, actx.UserID, body.Comment); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "rejected", "id": id})
}
