package store

import (
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/models"
)

// RunbookStore is the interface for runbook persistence.
type RunbookStore interface {
	CreateRunbook(*models.Runbook) *models.Runbook
	GetRunbook(id string) *models.Runbook
	ListRunbooks() []*models.Runbook
	UpdateRunbook(*models.Runbook) bool
	DeleteRunbook(id string) bool
	AddExecution(*models.ExecutionRecord)
	GetExecutions(runbookID string) []*models.ExecutionRecord
}

// MemoryStore is an in-memory implementation of RunbookStore.
type MemoryStore struct {
	mu          sync.RWMutex
	runbooks    map[string]*models.Runbook
	executions  []*models.ExecutionRecord
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		runbooks:   make(map[string]*models.Runbook),
		executions: make([]*models.ExecutionRecord, 0),
	}
}

// CreateRunbook stores a new runbook.
func (m *MemoryStore) CreateRunbook(r *models.Runbook) *models.Runbook {
	if r == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	r.UpdatedAt = time.Now()
	cp := *r
	m.runbooks[r.ID] = &cp
	return r
}

// GetRunbook retrieves a runbook by ID.
func (m *MemoryStore) GetRunbook(id string) *models.Runbook {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runbooks[id]
	if !ok {
		return nil
	}
	cp := *r
	return &cp
}

// ListRunbooks returns all runbooks.
func (m *MemoryStore) ListRunbooks() []*models.Runbook {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Runbook, 0, len(m.runbooks))
	for _, r := range m.runbooks {
		cp := *r
		out = append(out, &cp)
	}
	return out
}

// UpdateRunbook updates an existing runbook.
func (m *MemoryStore) UpdateRunbook(r *models.Runbook) bool {
	if r == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runbooks[r.ID]; !ok {
		return false
	}
	r.UpdatedAt = time.Now()
	cp := *r
	m.runbooks[r.ID] = &cp
	return true
}

// DeleteRunbook removes a runbook by ID.
func (m *MemoryStore) DeleteRunbook(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runbooks[id]; !ok {
		return false
	}
	delete(m.runbooks, id)
	return true
}

// AddExecution records a runbook execution.
func (m *MemoryStore) AddExecution(e *models.ExecutionRecord) {
	if e == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.StartedAt.IsZero() {
		e.StartedAt = time.Now()
	}
	m.executions = append(m.executions, e)
}

// GetExecutions returns execution history for a runbook.
func (m *MemoryStore) GetExecutions(runbookID string) []*models.ExecutionRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.ExecutionRecord, 0)
	for _, e := range m.executions {
		if e.RunbookID == runbookID {
			cp := *e
			out = append(out, &cp)
		}
	}
	return out
}
