package memory

import (
	"context"
	"fmt"
	"time"
)

// ConsumeIfUnderLimit 在 MemoryStore 同一把锁内完成窗口重置、限额检查与累加。
// 语义与 SQL 实现一致：本次会超限时不累加，返回 allowed=false。
func (m *MemoryStore) ConsumeIfUnderLimit(ctx context.Context, tenantID string, tokens int) (bool, BudgetUsage, error) {
	if tokens < 0 {
		return false, BudgetUsage{}, fmt.Errorf("tokens must not be negative: %d", tokens)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	b, ok := m.budgets[tenantID]
	if !ok {
		b = &TokenBudget{
			TenantID: tenantID, DailyLimit: 100000, MonthlyLimit: 1000000,
			AlertThreshold: 0.8, Timezone: "UTC", Alerted: make(map[string]bool),
		}
	}
	now := time.Now()
	normalizeMemoryBudgetWindow(now, b)
	if b.DailyLimit > 0 && b.UsedToday+tokens > b.DailyLimit ||
		b.MonthlyLimit > 0 && b.UsedMonth+tokens > b.MonthlyLimit {
		return false, memoryBudgetUsageOf(b), nil
	}
	b.UsedToday += tokens
	b.UsedMonth += tokens
	m.budgets[tenantID] = b
	return true, memoryBudgetUsageOf(b), nil
}

// memoryBudgetUsageOf 抽取消费后的窗口用量，避免调用方再读可变预算结构。
func memoryBudgetUsageOf(b *TokenBudget) BudgetUsage {
	return BudgetUsage{
		UsedToday: b.UsedToday, UsedMonth: b.UsedMonth,
		LastReset: b.LastReset, MonthReset: b.MonthReset, Timezone: b.Timezone,
	}
}

// normalizeMemoryBudgetWindow 与 cost.TokenBudget.normalizeLocked 同语义：
// 日窗口跨天清零 usedToday，月窗口跨月清零 usedMonth。
func normalizeMemoryBudgetWindow(now time.Time, b *TokenBudget) {
	loc := memoryBudgetLocation(b)
	day := memoryBudgetDayStart(now, loc)
	if b.LastReset.IsZero() || day.After(b.LastReset) {
		b.UsedToday = 0
		b.LastReset = day
	}
	month := memoryBudgetMonthStart(now, loc)
	if b.MonthReset.IsZero() {
		b.MonthReset = month
	} else if month.After(b.MonthReset) {
		b.UsedMonth = 0
		b.MonthReset = month
	}
}

func memoryBudgetLocation(b *TokenBudget) *time.Location {
	if b.Timezone == "" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(b.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

func memoryBudgetDayStart(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func memoryBudgetMonthStart(t time.Time, loc *time.Location) time.Time {
	y, m, _ := t.In(loc).Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, loc)
}
