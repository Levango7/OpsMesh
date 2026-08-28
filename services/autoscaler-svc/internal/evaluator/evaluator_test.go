package evaluator

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/models"
)

// mockMetricsReader is a mock implementation of MetricsReader.
type mockMetricsReader struct {
	values map[string]float64
	err    error
}

func (m *mockMetricsReader) ReadMetric(deployment, namespace, metric string) (float64, error) {
	if m.err != nil {
		return 0, m.err
	}
	key := deployment + "/" + namespace + "/" + metric
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	return 0, nil
}

// mockK8sScaler is a mock implementation of K8sScaler.
type mockK8sScaler struct {
	replicas map[string]int32
	err      error
}

func (m *mockK8sScaler) GetReplicas(deployment, namespace string) (int32, error) {
	if m.err != nil {
		return 0, m.err
	}
	key := namespace + "/" + deployment
	return m.replicas[key], nil
}

func (m *mockK8sScaler) SetReplicas(deployment, namespace string, replicas int32) error {
	if m.err != nil {
		return m.err
	}
	key := namespace + "/" + deployment
	m.replicas[key] = replicas
	return nil
}

func newTestEvaluator() *Evaluator {
	return NewEvaluator(func() time.Time {
		return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	})
}

func TestAddRule(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "High CPU Scale Up",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	rules := e.ListRules()
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}
}

func TestAddRuleInvalid(t *testing.T) {
	e := newTestEvaluator()

	tests := []struct {
		name string
		rule *models.ScaleRule
	}{
		{"nil rule", nil},
		{"empty ID", &models.ScaleRule{ID: ""}},
		{"negative min", &models.ScaleRule{ID: "r1", MinReplicas: -1, MaxReplicas: 5, ScaleUpThreshold: 80, ScaleDownThreshold: 20}},
		{"max <= min", &models.ScaleRule{ID: "r1", MinReplicas: 5, MaxReplicas: 5, ScaleUpThreshold: 80, ScaleDownThreshold: 20}},
		{"up <= down", &models.ScaleRule{ID: "r1", MinReplicas: 1, MaxReplicas: 10, ScaleUpThreshold: 20, ScaleDownThreshold: 80}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := e.AddRule(tt.rule); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestUpdateRule(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "Original",
		Deployment:         "web-app",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	rule.Name = "Updated"
	rule.ScaleUpThreshold = 85.0
	if err := e.UpdateRule(rule); err != nil {
		t.Fatalf("UpdateRule failed: %v", err)
	}

	updated, err := e.GetRule("rule-1")
	if err != nil {
		t.Fatalf("GetRule failed: %v", err)
	}
	if updated.Name != "Updated" {
		t.Errorf("expected name Updated, got %s", updated.Name)
	}
	if updated.ScaleUpThreshold != 85.0 {
		t.Errorf("expected threshold 85.0, got %f", updated.ScaleUpThreshold)
	}
}

func TestUpdateRuleNotFound(t *testing.T) {
	e := newTestEvaluator()
	err := e.UpdateRule(&models.ScaleRule{ID: "nonexistent"})
	if err == nil {
		t.Error("expected error for nonexistent rule")
	}
}

func TestDeleteRule(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "ToDelete",
		Deployment:         "web-app",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	if err := e.DeleteRule("rule-1"); err != nil {
		t.Fatalf("DeleteRule failed: %v", err)
	}

	_, err := e.GetRule("rule-1")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestDeleteRuleNotFound(t *testing.T) {
	e := newTestEvaluator()
	err := e.DeleteRule("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent rule")
	}
}

func TestEvaluateScaleUp(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "High CPU",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 95.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 3},
	}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}

	d := decisions[0]
	if d.Action != "scale_up" {
		t.Errorf("expected scale_up, got %s", d.Action)
	}
	if d.FromReplicas != 3 {
		t.Errorf("expected from 3, got %d", d.FromReplicas)
	}
	if d.ToReplicas != 4 {
		t.Errorf("expected to 4, got %d", d.ToReplicas)
	}
}

func TestEvaluateScaleDown(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "Low CPU",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 10.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 5},
	}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}

	d := decisions[0]
	if d.Action != "scale_down" {
		t.Errorf("expected scale_down, got %s", d.Action)
	}
	if d.FromReplicas != 5 {
		t.Errorf("expected from 5, got %d", d.FromReplicas)
	}
	if d.ToReplicas != 4 {
		t.Errorf("expected to 4, got %d", d.ToReplicas)
	}
}

func TestEvaluateNoAction(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "Normal",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 50.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 3},
	}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}

	if decisions[0].Action != "no_action" {
		t.Errorf("expected no_action, got %s", decisions[0].Action)
	}
}

func TestEvaluateMaxReplicasLimit(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "At Max",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        5,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 95.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 5},
	}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if decisions[0].Action != "no_action" {
		t.Errorf("expected no_action at max, got %s", decisions[0].Action)
	}
	if decisions[0].ToReplicas != 5 {
		t.Errorf("expected to remain at 5, got %d", decisions[0].ToReplicas)
	}
}

func TestEvaluateMinReplicasLimit(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "At Min",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        2,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 5.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 2},
	}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if decisions[0].Action != "no_action" {
		t.Errorf("expected no_action at min, got %s", decisions[0].Action)
	}
	if decisions[0].ToReplicas != 2 {
		t.Errorf("expected to remain at 2, got %d", decisions[0].ToReplicas)
	}
}

func TestEvaluateDisabledRule(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "Disabled",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            false,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 95.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 3},
	}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if len(decisions) != 0 {
		t.Errorf("expected 0 decisions for disabled rule, got %d", len(decisions))
	}
}

func TestEvaluateCooldown(t *testing.T) {
	e := newTestEvaluator()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	e.now = func() time.Time { return now }

	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "Cooldown Test",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		CooldownUp:        60 * time.Second,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 95.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 3},
	}

	decisions, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if decisions[0].Action != "scale_up" {
		t.Fatalf("expected first scale_up, got %s", decisions[0].Action)
	}

	scaler.replicas["default/web-app"] = 4

	decisions, err = e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if decisions[0].Action != "no_action" {
		t.Errorf("expected no_action during cooldown, got %s", decisions[0].Action)
	}
	if decisions[0].Reason != "scale up in cooldown period" {
		t.Errorf("expected cooldown reason, got %s", decisions[0].Reason)
	}
}

func TestDecisionsHistory(t *testing.T) {
	e := newTestEvaluator()
	rule := &models.ScaleRule{
		ID:                 "rule-1",
		Name:               "History Test",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}

	if err := e.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	reader := &mockMetricsReader{
		values: map[string]float64{"web-app/default/cpu_usage": 95.0},
	}
	scaler := &mockK8sScaler{
		replicas: map[string]int32{"default/web-app": 3},
	}

	_, err := e.Evaluate(reader, scaler, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	decisions := e.Decisions()
	if len(decisions) != 1 {
		t.Errorf("expected 1 decision in history, got %d", len(decisions))
	}
}
