package approval

import (
	"time"
)

// 预置审批人角色常量（userID 占位，实际由租户 RBAC 映射）。
const (
	ApproverAdmin   = "admin"    // 管理员
	ApproverOpsLead = "ops_lead" // 运维负责人
	ApproverSRELead = "sre_lead" // SRE 负责人
)

// DefaultFlows 预置审批流定义。
//
// 按触发类型覆盖常见高危场景，可作为新租户的初始策略。
// 调用方可通过 Engine.CreateFlow 覆盖。每个流的 ID 形如 "preset-<triggerType>"。
var DefaultFlows = []*ApprovalFlow{
	{
		ID:          "preset-shell",
		Name:        "Shell 命令执行审批",
		TenantID:    "", // 预置流租户空，表示模板；实例化时填入实际租户
		TriggerType: TriggerShell,
		Steps: []ApprovalStep{
			{
				ID:        "shell-1",
				Name:      "管理员或签",
				Order:     1,
				Mode:      StepAnyOf,
				Approvers: []string{ApproverAdmin},
				Timeout:   30 * time.Minute,
			},
		},
		Enabled:   true,
		CreatedAt: time.Time{},
	},
	{
		ID:          "preset-batch_restart",
		Name:        "批量重启审批",
		TenantID:    "",
		TriggerType: TriggerBatchRestart,
		Steps: []ApprovalStep{
			{
				ID:        "batch_restart-1",
				Name:      "运维负责人审批",
				Order:     1,
				Mode:      StepSequential,
				Approvers: []string{ApproverOpsLead, ApproverAdmin},
				Timeout:   30 * time.Minute,
			},
		},
		Enabled:   true,
		CreatedAt: time.Time{},
	},
	{
		ID:          "preset-k8s_delete",
		Name:        "K8s 资源删除审批",
		TenantID:    "",
		TriggerType: TriggerK8sDelete,
		Steps: []ApprovalStep{
			{
				ID:        "k8s_delete-1",
				Name:      "SRE 负责人与管理员会签",
				Order:     1,
				Mode:      StepCountersign,
				Approvers: []string{ApproverSRELead, ApproverAdmin},
				Timeout:   60 * time.Minute,
			},
		},
		Enabled:   true,
		CreatedAt: time.Time{},
	},
	{
		ID:          "preset-config_change",
		Name:        "配置变更审批",
		TenantID:    "",
		TriggerType: TriggerConfigChange,
		Steps: []ApprovalStep{
			{
				ID:        "config_change-1",
				Name:      "运维负责人或签",
				Order:     1,
				Mode:      StepAnyOf,
				Approvers: []string{ApproverOpsLead, ApproverAdmin},
				Timeout:   30 * time.Minute,
			},
		},
		Enabled:   true,
		CreatedAt: time.Time{},
	},
	{
		ID:          "preset-deploy",
		Name:        "部署审批",
		TenantID:    "",
		TriggerType: TriggerDeploy,
		Steps: []ApprovalStep{
			{
				ID:        "deploy-1",
				Name:      "运维负责人审批",
				Order:     1,
				Mode:      StepSequential,
				Approvers: []string{ApproverOpsLead},
				Timeout:   30 * time.Minute,
			},
			{
				ID:        "deploy-2",
				Name:      "管理员审批",
				Order:     2,
				Mode:      StepSequential,
				Approvers: []string{ApproverAdmin},
				Timeout:   30 * time.Minute,
			},
		},
		Enabled:   true,
		CreatedAt: time.Time{},
	},
}

// presetFlowByTrigger 预置流按触发类型索引（懒初始化）。
var presetFlowByTrigger = func() map[string]*ApprovalFlow {
	m := make(map[string]*ApprovalFlow, len(DefaultFlows))
	for _, f := range DefaultFlows {
		m[f.TriggerType] = f
	}
	return m
}()

// ShouldRequireApproval 判断指定触发类型与风险等级的操作是否需要审批。
//
// 策略：
//   - shell / batch_restart / k8s_delete：始终需要审批（高危操作）。
//   - config_change：risk != low 需要审批。
//   - deploy：risk == high 需要审批。
//   - 未知触发类型：risk == high 需要审批（保守策略）。
func ShouldRequireApproval(triggerType string, risk string) bool {
	switch triggerType {
	case TriggerShell, TriggerBatchRestart, TriggerK8sDelete:
		return true
	case TriggerConfigChange:
		return risk != RiskLow
	case TriggerDeploy:
		return risk == RiskHigh
	}
	// 未知触发类型：仅高危需审批。
	return risk == RiskHigh
}

// DefaultFlowForTrigger 返回指定触发类型的预置流模板（深拷贝）。
// 不存在返回 nil。调用方应填入 TenantID 与时间戳后用于 Engine.CreateFlow。
func DefaultFlowForTrigger(triggerType string) *ApprovalFlow {
	tmpl := presetFlowByTrigger[triggerType]
	if tmpl == nil {
		return nil
	}
	return cloneFlow(tmpl)
}

// cloneFlow 深拷贝审批流（含 Steps 切片与 Approvers 切片）。
func cloneFlow(f *ApprovalFlow) *ApprovalFlow {
	out := *f
	if f.Steps != nil {
		out.Steps = make([]ApprovalStep, len(f.Steps))
		for i := range f.Steps {
			out.Steps[i] = f.Steps[i]
			if f.Steps[i].Approvers != nil {
				out.Steps[i].Approvers = append([]string(nil), f.Steps[i].Approvers...)
			}
		}
	}
	return &out
}
