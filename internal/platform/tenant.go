
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
	"errors"
	"fmt"
	"strings"
	"time"

	"opsmesh/internal/store"
)

// ============================================================================
// 租户管理引擎
// ============================================================================

// TenantStatus 租户状态类型。
// active：正常使用；suspended：暂停（超额/违规，可恢复）；disabled：停用（不可恢复）。
type TenantStatus string

const (
	TenantStatusActive   TenantStatus = "active"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusDisabled TenantStatus = "disabled"
)

// ResourceUsage 租户资源用量（实时统计）。
// 由 BillingManager.CalculateUsage 周期填充，用于配额校验与计费。
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

// Tenant 租户实体。平台化多租户隔离的核心载体。
// 由 TenantManager.ValidateTenant 校验合法性；CheckQuota 校验资源配额。
type Tenant struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`        // 租户标识（唯一，URL-safe）
	DisplayName string       `json:"displayName"` // 显示名称（人类可读）
	Status      TenantStatus `json:"status"`      // active|suspended|disabled
	Quota       TenantQuota  `json:"quota"`       // 资源配额
	Usage       ResourceUsage `json:"usage"`      // 当前用量（实时统计）
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

// TenantManager 租户管理引擎。
// 封装租户校验/配额检查等业务规则，依赖 TenantStore 做持久化。
type TenantManager struct {
	store store.TenantStore
}

// NewTenantManager 构造租户管理引擎。
func NewTenantManager(s store.TenantStore) *TenantManager {
	return &TenantManager{store: s}
}

// ValidateTenant 校验租户合法性。
// 校验规则：
//   - ID/Name 非空；
//   - Status 为合法枚举值；
//   - DisplayName 非空（默认回退为 Name）。
// 不校验配额（配额由 CheckQuota 单独校验，便于按需调用）。
func (m *TenantManager) ValidateTenant(tenant *store.Tenant) error {
	if tenant == nil {
		return errors.New("tenant is nil")
	}
	if tenant.ID == "" {
		return errors.New("tenant id is required")
	}
	if tenant.Name == "" {
		return errors.New("tenant name is required")
	}
	if tenant.DisplayName == "" {
		tenant.DisplayName = tenant.Name
	}
	switch TenantStatus(tenant.Status) {
	case TenantStatusActive, TenantStatusSuspended, TenantStatusDisabled:
		// 合法状态。
	default:
		return fmt.Errorf("invalid tenant status: %q (want active|suspended|disabled)", tenant.Status)
	}
	return nil
}

// CheckQuota 校验租户资源配额。
// resourceType 为资源类型（devices/tasks/activeTasks/alerts/agents/webhooks/apiKeys）；
// current 为当前用量；超额返回 ErrQuotaExceeded，否则返回 nil。
// 配额为 0 表示不限制（无限配额），直接放行。
func (m *TenantManager) CheckQuota(tenantID string, resourceType string, current int) error {
	if tenantID == "" {
		return errors.New("tenant id is required")
	}
	tenant, ok := m.store.GetTenant(tenantID)
	if !ok || tenant == nil {
		return fmt.Errorf("tenant %q not found", tenantID)
	}
	// 停用/暂停租户拒绝一切新增资源。
	if TenantStatus(tenant.Status) != TenantStatusActive {
		return fmt.Errorf("tenant %q is %s, resource creation not allowed", tenantID, tenant.Status)
	}
	var limit int
	switch strings.ToLower(resourceType) {
	case "devices":
		limit = tenant.Quota.MaxDevices
	case "tasks":
		limit = tenant.Quota.MaxTasks
	case "activetasks":
		limit = tenant.Quota.MaxActiveTasks
	case "alerts":
		limit = tenant.Quota.MaxAlerts
	case "agents":
		limit = tenant.Quota.MaxAgents
	case "webhooks":
		limit = tenant.Quota.MaxWebhooks
	case "apikeys":
		limit = tenant.Quota.MaxAPIKeys
	default:
		return fmt.Errorf("unknown resource type: %q", resourceType)
	}
	// 0 = 不限制。
	if limit == 0 {
		return nil
	}
	if current >= limit {
		return fmt.Errorf("quota exceeded for %s: current=%d, limit=%d", resourceType, current, limit)
	}
	return nil
}