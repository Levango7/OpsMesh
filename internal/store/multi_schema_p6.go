// multi_schema_p6.go MultiSchemaStore 对 Phase 6 四个新接口（TenantStore / APIKeyStore /
// PluginStore / BillingStore）的委托实现。
//
// 设计要点（与 multi_schema_p5.go 风格一致）：
//   - 带 tenantID 参数的方法用 storeFor(tenantID) 路由；
//   - 不带 tenantID 参数的方法（如 ListTenants/ListPlugins/ListBillingPlans）遍历全部租户 store 聚合；
//   - 路由失败返回零值（nil/false），与现有方法风格一致。
package store

import "strings"

// ============================================================================
// TenantStore 实现（5 方法）
// ============================================================================

// CreateTenant 创建租户：用 tenant.ID 路由。
// M4：ID 为空时先预生成随机 ID 再按新 ID 路由——原实现把空 ID 归一为 default，
// 会把新租户的数据错落进 default schema。路由层负责预生成后，底层
// MemoryStore.CreateTenant 对非空 ID 不再改写，两侧职责无重叠；
// 返回值携带分配后的 ID，调用方无感知（与 Memory 后端行为对齐）。
// 注意：randTenantID 生成 "tenant-" + hex（含连字符），但 DefaultSchemaNamer 的
// validateIdent 只允许 [a-zA-Z0-9_]，故把 "-" 替换为 "_" 以通过 schema 名校验。
func (m *MultiSchemaStore) CreateTenant(tenant *Tenant) *Tenant {
	if tenant == nil {
		return nil
	}
	if tenant.ID == "" {
		tenant.ID = strings.ReplaceAll(randTenantID(), "-", "_")
	}
	s, err := m.storeFor(tenant.ID)
	if err != nil {
		return nil
	}
	return s.CreateTenant(tenant)
}

// GetTenant 按 ID 返回单个租户：用 ID 路由。
func (m *MultiSchemaStore) GetTenant(id string) (*Tenant, bool) {
	s, err := m.storeFor(id)
	if err != nil {
		return nil, false
	}
	return s.GetTenant(id)
}

// UpdateTenant 更新租户：用 tenant.ID 路由。
func (m *MultiSchemaStore) UpdateTenant(tenant *Tenant) (*Tenant, bool) {
	if tenant == nil {
		return nil, false
	}
	s, err := m.storeFor(tenant.ID)
	if err != nil {
		return nil, false
	}
	return s.UpdateTenant(tenant)
}

// ListTenants 返回全部租户：遍历全部租户 store 聚合。
func (m *MultiSchemaStore) ListTenants() []*Tenant {
	out := make([]*Tenant, 0)
	for _, s := range m.allStores() {
		out = append(out, s.ListTenants()...)
	}
	return out
}

// DeleteTenant 按 ID 删除租户：用 ID 路由。
func (m *MultiSchemaStore) DeleteTenant(id string) bool {
	s, err := m.storeFor(id)
	if err != nil {
		return false
	}
	return s.DeleteTenant(id)
}

// ============================================================================
// APIKeyStore 实现（5 方法）
// ============================================================================

// CreateAPIKey 创建 API Key：用 tenantID 路由。
func (m *MultiSchemaStore) CreateAPIKey(tenantID string, key *APIKey) *APIKey {
	if key == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateAPIKey(tenantID, key)
}

// GetAPIKey 按 (tenantID, id) 返回单个 API Key：用 tenantID 路由。
func (m *MultiSchemaStore) GetAPIKey(tenantID, id string) (*APIKey, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetAPIKey(tenantID, id)
}

// UpdateAPIKey 更新 API Key：用 tenantID 路由。
func (m *MultiSchemaStore) UpdateAPIKey(tenantID string, key *APIKey) (*APIKey, bool) {
	if key == nil {
		return nil, false
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.UpdateAPIKey(tenantID, key)
}

// ListAPIKeys 返回指定租户的全部 API Key：用 tenantID 路由。
// tenantID 为空串时遍历全部租户 store 聚合。
func (m *MultiSchemaStore) ListAPIKeys(tenantID string) []*APIKey {
	if tenantID != "" {
		s, err := m.storeFor(tenantID)
		if err != nil {
			return nil
		}
		return s.ListAPIKeys(tenantID)
	}
	// 空串=全部租户。
	out := make([]*APIKey, 0)
	for _, s := range m.allStores() {
		out = append(out, s.ListAPIKeys("")...)
	}
	return out
}

// DeleteAPIKey 删除 API Key：用 tenantID 路由。
func (m *MultiSchemaStore) DeleteAPIKey(tenantID, id string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeleteAPIKey(tenantID, id)
}

// ============================================================================
// PluginStore 实现（5 方法）
// ============================================================================

// CreatePlugin 创建插件：插件为全局资源，用 default 租户 store。
func (m *MultiSchemaStore) CreatePlugin(plugin *Plugin) *Plugin {
	if plugin == nil {
		return nil
	}
	s, err := m.storeFor("default")
	if err != nil {
		return nil
	}
	return s.CreatePlugin(plugin)
}

// GetPlugin 按 ID 返回单个插件：用 default 租户 store。
func (m *MultiSchemaStore) GetPlugin(id string) (*Plugin, bool) {
	s, err := m.storeFor("default")
	if err != nil {
		return nil, false
	}
	return s.GetPlugin(id)
}

// UpdatePlugin 更新插件：用 default 租户 store。
func (m *MultiSchemaStore) UpdatePlugin(plugin *Plugin) (*Plugin, bool) {
	if plugin == nil {
		return nil, false
	}
	s, err := m.storeFor("default")
	if err != nil {
		return nil, false
	}
	return s.UpdatePlugin(plugin)
}

// ListPlugins 返回全部插件：用 default 租户 store。
func (m *MultiSchemaStore) ListPlugins() []*Plugin {
	s, err := m.storeFor("default")
	if err != nil {
		return nil
	}
	return s.ListPlugins()
}

// DeletePlugin 按 ID 删除插件：用 default 租户 store。
func (m *MultiSchemaStore) DeletePlugin(id string) bool {
	s, err := m.storeFor("default")
	if err != nil {
		return false
	}
	return s.DeletePlugin(id)
}

// ============================================================================
// BillingStore 实现（13 方法）
// ============================================================================

// CreateBillingPlan 创建订阅计划：计划为全局资源，用 default 租户 store。
func (m *MultiSchemaStore) CreateBillingPlan(plan *SubscriptionPlan) *SubscriptionPlan {
	if plan == nil {
		return nil
	}
	s, err := m.storeFor("default")
	if err != nil {
		return nil
	}
	return s.CreateBillingPlan(plan)
}

// GetBillingPlan 按 ID 返回单个订阅计划：用 default 租户 store。
func (m *MultiSchemaStore) GetBillingPlan(id string) (*SubscriptionPlan, bool) {
	s, err := m.storeFor("default")
	if err != nil {
		return nil, false
	}
	return s.GetBillingPlan(id)
}

// ListBillingPlans 返回全部订阅计划：用 default 租户 store。
func (m *MultiSchemaStore) ListBillingPlans() []*SubscriptionPlan {
	s, err := m.storeFor("default")
	if err != nil {
		return nil
	}
	return s.ListBillingPlans()
}

// UpdateBillingPlan 更新订阅计划：用 default 租户 store。
func (m *MultiSchemaStore) UpdateBillingPlan(plan *SubscriptionPlan) (*SubscriptionPlan, bool) {
	if plan == nil {
		return nil, false
	}
	s, err := m.storeFor("default")
	if err != nil {
		return nil, false
	}
	return s.UpdateBillingPlan(plan)
}

// DeleteBillingPlan 按 ID 删除订阅计划：用 default 租户 store。
func (m *MultiSchemaStore) DeleteBillingPlan(id string) bool {
	s, err := m.storeFor("default")
	if err != nil {
		return false
	}
	return s.DeleteBillingPlan(id)
}

// CreateSubscription 创建订阅：用 sub.TenantID 路由。
func (m *MultiSchemaStore) CreateSubscription(sub *Subscription) *Subscription {
	if sub == nil {
		return nil
	}
	tenantID := sub.TenantID
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateSubscription(sub)
}

// GetSubscription 按 ID 返回单个订阅：遍历全部租户 store 查找。
func (m *MultiSchemaStore) GetSubscription(id string) (*Subscription, bool) {
	for _, s := range m.allStores() {
		if sub, ok := s.GetSubscription(id); ok {
			return sub, true
		}
	}
	return nil, false
}

// ListSubscriptions 返回指定租户的全部订阅：用 tenantID 路由。
// M5：tenantID 为空串时遍历全部租户 store 聚合（统一"空串=跨租户聚合"语义，
// 照抄 ListAPIKeys("") 既有模式），避免空串走 storeFor("") 返回 errEmptyTenant 导致 nil。
// 仅 admin 聚合视图使用（billing:read 为 admin-only）。
func (m *MultiSchemaStore) ListSubscriptions(tenantID string) []*Subscription {
	if tenantID != "" {
		s, err := m.storeFor(tenantID)
		if err != nil {
			return nil
		}
		return s.ListSubscriptions(tenantID)
	}
	// 空串=全部租户聚合。
	out := make([]*Subscription, 0)
	for _, s := range m.allStores() {
		out = append(out, s.ListSubscriptions("")...)
	}
	return out
}

// UpdateSubscription 更新订阅：用 sub.TenantID 路由。
func (m *MultiSchemaStore) UpdateSubscription(sub *Subscription) (*Subscription, bool) {
	if sub == nil {
		return nil, false
	}
	s, err := m.storeFor(sub.TenantID)
	if err != nil {
		return nil, false
	}
	return s.UpdateSubscription(sub)
}

// DeleteSubscription 按 ID 删除订阅：遍历全部租户 store 查找。
func (m *MultiSchemaStore) DeleteSubscription(id string) bool {
	for _, s := range m.allStores() {
		if s.DeleteSubscription(id) {
			return true
		}
	}
	return false
}

// CreateInvoice 创建账单：用 inv.TenantID 路由。
func (m *MultiSchemaStore) CreateInvoice(inv *Invoice) *Invoice {
	if inv == nil {
		return nil
	}
	tenantID := inv.TenantID
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateInvoice(inv)
}

// GetInvoice 按 ID 返回单个账单：遍历全部租户 store 查找。
func (m *MultiSchemaStore) GetInvoice(id string) (*Invoice, bool) {
	for _, s := range m.allStores() {
		if inv, ok := s.GetInvoice(id); ok {
			return inv, true
		}
	}
	return nil, false
}

// ListInvoices 返回指定租户的全部账单：用 tenantID 路由。
// M5：tenantID 为空串时遍历全部租户 store 聚合（统一"空串=跨租户聚合"语义，
// 照抄 ListAPIKeys("") 既有模式）。仅 admin 聚合视图使用。
func (m *MultiSchemaStore) ListInvoices(tenantID string) []*Invoice {
	if tenantID != "" {
		s, err := m.storeFor(tenantID)
		if err != nil {
			return nil
		}
		return s.ListInvoices(tenantID)
	}
	// 空串=全部租户聚合。
	out := make([]*Invoice, 0)
	for _, s := range m.allStores() {
		out = append(out, s.ListInvoices("")...)
	}
	return out
}
