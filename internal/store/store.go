// Package store 定义可插拔的持久化抽象 Store 及两种实现：
//   - MemoryStore：内存实现（默认后端，无需任何外部依赖即可运行）；
//   - SQLStore：MySQL + Redis 实现（数据本地化，私有部署）。
//
// 控制面通过 Store 接口与具体后端解耦；Registry 仅做薄转发。
//
// TD-61（后端拆包）后本包的分层：
//   - 契约与领域类型在中性层 internal/store/model（本文件仅以类型别名回导）；
//   - 内存后端在 internal/store/memory，SQL 后端在 internal/store/sqlstore，
//     多租户 schema 隔离包装层在 internal/store/multischema（末批下沉）；
//   - 共享内核（token 签名/随机串/bcrypt/指标环形缓冲与内存上限）在 internal/store/storekit；
//   - 失败可观测性在 internal/store/storefail。
//
// 本文件保留全部编译期断言（引用具体后端类型，必须对本包可见）。
package store

import "github.com/Levango7/OpsMesh/internal/store/model"

// 契约别名回导：model 中性层的契约以降级别名暴露给外部 import 方与后端实现。
//
// 为什么是别名而不是重定义：类型别名与原名是同一类型，Store 组合接口的
// `WithDemo(bool) Store` 签名因此与子包实现（memory.MemoryStore.WithDemo）逐字一致，
// 编译期断言 `var _ Store = (*MemoryStore)(nil)` 与外部 129 个 import 方全部零改动。
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
	Store                 = model.Store
)

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
	_ TenantStore           = (*MemoryStore)(nil)
	_ APIKeyStore           = (*MemoryStore)(nil)
	_ PluginStore           = (*MemoryStore)(nil)
	_ BillingStore          = (*MemoryStore)(nil)
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
	_ TenantStore           = (*SQLStore)(nil)
	_ APIKeyStore           = (*SQLStore)(nil)
	_ PluginStore           = (*SQLStore)(nil)
	_ BillingStore          = (*SQLStore)(nil)
	_ Store                 = (*SQLStore)(nil)

	// MultiSchemaStore 全量断言：确保多租户 schema 隔离实现满足各领域小接口。
	// 任一方法缺失会在编译期立刻暴露（M6：补齐第三组断言，与 MemoryStore/SQLStore 对齐）。
	_ DeviceStore           = (*MultiSchemaStore)(nil)
	_ TaskStore             = (*MultiSchemaStore)(nil)
	_ AlertStore            = (*MultiSchemaStore)(nil)
	_ AuditStore            = (*MultiSchemaStore)(nil)
	_ TokenStore            = (*MultiSchemaStore)(nil)
	_ LeaderStore           = (*MultiSchemaStore)(nil)
	_ UserStore             = (*MultiSchemaStore)(nil)
	_ RoleStore             = (*MultiSchemaStore)(nil)
	_ PermissionStore       = (*MultiSchemaStore)(nil)
	_ K8sClusterStore       = (*MultiSchemaStore)(nil)
	_ TemplateStore         = (*MultiSchemaStore)(nil)
	_ RefreshTokenStore     = (*MultiSchemaStore)(nil)
	_ SilenceStore          = (*MultiSchemaStore)(nil)
	_ NotifyChannelStore    = (*MultiSchemaStore)(nil)
	_ NotifyTemplateStore   = (*MultiSchemaStore)(nil)
	_ AgentLogStore         = (*MultiSchemaStore)(nil)
	_ QuotaStore            = (*MultiSchemaStore)(nil)
	_ ServiceDiscoveryStore = (*MultiSchemaStore)(nil)
	_ ConfigStore           = (*MultiSchemaStore)(nil)
	_ SecretStore           = (*MultiSchemaStore)(nil)
	_ TicketStore           = (*MultiSchemaStore)(nil)
	_ SLOStore              = (*MultiSchemaStore)(nil)
	_ TrafficStore          = (*MultiSchemaStore)(nil)
	_ PipelineStore         = (*MultiSchemaStore)(nil)
	_ ArgoCDStore           = (*MultiSchemaStore)(nil)
	_ ComplianceStore       = (*MultiSchemaStore)(nil)
	_ BackupStore           = (*MultiSchemaStore)(nil)
	_ NetworkStore          = (*MultiSchemaStore)(nil)
	_ AutomationStore       = (*MultiSchemaStore)(nil)
	_ WebhookStore          = (*MultiSchemaStore)(nil)
	_ ScriptStore           = (*MultiSchemaStore)(nil)
	_ TenantStore           = (*MultiSchemaStore)(nil)
	_ APIKeyStore           = (*MultiSchemaStore)(nil)
	_ PluginStore           = (*MultiSchemaStore)(nil)
	_ BillingStore          = (*MultiSchemaStore)(nil)
	_ Store                 = (*MultiSchemaStore)(nil)
)
