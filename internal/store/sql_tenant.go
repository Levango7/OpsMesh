
// sql_tenant.go 实现 SQLStore 的 TenantStore 子接口（Phase 6，桩实现）。
//
// TODO(p6): 接入 MySQL 持久化（tenants 表）。
// MVP 用桩实现，保证接口齐全 + go build 通过。
package store

import "time"

// CreateTenant 创建租户（桩实现）。
func (s *SQLStore) CreateTenant(tenant *Tenant) *Tenant {
	if tenant == nil {
		return nil
	}
	if tenant.ID == "" {
		tenant.ID = randTenantID()
	}
	now := time.Now().UTC()
	if tenant.CreatedAt.IsZero() {
		tenant.CreatedAt = now
	}
	if tenant.Status == "" {
		tenant.Status = TenantStatusActive
	}
	tenant.UpdatedAt = now
	return tenant
}

// GetTenant 按 ID 返回单个租户（桩实现）。
func (s *SQLStore) GetTenant(id string) (*Tenant, bool) {
	return nil, false
}

// UpdateTenant 更新租户（桩实现）。
func (s *SQLStore) UpdateTenant(tenant *Tenant) (*Tenant, bool) {
	return nil, false
}

// ListTenants 返回全部租户（桩实现）。
func (s *SQLStore) ListTenants() []*Tenant {
	return []*Tenant{}
}

// DeleteTenant 按 ID 删除租户（桩实现）。
func (s *SQLStore) DeleteTenant(id string) bool {
	return false
}