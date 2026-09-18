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
	LastFiredAt      time.Time // A-1 阶段 fire 闭包用于本分钟去重（与 controlplane 行为一致）；scheduler 周期 < 1min 时尤其重要
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

// ============================================================================
// 灰度发布（canary）模型 — 对齐 controlplane server_batch.go L36-79
// ============================================================================

// BatchTaskItem 批量中单设备任务状态。
type BatchTaskItem struct {
	DeviceID string `json:"deviceID"`
	TaskID   string `json:"taskID"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

// CanaryRelease 灰度发布记录。
type CanaryRelease struct {
	CanaryID   string            // 灰度 ID
	TenantID   string            // 租户
	TaskType   string            // 任务类型
	Command    string            // 命令
	Strategy   string            // 策略：percentage/group/label
	Percentage int               // 比例（strategy=percentage 时有效）
	Groups     []string          // 分组（strategy=group 时有效）
	Labels     map[string]string // 标签（strategy=label 时有效）
	CreatedAt  time.Time
	CreatedBy  string
	Phases     []CanaryPhase // 各阶段执行情况
}

// CanaryPhase 灰度发布单阶段。
type CanaryPhase struct {
	Phase      int             // 阶段序号（1-based）
	DeviceIDs  []string        // 本阶段设备
	Status     string          // 阶段状态：pending/running/done/failed/aborted
	Tasks      []BatchTaskItem // 本阶段每设备任务
	StartedAt  time.Time
	FinishedAt time.Time
}
