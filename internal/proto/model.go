// Package proto 定义控制面与 agent 之间共享的数据类型（JSON 友好）。
// 同一份二进制，控制面与 agent 复用这些结构。
// 设计上刻意只使用 JSON（不引入 protobuf 工具链）：gRPC 9090 走 JSON codec 传输，HTTP 仅 B/S 仪表盘。
package proto

import "time"

// AgentInfo 是 agent 注册/心跳时上报的元信息，也是控制面内存注册表的存储单元。
type AgentInfo struct {
	AgentID     string    `json:"agentID"`     // 控制面分配的唯一 ID
	Hostname    string    `json:"hostname"`    // agent 所在主机名（os.Hostname）
	Segment     string    `json:"segment"`     // 所属网段（跨网段代理纳管的关键分桶键）
	TenantID    string    `json:"tenantID"`    // 所属租户（行级隔离键；由网关注入，agent 不可伪造）
	Addr        string    `json:"addr"`        // agent 自身地址（占位，gRPC/metrics 用）
	GRPCPort    int       `json:"grpcPort"`    // agent gRPC 端口（约定 9090）
	MetricsPort int       `json:"metricsPort"` // agent metrics 端口（约定 9091）
	Status      string    `json:"status"`      // online / offline
	Load        int       `json:"load"`        // 负载（MVP 仅示例）
	LastSeen    time.Time `json:"lastSeen"`    // 最近一次心跳时间
	// 自动纳管闭环：agent 经 bootstrap 安装后携带一次性 install token 注册。
	// InstallToken 由控制面 Provision 发放（HMAC 签名、一次性、限时），agent 不可伪造。
	InstallToken string `json:"installToken"`
	// OnboardDeviceID 为内部字段（由 gRPC Register 校验 token 后回填，不依赖 agent 自报）：
	// 非空时 Register 把该「已发现候选设备」翻转 onboarded（Managed=true），而非新建占位设备。
	OnboardDeviceID string `json:"onboardDeviceID"`
	// 目标机基础元信息（agent 注册时上报，控制面据此填充对应 DeviceInfo 的 Hostname/OS/Arch）。
	OS   string `json:"os"`   // 操作系统：windows / linux / darwin
	Arch string `json:"arch"` // CPU 架构：amd64 / arm64
}

// DeviceInfo 被纳管的网段内设备（服务部署后整段网络打通，设备自动纳管）。
// MVP 降级：默认采用“agent 即设备”——agent 注册时落一个代表其自身主机的 DeviceInfo（真实 IP/Hostname）。
// 开启 --discover 时，控制面按真实网段扫描（internal/discover）为每个存活主机创建 DeviceInfo，
// 这才是产品“整段网络自动纳管”的完整兑现路径（见）。
//
// 自动纳管闭环：网段发现的开放端口主机记为 Managed=false / State="discovered" 的候选设备
// （发现 ≠ 纳管，需经 provision 推送 agent 才能真正纳管）；agent 主动注册的设备才 Managed=true。
// 失败回写：LastResult/LastResultAt 记录该设备最近一次任务结果，供看板高亮异常设备。
type DeviceInfo struct {
	DeviceID     string    `json:"deviceID"`
	Segment      string    `json:"segment"`
	TenantID     string    `json:"tenantID"`     // 所属租户（取自纳管该设备的 agent）
	IP           string    `json:"ip"`           // agent 上报的地址作为占位 IP
	AgentID      string    `json:"agentID"`      // 纳管该设备的 agent（discovered 候选为空）
	State        string    `json:"state"`        // online / offline / discovered（候选）/ provisioning（推送中）
	TaskState    string    `json:"taskState"`    // idle / running / done
	Managed      bool      `json:"managed"`      // true=agent 已注册纳管；false=网段发现候选（待装 agent）
	LastResult   string    `json:"lastResult"`   // success / failed（失败回写看板）
	LastResultAt time.Time `json:"lastResultAt"` // 最近结果时间
	Retired      bool      `json:"retired"`      // F5 设备退役：true=已退役/下线，不出现在活跃清单
	// 设备基础元信息（agent 注册时上报，设备列表/详情展示用）。
	// 与 DeviceMetrics 区别：这里是相对静态的设备属性，DeviceMetrics 是动态实时指标。
	Hostname string `json:"hostname"` // 主机名
	OS       string `json:"os"`       // 操作系统：windows / linux / darwin
	Arch     string `json:"arch"`     // CPU 架构：amd64 / arm64
}

// DeviceMetrics 设备实时监控指标（agent 采集，心跳上报，控制面缓存最新值）。
// 同时保留最近 N 小时历史快照（环形缓冲，默认 2h/240 条），供 GET /api/v1/devices/{id}/metrics?range=2h 查询。
// agent 每 30 秒采集一次（心跳每 10 秒一次，但采集频率独立降低以减少系统开销）。
type DeviceMetrics struct {
	DeviceID     string        `json:"deviceID"`
	Hostname     string        `json:"hostname"`
	OS           string        `json:"os"`        // windows / linux / darwin
	OSVersion    string        `json:"osVersion"` // 如 "Microsoft Windows 11 Pro" / "Ubuntu 22.04 LTS"
	Kernel       string        `json:"kernel"`    // 内核版本
	Arch         string        `json:"arch"`      // amd64 / arm64
	Uptime       int64         `json:"uptime"`    // 运行时长（秒）
	CPU          CPUMetrics    `json:"cpu"`
	Memory       MemMetrics    `json:"memory"`
	Disks        []DiskMetrics `json:"disks"`
	Network      []NetMetrics  `json:"network"`
	Services     []ServiceInfo `json:"services"`
	ProcessCount int           `json:"processCount"`
	CollectedAt  time.Time     `json:"collectedAt"`
}

// MetricsSeries 设备监控指标历史时序数据（环形缓冲查询结果）。
// 用于 GET /api/v1/devices/{id}/metrics?range=2h 返回历史序列而非仅最新值。
// Samples 按时间升序排列（最早在前，最新在后）；空表示该时间窗内无数据。
type MetricsSeries struct {
	DeviceID string          `json:"deviceID"` // 设备 ID
	Range    string          `json:"range"`    // 查询范围原始参数（如 "2h"），便于前端回显
	Samples  []DeviceMetrics `json:"samples"`  // 历史指标快照（按 CollectedAt 升序）
}

// CPUMetrics CPU 指标。
type CPUMetrics struct {
	Cores int     `json:"cores"` // 逻辑核心数
	Usage float64 `json:"usage"` // 使用率 0-100
	Model string  `json:"model"` // CPU 型号
}

// MemMetrics 内存指标（单位 MB）。
type MemMetrics struct {
	Total     uint64  `json:"total"`     // 总内存（MB）
	Used      uint64  `json:"used"`      // 已用（MB）
	Available uint64  `json:"available"` // 可用（MB）
	Usage     float64 `json:"usage"`     // 使用率 0-100
}

// DiskMetrics 单个磁盘/分区指标（单位 GB）。
type DiskMetrics struct {
	Mount string  `json:"mount"` // 挂载点/盘符
	Total uint64  `json:"total"` // 总容量（GB）
	Used  uint64  `json:"used"`  // 已用（GB）
	Free  uint64  `json:"free"`  // 可用（GB）
	Usage float64 `json:"usage"` // 使用率 0-100
	Type  string  `json:"type"`  // 文件系统类型 NTFS/ext4
}

// NetMetrics 单个网卡指标。
type NetMetrics struct {
	Name    string `json:"name"`    // 网卡名
	IP      string `json:"ip"`      // IP 地址
	MAC     string `json:"mac"`     // MAC 地址
	RxBytes uint64 `json:"rxBytes"` // 接收字节数
	TxBytes uint64 `json:"txBytes"` // 发送字节数
	Status  string `json:"status"`  // up/down
	Speed   int    `json:"speed"`   // 速率 Mbps
}

// ServiceInfo 服务状态（仅采集常见运维相关服务，避免列全部服务）。
type ServiceInfo struct {
	Name    string `json:"name"`    // 服务名
	Status  string `json:"status"`  // running/stopped
	Enabled bool   `json:"enabled"` // 是否开机自启
}

// 任务类型常量（agent 按 Type 选择执行器）。
const (
	TaskTypeShell   = "shell"   // 执行 shell 命令（默认）
	TaskTypeService = "service" // 操作系统服务：Command 为 start|stop|restart|status，Path 为服务名（可选，缺省取 Command 末段）
	TaskTypeFile    = "file"    // 写文件：Content 为内容，Path 为目标路径（原子写入）
)

// Task 控制面下发给 agent 的自动化任务。
type Task struct {
	TaskID    string    `json:"taskID"`
	AgentID   string    `json:"agentID"`
	TenantID  string    `json:"tenantID"`  // 任务所属租户（下发入口写入，用于租户归属校验）
	Type      string    `json:"type"`      // shell / service / file（见 TaskType* 常量）
	Command   string    `json:"command"`   // shell: 命令; service: start|stop|restart|status
	Content   string    `json:"content"`   // file 类型：写入文件的内容
	Path      string    `json:"path"`      // file 类型：目标路径; service 类型：服务名（可选）
	Status    string    `json:"status"`    // pending / running / done / failed / cancelled（生命周期，空串按 pending 处理）
	ClaimedBy string    `json:"claimedBy"` // 领取该任务的 worker 标识（HA 协调）
	ClaimedAt time.Time `json:"claimedAt"` // 领取时间
	// ClaimEpoch 任务所有权令牌（防双跑）：每次 ClaimTask 时 +1。
	// agent 上报结果时携带 ClaimEpoch，SubmitResult 校验 WHERE claim_epoch=?，
	// RowsAffected=0 表示持有者已易主（任务被回收重派），拒绝旧持有者上报防双跑。
	// 值为 0 表示未设置（旧 agent / 测试），SubmitResult 跳过校验向后兼容。
	ClaimEpoch int64     `json:"claimEpoch"`
	CreatedAt  time.Time `json:"createdAt"`
	// F2 失败重试 / 死信（业务闭环）：RetryCount 累计重试，达 MaxRetries 置 failed（死信）。
	RetryCount int  `json:"retryCount"`
	MaxRetries int  `json:"maxRetries"`
	DeadLetter bool `json:"deadLetter"` // 重试耗尽后置 true，表示进入死信（需人工处置）
	// 节点级超时与重试：
	//   - Timeout 任务超时（秒，0=不超时）。agent 端按此强制终止超时任务，覆盖全局 taskTimeout。
	//   - RetryDelay 两次重试之间的等待间隔（秒，0=立即重试）。store SubmitResult 失败重试时记录，
	//     控制面调度器扫描到期的 pending 任务重新入队（避免失败后立即重试造成雪崩）。
	Timeout    int `json:"timeout,omitempty"`
	RetryDelay int `json:"retryDelay,omitempty"`
	// F3 任务取消：控制面 CancelTask 置 cancelled，未起动的 pending 不会被执行。
	// F4 定时/周期调度：Schedule 为 5 字段 cron 表达式（空=不调度），派生实例写 ParentID + LastFiredAt。
	Schedule    string    `json:"schedule"`
	ParentID    string    `json:"parentID"`
	LastFiredAt time.Time `json:"lastFiredAt"`
	// M5 作业编排占位（完整版 DAG）：DependsOn 为前置任务 ID，MVP 仅记录不执行。
	DependsOn []string `json:"dependsOn"`
	// 任务审批：高风险任务下发前需管理员审批。
	//   - ApprovalRequired=true 时 CreateTask 将状态置为 pending_approval（不进入 ClaimTask 队列）；
	//   - ApproveTask 翻转 pending_approval → pending（ApprovedBy/ApprovedAt 记录审批信息）；
	//   - RejectTask  翻转 pending_approval → rejected（驳回，永不进入队列）。
	ApprovalRequired bool      `json:"approvalRequired"`
	ApprovedBy       string    `json:"approvedBy"`
	ApprovedAt       time.Time `json:"approvedAt"`
}

// AuditEvent 内核产出的审计事件（等保三级：操作 100% 留痕）。
// 内核从“只消费网关注入身份”升级为“同时产出审计事件”，供审计/合规检索。
//
// 分布式可观测性：TraceID 字段关联 OTel trace_id，
// 使审计日志可与链路追踪/日志/SSE 事件跨域关联检索。
// omitempty 保证旧 JSON 反序列化不受影响（向后兼容）。
type AuditEvent struct {
	TenantID  string    `json:"tenantID"`
	UserID    string    `json:"userID"`
	Action    string    `json:"action"` // register / create_task / report_result / heartbeat ...
	Target    string    `json:"target"` // agentID / taskID
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"createdAt"`
	// TraceID 关联 OTel 链路追踪的 trace_id（32 字符 hex），空串表示无关联（向后兼容）。
	// 由控制面 handler / gRPC handler 在构造 AuditEvent 时从 ctx 提取注入。
	TraceID string `json:"traceID,omitempty"`
}

// TaskResult agent 上报的任务执行结果。
type TaskResult struct {
	TaskID     string    `json:"taskID"`
	AgentID    string    `json:"agentID"`
	ExitCode   int       `json:"exitCode"` // 0 成功；-1 表示 agent 侧错误
	Stdout     string    `json:"stdout"`
	Stderr     string    `json:"stderr"`
	DurationMs int64     `json:"durationMs"` // 执行耗时毫秒（观测指标）
	FinishedAt time.Time `json:"finishedAt"`
	// ClaimEpoch 任务所有权令牌（防双跑）：上报时携带领取时拿到的 ClaimEpoch，
	// store 校验持有者是否仍为当前 epoch，拒绝旧持有者上报防双跑。
	// 值为 0 表示未设置（旧 agent / 测试），store 跳过校验向后兼容。
	ClaimEpoch int64 `json:"claimEpoch"`
}

// Alert 状态（M7 监控告警）。
const (
	AlertStatusFiring       = "firing"       // 触发中（未处理）
	AlertStatusAcknowledged = "acknowledged" // 已确认（已知晓，待处理）
	AlertStatusSilenced     = "silenced"     // 已静默（在静默截止前不再打扰）
)

// Alert 内核产出的告警事件（M7 监控告警，业务闭环最小数据源）。
// 设备最近任务 failed 时由 store 产出，经总线可接告警系统。
type Alert struct {
	AlertID   string    `json:"alertID"`
	TenantID  string    `json:"tenantID"`
	DeviceID  string    `json:"deviceID"`
	AgentID   string    `json:"agentID"`
	Severity  string    `json:"severity"` // warning / critical
	Message   string    `json:"message"`
	Metric    string    `json:"metric"` // 指标名（如 cpu.usage）；告警聚合键之一，空时回退到 Message
	CreatedAt time.Time `json:"createdAt"`
	// 处理状态（M7 ack/silence）：未处理=firing；确认=acknowledged；静默=silenced。
	Status         string    `json:"status"`
	AcknowledgedBy string    `json:"acknowledgedBy"` // 确认人
	SilencedUntil  time.Time `json:"silencedUntil"`  // 静默截止时间
	Comment        string    `json:"comment"`        // 处理备注
	UpdatedAt      time.Time `json:"updatedAt"`      // 最近一次状态变更时间
}

// CmdbAttr 一条扁平化的 CMDB 属性（agent→控制面增量上报）。
// Key 为属性名（如 "os.version"、"cpu.cores"），包含值类型标识。
type CmdbAttr struct {
	Key   string `json:"key"`   // 属性路径，如 os.version, cpu.cores
	Value string `json:"value"` // 字符串值（数字/布尔由消费端按类型解析）
	Type  string `json:"type"`  // string / int / float / bool
}

// CmdbReport agent 通过 Heartbeat 上报到控制面的 CMDB 增量数据。
// 非空时控制面 CmdbConsumer 处理并更新 CI 条目。空=无变化。
type CmdbReport struct {
	CiType string     `json:"ciType"` // 覆盖的 CI 类型（machine / os / service）
	Seq    int64      `json:"seq"`    // 顺序号（控制面去重）
	Attrs  []CmdbAttr `json:"attrs"`  // 属性列表
}

// LogLine agent 采集的单行日志（agent 日志上报 gRPC API）。
// Timestamp 为日志行产生时间（agent 侧解析或采集时刻）；Level 为日志级别；
// Message 为日志正文（已去除行尾换行）。
type LogLine struct {
	Timestamp time.Time `json:"timestamp"` // 日志行产生时间
	Level     string    `json:"level"`     // INFO / WARN / ERROR / DEBUG
	Message   string    `json:"message"`   // 日志正文
}

// LogReport agent 经 gRPC ReportLogs 上报到控制面的日志批次。
// agent 侧 logCollectLoop 周期读取配置的日志文件增量，按行切分后封装为 LogReport 上报；
// 控制面校验 agent 身份（HMAC 签名）后按 agent 归属租户落库（行级隔离，agent 不可伪造租户）。
// TenantID 由控制面按 agent 注册时盖章回填（agent 自报不信任），agent 端可留空。
type LogReport struct {
	AgentID     string    `json:"agentID"`     // 上报 agent 的 ID（控制面据此查归属租户）
	TenantID    string    `json:"tenantID"`    // 留空：控制面按 agent 归属回填（agent 不可伪造）
	Hostname    string    `json:"hostname"`    // agent 主机名（便于检索展示）
	LogName     string    `json:"logName"`     // 日志文件名/标识（如 /var/log/syslog）
	Lines       []LogLine `json:"lines"`       // 日志行批次
	CollectedAt time.Time `json:"collectedAt"` // 本批次采集时刻
}

// LogPushConfig 日志推送配置（agent 从控制面或命令行获取）。
// agent 据此构造 LogPusher，对 Files 列表中的文件尾随（tail -f）采集，
// 按 Pattern 正则过滤后批量推送到 Endpoint（Loki /api/v1/push 或 ES /_bulk）。
// Backend 取 "loki" | "es"，决定推送报文格式与 endpoint 路径拼接。
type LogPushConfig struct {
	Files    []string `json:"files"`    // 要采集的文件列表（绝对路径或相对 agent 工作目录）
	Pattern  string   `json:"pattern"`  // 正则过滤（空=不过滤，全部推送）
	Endpoint string   `json:"endpoint"` // 推送目标完整 URL（如 http://loki:3100/loki/api/v1/push）
	Backend  string   `json:"backend"`  // 后端类型：loki | es
}
