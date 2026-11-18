package cost

// memory_budget_store.go — BudgetStore 的进程内实现（issue #98 接线）。
//
// 此前 NewBudgetManager() 不传 store ⇒ bm.store=nil ⇒ 预算与告警只存在 BudgetManager
// 的 map 里、无统一持久化面。本实现让预算/告警走 BudgetStore 接口，
// 为后续替换为 SQL 后端留出接缝（接口不变，换实现即可）。
//
// 注意：这是进程内实现，进程重启数据仍会丢；它解决的是"接口写了没人接"，
// 不是跨副本共享。跨副本/持久化需换用 SQL 实现（同一 BudgetStore 接口）。

import (
	"context"
	"sort"
	"sync"
)

// MemoryBudgetStore 线程安全的进程内 BudgetStore。
type MemoryBudgetStore struct {
	mu      sync.RWMutex
	budgets map[string]*TokenBudget
	alerts  map[string][]BudgetAlert
}

// NewMemoryBudgetStore 构造空的进程内预算存储。
func NewMemoryBudgetStore() *MemoryBudgetStore {
	return &MemoryBudgetStore{
		budgets: make(map[string]*TokenBudget),
		alerts:  make(map[string][]BudgetAlert),
	}
}

// compile-time 接口校验。
var _ BudgetStore = (*MemoryBudgetStore)(nil)

func (s *MemoryBudgetStore) GetBudget(ctx context.Context, tenantID string) (*TokenBudget, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.budgets[tenantID]
	if !ok {
		return nil, nil
	}
	cp := *b
	return &cp, nil
}

func (s *MemoryBudgetStore) UpsertBudget(ctx context.Context, b *TokenBudget) error {
	if b == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *b
	s.budgets[b.TenantID] = &cp
	return nil
}

func (s *MemoryBudgetStore) ListBudgets(ctx context.Context) ([]*TokenBudget, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*TokenBudget, 0, len(s.budgets))
	for _, b := range s.budgets {
		cp := *b
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TenantID < out[j].TenantID })
	return out, nil
}

func (s *MemoryBudgetStore) RecordAlert(ctx context.Context, alert BudgetAlert) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alerts[alert.TenantID] = append(s.alerts[alert.TenantID], alert)
	return nil
}

func (s *MemoryBudgetStore) ListAlerts(ctx context.Context, tenantID string, limit int) ([]BudgetAlert, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := s.alerts[tenantID]
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:] // 返回最近的 limit 条
	}
	out := make([]BudgetAlert, len(all))
	copy(out, all)
	return out, nil
}
