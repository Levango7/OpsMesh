package approval

import (
	"errors"
	"time"
)

// RequestStatus 审批请求状态。
type RequestStatus string

const (
	// StatusPending 待审批。
	StatusPending RequestStatus = "pending"
	// StatusApproved 已通过（所有步骤完成）。
	StatusApproved RequestStatus = "approved"
	// StatusRejected 已拒绝。
	StatusRejected RequestStatus = "rejected"
	// StatusTimeout 已超时。
	StatusTimeout RequestStatus = "timeout"
	// StatusCancelled 已取消。
	StatusCancelled RequestStatus = "cancelled"
)

// IsTerminal 返回 s 是否为终态（不可再转换）。
func (s RequestStatus) IsTerminal() bool {
	switch s {
	case StatusApproved, StatusRejected, StatusTimeout, StatusCancelled:
		return true
	}
	return false
}

// 决策动作常量。
const (
	ActionApprove = "approve" // 同意
	ActionReject  = "reject"  // 拒绝
)

// Decision 单次审批决策。
type Decision struct {
	UserID    string    // 决策人 userID
	Action    string    // approve / reject
	Comment   string    // 备注
	Timestamp time.Time // 决策时间
}

// RequestStep 请求在某一步骤的状态快照。
type RequestStep struct {
	StepID    string        // 对应 ApprovalStep.ID
	Order     int           // 步骤 Order（冗余，便于查询排序）
	Status    RequestStatus // 该步骤状态：pending/approved/rejected/timeout
	Decisions []Decision    // 决策列表（按时间顺序追加）
	StartedAt time.Time     // 步骤开始时间（成为当前步骤的时刻，用于步骤超时计算）
}

// HasDecided 返回 userID 在本步骤是否已决策。
func (rs *RequestStep) HasDecided(userID string) bool {
	for i := range rs.Decisions {
		if rs.Decisions[i].UserID == userID {
			return true
		}
	}
	return false
}

// ApprovalRequest 审批请求实例。
//
// 状态机：Status 从 pending 出发，经审批步骤推进后转为 approved/rejected/timeout/cancelled。
// CurrentStep 指向当前进行中的步骤 Order（1-based）；已完成最后一步时 CurrentStep = LastOrder + 1。
type ApprovalRequest struct {
	ID          string        // 全局唯一 ID
	FlowID      string        // 关联审批流 ID
	TenantID    string        // 租户 ID
	TriggerType string        // 触发类型
	Operator    string        // 发起人 userID
	Target      string        // 操作目标描述
	Detail      string        // 操作详情
	Risk        string        // 风险等级 high/medium/low
	Status      RequestStatus // 整体状态
	CurrentStep int           // 当前审批步骤 Order
	Steps       []RequestStep // 各步骤状态快照
	CreatedAt   time.Time     // 创建时间
	UpdatedAt   time.Time     // 最近更新时间
	ExpireAt    time.Time     // 整体过期时间（<=0 值表示不过期）
}

// Validate 校验请求字段合法性（不校验业务推进规则，那由状态机负责）。
func (r *ApprovalRequest) Validate() error {
	if r.ID == "" {
		return errors.New("approval: request ID is required")
	}
	if r.FlowID == "" {
		return errors.New("approval: request FlowID is required")
	}
	if r.TenantID == "" {
		return errors.New("approval: request TenantID is required")
	}
	if r.TriggerType == "" {
		return errors.New("approval: request TriggerType is required")
	}
	if r.Operator == "" {
		return errors.New("approval: request Operator is required")
	}
	switch r.Status {
	case StatusPending, StatusApproved, StatusRejected, StatusTimeout, StatusCancelled:
	default:
		return errors.New("approval: invalid request Status: " + string(r.Status))
	}
	return nil
}

// StepByOrder 按 Order 查找请求步骤快照。未找到返回 nil。
func (r *ApprovalRequest) StepByOrder(order int) *RequestStep {
	for i := range r.Steps {
		if r.Steps[i].Order == order {
			return &r.Steps[i]
		}
	}
	return nil
}

// StepByID 按 StepID 查找请求步骤快照。未找到返回 nil。
func (r *ApprovalRequest) StepByID(id string) *RequestStep {
	for i := range r.Steps {
		if r.Steps[i].StepID == id {
			return &r.Steps[i]
		}
	}
	return nil
}

// IsExpired 返回请求是否已过期（ExpireAt 非零且已过）。
func (r *ApprovalRequest) IsExpired(now time.Time) bool {
	return !r.ExpireAt.IsZero() && now.After(r.ExpireAt)
}
