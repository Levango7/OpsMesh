package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
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

// ensureParseTime 保证 DSN 带 parseTime=true（否则 DATETIME 列无法 Scan 进 time.Time），
// 且对「已带查询参数」的 DSN 幂等。
//
// 修复的线上缺陷（2026-09-25 实测）：原实现无条件在末尾追加 "?parseTime=true"。
// 而 compose 等生产清单给出的 DSN 本身已含 "?parseTime=true"，拼出
// "...?parseTime=true?parseTime=true"，驱动报 invalid bool value: true?parseTime=true，
// sql.Open 直接失败 → 服务【静默退回 memory store】（生产重启即丢数据：
// 实测 alert-svc / config-svc 运行在内存存储上，库内无任何表）。
func ensureParseTime(dsn string) string {
	if strings.Contains(dsn, "parseTime=") {
		return dsn
	}
	if strings.Contains(dsn, "?") {
		return dsn + "&parseTime=true"
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
			rule_id VARCHAR(64),
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

	// 升级路径：alerts.rule_id 是后加的列（2026-10-04）。CREATE TABLE IF NOT EXISTS 对
	// **已存在**的表什么都不做，存量库不补列就会在第一次 INSERT 时炸
	// `Unknown column 'rule_id' in 'field list'`，升级即服务不可用。
	// MySQL 的 ADD COLUMN 没有 IF NOT EXISTS，重复执行报 1060，所以先查 information_schema
	// 再决定加不加——与主仓 migrations 对幂等的口径一致。
	if err := ensureColumn(ctx, db, "alerts", "rule_id",
		`ALTER TABLE alerts ADD COLUMN rule_id VARCHAR(64) AFTER tenant_id`); err != nil {
		return err
	}
	return nil
}

// ensureColumn 在列不存在时执行给定 DDL 补列；已存在则什么都不做。
// 表名/列名只用于查询条件，DDL 语句本身由调用方以字面量提供（不接受外部输入拼接）。
func ensureColumn(ctx context.Context, db *sql.DB, table, column, ddl string) error {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.COLUMNS
		 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`, table, column).Scan(&n)
	if err != nil {
		return fmt.Errorf("ensureColumn: 检查 %s.%s 失败: %w", table, column, err)
	}
	if n > 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("ensureColumn: 补列 %s.%s 失败: %w", table, column, err)
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

// alertColumns / alertRuleColumns 是两张表的读取列清单，必须与下面 nullAlert.dest() /
// nullAlertRule.dest() 的目标顺序逐位对齐（TestAlertColumnCountMatchesScanTargets 钉这一点）。
const alertColumns = "alert_id, tenant_id, rule_id, device_id, agent_id, severity, message, metric, " +
	"status, acknowledged_by, silenced_until, comment, created_at, updated_at"

const alertRuleColumns = "id, tenant_id, metric, op, threshold, for_duration, severity, message, " +
	"enabled, created_at, created_by"

// nullAlert 承接 alerts 表一行的原始值——**每**一列都用 sql.Null*。
//
// 为什么不能"能空的才用 Null*"：schema 里除 alert_id/tenant_id 外全部可空，而写侧
// AddAlert 对空字符串一律走 nullString() 落成 NULL，于是"这列没值"是常态而不是异常。
// database/sql 把 NULL Scan 进 string / time.Time 会直接报
// `converting NULL to string is unsupported`，一列 NULL 就让**整行**读不出来。
// 真机实测（2026-10-04，#57 外发腿活体）：agent_id 为 NULL ⇒
// GetAlert 恒 NotFound、ListAlerts 恒空、AckAlert 里取 DeviceID 拿到 nil ⇒
// 外发给 PagerDuty 的 source 字段是空的。写进去的告警在读侧整体消失，
// 而健康检查与 RPC 返回码全程正常——属于"接口活着、数据没了"那一类。
type nullAlert struct {
	alertID        sql.NullString
	tenantID       sql.NullString
	ruleID         sql.NullString
	deviceID       sql.NullString
	agentID        sql.NullString
	severity       sql.NullString
	message        sql.NullString
	metric         sql.NullString
	status         sql.NullString
	acknowledgedBy sql.NullString
	silencedUntil  sql.NullTime
	comment        sql.NullString
	createdAt      sql.NullTime
	updatedAt      sql.NullTime
}

func (n *nullAlert) dest() []any {
	return []any{&n.alertID, &n.tenantID, &n.ruleID, &n.deviceID, &n.agentID, &n.severity, &n.message,
		&n.metric, &n.status, &n.acknowledgedBy, &n.silencedUntil, &n.comment, &n.createdAt, &n.updatedAt}
}

func (n *nullAlert) toAlert() *Alert {
	a := &Alert{
		AlertID:        n.alertID.String,
		TenantID:       n.tenantID.String,
		RuleID:         n.ruleID.String,
		DeviceID:       n.deviceID.String,
		AgentID:        n.agentID.String,
		Severity:       n.severity.String,
		Message:        n.message.String,
		Metric:         n.metric.String,
		Status:         n.status.String,
		AcknowledgedBy: n.acknowledgedBy.String,
		Comment:        n.comment.String,
	}
	if n.silencedUntil.Valid {
		a.SilencedUntil = n.silencedUntil.Time
	}
	if n.createdAt.Valid {
		a.CreatedAt = n.createdAt.Time
	}
	if n.updatedAt.Valid {
		a.UpdatedAt = n.updatedAt.Time
	}
	return a
}

// nullAlertRule 同 nullAlert：全列 sql.Null*，理由一致（阈值/持续时间等列同样可空）。
type nullAlertRule struct {
	id          sql.NullString
	tenantID    sql.NullString
	metric      sql.NullString
	op          sql.NullString
	threshold   sql.NullFloat64
	forDuration sql.NullInt64
	severity    sql.NullString
	message     sql.NullString
	enabled     sql.NullInt64
	createdAt   sql.NullTime
	createdBy   sql.NullString
}

func (n *nullAlertRule) dest() []any {
	return []any{&n.id, &n.tenantID, &n.metric, &n.op, &n.threshold, &n.forDuration,
		&n.severity, &n.message, &n.enabled, &n.createdAt, &n.createdBy}
}

func (n *nullAlertRule) toRule() *AlertRule {
	r := &AlertRule{
		ID:          n.id.String,
		TenantID:    n.tenantID.String,
		Metric:      n.metric.String,
		Op:          n.op.String,
		Threshold:   n.threshold.Float64,
		ForDuration: int(n.forDuration.Int64),
		Severity:    n.severity.String,
		Message:     n.message.String,
		Enabled:     n.enabled.Int64 != 0,
		CreatedBy:   n.createdBy.String,
	}
	if n.createdAt.Valid {
		r.CreatedAt = n.createdAt.Time
	}
	return r
}

// Alerts returns alerts, optionally filtered by tenant.
func (m *MySQLStore) Alerts(tenantID string) []*Alert {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT ` + alertColumns + ` FROM alerts`
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
		var na nullAlert
		if err := rows.Scan(na.dest()...); err != nil {
			log.Printf("[store] Alerts 扫描失败: %v", err)
			continue
		}
		out = append(out, na.toAlert())
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
		`INSERT INTO alerts (alert_id, tenant_id, rule_id, device_id, agent_id, severity, message, metric, status, acknowledged_by, silenced_until, comment, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullString(a.AlertID), nullString(a.TenantID), nullString(a.RuleID), nullString(a.DeviceID), nullString(a.AgentID),
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
	row := m.db.QueryRowContext(ctx, `SELECT `+alertColumns+` FROM alerts WHERE alert_id=?`, id)
	var na nullAlert
	if err := row.Scan(na.dest()...); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] Alert 查询失败: %v", err)
		}
		return nil
	}
	return na.toAlert()
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
	n, err := res.RowsAffected()
	if err != nil {
		log.Printf("[store] RowsAffected: %v", err)
	}
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
	n, err := res.RowsAffected()
	if err != nil {
		log.Printf("[store] RowsAffected: %v", err)
	}
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
	n, err := res.RowsAffected()
	if err != nil {
		log.Printf("[store] RowsAffected: %v", err)
	}
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
	q := `SELECT ` + alertRuleColumns + ` FROM alert_rules`
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
		var nr nullAlertRule
		if err := rows.Scan(nr.dest()...); err != nil {
			log.Printf("[store] ListAlertRules 扫描失败: %v", err)
			continue
		}
		out = append(out, nr.toRule())
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
	n, err := res.RowsAffected()
	if err != nil {
		log.Printf("[store] RowsAffected: %v", err)
	}
	return n > 0
}

// GetAlertRule returns a rule by ID.
func (m *MySQLStore) GetAlertRule(id string) *AlertRule {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx, `SELECT `+alertRuleColumns+` FROM alert_rules WHERE id=?`, id)
	var nr nullAlertRule
	if err := row.Scan(nr.dest()...); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetAlertRule 查询失败: %v", err)
		}
		return nil
	}
	return nr.toRule()
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
	n, err := res.RowsAffected()
	if err != nil {
		log.Printf("[store] RowsAffected: %v", err)
	}
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
