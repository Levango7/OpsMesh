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

func TestScaleRuleJSONContract(t *testing.T) {
	now := time.Date(2026, 7, 8, 9, 10, 11, 0, time.UTC)
	rule := ScaleRule{
		ID:                 "rule-1",
		Name:               "cpu-based",
		Deployment:         "web",
		Namespace:          "prod",
		Metric:             "cpu",
		ScaleUpThreshold:   80,
		ScaleDownThreshold: 20,
		MinReplicas:        2,
		MaxReplicas:        10,
		CooldownUp:         3 * time.Minute,
		CooldownDown:       5 * time.Minute,
		Enabled:            true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	obj := marshalToObject(t, rule)
	assertKeySet(t, obj, "id", "name", "deployment", "namespace", "metric",
		"scaleUpThreshold", "scaleDownThreshold", "minReplicas", "maxReplicas",
		"cooldownUp", "cooldownDown", "enabled", "createdAt", "updatedAt")
	if obj["enabled"] != true || obj["minReplicas"] != float64(2) || obj["maxReplicas"] != float64(10) {
		t.Fatalf("字段值失真: %v", obj)
	}
	// time.Duration 的 JSON 形态是整数纳秒，前端/调用方必须按此解码。
	if obj["cooldownUp"] != float64(3*time.Minute) || obj["cooldownDown"] != float64(5*time.Minute) {
		t.Fatalf("cooldown 序列化 = %v/%v, want 纳秒整数", obj["cooldownUp"], obj["cooldownDown"])
	}
}

func TestScaleDecisionJSONContract(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 11, 12, 0, time.UTC)
	decision := ScaleDecision{
		ID:           "dec-1",
		RuleID:       "rule-1",
		Deployment:   "web",
		Namespace:    "prod",
		Action:       "scale_up",
		FromReplicas: 2,
		ToReplicas:   4,
		Reason:       "cpu > 80%",
		MetricValue:  91.5,
		Timestamp:    now,
	}

	obj := marshalToObject(t, decision)
	// Executor 为空时省略（memory-simulated / k8s-kubeconfig / k8s-in-cluster）。
	assertKeySet(t, obj, "id", "ruleId", "deployment", "namespace", "action",
		"fromReplicas", "toReplicas", "reason", "metricValue", "timestamp")
	if obj["metricValue"] != 91.5 || obj["fromReplicas"] != float64(2) || obj["toReplicas"] != float64(4) {
		t.Fatalf("字段值失真: %v", obj)
	}

	decision.Executor = "k8s-in-cluster"
	obj = marshalToObject(t, decision)
	if obj["executor"] != "k8s-in-cluster" {
		t.Fatalf("executor = %v", obj["executor"])
	}
}

func TestEvaluateAndMetricsJSONContract(t *testing.T) {
	resp := EvaluateResponse{
		Decisions: []ScaleDecision{{ID: "dec-1", Action: "no_action"}},
		Timestamp: time.Unix(0, 0).UTC(),
	}
	obj := marshalToObject(t, resp)
	assertKeySet(t, obj, "decisions", "timestamp")

	metrics := MetricsData{
		Deployment: "web",
		Namespace:  "prod",
		Values:     map[string]float64{"cpu": 42.5},
		Timestamp:  time.Unix(0, 0).UTC(),
	}
	obj = marshalToObject(t, metrics)
	assertKeySet(t, obj, "deployment", "namespace", "values", "timestamp")
	values, ok := obj["values"].(map[string]any)
	if !ok || values["cpu"] != 42.5 {
		t.Fatalf("values = %#v", obj["values"])
	}

	req := marshalToObject(t, EvaluateRequest{RuleID: ""})
	// ruleId 带 omitempty：空串时省略，表示「评估全部规则」。
	assertKeySet(t, req)
}
