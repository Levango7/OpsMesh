package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/storefail"
)

func validateSQLBudgetConsumption(b *TokenBudget, tokens int) error {
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

// mutateSQLBudget 在 REPEATABLE READ + FOR UPDATE 事务中基于最新行修改预算。
// 用量、配置与窗口都在同一行锁内读取和写回，调用方的陈旧快照不会成为写入源。
func (s *SQLStore) mutateSQLBudget(ctx context.Context, tenantID string, mutate func(time.Time, *TokenBudget) error) (*TokenBudget, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	ctx = s.budgetCtx(ctx)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	b, err := s.budgetForUpdate(ctx, tx, tenantID)
	if err != nil {
		storefail.Record("[store] 读取预算行锁失败 (tenant=%s): %v", tenantID, err)
		return nil, err
	}
	if b == nil {
		b = &TokenBudget{
			TenantID: tenantID, DailyLimit: 100000, MonthlyLimit: 1000000,
			AlertThreshold: 0.8, Timezone: "UTC", Alerted: make(map[string]bool),
		}
		if err := s.insertDefaultBudget(ctx, tx, b); err != nil {
			storefail.Record("[store] 初始化预算失败 (tenant=%s): %v", tenantID, err)
			return nil, err
		}
		b, err = s.budgetForUpdate(ctx, tx, tenantID)
		if err != nil || b == nil {
			storefail.Record("[store] 重读预算失败 (tenant=%s): %v", tenantID, err)
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("budget row not found after insert (tenant=%s)", tenantID)
		}
	}

	now := time.Now()
	normalizeBudgetWindow(now, b)
	if err := mutate(now, b); err != nil {
		return nil, err
	}
	alertedJSON := ""
	if b.Alerted != nil {
		buf, err := json.Marshal(b.Alerted)
		if err != nil {
			return nil, err
		}
		alertedJSON = string(buf)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE budgets SET daily_limit=?, monthly_limit=?, used_today=?, used_month=?, alert_threshold=?, last_reset=?, timezone=?, month_reset=?, alerted=?, updated_at=? WHERE tenant_id=?`,
		b.DailyLimit, b.MonthlyLimit, b.UsedToday, b.UsedMonth, b.AlertThreshold,
		b.LastReset.UTC(), b.Timezone, b.MonthReset.UTC(), alertedJSON, now.UTC(), tenantID); err != nil {
		storefail.Record("[store] 更新预算失败 (tenant=%s): %v", tenantID, err)
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		storefail.Record("[store] 提交预算事务失败 (tenant=%s): %v", tenantID, err)
		return nil, err
	}
	return b, nil
}

// ConsumeIfUnderLimit 把「窗口重置 → 判额 → 累加」收敛为单个事务。
// allowed=false 时不累加 tokens，但会提交已经发生的时间窗归一。
func (s *SQLStore) ConsumeIfUnderLimit(ctx context.Context, tenantID string, tokens int) (bool, BudgetUsage, error) {
	allowed := true
	b, err := s.mutateSQLBudget(ctx, tenantID, func(_ time.Time, b *TokenBudget) error {
		if err := validateSQLBudgetConsumption(b, tokens); err != nil {
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

func checkSQLBudgetAlerts(now time.Time, b *TokenBudget) []BudgetAlert {
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

// AddUsage 记录已经发生的真实用量，越限也必须入账；告警去重在同一行锁内完成。
func (s *SQLStore) AddUsage(ctx context.Context, tenantID string, tokens int) (*TokenBudget, []BudgetAlert, error) {
	var alerts []BudgetAlert
	b, err := s.mutateSQLBudget(ctx, tenantID, func(now time.Time, b *TokenBudget) error {
		if err := validateSQLBudgetConsumption(b, tokens); err != nil {
			return err
		}
		b.UsedToday += tokens
		b.UsedMonth += tokens
		alerts = checkSQLBudgetAlerts(now, b)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return b, alerts, nil
}

func (s *SQLStore) UpdateBudgetLimits(ctx context.Context, tenantID string, daily, monthly int, threshold float64) (*TokenBudget, error) {
	return s.mutateSQLBudget(ctx, tenantID, func(_ time.Time, b *TokenBudget) error {
		b.DailyLimit = daily
		b.MonthlyLimit = monthly
		b.AlertThreshold = threshold
		return nil
	})
}

func (s *SQLStore) UpdateBudgetTimezone(ctx context.Context, tenantID, timezone string) (*TokenBudget, error) {
	if timezone == "" {
		timezone = "UTC"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid budget timezone %q: %w", timezone, err)
	}
	return s.mutateSQLBudget(ctx, tenantID, func(now time.Time, b *TokenBudget) error {
		b.Timezone = loc.String()
		// 切换窗口时区保留当前用量，只重锚窗口起点；反复切换不能清零套利。
		b.LastReset = budgetDayStart(now, loc)
		b.MonthReset = budgetMonthStart(now, loc)
		return nil
	})
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
		var flags map[string]bool
		if err := json.Unmarshal([]byte(alerted.String), &flags); err == nil && flags != nil {
			b.Alerted = flags
		}
	}
	return &b, nil
}

// insertDefaultBudget 用方言幂等 upsert 插入默认预算，处理并发首插。
func (s *SQLStore) insertDefaultBudget(ctx context.Context, tx *sql.Tx, b *TokenBudget) error {
	now := time.Now().UTC()
	loc := budgetLocation(b)
	day := budgetDayStart(now, loc)
	month := budgetMonthStart(now, loc)
	// 并发首插只接受「不存在则插入」：若另一事务已建行，必须保留其
	// 额度与已入账用量，不能用默认行做全字段覆盖。
	query := s.dialect.InsertIgnore(
		"budgets",
		[]string{"tenant_id", "daily_limit", "monthly_limit", "used_today", "used_month", "alert_threshold", "last_reset", "timezone", "month_reset", "alerted", "updated_at"},
	)
	_, err := tx.ExecContext(ctx, query, //nolint:gosec // SQL 由固定表/列元数据生成，所有业务值均使用占位符
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
		clearSQLBudgetAlerts(b, "daily:")
	}
	month := budgetMonthStart(now, loc)
	if b.MonthReset.IsZero() {
		b.MonthReset = month
	} else if month.After(b.MonthReset) {
		b.UsedMonth = 0
		b.MonthReset = month
		clearSQLBudgetAlerts(b, "monthly:")
	}
}

func clearSQLBudgetAlerts(b *TokenBudget, prefix string) {
	for key := range b.Alerted {
		if strings.HasPrefix(key, prefix) {
			delete(b.Alerted, key)
		}
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
