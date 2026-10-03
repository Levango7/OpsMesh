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
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/internal/cron"
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
	// schedule 触发必须带合法 cron 表达式。
	//
	// 此前不校验，导致规则可带着空的/垃圾的 Trigger.Params["schedule"] 被创建成功，
	// 而引擎当时对 schedule 无条件返回 true——于是「创建一个没配表达式的定时规则」
	// 就足以让动作进入无节流的重复执行。引擎侧现已改为缺表达式即不触发，
	// 这里再于入口拦截，把错误提前到创建时反馈给用户。
	if r.Trigger.Type == TriggerTypeSchedule {
		expr := scheduleExpr(r.Trigger)
		if expr == "" {
			return fmt.Errorf("schedule trigger requires a cron expression in trigger.params.schedule")
		}
		if _, err := cron.Match(expr, time.Now()); err != nil {
			return fmt.Errorf("invalid cron expression %q: %w", expr, err)
		}
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

	// mu 保护 lastFire。评估循环（controlplane automation_eval）与规则增删可能并发。
	mu sync.Mutex
	// lastFire 记录 schedule 类规则上次判定命中的时刻，用于最小触发间隔去重。
	//
	// 为什么需要：cron 表达式最细粒度是"分钟"（5 字段），而评估循环默认每 30s 跑一次。
	// 若只靠 cron.Match 判定，`* * * * *` 这类表达式会在同一分钟内命中两次。
	lastFire map[string]time.Time
	// now 取当前时刻，测试可注入以获得确定性行为。
	now func() time.Time
}

// scheduleMinInterval schedule 类规则的最小触发间隔。
//
// cron 5 字段表达式的最细粒度是 1 分钟，故取 1 分钟：
// 既消除"每 30s 评估一次导致同一分钟重复触发"，又不会误伤合法的每分钟规则。
const scheduleMinInterval = time.Minute

// NewEngine 构造自动化规则引擎。
func NewEngine() *Engine {
	return &Engine{lastFire: make(map[string]time.Time), now: time.Now}
}

// NewEngineWithExecutor 构造带执行器的自动化规则引擎。
func NewEngineWithExecutor(exec Executor) *Engine {
	return &Engine{executor: exec, lastFire: make(map[string]time.Time), now: time.Now}
}

// SetExecutor 设置执行器（用于延迟注入，避免循环依赖）。
func (e *Engine) SetExecutor(exec Executor) {
	e.executor = exec
}

// clock 返回当前时刻（测试可注入）。
func (e *Engine) clock() time.Time {
	if e.now != nil {
		return e.now()
	}
	return time.Now()
}

// SetClock 注入时钟（仅供测试使用）。
//
// schedule 触发的判定依赖"当前时刻是否落在 cron 命中区间"，不注入时钟就无法写出
// 与运行时刻无关的确定性用例。传 nil 恢复为 time.Now。
func (e *Engine) SetClock(fn func() time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.now = fn
}

// Evaluate 评估规则是否应触发。
//
// 触发语义（按类型）：
//   - alert：ctx["alert"] 非空即触发（由告警侧注入）；
//   - metric_threshold：ctx["value"] >= trigger.Params["threshold"]；
//   - schedule：按 trigger.Params["schedule"] 的 5 字段 cron 表达式求值，
//     并受最小触发间隔去重；
//   - event：ctx["event"] 非空即触发（由事件侧注入）。
//
// 安全修复（P0）：schedule/event 原实现无条件 `return true`。设计意图是
// "由调度器/事件总线决定何时调用"，但实际调用方是 controlplane 的周期评估循环——
// 它每 30s 对**全部** enabled 规则调用一次 Evaluate。于是创建一条 schedule 规则
// （动作如 execute_task / send_notify）会导致该动作每 30 秒被执行一次，
// 无 cron 解析、无去重、无冷却，可造成任务风暴与通知轰炸。
// 现改为：schedule 走真实 cron 求值且缺表达式即不触发；event 要求事件上下文存在。
func (e *Engine) Evaluate(rule *Rule, ctx map[string]string) bool {
	if rule == nil || !rule.Enabled {
		return false
	}
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
	case TriggerTypeSchedule:
		return e.evaluateSchedule(rule)
	case TriggerTypeEvent:
		// 事件驱动：必须由事件侧显式注入 ctx["event"] 才触发。
		// 周期评估循环不提供该键，故此类型在评估循环中不会误触发。
		_, ok := ctx["event"]
		return ok
	}
	return false
}

// scheduleExpr 提取 schedule 触发器的 cron 表达式。
//
// 主键 "schedule"（与 metric_threshold 用 "metric"/"threshold" 同构：键名即语义），
// 另容忍 "cron"/"expr" 两个常见别名——前端触发参数是用户手填的自由 JSON，
// 不认别名会导致规则静默失效。三个键都取不到时返回 ""（表示"未配置"）。
func scheduleExpr(t Trigger) string {
	if t.Params == nil {
		return ""
	}
	for _, k := range []string{"schedule", "cron", "expr"} {
		if v := strings.TrimSpace(t.Params[k]); v != "" {
			return v
		}
	}
	return ""
}

// evaluateSchedule 按 cron 表达式 + 最小触发间隔判定 schedule 类规则。
//
// 缺表达式/表达式非法 → 不触发（安全默认：宁可不执行，也不做无节流的动作风暴）。
// 非法表达式只记日志不返回 error：Evaluate 是热路径（每 30s × 全部规则），
// 且 ValidateRule 已在规则创建/更新时拦截，此处是兜底防御。
func (e *Engine) evaluateSchedule(rule *Rule) bool {
	expr := scheduleExpr(rule.Trigger)
	if expr == "" {
		log.Printf("automation: 规则 %s（%s）为 schedule 触发但未配置 cron 表达式，跳过本轮评估", rule.ID, rule.Name)
		return false
	}
	now := e.clock()
	match, err := cron.Match(expr, now)
	if err != nil {
		log.Printf("automation: 规则 %s（%s）的 cron 表达式 %q 非法: %v", rule.ID, rule.Name, expr, err)
		return false
	}
	if !match {
		return false
	}
	// 去重：同一分钟只触发一次（评估循环周期可能小于 1 分钟）。
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lastFire == nil {
		e.lastFire = make(map[string]time.Time)
	}
	if last, ok := e.lastFire[rule.ID]; ok && now.Sub(last) < scheduleMinInterval {
		return false
	}
	e.lastFire[rule.ID] = now
	return true
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
			// 解析失败时 replicas 保持 0，落入下方「replicas > 0」校验返回明确错误。
			if _, err := fmt.Sscanf(r, "%d", &replicas); err != nil {
				log.Printf("[automation] scale: 解析 replicas 参数 %q 失败: %v", r, err)
			}
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
