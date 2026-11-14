package store

import (
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/models"
)

// RunbookStore is the interface for runbook persistence.
// RunbookStore 的每个读/改方法显式带租户（D4）：漏传即编译不过。
type RunbookStore interface {
	CreateRunbook(*models.Runbook) *models.Runbook
	GetRunbook(id, tenantID string) *models.Runbook
	ListRunbooks(tenantID string) []*models.Runbook
	UpdateRunbook(r *models.Runbook, tenantID string) bool
	DeleteRunbook(id, tenantID string) bool
	AddExecution(*models.ExecutionRecord)
	GetExecutions(runbookID, tenantID string) []*models.ExecutionRecord
}

// MemoryStore is an in-memory implementation of RunbookStore.
type MemoryStore struct {
	mu         sync.RWMutex
	runbooks   map[string]*models.Runbook
	executions []*models.ExecutionRecord
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		runbooks:   make(map[string]*models.Runbook),
		executions: make([]*models.ExecutionRecord, 0),
	}
}

// CreateRunbook stores a new runbook. 空租户拒绝写入（无主数据不落库）。
func (m *MemoryStore) CreateRunbook(r *models.Runbook) *models.Runbook {
	if r == nil || r.TenantID == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// 时间戳尊重调用方已设值（service.CreateRunbook 以同一 now 赋 CreatedAt==UpdatedAt，
	// "创建时两时间戳相等"是服务契约）；此前无条件用新 time.Now() 覆盖 UpdatedAt，
	// 高精度单调钟平台（Linux）产生纳秒级偏差打破契约（CI 实测失败；Windows
	// 定时器粒度粗偶然相等所以本地从不触发）。未设值时才兜底。
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = r.CreatedAt
	}
	cp := *r
	m.runbooks[r.ID] = &cp
	return r
}

// GetRunbook 按 id + 租户取；跨租户与不存在同样返回 nil。
func (m *MemoryStore) GetRunbook(id, tenantID string) *models.Runbook {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runbooks[id]
	if !ok || r.TenantID != tenantID {
		return nil
	}
	cp := *r
	return &cp
}

// ListRunbooks 只返回该租户的 runbook。
func (m *MemoryStore) ListRunbooks(tenantID string) []*models.Runbook {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Runbook, 0, len(m.runbooks))
	for _, r := range m.runbooks {
		if r.TenantID != tenantID {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	return out
}

// UpdateRunbook 跨租户不可改；且**不允许改租户**（归属变更=越权改挂）。
func (m *MemoryStore) UpdateRunbook(r *models.Runbook, tenantID string) bool {
	if r == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.runbooks[r.ID]
	if !ok || existing.TenantID != tenantID {
		return false
	}
	r.TenantID = existing.TenantID
	r.UpdatedAt = time.Now()
	cp := *r
	m.runbooks[r.ID] = &cp
	return true
}

// DeleteRunbook 跨租户删不掉。
func (m *MemoryStore) DeleteRunbook(id, tenantID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.runbooks[id]
	if !ok || existing.TenantID != tenantID {
		return false
	}
	delete(m.runbooks, id)
	return true
}

// AddExecution records a runbook execution. 归属由**父 runbook** 决定：不属于该租户 ⇒ 拒收。
func (m *MemoryStore) AddExecution(e *models.ExecutionRecord) {
	if e == nil {
		return
	}
	if rb, ok := m.runbooks[e.RunbookID]; !ok || rb.TenantID != e.TenantID {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.StartedAt.IsZero() {
		e.StartedAt = time.Now()
	}
	m.executions = append(m.executions, e)
}

// GetExecutions 归属由父 runbook 决定（跨租户 ⇒ 空）。
func (m *MemoryStore) GetExecutions(runbookID, tenantID string) []*models.ExecutionRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rb, ok := m.runbooks[runbookID]
	if !ok || rb.TenantID != tenantID {
		return nil
	}
	out := make([]*models.ExecutionRecord, 0)
	for _, e := range m.executions {
		if e.RunbookID == runbookID && e.TenantID == tenantID {
			cp := *e
			out = append(out, &cp)
		}
	}
	return out
}
