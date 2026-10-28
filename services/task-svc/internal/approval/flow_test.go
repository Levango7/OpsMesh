package approval

import (
	"testing"
)

func validFlow() *ApprovalFlow {
	return &ApprovalFlow{
		ID: "f1", Name: "测试流", TenantID: "t1", TriggerType: TriggerDeploy, Enabled: true,
		Steps: []ApprovalStep{
			{ID: "s1", Order: 1, Mode: StepSequential, Approvers: []string{"u1", "u2"}},
			{ID: "s2", Order: 2, Mode: StepAnyOf, Approvers: []string{"u3"}},
		},
	}
}

func TestFlowValidate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ApprovalFlow)
		wantOK bool
	}{
		{"合法两步骤", func(*ApprovalFlow) {}, true},
		{"缺 ID", func(f *ApprovalFlow) { f.ID = "" }, false},
		{"缺 Name", func(f *ApprovalFlow) { f.Name = "" }, false},
		{"缺 TenantID", func(f *ApprovalFlow) { f.TenantID = "" }, false},
		{"缺 TriggerType", func(f *ApprovalFlow) { f.TriggerType = "" }, false},
		{"无步骤", func(f *ApprovalFlow) { f.Steps = nil }, false},
		{"步骤缺 ID", func(f *ApprovalFlow) { f.Steps[0].ID = "" }, false},
		{"步骤 ID 重复", func(f *ApprovalFlow) { f.Steps[1].ID = f.Steps[0].ID }, false},
		{"Order 小于 1", func(f *ApprovalFlow) { f.Steps[0].Order = 0 }, false},
		{"Order 重复", func(f *ApprovalFlow) { f.Steps[1].Order = f.Steps[0].Order }, false},
		{"Order 未升序", func(f *ApprovalFlow) { f.Steps[0].Order, f.Steps[1].Order = 2, 1 }, false},
		{"非法 Mode", func(f *ApprovalFlow) { f.Steps[0].Mode = StepMode("bogus") }, false},
		{"空 Mode", func(f *ApprovalFlow) { f.Steps[0].Mode = "" }, false},
		{"步骤无审批人", func(f *ApprovalFlow) { f.Steps[0].Approvers = nil }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := validFlow()
			tc.mutate(f)
			err := f.Validate()
			if tc.wantOK && err != nil {
				t.Fatalf("期望通过，得到错误：%v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("期望报错，得到 nil（非法流定义被放行）")
			}
		})
	}
}

func TestFlowStepLookup(t *testing.T) {
	f := validFlow()
	if got := f.StepByOrder(2); got == nil || got.ID != "s2" {
		t.Errorf("StepByOrder(2) = %+v，期望 s2", got)
	}
	if got := f.StepByOrder(9); got != nil {
		t.Errorf("StepByOrder(9) = %+v，期望 nil", got)
	}
	if got := f.StepByID("s1"); got == nil || got.Order != 1 {
		t.Errorf("StepByID(s1) = %+v，期望 Order=1", got)
	}
	if got := f.StepByID("nope"); got != nil {
		t.Errorf("StepByID(nope) = %+v，期望 nil", got)
	}
}

func TestFlowLastOrder(t *testing.T) {
	if got := (&ApprovalFlow{}).LastOrder(); got != 0 {
		t.Errorf("空流 LastOrder() = %d，期望 0", got)
	}
	if got := validFlow().LastOrder(); got != 2 {
		t.Errorf("LastOrder() = %d，期望 2", got)
	}
}
