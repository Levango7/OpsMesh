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
	return cloneMemoryBudget(b), nil
}

// UpsertBudget 按 tenantID 幂等 upsert 预算。
func (m *MemoryStore) UpsertBudget(ctx context.Context, b *TokenBudget) error {
	if b == nil || b.TenantID == "" {
		return fmt.Errorf("tenantID must not be empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.budgets[b.TenantID] = cloneMemoryBudget(b)
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
		out = append(out, cloneMemoryBudget(b))
	}
	return out, nil
}

// RecordAlert 记录一条预算告警。
func (m *MemoryStore) RecordAlert(ctx context.Context, alert BudgetAlert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.budgetAlerts = append(m.budgetAlerts, alert)
	if b := m.budgets[alert.TenantID]; b != nil {
		if b.Alerted == nil {
			b.Alerted = make(map[string]bool)
		}
		b.Alerted[fmt.Sprintf("%s:%g", alert.Type, alert.Threshold)] = true
	}
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
