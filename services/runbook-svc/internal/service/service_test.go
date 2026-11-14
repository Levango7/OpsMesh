package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/runner"
	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/store"
)

func setupTestService() (*Service, *store.MemoryStore) {
	st := store.NewMemoryStore()
	r := runner.NewRunner()
	return NewService(st, r), st
}

func TestService_CreateRunbook(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	rb := &models.Runbook{
		TenantID: "t1",
		Name:     "Test Runbook",
		Steps:    []models.Step{{Name: "s1", Action: "shell", Command: "echo hi", OnError: "stop"}},
		Enabled:  true,
	}

	created, err := svc.CreateRunbook(ctx, rb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected ID to be set")
	}
	if created.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
}

func TestService_CreateRunbookInvalid(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	_, err := svc.CreateRunbook(ctx, nil)
	if !errors.Is(err, ErrRunbookInvalid) {
		t.Fatalf("expected ErrRunbookInvalid, got %v", err)
	}

	_, err = svc.CreateRunbook(ctx, &models.Runbook{TenantID: "t1", Name: ""})
	if !errors.Is(err, ErrRunbookInvalid) {
		t.Fatalf("expected ErrRunbookInvalid for empty name, got %v", err)
	}
}

func TestService_CreateRunbookNoSteps(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	rb := &models.Runbook{TenantID: "t1", Name: "No Steps", Enabled: true}
	_, err := svc.CreateRunbook(ctx, rb)
	if !errors.Is(err, ErrRunbookInvalid) {
		t.Fatalf("expected ErrRunbookInvalid for enabled runbook with no steps, got %v", err)
	}
}

func TestService_GetRunbook(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	rb := &models.Runbook{
		TenantID: "t1",
		Name:     "Get Test",
		Steps:    []models.Step{{Name: "s1", Action: "shell", Command: "echo get", OnError: "stop"}},
	}
	created, _ := svc.CreateRunbook(ctx, rb)

	got, err := svc.GetRunbook(ctx, created.ID, "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "Get Test" {
		t.Fatalf("expected name 'Get Test', got %s", got.Name)
	}
}

func TestService_GetRunbookNotFound(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	_, err := svc.GetRunbook(ctx, "nonexistent", "t1")
	if !errors.Is(err, ErrRunbookNotFound) {
		t.Fatalf("expected ErrRunbookNotFound, got %v", err)
	}
}

func TestService_ListRunbooks(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, _ = svc.CreateRunbook(ctx, &models.Runbook{
			TenantID: "t1",
			Name:     "Runbook",
			Steps:    []models.Step{{Name: "s", Action: "shell", Command: "echo list", OnError: "stop"}},
		})
	}

	list, err := svc.ListRunbooks(ctx, "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 runbooks, got %d", len(list))
	}
}

func TestService_UpdateRunbook(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	rb := &models.Runbook{
		TenantID: "t1",
		Name:     "Update Test",
		Steps:    []models.Step{{Name: "s1", Action: "shell", Command: "echo update", OnError: "stop"}},
	}
	created, _ := svc.CreateRunbook(ctx, rb)
	created.Name = "Updated"

	updated, err := svc.UpdateRunbook(ctx, created, "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "Updated" {
		t.Fatalf("expected 'Updated', got %s", updated.Name)
	}
}

func TestService_UpdateRunbookNotFound(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	_, err := svc.UpdateRunbook(ctx, &models.Runbook{ID: "ghost", Name: "Ghost", TenantID: "t1"}, "t1")
	if !errors.Is(err, ErrRunbookNotFound) {
		t.Fatalf("expected ErrRunbookNotFound, got %v", err)
	}
}

func TestService_DeleteRunbook(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	rb := &models.Runbook{
		TenantID: "t1",
		Name:     "Delete Test",
		Steps:    []models.Step{{Name: "s1", Action: "shell", Command: "echo delete", OnError: "stop"}},
	}
	created, _ := svc.CreateRunbook(ctx, rb)

	err := svc.DeleteRunbook(ctx, created.ID, "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.GetRunbook(ctx, created.ID, "t1")
	if !errors.Is(err, ErrRunbookNotFound) {
		t.Fatal("expected runbook to be deleted")
	}
}

func TestService_DeleteRunbookNotFound(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	err := svc.DeleteRunbook(ctx, "nonexistent", "t1")
	if !errors.Is(err, ErrRunbookNotFound) {
		t.Fatalf("expected ErrRunbookNotFound, got %v", err)
	}
}

func TestService_TriggerRunbook(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	rb := &models.Runbook{
		TenantID: "t1",
		Name:     "Trigger Test",
		Enabled:  true,
		Steps:    []models.Step{{Name: "s1", Action: "shell", Command: "echo trigger", OnError: "stop"}},
	}
	created, _ := svc.CreateRunbook(ctx, rb)

	record, err := svc.TriggerRunbook(ctx, created.ID, "test", "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if record.Status != "success" {
		t.Fatalf("expected success, got %s", record.Status)
	}
	if record.RunbookID != created.ID {
		t.Fatalf("expected runbook ID %s, got %s", created.ID, record.RunbookID)
	}
}

func TestService_TriggerRunbookDisabled(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	rb := &models.Runbook{
		TenantID: "t1",
		Name:     "Disabled",
		Enabled:  false,
		Steps:    []models.Step{{Name: "s1", Action: "shell", Command: "echo disabled", OnError: "stop"}},
	}
	created, _ := svc.CreateRunbook(ctx, rb)

	_, err := svc.TriggerRunbook(ctx, created.ID, "test", "t1")
	if !errors.Is(err, ErrRunbookDisabled) {
		t.Fatalf("expected ErrRunbookDisabled, got %v", err)
	}
}

func TestService_TriggerRunbookNotFound(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	_, err := svc.TriggerRunbook(ctx, "ghost", "test", "t1")
	if !errors.Is(err, ErrRunbookNotFound) {
		t.Fatalf("expected ErrRunbookNotFound, got %v", err)
	}
}

func TestService_GetHistory(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	rb := &models.Runbook{
		TenantID: "t1",
		Name:     "History Test",
		Enabled:  true,
		Steps:    []models.Step{{Name: "s1", Action: "shell", Command: "echo history", OnError: "stop"}},
	}
	created, _ := svc.CreateRunbook(ctx, rb)

	_, _ = svc.TriggerRunbook(ctx, created.ID, "test1", "t1")
	_, _ = svc.TriggerRunbook(ctx, created.ID, "test2", "t1")

	history, err := svc.GetHistory(ctx, created.ID, "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history records, got %d", len(history))
	}
}

func TestService_HandleWebhook(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	rb := &models.Runbook{
		TenantID: "t1",
		Name:     "Webhook Test",
		Enabled:  true,
		Triggers: []models.TriggerCondition{
			{EventType: "alert", Filters: map[string]string{"severity": "critical"}},
		},
		Steps: []models.Step{{Name: "s1", Action: "shell", Command: "echo webhook", OnError: "stop"}},
	}
	_, _ = svc.CreateRunbook(ctx, rb)

	payload := &models.WebhookPayload{
		EventType: "alert",
		Source:    "prometheus",
		Data:      map[string]string{"severity": "critical", "host": "server1"},
	}

	records, err := svc.HandleWebhook(ctx, payload, "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 triggered record, got %d", len(records))
	}
}

func TestService_HandleWebhookNoMatch(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	rb := &models.Runbook{
		TenantID: "t1",
		Name:     "No Match",
		Enabled:  true,
		Triggers: []models.TriggerCondition{
			{EventType: "alert", Filters: map[string]string{"severity": "warning"}},
		},
		Steps: []models.Step{{Name: "s1", Action: "shell", Command: "echo no-match", OnError: "stop"}},
	}
	_, _ = svc.CreateRunbook(ctx, rb)

	payload := &models.WebhookPayload{
		EventType: "alert",
		Source:    "prometheus",
		Data:      map[string]string{"severity": "info"},
	}

	records, err := svc.HandleWebhook(ctx, payload, "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 triggered records, got %d", len(records))
	}
}

func TestService_HandleWebhookNilPayload(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	_, err := svc.HandleWebhook(ctx, nil, "t1")
	if !errors.Is(err, ErrRunbookInvalid) {
		t.Fatalf("expected ErrRunbookInvalid, got %v", err)
	}
}

func TestService_WebhookSkipsDisabled(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	rb := &models.Runbook{
		TenantID: "t1",
		Name:     "Disabled Webhook",
		Enabled:  false,
		Triggers: []models.TriggerCondition{
			{EventType: "alert"},
		},
		Steps: []models.Step{{Name: "s1", Action: "shell", Command: "echo skip", OnError: "stop"}},
	}
	_, _ = svc.CreateRunbook(ctx, rb)

	payload := &models.WebhookPayload{EventType: "alert", Source: "test"}
	records, err := svc.HandleWebhook(ctx, payload, "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records (disabled runbook skipped), got %d", len(records))
	}
}

func TestService_TriggerRunbookRecordsExecution(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	rb := &models.Runbook{
		TenantID: "t1",
		Name:     "Record Test",
		Enabled:  true,
		Steps:    []models.Step{{Name: "s1", Action: "shell", Command: "echo record", OnError: "stop"}},
	}
	created, _ := svc.CreateRunbook(ctx, rb)

	_, _ = svc.TriggerRunbook(ctx, created.ID, "test", "t1")

	history, _ := svc.GetHistory(ctx, created.ID, "t1")
	if len(history) != 1 {
		t.Fatalf("expected execution to be recorded, got %d records", len(history))
	}
	if history[0].TriggeredBy != "test" {
		t.Fatalf("expected triggered_by 'test', got %s", history[0].TriggeredBy)
	}
}

func TestService_CreateRunbookSetsTimestamps(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	rb := &models.Runbook{
		TenantID: "t1",
		Name:     "Timestamp Test",
		Steps:    []models.Step{{Name: "s1", Action: "shell", Command: "echo ts", OnError: "stop"}},
	}
	created, _ := svc.CreateRunbook(ctx, rb)

	if created.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
	if created.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set")
	}
	if !created.CreatedAt.Equal(created.UpdatedAt) {
		t.Fatal("expected CreatedAt and UpdatedAt to be equal on creation")
	}
}

func TestService_UpdateRunbookUpdatesTimestamp(t *testing.T) {
	svc, _ := setupTestService()
	ctx := context.Background()

	rb := &models.Runbook{
		TenantID: "t1",
		Name:     "Update TS",
		Steps:    []models.Step{{Name: "s1", Action: "shell", Command: "echo ts", OnError: "stop"}},
	}
	created, _ := svc.CreateRunbook(ctx, rb)
	origUpdated := created.UpdatedAt

	time.Sleep(10 * time.Millisecond)
	created.Name = "Updated TS"
	updated, _ := svc.UpdateRunbook(ctx, created, "t1")

	if !updated.UpdatedAt.After(origUpdated) {
		t.Fatal("expected UpdatedAt to be updated")
	}
}
