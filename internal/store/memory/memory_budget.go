package memory

import (
	"context"
	"fmt"
)

// GetBudget 返回租户预算（不存在返回 nil + nil error）。
func (m *MemoryStore) GetBudget(ctx context.Context, tenantID string) (*TokenBudget, error) {
	if tenantID == "" {
		return nil, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.budgets[tenantID]
	if !ok || b == nil {
		return nil, nil
	}
	cp := *b
	if b.Alerted != nil {
		cp.Alerted = make(map[string]bool, len(b.Alerted))
		for k, v := range b.Alerted {
			cp.Alerted[k] = v
		}
	}
	return &cp, nil
}

// UpsertBudget 按 tenantID 幂等 upsert 预算。
func (m *MemoryStore) UpsertBudget(ctx context.Context, b *TokenBudget) error {
	if b == nil || b.TenantID == "" {
		return fmt.Errorf("tenantID must not be empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := *b
	if b.Alerted != nil {
		stored.Alerted = make(map[string]bool, len(b.Alerted))
		for k, v := range b.Alerted {
			stored.Alerted[k] = v
		}
	}
	m.budgets[b.TenantID] = &stored
	return nil
}

// ListBudgets 返回全部租户预算（跨租户视图）。
func (m *MemoryStore) ListBudgets(ctx context.Context) ([]*TokenBudget, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*TokenBudget, 0, len(m.budgets))
	for _, b := range m.budgets {
		if b == nil {
			continue
		}
		cp := *b
		if b.Alerted != nil {
			cp.Alerted = make(map[string]bool, len(b.Alerted))
			for k, v := range b.Alerted {
				cp.Alerted[k] = v
			}
		}
		out = append(out, &cp)
	}
	return out, nil
}

// RecordAlert 记录一条预算告警。
func (m *MemoryStore) RecordAlert(ctx context.Context, alert BudgetAlert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.budgetAlerts = append(m.budgetAlerts, alert)
	return nil
}

// ListAlerts 返回指定租户的预算告警（tenantID 为空返回全部），limit<=0 不限制。
func (m *MemoryStore) ListAlerts(ctx context.Context, tenantID string, limit int) ([]BudgetAlert, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]BudgetAlert, 0)
	for _, a := range m.budgetAlerts {
		if tenantID != "" && a.TenantID != tenantID {
			continue
		}
		out = append(out, a)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
