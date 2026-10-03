package automation

import (
	"fmt"
	"testing"
	"time"
)

func TestValidTriggerType(t *testing.T) {
	valid := []TriggerType{TriggerTypeAlert, TriggerTypeMetricThreshold, TriggerTypeSchedule, TriggerTypeEvent}
	for _, vt := range valid {
		if !ValidTriggerType(vt) {
			t.Errorf("ValidTriggerType(%q) = false, want true", vt)
		}
	}
	if ValidTriggerType("invalid") {
		t.Error("ValidTriggerType(invalid) = true, want false")
	}
}

func TestValidActionType(t *testing.T) {
	valid := []ActionType{ActionTypeExecuteTask, ActionTypeSendNotify, ActionTypeScale, ActionTypeRestart, ActionTypeIsolate}
	for _, vt := range valid {
		if !ValidActionType(vt) {
			t.Errorf("ValidActionType(%q) = false, want true", vt)
		}
	}
	if ValidActionType("invalid") {
		t.Error("ValidActionType(invalid) = true, want false")
	}
}

func TestAllTriggerTypes(t *testing.T) {
	all := AllTriggerTypes()
	if len(all) != 4 {
		t.Fatalf("AllTriggerTypes() = %d, want 4", len(all))
	}
}

func TestAllActionTypes(t *testing.T) {
	all := AllActionTypes()
	if len(all) != 5 {
		t.Fatalf("AllActionTypes() = %d, want 5", len(all))
	}
}

func TestValidateRule(t *testing.T) {
	validRule := &Rule{
		Name:    "test-rule",
		Trigger: Trigger{Type: TriggerTypeAlert},
		Actions: []Action{{Type: ActionTypeExecuteTask}},
	}
	if err := ValidateRule(validRule); err != nil {
		t.Errorf("ValidateRule(valid) = %v, want nil", err)
	}

	tests := []struct {
		name string
		rule *Rule
	}{
		{"nil rule", nil},
		{"empty name", &Rule{Name: "", Trigger: Trigger{Type: TriggerTypeAlert}, Actions: []Action{{Type: ActionTypeExecuteTask}}}},
		{"invalid trigger", &Rule{Name: "r", Trigger: Trigger{Type: "bad"}, Actions: []Action{{Type: ActionTypeExecuteTask}}}},
		{"no actions", &Rule{Name: "r", Trigger: Trigger{Type: TriggerTypeAlert}, Actions: nil}},
		{"invalid action", &Rule{Name: "r", Trigger: Trigger{Type: TriggerTypeAlert}, Actions: []Action{{Type: "bad"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateRule(tt.rule); err == nil {
				t.Errorf("ValidateRule(%s) = nil, want error", tt.name)
			}
		})
	}
}

func TestEvaluate_DisabledRule(t *testing.T) {
	e := NewEngine()
	rule := &Rule{Enabled: false, Trigger: Trigger{Type: TriggerTypeAlert}}
	if e.Evaluate(rule, map[string]string{"alert": "test"}) {
		t.Error("Evaluate(disabled rule) = true, want false")
	}
}

func TestEvaluate_NilRule(t *testing.T) {
	e := NewEngine()
	if e.Evaluate(nil, nil) {
		t.Error("Evaluate(nil) = true, want false")
	}
}

func TestEvaluate_AlertTrigger(t *testing.T) {
	e := NewEngine()
	rule := &Rule{Enabled: true, Trigger: Trigger{Type: TriggerTypeAlert}}
	if !e.Evaluate(rule, map[string]string{"alert": "cpu_high"}) {
		t.Error("Evaluate(alert, with alert ctx) = false, want true")
	}
	if e.Evaluate(rule, map[string]string{}) {
		t.Error("Evaluate(alert, empty ctx) = true, want false")
	}
}

func TestEvaluate_MetricThreshold(t *testing.T) {
	e := NewEngine()
	rule := &Rule{Enabled: true, Trigger: Trigger{
		Type:   TriggerTypeMetricThreshold,
		Params: map[string]string{"threshold": "80"},
	}}
	if !e.Evaluate(rule, map[string]string{"value": "90"}) {
		t.Error("Evaluate(metric, value>threshold) = false, want true")
	}
	if e.Evaluate(rule, map[string]string{"value": "70"}) {
		t.Error("Evaluate(metric, value<threshold) = true, want false")
	}
	if e.Evaluate(rule, map[string]string{}) {
		t.Error("Evaluate(metric, empty ctx) = true, want false")
	}
}

// TestEvaluate_ScheduleRequiresCronExpr 回归：schedule 触发器此前**无条件返回 true**，
// 而实际调用方是每 30s 遍历全部 enabled 规则的评估循环——于是「创建一个没配 cron 的
// 定时规则」就足以让动作进入无节流重复执行。现要求带合法 cron 表达式。
func TestEvaluate_ScheduleRequiresCronExpr(t *testing.T) {
	e := NewEngine()
	rule := &Rule{ID: "r1", Enabled: true, Trigger: Trigger{Type: TriggerTypeSchedule}}
	if e.Evaluate(rule, nil) {
		t.Error("Evaluate(schedule, 无 cron 表达式) = true, want false（缺表达式不得触发）")
	}
	// 非法表达式同样不触发。
	rule.Trigger.Params = map[string]string{"schedule": "not a cron expr"}
	if e.Evaluate(rule, nil) {
		t.Error("Evaluate(schedule, 非法 cron) = true, want false")
	}
}

// TestEvaluate_ScheduleCronMatch 验证 schedule 按 cron 表达式求值，而非恒命中。
func TestEvaluate_ScheduleCronMatch(t *testing.T) {
	base := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		expr string
		now  time.Time
		want bool
	}{
		{"命中分钟", "0 9 * * *", base, true},
		{"未到分钟", "30 9 * * *", base, false},
		{"不同小时", "0 8 * * *", base, false},
		{"每分钟通配在命中时为真", "* * * * *", base, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := NewEngine()
			at := c.now
			e.SetClock(func() time.Time { return at })
			rule := &Rule{ID: "r-" + c.name, Enabled: true, Trigger: Trigger{
				Type:   TriggerTypeSchedule,
				Params: map[string]string{"schedule": c.expr},
			}}
			if got := e.Evaluate(rule, nil); got != c.want {
				t.Errorf("Evaluate(expr=%q, now=%s) = %v, want %v", c.expr, c.now.Format(time.RFC3339), got, c.want)
			}
		})
	}
}

// TestEvaluate_ScheduleDedup 验证最小触发间隔去重：评估循环周期（30s）小于 cron 粒度（1min）时，
// 同一分钟内重复评估不得重复触发（否则每分钟会执行两次动作）。
func TestEvaluate_ScheduleDedup(t *testing.T) {
	e := NewEngine()
	rule := &Rule{ID: "r-dedup", Enabled: true, Trigger: Trigger{
		Type:   TriggerTypeSchedule,
		Params: map[string]string{"schedule": "* * * * *"},
	}}

	base := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	at := base
	e.SetClock(func() time.Time { return at })

	if !e.Evaluate(rule, nil) {
		t.Fatal("首次评估应触发")
	}
	// +30s：评估循环的第二次 tick，落在同一分钟内 → 应被去重。
	at = base.Add(30 * time.Second)
	if e.Evaluate(rule, nil) {
		t.Error("同分钟内第二次评估应被去重（触发间隔下限 1 分钟）")
	}
	// +61s：跨到下一分钟 → 应再次触发。
	at = base.Add(61 * time.Second)
	if !e.Evaluate(rule, nil) {
		t.Error("跨分钟后应可再次触发")
	}
}

// TestEvaluate_ScheduleAliasKeys 验证 cron 表达式别名键（cron/expr）与主键等价。
func TestEvaluate_ScheduleAliasKeys(t *testing.T) {
	base := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	for _, k := range []string{"schedule", "cron", "expr"} {
		e := NewEngine()
		at := base
		e.SetClock(func() time.Time { return at })
		rule := &Rule{ID: "r-" + k, Enabled: true, Trigger: Trigger{
			Type:   TriggerTypeSchedule,
			Params: map[string]string{k: "0 9 * * *"},
		}}
		if !e.Evaluate(rule, nil) {
			t.Errorf("params[%q] 未被识别为 cron 表达式", k)
		}
	}
}

// TestEvaluate_EventRequiresEventContext 回归：event 触发器此前与 schedule 同属
// `return true` 分支，在周期评估循环中每 30s 触发一次。现要求事件侧显式注入 ctx。
func TestEvaluate_EventRequiresEventContext(t *testing.T) {
	e := NewEngine()
	rule := &Rule{ID: "r-ev", Enabled: true, Trigger: Trigger{Type: TriggerTypeEvent}}
	if e.Evaluate(rule, map[string]string{}) {
		t.Error("Evaluate(event, 无 ctx) = true, want false")
	}
	if !e.Evaluate(rule, map[string]string{"event": "device.offline"}) {
		t.Error("Evaluate(event, ctx[event] 存在) = false, want true")
	}
}

// TestValidateRule_ScheduleRequiresCronExpr 验证入口校验：缺 cron / 非法 cron 均拒绝创建。
func TestValidateRule_ScheduleRequiresCronExpr(t *testing.T) {
	base := &Rule{
		Name:    "r",
		Enabled: true,
		Trigger: Trigger{Type: TriggerTypeSchedule},
		Actions: []Action{{Type: ActionTypeSendNotify}},
	}
	if err := ValidateRule(base); err == nil {
		t.Error("ValidateRule(schedule 无表达式) = nil, want error")
	}
	bad := *base
	bad.Trigger.Params = map[string]string{"schedule": "99 99 99"}
	if err := ValidateRule(&bad); err == nil {
		t.Error("ValidateRule(schedule 非法表达式) = nil, want error")
	}
	good := *base
	good.Trigger.Params = map[string]string{"schedule": "0 9 * * *"}
	if err := ValidateRule(&good); err != nil {
		t.Errorf("ValidateRule(schedule 合法表达式) = %v, want nil", err)
	}
}

func TestExecute_NoExecutor(t *testing.T) {
	e := NewEngine()
	rule := &Rule{
		ID:      "r1",
		Name:    "test",
		Actions: []Action{{Type: ActionTypeExecuteTask, Params: map[string]string{"device_id": "d1", "command": "echo hi"}}},
	}
	exec := e.Execute(rule)
	if exec == nil {
		t.Fatal("Execute returned nil")
	}
	if exec.Status != ExecutionStatusSucceeded {
		t.Errorf("Status = %q, want succeeded (no executor fallback)", exec.Status)
	}
}

func TestExecute_WithExecutor(t *testing.T) {
	exec := &mockExecutor{}
	e := NewEngineWithExecutor(exec)
	rule := &Rule{
		ID:      "r1",
		Name:    "test",
		Actions: []Action{{Type: ActionTypeExecuteTask, Params: map[string]string{"device_id": "d1", "command": "echo hi"}}},
	}
	result := e.Execute(rule)
	if result.Status != ExecutionStatusSucceeded {
		t.Errorf("Status = %q, want succeeded", result.Status)
	}
	if !exec.taskCalled {
		t.Error("ExecuteTask was not called")
	}
}

func TestExecute_WithExecutorError(t *testing.T) {
	exec := &mockExecutor{failTask: true}
	e := NewEngineWithExecutor(exec)
	rule := &Rule{
		ID:      "r1",
		Name:    "test",
		Actions: []Action{{Type: ActionTypeExecuteTask, Params: map[string]string{"device_id": "d1", "command": "echo hi"}}},
	}
	result := e.Execute(rule)
	if result.Status != ExecutionStatusFailed {
		t.Errorf("Status = %q, want failed", result.Status)
	}
}

var _ Executor = (*mockExecutor)(nil)

type mockExecutor struct {
	taskCalled bool
	failTask   bool
}

func (m *mockExecutor) ExecuteTask(tenantID, deviceID, command string, params map[string]string) (string, error) {
	m.taskCalled = true
	if m.failTask {
		return "", fmt.Errorf("task failed")
	}
	return "task-123", nil
}

func (m *mockExecutor) SendNotify(tenantID, channel, message string, params map[string]string) error {
	return nil
}

func (m *mockExecutor) Scale(tenantID, service string, replicas int, params map[string]string) (string, error) {
	return "scale-1", nil
}

func (m *mockExecutor) Restart(tenantID, target string, params map[string]string) (string, error) {
	return "restart-1", nil
}

func (m *mockExecutor) Isolate(tenantID, deviceID string, params map[string]string) (string, error) {
	return "isolate-1", nil
}
