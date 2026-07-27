package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// errInvalid 构造字段校验错误。
func errInvalid(msg string) error { return fmt.Errorf("invalid deploy: %s", msg) }

// ErrNotFound 部署任务不存在。
var ErrNotFound = errors.New("deploy not found")

// ErrTenantMismatch 租户越权访问。
var ErrTenantMismatch = errors.New("tenant mismatch")

// DeployStore 是 M3 部署任务的存储接口，双后端（Memory / SQL）。
// 行级租户隔离：所有读/写均按 TenantID 过滤（空串=开发模式放行全部）。
type DeployStore interface {
	// Create 创建部署任务（自动补 ID / 状态=created / 时间戳）。
	Create(ctx context.Context, dt *DeployTask) (*DeployTask, error)
	// Get 按 ID 查询（tenantID 非空时校验归属）。
	Get(ctx context.Context, id int64, tenantID string) (*DeployTask, error)
	// Update 更新部署任务（自动刷 UpdatedAt；禁止越权改租户）。
	Update(ctx context.Context, dt *DeployTask) error
	// List 按租户/状态列出（二者皆可空）。
	List(ctx context.Context, tenantID, status string) ([]DeployTask, error)
	// Delete 删除部署任务（tenantID 非空时校验归属）。
	Delete(ctx context.Context, id int64, tenantID string) error
}

// MemoryDeployStore 内存实现（默认后端，无外部依赖）。
type MemoryDeployStore struct {
	mu  sync.RWMutex
	m   map[int64]*DeployTask
	seq int64
}

// NewMemory 构造内存部署存储。
func NewMemory() *MemoryDeployStore {
	return &MemoryDeployStore{m: make(map[int64]*DeployTask)}
}

// Create 创建部署任务。
func (m *MemoryDeployStore) Create(ctx context.Context, dt *DeployTask) (*DeployTask, error) {
	if dt == nil {
		return nil, errInvalid("nil")
	}
	if dt.TenantID == "" {
		return nil, errInvalid("tenant_id required")
	}
	if err := dt.Valid(); err != nil {
		return nil, err
	}
	if dt.Status == "" {
		dt.Status = StatusCreated
	}
	now := time.Now()
	if dt.CreatedAt.IsZero() {
		dt.CreatedAt = now
	}
	dt.UpdatedAt = now
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	dt.ID = m.seq
	cp := *dt
	m.m[dt.ID] = &cp
	return &cp, nil
}

// Get 按 ID 查询（租户校验）。
func (m *MemoryDeployStore) Get(ctx context.Context, id int64, tenantID string) (*DeployTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dt, ok := m.m[id]
	if !ok {
		return nil, ErrNotFound
	}
	if tenantID != "" && dt.TenantID != tenantID {
		return nil, ErrTenantMismatch
	}
	cp := *dt
	return &cp, nil
}

// Update 更新部署任务。
func (m *MemoryDeployStore) Update(ctx context.Context, dt *DeployTask) error {
	if dt == nil {
		return errInvalid("nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.m[dt.ID]
	if !ok {
		return ErrNotFound
	}
	if dt.TenantID != "" && old.TenantID != dt.TenantID {
		return ErrTenantMismatch
	}
	dt.UpdatedAt = time.Now()
	m.m[dt.ID] = dt
	return nil
}

// List 按租户/状态列出。
func (m *MemoryDeployStore) List(ctx context.Context, tenantID, status string) ([]DeployTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]DeployTask, 0)
	for _, dt := range m.m {
		if tenantID != "" && dt.TenantID != tenantID {
			continue
		}
		if status != "" && dt.Status != status {
			continue
		}
		out = append(out, *dt)
	}
	return out, nil
}

// Delete 删除部署任务（租户校验）。
func (m *MemoryDeployStore) Delete(ctx context.Context, id int64, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dt, ok := m.m[id]
	if !ok {
		return ErrNotFound
	}
	if tenantID != "" && dt.TenantID != tenantID {
		return ErrTenantMismatch
	}
	delete(m.m, id)
	return nil
}

// SplitIDs 按逗号/空白切分目标 ID 列表（忽略空串）。
func SplitIDs(s string) []string {
	if s == "" {
		return nil
	}
	raw := strings.Split(s, ",")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		for _, seg := range strings.Split(p, " ") {
			seg = strings.TrimSpace(seg)
			if seg != "" {
				out = append(out, seg)
			}
		}
	}
	return out
}
