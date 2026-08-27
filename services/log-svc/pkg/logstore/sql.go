// SQL backend for log storage (MySQL).
package logstore

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// SQLLogStore is a MySQL-backed log store.
type SQLLogStore struct {
	db *sql.DB
}

// NewSQL creates a MySQL-backed log store.
func NewSQL(db *sql.DB) (*SQLLogStore, error) {
	s := &SQLLogStore{db: db}
	if err := s.initSchema(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

// initSchema creates the log_entries table if it doesn't exist.
func (s *SQLLogStore) initSchema(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS log_entries (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			device_id VARCHAR(64),
			agent_id VARCHAR(64),
			task_id VARCHAR(64),
			ts DATETIME NOT NULL,
			level VARCHAR(16),
			source VARCHAR(32),
			message MEDIUMTEXT
		)`)
	return err
}

// Append writes a log entry to MySQL.
func (s *SQLLogStore) Append(ctx context.Context, e *Entry) error {
	if e == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ts := e.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO log_entries (tenant_id, device_id, agent_id, task_id, ts, level, source, message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.TenantID, nullStr(e.DeviceID), nullStr(e.AgentID), nullStr(e.TaskID),
		ts, e.Level, e.Source, e.Message)
	return err
}

// Query searches log entries in MySQL.
func (s *SQLLogStore) Query(ctx context.Context, q Query) ([]Entry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}
	if q.Offset < 0 {
		q.Offset = 0
	}

	var where []string
	var args []interface{}
	if q.TenantID != "" {
		where = append(where, "tenant_id = ?")
		args = append(args, q.TenantID)
	}
	if q.DeviceID != "" {
		where = append(where, "device_id = ?")
		args = append(args, q.DeviceID)
	}
	if q.AgentID != "" {
		where = append(where, "agent_id = ?")
		args = append(args, q.AgentID)
	}
	if q.Level != "" {
		where = append(where, "level = ?")
		args = append(args, q.Level)
	}
	if q.Source != "" {
		where = append(where, "source = ?")
		args = append(args, q.Source)
	}
	if q.Keyword != "" {
		where = append(where, "message LIKE ?")
		args = append(args, "%"+q.Keyword+"%")
	}
	if !q.From.IsZero() {
		where = append(where, "ts >= ?")
		args = append(args, q.From)
	}
	if !q.To.IsZero() {
		where = append(where, "ts <= ?")
		args = append(args, q.To)
	}

	query := "SELECT id, tenant_id, device_id, agent_id, task_id, ts, level, source, message FROM log_entries"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY ts DESC LIMIT ? OFFSET ?"
	args = append(args, limit, q.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var ts time.Time
		if err := rows.Scan(&e.ID, &e.TenantID, &e.DeviceID, &e.AgentID, &e.TaskID, &ts, &e.Level, &e.Source, &e.Message); err != nil {
			return nil, err
		}
		e.Timestamp = ts
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// Close releases resources (does not close shared *sql.DB).
func (s *SQLLogStore) Close() error { return nil }

// nullStr converts empty string to SQL NULL.
func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// Ensure SQLLogStore implements LogStore.
var _ LogStore = (*SQLLogStore)(nil)
