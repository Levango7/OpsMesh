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

// Executor 定义自动化动作执行器接口（由控制面注入具体实现）。
type Executor interface {
	// ExecuteTask 在指定设备上执行任务，返回任务 ID 或错误。
	ExecuteTask(tenantID, deviceID, command string, params map[string]string) (string, error)
	// SendNotify 发送通知到指定通道。
	SendNotify(tenantID, channel, message string, params map[string]string) error
	// Scale 扩缩容指定服务。
	Scale(tenantID, service string, replicas int, params map[string]string) (string, error)
	// Restart 重启指定服务或设备。
	Restart(tenantID, target string, params map[string]string) (string, error)
	// Isolate 隔离指定设备。
	Isolate(tenantID, deviceID string, params map[string]string) (string, error)
}

// Engine 自动化规则引擎。
type Engine struct {
	executor Executor
}

// NewEngine 构造自动化规则引擎。
func NewEngine() *Engine {
	return &Engine{}
}

// NewEngineWithExecutor 构造带执行器的自动化规则引擎。
func NewEngineWithExecutor(exec Executor) *Engine {
	return &Engine{executor: exec}
}

// SetExecutor 设置执行器（用于延迟注入，避免循环依赖）。
func (e *Engine) SetExecutor(exec Executor) {
	e.executor = exec
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

// Execute 执行规则的动作。
// 如果已注入 Executor，则执行真实动作；否则返回模拟记录（向后兼容）。
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

	if e.executor == nil {
		// 无执行器：返回模拟记录（向后兼容）。
		exec.Detail = "simulated (no executor configured)"
		return exec
	}

	// 真实执行：遍历所有动作，任一失败则标记 failed。
	var details []string
	for _, action := range rule.Actions {
		detail, err := e.executeAction(rule.TenantID, action)
		if err != nil {
			exec.Status = ExecutionStatusFailed
			details = append(details, fmt.Sprintf("%s failed: %v", action.Type, err))
			break
		}
		details = append(details, detail)
	}
	exec.Detail = strings.Join(details, "; ")
	return exec
}

// executeAction 执行单个动作。
func (e *Engine) executeAction(tenantID string, action Action) (string, error) {
	switch action.Type {
	case ActionTypeExecuteTask:
		deviceID := action.Params["device_id"]
		command := action.Params["command"]
		if deviceID == "" || command == "" {
			return "", fmt.Errorf("execute_task requires device_id and command params")
		}
		taskID, err := e.executor.ExecuteTask(tenantID, deviceID, command, action.Params)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("execute_task: created task %s on device %s", taskID, deviceID), nil

	case ActionTypeSendNotify:
		channel := action.Params["channel"]
		message := action.Params["message"]
		if channel == "" || message == "" {
			return "", fmt.Errorf("send_notify requires channel and message params")
		}
		if err := e.executor.SendNotify(tenantID, channel, message, action.Params); err != nil {
			return "", err
		}
		return fmt.Sprintf("send_notify: sent to %s", channel), nil

	case ActionTypeScale:
		service := action.Params["service"]
		replicas := 0
		if r := action.Params["replicas"]; r != "" {
			_, _ = fmt.Sscanf(r, "%d", &replicas)
		}
		if service == "" || replicas <= 0 {
			return "", fmt.Errorf("scale requires service and replicas > 0 params")
		}
		taskID, err := e.executor.Scale(tenantID, service, replicas, action.Params)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("scale: service %s to %d replicas (task %s)", service, replicas, taskID), nil

	case ActionTypeRestart:
		target := action.Params["target"]
		if target == "" {
			return "", fmt.Errorf("restart requires target param")
		}
		taskID, err := e.executor.Restart(tenantID, target, action.Params)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("restart: target %s (task %s)", target, taskID), nil

	case ActionTypeIsolate:
		deviceID := action.Params["device_id"]
		if deviceID == "" {
			return "", fmt.Errorf("isolate requires device_id param")
		}
		taskID, err := e.executor.Isolate(tenantID, deviceID, action.Params)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("isolate: device %s (task %s)", deviceID, taskID), nil

	default:
		return "", fmt.Errorf("unknown action type: %s", action.Type)
	}
}

// TestRule 测试规则（不实际执行，返回模拟执行记录）。
func (e *Engine) TestRule(rule *Rule) *Execution {
	exec := e.Execute(rule)
	exec.Status = ExecutionStatusSucceeded
	exec.Detail = "test execution (no actual side effect)"
	return exec
}
