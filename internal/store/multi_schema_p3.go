package store

// multi_schema_p3.go MultiSchemaStore 对 Phase 3 两个新接口（ComplianceStore / BackupStore）的委托实现。
//
// MultiSchemaStore 按 tenantID 路由到 per-tenant Store（SQLStore 或测试 mock），
// 各方法委托给底层 store。新接口方法签名与 MemoryStore/SQLStore 一致，
// 仅在路由层做租户隔离分发。
//
// 设计要点（与 multi_schema_p2.go 风格一致）：
//   - 带 tenantID 参数的方法直接用 storeFor(tenantID) 路由。
//   - 路由失败返回零值（nil/false），与现有方法风格一致。

// ============================================================================
// ComplianceStore 实现（4 方法）
// ============================================================================

// SaveReport 保存合规报告：用参数 tenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) SaveReport(tenantID string, r *ComplianceReport) *ComplianceReport {
	if r == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.SaveReport(tenantID, r)
}

// GetReport 按 (tenantID, id) 返回单个合规报告：用 tenantID 路由。
func (m *MultiSchemaStore) GetReport(tenantID, id string) (*ComplianceReport, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetReport(tenantID, id)
}

// ListReports 返回指定租户的全部合规报告：用 tenantID 路由。
func (m *MultiSchemaStore) ListReports(tenantID string) []*ComplianceReport {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListReports(tenantID)
}

// DeleteReport 删除合规报告：用 tenantID 路由。
func (m *MultiSchemaStore) DeleteReport(tenantID, id string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeleteReport(tenantID, id)
}

// ============================================================================
// BackupStore 实现（4 方法）
// ============================================================================

// CreateBackup 创建备份记录：用参数 tenantID 路由（空串归一为 default）。
func (m *MultiSchemaStore) CreateBackup(tenantID string, b *BackupRecord) *BackupRecord {
	if b == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.CreateBackup(tenantID, b)
}

// GetBackup 按 (tenantID, id) 返回单个备份记录：用 tenantID 路由。
func (m *MultiSchemaStore) GetBackup(tenantID, id string) (*BackupRecord, bool) {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil, false
	}
	return s.GetBackup(tenantID, id)
}

// ListBackups 返回指定租户的全部备份记录：用 tenantID 路由。
func (m *MultiSchemaStore) ListBackups(tenantID string) []*BackupRecord {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return nil
	}
	return s.ListBackups(tenantID)
}

// DeleteBackup 删除备份记录：用 tenantID 路由。
func (m *MultiSchemaStore) DeleteBackup(tenantID, id string) bool {
	s, err := m.storeFor(tenantID)
	if err != nil {
		return false
	}
	return s.DeleteBackup(tenantID, id)
}
