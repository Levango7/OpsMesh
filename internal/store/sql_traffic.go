package store

// sql_traffic.go 实现 SQLStore 的 TrafficStore 子接口（Phase 2 流量治理）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - Create/Enable/Disable 返回 nil（不返回填充后的假对象）；
//   - Get/Update/Delete 返回 (nil, false) / false；List 返回非 nil 空切片。
//
// TODO(p2): 接入 MySQL 持久化（traffic_policies 表：id PK + tenant_id + name +
// service_name + type + canary_weights JSON + mirror_percent + timeout +
// retries + retry_timeout + max_conns + max_requests + status + created_at + updated_at）。

// CreatePolicy 创建流量策略（未实现的桩）。
func (s *SQLStore) CreatePolicy(tenantID string, p *TrafficPolicy) *TrafficPolicy {
	StubNotImplemented("traffic", "CreatePolicy")
	return nil
}

// GetPolicy 按 (tenantID, id) 返回单个策略（未实现的桩）。
func (s *SQLStore) GetPolicy(tenantID, id string) (*TrafficPolicy, bool) {
	StubNotImplemented("traffic", "GetPolicy")
	return nil, false
}

// UpdatePolicy 更新策略（未实现的桩）。
func (s *SQLStore) UpdatePolicy(tenantID string, p *TrafficPolicy) (*TrafficPolicy, bool) {
	StubNotImplemented("traffic", "UpdatePolicy")
	return nil, false
}

// ListPolicies 返回指定租户的全部策略（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListPolicies(tenantID string) []*TrafficPolicy {
	StubNotImplemented("traffic", "ListPolicies")
	return []*TrafficPolicy{}
}

// DeletePolicy 删除策略（未实现的桩）。
func (s *SQLStore) DeletePolicy(tenantID, id string) bool {
	StubNotImplemented("traffic", "DeletePolicy")
	return false
}

// EnablePolicy 启用策略（未实现的桩）。
func (s *SQLStore) EnablePolicy(tenantID, id string) (*TrafficPolicy, bool) {
	StubNotImplemented("traffic", "EnablePolicy")
	return nil, false
}

// DisablePolicy 禁用策略（未实现的桩）。
func (s *SQLStore) DisablePolicy(tenantID, id string) (*TrafficPolicy, bool) {
	StubNotImplemented("traffic", "DisablePolicy")
	return nil, false
}
