package approval

import (
	"testing"
	"time"
)

func TestRequestValidate(t *testing.T) {
	valid := func() *ApprovalRequest {
		return &ApprovalRequest{
			ID: "r1", FlowID: "f1", TenantID: "t1", TriggerType: TriggerShell, Operator: "op",
			Status: StatusPending,
		}
	}

	cases := []struct {
		name   string
		mutate func(*ApprovalRequest)
		wantOK bool
	}{
		{"完整字段", func(*ApprovalRequest) {}, true},
		{"每种合法状态", func(r *ApprovalRequest) { r.Status = StatusApproved }, true},
		{"缺 ID", func(r *ApprovalRequest) { r.ID = "" }, false},
		{"缺 FlowID", func(r *ApprovalRequest) { r.FlowID = "" }, false},
		{"缺 TenantID", func(r *ApprovalRequest) { r.TenantID = "" }, false},
		{"缺 TriggerType", func(r *ApprovalRequest) { r.TriggerType = "" }, false},
		{"缺 Operator", func(r *ApprovalRequest) { r.Operator = "" }, false},
		{"非法状态", func(r *ApprovalRequest) { r.Status = "bogus" }, false},
		{"空状态", func(r *ApprovalRequest) { r.Status = "" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := valid()
			tc.mutate(r)
			err := r.Validate()
			if tc.wantOK && err != nil {
				t.Fatalf("期望通过，得到错误：%v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("期望报错，得到 nil（非法请求被放行）")
			}
		})
	}
}

func TestRequestStepLookup(t *testing.T) {
	r := &ApprovalRequest{Steps: []RequestStep{
		{StepID: "s1", Order: 1},
		{StepID: "s2", Order: 2},
	}}

	if got := r.StepByOrder(2); got == nil || got.StepID != "s2" {
		t.Errorf("StepByOrder(2) = %+v，期望 s2", got)
	}
	if got := r.StepByOrder(9); got != nil {
		t.Errorf("StepByOrder(9) = %+v，期望 nil", got)
	}
	if got := r.StepByID("s1"); got == nil || got.Order != 1 {
		t.Errorf("StepByID(s1) = %+v，期望 Order=1", got)
	}
	if got := r.StepByID("nope"); got != nil {
		t.Errorf("StepByID(nope) = %+v，期望 nil", got)
	}
}

func TestRequestStepHasDecided(t *testing.T) {
	rs := &RequestStep{Decisions: []Decision{{UserID: "u1", Action: ActionApprove}}}
	if !rs.HasDecided("u1") {
		t.Error("u1 已决策，HasDecided 应返回 true")
	}
	if rs.HasDecided("u2") {
		t.Error("u2 未决策，HasDecided 应返回 false")
	}
}

func TestRequestIsExpired(t *testing.T) {
	now := testTime()
	r := &ApprovalRequest{}
	if r.IsExpired(now) {
		t.Error("ExpireAt 为零值应视为不过期")
	}
	r.ExpireAt = now
	if r.IsExpired(now) {
		t.Error("now 恰等于 ExpireAt 不应过期（严格 After）")
	}
	r.ExpireAt = now.Add(-1)
	if !r.IsExpired(now) {
		t.Error("ExpireAt 已过应判定过期")
	}
	r.ExpireAt = now.Add(time.Hour)
	if r.IsExpired(now) {
		t.Error("ExpireAt 未到不应过期")
	}
}

func TestRequestStatusIsTerminal(t *testing.T) {
	terminal := []RequestStatus{StatusApproved, StatusRejected, StatusTimeout, StatusCancelled}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%q 应为终态", s)
		}
	}
	if StatusPending.IsTerminal() {
		t.Error("pending 不应是终态")
	}
	if RequestStatus("bogus").IsTerminal() {
		t.Error("未知状态不应是终态")
	}
}
