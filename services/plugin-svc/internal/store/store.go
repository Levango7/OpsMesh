package store

import (
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/plugin-svc/internal/models"
)

// PluginStore is the interface for plugin persistence.
type PluginStore interface {
	List() []*models.Plugin
	Get(id string) (*models.Plugin, bool)
	Create(p *models.Plugin) *models.Plugin
	Update(p *models.Plugin) (*models.Plugin, bool)
	Delete(id string) bool
	AddVersion(v *models.PluginVersion)
	Versions(pluginID string) []*models.PluginVersion
}

// MemoryStore is an in-memory implementation of PluginStore.
type MemoryStore struct {
	mu       sync.RWMutex
	plugins  map[string]*models.Plugin
	versions map[string][]*models.PluginVersion
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		plugins:  make(map[string]*models.Plugin),
		versions: make(map[string][]*models.PluginVersion),
	}
}

// List returns all plugins.
func (m *MemoryStore) List() []*models.Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		cp := *p
		out = append(out, &cp)
	}
	return out
}

// Get returns a plugin by ID.
func (m *MemoryStore) Get(id string) (*models.Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.plugins[id]
	if !ok {
		return nil, false
	}
	cp := *p
	return &cp, true
}

// Create adds a new plugin.
func (m *MemoryStore) Create(p *models.Plugin) *models.Plugin {
	if p == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}
	if p.Status == "" {
		p.Status = models.StatusPending
	}
	cp := *p
	m.plugins[p.ID] = &cp
	return p
}

// Update modifies an existing plugin.
func (m *MemoryStore) Update(p *models.Plugin) (*models.Plugin, bool) {
	if p == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.plugins[p.ID]; !ok {
		return nil, false
	}
	p.UpdatedAt = time.Now()
	cp := *p
	m.plugins[p.ID] = &cp
	return &cp, true
}

// Delete removes a plugin by ID.
func (m *MemoryStore) Delete(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.plugins[id]; !ok {
		return false
	}
	delete(m.plugins, id)
	delete(m.versions, id)
	return true
}

// AddVersion adds a version entry to a plugin's history.
func (m *MemoryStore) AddVersion(v *models.PluginVersion) {
	if v == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if v.ReleasedAt.IsZero() {
		v.ReleasedAt = time.Now()
	}
	m.versions[v.PluginID] = append(m.versions[v.PluginID], v)
}

// Versions returns the version history for a plugin.
func (m *MemoryStore) Versions(pluginID string) []*models.PluginVersion {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hist := m.versions[pluginID]
	out := make([]*models.PluginVersion, len(hist))
	for i, v := range hist {
		cp := *v
		out[i] = &cp
	}
	return out
}
