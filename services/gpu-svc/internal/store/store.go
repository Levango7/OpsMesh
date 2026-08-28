package store

import (
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
)

// GPUStore is the interface for GPU service persistence.
type GPUStore interface {
	// Node operations
	UpsertNode(node *models.GPUNode) error
	GetNode(id string) (*models.GPUNode, bool)
	ListNodes(status string) []*models.GPUNode
	DeleteNode(id string) bool

	// Workload operations
	CreateWorkload(w *models.Workload) (*models.Workload, error)
	GetWorkload(id string) (*models.Workload, bool)
	UpdateWorkload(w *models.Workload) error
	ListWorkloads(status string) []*models.Workload

	// Model operations
	UpsertModel(m *models.GPUModel) error
	GetModel(name string) (*models.GPUModel, bool)
	ListModels() []*models.GPUModel
}

// MemoryStore is an in-memory implementation of GPUStore.
type MemoryStore struct {
	mu        sync.RWMutex
	nodes     map[string]*models.GPUNode
	workloads map[string]*models.Workload
	models    map[string]*models.GPUModel
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		nodes:     make(map[string]*models.GPUNode),
		workloads: make(map[string]*models.Workload),
		models:    make(map[string]*models.GPUModel),
	}
}

func (m *MemoryStore) UpsertNode(node *models.GPUNode) error {
	if node == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if node.CreatedAt.IsZero() {
		node.CreatedAt = time.Now()
	}
	node.UpdatedAt = time.Now()
	m.nodes[node.ID] = node
	return nil
}

func (m *MemoryStore) GetNode(id string) (*models.GPUNode, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.nodes[id]
	return n, ok
}

func (m *MemoryStore) ListNodes(status string) []*models.GPUNode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.GPUNode, 0)
	for _, n := range m.nodes {
		if status != "" && string(n.Status) != status {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (m *MemoryStore) DeleteNode(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.nodes[id]; !ok {
		return false
	}
	delete(m.nodes, id)
	return true
}

func (m *MemoryStore) CreateWorkload(w *models.Workload) (*models.Workload, error) {
	if w == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now()
	}
	w.UpdatedAt = time.Now()
	m.workloads[w.ID] = w
	return w, nil
}

func (m *MemoryStore) GetWorkload(id string) (*models.Workload, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.workloads[id]
	return w, ok
}

func (m *MemoryStore) UpdateWorkload(w *models.Workload) error {
	if w == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workloads[w.ID]; !ok {
		return nil
	}
	w.UpdatedAt = time.Now()
	m.workloads[w.ID] = w
	return nil
}

func (m *MemoryStore) ListWorkloads(status string) []*models.Workload {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Workload, 0)
	for _, w := range m.workloads {
		if status != "" && string(w.Status) != status {
			continue
		}
		out = append(out, w)
	}
	return out
}

func (m *MemoryStore) UpsertModel(model *models.GPUModel) error {
	if model == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.models[model.Name] = model
	return nil
}

func (m *MemoryStore) GetModel(name string) (*models.GPUModel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mod, ok := m.models[name]
	return mod, ok
}

func (m *MemoryStore) ListModels() []*models.GPUModel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.GPUModel, 0)
	for _, mod := range m.models {
		out = append(out, mod)
	}
	return out
}
