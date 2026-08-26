package store

// sql_backup.go 实现 SQLStore 的 BackupStore 子接口（Phase 3 灾备恢复，生产就绪）。
//
// 表结构：backup_records（id PK + tenant_id + type + status + size + path + created_at）。
// 迁移文件 migrations/012_p3_backup_compliance.sql 幂等建表。
//
// 设计要点（与 sql_argocd.go 风格一致）：
//   - 纯标量字段，无 JSON 列；无 updated_at（只有 created_at）；
//   - CreateBackup 用 INSERT ... ON DUPLICATE KEY UPDATE 实现幂等 upsert（按 id 唯一）；
//   - Get/Delete 均 WHERE id=? AND tenant_id=? 实现租户隔离；
//   - ListBackups 按 created_at DESC 返回；
//   - ID 生成复用 memory_backup.go 的 randBackupID()（前缀 backup-）；
//   - DB 不可用时返回零值（nil/false/空 slice），不 panic，与 SQLStore 其他方法一致。

import (
	"context"
	"log"
	"time"
)

// backupRecordColumns 是 backup_records 表的查询列清单（与 scanBackupRecord 顺序一致）。
const backupRecordColumns = `id, tenant_id, type, status, size, path, created_at`

// scanBackupRecord 从一行扫描出 *BackupRecord。
// 列顺序：id, tenant_id, type, status, size, path, created_at。
// 无行或扫描失败返回 nil（含 sql.ErrNoRows，由调用方解释为不存在）。
func scanBackupRecord(row rowScanner) *BackupRecord {
	var b BackupRecord
	var createdAt time.Time
	if err := row.Scan(&b.ID, &b.TenantID, &b.Type, &b.Status, &b.Size, &b.Path, &createdAt); err != nil {
		return nil
	}
	b.CreatedAt = createdAt
	return &b
}

// CreateBackup 创建备份记录（ID 为空时分配随机 ID）。
//
// 行为与 MemoryStore.CreateBackup 一致：
//   - b==nil 返回 nil；
//   - 空租户归一为 default；
//   - ID 为空时由 randBackupID() 分配；
//   - Type 空 → full；Status 空 → creating；
//   - CreatedAt 零值填 now；
//   - INSERT ... ON DUPLICATE KEY UPDATE 实现幂等 upsert（按 id 唯一）。
func (s *SQLStore) CreateBackup(tenantID string, b *BackupRecord) *BackupRecord {
	if b == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	b.TenantID = tenantID
	now := time.Now().UTC()
	if b.ID == "" {
		b.ID = randBackupID()
	}
	if b.Type == "" {
		b.Type = "full"
	}
	if b.Status == "" {
		b.Status = "creating"
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
	}
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO backup_records (id, tenant_id, type, status, size, path, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE type=VALUES(type), status=VALUES(status), size=VALUES(size), path=VALUES(path)`,
		b.ID, b.TenantID, b.Type, b.Status, b.Size, b.Path, b.CreatedAt); err != nil {
		log.Printf("[store] CreateBackup 插入失败 (tenant=%s id=%s): %v", tenantID, b.ID, err)
		return nil
	}
	return b
}

// GetBackup 按 (tenantID, id) 返回单个备份记录。不存在返回 (nil, false)。
func (s *SQLStore) GetBackup(tenantID, id string) (*BackupRecord, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT `+backupRecordColumns+` FROM backup_records WHERE id=? AND tenant_id=?`, id, tenantID)
	b := scanBackupRecord(row)
	if b == nil {
		return nil, false
	}
	return b, true
}

// ListBackups 返回指定租户的全部备份记录（按 created_at DESC）。
func (s *SQLStore) ListBackups(tenantID string) []*BackupRecord {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT `+backupRecordColumns+` FROM backup_records WHERE tenant_id=? ORDER BY created_at DESC`, tenantID)
	if err != nil {
		log.Printf("[store] ListBackups 查询失败 (tenant=%s): %v", tenantID, err)
		return []*BackupRecord{}
	}
	defer rows.Close()
	out := make([]*BackupRecord, 0)
	for rows.Next() {
		if b := scanBackupRecord(rows); b != nil {
			out = append(out, b)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListBackups 遍历失败: %v", err)
	}
	return out
}

// DeleteBackup 删除备份记录，返回是否删除成功（不存在或租户不匹配返回 false）。
func (s *SQLStore) DeleteBackup(tenantID, id string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`DELETE FROM backup_records WHERE id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		log.Printf("[store] DeleteBackup 失败 (tenant=%s id=%s): %v", tenantID, id, err)
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Printf("[store] DeleteBackup RowsAffected 失败 (tenant=%s id=%s): %v", tenantID, id, rowsErr)
		return false
	}
	return n > 0
}
