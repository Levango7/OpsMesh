// aliases.go sqlstore 子包的短名回导层（TD-61 批次 3-sql）。
//
// # 为什么需要
//
// sql*.go 的方法签名与函数体里全是裸短名（*User / *Ticket / QuotaConfig / storekit.MetricsRing …）。
// 类型从方法集里"挪包"后，这些名字在新包里必须依然可解析：本文件以**类型别名**把
// 中性层（internal/store/model）的名字按原名引入 sqlstore 包，搬迁因此是「换包声明 + 加一层别名」，
// 而不是「给几百个标识符加前缀」——后者既易误伤同名的局部变量，也会把 SQL 字符串/注释改坏
// （SQL 语句里的表名/列名与 Go 类型名同名风险尤甚）。
// 别名与原名是同一类型，方法签名与 model 契约逐字一致，编译期由 `var _ Store = (*SQLStore)(nil)`
// （父包 store.go 保留）兜底。
//
// # 组成
//
//   - 47 个领域数据结构（原 models.go，批次 2 下沉 model）；
//   - 3 个随契约下沉的类型（QuotaConfig / Usage / AuditChainVerifyResult，批次 3 上提）；
//   - 租户常量与 NormalizeTenantID 薄包装；
//   - RBAC 权限目录薄包装（rbacPermSpecs / RolePermissions）；
//   - Store：WithDemo 的方法签名 `WithDemo(bool) Store` 引用它——契约在 model 中性层，
//     sqlstore 子包因此可以引用同一具名类型，签名零漂移（契约留在父包则子包反向 import 成环）。
package sqlstore

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

// ---- 常量与归一化 ----

// DefaultTenantID：常量别名（原 models.go:35）。
const DefaultTenantID = model.DefaultTenantID

// 租户状态常量块（原 models.go:663）。
const (
	TenantStatusActive    = model.TenantStatusActive
	TenantStatusSuspended = model.TenantStatusSuspended
	TenantStatusDisabled  = model.TenantStatusDisabled
)

// normalizeTenantID：薄包装（原 models_shim.go；memory 内调用点零改动）。
func normalizeTenantID(tenantID string) string {
	return model.NormalizeTenantID(tenantID)
}

// ---- 契约 ----

// Store 组合接口（model 中性层）：仅 WithDemo 的方法签名引用它。
// 别名而非重定义——与父包 store.Store、契约 model.Store 是同一具名类型。
type Store = model.Store

// ---- RBAC 权限目录（model 中性层，两后端 seedRBAC 共用的单一来源） ----

// rbacPermSpecs 本包短名（= model.PermSpecs 同一切片）。
var rbacPermSpecs = model.PermSpecs

// RolePermissions 返回预置角色名→权限集合映射。
func RolePermissions() map[string][]string { return model.RolePermissions() }
