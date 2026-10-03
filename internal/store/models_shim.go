// models_shim.go —— 领域模型的父包回导层（TD-61 批次 2，2026-10-04）。
//
// models.go（47 个领域数据结构 + 归一化函数）下沉至 internal/store/model：
// 它是 memory_*/sql_*/multi_schema_* 共用的中性层，下沉后「后端拆包」才有
// 可 import 的共同依赖（否则子包反向依赖主包成环）。
//
// 手法 = 批次 1（storefail）同款：**类型别名 + 常量别名 + 薄包装**。
// 47 个类型以别名回导，store 包内全部引用点（含 58 个后端文件的未限定标识符）
// **零改动**；待后端真正拆包时，子包再直接 import internal/store/model。
package store

import "github.com/Levango7/OpsMesh/internal/store/model"

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

// DefaultTenantID：常量别名（原 models.go:35）。
const DefaultTenantID = model.DefaultTenantID

// 租户状态常量块（原 models.go:663；外部 21 处引用经此别名零改动）。
const (
	TenantStatusActive    = model.TenantStatusActive
	TenantStatusSuspended = model.TenantStatusSuspended
	TenantStatusDisabled  = model.TenantStatusDisabled
)

// normalizeTenantID：薄包装（store 内 5 个调用点零改动）。
func normalizeTenantID(tenantID string) string {
	return model.NormalizeTenantID(tenantID)
}
