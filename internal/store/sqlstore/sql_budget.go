package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Levango7/OpsMesh/internal/store/storefail"
)

func (s *SQLStore) budgetCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// GetBudget 返回租户预算（不存在返回 nil + nil error）。
func (s *SQLStore) GetBudget(ctx context.Context, tenantID string) (*TokenBudget, error) {
	ctx = s.budgetCtx(ctx)
	if tenantID == "" {
		return nil, nil
	}
	if s.db == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var b TokenBudget
	var lastReset, monthReset sql.NullTime
	var alerted sql.NullString
	var tz sql.NullString
	var daily, monthly, usedToday, usedMonth int
	var threshold float64
	err := s.db.QueryRowContext(ctx,
		`SELECT daily_limit, monthly_limit, used_today, used_month, alert_threshold, last_reset, timezone, month_reset, alerted FROM budgets WHERE tenant_id=?`,
		tenantID).Scan(&daily, &monthly, &usedToday, &usedMonth, &threshold, &lastReset, &tz, &monthReset, &alerted)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		storefail.Record("[store] GetBudget 查询失败 (tenant=%s): %v", tenantID, err)
		return nil, nil
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
	if alerted.Valid && alerted.String != "" {
		var m map[string]bool
		if err := json.Unmarshal([]byte(alerted.String), &m); err == nil {
			b.Alerted = m
		}
	}
	if b.Alerted == nil {
		b.Alerted = make(map[string]bool)
	}
	return &b, nil
}

// ListBudgets 返回全部租户预算（跨租户视图）。
func (s *SQLStore) ListBudgets(ctx context.Context) ([]*TokenBudget, error) {
	ctx = s.budgetCtx(ctx)
	if s.db == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		`SELECT tenant_id, daily_limit, monthly_limit, used_today, used_month, alert_threshold, last_reset, timezone, month_reset, alerted FROM budgets`)
	if err != nil {
		storefail.Record("[store] ListBudgets 查询失败: %v", err)
		return nil, nil
	}
	defer rows.Close()
	out := make([]*TokenBudget, 0)
	for rows.Next() {
		var b TokenBudget
		var tenantID string
		var lastReset, monthReset sql.NullTime
		var alerted sql.NullString
		var tz sql.NullString
		var daily, monthly, usedToday, usedMonth int
		var threshold float64
		if err := rows.Scan(&tenantID, &daily, &monthly, &usedToday, &usedMonth, &threshold, &lastReset, &tz, &monthReset, &alerted); err != nil {
			storefail.Record("[store] ListBudgets 扫描失败: %v", err)
			continue
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
		if alerted.Valid && alerted.String != "" {
			var m map[string]bool
			if err := json.Unmarshal([]byte(alerted.String), &m); err == nil {
				b.Alerted = m
			}
		}
		if b.Alerted == nil {
			b.Alerted = make(map[string]bool)
		}
		cp := b
		out = append(out, &cp)
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] ListBudgets 迭代失败: %v", err)
	}
	return out, nil
}

// UpsertBudget 按 tenantID 幂等 upsert 预算。
func (s *SQLStore) UpsertBudget(ctx context.Context, b *TokenBudget) error {
	ctx = s.budgetCtx(ctx)
	if b == nil || b.TenantID == "" {
		return fmt.Errorf("tenantID must not be empty")
	}
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	alertedJSON := ""
	if b.Alerted != nil {
		if buf, err := json.Marshal(b.Alerted); err == nil {
			alertedJSON = string(buf)
		}
	}
	var alertedArg interface{}
	if alertedJSON != "" {
		alertedArg = alertedJSON
	}
	var lastResetArg, monthResetArg interface{}
	if !b.LastReset.IsZero() {
		lastResetArg = b.LastReset.UTC()
	}
	if !b.MonthReset.IsZero() {
		monthResetArg = b.MonthReset.UTC()
	}
	tz := b.Timezone
	if tz == "" {
		tz = "UTC"
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO budgets (tenant_id, daily_limit, monthly_limit, used_today, used_month, alert_threshold, last_reset, timezone, month_reset, alerted, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE daily_limit=VALUES(daily_limit), monthly_limit=VALUES(monthly_limit),
		   used_today=VALUES(used_today), used_month=VALUES(used_month), alert_threshold=VALUES(alert_threshold),
		   last_reset=VALUES(last_reset), timezone=VALUES(timezone), month_reset=VALUES(month_reset),
		   alerted=VALUES(alerted), updated_at=VALUES(updated_at)`,
		b.TenantID, b.DailyLimit, b.MonthlyLimit, b.UsedToday, b.UsedMonth, b.AlertThreshold, lastResetArg, tz, monthResetArg, alertedArg, now)
	if err != nil {
		storefail.Record("[store] UpsertBudget 失败 (tenant=%s): %v", b.TenantID, err)
		return err
	}
	return nil
}

// RecordAlert 记录一条预算告警。
func (s *SQLStore) RecordAlert(ctx context.Context, alert BudgetAlert) error {
	ctx = s.budgetCtx(ctx)
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ts := alert.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO budget_alerts (tenant_id, type, threshold, used, limit_val, timestamp) VALUES (?, ?, ?, ?, ?, ?)`,
		alert.TenantID, alert.Type, alert.Threshold, alert.Used, alert.Limit, ts.UTC())
	if err != nil {
		storefail.Record("[store] RecordAlert 失败 (tenant=%s): %v", alert.TenantID, err)
		return err
	}
	return nil
}

// ListAlerts 返回指定租户的预算告警（tenantID 为空返回全部），limit<=0 不限制。
func (s *SQLStore) ListAlerts(ctx context.Context, tenantID string, limit int) ([]BudgetAlert, error) {
	ctx = s.budgetCtx(ctx)
	if s.db == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	query := `SELECT tenant_id, type, threshold, used, limit_val, timestamp FROM budget_alerts`
	args := make([]interface{}, 0)
	if tenantID != "" {
		query += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	query += ` ORDER BY timestamp ASC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		storefail.Record("[store] ListAlerts 查询失败: %v", err)
		return nil, nil
	}
	defer rows.Close()
	out := make([]BudgetAlert, 0)
	for rows.Next() {
		var a BudgetAlert
		var ts time.Time
		if err := rows.Scan(&a.TenantID, &a.Type, &a.Threshold, &a.Used, &a.Limit, &ts); err != nil {
			storefail.Record("[store] ListAlerts 扫描失败: %v", err)
			continue
		}
		a.Timestamp = ts
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		storefail.Record("[store] ListAlerts 迭代失败: %v", err)
	}
	return out, nil
}
