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
	UserID    string    `json:"userId"`    // 决策人 userID
	Action    string    `json:"action"`    // approve / reject
	Comment   string    `json:"comment"`   // 备注
	Timestamp time.Time `json:"timestamp"` // 决策时间
}

// RequestStep 请求在某一步骤的状态快照。
type RequestStep struct {
	StepID    string        `json:"stepId"`    // 对应 ApprovalStep.ID
	Order     int           `json:"order"`     // 步骤 Order（冗余，便于查询排序）
	Status    RequestStatus `json:"status"`    // 该步骤状态：pending/approved/rejected/timeout
	Decisions []Decision    `json:"decisions"` // 决策列表（按时间顺序追加）
	StartedAt time.Time     `json:"startedAt"` // 步骤开始时间（成为当前步骤的时刻，用于步骤超时计算）
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
	ID          string `json:"id"`          // 全局唯一 ID
	FlowID      string `json:"flowId"`      // 关联审批流 ID
	FlowName    string `json:"flowName"`    // 关联审批流名称（提交时从 Flow 快照填入）
	TenantID    string `json:"tenantId"`    // 租户 ID
	TriggerType string `json:"triggerType"` // 触发类型
	Operator    string `json:"operator"`    // 发起人 userID
	Requester   string `json:"requester"`   // Operator 的 UI 兼容字段
	Target      string `json:"target"`      // 操作目标描述
	Detail      string `json:"detail"`      // 操作详情
	Risk        string `json:"risk"`        // 风险等级 high/medium/low
	// ResourceType / ResourceID 是当前 UI 对 TriggerType / Target 的兼容字段，
	// 避免前端把缺少的字段渲染成空白。
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceID"`
	// IdempotencyKey 客户端提供的幂等键（可选）。同一租户内相同键的重复提交返回
	// 首次创建的请求，不会新建第二个请求——防止网络重试/双击造成重复审批。
	// 留空时行为与旧版一致（仅按 ID 去重）。
	IdempotencyKey string        `json:"idempotency_key,omitempty"`
	Status         RequestStatus `json:"status"`      // 整体状态
	CurrentStep    int           `json:"currentStep"` // 当前审批步骤 Order
	Steps          []RequestStep `json:"steps"`       // 各步骤状态快照
	CreatedAt      time.Time     `json:"createdAt"`   // 创建时间
	UpdatedAt      time.Time     `json:"updatedAt"`   // 最近更新时间
	ExpireAt       time.Time     `json:"expireAt"`    // 整体过期时间（<=0 值表示不过期）
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
