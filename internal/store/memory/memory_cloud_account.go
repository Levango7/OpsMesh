// memory_cloud_account.go 实现 MemoryStore 的 CloudAccountStore 子接口（Wave 2-B 云账号）。
//
// 云账号内存实现：
//   - cloudAccounts 字段在 MemoryStore struct 中定义（map[string]*CloudAccount）；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 6 个方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_tenant.go 风格一致）：
//   - CreateCloudAccount 分配随机 ID（"cloud-acct-" + 16 字节 hex）；TenantID 空时归一 default；
//     Status 默认 active，SyncStatus 默认 pending；
//   - GetCloudAccount 按 ID 直查（深拷贝；不存在返回 (nil, nil)）；
//   - ListCloudAccounts 按 tenant_id 过滤，返回总数 + 分页（offset/limit），按创建时间降序；
//   - UpdateCloudAccount 先校验存在，再按字段映射部分更新（CreatedAt/TenantID/ID 不可改）；
//   - DeleteCloudAccount / SyncCloudAccount 按 id 定位；
//   - 读路径返回深拷贝（CloneCloudAccount），避免外部修改污染内部状态。
package memory

import (
	"context"
	"errors"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
)

var (
	errCloudAccountNotFound = errors.New("cloud account not found")
	errCloudAccountInvalid  = errors.New("invalid input")
)

// CreateCloudAccount 创建云账号（按 ID 幂等；ID 为空时分配随机 ID）。
func (m *MemoryStore) CreateCloudAccount(_ context.Context, acct *CloudAccount) error {
	if acct == nil {
		return errCloudAccountInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if acct.TenantID == "" {
		acct.TenantID = "default"
	}
	now := time.Now()
	if acct.ID == "" {
		acct.ID = model.RandCloudAccountID()
	}
	if acct.CreatedAt.IsZero() {
		acct.CreatedAt = now
	}
	if acct.Status == "" {
		acct.Status = "active"
	}
	if acct.SyncStatus == "" {
		acct.SyncStatus = "pending"
	}
	acct.UpdatedAt = now
	m.cloudAccounts[acct.ID] = acct
	return nil
}

// GetCloudAccount 按 ID 返回单个云账号（深拷贝；不存在返回 (nil, nil)）。
func (m *MemoryStore) GetCloudAccount(_ context.Context, id string) (*CloudAccount, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.cloudAccounts[id]
	if !ok {
		return nil, nil
	}
	return model.CloneCloudAccount(a), nil
}

// ListCloudAccounts 返回指定租户的云账号列表（按 tenant_id 隔离，总数 + 分页）。
func (m *MemoryStore) ListCloudAccounts(_ context.Context, tenantID string, offset, limit int) ([]*CloudAccount, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var all []*CloudAccount
	for _, a := range m.cloudAccounts {
		if a.TenantID == tenantID {
			all = append(all, model.CloneCloudAccount(a))
		}
	}
	total := len(all)
	// 按创建时间降序（插入排序）。
	for i := 1; i < len(all); i++ {
		j := i
		for j > 0 && all[j].CreatedAt.After(all[j-1].CreatedAt) {
			all[j], all[j-1] = all[j-1], all[j]
			j--
		}
	}
	// 分页。
	start := offset
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	end := total
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	return all[start:end], total, nil
}

// UpdateCloudAccount 按 ID 部分更新云账号（updates 为字段->值映射）。
func (m *MemoryStore) UpdateCloudAccount(_ context.Context, id string, updates map[string]interface{}) error {
	if id == "" {
		return errCloudAccountInvalid
	}
	if len(updates) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.cloudAccounts[id]
	if !ok {
		return errCloudAccountNotFound
	}
	now := time.Now()
	for k, v := range updates {
		switch k {
		case "workspace_id":
			if s, ok := v.(string); ok {
				existing.WorkspaceID = s
			}
		case "name":
			if s, ok := v.(string); ok {
				existing.Name = s
			}
		case "provider":
			if s, ok := v.(string); ok {
				existing.Provider = s
			}
		case "region":
			if s, ok := v.(string); ok {
				existing.Region = s
			}
		case "auth_type":
			if s, ok := v.(string); ok {
				existing.AuthType = s
			}
		case "credentials_ref":
			if s, ok := v.(string); ok {
				existing.CredentialsRef = s
			}
		case "status":
			if s, ok := v.(string); ok {
				existing.Status = s
			}
		case "sync_status":
			if s, ok := v.(string); ok {
				existing.SyncStatus = s
			}
		case "discovered_count":
			if n, ok := v.(int); ok {
				existing.DiscoveredCount = n
			}
		case "created_by":
			if s, ok := v.(string); ok {
				existing.CreatedBy = s
			}
		}
	}
	existing.UpdatedAt = now
	m.cloudAccounts[id] = existing
	return nil
}

// DeleteCloudAccount 按 ID 删除云账号，不存在返回 error。
func (m *MemoryStore) DeleteCloudAccount(_ context.Context, id string) error {
	if id == "" {
		return errCloudAccountInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.cloudAccounts[id]; !ok {
		return errCloudAccountNotFound
	}
	delete(m.cloudAccounts, id)
	return nil
}

// SyncCloudAccount 回写同步状态（sync_status/discovered_count/last_sync_at/updated_at）。
// 不存在返回 error。
func (m *MemoryStore) SyncCloudAccount(_ context.Context, id string, syncStatus string, discoveredCount int) error {
	if id == "" {
		return errCloudAccountInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.cloudAccounts[id]
	if !ok {
		return errCloudAccountNotFound
	}
	a.SyncStatus = syncStatus
	a.DiscoveredCount = discoveredCount
	if syncStatus == "synced" {
		a.LastSyncAt = time.Now()
	}
	a.UpdatedAt = time.Now()
	m.cloudAccounts[id] = a
	return nil
}
