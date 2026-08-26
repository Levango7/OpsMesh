package store

// sql_argocd.go 实现 SQLStore 的 ArgoCDStore 子接口（Phase 2 ArgoCD 应用管理）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - Create/Sync 返回 nil（不返回填充后的假对象）；
//   - Get/Update/Delete 返回 (nil, false) / false；List 返回非 nil 空切片。
//
// TODO(p2): 接入 MySQL 持久化（argocd_apps 表：id PK + tenant_id + name +
// namespace + repo_url + path + target_revision + cluster_url + sync_policy +
// status + health_status + created_at + updated_at）。

// CreateApp 创建 ArgoCD 应用（未实现的桩）。
func (s *SQLStore) CreateApp(tenantID string, a *ArgoCDApp) *ArgoCDApp {
	StubNotImplemented("argocd", "CreateApp")
	return nil
}

// GetApp 按 (tenantID, id) 返回单个应用（未实现的桩）。
func (s *SQLStore) GetApp(tenantID, id string) (*ArgoCDApp, bool) {
	StubNotImplemented("argocd", "GetApp")
	return nil, false
}

// UpdateApp 更新应用（未实现的桩）。
func (s *SQLStore) UpdateApp(tenantID string, a *ArgoCDApp) (*ArgoCDApp, bool) {
	StubNotImplemented("argocd", "UpdateApp")
	return nil, false
}

// ListApps 返回指定租户的全部应用（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListApps(tenantID string) []*ArgoCDApp {
	StubNotImplemented("argocd", "ListApps")
	return []*ArgoCDApp{}
}

// DeleteApp 删除应用（未实现的桩）。
func (s *SQLStore) DeleteApp(tenantID, id string) bool {
	StubNotImplemented("argocd", "DeleteApp")
	return false
}

// SyncApp 触发同步（未实现的桩）。
func (s *SQLStore) SyncApp(tenantID, id string) (*ArgoCDApp, bool) {
	StubNotImplemented("argocd", "SyncApp")
	return nil, false
}
