package runner

import (
	"context"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/models"
)

func TestRunner_ShellStepSuccess(t *testing.T) {
	r := NewRunner()
	ctx := context.Background()

	step := &models.Step{
		Name:    "echo test",
		Action:  "shell",
		Command: "echo hello",
		OnError: "stop",
	}

	result := r.executeStep(ctx, step)
	if result.Status != "success" {
		t.Fatalf("expected success, got %s: %s", result.Status, result.Error)
	}
	if result.Output == "" {
		t.Fatal("expected non-empty output")
	}
}

func TestRunner_ShellStepFailure(t *testing.T) {
	r := NewRunner()
	ctx := context.Background()

	step := &models.Step{
		Name:    "bad command",
		Action:  "shell",
		Command: "false",
		OnError: "stop",
	}

	result := r.executeStep(ctx, step)
	if result.Status != "failed" {
		t.Fatalf("expected failed, got %s", result.Status)
	}
}

func TestRunner_UnknownAction(t *testing.T) {
	r := NewRunner()
	ctx := context.Background()

	step := &models.Step{
		Name:   "unknown",
		Action: "teleport",
	}

	result := r.executeStep(ctx, step)
	if result.Status != "failed" {
		t.Fatalf("expected failed, got %s", result.Status)
	}
	if result.Error == "" {
		t.Fatal("expected error message")
	}
}

func TestRunner_RunbookSuccess(t *testing.T) {
	r := NewRunner()
	ctx := context.Background()

	rb := &models.Runbook{
		ID: "rb-1",
		Steps: []models.Step{
			{Name: "step1", Action: "shell", Command: "echo first", OnError: "stop"},
			{Name: "step2", Action: "shell", Command: "echo second", OnError: "stop"},
		},
	}

	record := r.Runbook(ctx, rb, "test")
	if record.Status != "success" {
		t.Fatalf("expected success, got %s", record.Status)
	}
	if len(record.StepResults) != 2 {
		t.Fatalf("expected 2 step results, got %d", len(record.StepResults))
	}
}

func TestRunner_RunbookStopOnError(t *testing.T) {
	r := NewRunner()
	ctx := context.Background()

	rb := &models.Runbook{
		ID: "rb-2",
		Steps: []models.Step{
			{Name: "step1", Action: "shell", Command: "false", OnError: "stop"},
			{Name: "step2", Action: "shell", Command: "echo should-not-run", OnError: "stop"},
		},
	}

	record := r.Runbook(ctx, rb, "test")
	if record.Status != "failed" {
		t.Fatalf("expected failed, got %s", record.Status)
	}
	if len(record.StepResults) != 1 {
		t.Fatalf("expected 1 step result (stopped after first), got %d", len(record.StepResults))
	}
}

func TestRunner_RunbookContinueOnError(t *testing.T) {
	r := NewRunner()
	ctx := context.Background()

	rb := &models.Runbook{
		ID: "rb-3",
		Steps: []models.Step{
			{Name: "step1", Action: "shell", Command: "false", OnError: "continue"},
			{Name: "step2", Action: "shell", Command: "echo reached", OnError: "stop"},
		},
	}

	record := r.Runbook(ctx, rb, "test")
	if record.Status != "success" {
		t.Fatalf("expected success, got %s", record.Status)
	}
	if len(record.StepResults) != 2 {
		t.Fatalf("expected 2 step results, got %d", len(record.StepResults))
	}
}

func TestRunner_RunbookRetryOnError(t *testing.T) {
	r := NewRunner()
	ctx := context.Background()

	rb := &models.Runbook{
		ID: "rb-4",
		Steps: []models.Step{
			{Name: "step1", Action: "shell", Command: "echo ok", OnError: "retry"},
		},
	}

	record := r.Runbook(ctx, rb, "test")
	if record.Status != "success" {
		t.Fatalf("expected success, got %s", record.Status)
	}
}

func TestRunner_EmptyCommand(t *testing.T) {
	r := NewRunner()
	ctx := context.Background()

	step := &models.Step{
		Name:   "empty",
		Action: "shell",
	}

	result := r.executeStep(ctx, step)
	if result.Status != "failed" {
		t.Fatalf("expected failed, got %s", result.Status)
	}
}

func TestRunner_RunbookSetsTimestamps(t *testing.T) {
	r := NewRunner()
	ctx := context.Background()

	rb := &models.Runbook{
		ID: "rb-ts",
		Steps: []models.Step{
			{Name: "step1", Action: "shell", Command: "echo ts", OnError: "stop"},
		},
	}

	record := r.Runbook(ctx, rb, "manual")
	if record.StartedAt.IsZero() {
		t.Fatal("expected StartedAt to be set")
	}
	if record.CompletedAt.IsZero() {
		t.Fatal("expected CompletedAt to be set")
	}
}

func TestRunner_StepDuration(t *testing.T) {
	r := NewRunner()
	ctx := context.Background()

	step := &models.Step{
		Name:    "duration-test",
		Action:  "shell",
		Command: "echo duration",
	}

	result := r.executeStep(ctx, step)
	if result.Duration <= 0 {
		t.Fatal("expected positive duration")
	}
}

func TestRunner_RunbookRecordMetadata(t *testing.T) {
	r := NewRunner()
	ctx := context.Background()

	rb := &models.Runbook{
		ID:   "rb-meta",
		Name: "Meta Runbook",
		Steps: []models.Step{
			{Name: "s1", Action: "shell", Command: "echo meta", OnError: "stop"},
		},
	}

	record := r.Runbook(ctx, rb, "alert:cpu-high")
	if record.RunbookID != "rb-meta" {
		t.Fatalf("expected runbook ID rb-meta, got %s", record.RunbookID)
	}
	if record.TriggeredBy != "alert:cpu-high" {
		t.Fatalf("expected triggered by alert:cpu-high, got %s", record.TriggeredBy)
	}
}

func TestRunner_HTTPStepBadURL(t *testing.T) {
	r := NewRunner()
	r.httpClient.Timeout = 1 * time.Second
	ctx := context.Background()

	step := &models.Step{
		Name:   "http-test",
		Action: "http",
		Target: "http://192.0.2.1:9999/timeout", // non-routable
	}

	result := r.executeStep(ctx, step)
	if result.Status != "failed" {
		t.Fatalf("expected failed, got %s", result.Status)
	}
}

func TestRunner_ScriptAction(t *testing.T) {
	r := NewRunner()
	ctx := context.Background()

	step := &models.Step{
		Name:    "script-test",
		Action:  "script",
		Command: "echo script-ok",
	}

	result := r.executeStep(ctx, step)
	if result.Status != "success" {
		t.Fatalf("expected success, got %s: %s", result.Status, result.Error)
	}
}
