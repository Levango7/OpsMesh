package store

// memory_argocd.go 实现 MemoryStore 的 ArgoCDStore 子接口（Phase 2 ArgoCD 应用管理）。
//
// ArgoCD 应用内存实现：
//   - argocdApps 字段在 MemoryStore struct 中定义（map[string]*ArgoCDApp）；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 6 个方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_ticket.go 风格一致）：
//   - 返回深拷贝避免外部修改破坏内部状态；
//   - CreateApp 分配随机 ID（"argocd-" + 16 字节 hex）；
//   - SyncApp 将 Status 置为 "synced" 并刷新 HealthStatus。


import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// randArgoCDID 生成随机 ArgoCD 应用 ID（"argocd-" + 16 字节 hex）。
func randArgoCDID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("argocd-%d", time.Now().UnixNano())
	}
	return "argocd-" + hex.EncodeToString(b)
}

// cloneArgoCDApp 返回 a 的深拷贝。
func cloneArgoCDApp(a *ArgoCDApp) *ArgoCDApp {
	if a == nil {
		return nil
	}
	cp := *a
	return &cp
}

// CreateApp 创建 ArgoCD 应用（ID 为空时分配随机 ID）。
func (m *MemoryStore) CreateApp(tenantID string, a *ArgoCDApp) *ArgoCDApp {
	if a == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = "default"
	}
	a.TenantID = tenantID
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if a.ID == "" {
		a.ID = randArgoCDID()
	}
	if a.Status == "" {
		a.Status = "unknown"
	}
	if a.HealthStatus == "" {
		a.HealthStatus = "unknown"
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	m.argocdApps[a.ID] = a
	return cloneArgoCDApp(a)
}

// GetApp 按 (tenantID, id) 返回单个应用（深拷贝）。
func (m *MemoryStore) GetApp(tenantID, id string) (*ArgoCDApp, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.argocdApps[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && a.TenantID != tenantID {
		return nil, false
	}
	return cloneArgoCDApp(a), true
}

// UpdateApp 更新应用（按 a.ID 定位，校验 tenantID 归属）。
func (m *MemoryStore) UpdateApp(tenantID string, a *ArgoCDApp) (*ArgoCDApp, bool) {
	if a == nil || a.ID == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.argocdApps[a.ID]
	if !ok {
		return nil, false
	}
	if tenantID != "" && existing.TenantID != tenantID {
		return nil, false
	}
	a.TenantID = existing.TenantID
	a.CreatedAt = existing.CreatedAt
	a.UpdatedAt = time.Now()
	m.argocdApps[a.ID] = a
	return cloneArgoCDApp(a), true
}

// ListApps 返回指定租户的全部应用（深拷贝，按创建时间降序）。
func (m *MemoryStore) ListApps(tenantID string) []*ArgoCDApp {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*ArgoCDApp, 0)
	for _, a := range m.argocdApps {
		if tenantID != "" && a.TenantID != tenantID {
			continue
		}
		out = append(out, cloneArgoCDApp(a))
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// DeleteApp 删除应用。
func (m *MemoryStore) DeleteApp(tenantID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.argocdApps[id]
	if !ok {
		return false
	}
	if tenantID != "" && a.TenantID != tenantID {
		return false
	}
	delete(m.argocdApps, id)
	return true
}

// SyncApp 触发同步：置 Status="synced" + HealthStatus="healthy"（模拟同步结果）。
func (m *MemoryStore) SyncApp(tenantID, id string) (*ArgoCDApp, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.argocdApps[id]
	if !ok {
		return nil, false
	}
	if tenantID != "" && a.TenantID != tenantID {
		return nil, false
	}
	a.Status = "synced"
	a.HealthStatus = "healthy"
	a.UpdatedAt = time.Now()
	return cloneArgoCDApp(a), true
}