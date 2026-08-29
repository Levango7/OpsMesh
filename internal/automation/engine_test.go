package automation

import (
	"fmt"
	"testing"
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

func TestEvaluate_ScheduleAlwaysFires(t *testing.T) {
	e := NewEngine()
	rule := &Rule{Enabled: true, Trigger: Trigger{Type: TriggerTypeSchedule}}
	if !e.Evaluate(rule, nil) {
		t.Error("Evaluate(schedule) = false, want true")
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
