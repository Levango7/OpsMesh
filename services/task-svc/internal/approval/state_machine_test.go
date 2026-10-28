package approval

import (
	"errors"
	"testing"
	"time"
)

func TestCanTransition(t *testing.T) {
	all := []RequestStatus{StatusPending, StatusApproved, StatusRejected, StatusTimeout, StatusCancelled}
	for _, from := range all {
		for _, to := range all {
			got := CanTransition(from, to)
			want := from == StatusPending && to != StatusPending
			if got != want {
				t.Errorf("CanTransition(%q, %q) = %v，期望 %v", from, to, got, want)
			}
		}
	}
	if CanTransition(RequestStatus("bogus"), StatusApproved) {
		t.Error("未知起始状态不应允许转换")
	}
}

func TestEvaluateStep(t *testing.T) {
	approvals := func(n int) []Decision {
		out := make([]Decision, n)
		for i := range out {
			out[i] = Decision{Action: ActionApprove}
		}
		return out
	}
	withReject := func(n int) []Decision {
		d := approvals(n)
		d[len(d)-1].Action = ActionReject
		return d
	}

	cases := []struct {
		name       string
		mode       StepMode
		approvers  int
		decisions  []Decision
		wantDone   bool
		wantPassed bool
	}{
		{"sequential 无决策", StepSequential, 2, nil, false, false},
		{"sequential 部分通过", StepSequential, 2, approvals(1), false, false},
		{"sequential 全部通过", StepSequential, 2, approvals(2), true, true},
		{"sequential 一票否决", StepSequential, 2, withReject(2), true, false},
		{"countersign 无决策", StepCountersign, 2, nil, false, false},
		{"countersign 部分通过", StepCountersign, 2, approvals(1), false, false},
		{"countersign 全部通过", StepCountersign, 2, approvals(2), true, true},
		{"countersign 一票否决", StepCountersign, 3, withReject(2), true, false},
		{"anyof 无人决策", StepAnyOf, 2, nil, false, false},
		{"anyof 任一人同意", StepAnyOf, 2, approvals(1), true, true},
		{"anyof 任一人拒绝", StepAnyOf, 2, withReject(1), true, false},
		{"未知模式", StepMode("bogus"), 1, approvals(1), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := &ApprovalStep{Mode: tc.mode, Approvers: make([]string, tc.approvers)}
			rs := &RequestStep{Decisions: tc.decisions}
			done, passed := evaluateStep(step, rs)
			if done != tc.wantDone || passed != tc.wantPassed {
				t.Errorf("evaluateStep() = (%v, %v)，期望 (%v, %v)", done, passed, tc.wantDone, tc.wantPassed)
			}
		})
	}
}

func TestStepExpired(t *testing.T) {
	now := testTime()
	cases := []struct {
		name      string
		timeout   time.Duration
		startedAt time.Time
		want      bool
	}{
		{"未设置超时", 0, now.Add(-time.Hour), false},
		{"负超时视为不超时", -time.Minute, now.Add(-time.Hour), false},
		{"未启动计时", 10 * time.Minute, time.Time{}, false},
		{"恰好在超时点", 10 * time.Minute, now.Add(-10 * time.Minute), false},
		{"超过超时点", 10 * time.Minute, now.Add(-10*time.Minute - time.Nanosecond), true},
		{"远未超时", 10 * time.Minute, now.Add(-time.Minute), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := &ApprovalStep{Timeout: tc.timeout}
			rs := &RequestStep{StartedAt: tc.startedAt}
			if got := stepExpired(step, rs, now); got != tc.want {
				t.Errorf("stepExpired() = %v，期望 %v", got, tc.want)
			}
		})
	}
}

// TestValidateDecision 把"应用决策前"的六类前置校验逐条钉住，
// 它们的错误值也被 HTTP 层映射为不同状态码，漂移会直接改接口语义。
func TestValidateDecision(t *testing.T) {
	now := testTime()
	flow := &ApprovalFlow{
		ID: "f1", Name: "n", TenantID: "t1", TriggerType: TriggerDeploy, Enabled: true,
		Steps: []ApprovalStep{
			{ID: "s1", Order: 1, Mode: StepSequential, Approvers: []string{"u1", "u2"}},
			{ID: "s2", Order: 2, Mode: StepCountersign, Approvers: []string{"u1", "u2"}},
		},
	}
	newReq := func() *ApprovalRequest {
		return &ApprovalRequest{
			ID: "r1", Status: StatusPending, CurrentStep: 1,
			Steps: []RequestStep{
				{StepID: "s1", Order: 1, Status: StatusPending},
				{StepID: "s2", Order: 2, Status: StatusPending},
			},
		}
	}

	cases := []struct {
		name    string
		req     func() *ApprovalRequest
		userID  string
		wantErr error
		wantOK  bool
	}{
		{"sequential 首位审批人", newReq, "u1", nil, true},
		{"非 pending 请求", func() *ApprovalRequest { r := newReq(); r.Status = StatusApproved; return r }, "u1", ErrNotPending, false},
		{"请求已过期", func() *ApprovalRequest { r := newReq(); r.ExpireAt = now.Add(-time.Second); return r }, "u1", ErrRequestExpired, false},
		{"当前步骤不在流中", func() *ApprovalRequest { r := newReq(); r.CurrentStep = 9; return r }, "u1", ErrStepNotActive, false},
		{"请求缺少步骤快照", func() *ApprovalRequest { r := newReq(); r.Steps = nil; return r }, "u1", ErrStepNotActive, false},
		{"非本步骤审批人", newReq, "stranger", ErrNotApprover, false},
		{"已决策者重复决策", func() *ApprovalRequest {
			r := newReq()
			r.Steps[0].Decisions = []Decision{{UserID: "u1", Action: ActionApprove}}
			return r
		}, "u1", ErrStepAlreadyDecided, false},
		{"sequential 越序审批", newReq, "u2", ErrOutOfOrder, false},
		{"countersign 任意顺序", func() *ApprovalRequest { r := newReq(); r.CurrentStep = 2; return r }, "u2", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step, rs, err := validateDecision(tc.req(), flow, tc.userID, now)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("期望通过，得到错误：%v", err)
				}
				if step == nil || rs == nil {
					t.Fatal("通过时应返回步骤与请求快照指针")
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v，期望 %v", err, tc.wantErr)
			}
			if step != nil || rs != nil {
				t.Error("校验失败时不应返回指针")
			}
		})
	}
}
