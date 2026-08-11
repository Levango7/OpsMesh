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

// DeviceStore 设备/Agent 纳管领域：注册、心跳、设备视图、退役归档、监控指标。
// 共 12 个方法，覆盖 P0-2 真实网段发现、P2-17 agent 直查、F5 离线归档、监控指标采集。
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
	// StoreDeviceMetrics 存储设备最新监控指标（agent 心跳上报，仅保留最新值）。
	// deviceID 为空或 metrics 为 nil 时直接返回。控制面缓存最新值供 GET /api/v1/devices/{id}/metrics 查询。
	StoreDeviceMetrics(deviceID string, metrics *proto.DeviceMetrics)
	// DeviceMetrics 返回设备最新监控指标（无数据时返回 nil）。
	DeviceMetrics(deviceID string) *proto.DeviceMetrics
	// DeviceMetricsHistory 返回设备监控指标历史时序（环形缓冲查询，task 223）。
	// since 为零值时返回全部已存储历史；否则返回 CollectedAt >= since 的快照（按时间升序）。
	// 无数据时返回 nil。控制面 GET /api/v1/devices/{id}/metrics?range=2h 调用此方法。
	DeviceMetricsHistory(deviceID string, since time.Time) []proto.DeviceMetrics
	// AgentSecret 返回该 agent 的 HMAC 签名密钥（task 81 gRPC 身份绑定）。
	// 由 Register 时为每个 agent 随机生成 32 字节 hex 串并落库；agent 拉任务/上报/轮询取消时
	// 用此密钥计算 HMAC-SHA256(secret, timestamp+agentID) 签名，控制面据此验证 agent 身份，
	// 不再纯信任 agent 自报的 AgentID（防冒领任务/伪造上报）。agent 不存在或未生成密钥时返回空串。
	AgentSecret(agentID string) string
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
	// TaskByID 按 taskID 返回单条任务（不存在返回 nil）。
	// 用于按 ID 直查场景（如结果查询的租户归属校验），避免遍历 AllTasks（O(N) → O(1)）。
	// 返回深拷贝避免外部并发修改破坏内部状态。
	TaskByID(taskID string) *proto.Task
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
	// ApproveTask 审批通过任务（task 100）：将 pending_approval 状态翻转回 pending，
	// 记录审批人/审批时间。仅 pending_approval 状态可审批；其他状态返回 false。
	// tenantID 非空时校验任务归属，越权返回 false。
	ApproveTask(id, tenantID, approvedBy string) bool
	// RejectTask 驳回任务（task 100）：将 pending_approval 状态置为 rejected，
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
	// CreateAlertRule 创建告警规则（task 100）：ID 为空时由 store 分配随机 ID；
	// TenantID 为空时归一为 default。返回持久化后的规则（含分配的 ID）。
	CreateAlertRule(*AlertRule) *AlertRule
	// ListAlertRules 返回告警规则（task 100）；tenantID 非空时按租户过滤。
	ListAlertRules(tenantID string) []*AlertRule
	// DeleteAlertRule 删除告警规则（task 100），返回是否删除成功（不存在返回 false）。
	DeleteAlertRule(id string) bool
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
	// ChangePassword 改密（安全债 85）：按 userID 定位，写入新的 bcrypt 哈希，
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
	// ListK8sClusters 返回 K8s 集群配置（按创建时间升序）；tenantID 非空时仅返回同租户集群（task 88 租户隔离）。
	ListK8sClusters(tenantID string) []*K8sCluster
	// GetK8sCluster 按 ID 返回单个集群配置（不存在返回 nil）。
	GetK8sCluster(id string) *K8sCluster
	// SaveK8sCluster 创建或更新集群配置（按 ID 幂等），返回持久化错误（task 92）。
	// ID 为空时由 store 分配随机 ID；CreatedAt/UpdatedAt 为空时填当前时间。
	SaveK8sCluster(*K8sCluster) error
	// DeleteK8sCluster 删除集群配置，返回是否删除成功（不存在返回 false）。
	DeleteK8sCluster(id string) bool
}

// TemplateStore OS/中间件部署模板领域（task 100）：CRUD。
//
// 用于 B1 自动纳管闭环的「裸机→OS→agent」全自动安装链路 + 应用编排中间件实例化：
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

// RefreshTokenStore 刷新令牌领域（task 111）：access token 过期后的无感续期。
//
// 与 TokenStore（B1 install token，一次性、限时、HMAC 签名）解耦——refresh token
// 生命周期长（如 7d）、可多次使用（直至过期或被主动吊销）、由调用方（auth 层）
// 生成随机串并取 SHA-256 摘要后存库（P1-F7 明文不落库）。
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
	// P1-G4：原 consumeRefreshToken 的 Get→Delete 两步在并发下可被双消费，
	// 此方法将读取+删除收敛为单次原子操作（MemoryStore 用互斥锁，SQLStore 用事务）。
	ConsumeRefreshToken(tokenHash string) (*RefreshToken, bool)
}

// SilenceStore 静默规则领域（task 241 M2 集成）：告警事件按标签匹配 + 时间窗口抑制。
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
}

// NotifyChannelStore 通知渠道领域（task 241 M2 集成）：CRUD。
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

// NotifyTemplateStore 通知模板领域（task 241 M2 集成）：CRUD。
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

// Store 控制面注册表的可插拔持久化组合接口。
// 由 12 个领域小接口组合而成（M2-1A 拆分 + 用户中心扩展 + K8s 集群管理 + OS/中间件模板 + 刷新令牌），
// 方法签名刻意与旧版内存 Registry 保持一致，便于平滑替换。
// U-04: 数据本地化，默认 memory；生产可切换 mysql（MySQL/Redis 私有部署）。
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
	UserStore         // 用户中心：注册/登录/CRUD
	RoleStore         // 角色管理：CRUD
	PermissionStore   // 权限列表：只读
	K8sClusterStore   // K8s 集群管理：CRUD（Phase 3）
	TemplateStore     // OS/中间件部署模板：CRUD（task 100）
	RefreshTokenStore // 刷新令牌：续期/吊销（task 111）
	SilenceStore      // 静默规则：标签匹配 + 时间窗口抑制（task 241 M2）
	NotifyChannelStore    // 通知渠道：CRUD（task 241 M2）
	NotifyTemplateStore   // 通知模板：CRUD（task 241 M2）

	// WithDemo 设置是否开启演示模式（P0-5）：开启时每个 agent 注册预置 uname -a 示例任务。
	WithDemo(bool) Store
}

// 编译期断言：确保 MemoryStore / SQLStore 实现各领域小接口。
// 任一方法缺失会在编译期立刻暴露（而非运行期），降低后续拆分消费方时的回归风险。
var (
	_ DeviceStore         = (*MemoryStore)(nil)
	_ TaskStore           = (*MemoryStore)(nil)
	_ AlertStore          = (*MemoryStore)(nil)
	_ AuditStore          = (*MemoryStore)(nil)
	_ TokenStore          = (*MemoryStore)(nil)
	_ LeaderStore         = (*MemoryStore)(nil)
	_ UserStore           = (*MemoryStore)(nil)
	_ RoleStore           = (*MemoryStore)(nil)
	_ PermissionStore     = (*MemoryStore)(nil)
	_ K8sClusterStore     = (*MemoryStore)(nil)
	_ TemplateStore       = (*MemoryStore)(nil)
	_ RefreshTokenStore   = (*MemoryStore)(nil)
	_ SilenceStore        = (*MemoryStore)(nil)
	_ NotifyChannelStore  = (*MemoryStore)(nil)
	_ NotifyTemplateStore = (*MemoryStore)(nil)
	_ Store               = (*MemoryStore)(nil)

	_ DeviceStore         = (*SQLStore)(nil)
	_ TaskStore           = (*SQLStore)(nil)
	_ AlertStore          = (*SQLStore)(nil)
	_ AuditStore          = (*SQLStore)(nil)
	_ TokenStore          = (*SQLStore)(nil)
	_ LeaderStore         = (*SQLStore)(nil)
	_ UserStore           = (*SQLStore)(nil)
	_ RoleStore           = (*SQLStore)(nil)
	_ PermissionStore     = (*SQLStore)(nil)
	_ K8sClusterStore     = (*SQLStore)(nil)
	_ TemplateStore       = (*SQLStore)(nil)
	_ RefreshTokenStore   = (*SQLStore)(nil)
	_ SilenceStore        = (*SQLStore)(nil)
	_ NotifyChannelStore  = (*SQLStore)(nil)
	_ NotifyTemplateStore = (*SQLStore)(nil)
	_ Store               = (*SQLStore)(nil)
)
