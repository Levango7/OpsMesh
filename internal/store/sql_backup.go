package store

// sql_backup.go 实现 SQLStore 的 BackupStore 子接口（Phase 3 灾备恢复，桩实现）。
//
// TODO(p3): 接入 MySQL 持久化（backup_records 表：id PK + tenant_id + type +
// status + size + path + created_at）。
// MVP 用内存桩实现，保证接口齐全 + go build 通过；DB 不可用时返回零值，
// 不 panic，与 SQLStore 其他桩方法风格一致（参考 sql_traffic.go）。

// CreateBackup 创建备份记录（桩实现）。
func (s *SQLStore) CreateBackup(tenantID string, b *BackupRecord) *BackupRecord {
	return nil
}

// GetBackup 按 (tenantID, id) 返回单个备份记录（桩实现）。
func (s *SQLStore) GetBackup(tenantID, id string) (*BackupRecord, bool) {
	return nil, false
}

// ListBackups 返回指定租户的全部备份记录（桩实现）。
func (s *SQLStore) ListBackups(tenantID string) []*BackupRecord {
	return []*BackupRecord{}
}

// DeleteBackup 删除备份记录（桩实现）。
func (s *SQLStore) DeleteBackup(tenantID, id string) bool {
	return false
}