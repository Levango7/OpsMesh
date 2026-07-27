package orchestration

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// 工作流存储错误（与 cmdb/deploy 同构）。
var (
	ErrWFNotFound     = fmt.Errorf("workflow not found")
	ErrWFTenantMismatch = fmt.Errorf("workflow tenant mismatch")
)

// WorkflowStore 是 M5 工作流定义的存储接口，双后端（Memory / SQL）实现。
type WorkflowStore interface {
	Create(ctx context.Context, wf *WorkflowDef) error
	Get(ctx context.Context, id int64, tenantID string) (*WorkflowDef, error)
	List(ctx context.Context, tenantID string) ([]WorkflowDef, error)
	Update(ctx context.Context, wf *WorkflowDef) error
	Delete(ctx context.Context, id int64, tenantID string) error
}

// MemoryWorkflowStore 内存实现（默认后端，无外部依赖）。
type MemoryWorkflowStore struct {
	mu    sync.RWMutex
	items map[int64]*WorkflowDef
	seq   int64
}

// NewMemory 构造内存工作流存储。
func NewMemory() *MemoryWorkflowStore {
	return &MemoryWorkflowStore{items: make(map[int64]*WorkflowDef)}
}

func (s *MemoryWorkflowStore) Create(_ context.Context, wf *WorkflowDef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	wf.ID = s.seq
	if wf.Status == "" {
		wf.Status = StatusDraft
	}
	now := time.Now()
	if wf.CreatedAt.IsZero() {
		wf.CreatedAt = now
	}
	wf.UpdatedAt = now
	s.items[wf.ID] = wf
	return nil
}

func (s *MemoryWorkflowStore) Get(_ context.Context, id int64, tenantID string) (*WorkflowDef, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	wf, ok := s.items[id]
	if !ok {
		return nil, ErrWFNotFound
	}
	if tenantID != "" && wf.TenantID != tenantID {
		return nil, ErrWFTenantMismatch
	}
	return wf, nil
}

func (s *MemoryWorkflowStore) List(_ context.Context, tenantID string) ([]WorkflowDef, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]WorkflowDef, 0, len(s.items))
	for _, wf := range s.items {
		if tenantID != "" && wf.TenantID != tenantID {
			continue
		}
		out = append(out, *wf)
	}
	return out, nil
}

func (s *MemoryWorkflowStore) Update(_ context.Context, wf *WorkflowDef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.items[wf.ID]
	if !ok {
		return ErrWFNotFound
	}
	if wf.TenantID != cur.TenantID {
		return ErrWFTenantMismatch
	}
	wf.UpdatedAt = time.Now()
	s.items[wf.ID] = wf
	return nil
}

func (s *MemoryWorkflowStore) Delete(_ context.Context, id int64, tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.items[id]
	if !ok {
		return ErrWFNotFound
	}
	if tenantID != "" && cur.TenantID != tenantID {
		return ErrWFTenantMismatch
	}
	delete(s.items, id)
	return nil
}
