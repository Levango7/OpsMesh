package store

// memory_backup.go 实现 MemoryStore 的 BackupStore 子接口（Phase 3 灾备恢复）。
//
// 备份记录内存实现：
//   - backups 字段在 MemoryStore struct 中定义（map[string]*BackupRecord）；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 4 个方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_compliance.go 风格一致）：
//   - ListBackups 返回深拷贝避免外部修改破坏内部状态；
//   - CreateBackup 分配随机 ID（"backup-" + 16 字节 hex）。

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

// randBackupID 生成随机备份记录 ID（"backup-" + 16 字节 hex）。
func randBackupID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("backup-%d", time.Now().UnixNano())
	}
	return "backup-" + hex.EncodeToString(b)
}

// cloneBackupRecord 返回 b 的深拷贝。
func cloneBackupRecord(b *BackupRecord) *BackupRecord {
	if b == nil {
		return nil
	}
	cp := *b
	return &cp
}

// CreateBackup 创建备份记录（ID 为空时分配随机 ID）。
func (m *MemoryStore) CreateBackup(tenantID string, b *BackupRecord) *BackupRecord {
	if b == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	b.TenantID = tenantID
	m.mu.Lock()
	defer m.mu.Unlock()
	if b.ID == "" {
		b.ID = randBackupID()
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now()
	}
	if b.Status == "" {
		b.Status = "creating"
	}
	m.backups[b.ID] = b
	return cloneBackupRecord(b)
}

// GetBackup 按 (tenantID, id) 返回单个备份记录（深拷贝）。
func (m *MemoryStore) GetBackup(tenantID, id string) (*BackupRecord, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.backups[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && b.TenantID != tenantID {
		return nil, false
	}
	return cloneBackupRecord(b), true
}

// ListBackups 返回指定租户的全部备份记录（深拷贝，按创建时间降序）。
func (m *MemoryStore) ListBackups(tenantID string) []*BackupRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*BackupRecord, 0)
	for _, b := range m.backups {
		if tenantID != "" && b.TenantID != tenantID {
			continue
		}
		out = append(out, cloneBackupRecord(b))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// DeleteBackup 删除备份记录。
func (m *MemoryStore) DeleteBackup(tenantID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.backups[id]
	if !ok {
		return false
	}
	if tenantID != "" && b.TenantID != tenantID {
		return false
	}
	delete(m.backups, id)
	return true
}

// UpdateBackup 更新备份记录的 status/size/path（按 rec.ID 定位，校验 tenantID 归属）。
// 由控制面后台归档 goroutine 将 creating 推进为 completed（回填真实 Size/Path）或 failed。
// 不存在或租户不匹配返回 false；更新成功返回 true（调用方按需重新 GetBackup 读取最新值）。
func (m *MemoryStore) UpdateBackup(tenantID string, rec *BackupRecord) bool {
	if rec == nil || rec.ID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.backups[rec.ID]
	if !ok {
		return false
	}
	if tenantID != "" && existing.TenantID != tenantID {
		return false
	}
	// 仅更新状态机字段（status/size/path），保留其余字段（id/type/created_at/tenant_id）。
	existing.Status = rec.Status
	existing.Size = rec.Size
	existing.Path = rec.Path
	return true
}
