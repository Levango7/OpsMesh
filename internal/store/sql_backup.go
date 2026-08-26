package store

// sql_backup.go 实现 SQLStore 的 BackupStore 子接口（Phase 3 灾备恢复）。
//
// 【未实现的桩】本文件全部方法未接入 MySQL 持久化，经 stub_guard.StubNotImplemented
// 显式告警（每 (domain,method) 首次必打、60s 限频），并按统一约定返回零值：
//   - CreateBackup 返回 nil（不返回填充后的假对象）；
//   - Get/Delete 返回 (nil, false) / false；List 返回非 nil 空切片。
//
// TODO(p3): 接入 MySQL 持久化（backup_records 表：id PK + tenant_id + type +
// status + size + path + created_at）。

// CreateBackup 创建备份记录（未实现的桩）。
func (s *SQLStore) CreateBackup(tenantID string, b *BackupRecord) *BackupRecord {
	StubNotImplemented("backup", "CreateBackup")
	return nil
}

// GetBackup 按 (tenantID, id) 返回单个备份记录（未实现的桩）。
func (s *SQLStore) GetBackup(tenantID, id string) (*BackupRecord, bool) {
	StubNotImplemented("backup", "GetBackup")
	return nil, false
}

// ListBackups 返回指定租户的全部备份记录（未实现的桩；返回非 nil 空切片防上层 range panic）。
func (s *SQLStore) ListBackups(tenantID string) []*BackupRecord {
	StubNotImplemented("backup", "ListBackups")
	return []*BackupRecord{}
}

// DeleteBackup 删除备份记录（未实现的桩）。
func (s *SQLStore) DeleteBackup(tenantID, id string) bool {
	StubNotImplemented("backup", "DeleteBackup")
	return false
}
