package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLStore is a MySQL-backed implementation of AlertStore.
type MySQLStore struct {
	db *sql.DB
}

// NewMySQLStore creates a MySQLStore with connection pool.
// dsn format: user:pass@tcp(host:port)/dbname
// If dsn is empty, returns nil (caller should fall back to MemoryStore).
func NewMySQLStore(dsn string) (*MySQLStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("empty DSN")
	}
	db, err := sql.Open("mysql", ensureParseTime(dsn))
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		log.Printf("[store] mysql ping 失败（将延迟重连）: %v", err)
	}
	if err := initSchema(db); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &MySQLStore{db: db}, nil
}

// ensureParseTime ensures parseTime=true in DSN.
func ensureParseTime(dsn string) string {
	if len(dsn) > 0 && dsn[len(dsn)-1] == '/' {
		return dsn + "?parseTime=true"
	}
	return dsn + "?parseTime=true"
}

// initSchema creates tables if they don't exist.
func initSchema(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS alerts (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			alert_id VARCHAR(64) NOT NULL,
			tenant_id VARCHAR(64) NOT NULL,
			device_id VARCHAR(64),
			agent_id VARCHAR(64),
			severity VARCHAR(16),
			message TEXT,
			metric VARCHAR(128),
			status VARCHAR(16) DEFAULT 'firing',
			acknowledged_by VARCHAR(64),
			silenced_until DATETIME,
			comment TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			UNIQUE KEY uk_alert_id (alert_id),
			INDEX idx_tenant (tenant_id),
			INDEX idx_status (status)
		)`,
		`CREATE TABLE IF NOT EXISTS alert_rules (
			id VARCHAR(64) PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			metric VARCHAR(128),
			op VARCHAR(8),
			threshold DOUBLE,
			for_duration INT,
			severity VARCHAR(16),
			message TEXT,
			enabled TINYINT(1) DEFAULT 1,
			created_at DATETIME,
			created_by VARCHAR(64),
			INDEX idx_tenant (tenant_id)
		)`,
		`CREATE TABLE IF NOT EXISTS silences (
			id VARCHAR(64) PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			match_labels JSON,
			starts_at DATETIME,
			ends_at DATETIME,
			created_by VARCHAR(64),
			reason TEXT,
			created_at DATETIME,
			INDEX idx_tenant (tenant_id)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the database connection pool.
// 资源泄漏修复：原 MySQLStore 无 Close 方法，main 退出时连接池不释放
// （进程退出兜底但优雅退出窗口内连接悬挂；长驻测试/嵌入场景持续泄漏句柄）。
// main 的 MySQL 分支成功后 defer ms.Close() 对齐 task-svc 写法。
func (m *MySQLStore) Close() error {
	return m.db.Close()
}

// Alerts returns alerts, optionally filtered by tenant.
func (m *MySQLStore) Alerts(tenantID string) []*Alert {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT alert_id, tenant_id, device_id, agent_id, severity, message, metric, status, acknowledged_by, silenced_until, comment, created_at, updated_at FROM alerts`
	var args []interface{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] Alerts 查询失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*Alert
	for rows.Next() {
		a := &Alert{}
		var createdAt, silencedUntil, updatedAt sql.NullTime
		var alertID, status, ackBy, comment sql.NullString
		if err := rows.Scan(&alertID, &a.TenantID, &a.DeviceID, &a.AgentID, &a.Severity, &a.Message, &a.Metric, &status, &ackBy, &silencedUntil, &comment, &createdAt, &updatedAt); err != nil {
			log.Printf("[store] Alerts 扫描失败: %v", err)
			continue
		}
		a.AlertID = alertID.String
		a.Status = status.String
		a.AcknowledgedBy = ackBy.String
		if silencedUntil.Valid {
			a.SilencedUntil = silencedUntil.Time
		}
		a.Comment = comment.String
		if createdAt.Valid {
			a.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			a.UpdatedAt = updatedAt.Time
		}
		out = append(out, a)
	}
	return out
}

// AddAlert adds an alert.
func (m *MySQLStore) AddAlert(a *Alert) {
	if a == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.Status == "" {
		a.Status = "firing"
	}
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO alerts (alert_id, tenant_id, device_id, agent_id, severity, message, metric, status, acknowledged_by, silenced_until, comment, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullString(a.AlertID), nullString(a.TenantID), nullString(a.DeviceID), nullString(a.AgentID),
		nullString(a.Severity), nullString(a.Message), nullString(a.Metric), nullString(a.Status),
		nullString(a.AcknowledgedBy), nullTime(a.SilencedUntil), nullString(a.Comment),
		nullTime(a.CreatedAt), nullTime(a.UpdatedAt))
	if err != nil {
		log.Printf("[store] AddAlert 失败: %v", err)
	}
}

// Alert returns an alert by ID.
func (m *MySQLStore) Alert(id string) *Alert {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT alert_id, tenant_id, device_id, agent_id, severity, message, metric, status, acknowledged_by, silenced_until, comment, created_at, updated_at FROM alerts WHERE alert_id=?`, id)
	a := &Alert{}
	var createdAt, silencedUntil, updatedAt sql.NullTime
	var alertID, status, ackBy, comment sql.NullString
	if err := row.Scan(&alertID, &a.TenantID, &a.DeviceID, &a.AgentID, &a.Severity, &a.Message, &a.Metric, &status, &ackBy, &silencedUntil, &comment, &createdAt, &updatedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] Alert 查询失败: %v", err)
		}
		return nil
	}
	a.AlertID = alertID.String
	a.Status = status.String
	a.AcknowledgedBy = ackBy.String
	if silencedUntil.Valid {
		a.SilencedUntil = silencedUntil.Time
	}
	a.Comment = comment.String
	if createdAt.Valid {
		a.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		a.UpdatedAt = updatedAt.Time
	}
	return a
}

// AckAlert acknowledges an alert.
func (m *MySQLStore) AckAlert(id, tenantID, by string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx,
		`UPDATE alerts SET status=?, acknowledged_by=?, updated_at=? WHERE alert_id=? AND (tenant_id=? OR ?='')`,
		"acknowledged", by, time.Now().UTC(), id, tenantID, tenantID)
	if err != nil {
		log.Printf("[store] AckAlert 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// SilenceAlert silences an alert.
func (m *MySQLStore) SilenceAlert(id, tenantID, by string, until time.Time, comment string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if until.IsZero() {
		until = time.Now().UTC().Add(24 * time.Hour)
	}
	res, err := m.db.ExecContext(ctx,
		`UPDATE alerts SET status=?, acknowledged_by=?, silenced_until=?, comment=?, updated_at=? WHERE alert_id=? AND (tenant_id=? OR ?='')`,
		"silenced", by, until, comment, time.Now().UTC(), id, tenantID, tenantID)
	if err != nil {
		log.Printf("[store] SilenceAlert 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// ResolveAlert resolves an alert.
func (m *MySQLStore) ResolveAlert(id, tenantID, by string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx,
		`UPDATE alerts SET status=?, acknowledged_by=?, updated_at=? WHERE alert_id=? AND (tenant_id=? OR ?='')`,
		"resolved", by, time.Now().UTC(), id, tenantID, tenantID)
	if err != nil {
		log.Printf("[store] ResolveAlert 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// CreateAlertRule creates a rule.
func (m *MySQLStore) CreateAlertRule(r *AlertRule) *AlertRule {
	if r == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO alert_rules (id, tenant_id, metric, op, threshold, for_duration, severity, message, enabled, created_at, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE tenant_id=VALUES(tenant_id), metric=VALUES(metric), op=VALUES(op),
		   threshold=VALUES(threshold), for_duration=VALUES(for_duration), severity=VALUES(severity),
		   message=VALUES(message), enabled=VALUES(enabled), created_by=VALUES(created_by)`,
		r.ID, r.TenantID, r.Metric, r.Op, r.Threshold, r.ForDuration, r.Severity, r.Message,
		boolToInt(r.Enabled), r.CreatedAt, nullString(r.CreatedBy))
	if err != nil {
		log.Printf("[store] CreateAlertRule 失败: %v", err)
		return nil
	}
	return r
}

// ListAlertRules returns rules, optionally filtered by tenant.
func (m *MySQLStore) ListAlertRules(tenantID string) []*AlertRule {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, tenant_id, metric, op, threshold, for_duration, severity, message, enabled, created_at, created_by FROM alert_rules`
	var args []interface{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at ASC`
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListAlertRules 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*AlertRule
	for rows.Next() {
		r := &AlertRule{}
		var createdAt sql.NullTime
		var createdBy sql.NullString
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Metric, &r.Op, &r.Threshold, &r.ForDuration, &r.Severity, &r.Message, &r.Enabled, &createdAt, &createdBy); err != nil {
			log.Printf("[store] ListAlertRules 扫描失败: %v", err)
			continue
		}
		if createdAt.Valid {
			r.CreatedAt = createdAt.Time
		}
		r.CreatedBy = createdBy.String
		out = append(out, r)
	}
	return out
}

// DeleteAlertRule deletes a rule.
func (m *MySQLStore) DeleteAlertRule(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx, `DELETE FROM alert_rules WHERE id=?`, id)
	if err != nil {
		log.Printf("[store] DeleteAlertRule 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// GetAlertRule returns a rule by ID.
func (m *MySQLStore) GetAlertRule(id string) *AlertRule {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, metric, op, threshold, for_duration, severity, message, enabled, created_at, created_by FROM alert_rules WHERE id=?`, id)
	r := &AlertRule{}
	var createdAt sql.NullTime
	var createdBy sql.NullString
	if err := row.Scan(&r.ID, &r.TenantID, &r.Metric, &r.Op, &r.Threshold, &r.ForDuration, &r.Severity, &r.Message, &r.Enabled, &createdAt, &createdBy); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetAlertRule 查询失败: %v", err)
		}
		return nil
	}
	if createdAt.Valid {
		r.CreatedAt = createdAt.Time
	}
	r.CreatedBy = createdBy.String
	return r
}

// UpdateAlertRule updates a rule.
func (m *MySQLStore) UpdateAlertRule(r *AlertRule) bool {
	if r == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx,
		`UPDATE alert_rules SET tenant_id=?, metric=?, op=?, threshold=?, for_duration=?, severity=?, message=?, enabled=?, created_by=? WHERE id=?`,
		r.TenantID, r.Metric, r.Op, r.Threshold, r.ForDuration, r.Severity, r.Message,
		boolToInt(r.Enabled), nullString(r.CreatedBy), r.ID)
	if err != nil {
		log.Printf("[store] UpdateAlertRule 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// Helper functions
func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// 编译期断言：MySQLStore 实现 AlertStore 接口。
var _ AlertStore = (*MySQLStore)(nil)
