
// sql_slo.go 实现 SQLStore 的 SLOStore 子接口（Phase 1 SLO 管理）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - CreateSLO 返回 nil（不返回填充后的假对象）；
//   - Get/Update/Delete 返回 (nil, false) / false；List/SLIStatus 返回空切片或 nil。
//
// TODO(p1): 接入 MySQL 持久化（slos 表：id PK + tenant_id + name + description +
// service_name + target + window + slis JSON + created_at + updated_at）。
package store

// CreateSLO 创建 SLO（未实现的桩）。
// TODO(p1): 落库 slos 表（INSERT ... ON DUPLICATE KEY UPDATE）。
func (s *SQLStore) CreateSLO(tenantID string, slo *SLO) *SLO {
	StubNotImplemented("slo", "CreateSLO")
	return nil
}

// GetSLO 按 (tenantID, id) 返回单个 SLO（未实现的桩）。
// TODO(p1): SELECT * FROM slos WHERE id=? AND tenant_id=?。
func (s *SQLStore) GetSLO(tenantID, id string) (*SLO, bool) {
	StubNotImplemented("slo", "GetSLO")
	return nil, false
}

// UpdateSLO 更新 SLO（未实现的桩）。
// TODO(p1): UPDATE slos SET ... WHERE id=? AND tenant_id=?。
func (s *SQLStore) UpdateSLO(tenantID string, slo *SLO) (*SLO, bool) {
	StubNotImplemented("slo", "UpdateSLO")
	return nil, false
}

// ListSLOs 返回指定租户的全部 SLO（未实现的桩；返回非 nil 空切片防上层 range panic）。
// TODO(p1): SELECT * FROM slos WHERE tenant_id=? ORDER BY created_at ASC。
func (s *SQLStore) ListSLOs(tenantID string) []*SLO {
	StubNotImplemented("slo", "ListSLOs")
	return []*SLO{}
}

// DeleteSLO 删除 SLO（未实现的桩）。
// TODO(p1): DELETE FROM slos WHERE id=? AND tenant_id=?。
func (s *SQLStore) DeleteSLO(tenantID, id string) bool {
	StubNotImplemented("slo", "DeleteSLO")
	return false
}

// SLIStatus 返回指定 SLO 下各 SLI 的当前状态（未实现的桩）。
// TODO(p1): 接入 Prometheus 真实评估，按 SLI.Metric 查询当前值并比对 Target。
func (s *SQLStore) SLIStatus(tenantID, id string) []*SLIStatus {
	StubNotImplemented("slo", "SLIStatus")
	return nil
}
