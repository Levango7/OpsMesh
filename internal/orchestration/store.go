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
// CreateRun/ListRuns/UpdateRun 管理工作流运行历史（WorkflowRun），用于执行历史与回放。
type WorkflowStore interface {
	Create(ctx context.Context, wf *WorkflowDef) error
	Get(ctx context.Context, id int64, tenantID string) (*WorkflowDef, error)
	List(ctx context.Context, tenantID string) ([]WorkflowDef, error)
	Update(ctx context.Context, wf *WorkflowDef) error
	Delete(ctx context.Context, id int64, tenantID string) error
	CreateRun(ctx context.Context, run *WorkflowRun) error
	ListRuns(ctx context.Context, workflowID int64, tenantID string) ([]WorkflowRun, error)
	UpdateRun(ctx context.Context, run *WorkflowRun) error
}

// MemoryWorkflowStore 内存实现（默认后端，无外部依赖）。
type MemoryWorkflowStore struct {
	mu    sync.RWMutex
	items map[int64]*WorkflowDef
	runs  map[int64]*WorkflowRun // key = run.ID
	seq   int64
	runSeq int64
}

// NewMemory 构造内存工作流存储。
func NewMemory() *MemoryWorkflowStore {
	return &MemoryWorkflowStore{
		items: make(map[int64]*WorkflowDef),
		runs:  make(map[int64]*WorkflowRun),
	}
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

// CreateRun 创建一条工作流运行记录（执行历史）。分配自增 ID，StartedAt 缺省取当前时间。
func (s *MemoryWorkflowStore) CreateRun(_ context.Context, run *WorkflowRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runSeq++
	run.ID = s.runSeq
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	if run.Status == "" {
		run.Status = StatusRunning
	}
	s.runs[run.ID] = run
	return nil
}

// ListRuns 列出指定工作流的运行历史（按 ID 升序），支持租户隔离。
func (s *MemoryWorkflowStore) ListRuns(_ context.Context, workflowID int64, tenantID string) ([]WorkflowRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]WorkflowRun, 0)
	for i := int64(1); i <= s.runSeq; i++ {
		r, ok := s.runs[i]
		if !ok {
			continue
		}
		if r.WorkflowID != workflowID {
			continue
		}
		if tenantID != "" && r.TenantID != tenantID {
			continue
		}
		out = append(out, *r)
	}
	return out, nil
}

// UpdateRun 更新一条已存在的工作流运行记录（执行历史）。不存在时返回 ErrWFNotFound。
func (s *MemoryWorkflowStore) UpdateRun(_ context.Context, run *WorkflowRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[run.ID]; !ok {
		return ErrWFNotFound
	}
	s.runs[run.ID] = run
	return nil
}
