package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	alertv1 "github.com/Levango7/OpsMesh/services/alert-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/engine"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/store"
)

func newTestService() *Service {
	eng := engine.NewEngine(nil)
	st := store.NewMemoryStore()
	return NewService(eng, st)
}

func TestCreateRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	req := &alertv1.CreateRuleRequest{
		Rule: &alertv1.AlertRule{
			Name:      "High CPU",
			TenantId:  "tenant-1",
			Metric:    "cpu_usage",
			Op:        ">",
			Threshold: 80.0,
			Duration:  300,
			Severity:  "critical",
			Enabled:   true,
		},
	}

	rule, err := svc.CreateRule(ctx, req)
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	if rule.Id == "" {
		t.Error("expected rule ID to be set")
	}
	if rule.CreatedAt == nil {
		t.Error("expected CreatedAt to be set")
	}
	if rule.UpdatedAt == nil {
		t.Error("expected UpdatedAt to be set")
	}
}

func TestCreateRuleNil(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateRule(ctx, &alertv1.CreateRuleRequest{})
	if err != ErrRuleInvalid {
		t.Fatalf("expected ErrRuleInvalid, got: %v", err)
	}
}

func TestGetRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateRule(ctx, &alertv1.CreateRuleRequest{
		Rule: &alertv1.AlertRule{
			Name:      "High Memory",
			TenantId:  "tenant-1",
			Metric:    "mem_usage",
			Op:        ">",
			Threshold: 90.0,
			Severity:  "warning",
			Enabled:   true,
		},
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	got, err := svc.GetRule(ctx, &alertv1.GetRuleRequest{Id: created.Id})
	if err != nil {
		t.Fatalf("GetRule failed: %v", err)
	}

	if got.Id != created.Id {
		t.Errorf("expected ID %s, got %s", created.Id, got.Id)
	}
	if got.Name != "High Memory" {
		t.Errorf("expected name High Memory, got %s", got.Name)
	}
	if got.Metric != "mem_usage" {
		t.Errorf("expected metric mem_usage, got %s", got.Metric)
	}
}

func TestGetRuleNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.GetRule(ctx, &alertv1.GetRuleRequest{Id: "nonexistent"})
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound, got: %v", err)
	}
}

func TestListRules(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.CreateRule(ctx, &alertv1.CreateRuleRequest{
			Rule: &alertv1.AlertRule{
				Name:      "Rule " + uuid.New().String(),
				TenantId:  "tenant-1",
				Metric:    "cpu_usage",
				Op:        ">",
				Threshold: float64(70 + i),
				Severity:  "warning",
				Enabled:   true,
			},
		})
		if err != nil {
			t.Fatalf("CreateRule failed: %v", err)
		}
	}

	resp, err := svc.ListRules(ctx)
	if err != nil {
		t.Fatalf("ListRules failed: %v", err)
	}

	if len(resp.Rules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(resp.Rules))
	}
}

func TestUpdateRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateRule(ctx, &alertv1.CreateRuleRequest{
		Rule: &alertv1.AlertRule{
			Name:      "Original",
			TenantId:  "tenant-1",
			Metric:    "cpu_usage",
			Op:        ">",
			Threshold: 80.0,
			Severity:  "warning",
			Enabled:   true,
		},
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	created.Name = "Updated"
	created.Threshold = 90.0

	updated, err := svc.UpdateRule(ctx, &alertv1.UpdateRuleRequest{Rule: created})
	if err != nil {
		t.Fatalf("UpdateRule failed: %v", err)
	}

	if updated.Name != "Updated" {
		t.Errorf("expected name Updated, got %s", updated.Name)
	}
	if updated.Threshold != 90.0 {
		t.Errorf("expected threshold 90.0, got %f", updated.Threshold)
	}
}

func TestUpdateRuleNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.UpdateRule(ctx, &alertv1.UpdateRuleRequest{
		Rule: &alertv1.AlertRule{
			Id:        "nonexistent",
			Name:      "Ghost",
			TenantId:  "tenant-1",
			Metric:    "cpu_usage",
			Op:        ">",
			Threshold: 80.0,
		},
	})
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound, got: %v", err)
	}
}

func TestDeleteRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateRule(ctx, &alertv1.CreateRuleRequest{
		Rule: &alertv1.AlertRule{
			Name:      "ToDelete",
			TenantId:  "tenant-1",
			Metric:    "cpu_usage",
			Op:        ">",
			Threshold: 80.0,
			Severity:  "warning",
			Enabled:   true,
		},
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	err = svc.DeleteRule(ctx, &alertv1.DeleteRuleRequest{Id: created.Id})
	if err != nil {
		t.Fatalf("DeleteRule failed: %v", err)
	}

	_, err = svc.GetRule(ctx, &alertv1.GetRuleRequest{Id: created.Id})
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound after delete, got: %v", err)
	}
}

func TestDeleteRuleNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	err := svc.DeleteRule(ctx, &alertv1.DeleteRuleRequest{Id: "nonexistent"})
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound, got: %v", err)
	}
}

func TestEvaluate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateRule(ctx, &alertv1.CreateRuleRequest{
		Rule: &alertv1.AlertRule{
			Name:      "High CPU",
			TenantId:  "tenant-1",
			Metric:    "cpu_usage",
			Op:        ">",
			Threshold: 80.0,
			Severity:  "critical",
			Enabled:   true,
		},
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	resp, err := svc.Evaluate(ctx, &alertv1.EvaluateRequest{
		TenantId: "tenant-1",
		DeviceId: "device-1",
		Metrics:  map[string]float64{"cpu_usage": 95.0},
	})
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	_ = resp
}

func TestAckAlert(t *testing.T) {
	svc := newTestService()

	st := store.NewMemoryStore()
	st.AddAlert(&store.Alert{
		AlertID:  "alert-1",
		TenantID: "tenant-1",
		Status:   "firing",
	})

	svc.store = st

	err := svc.AckAlert(context.Background(), &alertv1.AckAlertRequest{Id: "nonexistent"})
	if err != ErrAlertNotFound {
		t.Fatalf("expected ErrAlertNotFound, got: %v", err)
	}

	err = svc.AckAlert(context.Background(), &alertv1.AckAlertRequest{Id: "alert-1"})
	if err != nil {
		t.Fatalf("AckAlert failed: %v", err)
	}
}

func TestSilenceAlert(t *testing.T) {
	svc := newTestService()

	st := store.NewMemoryStore()
	st.AddAlert(&store.Alert{
		AlertID:  "alert-1",
		TenantID: "tenant-1",
		Status:   "firing",
	})

	svc.store = st

	err := svc.SilenceAlert(context.Background(), &alertv1.SilenceAlertRequest{
		Id:              "nonexistent",
		DurationMinutes: 30,
		Comment:         "maintenance",
	})
	if err != ErrAlertNotFound {
		t.Fatalf("expected ErrAlertNotFound, got: %v", err)
	}

	err = svc.SilenceAlert(context.Background(), &alertv1.SilenceAlertRequest{
		Id:              "alert-1",
		DurationMinutes: 30,
		Comment:         "maintenance",
	})
	if err != nil {
		t.Fatalf("SilenceAlert failed: %v", err)
	}
}
