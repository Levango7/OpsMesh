package cost

import (
	"context"
	"fmt"
	"time"
)

// ConsumeIfUnderLimit 在同一把互斥锁内完成窗口重置、限额检查与累加。
// 语义与 SQL 实现一致：本次会超限时不累加，返回 allowed=false。
func (s *MemoryBudgetStore) ConsumeIfUnderLimit(ctx context.Context, tenantID string, tokens int) (bool, BudgetUsage, error) {
	if tokens < 0 {
		return false, BudgetUsage{}, fmt.Errorf("tokens must not be negative: %d", tokens)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.budgets[tenantID]
	if !ok {
		b = &TokenBudget{
			TenantID: tenantID, DailyLimit: 100000, MonthlyLimit: 1000000,
			AlertThreshold: 0.8, Timezone: "UTC", Alerted: make(map[string]bool),
		}
	}
	now := time.Now()
	b.normalizeLocked(now)
	if b.DailyLimit > 0 && b.UsedToday+tokens > b.DailyLimit ||
		b.MonthlyLimit > 0 && b.UsedMonth+tokens > b.MonthlyLimit {
		return false, budgetUsageOf(b), nil
	}
	b.UsedToday += tokens
	b.UsedMonth += tokens
	s.budgets[tenantID] = b
	return true, budgetUsageOf(b), nil
}

// budgetUsageOf 抽取一次消费后的窗口用量，避免调用方再读可变预算结构。
func budgetUsageOf(b *TokenBudget) BudgetUsage {
	return BudgetUsage{
		UsedToday: b.UsedToday, UsedMonth: b.UsedMonth,
		LastReset: b.LastReset, MonthReset: b.MonthReset, Timezone: b.Timezone,
	}
}
