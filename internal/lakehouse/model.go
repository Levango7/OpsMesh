// Package lakehouse 提供湖仓表维护（Iceberg/Delta/Hudi）与批/流统一作业模型。
//
// 设计原则与仓库一致：维护命令为 shell 模板（dry-run 时仅渲染，不执行），
// 存储经 LakehouseStore 接口抽象（内存实现先行，SQL 实现后续批次补齐）。
package lakehouse

import "time"

// TableFormat 表格式类型。
const (
	FormatIceberg = "iceberg"
	FormatDelta   = "delta"
	FormatHudi    = "hudi"
)

// IsValidFormat 校验表格式。
func IsValidFormat(f string) bool {
	switch f {
	case FormatIceberg, FormatDelta, FormatHudi:
		return true
	}
	return false
}

// MaintenanceAction 维护动作类型。
const (
	ActionCompact          = "compact"
	ActionExpireSnapshots  = "expire-snapshots"
	ActionRemoveOrphans    = "remove-orphans"
	ActionRewriteManifests = "rewrite-manifests"
	ActionVacuum           = "vacuum"
)

// IsValidAction 校验维护动作。
func IsValidAction(a string) bool {
	switch a {
	case ActionCompact, ActionExpireSnapshots, ActionRemoveOrphans, ActionRewriteManifests, ActionVacuum:
		return true
	}
	return false
}

// TableConfig 湖仓表配置。
type TableConfig struct {
	ID         string            `json:"id"`
	TenantID   string            `json:"tenantId"`
	Name       string            `json:"name"`
	Format     string            `json:"format"`
	Location   string            `json:"location"`
	Catalog    string            `json:"catalog"` // hive, glue, nessie, etc.
	Properties map[string]string `json:"properties,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

// MaintenanceJob 维护作业。
type MaintenanceJob struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	TableID   string    `json:"tableId"`
	TableName string    `json:"tableName"`
	Format    string    `json:"format"`
	Action    string    `json:"action"`
	Params    string    `json:"params"` // e.g. "retainLast=10,olderThan=7d"
	DryRun    bool      `json:"dryRun"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt,omitempty"`
	Duration  string    `json:"duration,omitempty"`
	Logs      string    `json:"logs,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// JobKind 统一批/流作业类型。
const (
	JobKindBatch  = "batch"
	JobKindStream = "stream"
	JobKindMicro  = "microbatch"
)

// IsValidJobKind 校验作业类型。
func IsValidJobKind(k string) bool {
	switch k {
	case JobKindBatch, JobKindStream, JobKindMicro:
		return true
	}
	return false
}

// JobStatus 作业状态。
const (
	JobStatusPending   = "pending"
	JobStatusRunning   = "running"
	JobStatusSucceeded = "succeeded"
	JobStatusFailed    = "failed"
	JobStatusCancelled = "cancelled"
)

// IsValidJobStatus 校验作业状态。
func IsValidJobStatus(s string) bool {
	switch s {
	case JobStatusPending, JobStatusRunning, JobStatusSucceeded, JobStatusFailed, JobStatusCancelled:
		return true
	}
	return false
}

// UnifiedJob 统一批/流作业（与 EtlJob 共享语义，但面向湖仓）。
type UnifiedJob struct {
	ID         string            `json:"id"`
	TenantID   string            `json:"tenantId"`
	Name       string            `json:"name"`
	Kind       string            `json:"kind"` // batch|stream|microbatch
	Engine     string            `json:"engine"`
	Source     string            `json:"source"`
	Sink       string            `json:"sink"`
	Transform  string            `json:"transform"`
	Schedule   string            `json:"schedule"`
	Enabled    bool              `json:"enabled"`
	MaxRetries int               `json:"maxRetries"`
	TimeoutSec int               `json:"timeoutSec"`
	Params     map[string]string `json:"params,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

// JobRun 统一运行记录。
type JobRun struct {
	ID        string    `json:"id"`
	JobID     string    `json:"jobId"`
	TenantID  string    `json:"tenantId"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt,omitempty"`
	Duration  string    `json:"duration,omitempty"`
	Logs      string    `json:"logs,omitempty"`
	Error     string    `json:"error,omitempty"`
	Attempt   int       `json:"attempt"`
}

// LakehouseStore 湖仓存储抽象。
type LakehouseStore interface {
	CreateTable(t TableConfig) (TableConfig, error)
	GetTable(tenantID, id string) (TableConfig, error)
	ListTables(tenantID string) []TableConfig
	UpdateTable(t TableConfig) (TableConfig, error)
	DeleteTable(tenantID, id string) error

	CreateMaintenanceJob(j MaintenanceJob) (MaintenanceJob, error)
	GetMaintenanceJob(tenantID, id string) (MaintenanceJob, error)
	ListMaintenanceJobs(tenantID, tableID string) []MaintenanceJob
	UpdateMaintenanceJob(j MaintenanceJob) (MaintenanceJob, error)

	CreateUnifiedJob(j UnifiedJob) (UnifiedJob, error)
	GetUnifiedJob(tenantID, id string) (UnifiedJob, error)
	ListUnifiedJobs(tenantID string) []UnifiedJob
	UpdateUnifiedJob(j UnifiedJob) (UnifiedJob, error)
	DeleteUnifiedJob(tenantID, id string) error

	CreateJobRun(r JobRun) (JobRun, error)
	GetJobRun(tenantID, id string) (JobRun, error)
	ListJobRuns(tenantID, jobID string) []JobRun
	UpdateJobRun(r JobRun) (JobRun, error)
}
