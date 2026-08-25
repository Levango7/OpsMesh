package store

// sql_compliance.go 实现 SQLStore 的 ComplianceStore 子接口（Phase 3 安全合规，桩实现）。
//
// TODO(p3): 接入 MySQL 持久化（compliance_reports 表：id PK + tenant_id + device_id +
// results JSON + score + created_at）。
// MVP 用内存桩实现，保证接口齐全 + go build 通过；DB 不可用时返回零值，
// 不 panic，与 SQLStore 其他桩方法风格一致（参考 sql_traffic.go）。

// SaveReport 保存合规报告（桩实现）。
func (s *SQLStore) SaveReport(tenantID string, r *ComplianceReport) *ComplianceReport {
	return nil
}

// GetReport 按 (tenantID, id) 返回单个合规报告（桩实现）。
func (s *SQLStore) GetReport(tenantID, id string) (*ComplianceReport, bool) {
	return nil, false
}

// ListReports 返回指定租户的全部合规报告（桩实现）。
func (s *SQLStore) ListReports(tenantID string) []*ComplianceReport {
	return []*ComplianceReport{}
}

// DeleteReport 删除合规报告（桩实现）。
func (s *SQLStore) DeleteReport(tenantID, id string) bool {
	return false
}