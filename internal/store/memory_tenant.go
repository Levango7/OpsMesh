// memory_tenant.go 实现 MemoryStore 的 TenantStore 子接口（Phase 6 租户管理）。
//
// 租户内存实现：
//   - tenants 字段在 MemoryStore struct 中定义（map[string]*Tenant）；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 5 个方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_ticket.go 风格一致）：
//   - ListTenants 返回深拷贝避免外部修改破坏内部状态；
//   - CreateTenant 分配随机 ID（"tenant-" + 16 字节 hex）；
//   - ListTenants 按创建时间升序。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// randTenantID 生成随机租户 ID（"tenant-" + 16 字节 hex）。
func randTenantID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("tenant-%d", time.Now().UnixNano())
	}
	return "tenant-" + hex.EncodeToString(b)
}

// cloneTenant 返回 t 的深拷贝（值类型字段浅拷贝即深拷贝）。
func cloneTenant(t *Tenant) *Tenant {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

// CreateTenant 创建租户（按 ID 幂等；ID 为空时分配随机 ID）。
func (m *MemoryStore) CreateTenant(tenant *Tenant) *Tenant {
	if tenant == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if tenant.ID == "" {
		tenant.ID = randTenantID()
	}
	if tenant.CreatedAt.IsZero() {
		tenant.CreatedAt = now
	}
	if tenant.Status == "" {
		tenant.Status = TenantStatusActive
	}
	tenant.UpdatedAt = now
	m.tenants[tenant.ID] = tenant
	return cloneTenant(tenant)
}

// GetTenant 按 ID 返回单个租户（深拷贝；不存在返回 (nil, false)）。
func (m *MemoryStore) GetTenant(id string) (*Tenant, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tenants[id]
	if !ok {
		return nil, false
	}
	return cloneTenant(t), true
}

// UpdateTenant 更新租户（按 tenant.ID 定位）。
// CreatedAt 不可改（保留原值）；UpdatedAt 始终刷新。
func (m *MemoryStore) UpdateTenant(tenant *Tenant) (*Tenant, bool) {
	if tenant == nil || tenant.ID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.tenants[tenant.ID]
	if !ok {
		return nil, false
	}
	tenant.ID = existing.ID
	tenant.CreatedAt = existing.CreatedAt
	tenant.UpdatedAt = time.Now()
	m.tenants[tenant.ID] = tenant
	return cloneTenant(tenant), true
}

// ListTenants 返回全部租户（按创建时间升序）。
func (m *MemoryStore) ListTenants() []*Tenant {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Tenant, 0, len(m.tenants))
	for _, t := range m.tenants {
		out = append(out, cloneTenant(t))
	}
	// 按创建时间升序（插入排序）。
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// DeleteTenant 按 ID 删除租户。不存在返回 false。
func (m *MemoryStore) DeleteTenant(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tenants[id]; !ok {
		return false
	}
	delete(m.tenants, id)
	return true
}
