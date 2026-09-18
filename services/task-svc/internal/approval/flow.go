// Package approval 实现运维操作的审批流程核心引擎。
//
// 功能特性：
//   - 审批流定义：按租户/触发类型组织多级审批步骤。
//   - 状态机：pending → approved / rejected / timeout / cancelled。
//   - 多级审批模式：sequential（逐级）/ countersign（会签）/ anyof（或签）。
//   - 审批历史：完整记录提交、决策、步骤推进、超时与取消事件。
//   - 预置策略：高危操作（shell / batch_restart / k8s_delete 等）自动触发审批。
//
// 线程安全：Engine 通过 sync.RWMutex 保护 flows 与 requests 索引，可被多 goroutine 并发调用。
//
// 自包含：本包不依赖 server.go / store.go / notify 等外部模块，通知通过 Notifier 回调注入。
package approval

import (
	"errors"
	"time"
)

// StepMode 审批步骤模式。
type StepMode string

const (
	// StepSequential 逐级审批：按 Approvers 顺序每人审批，全部通过才进入下一步。
	StepSequential StepMode = "sequential"
	// StepCountersign 会签：所有 Approvers 都需审批且全部同意才通过。
	StepCountersign StepMode = "countersign"
	// StepAnyOf 或签：任一 Approver 同意即通过，任一拒绝即拒绝。
	StepAnyOf StepMode = "anyof"
)

// 触发类型常量（与 preset.DefaultFlows 对应）。
const (
	TriggerShell        = "shell"         // Shell 命令执行
	TriggerBatchRestart = "batch_restart" // 批量重启
	TriggerConfigChange = "config_change" // 配置变更
	TriggerK8sDelete    = "k8s_delete"    // K8s 资源删除
	TriggerDeploy       = "deploy"        // 部署
)

// 风险等级常量。
const (
	RiskHigh   = "high"
	RiskMedium = "medium"
	RiskLow    = "low"
)

// ApprovalFlow 审批流定义。
//
// 一个审批流绑定（租户, 触发类型），描述该场景下的多级审批步骤。
// 同一租户下同一 TriggerType 仅允许一个启用流（由 Engine 维护不变量）。
type ApprovalFlow struct {
	ID          string         // 全局唯一 ID
	Name        string         // 流名称
	TenantID    string         // 租户 ID
	TriggerType string         // 触发类型（shell/batch_restart/config_change/k8s_delete/deploy）
	Steps       []ApprovalStep // 审批步骤（按 Order 升序执行）
	Enabled     bool           // 是否启用
	CreatedAt   time.Time      // 创建时间
	UpdatedAt   time.Time      // 最近更新时间
}

// ApprovalStep 审批步骤定义。
type ApprovalStep struct {
	ID        string        // 步骤 ID（流内唯一）
	Name      string        // 步骤名称
	Order     int           // 步骤顺序（1=第一级，2=第二级...）
	Mode      StepMode      // 审批模式：sequential/countersign/anyof
	Approvers []string      // 审批人 userID 列表
	Timeout   time.Duration // 审批超时（<=0 表示不超时）
}

// Validate 校验审批流定义合法性。
//   - ID / Name / TenantID / TriggerType 非空。
//   - Steps.Order 从 1 起且连续唯一；每步 ID 非空、Mode 合法、Approvers 非空。
func (f *ApprovalFlow) Validate() error {
	if f.ID == "" {
		return errors.New("approval: flow ID is required")
	}
	if f.Name == "" {
		return errors.New("approval: flow Name is required")
	}
	if f.TenantID == "" {
		return errors.New("approval: flow TenantID is required")
	}
	if f.TriggerType == "" {
		return errors.New("approval: flow TriggerType is required")
	}
	if len(f.Steps) == 0 {
		return errors.New("approval: flow must have at least one step")
	}
	seenOrder := make(map[int]bool, len(f.Steps))
	seenID := make(map[string]bool, len(f.Steps))
	for i, s := range f.Steps {
		if s.ID == "" {
			return errors.New("approval: step ID is required")
		}
		if seenID[s.ID] {
			return errors.New("approval: duplicate step ID: " + s.ID)
		}
		seenID[s.ID] = true
		if s.Order <= 0 {
			return errors.New("approval: step Order must be >= 1")
		}
		if seenOrder[s.Order] {
			return errors.New("approval: duplicate step Order")
		}
		seenOrder[s.Order] = true
		switch s.Mode {
		case StepSequential, StepCountersign, StepAnyOf:
		default:
			return errors.New("approval: invalid step Mode: " + string(s.Mode))
		}
		if len(s.Approvers) == 0 {
			return errors.New("approval: step Approvers must not be empty")
		}
		// 顺序校验：Steps 按 Order 升序排列（允许调用方乱序，但建议升序）。
		if i > 0 && f.Steps[i-1].Order >= s.Order {
			return errors.New("approval: steps must be sorted by Order ascending")
		}
	}
	return nil
}

// StepByOrder 按 Order 查找步骤。未找到返回 nil。
func (f *ApprovalFlow) StepByOrder(order int) *ApprovalStep {
	for i := range f.Steps {
		if f.Steps[i].Order == order {
			return &f.Steps[i]
		}
	}
	return nil
}

// StepByID 按 ID 查找步骤。未找到返回 nil。
func (f *ApprovalFlow) StepByID(id string) *ApprovalStep {
	for i := range f.Steps {
		if f.Steps[i].ID == id {
			return &f.Steps[i]
		}
	}
	return nil
}

// LastOrder 返回最后一步的 Order（流已校验升序）。无步骤返回 0。
func (f *ApprovalFlow) LastOrder() int {
	if len(f.Steps) == 0 {
		return 0
	}
	return f.Steps[len(f.Steps)-1].Order
}
