package store

import (
	"errors"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/models"
)

// AutoscalerStore is the interface for autoscaler persistence.
// AutoscalerStore 的每个读/改方法显式带租户（D4）：漏传即编译不过。
type AutoscalerStore interface {
	// Scaling rule operations
	CreateRule(rule *models.ScaleRule) (*models.ScaleRule, error)
	GetRule(id, tenantID string) (*models.ScaleRule, bool)
	UpdateRule(rule *models.ScaleRule, tenantID string) error
	DeleteRule(id, tenantID string) bool
	ListRules(tenantID string) []*models.ScaleRule

	// Scaling decision operations
	CreateDecision(d *models.ScaleDecision) (*models.ScaleDecision, error)
	ListDecisions(tenantID, ruleID string, limit int) []*models.ScaleDecision
}

// ErrTenantMismatch：跨租户访问（读/改/删/决策）统一返回它——调用方据此 404/403。
var ErrTenantMismatch = errors.New("tenant mismatch")

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

// CreateRule 空租户拒绝写入（无主数据不落库）；跨租户同 ID 不覆盖。
func (m *MemoryStore) CreateRule(rule *models.ScaleRule) (*models.ScaleRule, error) {
	if rule == nil {
		return nil, nil
	}
	if rule.TenantID == "" {
		return nil, ErrTenantMismatch
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

// GetRule 按 id + 租户取；跨租户与不存在同样返回 false。
func (m *MemoryStore) GetRule(id, tenantID string) (*models.ScaleRule, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rules[id]
	if !ok || r.TenantID != tenantID {
		return nil, false
	}
	return r, true
}

// UpdateRule 跨租户不可改（返回 ErrTenantMismatch）；且不允许改租户。
func (m *MemoryStore) UpdateRule(rule *models.ScaleRule, tenantID string) error {
	if rule == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.rules[rule.ID]
	if !ok {
		return nil
	}
	if existing.TenantID != tenantID {
		return ErrTenantMismatch
	}
	rule.TenantID = existing.TenantID
	rule.UpdatedAt = time.Now()
	m.rules[rule.ID] = rule
	return nil
}

// DeleteRule 跨租户删不掉。
func (m *MemoryStore) DeleteRule(id, tenantID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.rules[id]
	if !ok || existing.TenantID != tenantID {
		return false
	}
	delete(m.rules, id)
	return true
}

// ListRules 只返回该租户的规则。
func (m *MemoryStore) ListRules(tenantID string) []*models.ScaleRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.ScaleRule, 0)
	for _, r := range m.rules {
		if r.TenantID != tenantID {
			continue
		}
		out = append(out, r)
	}
	return out
}

// CreateDecision 归属由父规则决定：父规则不属于该租户 ⇒ 拒收。
func (m *MemoryStore) CreateDecision(d *models.ScaleDecision) (*models.ScaleDecision, error) {
	if d == nil {
		return nil, nil
	}
	if rb, ok := m.rules[d.RuleID]; !ok || rb.TenantID != d.TenantID {
		return nil, ErrTenantMismatch
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if d.Timestamp.IsZero() {
		d.Timestamp = time.Now()
	}
	m.decisions = append(m.decisions, d)
	return d, nil
}

// ListDecisions 只返回该租户的决策（可按规则再收窄）。
func (m *MemoryStore) ListDecisions(tenantID, ruleID string, limit int) []*models.ScaleDecision {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.ScaleDecision, 0)
	for i := len(m.decisions) - 1; i >= 0; i-- {
		d := m.decisions[i]
		if d.TenantID != tenantID {
			continue
		}
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
