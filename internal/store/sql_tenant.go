// sql_tenant.go 实现 SQLStore 的 TenantStore 子接口（Phase 6 租户管理）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - CreateTenant 返回 nil（不返回填充后的假对象——杜绝「201 假成功 → GET 404」链路，
//     也避免 multi-schema 下租户元数据与实际 schema 路由状态不一致）；
//   - Get/Update/Delete 返回 (nil, false) / false；ListTenants 返回非 nil 空切片。
//
// TODO(p6): 接入 MySQL 持久化（tenants 表）。
package store

// CreateTenant 创建租户（未实现的桩）。
func (s *SQLStore) CreateTenant(tenant *Tenant) *Tenant {
	StubNotImplemented("tenant", "CreateTenant")
	return nil
}

// GetTenant 按 ID 返回单个租户（未实现的桩）。
func (s *SQLStore) GetTenant(id string) (*Tenant, bool) {
	StubNotImplemented("tenant", "GetTenant")
	return nil, false
}

// UpdateTenant 更新租户（未实现的桩）。
func (s *SQLStore) UpdateTenant(tenant *Tenant) (*Tenant, bool) {
	StubNotImplemented("tenant", "UpdateTenant")
	return nil, false
}

// ListTenants 返回全部租户（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListTenants() []*Tenant {
	StubNotImplemented("tenant", "ListTenants")
	return []*Tenant{}
}

// DeleteTenant 按 ID 删除租户（未实现的桩）。
func (s *SQLStore) DeleteTenant(id string) bool {
	StubNotImplemented("tenant", "DeleteTenant")
	return false
}
