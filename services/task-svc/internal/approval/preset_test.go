package approval

import (
	"testing"
)

func TestShouldRequireApproval(t *testing.T) {
	cases := []struct {
		trigger string
		risk    string
		want    bool
	}{
		{TriggerShell, RiskLow, true}, // 高危操作无视风险等级，始终审批
		{TriggerShell, RiskHigh, true},
		{TriggerBatchRestart, RiskLow, true},
		{TriggerK8sDelete, RiskMedium, true},
		{TriggerConfigChange, RiskLow, false}, // 配置变更：非 low 才审批
		{TriggerConfigChange, RiskMedium, true},
		{TriggerConfigChange, RiskHigh, true},
		{TriggerConfigChange, "", true}, // 未标风险按非 low 处理（保守）
		{TriggerDeploy, RiskHigh, true}, // 部署：仅 high 审批
		{TriggerDeploy, RiskMedium, false},
		{TriggerDeploy, RiskLow, false},
		{"unknown_trigger", RiskHigh, true}, // 未知触发：仅 high 审批
		{"unknown_trigger", RiskLow, false},
	}
	for _, tc := range cases {
		if got := ShouldRequireApproval(tc.trigger, tc.risk); got != tc.want {
			t.Errorf("ShouldRequireApproval(%q, %q) = %v，期望 %v", tc.trigger, tc.risk, got, tc.want)
		}
	}
}

// TestDefaultFlowForTrigger 钉住预置流模板的三件事：
// 触发类型与 ID 对应、模板本身合法、每次返回深拷贝（调用方改坏不影响后续租户）。
func TestDefaultFlowForTrigger(t *testing.T) {
	for _, trigger := range []string{
		TriggerShell, TriggerBatchRestart, TriggerConfigChange, TriggerK8sDelete, TriggerDeploy,
	} {
		f := DefaultFlowForTrigger(trigger)
		if f == nil {
			t.Fatalf("%q 缺少预置流", trigger)
		}
		if f.TriggerType != trigger {
			t.Errorf("预置流 %q 的 TriggerType = %q", trigger, f.TriggerType)
		}
		if want := "preset-" + trigger; f.ID != want {
			t.Errorf("预置流 ID = %q，期望 %q", f.ID, want)
		}
		if !f.Enabled {
			t.Errorf("预置流 %q 应为启用状态", trigger)
		}
		// 预置流是跨租户模板（TenantID 为空），实例化后才校验；此处补租户再验定义本身。
		f.TenantID = "t1"
		if err := f.Validate(); err != nil {
			t.Errorf("预置流 %q 定义不合法：%v", trigger, err)
		}
	}

	if got := DefaultFlowForTrigger("no_such_trigger"); got != nil {
		t.Errorf("未知触发类型应返回 nil，得到 %+v", got)
	}
}

func TestDefaultFlowForTrigger_DeepCopy(t *testing.T) {
	first := DefaultFlowForTrigger(TriggerDeploy)
	first.Steps[0].Approvers[0] = "hacked"
	first.Steps[1].Order = 99

	second := DefaultFlowForTrigger(TriggerDeploy)
	if second.Steps[0].Approvers[0] == "hacked" {
		t.Error("修改返回值的 Approvers 污染了预置模板（应为深拷贝）")
	}
	if second.Steps[1].Order != 2 {
		t.Errorf("修改返回值的 Steps 污染了预置模板：Order=%d", second.Steps[1].Order)
	}
}
