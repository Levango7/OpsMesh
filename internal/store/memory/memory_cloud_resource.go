// memory_cloud_resource.go 实现 MemoryStore 的 CloudResourceStore 子接口（Wave 2-B 云资源）。
//
// 云资源内存实现：
//   - cloudResources 字段在 MemoryStore struct 中定义（map[string]*CloudResource）；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 4 个方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_cloud_account.go 风格一致）：
//   - CreateCloudResource 分配随机 ID（"cloud-res-" + 16 字节 hex）；TenantID 空时归一 default；
//     Status 默认 unknown，Tags 空时存 "{}"；
//   - GetCloudResource 按 ID 直查（深拷贝；不存在返回 (nil, nil)）；
//   - ListCloudResources 按 tenant_id 过滤，可按 cloud_account_id / resource_type 附加过滤，
//     返回总数 + 分页（offset/limit），按创建时间降序；
//   - DeleteCloudResourcesByAccount 按 cloud_account_id 批量删除；
//   - 读路径返回深拷贝（CloneCloudResource）。
package memory

import (
	"context"
	"errors"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/model"
)

var (
	errCloudResourceNotFound = errors.New("cloud resource not found")
	errCloudResourceInvalid  = errors.New("invalid input")
)

// CreateCloudResource 创建云资源（按 ID 幂等；ID 为空时分配随机 ID）。
func (m *MemoryStore) CreateCloudResource(_ context.Context, res *CloudResource) error {
	if res == nil {
		return errCloudResourceInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if res.TenantID == "" {
		res.TenantID = "default"
	}
	now := time.Now()
	if res.ID == "" {
		res.ID = model.RandCloudResourceID()
	}
	if res.CreatedAt.IsZero() {
		res.CreatedAt = now
	}
	if res.Status == "" {
		res.Status = "unknown"
	}
	if res.Tags == "" {
		res.Tags = "{}"
	}
	res.UpdatedAt = now
	m.cloudResources[res.ID] = res
	return nil
}

// GetCloudResource 按 ID 返回单个云资源（深拷贝；不存在返回 (nil, nil)）。
func (m *MemoryStore) GetCloudResource(_ context.Context, id string) (*CloudResource, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.cloudResources[id]
	if !ok {
		return nil, nil
	}
	return model.CloneCloudResource(r), nil
}

// ListCloudResources 返回指定租户的云资源列表（按 tenant_id 隔离，可按 accountID /
// resourceType 过滤，总数 + 分页）。
func (m *MemoryStore) ListCloudResources(_ context.Context, tenantID string, accountID string, resourceType string, offset, limit int) ([]*CloudResource, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var all []*CloudResource
	for _, r := range m.cloudResources {
		if r.TenantID != tenantID {
			continue
		}
		if accountID != "" && r.CloudAccountID != accountID {
			continue
		}
		if resourceType != "" && r.ResourceType != resourceType {
			continue
		}
		all = append(all, model.CloneCloudResource(r))
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

// DeleteCloudResourcesByAccount 删除指定云账号下全部云资源，返回删除条数（error 始终 nil）。
func (m *MemoryStore) DeleteCloudResourcesByAccount(_ context.Context, accountID string) error {
	if accountID == "" {
		return errCloudResourceInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, r := range m.cloudResources {
		if r.CloudAccountID == accountID {
			delete(m.cloudResources, id)
			n++
		}
	}
	_ = n
	return nil
}
