// Package dataetl 提供 ETL 作业模型、内存存储与引擎适配器。
//
// 设计原则与仓库一致：作业/运行记录为纯模型，存储经 EtlStore 接口抽象
// （内存实现先行，SQL 实现后续批次补齐）；引擎适配器把作业转成具体命令。
package dataetl

import "time"

// EtlEngine ETL 引擎类型。
const (
	EngineSeaTunnel = "seatunnel"
	EngineDataX     = "datax"
	EngineSqoop     = "sqoop"
	EngineSpark     = "spark"
	EngineAirflow   = "airflow"
	EngineKafkaConn = "kafka-connect"
	EngineFlinkCDC  = "flink-cdc"
	EngineCanal     = "canal"
)

// EtlStatus 作业状态。
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// EtlJob 一个 ETL 作业定义。
type EtlJob struct {
	ID         string            `json:"id"`
	TenantID   string            `json:"tenantId"`
	Name       string            `json:"name"`
	Engine     string            `json:"engine"`
	Source     string            `json:"source"`    // 源端连接串/表
	Sink       string            `json:"sink"`      // 目标端连接串/表
	Transform  string            `json:"transform"` // 转换脚本/SQL
	Schedule   string            `json:"schedule"`  // cron 表达式（空=手动）
	Enabled    bool              `json:"enabled"`
	MaxRetries int               `json:"maxRetries"`
	TimeoutSec int               `json:"timeoutSec"`
	Params     map[string]string `json:"params,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

// EtlRun 一次 ETL 运行记录。
type EtlRun struct {
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

// EtlStore ETL 存储抽象。
type EtlStore interface {
	CreateJob(job EtlJob) (EtlJob, error)
	GetJob(tenantID, id string) (EtlJob, error)
	ListJobs(tenantID string) []EtlJob
	UpdateJob(job EtlJob) (EtlJob, error)
	DeleteJob(tenantID, id string) error

	CreateRun(run EtlRun) (EtlRun, error)
	GetRun(tenantID, id string) (EtlRun, error)
	ListRuns(tenantID, jobID string) []EtlRun
	UpdateRun(run EtlRun) (EtlRun, error)
}

// EngineAdapter 将 EtlJob 转成可执行命令。
type EngineAdapter interface {
	Name() string
	BuildCommand(job EtlJob) (string, error)
}

// EtlEngineNames 返回支持的引擎列表。
func EtlEngineNames() []string {
	return []string{EngineSeaTunnel, EngineDataX, EngineSqoop, EngineSpark,
		EngineAirflow, EngineKafkaConn, EngineFlinkCDC, EngineCanal}
}

// IsValidEngine 校验引擎类型。
func IsValidEngine(e string) bool {
	for _, n := range EtlEngineNames() {
		if n == e {
			return true
		}
	}
	return false
}

// IsValidStatus 校验运行状态。
func IsValidStatus(s string) bool {
	switch s {
	case StatusPending, StatusRunning, StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	}
	return false
}
