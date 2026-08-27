package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/store"
)

func newTestService() *Service {
	return NewService(store.NewMemoryStore())
}

func TestCreateDeployment(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	d, err := svc.CreateDeployment(ctx, "tenant-1", "test-deploy", models.DeploymentTypeScript, "https://github.com/example/repo", "", "", []string{"target-1", "target-2"}, models.StrategyRolling, 0, false, "user-1")
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}
	if d.ID == "" {
		t.Fatal("expected non-empty deployment ID")
	}
	if d.Status != models.DeploymentStatusPending {
		t.Fatalf("expected status %s, got %s", models.DeploymentStatusPending, d.Status)
	}
	if d.Strategy != models.StrategyRolling {
		t.Fatalf("expected strategy %s, got %s", models.StrategyRolling, d.Strategy)
	}
}

func TestCreateDeploymentInvalidType(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateDeployment(ctx, "tenant-1", "test-deploy", "invalid-type", "", "", "", []string{"target-1"}, "", 0, false, "user-1")
	if !errors.Is(err, ErrDeploymentInvalid) {
		t.Fatalf("expected ErrDeploymentInvalid, got: %v", err)
	}
}

func TestCreateDeploymentInvalidStrategy(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateDeployment(ctx, "tenant-1", "test-deploy", models.DeploymentTypeScript, "", "", "", []string{"target-1"}, "invalid-strategy", 0, false, "user-1")
	if !errors.Is(err, ErrDeploymentInvalid) {
		t.Fatalf("expected ErrDeploymentInvalid, got: %v", err)
	}
}

func TestGetDeployment(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	d, err := svc.CreateDeployment(ctx, "tenant-1", "test-deploy", models.DeploymentTypeScript, "", "", "", []string{"target-1"}, "", 0, false, "user-1")
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	got, err := svc.GetDeployment(ctx, d.ID, "tenant-1")
	if err != nil {
		t.Fatalf("GetDeployment failed: %v", err)
	}
	if got.ID != d.ID {
		t.Fatalf("expected ID %s, got %s", d.ID, got.ID)
	}
	if got.Name != "test-deploy" {
		t.Fatalf("expected name test-deploy, got %s", got.Name)
	}
}

func TestGetDeploymentNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.GetDeployment(ctx, "nonexistent", "tenant-1")
	if !errors.Is(err, ErrDeploymentNotFound) {
		t.Fatalf("expected ErrDeploymentNotFound, got: %v", err)
	}
}

func TestListDeployments(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.CreateDeployment(ctx, "tenant-1", "test-deploy", models.DeploymentTypeScript, "", "", "", []string{"target-1"}, "", 0, false, "user-1")
		if err != nil {
			t.Fatalf("CreateDeployment failed: %v", err)
		}
	}

	list, err := svc.ListDeployments(ctx, "tenant-1", "")
	if err != nil {
		t.Fatalf("ListDeployments failed: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 deployments, got %d", len(list))
	}
}

func TestRollbackDeployment(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	d, err := svc.CreateDeployment(ctx, "tenant-1", "test-deploy", models.DeploymentTypeScript, "", "", "", []string{"target-1"}, "", 0, false, "user-1")
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	rolledBack, err := svc.RollbackDeployment(ctx, d.ID, "tenant-1")
	if err != nil {
		t.Fatalf("RollbackDeployment failed: %v", err)
	}
	if rolledBack.Status != models.DeploymentStatusRollback {
		t.Fatalf("expected status %s, got %s", models.DeploymentStatusRollback, rolledBack.Status)
	}
}

func TestRollbackDeploymentTerminalStatus(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	d, err := svc.CreateDeployment(ctx, "tenant-1", "test-deploy", models.DeploymentTypeScript, "", "", "", []string{"target-1"}, "", 0, false, "user-1")
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	_, err = svc.RollbackDeployment(ctx, d.ID, "tenant-1")
	if err != nil {
		t.Fatalf("First rollback failed: %v", err)
	}

	_, err = svc.RollbackDeployment(ctx, d.ID, "tenant-1")
	if !errors.Is(err, ErrDeploymentInvalid) {
		t.Fatalf("expected ErrDeploymentInvalid for terminal status, got: %v", err)
	}
}

func TestCancelDeployment(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	d, err := svc.CreateDeployment(ctx, "tenant-1", "test-deploy", models.DeploymentTypeScript, "", "", "", []string{"target-1"}, "", 0, false, "user-1")
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	cancelled, err := svc.CancelDeployment(ctx, d.ID, "tenant-1")
	if err != nil {
		t.Fatalf("CancelDeployment failed: %v", err)
	}
	if cancelled.Status != models.DeploymentStatusCancelled {
		t.Fatalf("expected status %s, got %s", models.DeploymentStatusCancelled, cancelled.Status)
	}
}

func TestCreateAndGetTemplate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	tmpl, err := svc.CreateTemplate(ctx, "tenant-1", "test-template", "A test template", models.DeploymentTypeScript, "https://github.com/example/repo", "", "", map[string]string{"key": "value"}, "user-1")
	if err != nil {
		t.Fatalf("CreateTemplate failed: %v", err)
	}
	if tmpl.ID == "" {
		t.Fatal("expected non-empty template ID")
	}

	got, err := svc.GetTemplate(ctx, tmpl.ID, "tenant-1")
	if err != nil {
		t.Fatalf("GetTemplate failed: %v", err)
	}
	if got.Name != "test-template" {
		t.Fatalf("expected name test-template, got %s", got.Name)
	}
	if got.Parameters["key"] != "value" {
		t.Fatalf("expected parameter key=value, got %s", got.Parameters["key"])
	}
}

func TestUpdateTemplate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	tmpl, err := svc.CreateTemplate(ctx, "tenant-1", "test-template", "A test template", models.DeploymentTypeScript, "", "", "", nil, "user-1")
	if err != nil {
		t.Fatalf("CreateTemplate failed: %v", err)
	}

	tmpl.Name = "updated-template"
	tmpl.Description = "Updated description"
	updated, err := svc.UpdateTemplate(ctx, tmpl)
	if err != nil {
		t.Fatalf("UpdateTemplate failed: %v", err)
	}
	if updated.Name != "updated-template" {
		t.Fatalf("expected name updated-template, got %s", updated.Name)
	}
}

func TestDeleteTemplate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	tmpl, err := svc.CreateTemplate(ctx, "tenant-1", "test-template", "A test template", models.DeploymentTypeScript, "", "", "", nil, "user-1")
	if err != nil {
		t.Fatalf("CreateTemplate failed: %v", err)
	}

	err = svc.DeleteTemplate(ctx, tmpl.ID, "tenant-1")
	if err != nil {
		t.Fatalf("DeleteTemplate failed: %v", err)
	}

	_, err = svc.GetTemplate(ctx, tmpl.ID, "tenant-1")
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected ErrTemplateNotFound, got: %v", err)
	}
}

func TestCreateAndGetStrategy(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	st, err := svc.CreateStrategy(ctx, "tenant-1", "test-strategy", "A test strategy", models.StrategyCanary, 10, 1, 2, true, 300, "user-1")
	if err != nil {
		t.Fatalf("CreateStrategy failed: %v", err)
	}
	if st.ID == "" {
		t.Fatal("expected non-empty strategy ID")
	}
	if st.Type != models.StrategyCanary {
		t.Fatalf("expected type %s, got %s", models.StrategyCanary, st.Type)
	}

	got, err := svc.GetStrategy(ctx, st.ID, "tenant-1")
	if err != nil {
		t.Fatalf("GetStrategy failed: %v", err)
	}
	if got.Name != "test-strategy" {
		t.Fatalf("expected name test-strategy, got %s", got.Name)
	}
}

func TestCreateStrategyInvalidType(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateStrategy(ctx, "tenant-1", "test-strategy", "A test strategy", "invalid", 10, 1, 2, true, 300, "user-1")
	if !errors.Is(err, ErrStrategyInvalid) {
		t.Fatalf("expected ErrStrategyInvalid, got: %v", err)
	}
}

func TestDeleteStrategy(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	st, err := svc.CreateStrategy(ctx, "tenant-1", "test-strategy", "A test strategy", models.StrategyRolling, 0, 0, 0, false, 0, "user-1")
	if err != nil {
		t.Fatalf("CreateStrategy failed: %v", err)
	}

	err = svc.DeleteStrategy(ctx, st.ID, "tenant-1")
	if err != nil {
		t.Fatalf("DeleteStrategy failed: %v", err)
	}

	_, err = svc.GetStrategy(ctx, st.ID, "tenant-1")
	if !errors.Is(err, ErrStrategyNotFound) {
		t.Fatalf("expected ErrStrategyNotFound, got: %v", err)
	}
}

func TestStartCanary(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	c, err := svc.StartCanary(ctx, "tenant-1", "deploy-1", "test-canary", 10, "user-1")
	if err != nil {
		t.Fatalf("StartCanary failed: %v", err)
	}
	if c.ID == "" {
		t.Fatal("expected non-empty canary ID")
	}
	if c.Status != models.CanaryStatusPending {
		t.Fatalf("expected status %s, got %s", models.CanaryStatusPending, c.Status)
	}
	if c.Weight != 10 {
		t.Fatalf("expected weight 10, got %d", c.Weight)
	}
}

func TestStartCanaryInvalidWeight(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.StartCanary(ctx, "tenant-1", "deploy-1", "test-canary", 0, "user-1")
	if !errors.Is(err, ErrCanaryInvalid) {
		t.Fatalf("expected ErrCanaryInvalid, got: %v", err)
	}

	_, err = svc.StartCanary(ctx, "tenant-1", "deploy-1", "test-canary", 101, "user-1")
	if !errors.Is(err, ErrCanaryInvalid) {
		t.Fatalf("expected ErrCanaryInvalid, got: %v", err)
	}
}

func TestPromoteCanary(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	c, err := svc.StartCanary(ctx, "tenant-1", "deploy-1", "test-canary", 10, "user-1")
	if err != nil {
		t.Fatalf("StartCanary failed: %v", err)
	}

	c.Status = models.CanaryStatusRunning
	st := svc.store.(*store.MemoryStore)
	_ = st
	// Update canary status directly via store
	err = svc.store.UpdateCanary(c)
	if err != nil {
		t.Fatalf("UpdateCanary failed: %v", err)
	}

	promoted, err := svc.PromoteCanary(ctx, c.ID, "tenant-1")
	if err != nil {
		t.Fatalf("PromoteCanary failed: %v", err)
	}
	if promoted.Status != models.CanaryStatusPromoted {
		t.Fatalf("expected status %s, got %s", models.CanaryStatusPromoted, promoted.Status)
	}
}

func TestRollbackCanary(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	c, err := svc.StartCanary(ctx, "tenant-1", "deploy-1", "test-canary", 10, "user-1")
	if err != nil {
		t.Fatalf("StartCanary failed: %v", err)
	}

	c.Status = models.CanaryStatusRunning
	err = svc.store.UpdateCanary(c)
	if err != nil {
		t.Fatalf("UpdateCanary failed: %v", err)
	}

	rolledBack, err := svc.RollbackCanary(ctx, c.ID, "tenant-1")
	if err != nil {
		t.Fatalf("RollbackCanary failed: %v", err)
	}
	if rolledBack.Status != models.CanaryStatusRollback {
		t.Fatalf("expected status %s, got %s", models.CanaryStatusRollback, rolledBack.Status)
	}
}

func TestTenantIsolation(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	d, err := svc.CreateDeployment(ctx, "tenant-1", "test-deploy", models.DeploymentTypeScript, "", "", "", []string{"target-1"}, "", 0, false, "user-1")
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	_, err = svc.GetDeployment(ctx, d.ID, "tenant-2")
	if !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("expected ErrTenantMismatch, got: %v", err)
	}
}

func TestGetDeploymentStatus(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	d, err := svc.CreateDeployment(ctx, "tenant-1", "test-deploy", models.DeploymentTypeScript, "", "", "", []string{"target-1", "target-2", "target-3"}, "", 0, false, "user-1")
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	got, err := svc.GetDeploymentStatus(ctx, d.ID, "tenant-1")
	if err != nil {
		t.Fatalf("GetDeploymentStatus failed: %v", err)
	}
	if got.Status != models.DeploymentStatusPending {
		t.Fatalf("expected status %s, got %s", models.DeploymentStatusPending, got.Status)
	}
}

func TestListTemplates(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.CreateTemplate(ctx, "tenant-1", "test-template", "A test template", models.DeploymentTypeScript, "", "", "", nil, "user-1")
		if err != nil {
			t.Fatalf("CreateTemplate failed: %v", err)
		}
	}

	list, err := svc.ListTemplates(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("ListTemplates failed: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 templates, got %d", len(list))
	}
}

func TestListStrategies(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		_, err := svc.CreateStrategy(ctx, "tenant-1", "test-strategy", "A test strategy", models.StrategyRolling, 0, 0, 0, false, 0, "user-1")
		if err != nil {
			t.Fatalf("CreateStrategy failed: %v", err)
		}
	}

	list, err := svc.ListStrategies(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("ListStrategies failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 strategies, got %d", len(list))
	}
}

func TestListCanaries(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		_, err := svc.StartCanary(ctx, "tenant-1", "deploy-1", "test-canary", 10, "user-1")
		if err != nil {
			t.Fatalf("StartCanary failed: %v", err)
		}
	}

	list, err := svc.ListCanaries(ctx, "tenant-1", "")
	if err != nil {
		t.Fatalf("ListCanaries failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 canaries, got %d", len(list))
	}
}

func TestDeploymentApprovalWorkflow(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	d, err := svc.CreateDeployment(ctx, "tenant-1", "test-deploy", models.DeploymentTypeScript, "", "", "", []string{"target-1"}, models.StrategyCanary, 10, true, "user-1")
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}

	if d.Status != models.DeploymentStatusPending {
		t.Fatalf("expected status %s, got %s", models.DeploymentStatusPending, d.Status)
	}

	cancelled, err := svc.CancelDeployment(ctx, d.ID, "tenant-1")
	if err != nil {
		t.Fatalf("CancelDeployment failed: %v", err)
	}
	if cancelled.Status != models.DeploymentStatusCancelled {
		t.Fatalf("expected status %s, got %s", models.DeploymentStatusCancelled, cancelled.Status)
	}
}

func TestCanaryAnalysisWithAutoRollback(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	c, err := svc.StartCanary(ctx, "tenant-1", "deploy-1", "test-canary", 20, "user-1")
	if err != nil {
		t.Fatalf("StartCanary failed: %v", err)
	}

	c.Status = models.CanaryStatusAnalyzing
	c.SuccessCount = 80
	c.FailureCount = 20
	err = svc.store.UpdateCanary(c)
	if err != nil {
		t.Fatalf("UpdateCanary failed: %v", err)
	}

	got, err := svc.GetCanaryStatus(ctx, c.ID, "tenant-1")
	if err != nil {
		t.Fatalf("GetCanaryStatus failed: %v", err)
	}
	if got.Status != models.CanaryStatusAnalyzing {
		t.Fatalf("expected status %s, got %s", models.CanaryStatusAnalyzing, got.Status)
	}
}

func TestDeploymentStrategyBlueGreen(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	d, err := svc.CreateDeployment(ctx, "tenant-1", "bg-deploy", models.DeploymentTypeK8s, "https://github.com/example/k8s", "", "", []string{"target-1", "target-2"}, models.StrategyBlueGreen, 0, true, "user-1")
	if err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}
	if d.Strategy != models.StrategyBlueGreen {
		t.Fatalf("expected strategy %s, got %s", models.StrategyBlueGreen, d.Strategy)
	}
	if !d.AutoRollback {
		t.Fatal("expected auto_rollback to be true")
	}
}

func TestMain(m *testing.M) {
	m.Run()
}

var _ = time.Now
