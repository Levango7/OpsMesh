// aliases.go multischema 子包的短名回导层（TD-61 末批：multi_schema 包装层下沉）。
//
// # 为什么需要
//
// multi_schema*.go 的方法签名与函数体里全是裸短名（*Tenant / *Ticket / *APIKey / proto.DeviceInfo …）。
// 类型从方法集里"挪包"后，这些名字在新包里必须依然可解析：本文件以**类型别名**把
// 中性层（internal/store/model）的名字按原名引入 multischema 包，搬迁因此是「换包声明 + 加一层别名」，
// 而不是「给几百个标识符加前缀」——后者既易误伤同名的局部变量，也会把 SQL 字符串/注释改坏。
// 别名与原名是同一类型，方法签名与 model 契约逐字一致，编译期由父包 store.go 的
// `var _ Store = (*MultiSchemaStore)(nil)` 等 37 条断言兜底。
//
// # 与 sqlstore/memory 两包的差异
//
// 本包不需要 RBAC 权限目录回导（rbacPermSpecs / RolePermissions）与 normalizeTenantID：
// 多 schema 包装层只做路由与委托，不 seed RBAC、不做租户归一化（实测 0 引用）。
package multischema

import (
	"github.com/Levango7/OpsMesh/internal/store/model"
)

// ---- 领域数据结构（47，批次 2 由 models.go 下沉 model） ----

type User = model.User
type Role = model.Role
type Permission = model.Permission
type K8sCluster = model.K8sCluster
type AlertRule = model.AlertRule
type OSTemplate = model.OSTemplate
type MiddlewareTemplate = model.MiddlewareTemplate
type RefreshToken = model.RefreshToken
type SilenceRule = model.SilenceRule
type NotifyChannel = model.NotifyChannel
type NotifyTemplate = model.NotifyTemplate
type ServiceInstance = model.ServiceInstance
type ConfigItem = model.ConfigItem
type SecretItem = model.SecretItem
type SecretMeta = model.SecretMeta
type Ticket = model.Ticket
type TicketFilter = model.TicketFilter
type SLO = model.SLO
type SLI = model.SLI
type SLIStatus = model.SLIStatus
type TrafficPolicy = model.TrafficPolicy
type PipelineTemplate = model.PipelineTemplate
type PipelineParam = model.PipelineParam
type PipelineRun = model.PipelineRun
type ArgoCDApp = model.ArgoCDApp
type ComplianceReport = model.ComplianceReport
type ComplianceResult = model.ComplianceResult
type BackupRecord = model.BackupRecord
type NetworkDevice = model.NetworkDevice
type NetworkMetrics = model.NetworkMetrics
type AutomationRule = model.AutomationRule
type AutomationAction = model.AutomationAction
type AutomationExecution = model.AutomationExecution
type Webhook = model.Webhook
type WebhookDelivery = model.WebhookDelivery
type Script = model.Script
type ScriptExecution = model.ScriptExecution
type TenantStatus = model.TenantStatus
type ResourceUsage = model.ResourceUsage
type TenantQuota = model.TenantQuota
type Tenant = model.Tenant
type APIKey = model.APIKey
type Plugin = model.Plugin
type SubscriptionPlan = model.SubscriptionPlan
type Subscription = model.Subscription
type InvoiceItem = model.InvoiceItem
type Invoice = model.Invoice

// ---- 随契约下沉上提的类型（3，批次 3） ----

type QuotaConfig = model.QuotaConfig
type Usage = model.Usage
type AuditChainVerifyResult = model.AuditChainVerifyResult

// ---- 常量 ----

// DefaultTenantID：常量别名（原 models.go:35）。
const DefaultTenantID = model.DefaultTenantID

// 租户状态常量块（原 models.go:663）。
const (
	TenantStatusActive    = model.TenantStatusActive
	TenantStatusSuspended = model.TenantStatusSuspended
	TenantStatusDisabled  = model.TenantStatusDisabled
)

// ---- 契约 ----

// Store 组合接口（model 中性层）：多 schema 包装层持各租户后端句柄、逐方法委托，
// storeFactory 的签名 `func(schema string) (Store, error)` 引用它。
// 别名而非重定义——与父包 store.Store、契约 model.Store 是同一具名类型。
type Store = model.Store

// ---- 领域小接口（model 中性层） ----
//
// multi_schema.go 文末的编译期断言块直接引用这些名字（`_ DeviceStore = (*MultiSchemaStore)(nil)`
// 等 18 条），与父包 store.go 的全量断言同源；方法签名逐字一致，别名保证「同一类型」判等。
type (
	DeviceStore           = model.DeviceStore
	TaskStore             = model.TaskStore
	AlertStore            = model.AlertStore
	AuditStore            = model.AuditStore
	TokenStore            = model.TokenStore
	LeaderStore           = model.LeaderStore
	UserStore             = model.UserStore
	RoleStore             = model.RoleStore
	PermissionStore       = model.PermissionStore
	K8sClusterStore       = model.K8sClusterStore
	TemplateStore         = model.TemplateStore
	RefreshTokenStore     = model.RefreshTokenStore
	SilenceStore          = model.SilenceStore
	NotifyChannelStore    = model.NotifyChannelStore
	NotifyTemplateStore   = model.NotifyTemplateStore
	AgentLogStore         = model.AgentLogStore
	QuotaStore            = model.QuotaStore
	ServiceDiscoveryStore = model.ServiceDiscoveryStore
	ConfigStore           = model.ConfigStore
	SecretStore           = model.SecretStore
	TicketStore           = model.TicketStore
	TrafficStore          = model.TrafficStore
	PipelineStore         = model.PipelineStore
	ArgoCDStore           = model.ArgoCDStore
	ComplianceStore       = model.ComplianceStore
	BackupStore           = model.BackupStore
	NetworkStore          = model.NetworkStore
	AutomationStore       = model.AutomationStore
	SLOStore              = model.SLOStore
	WebhookStore          = model.WebhookStore
	ScriptStore           = model.ScriptStore
	TenantStore           = model.TenantStore
	APIKeyStore           = model.APIKeyStore
	PluginStore           = model.PluginStore
	BillingStore          = model.BillingStore
)
