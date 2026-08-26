package store

// sql_compliance.go 实现 SQLStore 的 ComplianceStore 子接口（Phase 3 安全合规）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - SaveReport 返回 nil（不返回填充后的假对象）；
//   - Get/Delete 返回 (nil, false) / false；List 返回非 nil 空切片。
//
// TODO(p3): 接入 MySQL 持久化（compliance_reports 表：id PK + tenant_id + device_id +
// results JSON + score + created_at）。

// SaveReport 保存合规报告（未实现的桩）。
func (s *SQLStore) SaveReport(tenantID string, r *ComplianceReport) *ComplianceReport {
	StubNotImplemented("compliance", "SaveReport")
	return nil
}

// GetReport 按 (tenantID, id) 返回单个合规报告（未实现的桩）。
func (s *SQLStore) GetReport(tenantID, id string) (*ComplianceReport, bool) {
	StubNotImplemented("compliance", "GetReport")
	return nil, false
}

// ListReports 返回指定租户的全部合规报告（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListReports(tenantID string) []*ComplianceReport {
	StubNotImplemented("compliance", "ListReports")
	return []*ComplianceReport{}
}

// DeleteReport 删除合规报告（未实现的桩）。
func (s *SQLStore) DeleteReport(tenantID, id string) bool {
	StubNotImplemented("compliance", "DeleteReport")
	return false
}
