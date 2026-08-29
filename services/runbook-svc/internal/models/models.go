package models

import "time"

// Runbook represents an automated playbook with ordered steps.
type Runbook struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Triggers    []TriggerCondition `json:"triggers"`
	Steps       []Step             `json:"steps"`
	Enabled     bool               `json:"enabled"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

// TriggerCondition defines when a runbook should auto-trigger.
type TriggerCondition struct {
	EventType string            `json:"event_type"` // e.g. "alert", "webhook", "schedule"
	Filters   map[string]string `json:"filters"`    // key-value match conditions
}

// Step is a single action in a runbook.
type Step struct {
	Name    string        `json:"name"`
	Action  string        `json:"action"`  // shell/http/script
	Target  string        `json:"target"`  // device_id or URL
	Command string        `json:"command"` // actual command/payload
	Timeout time.Duration `json:"timeout"`
	OnError string        `json:"on_error"` // continue/stop/retry
}

// ExecutionRecord captures the result of a runbook execution.
type ExecutionRecord struct {
	ID           string       `json:"id"`
	RunbookID    string       `json:"runbook_id"`
	TriggeredBy  string       `json:"triggered_by"`
	Status       string       `json:"status"` // running/success/failed
	StepResults  []StepResult `json:"step_results"`
	StartedAt    time.Time    `json:"started_at"`
	CompletedAt  time.Time    `json:"completed_at"`
	ErrorMessage string       `json:"error_message,omitempty"`
}

// StepResult captures the result of a single step execution.
type StepResult struct {
	StepName  string        `json:"step_name"`
	Action    string        `json:"action"`
	Status    string        `json:"status"` // success/failed/skipped
	Output    string        `json:"output"`
	Error     string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
	StartedAt time.Time     `json:"started_at"`
}

// WebhookPayload represents an incoming webhook trigger.
type WebhookPayload struct {
	EventType string            `json:"event_type"`
	Source    string            `json:"source"`
	Data      map[string]string `json:"data"`
}
