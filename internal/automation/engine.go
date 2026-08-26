package automation

// Package automation 实现自动化闭环引擎：规则（条件→动作）、执行记录、规则引擎。
//
// 设计要点：
//   - 纯领域模型，无外部依赖，可被 controlplane/store 复用；
//   - 触发器类型：alert/metric_threshold/schedule/event；
//   - 动作类型：execute_task/send_notify/scale/restart/isolate；
//   - 规则引擎 Evaluate 方法判定触发条件是否满足（MVP 桩实现）。

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TriggerType 触发器类型。
type TriggerType string

const (
	TriggerTypeAlert           TriggerType = "alert"            // 告警触发
	TriggerTypeMetricThreshold TriggerType = "metric_threshold" // 指标阈值触发
	TriggerTypeSchedule        TriggerType = "schedule"         // 定时触发
	TriggerTypeEvent           TriggerType = "event"            // 事件触发
)

// AllTriggerTypes 返回全部预置触发器类型。
func AllTriggerTypes() []TriggerType {
	return []TriggerType{TriggerTypeAlert, TriggerTypeMetricThreshold, TriggerTypeSchedule, TriggerTypeEvent}
}

// ValidTriggerType 校验触发器类型是否合法。
func ValidTriggerType(t TriggerType) bool {
	switch t {
	case TriggerTypeAlert, TriggerTypeMetricThreshold, TriggerTypeSchedule, TriggerTypeEvent:
		return true
	}
	return false
}

// ActionType 动作类型。
type ActionType string

const (
	ActionTypeExecuteTask ActionType = "execute_task" // 执行任务
	ActionTypeSendNotify  ActionType = "send_notify"  // 发送通知
	ActionTypeScale       ActionType = "scale"        // 扩缩容
	ActionTypeRestart     ActionType = "restart"      // 重启
	ActionTypeIsolate     ActionType = "isolate"      // 隔离
)

// AllActionTypes 返回全部预置动作类型。
func AllActionTypes() []ActionType {
	return []ActionType{ActionTypeExecuteTask, ActionTypeSendNotify, ActionTypeScale, ActionTypeRestart, ActionTypeIsolate}
}

// ValidActionType 校验动作类型是否合法。
func ValidActionType(a ActionType) bool {
	switch a {
	case ActionTypeExecuteTask, ActionTypeSendNotify, ActionTypeScale, ActionTypeRestart, ActionTypeIsolate:
		return true
	}
	return false
}

// Trigger 触发器定义。
type Trigger struct {
	Type   TriggerType       `json:"type"`
	Params map[string]string `json:"params"` // 触发参数（如 metric=cpu, threshold=90）
}

// Action 动作定义。
type Action struct {
	Type   ActionType        `json:"type"`
	Params map[string]string `json:"params"` // 动作参数（如 task_id, target, notify_channel）
}

// Rule 自动化规则（条件→动作）。
type Rule struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenantID"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Trigger     Trigger   `json:"trigger"`
	Actions     []Action  `json:"actions"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ExecutionStatus 执行状态。
type ExecutionStatus string

const (
	ExecutionStatusPending   ExecutionStatus = "pending"
	ExecutionStatusRunning   ExecutionStatus = "running"
	ExecutionStatusSucceeded ExecutionStatus = "succeeded"
	ExecutionStatusFailed    ExecutionStatus = "failed"
	ExecutionStatusSkipped   ExecutionStatus = "skipped" // 条件不满足
)

// Execution 自动化规则执行记录。
type Execution struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenantID"`
	RuleID    string          `json:"ruleID"`
	RuleName  string          `json:"ruleName"`
	Trigger   Trigger         `json:"trigger"`
	Actions   []Action        `json:"actions"`
	Status    ExecutionStatus `json:"status"`
	Detail    string          `json:"detail"` // 执行详情/错误信息
	StartedAt time.Time       `json:"startedAt"`
	EndedAt   *time.Time      `json:"endedAt,omitempty"`
}

// ValidateRule 校验规则定义合法性。
func ValidateRule(r *Rule) error {
	if r == nil {
		return fmt.Errorf("rule is nil")
	}
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("rule name is required")
	}
	if !ValidTriggerType(r.Trigger.Type) {
		return fmt.Errorf("invalid trigger type: %q", r.Trigger.Type)
	}
	if len(r.Actions) == 0 {
		return fmt.Errorf("rule must have at least one action")
	}
	for i, a := range r.Actions {
		if !ValidActionType(a.Type) {
			return fmt.Errorf("action[%d] invalid type: %q", i, a.Type)
		}
	}
	return nil
}

// ============================================================================
// 规则引擎
// ============================================================================

// Engine 自动化规则引擎（MVP 桩实现）。
type Engine struct{}

// NewEngine 构造自动化规则引擎。
func NewEngine() *Engine {
	return &Engine{}
}

// Evaluate 评估规则是否应触发（MVP：始终返回 true 表示触发，生产实现应检查触发条件）。
//
// ctx 包含触发上下文（如告警事件、指标值、定时时刻）。
func (e *Engine) Evaluate(rule *Rule, ctx map[string]string) bool {
	if rule == nil || !rule.Enabled {
		return false
	}
	// MVP：alert 类型只要有 ctx["alert"] 即触发；
	// metric_threshold 类型比较 ctx["value"] 与 trigger.Params["threshold"]；
	// schedule/event 类型始终触发（由调度器/事件总线决定何时调用）。
	switch rule.Trigger.Type {
	case TriggerTypeAlert:
		_, ok := ctx["alert"]
		return ok
	case TriggerTypeMetricThreshold:
		val, ok1 := ctx["value"]
		thresh, ok2 := rule.Trigger.Params["threshold"]
		if !ok1 || !ok2 {
			return false
		}
		v, err1 := strconv.ParseFloat(val, 64)
		t, err2 := strconv.ParseFloat(thresh, 64)
		if err1 != nil || err2 != nil {
			// 解析失败：指标值/阈值非数字，不触发
			return false
		}
		return v >= t
	case TriggerTypeSchedule, TriggerTypeEvent:
		return true
	}
	return false
}

// Execute 执行规则的动作（MVP：返回成功执行记录，不实际执行）。
func (e *Engine) Execute(rule *Rule) *Execution {
	now := time.Now()
	exec := &Execution{
		ID:        fmt.Sprintf("exec-%d", now.UnixNano()),
		TenantID:  rule.TenantID,
		RuleID:    rule.ID,
		RuleName:  rule.Name,
		Trigger:   rule.Trigger,
		Actions:   rule.Actions,
		Status:    ExecutionStatusSucceeded,
		StartedAt: now,
	}
	end := now
	exec.EndedAt = &end
	return exec
}

// TestRule 测试规则（不实际执行，返回模拟执行记录）。
func (e *Engine) TestRule(rule *Rule) *Execution {
	exec := e.Execute(rule)
	exec.Status = ExecutionStatusSucceeded
	exec.Detail = "test execution (no actual side effect)"
	return exec
}
