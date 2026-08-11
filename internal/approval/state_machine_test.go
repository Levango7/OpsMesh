package approval

import (
	"testing"
	"time"
)

func TestCanTransition(t *testing.T) {
	cases := []struct {
		from, to RequestStatus
		want     bool
	}{
		{StatusPending, StatusApproved, true},
		{StatusPending, StatusRejected, true},
		{StatusPending, StatusTimeout, true},
		{StatusPending, StatusCancelled, true},
		{StatusPending, StatusPending, false},     // 自转
		{StatusApproved, StatusPending, false},    // 终态出发
		{StatusApproved, StatusRejected, false},   // 终态间
		{StatusRejected, StatusApproved, false},   // 终态间
		{StatusTimeout, StatusCancelled, false},   // 终态间
		{StatusCancelled, StatusApproved, false},  // 终态间
		{StatusPending, "unknown", false},         // 未知目标
		{"unknown", StatusApproved, false},        // 未知源
	}
	for _, c := range cases {
		got := CanTransition(c.from, c.to)
		if got != c.want {
			t.Errorf("CanTransition(%q,%q)=%v want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestRequestStatusIsTerminal(t *testing.T) {
	terminals := []RequestStatus{StatusApproved, StatusRejected, StatusTimeout, StatusCancelled}
	for _, s := range terminals {
		if !s.IsTerminal() {
			t.Errorf("%q should be terminal", s)
		}
	}
	if StatusPending.IsTerminal() {
		t.Errorf("pending should not be terminal")
	}
}

func TestIsApprover(t *testing.T) {
	step := &ApprovalStep{Approvers: []string{"alice", "bob"}}
	if !isApprover(step, "alice") {
		t.Error("alice should be approver")
	}
	if !isApprover(step, "bob") {
		t.Error("bob should be approver")
	}
	if isApprover(step, "carol") {
		t.Error("carol should not be approver")
	}
}

func TestNextSequentialApprover(t *testing.T) {
	step := &ApprovalStep{Approvers: []string{"alice", "bob", "carol"}}
	rs := &RequestStep{}
	if got := nextSequentialApprover(step, rs); got != "alice" {
		t.Errorf("first approver = %q want alice", got)
	}
	rs.Decisions = append(rs.Decisions, Decision{UserID: "alice", Action: ActionApprove})
	if got := nextSequentialApprover(step, rs); got != "bob" {
		t.Errorf("second approver = %q want bob", got)
	}
	rs.Decisions = append(rs.Decisions, Decision{UserID: "bob", Action: ActionApprove})
	if got := nextSequentialApprover(step, rs); got != "carol" {
		t.Errorf("third approver = %q want carol", got)
	}
	rs.Decisions = append(rs.Decisions, Decision{UserID: "carol", Action: ActionApprove})
	if got := nextSequentialApprover(step, rs); got != "" {
		t.Errorf("after all decided = %q want empty", got)
	}
}

func TestEvaluateStepSequential(t *testing.T) {
	step := &ApprovalStep{Mode: StepSequential, Approvers: []string{"alice", "bob"}}

	// 无决策：未完成。
	rs := &RequestStep{}
	if done, passed := evaluateStep(step, rs); done || passed {
		t.Errorf("empty sequential: done=%v passed=%v want false/false", done, passed)
	}

	// 第一个同意，仍需第二个。
	rs.Decisions = append(rs.Decisions, Decision{Action: ActionApprove})
	if done, passed := evaluateStep(step, rs); done || passed {
		t.Errorf("one approve: done=%v passed=%v want false/false", done, passed)
	}

	// 第二个同意，步骤通过。
	rs.Decisions = append(rs.Decisions, Decision{Action: ActionApprove})
	if done, passed := evaluateStep(step, rs); !done || !passed {
		t.Errorf("two approves: done=%v passed=%v want true/true", done, passed)
	}

	// 任一拒绝 → 拒绝。
	rs2 := &RequestStep{Decisions: []Decision{{Action: ActionApprove}, {Action: ActionReject}}}
	if done, passed := evaluateStep(step, rs2); !done || passed {
		t.Errorf("approve+reject: done=%v passed=%v want true/false", done, passed)
	}

	// 第一个就拒绝。
	rs3 := &RequestStep{Decisions: []Decision{{Action: ActionReject}}}
	if done, passed := evaluateStep(step, rs3); !done || passed {
		t.Errorf("first reject: done=%v passed=%v want true/false", done, passed)
	}
}

func TestEvaluateStepCountersign(t *testing.T) {
	step := &ApprovalStep{Mode: StepCountersign, Approvers: []string{"alice", "bob", "carol"}}

	// 部分同意：未完成。
	rs := &RequestStep{Decisions: []Decision{{Action: ActionApprove}, {Action: ActionApprove}}}
	if done, _ := evaluateStep(step, rs); done {
		t.Error("countersign partial approves should not be done")
	}

	// 全部同意：通过。
	rs.Decisions = append(rs.Decisions, Decision{Action: ActionApprove})
	if done, passed := evaluateStep(step, rs); !done || !passed {
		t.Errorf("countersign all approve: done=%v passed=%v want true/true", done, passed)
	}

	// 任一拒绝：拒绝（即使其他人同意）。
	rs2 := &RequestStep{Decisions: []Decision{{Action: ActionApprove}, {Action: ActionReject}, {Action: ActionApprove}}}
	if done, passed := evaluateStep(step, rs2); !done || passed {
		t.Errorf("countersign with reject: done=%v passed=%v want true/false", done, passed)
	}

	// 第一个就拒绝：立即拒绝。
	rs3 := &RequestStep{Decisions: []Decision{{Action: ActionReject}}}
	if done, passed := evaluateStep(step, rs3); !done || passed {
		t.Errorf("countersign first reject: done=%v passed=%v want true/false", done, passed)
	}
}

func TestEvaluateStepAnyOf(t *testing.T) {
	step := &ApprovalStep{Mode: StepAnyOf, Approvers: []string{"alice", "bob", "carol"}}

	// 无决策：未完成。
	rs := &RequestStep{}
	if done, _ := evaluateStep(step, rs); done {
		t.Error("anyof empty should not be done")
	}

	// 任一同意：通过。
	rs1 := &RequestStep{Decisions: []Decision{{Action: ActionApprove}}}
	if done, passed := evaluateStep(step, rs1); !done || !passed {
		t.Errorf("anyof one approve: done=%v passed=%v want true/true", done, passed)
	}

	// 任一拒绝：拒绝（先到先得）。
	rs2 := &RequestStep{Decisions: []Decision{{Action: ActionReject}}}
	if done, passed := evaluateStep(step, rs2); !done || passed {
		t.Errorf("anyof one reject: done=%v passed=%v want true/false", done, passed)
	}

	// 同意优先于拒绝（按决策顺序，先到先得）。
	rs3 := &RequestStep{Decisions: []Decision{{Action: ActionApprove}, {Action: ActionReject}}}
	if done, passed := evaluateStep(step, rs3); !done || !passed {
		t.Errorf("anyof approve first: done=%v passed=%v want true/true", done, passed)
	}
}

func TestStepExpired(t *testing.T) {
	base := time.Now()
	step := &ApprovalStep{Timeout: 10 * time.Minute}

	// 未启动计时：不超时。
	rs := &RequestStep{}
	if stepExpired(step, rs, base) {
		t.Error("zero StartedAt should not expire")
	}

	// 启动计时，未超时。
	rs.StartedAt = base
	if stepExpired(step, rs, base.Add(5*time.Minute)) {
		t.Error("5min < 10min should not expire")
	}

	// 超时。
	if !stepExpired(step, rs, base.Add(11*time.Minute)) {
		t.Error("11min > 10min should expire")
	}

	// 边界：恰好等于不超时（> 才超时）。
	if stepExpired(step, rs, base.Add(10*time.Minute)) {
		t.Error("10min == 10min should not expire (strict >)")
	}

	// Timeout <= 0：永不超时。
	noTimeout := &ApprovalStep{Timeout: 0}
	if stepExpired(noTimeout, rs, base.Add(100*time.Hour)) {
		t.Error("Timeout=0 should never expire")
	}
}

func TestValidateDecisionErrors(t *testing.T) {
	now := time.Now()
	flow := &ApprovalFlow{
		ID: "f1", Name: "f", TenantID: "t1", TriggerType: TriggerShell,
		Steps: []ApprovalStep{
			{ID: "s1", Order: 1, Mode: StepSequential, Approvers: []string{"alice", "bob"}},
		},
	}
	req := &ApprovalRequest{
		ID: "r1", FlowID: "f1", TenantID: "t1", TriggerType: TriggerShell,
		Operator: "ops", Status: StatusPending, CurrentStep: 1,
		Steps: []RequestStep{{StepID: "s1", Order: 1, Status: StatusPending, StartedAt: now}},
	}

	// 正常 sequential：alice 先审批。
	_, _, err := validateDecision(req, flow, "alice", now)
	if err != nil {
		t.Errorf("alice first sequential: unexpected err %v", err)
	}

	// 顺序错误：bob 不能先审批。
	req2 := *req
	req2.Steps = []RequestStep{{StepID: "s1", Order: 1, Status: StatusPending, StartedAt: now}}
	_, _, err = validateDecision(&req2, flow, "bob", now)
	if err != ErrOutOfOrder {
		t.Errorf("bob out of order: err=%v want %v", err, ErrOutOfOrder)
	}

	// 非审批人。
	_, _, err = validateDecision(req, flow, "carol", now)
	if err != ErrNotApprover {
		t.Errorf("carol not approver: err=%v want %v", err, ErrNotApprover)
	}

	// 重复决策。
	req3 := *req
	req3.Steps = []RequestStep{{
		StepID: "s1", Order: 1, Status: StatusPending, StartedAt: now,
		Decisions: []Decision{{UserID: "alice", Action: ActionApprove, Timestamp: now}},
	}}
	_, _, err = validateDecision(&req3, flow, "alice", now)
	if err != ErrStepAlreadyDecided {
		t.Errorf("alice already decided: err=%v want %v", err, ErrStepAlreadyDecided)
	}

	// 非待审批状态。
	req4 := *req
	req4.Status = StatusApproved
	_, _, err = validateDecision(&req4, flow, "alice", now)
	if err != ErrNotPending {
		t.Errorf("approved status: err=%v want %v", err, ErrNotPending)
	}

	// 已过期。
	req5 := *req
	req5.ExpireAt = now.Add(-1 * time.Second)
	_, _, err = validateDecision(&req5, flow, "alice", now)
	if err != ErrRequestExpired {
		t.Errorf("expired: err=%v want %v", err, ErrRequestExpired)
	}
}