// models.go 定义用户中心（用户/角色/权限）领域数据模型。
//
// 设计目标：为 OpsMesh 控制面提供完整 RBAC 能力，支持：
//   - 用户：注册/登录/查询/CRUD，密码 bcrypt 哈希；
//   - 角色：CRUD，角色绑定一组权限字符串（如 "device:read"）；
//   - 权限：预定义权限列表，按组分类（device/task/alert/cmdb/...）。
//
// 与现有 6 领域（Device/Task/Alert/Audit/Token/Leader）解耦，
// 通过 UserStore/RoleStore/PermissionStore 三个小接口暴露，组合进 Store。
package store

import "time"

// User 用户实体。PasswordHash 为 bcrypt 哈希（绝不存明文）。
// RoleIDs 为该用户绑定的角色 ID 列表（用户经角色间接获得权限）。
type User struct {
	ID                 string    `json:"id"`
	Username           string    `json:"username"`
	Email              string    `json:"email"`
	PasswordHash       string    `json:"-"`      // bcrypt 哈希；JSON 序列化时不输出（防泄露）
	Status             string    `json:"status"` // "active" | "pending" | "rejected" | "disabled"（pending=待管理员审批）
	RoleIDs            []string  `json:"roleIDs"`
	CreatedAt          time.Time `json:"createdAt"`
	MustChangePassword bool      `json:"mustChangePassword"` // 强制改密标记：预置弱口令用户首登须改密（安全债）
	// EffectivePermissions 为角色展开后的有效权限集合（由 /auth/me 计算填充，非持久化字段）。
	// 供前端侧栏按权限过滤功能入口，与后端 RBAC 闸（requireProd）同源，杜绝定义漂移。
	EffectivePermissions []string `json:"permissions"`
}

// Role 角色实体。Permissions 为权限字符串数组（如 ["device:read", "task:write"]）。
type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Permission 权限实体。Name 为权限字符串（如 "device:read"），Group 为所属分组（如 "device"）。
type Permission struct {
	ID          string `json:"id"`
	Name        string `json:"name"` // 如 "device:read"
	Description string `json:"description"`
	Group       string `json:"group"` // 如 "device", "task", "alert"
}

// K8sCluster K8s 集群配置实体（Phase 3 后端 K8s 集群管理）。
//
// 字段说明：
//   - ID：集群唯一标识（创建时由 store 分配随机 ID）；
//   - Name：集群展示名（用户输入，如 "prod-cluster"）；
//   - Server：API Server 地址（从 kubeconfig 解析得到，便于列表展示无需解 kubeconfig）；
//   - Kubeconfig：kubeconfig YAML 内容（敏感，API 返回时须脱敏为 ***）；
//   - Status：连接状态（online/offline/unknown，由 test API 刷新）；
//   - CreatedAt / UpdatedAt：创建/更新时间戳。
//
// 安全要点：Kubeconfig 含集群凭据，绝不原样返回给前端；API 层负责脱敏。
type K8sCluster struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenantId"` // 所属租户（租户隔离；空值保存时归一为 default）
	Name       string    `json:"name"`
	Server     string    `json:"server"`     // API Server 地址
	Kubeconfig string    `json:"kubeconfig"` // kubeconfig 内容（YAML，敏感）
	Status     string    `json:"status"`     // online/offline/unknown
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// AlertRule 告警规则实体：定义基于指标阈值的告警触发条件。
//
// 由控制面告警引擎周期评估：对每条 Enabled 的规则，按 Metric 取设备最新指标，
// 经 Op（> / < / >= / <= / == / !=）与 Threshold 比对，持续 ForDuration 满足
// 则产出一条 Alert（M7）。规则按租户隔离，删除即从评估清单移除。
//
// 字段说明：
//   - ID：规则唯一标识（CreateAlertRule 时由 store 分配随机 ID）；
//   - TenantID：所属租户（隔离；空值保存时归一为 default）；
//   - Metric：指标名（如 "cpu_usage"、"disk_usage"），与 DeviceMetrics 字段对齐；
//   - Op：比较运算符（> / < / >= / <= / == / !=）；
//   - Threshold：阈值（指标值经 Op 比对越线即触发）；
//   - ForDuration：持续满足时长（秒），避免瞬时抖动误报；
//   - Severity：告警级别（warning / critical），产出 Alert 时写入；
//   - Message：告警消息模板（产出 Alert.Message）；
//   - Enabled：是否启用（false 时跳过评估）；
//   - CreatedAt：创建时间戳；
//   - CreatedBy：创建人（M2 持久化，由 controlplane 迁移 globalAlertRules 时填充）。
type AlertRule struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenantID"`
	Metric      string    `json:"metric"`
	Op          string    `json:"op"`
	Threshold   float64   `json:"threshold"`
	ForDuration int       `json:"forDuration"`
	Severity    string    `json:"severity"`
	Message     string    `json:"message"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
	CreatedBy   string    `json:"createdBy"`
}

// OSTemplate OS 安装模板实体：裸机/虚拟机自动安装操作系统的模板配置。
//
// 用于 自动纳管闭环的「裸机→OS→agent」全自动安装链路：
//   - 模板定义 OS 类型（centos/ubuntu/...）、安装源（kickstart/preseed URL）、
//     最小化配置（分区/网络/账户）等；
//   - Provision 时按设备元信息匹配模板，经 IPMI/PXE 推送安装；
//   - 模板按租户隔离，敏感字段（如 rootPasswordHash）由 API 层脱敏。
//
// 字段说明：
//   - ID：模板唯一标识（SaveOSTemplate 时由 store 分配随机 ID）；
//   - TenantID：所属租户（隔离；空值保存时归一为 default）；
//   - Name：模板展示名（用户输入，如 "centos-7-minimal"）；
//   - OS：OS 类型（centos/ubuntu/debian/...）；
//   - Version：OS 版本（如 "7"、"22.04"）；
//   - Arch：CPU 架构（amd64/arm64）；
//   - InstallURL：安装源 URL（kickstart/preseed 配置文件 URL）；
//   - Config：模板配置 JSON（分区/网络/账户等，敏感字段由 API 层脱敏）；
//   - CreatedAt / UpdatedAt：创建/更新时间戳。
type OSTemplate struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenantId"`
	Name       string    `json:"name"`
	OS         string    `json:"os"`
	Version    string    `json:"version"`
	Arch       string    `json:"arch"`
	InstallURL string    `json:"installUrl"`
	Config     string    `json:"config"` // 模板配置 JSON（敏感，API 层负责脱敏）
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// MiddlewareTemplate 中间件部署模板实体：定义中间件（如 MySQL/Redis/Kafka）
// 的标准化部署配置模板，供「应用编排→中间件实例化」复用。
//
// 字段说明：
//   - ID：模板唯一标识（SaveMiddlewareTemplate 时由 store 分配随机 ID）；
//   - TenantID：所属租户（隔离；空值保存时归一为 default）；
//   - Name：模板展示名（如 "mysql-8.0-single"）；
//   - Type：中间件类型（mysql/redis/kafka/nginx/...）；
//   - Version：中间件版本（如 "8.0.35"）；
//   - Config：部署配置 JSON（端口/内存/副本数/持久化等，敏感字段由 API 层脱敏）；
//   - CreatedAt / UpdatedAt：创建/更新时间戳。
type MiddlewareTemplate struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Version   string    `json:"version"`
	Config    string    `json:"config"` // 部署配置 JSON（敏感，API 层负责脱敏）
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RefreshToken 刷新令牌实体：用于 access token 过期后的无感续期。
//
// 安全设计（与 install_tokens 同范式）：
//   - TokenHash 为 refresh token 的 SHA-256 摘要（hex），库存/内存只存摘要，
//     不存明文 token——DB 只读账号/备份泄露不等于活体 refresh token 泄露；
//   - DeviceFP 为设备指纹（User-Agent + IP 段等），用于校验 refresh token 仅在
//     原签发设备上使用，防 token 跨设备重放；
//   - ExpiresAt 为 refresh token 过期时间（通常远长于 access token，如 7d）；
//   - CreatedAt 为签发时间戳。
//
// 字段说明：
//   - TokenHash：token 的 SHA-256 摘要（主键；JSON 序列化时不输出，防泄露）；
//   - UserID：所属用户 ID（登录态归属）；
//   - TenantID：所属租户（隔离；空值保存时归一为 default）；
//   - DeviceFP：签发设备指纹（防跨设备重放；空串表示不校验设备）；
//   - ExpiresAt：过期时间戳；
//   - CreatedAt：签发时间戳。
type RefreshToken struct {
	TokenHash string    `json:"-"` // SHA-256 摘要（主键；不序列化，防泄露）
	UserID    string    `json:"userId"`
	TenantID  string    `json:"tenantId"`
	DeviceFP  string    `json:"deviceFp"` // 设备指纹（防跨设备重放；空=不校验）
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// SilenceRule 静默/抑制规则实体（M2 集成）。
//
// 在 [StartAt, EndAt] 时间窗口内，对 Labels 匹配 MatchLabels 的告警事件进行抑制。
// MatchLabels 中每个键值对都需在事件 Labels 中存在且相等（AND 语义）；
// 空 MatchLabels 表示匹配该租户下所有事件。
//
// 字段说明：
//   - ID：静默规则唯一标识（CreateSilence 时由 store 分配随机 ID）；
//   - TenantID：所属租户（隔离；空值保存时归一为 default）；
//   - MatchLabels：匹配标签键值对（AND 语义，如 {"severity":"critical","deviceID":"dev-1"}）；
//   - StartAt / EndAt：静默起止时间（零值表示不限）；
//   - CreatedBy：创建人；
//   - Reason：静默原因；
//   - CreatedAt：创建时间戳。
type SilenceRule struct {
	ID          string            `json:"id"`
	TenantID    string            `json:"tenantID"`
	MatchLabels map[string]string `json:"matchLabels"`
	StartAt     time.Time         `json:"startAt"`
	EndAt       time.Time         `json:"endAt"`
	CreatedBy   string            `json:"createdBy"`
	Reason      string            `json:"reason"`
	CreatedAt   time.Time         `json:"createdAt"`
}

// NotifyChannel 通知渠道实体（M2 集成）。
//
// 定义一个通知渠道（钉钉/企业微信/飞书/Slack/邮件/Webhook）的配置，
// 告警规则通过 NotifyChannels 引用渠道 ID 列表，触发时经 Notifier 推送。
//
// 字段说明：
//   - ID：渠道唯一标识（CreateNotifyChannel 时由 store 分配随机 ID）；
//   - TenantID：所属租户（隔离；空值保存时归一为 default）；
//   - Name：渠道展示名（如 "运维钉钉群"）；
//   - Type：渠道类型（dingtalk/wecom/feishu/slack/email/webhook）；
//   - Config：渠道配置 JSON（webhook URL/secret/SMTP 等，敏感字段由 API 层脱敏）；
//   - Enabled：是否启用（false 时跳过推送）；
//   - CreatedAt / UpdatedAt：创建/更新时间戳。
type NotifyChannel struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantID"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Config    string    `json:"config"` // 渠道配置 JSON（敏感，API 层负责脱敏）
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// NotifyTemplate 通知模板实体（M2 集成）。
//
// 定义通知消息的标题/正文模板（Go text/template 变量替换），
// 渠道推送时按模板渲染产出消息正文。
//
// 字段说明：
//   - ID：模板唯一标识（CreateNotifyTemplate 时由 store 分配随机 ID）；
//   - TenantID：所属租户（隔离；空值保存时归一为 default）；
//   - Name：模板展示名；
//   - Type：模板类型（alert/task/device/system）；
//   - Title：模板标题（支持变量替换）；
//   - Body：模板正文（支持变量替换）；
//   - Format：正文格式（markdown/text/html）；
//   - CreatedAt / UpdatedAt：创建/更新时间戳。
type NotifyTemplate struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantID"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Format    string    `json:"format"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
