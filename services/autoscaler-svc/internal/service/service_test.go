package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/evaluator"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/k8s"
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
	return 0, fmt.Errorf("metric not found: %s", key)
}

func newTestService() *Service {
	eng := evaluator.NewEvaluator(func() time.Time {
		return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	})
	scaler := k8s.NewClient()
	reader := &mockMetricsReader{
		values: map[string]float64{
			"web-app/default/cpu_usage": 50.0,
		},
	}
	return NewService(eng, reader, scaler)
}

func TestCreateRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	rule, err := svc.CreateRule(ctx, &models.ScaleRule{
		Name:               "High CPU",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	if rule.ID == "" {
		t.Error("expected rule ID to be set")
	}
	if rule.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if rule.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}
}

func TestCreateRuleNil(t *testing.T) {
	svc := newTestService()
	_, err := svc.CreateRule(context.Background(), nil)
	if err != ErrRuleInvalid {
		t.Fatalf("expected ErrRuleInvalid, got: %v", err)
	}
}

func TestGetRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateRule(ctx, &models.ScaleRule{
		Name:               "Test",
		Deployment:         "web-app",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	got, err := svc.GetRule(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetRule failed: %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("expected ID %s, got %s", created.ID, got.ID)
	}
	if got.Name != "Test" {
		t.Errorf("expected name Test, got %s", got.Name)
	}
}

func TestGetRuleNotFound(t *testing.T) {
	svc := newTestService()
	_, err := svc.GetRule(context.Background(), "nonexistent")
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound, got: %v", err)
	}
}

func TestListRules(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.CreateRule(ctx, &models.ScaleRule{
			Name:               "Rule",
			Deployment:         "web-app",
			Metric:             "cpu_usage",
			ScaleUpThreshold:   80.0,
			ScaleDownThreshold: 20.0,
			MinReplicas:        1,
			MaxReplicas:        10,
			Enabled:            true,
		})
		if err != nil {
			t.Fatalf("CreateRule failed: %v", err)
		}
	}

	rules, err := svc.ListRules(ctx)
	if err != nil {
		t.Fatalf("ListRules failed: %v", err)
	}

	if len(rules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(rules))
	}
}

func TestUpdateRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateRule(ctx, &models.ScaleRule{
		Name:               "Original",
		Deployment:         "web-app",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	created.Name = "Updated"
	created.ScaleUpThreshold = 85.0

	updated, err := svc.UpdateRule(ctx, created)
	if err != nil {
		t.Fatalf("UpdateRule failed: %v", err)
	}

	if updated.Name != "Updated" {
		t.Errorf("expected name Updated, got %s", updated.Name)
	}
	if updated.ScaleUpThreshold != 85.0 {
		t.Errorf("expected threshold 85.0, got %f", updated.ScaleUpThreshold)
	}
}

func TestUpdateRuleNotFound(t *testing.T) {
	svc := newTestService()
	_, err := svc.UpdateRule(context.Background(), &models.ScaleRule{
		ID:                 "nonexistent",
		Name:               "Ghost",
		Deployment:         "web-app",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
	})
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound, got: %v", err)
	}
}

func TestDeleteRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateRule(ctx, &models.ScaleRule{
		Name:               "ToDelete",
		Deployment:         "web-app",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	err = svc.DeleteRule(ctx, created.ID)
	if err != nil {
		t.Fatalf("DeleteRule failed: %v", err)
	}

	_, err = svc.GetRule(ctx, created.ID)
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound after delete, got: %v", err)
	}
}

func TestDeleteRuleNotFound(t *testing.T) {
	svc := newTestService()
	err := svc.DeleteRule(context.Background(), "nonexistent")
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound, got: %v", err)
	}
}

func TestEvaluate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateRule(ctx, &models.ScaleRule{
		Name:               "High CPU",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	svc.scaler.RegisterDeployment("web-app", "default", 3)

	resp, err := svc.Evaluate(ctx, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if len(resp.Decisions) != 1 {
		t.Errorf("expected 1 decision, got %d", len(resp.Decisions))
	}
}

func TestGetDecisions(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateRule(ctx, &models.ScaleRule{
		Name:               "Test",
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu_usage",
		ScaleUpThreshold:   80.0,
		ScaleDownThreshold: 20.0,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	svc.scaler.RegisterDeployment("web-app", "default", 3)

	_, err = svc.Evaluate(ctx, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	decisions := svc.GetDecisions(ctx)
	if len(decisions) != 1 {
		t.Errorf("expected 1 decision, got %d", len(decisions))
	}
}
