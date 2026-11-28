package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/storefail"
)

// ConsumeIfUnderLimit 用 REPEATABLE READ + FOR UPDATE 把「窗口重置 → 判额 → 累加」
// 收敛为单个事务。行锁保证同一租户同一时刻只有一个请求能完成读-判-写，
// 并发/多副本下不会丢失更新或穿透硬限。存储故障返回 err，由调用方 fail-closed。
func (s *SQLStore) ConsumeIfUnderLimit(ctx context.Context, tenantID string, tokens int) (bool, BudgetUsage, error) {
	if tokens < 0 {
		return false, BudgetUsage{}, fmt.Errorf("tokens must not be negative: %d", tokens)
	}
	if s.db == nil {
		return false, BudgetUsage{}, fmt.Errorf("database not available")
	}
	ctx = s.budgetCtx(ctx)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return false, BudgetUsage{}, err
	}
	defer func() { _ = tx.Rollback() }()

	b, err := s.budgetForUpdate(ctx, tx, tenantID)
	if err != nil {
		storefail.Record("[store] ConsumeIfUnderLimit 读取预算失败 (tenant=%s): %v", tenantID, err)
		return false, BudgetUsage{}, err
	}
	if b == nil {
		// 无配置时沿用 BudgetManager 的默认额度，插入后重新加行锁读取。
		b = &TokenBudget{
			TenantID: tenantID, DailyLimit: 100000, MonthlyLimit: 1000000,
			AlertThreshold: 0.8, Timezone: "UTC", Alerted: make(map[string]bool),
		}
		if err := s.insertDefaultBudget(ctx, tx, b); err != nil {
			storefail.Record("[store] ConsumeIfUnderLimit 初始化预算失败 (tenant=%s): %v", tenantID, err)
			return false, BudgetUsage{}, err
		}
		b, err = s.budgetForUpdate(ctx, tx, tenantID)
		if err != nil {
			storefail.Record("[store] ConsumeIfUnderLimit 重读预算失败 (tenant=%s): %v", tenantID, err)
			return false, BudgetUsage{}, err
		}
		if b == nil {
			return false, BudgetUsage{}, fmt.Errorf("budget row not found after insert (tenant=%s)", tenantID)
		}
	}

	now := time.Now()
	normalizeBudgetWindow(now, b)
	if b.DailyLimit > 0 && b.UsedToday+tokens > b.DailyLimit ||
		b.MonthlyLimit > 0 && b.UsedMonth+tokens > b.MonthlyLimit {
		return false, budgetUsageOf(b), nil
	}
	b.UsedToday += tokens
	b.UsedMonth += tokens
	if _, err := tx.ExecContext(ctx,
		`UPDATE budgets SET used_today=?, used_month=?, last_reset=?, month_reset=?, timezone=?, updated_at=? WHERE tenant_id=?`,
		b.UsedToday, b.UsedMonth, b.LastReset.UTC(), b.MonthReset.UTC(), b.Timezone, now.UTC(), tenantID); err != nil {
		storefail.Record("[store] ConsumeIfUnderLimit 更新预算失败 (tenant=%s): %v", tenantID, err)
		return false, BudgetUsage{}, err
	}
	if err := tx.Commit(); err != nil {
		storefail.Record("[store] ConsumeIfUnderLimit 提交失败 (tenant=%s): %v", tenantID, err)
		return false, BudgetUsage{}, err
	}
	return true, budgetUsageOf(b), nil
}

// budgetForUpdate 在事务内读取并锁定租户预算行。
func (s *SQLStore) budgetForUpdate(ctx context.Context, tx *sql.Tx, tenantID string) (*TokenBudget, error) {
	var b TokenBudget
	var lastReset, monthReset sql.NullTime
	var alerted, tz sql.NullString
	var daily, monthly, usedToday, usedMonth int
	var threshold float64
	err := tx.QueryRowContext(ctx,
		`SELECT daily_limit, monthly_limit, used_today, used_month, alert_threshold, last_reset, timezone, month_reset, alerted FROM budgets WHERE tenant_id=? FOR UPDATE`,
		tenantID).Scan(&daily, &monthly, &usedToday, &usedMonth, &threshold, &lastReset, &tz, &monthReset, &alerted)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.TenantID = tenantID
	b.DailyLimit = daily
	b.MonthlyLimit = monthly
	b.UsedToday = usedToday
	b.UsedMonth = usedMonth
	b.AlertThreshold = threshold
	if lastReset.Valid {
		b.LastReset = lastReset.Time
	}
	if monthReset.Valid {
		b.MonthReset = monthReset.Time
	}
	if tz.Valid && tz.String != "" {
		b.Timezone = tz.String
	} else {
		b.Timezone = "UTC"
	}
	b.Alerted = make(map[string]bool)
	if alerted.Valid && alerted.String != "" {
		// 告警标记仅作读路径兼容；消费事务不改它，避免与用量更新混成 RMW。
		_ = alerted
	}
	return &b, nil
}

// insertDefaultBudget 用方言幂等 upsert 插入默认预算，处理并发首插。
func (s *SQLStore) insertDefaultBudget(ctx context.Context, tx *sql.Tx, b *TokenBudget) error {
	now := time.Now().UTC()
	loc := budgetLocation(b)
	day := budgetDayStart(now, loc)
	month := budgetMonthStart(now, loc)
	query := s.dialect.Upsert(
		"budgets",
		[]string{"tenant_id", "daily_limit", "monthly_limit", "used_today", "used_month", "alert_threshold", "last_reset", "timezone", "month_reset", "alerted", "updated_at"},
		[]string{"?", "?", "?", "?", "?", "?", "?", "?", "?", "?", "?"},
		[]string{"tenant_id"},
		[]string{"daily_limit", "monthly_limit", "used_today", "used_month", "alert_threshold", "last_reset", "timezone", "month_reset", "alerted", "updated_at"},
	)
	_, err := tx.ExecContext(ctx, query,
		b.TenantID, b.DailyLimit, b.MonthlyLimit, 0, 0, b.AlertThreshold,
		day.UTC(), b.Timezone, month.UTC(), nil, now)
	return err
}

// budgetUsageOf 抽取消费后的窗口用量，避免调用方再读可变预算结构。
func budgetUsageOf(b *TokenBudget) BudgetUsage {
	return BudgetUsage{
		UsedToday: b.UsedToday, UsedMonth: b.UsedMonth,
		LastReset: b.LastReset, MonthReset: b.MonthReset, Timezone: b.Timezone,
	}
}

// normalizeBudgetWindow 与 cost.TokenBudget.normalizeLocked 同语义：
// 日窗口跨天则清零 usedToday，月窗口跨月则清零 usedMonth。
func normalizeBudgetWindow(now time.Time, b *TokenBudget) {
	loc := budgetLocation(b)
	day := budgetDayStart(now, loc)
	if b.LastReset.IsZero() || day.After(b.LastReset) {
		b.UsedToday = 0
		b.LastReset = day
	}
	month := budgetMonthStart(now, loc)
	if b.MonthReset.IsZero() {
		b.MonthReset = month
	} else if month.After(b.MonthReset) {
		b.UsedMonth = 0
		b.MonthReset = month
	}
}

func budgetLocation(b *TokenBudget) *time.Location {
	if b.Timezone == "" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(b.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

func budgetDayStart(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func budgetMonthStart(t time.Time, loc *time.Location) time.Time {
	y, m, _ := t.In(loc).Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, loc)
}
