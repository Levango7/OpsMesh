package cost

import (
	"context"
	"fmt"
	"time"
)

func validateBudgetConsumption(b *TokenBudget, tokens int) error {
	if tokens < 0 {
		return fmt.Errorf("tokens must not be negative: %d", tokens)
	}
	if b.UsedToday < 0 || b.UsedMonth < 0 {
		return fmt.Errorf("stored budget usage must not be negative")
	}
	maxInt := int(^uint(0) >> 1)
	if tokens > maxInt-b.UsedToday || tokens > maxInt-b.UsedMonth {
		return fmt.Errorf("budget usage overflow")
	}
	return nil
}

func (s *MemoryBudgetStore) mutateBudget(tenantID string, mutate func(time.Time, *TokenBudget) error) (*TokenBudget, error) {
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
	if err := mutate(now, b); err != nil {
		return nil, err
	}
	s.budgets[tenantID] = b
	return cloneBudget(b), nil
}

// ConsumeIfUnderLimit 在同一把互斥锁内完成窗口重置、限额检查与累加。
// 语义与 SQL 实现一致：本次超限时不累加，返回 allowed=false。
func (s *MemoryBudgetStore) ConsumeIfUnderLimit(ctx context.Context, tenantID string, tokens int) (bool, BudgetUsage, error) {
	allowed := true
	b, err := s.mutateBudget(tenantID, func(_ time.Time, b *TokenBudget) error {
		if err := validateBudgetConsumption(b, tokens); err != nil {
			return err
		}
		if b.DailyLimit > 0 && b.UsedToday+tokens > b.DailyLimit ||
			b.MonthlyLimit > 0 && b.UsedMonth+tokens > b.MonthlyLimit {
			allowed = false
			return nil
		}
		b.UsedToday += tokens
		b.UsedMonth += tokens
		return nil
	})
	if err != nil {
		return false, BudgetUsage{}, err
	}
	return allowed, budgetUsageOf(b), nil
}

func (s *MemoryBudgetStore) AddUsage(ctx context.Context, tenantID string, tokens int) (*TokenBudget, []BudgetAlert, error) {
	var alerts []BudgetAlert
	b, err := s.mutateBudget(tenantID, func(now time.Time, b *TokenBudget) error {
		if err := validateBudgetConsumption(b, tokens); err != nil {
			return err
		}
		b.UsedToday += tokens
		b.UsedMonth += tokens
		alerts = b.checkAlertLocked(now)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return b, alerts, nil
}

func (s *MemoryBudgetStore) UpdateBudgetLimits(ctx context.Context, tenantID string, daily, monthly int, threshold float64) (*TokenBudget, error) {
	return s.mutateBudget(tenantID, func(_ time.Time, b *TokenBudget) error {
		b.DailyLimit = daily
		b.MonthlyLimit = monthly
		b.AlertThreshold = threshold
		return nil
	})
}

func (s *MemoryBudgetStore) UpdateBudgetTimezone(ctx context.Context, tenantID, timezone string) (*TokenBudget, error) {
	if timezone == "" {
		timezone = "UTC"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid budget timezone %q: %w", timezone, err)
	}
	return s.mutateBudget(tenantID, func(now time.Time, b *TokenBudget) error {
		b.Timezone = loc.String()
		// 切换窗口时区保留当前用量，只重锚窗口起点；反复切换不能清零套利。
		b.LastReset = dayStart(now, loc)
		b.MonthReset = monthStart(now, loc)
		return nil
	})
}

// budgetUsageOf 抽取一次消费后的窗口用量，避免调用方再读可变预算结构。
func budgetUsageOf(b *TokenBudget) BudgetUsage {
	return BudgetUsage{
		UsedToday: b.UsedToday, UsedMonth: b.UsedMonth,
		LastReset: b.LastReset, MonthReset: b.MonthReset, Timezone: b.Timezone,
	}
}
