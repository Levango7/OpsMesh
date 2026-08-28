package store

import (
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/models"
)

// WorkflowStore is the interface for workflow persistence.
type WorkflowStore interface {
	// Workflow operations
	CreateWorkflow(w *models.Workflow) (*models.Workflow, error)
	GetWorkflow(id string) (*models.Workflow, bool)
	UpdateWorkflow(w *models.Workflow) error
	DeleteWorkflow(id string) bool
	ListWorkflows(status string) []*models.Workflow

	// Execution operations
	CreateExecution(e *models.Execution) (*models.Execution, error)
	GetExecution(id string) (*models.Execution, bool)
	UpdateExecution(e *models.Execution) error
	ListExecutions(workflowID string) []*models.Execution
}

// MemoryStore is an in-memory implementation of WorkflowStore.
type MemoryStore struct {
	mu          sync.RWMutex
	workflows   map[string]*models.Workflow
	executions  map[string]*models.Execution
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		workflows:  make(map[string]*models.Workflow),
		executions: make(map[string]*models.Execution),
	}
}

func (m *MemoryStore) CreateWorkflow(w *models.Workflow) (*models.Workflow, error) {
	if w == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now()
	}
	w.UpdatedAt = time.Now()
	m.workflows[w.ID] = w
	return w, nil
}

func (m *MemoryStore) GetWorkflow(id string) (*models.Workflow, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.workflows[id]
	return w, ok
}

func (m *MemoryStore) UpdateWorkflow(w *models.Workflow) error {
	if w == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workflows[w.ID]; !ok {
		return nil
	}
	w.UpdatedAt = time.Now()
	m.workflows[w.ID] = w
	return nil
}

func (m *MemoryStore) DeleteWorkflow(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workflows[id]; !ok {
		return false
	}
	delete(m.workflows, id)
	return true
}

func (m *MemoryStore) ListWorkflows(status string) []*models.Workflow {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Workflow, 0)
	for _, w := range m.workflows {
		if status != "" && string(w.Status) != status {
			continue
		}
		out = append(out, w)
	}
	return out
}

func (m *MemoryStore) CreateExecution(e *models.Execution) (*models.Execution, error) {
	if e == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.StartedAt.IsZero() {
		e.StartedAt = time.Now()
	}
	m.executions[e.ID] = e
	return e, nil
}

func (m *MemoryStore) GetExecution(id string) (*models.Execution, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.executions[id]
	return e, ok
}

func (m *MemoryStore) UpdateExecution(e *models.Execution) error {
	if e == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.executions[e.ID]; !ok {
		return nil
	}
	m.executions[e.ID] = e
	return nil
}

func (m *MemoryStore) ListExecutions(workflowID string) []*models.Execution {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Execution, 0)
	for _, e := range m.executions {
		if workflowID != "" && e.WorkflowID != workflowID {
			continue
		}
		out = append(out, e)
	}
	return out
}
