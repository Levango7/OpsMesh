// Package store 定义可插拔的持久化抽象 Store 及两种实现：
//   - MemoryStore：内存实现（默认后端，无需任何外部依赖即可运行）。
//   - SQLStore：MySQL + Redis 实现（数据本地化，私有部署）。
//
// 控制面通过 Store 接口与具体后端解耦；Registry 仅做薄转发。
//
// 接口拆分：原 37 方法巨型 Store 接口按领域拆为 6 个小接口
// （DeviceStore / TaskStore / AlertStore / AuditStore / TokenStore / LeaderStore），
// Store 保留为它们的组合接口，向后兼容现有消费方。
// 小接口本身即文档化领域边界；消费方可按需依赖最小接口，当前保留 Store
// 组合接口向后兼容，后续可渐进迁移到小接口以降低耦合。
package store

import (
	"time"

	"opsmesh/internal/proto"
)

// DeviceStore 设备/Agent 纳管领域：注册、心跳、设备视图、退役归档、监控指标。
// 共 12 个方法，覆盖 真实网段发现、agent 直查、F5 离线归档、监控指标采集。
type DeviceStore interface {
	// Register 注册一个 agent，返回（可能被分配了 agentID 的）agent 信息。
	Register(*proto.AgentInfo) *proto.AgentInfo
	// Heartbeat 更新 agent 在线状态/负载，返回是否已知该 agent。
	Heartbeat(agentID, status string, load int) bool
	// Device 按 deviceID 返回单台设备（供设备详情端点）。
	Device(id string) *proto.DeviceInfo
	// Results 返回某 agent 的上报结果（供设备详情端点）。
	Results(agentID string) []*proto.TaskResult
	// UpsertDevice 写入/更新一台纳管设备（真实网段发现 用；按 deviceID 幂等）。
	UpsertDevice(*proto.DeviceInfo)
	// RetireDevice 退役/下线设备（F5）：标记 retired，退出活跃清单但仍可查归档；租户隔离。
	RetireDevice(id, tenantID string) bool
	// Snapshot 返回 segment -> 设备列表 的当前视图（tenantID 非空时按租户过滤）。
	Snapshot(tenantID string) map[string][]proto.DeviceInfo
	// Agents 返回已注册 agent（tenantID 非空时按租户过滤；空串=全部）。
	Agents(tenantID string) []*proto.AgentInfo
	// Agent 按 agentID 直接返回单台 agent（下发入口校验租户归属用，O(1) 直查）。
	Agent(id string) *proto.AgentInfo
	// RetireStaleDevices F5 离线超龄自动归档：扫描最后心跳早于 maxAge 的 agent 所对应设备
	// （或已无 agent 的孤儿设备），批量标记 retired（退出活跃清单但仍可查归档）。返回归档数。
	// 仅 leader 周期执行（归档属协调任务，避免多副本重复）。
	RetireStaleDevices(maxAge time.Duration) int
	// StoreDeviceMetrics 存储设备最新监控指标（agent 心跳上报，仅保留最新值）。
	// deviceID 为空或 metrics 为 nil 时直接返回。控制面缓存最新值供 GET /api/v1/devices/{id}/metrics 查询。
	StoreDeviceMetrics(deviceID string, metrics *proto.DeviceMetrics)
	// DeviceMetrics 返回设备最新监控指标（无数据时返回 nil）。
	DeviceMetrics(deviceID string) *proto.DeviceMetrics
	// DeviceMetricsHistory 返回设备监控指标历史时序（环形缓冲查询）。
	// since 为零值时返回全部已存储历史；否则返回 CollectedAt >= since 的快照（按时间升序）。
	// 无数据时返回 nil。控制面 GET /api/v1/devices/{id}/metrics?range=2h 调用此方法。
	DeviceMetricsHistory(deviceID string, since time.Time) []proto.DeviceMetrics
	// AgentSecret 返回该 agent 的 HMAC 签名密钥（gRPC 身份绑定）。
	// 由 Register 时为每个 agent 随机生成 32 字节 hex 串并落库；agent 拉任务/上报/轮询取消时
	// 用此密钥计算 HMAC-SHA256(secret, timestamp+agentID) 签名，控制面据此验证 agent 身份，
	// 不再纯信任 agent 自报的 AgentID（防冒领任务/伪造上报）。agent 不存在或未生成密钥时返回空串。
	AgentSecret(agentID string) string
}

// TaskStore 任务调度领域：下发、领取、上报、取消、定时派生、失联复位。
// 共 12 个方法，覆盖 任务必达、HA 领取、F3 取消、F4 定时调度、队列深度。
type TaskStore interface {
	// GetTasks 返回指定 agent 的待执行任务（仅 pending；只读，不改动状态，用于检视/调试）。
	GetTasks(agentID string) []*proto.Task
	// TasksByParent 返回指定 parent_id 的全部任务（跨状态，用于 M5 工作流运行归组 / F4 模板血缘）。
	TasksByParent(parentID string) []*proto.Task
	// ClaimTask 原子领取该 agent 的下一条 pending 任务：翻转 pending→running 并返回。
	// 多副本控制面并发调用时，同一任务只会被一个副本领取（HA 协调）。
	ClaimTask(agentID string) *proto.Task
	// CreateTask 下发一个任务给指定 agent（agentID 必填，TaskID 为空时由 store 分配）。
	CreateTask(*proto.Task) *proto.Task
	// SubmitResult 接收 agent 上报的执行结果，并把对应 task 标记为 done。
	SubmitResult(*proto.TaskResult)
	// AllTasks 返回全部任务（tenantID 非空时按租户过滤；供任务列表端点，功能补全）。
	AllTasks(tenantID string) []*proto.Task
	// TaskByID 按 taskID 返回单条任务（不存在返回 nil）。
	// 用于按 ID 直查场景（如结果查询的租户归属校验），避免遍历 AllTasks（O(N) → O(1)）。
	// 返回深拷贝避免外部并发修改破坏内部状态。
	TaskByID(taskID string) *proto.Task
	// TaskResult 按 taskID 返回单条执行结果（A5/F7 结果查询 API；供 GET /api/v1/tasks/{id}/result）。
	TaskResult(taskID string) *proto.TaskResult
	// CancelTask 取消任务（F3）：pending/running -> cancelled；已 done/failed 不可取消。返回是否生效。
	CancelTask(id, tenantID string) bool
	// PendingDepth 返回当前 pending 任务总数（观测队列深度）。
	PendingDepth() int
	// ReclaimStaleTasks 复位超期未完成的 running 任务为 pending（任务必达）：
	// agent 领取后超过 maxAge 仍未上报结果，视为失联，重新进入调度队列。返回被复位的任务数。
	ReclaimStaleTasks(maxAge time.Duration) int
	// CancelledTaskIDs 返回该 agent 当前处于 cancelled 状态的任务 ID 列表（F3 取消信号下发用）。
	// agent 侧 cancelLoop 轮询此接口，命中正在执行的任务即中止本地执行（不回写 store，避免误触重试/死信）。
	CancelledTaskIDs(agentID string) []string
	// FireDueSchedules 评估所有模板任务（ParentID=="" 且 Schedule!=""），
	// 对到点（cron 匹配 now 且 LastFiredAt 早于本分钟）的模板派生一个 pending 实例并回写 LastFiredAt。
	// 返回本批次派生的实例数（F4 定时/周期调度；控制面 scheduleLoop 周期调用）。
	FireDueSchedules(now time.Time) int
	// ApproveTask 审批通过任务：将 pending_approval 状态翻转回 pending，
	// 记录审批人/审批时间。仅 pending_approval 状态可审批；其他状态返回 false。
	// tenantID 非空时校验任务归属，越权返回 false。
	ApproveTask(id, tenantID, approvedBy string) bool
	// RejectTask 驳回任务：将 pending_approval 状态置为 rejected，
	// 记录审批人/审批时间。被驳回任务永不进入 ClaimTask 队列。仅 pending_approval 状态可驳回。
	// tenantID 非空时校验任务归属，越权返回 false。
	RejectTask(id, tenantID, approvedBy string) bool
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
	// CreateAlertRule 创建告警规则：ID 为空时由 store 分配随机 ID；
	// TenantID 为空时归一为 default。返回持久化后的规则（含分配的 ID）。
	CreateAlertRule(*AlertRule) *AlertRule
	// ListAlertRules 返回告警规则；tenantID 非空时按租户过滤。
	ListAlertRules(tenantID string) []*AlertRule
	// DeleteAlertRule 删除告警规则，返回是否删除成功（不存在返回 false）。
	DeleteAlertRule(id string) bool
	// GetAlertRule 按 ID 返回单个告警规则（M2 持久化补全；不存在返回 nil）。
	GetAlertRule(id string) *AlertRule
	// UpdateAlertRule 更新告警规则（M2 持久化补全）。不存在返回 false。
	UpdateAlertRule(*AlertRule) bool
}

// AuditStore 审计领域（等保三级留痕）：记录、全量、按条件检索。
type AuditStore interface {
	// Audit 记录一条审计事件（内核产出审计，等保三级留痕）。
	Audit(*proto.AuditEvent)
	// Audits 返回已记录审计事件（MVP 全量；生产可改分页/时间窗）。
	Audits() []*proto.AuditEvent
	// QueryAudits 按租户/动作/时间窗过滤审计事件（审计可查；等保三级留痕必须可检索）。
	// limit<=0 表示不限制（默认建议 100）。
	QueryAudits(tenant, action string, since, until time.Time, limit int) []*proto.AuditEvent
}

// TokenStore 自动纳管 install token 领域：签发、登记、消费、清理。
type TokenStore interface {
	// Provision 自动纳管闭环：为「已发现候选设备」发放一次性、限时的 install token
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

// LeaderStore 真 HA 领导者选举领域：续租、查询本实例是否为主。
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

// UserStore 用户领域：注册、查询、创建、更新、删除。
// 密码以 bcrypt 哈希存储（CreateUser 入参 u.PasswordHash 已哈希，store 不再二次哈希）。
type UserStore interface {
	// GetUser 按 ID 返回单用户（不存在返回 nil）。
	GetUser(id string) *User
	// GetUserByUsername 按用户名返回单用户（登录用；不存在返回 nil）。
	GetUserByUsername(username string) *User
	// ListUsers 返回全部用户（按创建时间升序）。
	ListUsers() []*User
	// CreateUser 创建用户。调用方须先 bcrypt 哈希密码填入 u.PasswordHash。
	// 用户名重复时返回 nil（调用方据此判断冲突）。
	CreateUser(u *User) *User
	// UpdateUser 更新用户 email/roles/status（按 u.ID 定位）。不存在返回 false。
	UpdateUser(u *User) bool
	// DeleteUser 按 ID 删除用户。不存在返回 false。
	DeleteUser(id string) bool
	// ChangePassword 改密（安全债）：按 userID 定位，写入新的 bcrypt 哈希，
	// 并清除 MustChangePassword 标记。返回是否成功（用户不存在/哈希失败为 false）。
	// 与 UpdateUser 分离，避免 UpdateUser 误覆盖 PasswordHash。
	ChangePassword(userID, newPasswordHash string) bool
}

// RoleStore 角色领域：CRUD。
type RoleStore interface {
	// GetRole 按 ID 返回单角色（不存在返回 nil）。
	GetRole(id string) *Role
	// ListRoles 返回全部角色。
	ListRoles() []*Role
	// CreateRole 创建角色。角色名重复时返回 nil。
	CreateRole(r *Role) *Role
	// UpdateRole 更新角色 description/permissions（按 r.ID 定位）。不存在返回 false。
	UpdateRole(r *Role) bool
	// DeleteRole 按 ID 删除角色。不存在返回 false。
	DeleteRole(id string) bool
}

// PermissionStore 权限领域：预定义权限列表（只读）。
type PermissionStore interface {
	// ListPermissions 返回全部预定义权限（按组分类）。
	ListPermissions() []*Permission
}

// K8sClusterStore K8s 集群配置管理领域（Phase 3）：CRUD。
//
// 与 DeviceStore 等领域解耦，独立小接口组合进 Store。
// Kubeconfig 为敏感内容，调用方（API 层）负责脱敏后再返回前端。
type K8sClusterStore interface {
	// ListK8sClusters 返回 K8s 集群配置（按创建时间升序）；tenantID 非空时仅返回同租户集群（租户隔离）。
	ListK8sClusters(tenantID string) []*K8sCluster
	// GetK8sCluster 按 ID 返回单个集群配置（不存在返回 nil）。
	GetK8sCluster(id string) *K8sCluster
	// SaveK8sCluster 创建或更新集群配置（按 ID 幂等），返回持久化错误。
	// ID 为空时由 store 分配随机 ID；CreatedAt/UpdatedAt 为空时填当前时间。
	SaveK8sCluster(*K8sCluster) error
	// DeleteK8sCluster 删除集群配置，返回是否删除成功（不存在返回 false）。
	DeleteK8sCluster(id string) bool
}

// TemplateStore OS/中间件部署模板领域：CRUD。
//
// 用于 自动纳管闭环的「裸机→OS→agent」全自动安装链路 + 应用编排中间件实例化：
//   - OSTemplate 定义 OS 安装模板（kickstart/preseed），Provision 时按设备元信息匹配推送；
//   - MiddlewareTemplate 定义中间件（MySQL/Redis/Kafka/...）标准化部署配置，供应用编排复用。
//
// 与 K8sClusterStore 同样按租户隔离；Config 为敏感内容（含 root 密码/连接串等），
// 调用方（API 层）负责脱敏后再返回前端。
type TemplateStore interface {
	// SaveOSTemplate 创建或更新 OS 安装模板（按 ID 幂等）。
	// ID 为空时由 store 分配随机 ID；TenantID 为空时归一为 default；
	// CreatedAt 为空时填当前时间；UpdatedAt 始终刷新。
	SaveOSTemplate(*OSTemplate) error
	// ListOSTemplates 返回 OS 安装模板（按创建时间升序）；tenantID 非空时按租户过滤。
	ListOSTemplates(tenantID string) []*OSTemplate
	// GetOSTemplate 按 ID 返回单个 OS 安装模板（不存在返回 nil）。
	GetOSTemplate(id string) *OSTemplate
	// DeleteOSTemplate 删除 OS 安装模板，返回是否删除成功（不存在返回 false）。
	DeleteOSTemplate(id string) bool
	// SaveMiddlewareTemplate 创建或更新中间件部署模板（按 ID 幂等）。
	// ID 为空时由 store 分配随机 ID；TenantID 为空时归一为 default；
	// CreatedAt 为空时填当前时间；UpdatedAt 始终刷新。
	SaveMiddlewareTemplate(*MiddlewareTemplate) error
	// ListMiddlewareTemplates 返回中间件部署模板（按创建时间升序）；tenantID 非空时按租户过滤。
	ListMiddlewareTemplates(tenantID string) []*MiddlewareTemplate
	// GetMiddlewareTemplate 按 ID 返回单个中间件部署模板（不存在返回 nil）。
	GetMiddlewareTemplate(id string) *MiddlewareTemplate
	// DeleteMiddlewareTemplate 删除中间件部署模板，返回是否删除成功（不存在返回 false）。
	DeleteMiddlewareTemplate(id string) bool
}

// RefreshTokenStore 刷新令牌领域：access token 过期后的无感续期。
//
// 与 TokenStore（B1 install token，一次性、限时、HMAC 签名）解耦——refresh token
// 生命周期长（如 7d）、可多次使用（直至过期或被主动吊销）、由调用方（auth 层）
// 生成随机串并取 SHA-256 摘要后存库（明文不落库）。
//
// 设计要点：
//   - 按 TokenHash（SHA-256 摘要）为主键，CRUD 均以摘要为键；
//   - DeviceFP 用于校验 refresh token 仅在原签发设备上使用（防跨设备重放）；
//   - 多副本控制面共享同一 MySQL 时，refresh token 落库以实现跨副本续期一致性。
type RefreshTokenStore interface {
	// SaveRefreshToken 保存/更新一个 refresh token（按 TokenHash 幂等 upsert）。
	// 调用方须先对明文 token 取 SHA-256 摘要填入 rt.TokenHash，并填好 UserID/TenantID/
	// DeviceFP/ExpiresAt/CreatedAt。TenantID 为空时归一为 default。返回持久化错误。
	SaveRefreshToken(rt *RefreshToken) error
	// GetRefreshToken 按 TokenHash 返回单个 refresh token（不存在返回 nil）。
	// 续期流程：调用方传入明文 token 的摘要，store 据此查回元信息（UserID/TenantID/
	// DeviceFP/ExpiresAt），校验未过期且 DeviceFP 匹配后签发新 access token。
	GetRefreshToken(tokenHash string) *RefreshToken
	// DeleteRefreshToken 按 TokenHash 删除 refresh token（登出/吊销/过期清理用）。
	// 返回是否删除成功（不存在返回 false）。
	DeleteRefreshToken(tokenHash string) bool
	// ConsumeRefreshToken 原子消费 refresh token：读取并立即删除，返回被消费的 token。
	// 多副本并发下同一 token 只能被消费一次（原子 Get+Delete），防重放。
	// 不存在返回 (nil, false)。调用方拿到后自行校验过期/设备指纹。
	// 原 consumeRefreshToken 的 Get→Delete 两步在并发下可被双消费，
	// 此方法将读取+删除收敛为单次原子操作（MemoryStore 用互斥锁，SQLStore 用事务）。
	ConsumeRefreshToken(tokenHash string) (*RefreshToken, bool)
	// CleanupRefreshTokens 清理过期 refresh token（未过期则保留）。
	// 用于登录防爆破等场景：过期 token 已无意义，但需保证幂等安全。
	// 返回清理条数。仅 leader 周期调用。
	CleanupRefreshTokens() int
}

// SilenceStore 静默规则领域（M2 集成）：告警事件按标签匹配 + 时间窗口抑制。
//
// 与 AlertStore.SilenceAlert（单条告警静默）解耦——SilenceRule 是基于标签匹配的
// 批量静默规则，可一次抑制同标签的所有告警事件（如"所有 critical 告警静默 1h"）。
type SilenceStore interface {
	// CreateSilence 创建静默规则。ID 为空时由 store 分配随机 ID；
	// TenantID 为空时归一为 default。返回持久化后的规则（含分配的 ID）。
	CreateSilence(*SilenceRule) *SilenceRule
	// DeleteSilence 删除静默规则，返回是否删除成功（不存在或租户不匹配返回 false）。
	DeleteSilence(id, tenantID string) bool
	// ListSilences 返回静默规则；tenantID 非空时按租户过滤。
	ListSilences(tenantID string) []*SilenceRule
	// GetSilence 按 ID 返回单个静默规则（M2 持久化补全；不存在返回 nil）。
	GetSilence(id string) *SilenceRule
}

// NotifyChannelStore 通知渠道领域（M2 集成）：CRUD。
//
// 渠道定义通知推送目标（钉钉/企业微信/飞书/Slack/邮件/Webhook），
// 告警规则通过 NotifyChannels 引用渠道 ID 列表。
// Config 为敏感内容（含 webhook URL/secret/SMTP 密码等），API 层负责脱敏。
type NotifyChannelStore interface {
	// CreateNotifyChannel 创建通知渠道。ID 为空时由 store 分配随机 ID；
	// TenantID 为空时归一为 default。返回持久化后的渠道（含分配的 ID）。
	CreateNotifyChannel(*NotifyChannel) *NotifyChannel
	// UpdateNotifyChannel 更新通知渠道。不存在返回 false。
	UpdateNotifyChannel(*NotifyChannel) bool
	// DeleteNotifyChannel 删除通知渠道，返回是否删除成功（不存在或租户不匹配返回 false）。
	DeleteNotifyChannel(id, tenantID string) bool
	// GetNotifyChannel 按 ID 返回单个通知渠道（不存在返回 nil）。
	GetNotifyChannel(id string) *NotifyChannel
	// ListNotifyChannels 返回通知渠道；tenantID 非空时按租户过滤。
	ListNotifyChannels(tenantID string) []*NotifyChannel
}

// NotifyTemplateStore 通知模板领域（M2 集成）：CRUD。
//
// 模板定义通知消息的标题/正文（Go text/template 变量替换），
// 渠道推送时按模板渲染产出消息正文。
type NotifyTemplateStore interface {
	// CreateNotifyTemplate 创建通知模板。ID 为空时由 store 分配随机 ID；
	// TenantID 为空时归一为 default。返回持久化后的模板（含分配的 ID）。
	CreateNotifyTemplate(*NotifyTemplate) *NotifyTemplate
	// UpdateNotifyTemplate 更新通知模板。不存在返回 false。
	UpdateNotifyTemplate(*NotifyTemplate) bool
	// DeleteNotifyTemplate 删除通知模板，返回是否删除成功（不存在或租户不匹配返回 false）。
	DeleteNotifyTemplate(id, tenantID string) bool
	// GetNotifyTemplate 按 ID 返回单个通知模板（不存在返回 nil）。
	GetNotifyTemplate(id string) *NotifyTemplate
	// ListNotifyTemplates 返回通知模板；tenantID 非空时按租户过滤。
	ListNotifyTemplates(tenantID string) []*NotifyTemplate
}

// AgentLogStore agent 日志上报领域：agent 经 gRPC ReportLogs 上报的日志批次落库。
//
// 与 logstore.LogStore（M6 日志检索后端）解耦——本接口仅承接 agent 上报的 LogReport 批次，
// 按 agent 归属租户落库（行级隔离，调用方强制赋值 tenantID，禁止 agent 自报覆盖）；
// logstore.LogStore 负责检索侧（Query/Append），两者可经控制面 handler 桥接。
//
// MemoryStore 用内存 slice 暂存（按 tenantID + logName 分组）；
// SQLStore 同样用内存 slice 暂存（agent 上报日志的高频写入不宜直接落 MySQL，
// 检索侧由 logstore.SQLLogStore 走独立表/连接池承担）。
type AgentLogStore interface {
	// SaveLogs 把 agent 上报的日志批次落库。tenantID 为 agent 归属租户（由控制面回填，agent 不可伪造）；
	// report 为上报批次（AgentID/LogName/Lines 等）。report 为 nil 时直接返回。
	SaveLogs(tenantID string, report *proto.LogReport) error
	// AgentLogs 查询已落库的 agent 日志；tenantID 非空时按租户过滤，
	// agentID 非空时按 agent 过滤，logName 非空时按日志标识过滤。
	// 供 HTTP API GET /api/v1/agent-logs 检索。返回深拷贝避免外部并发修改。
	AgentLogs(tenantID, agentID, logName string) []proto.LogReport
}

// QuotaConfig 租户资源配额配置（多租户资源配额与计费）。
//
// 每个字段表示该租户对应资源允许的最大数量；0 表示不限制（无限配额）。
// 由 QuotaManager 在 CheckDevice/CheckTask/CheckAlert 时读取，
// 与 store 中当前用量比较，超额返回 ErrQuotaExceeded。
//
// 设计要点：
//   - 定义在 store 包而非 controlplane 包，避免 store→controlplane 循环依赖；
//   - 字段均为值类型，浅拷贝即深拷贝，便于并发安全返回拷贝；
//   - JSON 标签用于 API 响应序列化（GET /api/v1/quotas/{tenantID}）。
type QuotaConfig struct {
	MaxDevices int `json:"maxDevices"` // 最大设备数（0=不限）
	MaxTasks   int `json:"maxTasks"`   // 最大任务数（0=不限）
	MaxAlerts  int `json:"maxAlerts"`  // 最大告警数（0=不限）
}

// QuotaStore 配额配置存储（多租户资源配额）。
//
// 与 DeviceStore/TaskStore/AlertStore 等领域小接口同构：
//   - GetQuota 返回租户配额配置（未设置返回 nil + nil error，由调用方回退默认配额）；
//   - SetQuota 设置/更新租户配额（按 tenantID 幂等 upsert）。
//
// MemoryStore 用 map 内存存储；SQLStore 用 quota_configs 表持久化（migrations/006_quota_configs.sql）。
// 配额检查（CheckDevice/CheckTask/CheckAlert）在 controlplane.QuotaManager 中实现，
// 结合 QuotaStore 读取配额 + DeviceStore/TaskStore/AlertStore 读取当前用量做比较。
type QuotaStore interface {
	// GetQuota 返回租户配额配置（不存在返回 nil，由调用方回退默认配额）。
	GetQuota(tenantID string) (*QuotaConfig, error)
	// SetQuota 设置或更新租户配额（按 tenantID 幂等 upsert）。返回持久化错误。
	SetQuota(tenantID string, cfg *QuotaConfig) error
}

// ServiceDiscoveryStore 服务发现领域：注册/反注册/实例查询/心跳/过期清理。
// 按 (TenantID, ServiceID) 唯一标识一个服务实例；ServiceName 用于聚合查询。
// 心跳驱动健康状态：LastHeartbeat 超过阈值由 StaleServices 标记下线。
type ServiceDiscoveryStore interface {
	// RegisterService 注册一个服务实例（按 ServiceID 幂等 upsert）。
	// 已存在则更新 Address/Port/Metadata/Status/LastHeartbeat；不存在则新建。
	RegisterService(inst *ServiceInstance) *ServiceInstance
	// DeregisterService 反注册服务实例（按 tenantID + serviceID）。返回是否删除成功。
	DeregisterService(tenantID, serviceID string) bool
	// ServiceInstances 返回指定服务名下的全部实例（按 tenantID 隔离）。
	ServiceInstances(tenantID, serviceName string) []*ServiceInstance
	// AllServices 返回全部服务实例（按 tenantID 隔离；空串=全部租户）。
	AllServices(tenantID string) []*ServiceInstance
	// HeartbeatService 服务实例心跳：刷新 LastHeartbeat 与 Status。返回是否已知该实例。
	HeartbeatService(tenantID, serviceID, status string) bool
	// StaleServices 返回最后心跳早于 maxAge 的不健康实例（按 tenantID 隔离）。
	// 控制面周期调用以驱动健康检查 / 自动摘流。
	StaleServices(tenantID string, maxAge time.Duration) []*ServiceInstance
}

// ConfigStore 配置中心领域：Get/Set/Delete/List + 版本历史 + 发布。
// 按 (TenantID, Key) 唯一；每次 SetConfig 产生新版本并写入历史（ConfigHistory 查询）。
// PublishConfig 用于配置变更的灰度/广播触发（MVP 仅标记版本，后续可联动事件总线）。
type ConfigStore interface {
	// GetConfig 按 (tenantID, key) 返回当前配置项；不存在返回 (nil, false)。
	GetConfig(tenantID, key string) (*ConfigItem, bool)
	// SetConfig 写入/更新配置（按 key 幂等）。已存在则 Version+1 并写入历史；不存在则 Version=1。
	// 返回更新后的配置项。
	SetConfig(item *ConfigItem) *ConfigItem
	// DeleteConfig 删除配置（按 tenantID + key）。返回是否删除成功。
	DeleteConfig(tenantID, key string) bool
	// ListConfigs 列出指定租户的全部配置（按 key 升序）。
	ListConfigs(tenantID string) []*ConfigItem
	// ConfigHistory 返回指定配置的版本历史（按 version 升序；最多保留最近 N 条，N 由实现决定）。
	ConfigHistory(tenantID, key string) []*ConfigItem
	// PublishConfig 发布配置变更（标记当前版本为已发布；MVP 仅返回最新配置，后续可触发事件）。
	// 不存在返回 (nil, false)。
	PublishConfig(tenantID, key string) (*ConfigItem, bool)
}

// SecretStore 密钥管理领域：Get/Set/Delete/List + 轮换 + 版本历史。
// 按 (TenantID, Key, Version) 唯一标识一个密钥版本；RotateSecret 产生新版本。
// GetSecret 返回明文值（仅在内部流转；API 层须转为 SecretMeta 脱敏视图）。
type SecretStore interface {
	// GetSecret 按 (tenantID, key) 返回当前版本密钥明文项；不存在返回 (nil, false)。
	GetSecret(tenantID, key string) (*SecretItem, bool)
	// SetSecret 写入/轮换密钥（按 key 幂等）。已存在则 Version+1；不存在则 Version=1。
	// 返回更新后的密钥元信息（脱敏视图，不含 Value）。
	SetSecret(item *SecretItem, tenantID string) *SecretMeta
	// DeleteSecret 删除密钥（按 tenantID + key，含全部历史版本）。返回是否删除成功。
	DeleteSecret(tenantID, key string) bool
	// ListSecrets 列出指定租户的全部密钥元信息（脱敏视图，按 key 升序）。
	ListSecrets(tenantID string) []*SecretMeta
	// RotateSecret 轮换密钥：用新值产生新版本。不存在则等价于 SetSecret。
	// 返回新版本元信息。
	RotateSecret(tenantID, key, newValue string) *SecretMeta
	// SecretVersions 返回指定密钥的全部版本元信息（按 version 升序）。
	SecretVersions(tenantID, key string) []*SecretMeta
}

// TicketStore 工单管理领域：创建/查询/更新/列表/关闭。
type TicketStore interface {
	// CreateTicket 创建工单。ID 为空时由 store 分配随机 ID；
	// TenantID 为空时归一为 default。返回持久化后的工单（含分配的 ID）。
	CreateTicket(tenantID string, t *Ticket) *Ticket
	// GetTicket 按 (tenantID, id) 返回单个工单（不存在返回 (nil, false)）。
	GetTicket(tenantID, id string) (*Ticket, bool)
	// UpdateTicket 更新工单（按 t.ID 定位，校验 tenantID 归属）。不存在或越权返回 (nil, false)。
	UpdateTicket(tenantID string, t *Ticket) (*Ticket, bool)
	// ListTickets 返回指定租户的工单列表（按 filter 过滤 + 按创建时间降序）。
	ListTickets(tenantID string, filter TicketFilter) []*Ticket
	// CloseTicket 关闭工单：置 Status="closed" + ResolvedAt=now。不存在或越权返回 (nil, false)。
	CloseTicket(tenantID, id string) (*Ticket, bool)
}

// TrafficStore 流量治理领域：CRUD + 启用/禁用。
type TrafficStore interface {
	CreatePolicy(tenantID string, p *TrafficPolicy) *TrafficPolicy
	GetPolicy(tenantID, id string) (*TrafficPolicy, bool)
	UpdatePolicy(tenantID string, p *TrafficPolicy) (*TrafficPolicy, bool)
	ListPolicies(tenantID string) []*TrafficPolicy
	DeletePolicy(tenantID, id string) bool
	EnablePolicy(tenantID, id string) (*TrafficPolicy, bool)
	DisablePolicy(tenantID, id string) (*TrafficPolicy, bool)
}

// PipelineStore CI/CD 流水线领域：模板 + 运行记录。
type PipelineStore interface {
	CreateTemplate(tenantID string, t *PipelineTemplate) *PipelineTemplate
	GetTemplate(tenantID, id string) (*PipelineTemplate, bool)
	ListTemplates(tenantID string) []*PipelineTemplate
	DeleteTemplate(tenantID, id string) bool
	CreateRun(tenantID string, r *PipelineRun) *PipelineRun
	GetRun(tenantID, id string) (*PipelineRun, bool)
	ListRuns(tenantID string, templateID string) []*PipelineRun
	UpdateRun(tenantID string, r *PipelineRun) (*PipelineRun, bool)
}

// ArgoCDStore ArgoCD 应用管理领域。
type ArgoCDStore interface {
	CreateApp(tenantID string, a *ArgoCDApp) *ArgoCDApp
	GetApp(tenantID, id string) (*ArgoCDApp, bool)
	UpdateApp(tenantID string, a *ArgoCDApp) (*ArgoCDApp, bool)
	ListApps(tenantID string) []*ArgoCDApp
	DeleteApp(tenantID, id string) bool
	SyncApp(tenantID, id string) (*ArgoCDApp, bool)
}

// ComplianceStore P3 合规报告领域：CRUD。
//
// 由合规检查引擎扫描设备产出 ComplianceReport，按 (TenantID, ID) 唯一标识。
// SaveReport 创建或更新报告（ID 为空时由 store 分配）；ListReports 按租户列出。
type ComplianceStore interface {
	// SaveReport 保存合规报告（ID 为空时由 store 分配随机 ID）。
	// TenantID 为空时归一为 default。返回持久化后的报告（含分配的 ID）。
	SaveReport(tenantID string, r *ComplianceReport) *ComplianceReport
	// GetReport 按 (tenantID, id) 返回单个合规报告（不存在返回 (nil, false)）。
	GetReport(tenantID, id string) (*ComplianceReport, bool)
	// ListReports 返回指定租户的全部合规报告（按创建时间降序）。
	ListReports(tenantID string) []*ComplianceReport
	// DeleteReport 删除合规报告，返回是否删除成功（不存在或租户不匹配返回 false）。
	DeleteReport(tenantID, id string) bool
}

// BackupStore P3 灾备备份领域：CRUD。
//
// 由灾备恢复 API 创建 BackupRecord，按 (TenantID, ID) 唯一标识。
// CreateBackup 创建备份记录（ID 为空时由 store 分配）；ListBackups 按租户列出。
type BackupStore interface {
	// CreateBackup 创建备份记录（ID 为空时由 store 分配随机 ID）。
	// TenantID 为空时归一为 default。返回持久化后的记录（含分配的 ID）。
	CreateBackup(tenantID string, b *BackupRecord) *BackupRecord
	// GetBackup 按 (tenantID, id) 返回单个备份记录（不存在返回 (nil, false)）。
	GetBackup(tenantID, id string) (*BackupRecord, bool)
	// ListBackups 返回指定租户的全部备份记录（按创建时间降序）。
	ListBackups(tenantID string) []*BackupRecord
	// DeleteBackup 删除备份记录，返回是否删除成功（不存在或租户不匹配返回 false）。
	DeleteBackup(tenantID, id string) bool
}

// NetworkStore 网络管理领域（Phase 4）：网络设备 CRUD + 监控指标 + 配置下发。
//
// 与 DeviceStore（纳管主机/agent）解耦——NetworkDevice 是网络拓扑中的网络设备
// （switch/router/firewall/load_balancer），通过 SNMP/CLI 管理而非 agent。
// 按 (TenantID, ID) 唯一标识，按 TenantID 隔离。
type NetworkStore interface {
	// CreateNetworkDevice 创建网络设备（ID 为空时由 store 分配随机 ID）。
	// TenantID 为空时归一为 default。返回持久化后的设备（含分配的 ID）。
	CreateNetworkDevice(tenantID string, d *NetworkDevice) *NetworkDevice
	// GetNetworkDevice 按 (tenantID, id) 返回单个网络设备（不存在返回 (nil, false)）。
	GetNetworkDevice(tenantID, id string) (*NetworkDevice, bool)
	// ListNetworkDevices 返回指定租户的全部网络设备（按创建时间降序）。
	ListNetworkDevices(tenantID string) []*NetworkDevice
	// UpdateNetworkDevice 更新网络设备（按 d.ID 定位，校验 tenantID 归属）。不存在或越权返回 (nil, false)。
	UpdateNetworkDevice(tenantID string, d *NetworkDevice) (*NetworkDevice, bool)
	// DeleteNetworkDevice 删除网络设备，返回是否删除成功（不存在或租户不匹配返回 false）。
	DeleteNetworkDevice(tenantID, id string) bool
	// StoreNetworkMetrics 存储网络设备监控指标（按 deviceID 关联，保留最近 N 条历史）。
	StoreNetworkMetrics(deviceID string, m *NetworkMetrics)
	// GetNetworkMetrics 返回网络设备最近一次监控指标（不存在返回 nil）。
	GetNetworkMetrics(deviceID string) *NetworkMetrics
	// UpdateNetworkConfig 下发网络配置（更新 d.Config 字段，返回更新后的设备）。
	UpdateNetworkConfig(tenantID, id, config string) (*NetworkDevice, bool)
}

// AutomationStore 自动化闭环领域（Phase 4）：规则 CRUD + 启用/禁用 + 执行记录。
//
// 规则由触发器（Trigger）+ 动作列表（Actions）组成"条件→动作"闭环。
// 按 (TenantID, ID) 唯一标识，按 TenantID 隔离。
type AutomationStore interface {
	// CreateAutomationRule 创建自动化规则（ID 为空时由 store 分配随机 ID）。
	// TenantID 为空时归一为 default。返回持久化后的规则（含分配的 ID）。
	CreateAutomationRule(tenantID string, r *AutomationRule) *AutomationRule
	// GetAutomationRule 按 (tenantID, id) 返回单个规则（不存在返回 (nil, false)）。
	GetAutomationRule(tenantID, id string) (*AutomationRule, bool)
	// ListAutomationRules 返回指定租户的全部规则（按创建时间降序）。
	ListAutomationRules(tenantID string) []*AutomationRule
	// UpdateAutomationRule 更新规则（按 r.ID 定位，校验 tenantID 归属）。不存在或越权返回 (nil, false)。
	UpdateAutomationRule(tenantID string, r *AutomationRule) (*AutomationRule, bool)
	// DeleteAutomationRule 删除规则，返回是否删除成功（不存在或租户不匹配返回 false）。
	DeleteAutomationRule(tenantID, id string) bool
	// EnableAutomationRule 启用规则（置 Enabled=true）。不存在或越权返回 (nil, false)。
	EnableAutomationRule(tenantID, id string) (*AutomationRule, bool)
	// DisableAutomationRule 禁用规则（置 Enabled=false）。不存在或越权返回 (nil, false)。
	DisableAutomationRule(tenantID, id string) (*AutomationRule, bool)
	// CreateAutomationExecution 创建执行记录（ID 为空时由 store 分配随机 ID）。
	CreateAutomationExecution(tenantID string, e *AutomationExecution) *AutomationExecution
	// GetAutomationExecution 按 (tenantID, id) 返回单条执行记录（不存在返回 (nil, false)）。
	GetAutomationExecution(tenantID, id string) (*AutomationExecution, bool)
	// ListAutomationExecutions 返回指定租户的执行记录列表（按开始时间降序，limit<=0 时返回全部）。
	ListAutomationExecutions(tenantID string, limit int) []*AutomationExecution
}

// SLOStore SLO 管理领域：CRUD + SLI 状态查询。
type SLOStore interface {
	// CreateSLO 创建 SLO。ID 为空时由 store 分配随机 ID；
	// TenantID 为空时归一为 default。返回持久化后的 SLO（含分配的 ID）。
	CreateSLO(tenantID string, slo *SLO) *SLO
	// GetSLO 按 (tenantID, id) 返回单个 SLO（不存在返回 (nil, false)）。
	GetSLO(tenantID, id string) (*SLO, bool)
	// UpdateSLO 更新 SLO（按 slo.ID 定位，校验 tenantID 归属）。不存在或越权返回 (nil, false)。
	UpdateSLO(tenantID string, slo *SLO) (*SLO, bool)
	// ListSLOs 返回指定租户的全部 SLO（按创建时间升序）。
	ListSLOs(tenantID string) []*SLO
	// DeleteSLO 删除 SLO，返回是否删除成功（不存在或租户不匹配返回 false）。
	DeleteSLO(tenantID, id string) bool
	// SLIStatus 返回指定 SLO 下各 SLI 的当前状态（MVP 返回模拟状态）。
	SLIStatus(tenantID, id string) []*SLIStatus
}

// WebhookStore P5 Webhook 管理：CRUD + 投递记录。
type WebhookStore interface {
	// CreateWebhook 创建 Webhook。ID 为空时由 store 分配随机 ID；
	// TenantID 为空时归一为 default。返回持久化后的 Webhook（含分配的 ID）。
	CreateWebhook(tenantID string, wh *Webhook) *Webhook
	// GetWebhook 按 (tenantID, id) 返回单个 Webhook（不存在返回 (nil, false)）。
	GetWebhook(tenantID, id string) (*Webhook, bool)
	// UpdateWebhook 更新 Webhook（按 wh.ID 定位，校验 tenantID 归属）。不存在或越权返回 (nil, false)。
	UpdateWebhook(tenantID string, wh *Webhook) (*Webhook, bool)
	// ListWebhooks 返回指定租户的全部 Webhook（按创建时间降序）。
	ListWebhooks(tenantID string) []*Webhook
	// DeleteWebhook 删除 Webhook，返回是否删除成功（不存在或租户不匹配返回 false）。
	DeleteWebhook(tenantID, id string) bool
	// ListWebhookDeliveries 返回指定 Webhook 的投递记录（按投递时间降序）。
	ListWebhookDeliveries(tenantID, webhookID string) []*WebhookDelivery
}

// ScriptStore P5 自定义脚本：CRUD + 执行记录。
type ScriptStore interface {
	// CreateScript 创建脚本。ID 为空时由 store 分配随机 ID；
	// TenantID 为空时归一为 default。返回持久化后的脚本（含分配的 ID）。
	CreateScript(tenantID string, s *Script) *Script
	// GetScript 按 (tenantID, id) 返回单个脚本（不存在返回 (nil, false)）。
	GetScript(tenantID, id string) (*Script, bool)
	// UpdateScript 更新脚本（按 s.ID 定位，校验 tenantID 归属）。不存在或越权返回 (nil, false)。
	UpdateScript(tenantID string, s *Script) (*Script, bool)
	// ListScripts 返回指定租户的全部脚本（按创建时间降序）。
	ListScripts(tenantID string) []*Script
	// DeleteScript 删除脚本，返回是否删除成功（不存在或租户不匹配返回 false）。
	DeleteScript(tenantID, id string) bool
	// ListScriptExecutions 返回指定脚本的执行记录（按开始时间降序）。
	ListScriptExecutions(tenantID, scriptID string) []*ScriptExecution
}

// Store 控制面注册表的可插拔持久化组合接口。
// 由 12 个领域小接口组合而成（拆分 + 用户中心扩展 + K8s 集群管理 + OS/中间件模板 + 刷新令牌），
// 方法签名刻意与旧版内存 Registry 保持一致，便于平滑替换。
// 数据本地化，默认 memory；生产可切换 mysql（MySQL/Redis 私有部署）。
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
	UserStore             // 用户中心：注册/登录/CRUD
	RoleStore             // 角色管理：CRUD
	PermissionStore       // 权限列表：只读
	K8sClusterStore       // K8s 集群管理：CRUD（Phase 3）
	TemplateStore         // OS/中间件部署模板：CRUD
	RefreshTokenStore     // 刷新令牌：续期/吊销
	SilenceStore          // 静默规则：标签匹配 + 时间窗口抑制（M2）
	NotifyChannelStore    // 通知渠道：CRUD（M2）
	NotifyTemplateStore   // 通知模板：CRUD（M2）
	AgentLogStore         // agent 日志上报：落库 + 检索
	QuotaStore            // 租户配额：Get/Set（多租户资源配额）
	ServiceDiscoveryStore // P0.3 服务发现：注册/心跳/实例查询
	ConfigStore           // P0.3 配置中心：Get/Set/版本历史/发布
	SecretStore           // P0.3 密钥管理：Get/Set/轮换/版本历史
	TicketStore           // P1 工单管理：创建/查询/更新/关闭
	SLOStore              // P1 SLO 管理：CRUD + SLI 状态
	TrafficStore          // P2 流量治理：策略 CRUD + 启停
	PipelineStore         // P2 CI/CD 流水线：模板 + 运行记录
	ArgoCDStore           // P2 ArgoCD 应用：CRUD + 同步
	ComplianceStore       // P3 合规报告：CRUD
	BackupStore           // P3 灾备备份：CRUD
	NetworkStore          // P4 网络管理：设备 CRUD + 指标 + 配置下发
	AutomationStore       // P4 自动化闭环：规则 CRUD + 启停 + 执行记录
	WebhookStore          // P5 Webhook：CRUD + 投递记录
	ScriptStore           // P5 自定义脚本：CRUD + 执行记录

	// WithDemo 设置是否开启演示模式：开启时每个 agent 注册预置 uname -a 示例任务。
	WithDemo(bool) Store
}

// 编译期断言：确保 MemoryStore / SQLStore 实现各领域小接口。
// 任一方法缺失会在编译期立刻暴露（而非运行期），降低后续拆分消费方时的回归风险。
var (
	_ DeviceStore           = (*MemoryStore)(nil)
	_ TaskStore             = (*MemoryStore)(nil)
	_ AlertStore            = (*MemoryStore)(nil)
	_ AuditStore            = (*MemoryStore)(nil)
	_ TokenStore            = (*MemoryStore)(nil)
	_ LeaderStore           = (*MemoryStore)(nil)
	_ UserStore             = (*MemoryStore)(nil)
	_ RoleStore             = (*MemoryStore)(nil)
	_ PermissionStore       = (*MemoryStore)(nil)
	_ K8sClusterStore       = (*MemoryStore)(nil)
	_ TemplateStore         = (*MemoryStore)(nil)
	_ RefreshTokenStore     = (*MemoryStore)(nil)
	_ SilenceStore          = (*MemoryStore)(nil)
	_ NotifyChannelStore    = (*MemoryStore)(nil)
	_ NotifyTemplateStore   = (*MemoryStore)(nil)
	_ AgentLogStore         = (*MemoryStore)(nil)
	_ QuotaStore            = (*MemoryStore)(nil)
	_ ServiceDiscoveryStore = (*MemoryStore)(nil)
	_ ConfigStore           = (*MemoryStore)(nil)
	_ SecretStore           = (*MemoryStore)(nil)
	_ TicketStore           = (*MemoryStore)(nil)
	_ SLOStore              = (*MemoryStore)(nil)
	_ TrafficStore          = (*MemoryStore)(nil)
	_ PipelineStore         = (*MemoryStore)(nil)
	_ ArgoCDStore           = (*MemoryStore)(nil)
	_ ComplianceStore       = (*MemoryStore)(nil)
	_ BackupStore           = (*MemoryStore)(nil)
	_ NetworkStore          = (*MemoryStore)(nil)
	_ AutomationStore       = (*MemoryStore)(nil)
	_ WebhookStore          = (*MemoryStore)(nil)
	_ ScriptStore           = (*MemoryStore)(nil)
	_ Store                 = (*MemoryStore)(nil)

	_ DeviceStore           = (*SQLStore)(nil)
	_ TaskStore             = (*SQLStore)(nil)
	_ AlertStore            = (*SQLStore)(nil)
	_ AuditStore            = (*SQLStore)(nil)
	_ TokenStore            = (*SQLStore)(nil)
	_ LeaderStore           = (*SQLStore)(nil)
	_ UserStore             = (*SQLStore)(nil)
	_ RoleStore             = (*SQLStore)(nil)
	_ PermissionStore       = (*SQLStore)(nil)
	_ K8sClusterStore       = (*SQLStore)(nil)
	_ TemplateStore         = (*SQLStore)(nil)
	_ RefreshTokenStore     = (*SQLStore)(nil)
	_ SilenceStore          = (*SQLStore)(nil)
	_ NotifyChannelStore    = (*SQLStore)(nil)
	_ NotifyTemplateStore   = (*SQLStore)(nil)
	_ AgentLogStore         = (*SQLStore)(nil)
	_ QuotaStore            = (*SQLStore)(nil)
	_ ServiceDiscoveryStore = (*SQLStore)(nil)
	_ ConfigStore           = (*SQLStore)(nil)
	_ SecretStore           = (*SQLStore)(nil)
	_ TicketStore           = (*SQLStore)(nil)
	_ SLOStore              = (*SQLStore)(nil)
	_ TrafficStore          = (*SQLStore)(nil)
	_ PipelineStore         = (*SQLStore)(nil)
	_ ArgoCDStore           = (*SQLStore)(nil)
	_ ComplianceStore       = (*SQLStore)(nil)
	_ BackupStore           = (*SQLStore)(nil)
	_ NetworkStore          = (*SQLStore)(nil)
	_ AutomationStore       = (*SQLStore)(nil)
	_ WebhookStore          = (*SQLStore)(nil)
	_ ScriptStore           = (*SQLStore)(nil)
	_ Store                 = (*SQLStore)(nil)
)
