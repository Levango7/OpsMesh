package approval

import (
	"errors"
	"time"
)

// 状态机相关错误。
var (
	// ErrInvalidTransition 非法整体状态转换。
	ErrInvalidTransition = errors.New("approval: invalid status transition")
	// ErrStepAlreadyDecided 该用户已在当前步骤做出决策。
	ErrStepAlreadyDecided = errors.New("approval: user already decided at this step")
	// ErrNotApprover 该用户不是当前步骤的审批人。
	ErrNotApprover = errors.New("approval: user is not an approver of current step")
	// ErrNotPending 请求不在待审批状态，无法继续审批。
	ErrNotPending = errors.New("approval: request is not pending")
	// ErrOutOfOrder sequential 模式下未轮到该用户审批。
	ErrOutOfOrder = errors.New("approval: out-of-order approver for sequential step")
	// ErrStepNotActive 当前步骤非活动步骤。
	ErrStepNotActive = errors.New("approval: step is not the active step")
	// ErrRequestExpired 请求已过期。
	ErrRequestExpired = errors.New("approval: request expired")
)

// CanTransition 检查整体状态转换是否合法。
//
// 仅允许 pending → approved/rejected/timeout/cancelled；终态不可再转换。
// 自转（from==to）返回 false。
func CanTransition(from, to RequestStatus) bool {
	if from == to {
		return false
	}
	if from != StatusPending {
		return false
	}
	switch to {
	case StatusApproved, StatusRejected, StatusTimeout, StatusCancelled:
		return true
	}
	return false
}

// isApprover 返回 userID 是否为 step 的审批人。
func isApprover(step *ApprovalStep, userID string) bool {
	for i := range step.Approvers {
		if step.Approvers[i] == userID {
			return true
		}
	}
	return false
}

// nextSequentialApprover 返回 sequential 模式下当前应审批的用户。
// 已全部审批完则返回空串。调用方应保证 rs 决策顺序与 step.Approvers 顺序一致。
func nextSequentialApprover(step *ApprovalStep, rs *RequestStep) string {
	idx := len(rs.Decisions)
	if idx >= len(step.Approvers) {
		return ""
	}
	return step.Approvers[idx]
}

// evaluateStep 评估步骤在当前决策集合下是否已完成及结果。
//
// 返回 (done, passed)：
//   - done=false: 步骤仍在进行中，可继续接受决策。
//   - done=true, passed=true: 步骤通过。
//   - done=true, passed=false: 步骤拒绝。
//
// 模式语义：
//   - sequential: 按 Approvers 顺序每人审批；任一拒绝→拒绝；全部通过→通过。
//   - countersign: 不限顺序，所有人需决策；任一拒绝→拒绝；全部同意→通过。
//   - anyof: 任一同意→通过；任一拒绝→拒绝（先到先得）。
func evaluateStep(step *ApprovalStep, rs *RequestStep) (done, passed bool) {
	switch step.Mode {
	case StepSequential:
		for i := range rs.Decisions {
			if rs.Decisions[i].Action == ActionReject {
				return true, false
			}
		}
		if len(rs.Decisions) >= len(step.Approvers) {
			return true, true
		}
		return false, false
	case StepCountersign:
		for i := range rs.Decisions {
			if rs.Decisions[i].Action == ActionReject {
				return true, false
			}
		}
		if len(rs.Decisions) >= len(step.Approvers) {
			return true, true
		}
		return false, false
	case StepAnyOf:
		for i := range rs.Decisions {
			if rs.Decisions[i].Action == ActionApprove {
				return true, true
			}
			if rs.Decisions[i].Action == ActionReject {
				return true, false
			}
		}
		return false, false
	}
	return false, false
}

// stepExpired 判断步骤是否已超时。
//   - step.Timeout <= 0：不超时，返回 false。
//   - rs.StartedAt 为零值：不超时（未启动计时）。
//   - 否则 now - rs.StartedAt > step.Timeout 即超时。
func stepExpired(step *ApprovalStep, rs *RequestStep, now time.Time) bool {
	if step.Timeout <= 0 || rs.StartedAt.IsZero() {
		return false
	}
	return now.Sub(rs.StartedAt) > step.Timeout
}

// validateDecision 在应用决策前校验前置条件，返回当前步骤指针与请求步骤快照。
//
// 校验规则：
//   - 请求状态必须为 pending。
//   - 当前步骤必须存在于 flow 且对应请求快照存在。
//   - userID 必须是当前步骤的审批人。
//   - sequential 模式下必须按 Approvers 顺序审批。
//   - 用户不能在同一步骤重复决策。
func validateDecision(req *ApprovalRequest, flow *ApprovalFlow, userID string, now time.Time) (*ApprovalStep, *RequestStep, error) {
	if req.Status != StatusPending {
		return nil, nil, ErrNotPending
	}
	if !req.ExpireAt.IsZero() && now.After(req.ExpireAt) {
		return nil, nil, ErrRequestExpired
	}
	step := flow.StepByOrder(req.CurrentStep)
	if step == nil {
		return nil, nil, ErrStepNotActive
	}
	rs := req.StepByOrder(req.CurrentStep)
	if rs == nil {
		return nil, nil, ErrStepNotActive
	}
	if !isApprover(step, userID) {
		return nil, nil, ErrNotApprover
	}
	// 优先检查是否已决策（最明确的错误），再检查 sequential 顺序。
	if rs.HasDecided(userID) {
		return nil, nil, ErrStepAlreadyDecided
	}
	if step.Mode == StepSequential {
		expected := nextSequentialApprover(step, rs)
		if expected != "" && expected != userID {
			return nil, nil, ErrOutOfOrder
		}
	}
	return step, rs, nil
}
