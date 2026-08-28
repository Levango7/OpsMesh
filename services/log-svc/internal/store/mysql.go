package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLStore is a MySQL-backed implementation of LogStore.
type MySQLStore struct {
	db *sql.DB
}

// NewMySQLStore creates a MySQLStore with connection pool.
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

func ensureParseTime(dsn string) string {
	return dsn + "?parseTime=true"
}

func initSchema(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS agent_logs (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			device_id VARCHAR(64),
			agent_id VARCHAR(64),
			task_id VARCHAR(64),
			level VARCHAR(16),
			source VARCHAR(128),
			message TEXT,
			timestamp DATETIME,
			INDEX idx_tenant (tenant_id),
			INDEX idx_agent (agent_id),
			INDEX idx_level (level),
			INDEX idx_timestamp (timestamp)
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			user_id VARCHAR(64),
			action VARCHAR(128),
			target VARCHAR(255),
			detail TEXT,
			timestamp DATETIME,
			INDEX idx_tenant (tenant_id),
			INDEX idx_timestamp (timestamp)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLStore) AppendAgentLog(entry *LogEntry) error {
	if entry == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO agent_logs (tenant_id, device_id, agent_id, task_id, level, source, message, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		nullString(entry.TenantID), nullString(entry.DeviceID), nullString(entry.AgentID), nullString(entry.TaskID),
		nullString(entry.Level), nullString(entry.Source), nullString(entry.Message), nullTime(entry.Timestamp))
	return err
}

func (m *MySQLStore) ListAgentLogs(tenantID, agentID, level string, limit int) []*LogEntry {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT tenant_id, device_id, agent_id, task_id, level, source, message, timestamp FROM agent_logs`
	var args []interface{}
	var where []string
	if tenantID != "" {
		where = append(where, "tenant_id=?")
		args = append(args, tenantID)
	}
	if agentID != "" {
		where = append(where, "agent_id=?")
		args = append(args, agentID)
	}
	if level != "" {
		where = append(where, "level=?")
		args = append(args, level)
	}
	if len(where) > 0 {
		q += " WHERE " + joinWhere(where)
	}
	q += " ORDER BY timestamp DESC"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListAgentLogs 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*LogEntry
	for rows.Next() {
		e := &LogEntry{}
		var ts sql.NullTime
		if err := rows.Scan(&e.TenantID, &e.DeviceID, &e.AgentID, &e.TaskID, &e.Level, &e.Source, &e.Message, &ts); err != nil {
			continue
		}
		if ts.Valid {
			e.Timestamp = ts.Time
		}
		out = append(out, e)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (m *MySQLStore) AppendAuditLog(entry *AuditLog) error {
	if entry == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO audit_logs (tenant_id, user_id, action, target, detail, timestamp) VALUES (?, ?, ?, ?, ?, ?)`,
		nullString(entry.TenantID), nullString(entry.UserID), nullString(entry.Action),
		nullString(entry.Target), nullString(entry.Detail), nullTime(entry.Timestamp))
	return err
}

func (m *MySQLStore) ListAuditLogs(tenantID string, limit int) []*AuditLog {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT tenant_id, user_id, action, target, detail, timestamp FROM audit_logs`
	var args []interface{}
	if tenantID != "" {
		q += " WHERE tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY timestamp DESC"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListAuditLogs 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*AuditLog
	for rows.Next() {
		e := &AuditLog{}
		var ts sql.NullTime
		if err := rows.Scan(&e.TenantID, &e.UserID, &e.Action, &e.Target, &e.Detail, &ts); err != nil {
			continue
		}
		if ts.Valid {
			e.Timestamp = ts.Time
		}
		out = append(out, e)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

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

func joinWhere(conds []string) string {
	out := ""
	for i, c := range conds {
		if i > 0 {
			out += " AND "
		}
		out += c
	}
	return out
}
