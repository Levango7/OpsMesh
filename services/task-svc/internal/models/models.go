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
	Priority         int
	IdempotencyKey   string
	StatusMachine    string
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
//
// JSON tag 的用途（2026-10-15 R6 落库）：task-svc 的 MySQL 后端把整条灰度记录
// 序列化进 canary_releases.payload（设计取舍与 controlplane task_records 同：
// 只按 ID 整条读写、Phases 是嵌套结构，逐列建列必然漂移）。tag 即该载荷的
// 字段契约——改名会让存量行的该字段解不出来（按"记录不可用"处理，不返回半条），
// 故字段名演化必须连同这里一起改。
type CanaryRelease struct {
	CanaryID   string            `json:"canaryID"`         // 灰度 ID
	TenantID   string            `json:"tenantID"`         // 租户
	TaskType   string            `json:"taskType"`         // 任务类型
	Command    string            `json:"command"`          // 命令
	Strategy   string            `json:"strategy"`         // 策略：percentage/group/label
	Percentage int               `json:"percentage"`       // 比例（strategy=percentage 时有效）
	Groups     []string          `json:"groups,omitempty"` // 分组（strategy=group 时有效）
	Labels     map[string]string `json:"labels,omitempty"` // 标签（strategy=label 时有效）
	CreatedAt  time.Time         `json:"createdAt"`
	CreatedBy  string            `json:"createdBy"`
	Phases     []CanaryPhase     `json:"phases"` // 各阶段执行情况
}

// CanaryPhase 灰度发布单阶段。
type CanaryPhase struct {
	Phase      int             `json:"phase"`     // 阶段序号（1-based）
	DeviceIDs  []string        `json:"deviceIDs"` // 本阶段设备
	Status     string          `json:"status"`    // 阶段状态：pending/running/done/failed/aborted
	Tasks      []BatchTaskItem `json:"tasks"`     // 本阶段每设备任务
	StartedAt  time.Time       `json:"startedAt"`
	FinishedAt time.Time       `json:"finishedAt"`
}
