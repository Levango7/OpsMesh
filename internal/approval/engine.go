package approval

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// NotifierFunc 审批通知回调。
// 在状态变更后调用（已释放引擎锁，回调内可安全调用 Engine 方法）。
// action 取值：submit / approve / reject / cancel / timeout / step_advance。
type NotifierFunc func(req *ApprovalRequest, action string)

// Option Engine 构造选项。
type Option func(*Engine)

// WithNotifier 注入通知回调。
func WithNotifier(n NotifierFunc) Option {
	return func(e *Engine) { e.notifier = n }
}

// WithNow 注入时间函数（默认 time.Now），便于测试注入虚拟时钟。
func WithNow(fn func() time.Time) Option {
	return func(e *Engine) { e.now = fn }
}

// Engine 审批引擎：管理审批流定义与审批请求的状态机推进。
//
// 线程安全：所有公共方法通过 mu 保护 flows / requests / histories 索引。
// 通知回调在锁外执行，避免回调内再次调用 Engine 方法导致死锁。
type Engine struct {
	flows     map[string]*ApprovalFlow
	requests  map[string]*ApprovalRequest
	histories map[string]*History
	mu        sync.RWMutex
	notifier  NotifierFunc
	now       func() time.Time
}

// New 构造审批引擎实例。
func New(opts ...Option) *Engine {
	e := &Engine{
		flows:     make(map[string]*ApprovalFlow),
		requests:  make(map[string]*ApprovalRequest),
		histories: make(map[string]*History),
		now:       time.Now,
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// nowTime 返回当前时间（兼容 now 为 nil 的边界情况）。
func (e *Engine) nowTime() time.Time {
	if e.now == nil {
		return time.Now()
	}
	return e.now()
}

// SetNotifier 运行时替换通知回调。线程安全。
func (e *Engine) SetNotifier(n NotifierFunc) {
	e.mu.Lock()
	e.notifier = n
	e.mu.Unlock()
}

// ========== 审批流 CRUD ==========

// ErrFlowNotFound 审批流不存在。
var ErrFlowNotFound = errors.New("approval: flow not found")

// ErrFlowExists 审批流 ID 已存在。
var ErrFlowExists = errors.New("approval: flow already exists")

// ErrRequestNotFound 审批请求不存在。
var ErrRequestNotFound = errors.New("approval: request not found")

// ErrRequestExists 审批请求 ID 已存在。
var ErrRequestExists = errors.New("approval: request already exists")

// CreateFlow 创建审批流。
//   - 校验 flow.Validate()。
//   - ID 重复返回 ErrFlowExists。
//   - CreatedAt / UpdatedAt 为零值时填入当前时间。
func (e *Engine) CreateFlow(flow *ApprovalFlow) error {
	if err := flow.Validate(); err != nil {
		return err
	}
	now := e.nowTime()
	cp := cloneFlow(flow)
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = now
	}
	if cp.UpdatedAt.IsZero() {
		cp.UpdatedAt = now
	}

	e.mu.Lock()
	if _, exists := e.flows[cp.ID]; exists {
		e.mu.Unlock()
		return ErrFlowExists
	}
	e.flows[cp.ID] = cp
	e.mu.Unlock()
	return nil
}

// UpdateFlow 更新审批流。不存在返回 ErrFlowNotFound。
// 不影响已基于该流创建的进行中请求（请求持有创建时的快照）。
func (e *Engine) UpdateFlow(flow *ApprovalFlow) error {
	if err := flow.Validate(); err != nil {
		return err
	}
	now := e.nowTime()
	cp := cloneFlow(flow)
	cp.UpdatedAt = now

	e.mu.Lock()
	if _, exists := e.flows[cp.ID]; !exists {
		e.mu.Unlock()
		return ErrFlowNotFound
	}
	cp.CreatedAt = e.flows[cp.ID].CreatedAt // 保留原创建时间
	e.flows[cp.ID] = cp
	e.mu.Unlock()
	return nil
}

// DeleteFlow 删除审批流。不存在返回 ErrFlowNotFound。
// 不阻止删除仍有进行中请求的流（请求持有快照）。
func (e *Engine) DeleteFlow(id string) error {
	e.mu.Lock()
	if _, exists := e.flows[id]; !exists {
		e.mu.Unlock()
		return ErrFlowNotFound
	}
	delete(e.flows, id)
	e.mu.Unlock()
	return nil
}

// GetFlow 按 ID 查询审批流。不存在返回 ErrFlowNotFound。
func (e *Engine) GetFlow(id string) (*ApprovalFlow, error) {
	e.mu.RLock()
	f, ok := e.flows[id]
	e.mu.RUnlock()
	if !ok {
		return nil, ErrFlowNotFound
	}
	return cloneFlow(f), nil
}

// ListFlows 列出指定租户的所有审批流，按 ID 升序。
// tenantID 为空时返回所有流。
func (e *Engine) ListFlows(tenantID string) []*ApprovalFlow {
	e.mu.RLock()
	out := make([]*ApprovalFlow, 0, len(e.flows))
	for _, f := range e.flows {
		if tenantID != "" && f.TenantID != tenantID {
			continue
		}
		out = append(out, cloneFlow(f))
	}
	e.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ========== 审批请求生命周期 ==========

// Submit 提交审批请求。
//   - 校验 req.Validate()。
//   - req.ID 重复返回 ErrRequestExists。
//   - 关联 FlowID 必须存在且 Enabled。
//   - 初始化 Status=pending、CurrentStep=1、各步骤快照。
//   - 触发 submit 通知。
func (e *Engine) Submit(req *ApprovalRequest) error {
	// Status 为空时视为 pending（调用方未显式设置）。
	if req.Status == "" {
		req.Status = StatusPending
	}
	if err := req.Validate(); err != nil {
		return err
	}
	now := e.nowTime()

	e.mu.Lock()
	if _, exists := e.requests[req.ID]; exists {
		e.mu.Unlock()
		return ErrRequestExists
	}
	flow, ok := e.flows[req.FlowID]
	if !ok {
		e.mu.Unlock()
		return ErrFlowNotFound
	}
	if !flow.Enabled {
		e.mu.Unlock()
		return errors.New("approval: flow is disabled")
	}

	cp := *req
	cp.Status = StatusPending
	cp.CurrentStep = 1
	cp.Steps = make([]RequestStep, len(flow.Steps))
	for i := range flow.Steps {
		cp.Steps[i] = RequestStep{
			StepID: flow.Steps[i].ID,
			Order:  flow.Steps[i].Order,
			Status: StatusPending,
		}
		if flow.Steps[i].Order == 1 {
			cp.Steps[i].StartedAt = now
		}
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = now
	}
	cp.UpdatedAt = now

	e.requests[cp.ID] = &cp
	hist := &History{RequestID: cp.ID}
	hist.Append(HistoryEntry{
		Timestamp: now,
		Action:    HistorySubmit,
		UserID:    cp.Operator,
		Comment:   "submit approval request",
	})
	e.histories[cp.ID] = hist
	e.mu.Unlock()

	e.notify(&cp, HistorySubmit)
	return nil
}

// applyDecision 是 Approve / Reject 的共用实现。action ∈ {approve, reject}。
// 返回更新后的请求副本（用于通知）。
func (e *Engine) applyDecision(requestID, userID, comment, action string) (*ApprovalRequest, error) {
	now := e.nowTime()

	e.mu.Lock()
	req, ok := e.requests[requestID]
	if !ok {
		e.mu.Unlock()
		return nil, ErrRequestNotFound
	}
	if req.Status != StatusPending {
		e.mu.Unlock()
		return nil, ErrNotPending
	}
	flow, ok := e.flows[req.FlowID]
	if !ok {
		// 流被删除：拒绝继续审批。
		e.mu.Unlock()
		return nil, ErrFlowNotFound
	}
	step, rs, err := validateDecision(req, flow, userID, now)
	if err != nil {
		e.mu.Unlock()
		return nil, err
	}

	// 追加决策。
	rs.Decisions = append(rs.Decisions, Decision{
		UserID:    userID,
		Action:    action,
		Comment:   comment,
		Timestamp: now,
	})
	req.UpdatedAt = now

	histEntry := HistoryEntry{
		Timestamp: now,
		Action:    action,
		UserID:    userID,
		StepID:    step.ID,
		Comment:   comment,
	}

	done, passed := evaluateStep(step, rs)
	notifyAction := action
	if done {
		if passed {
			rs.Status = StatusApproved
			// 推进到下一步或完成。
			if step.Order >= flow.LastOrder() {
				req.Status = StatusApproved
				req.CurrentStep = step.Order + 1 // 标记越界=已完成
			} else {
				req.CurrentStep = step.Order + 1
				nextRS := req.StepByOrder(req.CurrentStep)
				if nextRS != nil {
					nextRS.StartedAt = now
				}
				notifyAction = HistoryStepAdvance
				histEntry.Action = HistoryStepAdvance
				histEntry.Comment = "step " + step.ID + " approved, advance to next step"
			}
		} else {
			rs.Status = StatusRejected
			req.Status = StatusRejected
		}
	}

	snapshot := *req
	// 深拷贝 Steps 供锁外通知。
	snapshot.Steps = make([]RequestStep, len(req.Steps))
	for i := range req.Steps {
		snapshot.Steps[i] = req.Steps[i]
		snapshot.Steps[i].Decisions = append([]Decision(nil), req.Steps[i].Decisions...)
	}

	if h := e.histories[requestID]; h != nil {
		h.Append(histEntry)
	}
	e.mu.Unlock()

	e.notify(&snapshot, notifyAction)
	return &snapshot, nil
}

// Approve 同意当前步骤的审批。
func (e *Engine) Approve(requestID, userID, comment string) error {
	_, err := e.applyDecision(requestID, userID, comment, ActionApprove)
	return err
}

// Reject 拒绝当前步骤的审批。任一拒绝即整体拒绝。
func (e *Engine) Reject(requestID, userID, comment string) error {
	_, err := e.applyDecision(requestID, userID, comment, ActionReject)
	return err
}

// Cancel 取消审批请求。仅 pending 状态可取消。
// userID 为取消操作人（通常为发起人）。
func (e *Engine) Cancel(requestID, userID string) error {
	now := e.nowTime()

	e.mu.Lock()
	req, ok := e.requests[requestID]
	if !ok {
		e.mu.Unlock()
		return ErrRequestNotFound
	}
	if req.Status != StatusPending {
		e.mu.Unlock()
		return ErrInvalidTransition
	}
	req.Status = StatusCancelled
	req.UpdatedAt = now
	snapshot := *req
	snapshot.Steps = make([]RequestStep, len(req.Steps))
	for i := range req.Steps {
		snapshot.Steps[i] = req.Steps[i]
		snapshot.Steps[i].Decisions = append([]Decision(nil), req.Steps[i].Decisions...)
	}
	if h := e.histories[requestID]; h != nil {
		h.Append(HistoryEntry{
			Timestamp: now,
			Action:    HistoryCancel,
			UserID:    userID,
			Comment:   "cancel approval request",
		})
	}
	e.mu.Unlock()

	e.notify(&snapshot, HistoryCancel)
	return nil
}

// CheckTimeout 检查请求当前步骤是否超时，超时则标记为 timeout 并整体拒绝。
// 返回 true 表示触发了超时处理。已终态或未超时返回 false。
//
// 调用方应周期性轮询 pending 请求调用此方法，或由外部定时器驱动。
func (e *Engine) CheckTimeout(requestID string) (bool, error) {
	now := e.nowTime()

	e.mu.Lock()
	req, ok := e.requests[requestID]
	if !ok {
		e.mu.Unlock()
		return false, ErrRequestNotFound
	}
	if req.Status != StatusPending {
		e.mu.Unlock()
		return false, nil
	}
	// 整体过期。
	if req.IsExpired(now) {
		req.Status = StatusTimeout
		req.UpdatedAt = now
		e.recordTimeout(req, "", now, "request expired")
		snapshot := *req
		e.mu.Unlock()
		e.notify(&snapshot, HistoryTimeout)
		return true, nil
	}
	flow, ok := e.flows[req.FlowID]
	if !ok {
		e.mu.Unlock()
		return false, ErrFlowNotFound
	}
	step := flow.StepByOrder(req.CurrentStep)
	if step == nil {
		e.mu.Unlock()
		return false, nil
	}
	rs := req.StepByOrder(req.CurrentStep)
	if rs == nil {
		e.mu.Unlock()
		return false, nil
	}
	if !stepExpired(step, rs, now) {
		e.mu.Unlock()
		return false, nil
	}

	// 步骤超时 → 步骤 reject → 整体 reject（按任务要求"步骤超时自动 reject"）。
	rs.Status = StatusRejected
	req.Status = StatusRejected
	req.UpdatedAt = now
	e.recordTimeout(req, step.ID, now, "step "+step.ID+" timed out")
	snapshot := *req
	snapshot.Steps = make([]RequestStep, len(req.Steps))
	for i := range req.Steps {
		snapshot.Steps[i] = req.Steps[i]
		snapshot.Steps[i].Decisions = append([]Decision(nil), req.Steps[i].Decisions...)
	}
	e.mu.Unlock()

	e.notify(&snapshot, HistoryTimeout)
	return true, nil
}

// recordTimeout 在历史中追加超时条目。调用方须持锁。
func (e *Engine) recordTimeout(req *ApprovalRequest, stepID string, now time.Time, comment string) {
	if h := e.histories[req.ID]; h != nil {
		h.Append(HistoryEntry{
			Timestamp: now,
			Action:    HistoryTimeout,
			StepID:    stepID,
			Comment:   comment,
		})
	}
}

// GetRequest 按 ID 查询审批请求。不存在返回 ErrRequestNotFound。
func (e *Engine) GetRequest(id string) (*ApprovalRequest, error) {
	e.mu.RLock()
	req, ok := e.requests[id]
	e.mu.RUnlock()
	if !ok {
		return nil, ErrRequestNotFound
	}
	return cloneRequest(req), nil
}

// ListRequests 列出指定租户的审批请求，可选按状态过滤。
// tenantID 为空时返回所有租户；status 为空时返回所有状态。
// 按 CreatedAt 降序（最近优先）。
func (e *Engine) ListRequests(tenantID string, status string) []*ApprovalRequest {
	e.mu.RLock()
	out := make([]*ApprovalRequest, 0, len(e.requests))
	for _, r := range e.requests {
		if tenantID != "" && r.TenantID != tenantID {
			continue
		}
		if status != "" && string(r.Status) != status {
			continue
		}
		out = append(out, cloneRequest(r))
	}
	e.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// ListPendingApprovals 返回指定用户待审批的请求列表。
// 待审批定义：请求 pending 且用户属于当前步骤的 Approvers 且尚未在该步骤决策。
// 按 CreatedAt 升序（先提交先审批）。
func (e *Engine) ListPendingApprovals(userID string) []*ApprovalRequest {
	e.mu.RLock()
	out := make([]*ApprovalRequest, 0)
	for _, r := range e.requests {
		if r.Status != StatusPending {
			continue
		}
		flow, ok := e.flows[r.FlowID]
		if !ok {
			continue
		}
		step := flow.StepByOrder(r.CurrentStep)
		if step == nil {
			continue
		}
		if !isApprover(step, userID) {
			continue
		}
		rs := r.StepByOrder(r.CurrentStep)
		if rs == nil || rs.HasDecided(userID) {
			continue
		}
		// sequential 模式：仅当前应审批人可见。
		if step.Mode == StepSequential {
			if next := nextSequentialApprover(step, rs); next != "" && next != userID {
				continue
			}
		}
		out = append(out, cloneRequest(r))
	}
	e.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// GetHistory 返回指定请求的审批历史。不存在请求返回 ErrRequestNotFound。
func (e *Engine) GetHistory(requestID string) (*History, error) {
	e.mu.RLock()
	_, ok := e.requests[requestID]
	if !ok {
		e.mu.RUnlock()
		return nil, ErrRequestNotFound
	}
	h := e.histories[requestID]
	e.mu.RUnlock()
	if h == nil {
		return &History{RequestID: requestID}, nil
	}
	return cloneHistory(h), nil
}

// ========== 内部工具 ==========

// notify 调用通知回调（锁外）。nil 回调为空操作。
func (e *Engine) notify(req *ApprovalRequest, action string) {
	if e.notifier == nil {
		return
	}
	e.notifier(req, action)
}

// cloneRequest 深拷贝请求（含 Steps 与 Decisions）。
func cloneRequest(r *ApprovalRequest) *ApprovalRequest {
	out := *r
	if r.Steps != nil {
		out.Steps = make([]RequestStep, len(r.Steps))
		for i := range r.Steps {
			out.Steps[i] = r.Steps[i]
			if r.Steps[i].Decisions != nil {
				out.Steps[i].Decisions = append([]Decision(nil), r.Steps[i].Decisions...)
			}
		}
	}
	return &out
}

// cloneHistory 深拷贝历史。
func cloneHistory(h *History) *History {
	out := &History{RequestID: h.RequestID}
	if h.Timeline != nil {
		out.Timeline = append([]HistoryEntry(nil), h.Timeline...)
	}
	return out
}
