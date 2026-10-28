package models

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

func marshalToObject(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %T: %v (json=%s)", v, err, raw)
	}
	return out
}

func assertKeySet(t *testing.T, obj map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("键集合 = %v, want %v", got, want)
	}
}

func TestRunbookJSONContract(t *testing.T) {
	now := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	runbook := Runbook{
		ID:          "rb-1",
		Name:        "restart nginx",
		Description: "d",
		Triggers:    []TriggerCondition{{EventType: "alert", Filters: map[string]string{"severity": "high"}}},
		Steps: []Step{{
			Name:    "restart",
			Action:  "shell",
			Target:  "web-01",
			Command: "systemctl restart nginx",
			Timeout: 30 * time.Second,
			OnError: "stop",
		}},
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	obj := marshalToObject(t, runbook)
	// Content 为空时省略；其余字段恒在。
	assertKeySet(t, obj, "id", "name", "description", "triggers", "steps", "enabled",
		"created_at", "updated_at")
	if obj["enabled"] != true {
		t.Fatalf("enabled = %v, want true", obj["enabled"])
	}

	steps, ok := obj["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("steps = %#v", obj["steps"])
	}
	step, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("steps[0] 不是对象: %#v", steps[0])
	}
	assertKeySet(t, step, "name", "action", "target", "command", "timeout", "on_error")
	if step["timeout"] != float64(30*time.Second) {
		t.Fatalf("timeout = %v, want 纳秒整数 %v", step["timeout"], float64(30*time.Second))
	}

	trigger, ok := obj["triggers"].([]any)[0].(map[string]any)
	if !ok {
		t.Fatalf("triggers[0] 不是对象")
	}
	assertKeySet(t, trigger, "event_type", "filters")

	// Content 是前端编辑器的文本契约，与 Steps 并存；非空时必须出现。
	runbook.Content = "steps:\n- restart\n"
	obj = marshalToObject(t, runbook)
	if obj["content"] != "steps:\n- restart\n" {
		t.Fatalf("content = %v", obj["content"])
	}
}

func TestExecutionRecordJSONContract(t *testing.T) {
	now := time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC)
	record := ExecutionRecord{
		ID:          "exec-1",
		RunbookID:   "rb-1",
		TriggeredBy: "alice",
		Status:      "success",
		StepResults: []StepResult{{
			StepName:  "restart",
			Action:    "shell",
			Status:    "success",
			Output:    "ok",
			Duration:  1500 * time.Millisecond,
			StartedAt: now,
		}},
		StartedAt:   now,
		CompletedAt: now.Add(time.Second),
	}

	obj := marshalToObject(t, record)
	assertKeySet(t, obj, "id", "runbook_id", "triggered_by", "status", "step_results",
		"started_at", "completed_at")
	if _, ok := obj["error_message"]; ok {
		t.Fatal("空 error_message 应省略")
	}

	results, ok := obj["step_results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("step_results = %#v", obj["step_results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("step_results[0] 不是对象")
	}
	assertKeySet(t, result, "step_name", "action", "status", "output", "duration", "started_at")
	if result["duration"] != float64(1500*time.Millisecond) {
		t.Fatalf("duration = %v, want 纳秒整数", result["duration"])
	}

	record.ErrorMessage = "boom"
	if obj = marshalToObject(t, record); obj["error_message"] != "boom" {
		t.Fatalf("error_message = %v", obj["error_message"])
	}
}

func TestWebhookPayloadJSONContract(t *testing.T) {
	obj := marshalToObject(t, WebhookPayload{EventType: "alert", Source: "prometheus",
		Data: map[string]string{"device": "web-01"}})
	assertKeySet(t, obj, "event_type", "source", "data")
	if obj["event_type"] != "alert" {
		t.Fatalf("event_type = %v", obj["event_type"])
	}
}
