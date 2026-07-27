// Package domain 定义 OpsMesh 的纯领域模型（DDD 分层中的 domain 层）。
// 它与 proto（gRPC/HTTP 传输层）解耦：proto 负责线上格式，domain 负责业务语义。
// 防腐层（ACL）由 mapper.go 提供，在 gRPC/HTTP 边界做 proto <-> domain 转换，
// 避免传输结构泄漏进业务逻辑（回应二次审计⑩「DDD/ACL 代码层不存在」）。
package domain

import "time"

// Tenant 租户（U-04 行级隔离键）。
type Tenant struct {
	ID string
}

// Agent 已注册 agent 的领域实体。
type Agent struct {
	AgentID     string
	Hostname    string
	Segment     string
	TenantID    string
	Addr        string
	GRPCPort    int
	MetricsPort int
	Status      string
	Load        int
	LastSeen    time.Time
	// B1 自动纳管闭环：经 install token 校验后回填的候选设备 ID（不依赖 agent 自报）。
	// 非空时控制面 Register 把该「已发现候选设备」翻转 onboarded（Managed=true）。
	OnboardDeviceID string
}

// Device 被纳管的网段内设备（U-02：服务部署后整段网络打通，设备自动纳管）。
// Device 是控制面对外暴露的设备模型（经防腐层从 proto 映射）。
// 显式 json tag 与外部 API 契约一致（镜像 proto 的小写键），避免默认导出大写字段名导致前端取不到值。
type Device struct {
	DeviceID     string    `json:"deviceID"`
	Segment      string    `json:"segment"`
	TenantID    string    `json:"tenantID"`
	IP           string    `json:"ip"`
	AgentID      string    `json:"agentID"`
	State        string    `json:"state"`     // online / offline / discovered（B1 候选）/ provisioning（B1 推送中）
	TaskState    string    `json:"taskState"`
	Managed      bool      `json:"managed"`   // true=agent 已注册纳管；false=网段发现候选（待装 agent，B1）
	LastResult   string    `json:"lastResult"` // success / failed（B2 失败回写看板）
	LastResultAt time.Time `json:"lastResultAt"`
	Retired      bool      `json:"retired"`   // F5 设备退役
}

// Task 下发给 agent 的自动化任务。
// Task 对外暴露的任务模型（显式 json tag 与外部 API 契约一致）。
type Task struct {
	TaskID      string    `json:"taskID"`
	AgentID     string    `json:"agentID"`
	TenantID    string    `json:"tenantID"`
	Type        string    `json:"type"`    // shell / service / file（见 proto.TaskType* 常量）
	Command     string    `json:"command"` // shell: 命令; service: start|stop|restart|status
	Content     string    `json:"content"` // file 类型：写入文件的内容
	Path        string    `json:"path"`    // file 类型：目标路径; service 类型：服务名（可选）
	Status      string    `json:"status"`  // pending / running / done / failed / cancelled
	ClaimedBy   string    `json:"claimedBy"`
	ClaimedAt   time.Time `json:"claimedAt"`
	CreatedAt   time.Time `json:"createdAt"`
	RetryCount   int       `json:"retryCount"` // F2 重试累计
	MaxRetries  int       `json:"maxRetries"` // F2 重试上限
	DeadLetter  bool      `json:"deadLetter"` // F2 死信标记
	Schedule    string    `json:"schedule"`   // F4 cron 表达式（空=不调度）
	ParentID    string    `json:"parentID"`   // F4 派生实例的模板 ID
	LastFiredAt time.Time `json:"lastFiredAt"`
	DependsOn   []string  `json:"dependsOn"` // M5 作业编排占位（前置任务 ID）
}

// TaskResult agent 上报的任务执行结果（显式 json tag）。
type TaskResult struct {
	TaskID     string    `json:"taskID"`
	AgentID    string    `json:"agentID"`
	ExitCode   int       `json:"exitCode"`
	Stdout     string    `json:"stdout"`
	Stderr     string    `json:"stderr"`
	DurationMs int64     `json:"durationMs"`
	FinishedAt time.Time `json:"finishedAt"`
}

// AuditEvent 内核产出的审计事件（U-04 等保三级：操作 100% 留痕，显式 json tag）。
type AuditEvent struct {
	TenantID  string    `json:"tenantID"`
	UserID    string    `json:"userID"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"createdAt"`
}

// Alert 内核产出的告警事件（M7 监控告警，业务闭环最小数据源，显式 json tag）。
type Alert struct {
	AlertID   string    `json:"alertID"`
	TenantID  string    `json:"tenantID"`
	DeviceID  string    `json:"deviceID"`
	AgentID   string    `json:"agentID"`
	Severity  string    `json:"severity"` // warning / critical
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}
