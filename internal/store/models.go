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

// ============================================================================
// P0.3 Store 接口扩展：服务发现 / 配置中心 / 密钥管理 领域数据模型。
// 与现有 12 领域解耦，通过 ServiceDiscoveryStore / ConfigStore / SecretStore
// 三个小接口暴露，组合进 Store（向后兼容，不破坏现有接口）。
// ============================================================================

// ServiceInstance 服务发现中的服务实例。
// 一个服务可注册多个实例（按 ServiceID 唯一），按 ServiceName 聚合查询。
// 心跳驱动健康状态：LastHeartbeat 超过阈值由 StaleServices 标记下线。
type ServiceInstance struct {
	ServiceID     string            `json:"serviceID"`     // 实例唯一标识（服务名+地址+端口或 UUID）
	ServiceName   string            `json:"serviceName"`   // 服务逻辑名（按此聚合）
	Address       string            `json:"address"`       // 实例地址（IP 或主机名）
	Port          int               `json:"port"`          // 实例端口
	Metadata      map[string]string `json:"metadata"`      // 实例元数据（权重/标签/region 等）
	Status        string            `json:"status"`        // 健康状态：healthy/unhealthy/unknown
	LastHeartbeat time.Time         `json:"lastHeartbeat"` // 最近一次心跳时间
	TenantID      string            `json:"tenantID"`      // 所属租户（隔离）
	CreatedAt     time.Time         `json:"createdAt"`     // 注册时间
}

// ConfigItem 配置中心的配置项。
// 按 (TenantID, Key) 唯一；Version 单调递增，每次 SetConfig 产生新版本并写入历史。
// Format 支持 json/yaml/toml/properties/text；Value 为配置原文（不脱敏）。
type ConfigItem struct {
	Key         string    `json:"key"`         // 配置键（按 / 分隔的命名空间路径，如 app/db/pool）
	Value       string    `json:"value"`       // 配置值原文
	Format      string    `json:"format"`      // 配置格式：json/yaml/toml/properties/text
	Version     int       `json:"version"`     // 版本号（从 1 单调递增）
	Description string    `json:"description"` // 配置说明
	TenantID    string    `json:"tenantID"`    // 所属租户（隔离）
	UpdatedBy   string    `json:"updatedBy"`   // 最后更新人（用户 ID）
	UpdatedAt   time.Time `json:"updatedAt"`   // 最后更新时间
}

// SecretItem 密钥项（含明文值，仅在 SetSecret/GetSecret/RotateSecret 内部流转）。
// API 层对外暴露时须转为 SecretMeta（脱去 Value）。KeyType 支持 aes/hmac/rsa/ecdsa/passphrase。
type SecretItem struct {
	Key     string `json:"key"`     // 密钥逻辑名（按 / 分隔路径，如 app/db/password）
	Value   string `json:"value"`   // 密钥明文值（仅在内部流转；API 须脱敏）
	KeyType string `json:"keyType"` // 密钥类型：aes/hmac/rsa/ecdsa/passphrase
}

// SecretMeta 密钥元信息（脱敏视图，对外暴露）。
// 不含 Value；按 (TenantID, Key, Version) 唯一标识一个密钥版本。
type SecretMeta struct {
	Key       string    `json:"key"`       // 密钥逻辑名
	KeyType   string    `json:"keyType"`   // 密钥类型
	Version   int       `json:"version"`   // 版本号（从 1 单调递增；轮换产生新版本）
	TenantID  string    `json:"tenantID"`  // 所属租户（隔离）
	CreatedAt time.Time `json:"createdAt"` // 创建时间
	UpdatedAt time.Time `json:"updatedAt"` // 最近一次轮换时间
}

// ============================================================================
// Phase 1 服务台与工单管理 / SLO 管理领域数据模型。
// 与现有领域解耦，通过 TicketStore / SLOStore 两个小接口暴露，
// 组合进 Store（向后兼容，不破坏现有接口）。
// ============================================================================

// Ticket 工单实体（Phase 1 服务台与工单管理）。
type Ticket struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenantID"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Status        string     `json:"status"`   // "open" | "in_progress" | "resolved" | "closed"
	Priority      string     `json:"priority"` // "low" | "medium" | "high" | "urgent"
	Category      string     `json:"category"` // "incident" | "change" | "request" | "problem"
	AssigneeID    string     `json:"assigneeID"`
	CreatorID     string     `json:"creatorID"`
	RelatedDevice string     `json:"relatedDevice"`
	RelatedTask   string     `json:"relatedTask"`
	Tags          []string   `json:"tags"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	ResolvedAt    *time.Time `json:"resolvedAt,omitempty"`
}

// TicketFilter 工单查询过滤条件。
type TicketFilter struct {
	Status     string
	Priority   string
	Category   string
	AssigneeID string
}

// SLO 服务级别目标实体（Phase 1 SLO 管理）。
type SLO struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenantID"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	ServiceName string    `json:"serviceName"`
	Target      float64   `json:"target"` // 如 99.9 表示 99.9%
	Window      string    `json:"window"` // 如 "30d", "7d"
	SLIs        []SLI     `json:"slis"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// SLI 服务级别指标。
type SLI struct {
	Name     string  `json:"name"`     // 如 "availability", "latency_p99"
	Metric   string  `json:"metric"`   // Prometheus metric 表达式
	Target   float64 `json:"target"`   // 目标值
	Operator string  `json:"operator"` // ">=", "<=", ">", "<"
}

// SLIStatus SLI 当前状态。
type SLIStatus struct {
	SLIName       string    `json:"sliName"`
	CurrentValue  float64   `json:"currentValue"`
	TargetValue   float64   `json:"targetValue"`
	Status        string    `json:"status"` // "met" | "breached" | "nodata"
	LastEvaluated time.Time `json:"lastEvaluated"`
}

// ============================================================================
// Phase 2 微服务治理 + CI/CD 领域数据模型。
// 与现有领域解耦，通过 TrafficStore / PipelineStore / ArgoCDStore
// 三个小接口暴露，组合进 Store。
// ============================================================================

// TrafficPolicy 流量治理策略（Phase 2 微服务治理）。
type TrafficPolicy struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenantID"`
	Name          string         `json:"name"`
	ServiceName   string         `json:"serviceName"`
	Type          string         `json:"type"`          // "canary","timeout","retry","circuit_breaker","mirror"
	CanaryWeights map[string]int `json:"canaryWeights"` // version -> weight%
	MirrorPercent int            `json:"mirrorPercent"` // 镜像流量百分比
	Timeout       string         `json:"timeout"`       // "5s"
	Retries       int            `json:"retries"`
	RetryTimeout  string         `json:"retryTimeout"`
	MaxConns      int            `json:"maxConns"` // circuit_breaker
	MaxRequests   int            `json:"maxRequests"`
	Status        string         `json:"status"` // "active","inactive"
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

// PipelineTemplate CI/CD 流水线模板（Phase 2 CI/CD 流水线）。
type PipelineTemplate struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenantID"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"` // "tekton","jenkins"
	YAML        string `json:"yaml"` // pipeline 定义
	// AgentID 执行该流水线的默认 agent（触发 run 时派生执行任务写入 Task.AgentID）。
	// 空=未指定（run 推进时任务无法下发，run 置 failed 并记录日志）。
	AgentID    string          `json:"agentID"`
	Parameters []PipelineParam `json:"parameters"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// PipelineParam 流水线参数。
type PipelineParam struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     string `json:"default"`
	Required    bool   `json:"required"`
}

// PipelineRun 流水线执行记录。
type PipelineRun struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenantID"`
	TemplateID   string            `json:"templateID"`
	TemplateName string            `json:"templateName"`
	Status       string            `json:"status"` // "pending","running","succeeded","failed","cancelled"
	Parameters   map[string]string `json:"parameters"`
	Logs         string            `json:"logs"`
	StartedAt    *time.Time        `json:"startedAt,omitempty"`
	FinishedAt   *time.Time        `json:"finishedAt,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
}

// ArgoCDApp ArgoCD 应用定义（Phase 2 CI/CD 流水线）。
type ArgoCDApp struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenantID"`
	Name           string    `json:"name"`
	Namespace      string    `json:"namespace"`
	RepoURL        string    `json:"repoURL"`
	Path           string    `json:"path"`
	TargetRevision string    `json:"targetRevision"` // "main","HEAD"
	ClusterURL     string    `json:"clusterURL"`     // K8s 集群 API 地址
	SyncPolicy     string    `json:"syncPolicy"`     // "manual","auto"
	Status         string    `json:"status"`         // "synced","outofsync","unknown"
	HealthStatus   string    `json:"healthStatus"`   // "healthy","degraded","missing","unknown"
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// ComplianceReport 合规检查报告（Phase 3 安全合规）。
//
// 由合规引擎扫描设备产出，按 (TenantID, ID) 唯一标识，按 TenantID 隔离。
// Results 为各规则检查结果；Score 为汇总分数 0-100。
type ComplianceReport struct {
	ID        string             `json:"id"`
	TenantID  string             `json:"tenantID"`
	DeviceID  string             `json:"deviceID"`
	Results   []ComplianceResult `json:"results"`
	Score     int                `json:"score"`
	CreatedAt time.Time          `json:"createdAt"`
}

// ComplianceResult 合规检查单条结果（Phase 3 安全合规）。
type ComplianceResult struct {
	RuleID    string    `json:"ruleId"`
	Passed    bool      `json:"passed"`
	Output    string    `json:"output"`
	CheckedAt time.Time `json:"checkedAt"`
}

// BackupRecord 灾备备份记录（Phase 3 高可用）。
//
// 由灾备恢复 API 创建，按 (TenantID, ID) 唯一标识，按 TenantID 隔离。
// Type 为备份类型（full/config/devices/tasks）；Status 为状态（creating/completed/failed）。
type BackupRecord struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantID"`
	Type      string    `json:"type"` // "full", "config", "devices", "tasks"
	Status    string    `json:"status"`
	Size      int64     `json:"size"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"createdAt"`
}

// ============================================================================
// Phase 4 网络管理 + 自动化闭环
// ============================================================================

// NetworkDevice 网络设备实体（Phase 4 网络管理）。
//
// 与 DeviceStore.DeviceInfo（纳管主机/agent）解耦——NetworkDevice 是网络拓扑中的
// 网络设备（switch/router/firewall/load_balancer），通过 SNMP/CLI 管理而非 agent。
// 按 (TenantID, ID) 唯一标识，按 TenantID 隔离。
type NetworkDevice struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenantID"`
	Name          string    `json:"name"`
	Type          string    `json:"type"` // switch/router/firewall/load_balancer
	Vendor        string    `json:"vendor"`
	Model         string    `json:"model"`
	IP            string    `json:"ip"`
	Mask          string    `json:"mask"`
	Mac           string    `json:"mac"`
	Location      string    `json:"location"`
	SnmpCommunity string    `json:"snmpCommunity"`
	Status        string    `json:"status"` // up/down/unknown/maintain
	Config        string    `json:"config,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// NetworkMetrics 网络设备监控指标（Phase 4 网络管理）。
type NetworkMetrics struct {
	DeviceID    string    `json:"deviceID"`
	TenantID    string    `json:"tenantID"`
	Timestamp   time.Time `json:"timestamp"`
	CPUUsage    float64   `json:"cpuUsage"`
	MemoryUsage float64   `json:"memoryUsage"`
	Temperature float64   `json:"temperature"`
	Uptime      int64     `json:"uptime"`
}

// AutomationRule 自动化规则实体（Phase 4 自动化闭环）。
//
// 触发器（Trigger）+ 动作列表（Actions）组成"条件→动作"闭环。
// TriggerType: alert/metric_threshold/schedule/event。
// ActionType: execute_task/send_notify/scale/restart/isolate。
// 按 (TenantID, ID) 唯一标识，按 TenantID 隔离。
type AutomationRule struct {
	ID            string             `json:"id"`
	TenantID      string             `json:"tenantID"`
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	TriggerType   string             `json:"triggerType"`
	TriggerParams map[string]string  `json:"triggerParams"`
	Actions       []AutomationAction `json:"actions"`
	Enabled       bool               `json:"enabled"`
	CreatedAt     time.Time          `json:"createdAt"`
	UpdatedAt     time.Time          `json:"updatedAt"`
}

// AutomationAction 自动化动作。
type AutomationAction struct {
	Type   string            `json:"type"` // execute_task/send_notify/scale/restart/isolate
	Params map[string]string `json:"params"`
}

// AutomationExecution 自动化规则执行记录（Phase 4 自动化闭环）。
type AutomationExecution struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenantID"`
	RuleID    string     `json:"ruleID"`
	RuleName  string     `json:"ruleName"`
	Status    string     `json:"status"` // pending/running/succeeded/failed/skipped
	Detail    string     `json:"detail"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
}

// ============================================================================
// Phase 5 扩展能力：Webhook + 自定义脚本
// ============================================================================

// Webhook Webhook 配置实体（Phase 5 扩展能力）。
//
// 用于事件驱动集成：当指定 Events（如 alert.created/ticket.closed）发生时，
// 推送 HTTP POST 到 URL，BodyTemplate 渲染为请求体；Headers 附加自定义请求头。
// RetryCount/RetryIntervalSec 控制失败重试；Enabled 控制是否生效。
// 按 (TenantID, ID) 唯一标识，按 TenantID 隔离。
type Webhook struct {
	ID               string            `json:"id"`
	TenantID         string            `json:"tenantID"`
	Name             string            `json:"name"`
	URL              string            `json:"url"`
	Events           []string          `json:"events"`
	Headers          map[string]string `json:"headers"`
	BodyTemplate     string            `json:"bodyTemplate"`
	Enabled          bool              `json:"enabled"`
	RetryCount       int               `json:"retryCount"`
	RetryIntervalSec int               `json:"retryIntervalSec"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
}

// WebhookDelivery Webhook 投递记录（Phase 5 扩展能力）。
//
// 每次推送（含重试）产生一条投递记录，记录响应状态码/响应体/错误信息/投递时间。
// 按 (TenantID, ID) 唯一标识；WebhookID 关联到 Webhook.ID。
type WebhookDelivery struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenantID"`
	WebhookID   string    `json:"webhookID"`
	Event       string    `json:"event"`
	Payload     string    `json:"payload"`
	StatusCode  int       `json:"statusCode"`
	Response    string    `json:"response"`
	Error       string    `json:"error"`
	DeliveredAt time.Time `json:"deliveredAt"`
}

// Script 自定义脚本实体（Phase 5 扩展能力）。
//
// 支持 shell/python 两种语言；Content 为脚本内容；Params 为参数模板；
// TimeoutSec 控制执行超时；Enabled 控制是否允许执行。
// 按 (TenantID, ID) 唯一标识，按 TenantID 隔离。
type Script struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenantID"`
	Name       string    `json:"name"`
	Language   string    `json:"language"` // shell|python
	Content    string    `json:"content"`
	Params     string    `json:"params"`
	TimeoutSec int       `json:"timeoutSec"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// ScriptExecution 脚本执行记录（Phase 5 扩展能力）。
//
// 每次执行产生一条记录，记录标准输出/标准错误/状态/起止时间。
// 按 (TenantID, ID) 唯一标识；ScriptID 关联到 Script.ID；DeviceID 为执行设备。
type ScriptExecution struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenantID"`
	ScriptID   string     `json:"scriptID"`
	DeviceID   string     `json:"deviceID"`
	Status     string     `json:"status"` // pending/running/succeeded/failed
	Stdout     string     `json:"stdout"`
	Stderr     string     `json:"stderr"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// ============================================================================
// Phase 6 平台化：租户 / API Key / 插件市场 / 计费
// ============================================================================

// TenantStatus 租户状态类型。
// active：正常使用；suspended：暂停（超额/违规，可恢复）；disabled：停用（不可恢复）。
type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusDisabled  TenantStatus = "disabled"
)

// ResourceUsage 租户资源用量（实时统计）。
// 由控制面周期填充，用于配额校验与计费。
type ResourceUsage struct {
	Devices     int `json:"devices"`     // 已纳管设备数
	Tasks       int `json:"tasks"`       // 历史任务总数
	ActiveTasks int `json:"activeTasks"` // 当前活跃任务数（pending+running）
	Alerts      int `json:"alerts"`      // 活跃告警数
	Agents      int `json:"agents"`      // 已注册 agent 数
	Webhooks    int `json:"webhooks"`    // Webhook 数
	APIKeys     int `json:"apiKeys"`     // API Key 数
}

// TenantQuota 租户资源配额。
// 0 表示不限制（无限配额）；由 TenantManager.CheckQuota 校验。
type TenantQuota struct {
	MaxDevices     int `json:"maxDevices"`
	MaxTasks       int `json:"maxTasks"`
	MaxActiveTasks int `json:"maxActiveTasks"`
	MaxAlerts      int `json:"maxAlerts"`
	MaxAgents      int `json:"maxAgents"`
	MaxWebhooks    int `json:"maxWebhooks"`
	MaxAPIKeys     int `json:"maxAPIKeys"`
}

// Tenant 租户实体（Phase 6 平台化多租户隔离的核心载体）。
//
// 平台化支持多租户隔离：每个租户有独立的资源配额、API Key、订阅计划。
// 由 TenantManager.ValidateTenant 校验合法性；CheckQuota 校验资源配额。
type Tenant struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`        // 租户标识（唯一，URL-safe）
	DisplayName string        `json:"displayName"` // 显示名称（人类可读）
	Status      TenantStatus  `json:"status"`      // active|suspended|disabled
	Quota       TenantQuota   `json:"quota"`       // 资源配额
	Usage       ResourceUsage `json:"usage"`       // 当前用量（实时统计）
	CreatedAt   time.Time     `json:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
}

// APIKey API Key 实体（Phase 6 程序化访问控制）。
//
// 用于 CI/CD 系统、自动化脚本、第三方集成等场景替代用户名/密码登录。
// Key 字段存储 SHA-256 hash（明文仅在创建时返回一次）。
type APIKey struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenantID"`
	Name            string    `json:"name"`            // 人类可读名称
	Key             string    `json:"-"`               // SHA-256 hash；JSON 序列化时不输出（防泄露）
	Scopes          []string  `json:"scopes"`          // 权限范围（如 ["device:read","task:write"]）
	RateLimitPerSec int       `json:"rateLimitPerSec"` // 每秒限流（0=不限）
	ExpiresAt       time.Time `json:"expiresAt"`       // 过期时间（零值=永不过期）
	LastUsedAt      time.Time `json:"lastUsedAt"`      // 最后使用时间
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"createdAt"`
}

// Plugin 插件实体（Phase 6 插件市场）。
//
// 支持三种类型：agent（扩展 agent 端）、controlplane（扩展控制面 API）、ui（扩展前端）。
// DownloadURL 外链插件二进制/源码；Checksum（SHA-256）用于下载完整性校验。
type Plugin struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	Author      string    `json:"author"`
	Type        string    `json:"type"` // agent|controlplane|ui
	DownloadURL string    `json:"downloadURL"`
	Checksum    string    `json:"checksum"` // SHA-256
	Installed   bool      `json:"installed"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
}

// SubscriptionPlan 订阅计划实体（Phase 6 计费）。
//
// 定义价格/周期/功能/资源限额。Interval 为 monthly|yearly。
// Price 单位为分（int），避免浮点精度问题。
type SubscriptionPlan struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	Price          int         `json:"price"`          // 单位：分
	Interval       string      `json:"interval"`       // monthly|yearly
	Features       []string    `json:"features"`       // 功能列表
	ResourceLimits TenantQuota `json:"resourceLimits"` // 资源限额
	CreatedAt      time.Time   `json:"createdAt"`
}

// Subscription 订阅实体（Phase 6 计费）。
//
// 关联租户与计划，管理订阅生命周期。
// Status: active|canceled|expired。
type Subscription struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantID"`
	PlanID    string    `json:"planID"`
	Status    string    `json:"status"` // active|canceled|expired
	StartedAt time.Time `json:"startedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// InvoiceItem 账单明细项。
type InvoiceItem struct {
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	UnitPrice int    `json:"unitPrice"` // 单位：分
	Amount    int    `json:"amount"`    // 单位：分
}

// Invoice 账单实体（Phase 6 计费）。
//
// 按订阅周期生成，记录消费明细。Status: pending|paid|overdue。
type Invoice struct {
	ID             string        `json:"id"`
	TenantID       string        `json:"tenantID"`
	SubscriptionID string        `json:"subscriptionID"`
	Amount         int           `json:"amount"` // 单位：分
	PeriodStart    time.Time     `json:"periodStart"`
	PeriodEnd      time.Time     `json:"periodEnd"`
	Status         string        `json:"status"` // pending|paid|overdue
	Items          []InvoiceItem `json:"items"`
	CreatedAt      time.Time     `json:"createdAt"`
}
