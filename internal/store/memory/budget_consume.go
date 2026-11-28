package memory

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func validateMemoryBudgetConsumption(b *TokenBudget, tokens int) error {
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

func cloneMemoryBudget(b *TokenBudget) *TokenBudget {
	if b == nil {
		return nil
	}
	cp := *b
	if b.Alerted != nil {
		cp.Alerted = make(map[string]bool, len(b.Alerted))
		for k, v := range b.Alerted {
			cp.Alerted[k] = v
		}
	}
	return &cp
}

func (m *MemoryStore) mutateBudget(tenantID string, mutate func(time.Time, *TokenBudget) error) (*TokenBudget, error) {
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
	if err := mutate(now, b); err != nil {
		return nil, err
	}
	m.budgets[tenantID] = b
	return cloneMemoryBudget(b), nil
}

// ConsumeIfUnderLimit 在 MemoryStore 同一把锁内完成窗口重置、限额检查与累加。
// 语义与 SQL 实现一致：本次超限时不累加，返回 allowed=false。
func (m *MemoryStore) ConsumeIfUnderLimit(ctx context.Context, tenantID string, tokens int) (bool, BudgetUsage, error) {
	allowed := true
	b, err := m.mutateBudget(tenantID, func(_ time.Time, b *TokenBudget) error {
		if err := validateMemoryBudgetConsumption(b, tokens); err != nil {
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
	return allowed, memoryBudgetUsageOf(b), nil
}

func checkMemoryBudgetAlerts(now time.Time, b *TokenBudget) []BudgetAlert {
	var out []BudgetAlert
	collect := func(alertType string, used, limit int) {
		if limit <= 0 || float64(used)/float64(limit) < b.AlertThreshold {
			return
		}
		key := fmt.Sprintf("%s:%g", alertType, b.AlertThreshold)
		if b.Alerted[key] {
			return
		}
		if b.Alerted == nil {
			b.Alerted = make(map[string]bool)
		}
		b.Alerted[key] = true
		out = append(out, BudgetAlert{TenantID: b.TenantID, Type: alertType, Threshold: b.AlertThreshold, Used: used, Limit: limit, Timestamp: now})
	}
	collect("daily", b.UsedToday, b.DailyLimit)
	collect("monthly", b.UsedMonth, b.MonthlyLimit)
	return out
}

func (m *MemoryStore) AddUsage(ctx context.Context, tenantID string, tokens int) (*TokenBudget, []BudgetAlert, error) {
	var alerts []BudgetAlert
	b, err := m.mutateBudget(tenantID, func(now time.Time, b *TokenBudget) error {
		if err := validateMemoryBudgetConsumption(b, tokens); err != nil {
			return err
		}
		b.UsedToday += tokens
		b.UsedMonth += tokens
		alerts = checkMemoryBudgetAlerts(now, b)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return b, alerts, nil
}

func (m *MemoryStore) UpdateBudgetLimits(ctx context.Context, tenantID string, daily, monthly int, threshold float64) (*TokenBudget, error) {
	return m.mutateBudget(tenantID, func(_ time.Time, b *TokenBudget) error {
		b.DailyLimit = daily
		b.MonthlyLimit = monthly
		b.AlertThreshold = threshold
		return nil
	})
}

func (m *MemoryStore) UpdateBudgetTimezone(ctx context.Context, tenantID, timezone string) (*TokenBudget, error) {
	if timezone == "" {
		timezone = "UTC"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid budget timezone %q: %w", timezone, err)
	}
	return m.mutateBudget(tenantID, func(now time.Time, b *TokenBudget) error {
		b.Timezone = loc.String()
		b.LastReset = memoryBudgetDayStart(now, loc)
		b.MonthReset = memoryBudgetMonthStart(now, loc)
		return nil
	})
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
		clearMemoryBudgetAlerts(b, "daily:")
	}
	month := memoryBudgetMonthStart(now, loc)
	if b.MonthReset.IsZero() {
		b.MonthReset = month
	} else if month.After(b.MonthReset) {
		b.UsedMonth = 0
		b.MonthReset = month
		clearMemoryBudgetAlerts(b, "monthly:")
	}
}

func clearMemoryBudgetAlerts(b *TokenBudget, prefix string) {
	for key := range b.Alerted {
		if strings.HasPrefix(key, prefix) {
			delete(b.Alerted, key)
		}
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
