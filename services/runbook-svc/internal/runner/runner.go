package runner

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/models"
)

// Runner executes runbook steps sequentially.
type Runner struct {
	httpClient *http.Client
}

// NewRunner creates a new Runner.
func NewRunner() *Runner {
	return &Runner{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Execute runs all steps of a runbook sequentially and returns the execution record.
func (r *Runner) Runbook(ctx context.Context, rb *models.Runbook, triggeredBy string) *models.ExecutionRecord {
	record := &models.ExecutionRecord{
		RunbookID:   rb.ID,
		TriggeredBy: triggeredBy,
		Status:      "running",
		StepResults: make([]models.StepResult, 0, len(rb.Steps)),
		StartedAt:   time.Now(),
	}

	for i := range rb.Steps {
		step := rb.Steps[i]
		result := r.executeStep(ctx, &step)
		record.StepResults = append(record.StepResults, result)

		if result.Status == "failed" {
			switch step.OnError {
			case "stop":
				record.Status = "failed"
				record.ErrorMessage = fmt.Sprintf("step '%s' failed: %s", step.Name, result.Error)
				record.CompletedAt = time.Now()
				return record
			case "retry":
				retried := r.executeStep(ctx, &step)
				record.StepResults[len(record.StepResults)-1] = retried
				if retried.Status == "failed" {
					record.Status = "failed"
					record.ErrorMessage = fmt.Sprintf("step '%s' failed after retry: %s", step.Name, retried.Error)
					record.CompletedAt = time.Now()
					return record
				}
			}
			// continue: proceed to next step
		}
	}

	record.Status = "success"
	record.CompletedAt = time.Now()
	return record
}

func (r *Runner) executeStep(ctx context.Context, step *models.Step) models.StepResult {
	result := models.StepResult{
		StepName:  step.Name,
		Action:    step.Action,
		StartedAt: time.Now(),
	}

	stepCtx := ctx
	if step.Timeout > 0 {
		var cancel context.CancelFunc
		stepCtx, cancel = context.WithTimeout(ctx, step.Timeout)
		defer cancel()
	}

	var output string
	var execErr error

	switch step.Action {
	case "shell":
		output, execErr = r.runShell(stepCtx, step.Command)
	case "http":
		output, execErr = r.runHTTP(stepCtx, step.Target)
	case "script":
		output, execErr = r.runScript(stepCtx, step.Command)
	default:
		execErr = fmt.Errorf("unknown action type: %s", step.Action)
	}

	result.Duration = time.Since(result.StartedAt)

	if execErr != nil {
		result.Status = "failed"
		result.Error = execErr.Error()
	} else {
		result.Status = "success"
		result.Output = output
	}

	return result
}

func (r *Runner) runShell(ctx context.Context, command string) (string, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty command")
	}
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("shell execution failed: %w", err)
	}
	return string(out), nil
}

func (r *Runner) runHTTP(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d error from %s", resp.StatusCode, url)
	}
	return fmt.Sprintf("HTTP %d OK", resp.StatusCode), nil
}

func (r *Runner) runScript(ctx context.Context, command string) (string, error) {
	return r.runShell(ctx, command)
}
