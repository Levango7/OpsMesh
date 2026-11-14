package models

import "time"

// Runbook represents an automated playbook with ordered steps.
type Runbook struct {
	ID string `json:"id"`
	// TenantID 是行级租户隔离键（D4 / issue #27 裁决②）：runbook 是租户的自动化手册，
	// 写入必钉（空值拒绝），读取/更新/删除一律按它过滤。
	TenantID    string `json:"tenant_id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Content 是前端编辑器的文本形式契约（web/enterprise/src/api/runbook.js），
	// 与 Steps 并存：Steps 为结构化形式，两者可同时存在。
	Content   string             `json:"content,omitempty"`
	Triggers  []TriggerCondition `json:"triggers"`
	Steps     []Step             `json:"steps"`
	Enabled   bool               `json:"enabled"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
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
	ID string `json:"id"`
	// TenantID 与所属 Runbook 一致（归属由父对象派生，不接受调用方指定）。
	TenantID     string       `json:"tenant_id,omitempty"`
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
	// TenantID：webhook 是外部触发，租户只能由**调用方声明**（缺失即 400）。
	// 触发范围因此只限该租户的 runbook，不会跨租户触发。
	TenantID  string            `json:"tenant_id"`
	EventType string            `json:"event_type"`
	Source    string            `json:"source"`
	Data      map[string]string `json:"data"`
}
