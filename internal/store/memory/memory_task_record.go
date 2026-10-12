// memory_task_record.go MemoryStore 对 TaskRecordStore 的实现（TD-60 task 域 R6）。
//
// 与 gateway_routes 同一模式：复用 MemoryStore.mu；外层按 tenantID 隔离（与 task_records
// 表的 tenant_id 同语义）；读写一律深拷贝。键 = kind|id（与 SQL 侧联合主键同形）。
//
// memory 后端下"重启即丢"是它的既有语义（存储层文档已声明），此实现是"照 SQL 语义把契约补齐"，
// 好让控制面不必区分后端读同一套方法。
package memory

import (
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
)

func taskRecordKey(kind, id string) string { return kind + "|" + id }

// SaveTaskRecord 幂等 upsert（按 tenantID + kind + id）。新建填 CreatedAt，已存在保留原创建时间。
func (m *MemoryStore) SaveTaskRecord(rec *model.TaskRecord) error {
	if rec == nil || rec.TenantID == "" || rec.Kind == "" || rec.ID == "" {
		return nil // 无主键可持久化即静默忽略（上游已校验，这里兜底不 panic）
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.taskRecords == nil {
		m.taskRecords = make(map[string]map[string]*model.TaskRecord)
	}
	if m.taskRecords[rec.TenantID] == nil {
		m.taskRecords[rec.TenantID] = make(map[string]*model.TaskRecord)
	}
	cp := model.CloneTaskRecord(rec)
	if existing, ok := m.taskRecords[rec.TenantID][taskRecordKey(rec.Kind, rec.ID)]; ok {
		cp.CreatedAt = existing.CreatedAt
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	m.taskRecords[rec.TenantID][taskRecordKey(rec.Kind, rec.ID)] = cp
	return nil
}

// GetTaskRecord 按租户 + kind + ID 取单条（不存在返回 nil，深拷贝）。
func (m *MemoryStore) GetTaskRecord(tenantID, kind, id string) *model.TaskRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return model.CloneTaskRecord(m.taskRecords[tenantID][taskRecordKey(kind, id)])
}
