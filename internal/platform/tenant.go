// Package platform 实现平台化业务引擎：租户管理 / API Key / 插件市场 / 计费。
//
// 与 controlplane（HTTP handler）和 store（持久化）解耦：
//   - platform 包封装业务规则（校验/配额/计费/插件安装）；
//   - controlplane 包负责 HTTP 协议层（路由/鉴权/JSON）；
//   - store 包负责持久化（CRUD）。
//
// 设计要点：
//   - 引擎不直接持有 HTTP 上下文，便于被 CLI/gRPC 等多入口复用；
//   - 引擎对 store 仅依赖最小子接口（TenantStore/APIKeyStore/...），降低耦合；
//   - 校验逻辑集中在引擎层，handler 仅做协议层错误转换。
package platform

import (
	"time"
)

// ============================================================================
// 租户管理数据模型
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
// 由控制面 handler 周期填充（调用 store.TenantStore），用于配额校验与计费展示。
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
// 0 表示不限制（无限配额）；由 controlplane handler 调用 store.TenantStore 时校验。
type TenantQuota struct {
	MaxDevices     int `json:"maxDevices"`
	MaxTasks       int `json:"maxTasks"`
	MaxActiveTasks int `json:"maxActiveTasks"`
	MaxAlerts      int `json:"maxAlerts"`
	MaxAgents      int `json:"maxAgents"`
	MaxWebhooks    int `json:"maxWebhooks"`
	MaxAPIKeys     int `json:"maxAPIKeys"`
}

// Tenant 租户实体。平台化多租户隔离的核心载体。
// 校验/配额检查由 controlplane handler 直接调用 store.TenantStore 完成。
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

// 历史：原 TenantManager 租户管理引擎（ValidateTenant/CheckQuota）已作为 H7 平台
// 死代码清理删除——controlplane tenant handler 直接调用 store.TenantStore 接口，
// 校验逻辑在 handler 侧就近实现，不经 platform 层冗余封装。

