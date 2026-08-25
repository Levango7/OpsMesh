package store

// sql_traffic.go 实现 SQLStore 的 TrafficStore 子接口（Phase 2 流量治理，桩实现）。
//
// TODO(p2): 接入 MySQL 持久化（traffic_policies 表：id PK + tenant_id + name +
// service_name + type + canary_weights JSON + mirror_percent + timeout +
// retries + retry_timeout + max_conns + max_requests + status + created_at + updated_at）。
// MVP 用内存桩实现，保证接口齐全 + go build 通过；DB 不可用时返回零值，
// 不 panic，与 SQLStore 其他桩方法风格一致（参考 sql_slo.go）。


// CreatePolicy 创建流量策略（桩实现）。
func (s *SQLStore) CreatePolicy(tenantID string, p *TrafficPolicy) *TrafficPolicy {
	return nil
}

// GetPolicy 按 (tenantID, id) 返回单个策略（桩实现）。
func (s *SQLStore) GetPolicy(tenantID, id string) (*TrafficPolicy, bool) {
	return nil, false
}

// UpdatePolicy 更新策略（桩实现）。
func (s *SQLStore) UpdatePolicy(tenantID string, p *TrafficPolicy) (*TrafficPolicy, bool) {
	return nil, false
}

// ListPolicies 返回指定租户的全部策略（桩实现）。
func (s *SQLStore) ListPolicies(tenantID string) []*TrafficPolicy {
	return []*TrafficPolicy{}
}

// DeletePolicy 删除策略（桩实现）。
func (s *SQLStore) DeletePolicy(tenantID, id string) bool {
	return false
}

// EnablePolicy 启用策略（桩实现）。
func (s *SQLStore) EnablePolicy(tenantID, id string) (*TrafficPolicy, bool) {
	return nil, false
}

// DisablePolicy 禁用策略（桩实现）。
func (s *SQLStore) DisablePolicy(tenantID, id string) (*TrafficPolicy, bool) {
	return nil, false
}