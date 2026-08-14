package approval

import (
	"testing"
	"time"
)

func TestShouldRequireApproval(t *testing.T) {
	cases := []struct {
		trigger string
		risk    string
		want    bool
	}{
		{TriggerShell, RiskLow, true},
		{TriggerShell, RiskHigh, true},
		{TriggerBatchRestart, RiskMedium, true},
		{TriggerK8sDelete, RiskLow, true},
		{TriggerConfigChange, RiskHigh, true},
		{TriggerConfigChange, RiskMedium, true},
		{TriggerConfigChange, RiskLow, false},
		{TriggerDeploy, RiskHigh, true},
		{TriggerDeploy, RiskMedium, false},
		{TriggerDeploy, RiskLow, false},
		{"unknown", RiskHigh, true}, // 未知触发类型仅高危
		{"unknown", RiskLow, false},
		{"unknown", RiskMedium, false},
	}
	for _, c := range cases {
		got := ShouldRequireApproval(c.trigger, c.risk)
		if got != c.want {
			t.Errorf("ShouldRequireApproval(%q,%q)=%v want %v", c.trigger, c.risk, got, c.want)
		}
	}
}

func TestDefaultFlowsValidity(t *testing.T) {
	if len(DefaultFlows) == 0 {
		t.Fatal("DefaultFlows is empty")
	}
	seen := make(map[string]bool, len(DefaultFlows))
	for i, f := range DefaultFlows {
		if seen[f.TriggerType] {
			t.Errorf("DefaultFlows[%d]: duplicate trigger type %q", i, f.TriggerType)
		}
		seen[f.TriggerType] = true
		// 预置流 TenantID 为空（模板），Validate 要求非空，故填入临时租户校验。
		cp := cloneFlow(f)
		cp.TenantID = "__validate__"
		if !cp.CreatedAt.IsZero() {
			t.Errorf("DefaultFlows[%d] CreatedAt should be zero", i)
		}
		cp.CreatedAt = time.Now()
		if err := cp.Validate(); err != nil {
			t.Errorf("DefaultFlows[%d] (%s) invalid: %v", i, f.TriggerType, err)
		}
		if !cp.Enabled {
			t.Errorf("DefaultFlows[%d] should be enabled", i)
		}
	}
}

func TestDefaultFlowForTrigger(t *testing.T) {
	for _, trigger := range []string{TriggerShell, TriggerBatchRestart, TriggerK8sDelete, TriggerConfigChange, TriggerDeploy} {
		f := DefaultFlowForTrigger(trigger)
		if f == nil {
			t.Errorf("DefaultFlowForTrigger(%q) = nil", trigger)
			continue
		}
		if f.TriggerType != trigger {
			t.Errorf("trigger mismatch: %q vs %q", f.TriggerType, trigger)
		}
		// 确保是深拷贝：修改返回值不影响原模板。
		origLen := len(f.Steps)
		f.Steps = nil
		f2 := DefaultFlowForTrigger(trigger)
		if len(f2.Steps) != origLen {
			t.Errorf("DefaultFlowForTrigger(%q) mutated original: steps len %d vs %d", trigger, len(f2.Steps), origLen)
		}
	}
	if f := DefaultFlowForTrigger("nonexistent"); f != nil {
		t.Errorf("DefaultFlowForTrigger(nonexistent) = %+v want nil", f)
	}
}

func TestCloneFlow(t *testing.T) {
	src := &ApprovalFlow{
		ID: "f1", Steps: []ApprovalStep{
			{ID: "s1", Approvers: []string{"a", "b"}},
		},
	}
	dst := cloneFlow(src)
	dst.Steps[0].Approvers[0] = "X"
	if src.Steps[0].Approvers[0] == "X" {
		t.Error("cloneFlow did not deep copy Approvers")
	}
	dst.Steps[0].ID = "mut"
	if src.Steps[0].ID == "mut" {
		t.Error("cloneFlow did not deep copy Steps")
	}
}
