package store

import (
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/models"
)

// AutoscalerStore is the interface for autoscaler persistence.
type AutoscalerStore interface {
	// Scaling rule operations
	CreateRule(rule *models.ScaleRule) (*models.ScaleRule, error)
	GetRule(id string) (*models.ScaleRule, bool)
	UpdateRule(rule *models.ScaleRule) error
	DeleteRule(id string) bool
	ListRules() []*models.ScaleRule

	// Scaling decision operations
	CreateDecision(d *models.ScaleDecision) (*models.ScaleDecision, error)
	ListDecisions(ruleID string, limit int) []*models.ScaleDecision
}

// MemoryStore is an in-memory implementation of AutoscalerStore.
type MemoryStore struct {
	mu        sync.RWMutex
	rules     map[string]*models.ScaleRule
	decisions []*models.ScaleDecision
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		rules:     make(map[string]*models.ScaleRule),
		decisions: make([]*models.ScaleDecision, 0),
	}
}

func (m *MemoryStore) CreateRule(rule *models.ScaleRule) (*models.ScaleRule, error) {
	if rule == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = time.Now()
	}
	rule.UpdatedAt = time.Now()
	m.rules[rule.ID] = rule
	return rule, nil
}

func (m *MemoryStore) GetRule(id string) (*models.ScaleRule, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rules[id]
	return r, ok
}

func (m *MemoryStore) UpdateRule(rule *models.ScaleRule) error {
	if rule == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rules[rule.ID]; !ok {
		return nil
	}
	rule.UpdatedAt = time.Now()
	m.rules[rule.ID] = rule
	return nil
}

func (m *MemoryStore) DeleteRule(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rules[id]; !ok {
		return false
	}
	delete(m.rules, id)
	return true
}

func (m *MemoryStore) ListRules() []*models.ScaleRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.ScaleRule, 0)
	for _, r := range m.rules {
		out = append(out, r)
	}
	return out
}

func (m *MemoryStore) CreateDecision(d *models.ScaleDecision) (*models.ScaleDecision, error) {
	if d == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if d.Timestamp.IsZero() {
		d.Timestamp = time.Now()
	}
	m.decisions = append(m.decisions, d)
	return d, nil
}

func (m *MemoryStore) ListDecisions(ruleID string, limit int) []*models.ScaleDecision {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.ScaleDecision, 0)
	for i := len(m.decisions) - 1; i >= 0; i-- {
		d := m.decisions[i]
		if ruleID != "" && d.RuleID != ruleID {
			continue
		}
		out = append(out, d)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
