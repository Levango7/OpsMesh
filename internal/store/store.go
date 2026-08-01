// Package store 定义可插拔的持久化抽象 Store 及两种实现：
//   - MemoryStore：内存实现（默认后端，无需任何外部依赖即可运行）。
//   - SQLStore：MySQL + Redis 实现（U-04 数据本地化，私有部署）。
//
// 控制面通过 Store 接口与具体后端解耦；Registry 仅做薄转发。
//
// M2-1A 接口拆分：原 37 方法巨型 Store 接口按领域拆为 6 个小接口
// （DeviceStore / TaskStore / AlertStore / AuditStore / TokenStore / LeaderStore），
// Store 保留为它们的组合接口，向后兼容现有消费方。
// 小接口本身即文档化领域边界；消费方可按需依赖最小接口，当前保留 Store
// 组合接口向后兼容，后续可渐进迁移到小接口以降低耦合。
package store

import (
	"time"

	"opsmesh/internal/proto"
)

// DeviceStore 设备/Agent 纳管领域：注册、心跳、设备视图、退役归档。
// 共 10 个方法，覆盖 P0-2 真实网段发现、P2-17 agent 直查、F5 离线归档。
type DeviceStore interface {
	// Register 注册一个 agent，返回（可能被分配了 agentID 的）agent 信息。
	Register(*proto.AgentInfo) *proto.AgentInfo
	// Heartbeat 更新 agent 在线状态/负载，返回是否已知该 agent。
	Heartbeat(agentID, status string, load int) bool
	// Device 按 deviceID 返回单台设备（供设备详情端点）。
	Device(id string) *proto.DeviceInfo
	// Results 返回某 agent 的上报结果（供设备详情端点）。
	Results(agentID string) []*proto.TaskResult
	// UpsertDevice 写入/更新一台纳管设备（真实网段发现 P0-2 用；按 deviceID 幂等）。
	UpsertDevice(*proto.DeviceInfo)
	// RetireDevice 退役/下线设备（F5）：标记 retired，退出活跃清单但仍可查归档；租户隔离。
	RetireDevice(id, tenantID string) bool
	// Snapshot 返回 segment -> 设备列表 的当前视图（tenantID 非空时按租户过滤）。
	Snapshot(tenantID string) map[string][]proto.DeviceInfo
	// Agents 返回已注册 agent（tenantID 非空时按租户过滤；空串=全部）。
	Agents(tenantID string) []*proto.AgentInfo
	// Agent 按 agentID 直接返回单台 agent（下发入口校验租户归属用，O(1) 直查，P2-17）。
	Agent(id string) *proto.AgentInfo
	// RetireStaleDevices F5 离线超龄自动归档：扫描最后心跳早于 maxAge 的 agent 所对应设备
	// （或已无 agent 的孤儿设备），批量标记 retired（退出活跃清单但仍可查归档）。返回归档数。
	// 仅 leader 周期执行（归档属协调任务，避免多副本重复）。
	RetireStaleDevices(maxAge time.Duration) int
}

// TaskStore 任务调度领域：下发、领取、上报、取消、定时派生、失联复位。
// 共 12 个方法，覆盖 P0-1 任务必达、P1-1 HA 领取、F3 取消、F4 定时调度、P2-1 队列深度。
type TaskStore interface {
	// GetTasks 返回指定 agent 的待执行任务（仅 pending；只读，不改动状态，用于检视/调试）。
	GetTasks(agentID string) []*proto.Task
	// TasksByParent 返回指定 parent_id 的全部任务（跨状态，用于 M5 工作流运行归组 / F4 模板血缘）。
	TasksByParent(parentID string) []*proto.Task
	// ClaimTask 原子领取该 agent 的下一条 pending 任务：翻转 pending→running 并返回。
	// 多副本控制面并发调用时，同一任务只会被一个副本领取（HA 协调，P1-1）。
	ClaimTask(agentID string) *proto.Task
	// CreateTask 下发一个任务给指定 agent（agentID 必填，TaskID 为空时由 store 分配）。
	CreateTask(*proto.Task) *proto.Task
	// SubmitResult 接收 agent 上报的执行结果，并把对应 task 标记为 done。
	SubmitResult(*proto.TaskResult)
	// AllTasks 返回全部任务（tenantID 非空时按租户过滤；供任务列表端点，功能补全）。
	AllTasks(tenantID string) []*proto.Task
	// TaskResult 按 taskID 返回单条执行结果（A5/F7 结果查询 API；供 GET /api/v1/tasks/{id}/result）。
	TaskResult(taskID string) *proto.TaskResult
	// CancelTask 取消任务（F3）：pending/running -> cancelled；已 done/failed 不可取消。返回是否生效。
	CancelTask(id, tenantID string) bool
	// PendingDepth 返回当前 pending 任务总数（观测队列深度 P2-1）。
	PendingDepth() int
	// ReclaimStaleTasks 复位超期未完成的 running 任务为 pending（P0-1 任务必达）：
	// agent 领取后超过 maxAge 仍未上报结果，视为失联，重新进入调度队列。返回被复位的任务数。
	ReclaimStaleTasks(maxAge time.Duration) int
	// CancelledTaskIDs 返回该 agent 当前处于 cancelled 状态的任务 ID 列表（F3 取消信号下发用）。
	// agent 侧 cancelLoop 轮询此接口，命中正在执行的任务即中止本地执行（不回写 store，避免误触重试/死信）。
	CancelledTaskIDs(agentID string) []string
	// FireDueSchedules 评估所有模板任务（ParentID=="" 且 Schedule!=""），
	// 对到点（cron 匹配 now 且 LastFiredAt 早于本分钟）的模板派生一个 pending 实例并回写 LastFiredAt。
	// 返回本批次派生的实例数（F4 定时/周期调度；控制面 scheduleLoop 周期调用）。
	FireDueSchedules(now time.Time) int
}

// AlertStore 告警领域（M7）：列表、记录、单查、确认、静默。
type AlertStore interface {
	// Alerts 返回活跃告警（M7）；tenantID 非空时按租户过滤。
	Alerts(tenantID string) []*proto.Alert
	// AddAlert 记录一条告警（M7）。
	AddAlert(*proto.Alert)
	// Alert 按 alertID 返回单条告警（M7；供 ack/silence 定位）。id 为空或不存在时返回 nil。
	Alert(id string) *proto.Alert
	// AckAlert 确认告警（M7）：置 acknowledged 并记录确认人；tenantID 非空时校验归属，越权返回 false。
	AckAlert(id, tenantID, by string) bool
	// SilenceAlert 静默告警（M7）：置 silenced 并记录静默截止与备注；tenantID 非空时校验归属，越权返回 false。
	// until 为零值时由存储层默认静默 24h。
	SilenceAlert(id, tenantID, by string, until time.Time, comment string) bool
}

// AuditStore 审计领域（U-04 等保三级留痕）：记录、全量、按条件检索。
type AuditStore interface {
	// Audit 记录一条审计事件（内核产出审计，U-04 等保三级留痕）。
	Audit(*proto.AuditEvent)
	// Audits 返回已记录审计事件（MVP 全量；生产可改分页/时间窗）。
	Audits() []*proto.AuditEvent
	// QueryAudits 按租户/动作/时间窗过滤审计事件（P0-4 审计可查；U-04 等保三级留痕必须可检索）。
	// limit<=0 表示不限制（默认建议 100）。
	QueryAudits(tenant, action string, since, until time.Time, limit int) []*proto.AuditEvent
}

// TokenStore B1 自动纳管 install token 领域：签发、登记、消费、清理。
type TokenStore interface {
	// Provision B1 自动纳管闭环：为「已发现候选设备」发放一次性、限时的 install token
	// （HMAC 签名，密钥来自 store 构造时注入的 ProvisionSecret），返回 token 与 bootstrap 提示；
	// 同时把候选设备标记 provisioning（推送中）。deviceID 不存在或已纳管时返回错误。
	Provision(deviceID, host, tenantID string) (token, bootstrap string, err error)
	// IssueToken 生成并登记一个一次性 install token（HMAC(deviceID|tenantID|expiry|nonce)，ttl 为有效期。
	IssueToken(deviceID, tenantID string, ttl time.Duration) (token string, err error)
	// ConsumeToken 校验并消费 token：限时、未用过才返回设备与租户并置 consumed；否则返回 ok=false。
	ConsumeToken(token string) (deviceID, tenantID string, ok bool)
	// CleanupTokens 清理过期/已消费的 install token（F9 无界增长防护）。
	// batch 为单次最大清理数（<=0 时不限制）。返回清理数。仅 leader 周期执行。
	CleanupTokens(batch int) int
}

// LeaderStore A3 真 HA 领导者选举领域：续租、查询本实例是否为主。
//
// 多副本控制面中仅 leader 执行周期性协调任务（reclaim / schedule / provision / 离线归档），
// 避免重复执行导致任务被多副本重复派生/回收。
// MemoryStore 恒为 leader（单实例，config 已拒绝 memory+replicas>1）；
// SQLStore 经 leader_lease 表原子抢占/续租实现分布式选主。
type LeaderStore interface {
	// RenewLeadership 尝试抢占或续租领导租约，返回本实例当前是否持有租约（即是否为 leader）。
	RenewLeadership(ttl time.Duration) bool
	// IsLeader 返回本实例当前是否自认为 leader（租约未过期）。
	IsLeader() bool
}

// Store 控制面注册表的可插拔持久化组合接口。
// 由 6 个领域小接口组合而成（M2-1A 拆分），方法签名刻意与旧版内存 Registry 保持一致，
// 便于平滑替换。U-04: 数据本地化，默认 memory；生产可切换 mysql（MySQL/Redis 私有部署）。
//
// 消费方可按需依赖最小子接口（如 provision 只需 TokenStore），当前保留 Store
// 组合接口向后兼容，后续可渐进迁移到小接口以降低耦合。
type Store interface {
	DeviceStore
	TaskStore
	AlertStore
	AuditStore
	TokenStore
	LeaderStore

	// WithDemo 设置是否开启演示模式（P0-5）：开启时每个 agent 注册预置 uname -a 示例任务。
	WithDemo(bool) Store
}

// 编译期断言：确保 MemoryStore / SQLStore 实现各领域小接口。
// 任一方法缺失会在编译期立刻暴露（而非运行期），降低后续拆分消费方时的回归风险。
var (
	_ DeviceStore = (*MemoryStore)(nil)
	_ TaskStore   = (*MemoryStore)(nil)
	_ AlertStore  = (*MemoryStore)(nil)
	_ AuditStore  = (*MemoryStore)(nil)
	_ TokenStore  = (*MemoryStore)(nil)
	_ LeaderStore = (*MemoryStore)(nil)
	_ Store       = (*MemoryStore)(nil)

	_ DeviceStore = (*SQLStore)(nil)
	_ TaskStore   = (*SQLStore)(nil)
	_ AlertStore  = (*SQLStore)(nil)
	_ AuditStore  = (*SQLStore)(nil)
	_ TokenStore  = (*SQLStore)(nil)
	_ LeaderStore = (*SQLStore)(nil)
	_ Store       = (*SQLStore)(nil)
)
