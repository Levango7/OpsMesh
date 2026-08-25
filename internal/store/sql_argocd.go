package store

// sql_argocd.go 实现 SQLStore 的 ArgoCDStore 子接口（Phase 2 ArgoCD 应用管理，桩实现）。
//
// TODO(p2): 接入 MySQL 持久化（argocd_apps 表：id PK + tenant_id + name +
// namespace + repo_url + path + target_revision + cluster_url + sync_policy +
// status + health_status + created_at + updated_at）。
// MVP 用内存桩实现，保证接口齐全 + go build 通过；DB 不可用时返回零值，
// 不 panic，与 SQLStore 其他桩方法风格一致（参考 sql_slo.go）。


// CreateApp 创建 ArgoCD 应用（桩实现）。
func (s *SQLStore) CreateApp(tenantID string, a *ArgoCDApp) *ArgoCDApp {
	return nil
}

// GetApp 按 (tenantID, id) 返回单个应用（桩实现）。
func (s *SQLStore) GetApp(tenantID, id string) (*ArgoCDApp, bool) {
	return nil, false
}

// UpdateApp 更新应用（桩实现）。
func (s *SQLStore) UpdateApp(tenantID string, a *ArgoCDApp) (*ArgoCDApp, bool) {
	return nil, false
}

// ListApps 返回指定租户的全部应用（桩实现）。
func (s *SQLStore) ListApps(tenantID string) []*ArgoCDApp {
	return []*ArgoCDApp{}
}

// DeleteApp 删除应用（桩实现）。
func (s *SQLStore) DeleteApp(tenantID, id string) bool {
	return false
}

// SyncApp 触发同步（桩实现）。
func (s *SQLStore) SyncApp(tenantID, id string) (*ArgoCDApp, bool) {
	return nil, false
}