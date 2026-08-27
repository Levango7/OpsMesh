package models

import "time"

// TaskStatus represents the status of a task.
const (
	TaskStatusPending         = "pending"
	TaskStatusClaimed         = "claimed"
	TaskStatusRunning         = "running"
	TaskStatusDone            = "done"
	TaskStatusFailed          = "failed"
	TaskStatusCancelled       = "cancelled"
	TaskStatusPendingApproval = "pending_approval"
	TaskStatusRejected        = "rejected"
)

// TaskType represents the type of a task.
const (
	TaskTypeShell   = "shell"
	TaskTypeService = "service"
	TaskTypeFile    = "file"
)

// BatchStatus represents the status of a batch.
const (
	BatchStatusPending = "pending"
	BatchStatusRunning = "running"
	BatchStatusDone    = "done"
	BatchStatusFailed  = "failed"
)

// Task represents a task in the system.
type Task struct {
	TaskID           string
	AgentID          string
	TenantID         string
	Type             string
	Command          string
	Content          string
	Path             string
	Status           string
	ClaimedBy        string
	ClaimedAt        time.Time
	ClaimEpoch       int64
	CreatedAt        time.Time
	RetryCount       int
	MaxRetries       int
	DeadLetter       bool
	Timeout          int
	RetryDelay       int
	Schedule         string
	ParentID         string
	DependsOn        []string
	ApprovalRequired bool
	ApprovedBy       string
	ApprovedAt       time.Time
	BatchID          string
}

// TaskResult represents the result of a task execution.
type TaskResult struct {
	TaskID     string
	AgentID    string
	ExitCode   int
	Stdout     string
	Stderr     string
	DurationMs int64
	FinishedAt time.Time
	ClaimEpoch int64
}

// Schedule represents a scheduled task template.
type Schedule struct {
	ID          string
	TenantID    string
	Name        string
	CronExpr    string
	TaskType    string
	Command     string
	Content     string
	Path        string
	AgentID     string
	Enabled     bool
	LastFiredAt time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// BatchTask represents a batch of tasks.
type BatchTask struct {
	BatchID      string
	TenantID     string
	Name         string
	TotalCount   int
	SuccessCount int
	FailedCount  int
	PendingCount int
	Status       string
	CreatedAt    time.Time
}

// LogLine represents a single log line.
type LogLine struct {
	Timestamp time.Time
	Level     string
	Message   string
}

// TaskLog stores logs for a task.
type TaskLog struct {
	TaskID string
	Logs   []LogLine
}
