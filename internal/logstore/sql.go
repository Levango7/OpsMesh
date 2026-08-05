package logstore

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// SQLLogStore MySQL 后端（U-04 数据本地化，私有部署）。
// 复用控制面 SQLStore 的 *sql.DB（共享连接池），自身不关闭该连接。
type SQLLogStore struct {
	db *sql.DB
}

// initSchema 幂等建表（CREATE TABLE IF NOT EXISTS）。
// ctx 为 nil 时退化为 context.Background()，便于无 ctx 的构造期建表。
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
	if err != nil {
		return err
	}
	// 工程债治理：补二级索引，避免按租户分页查询（tenant_id + ts DESC）全表扫描。
	// MySQL 不支持 CREATE INDEX IF NOT EXISTS，先查 information_schema 再建。
	var idxCnt int
	if qerr := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.statistics
		 WHERE table_schema=DATABASE() AND table_name='log_entries' AND index_name='idx_logs_tenant_created'`,
	).Scan(&idxCnt); qerr == nil && idxCnt == 0 {
		if _, ierr := s.db.ExecContext(ctx, `CREATE INDEX idx_logs_tenant_created ON log_entries(tenant_id, ts DESC)`); ierr != nil {
			// 索引创建失败不阻断启动（兼容老库缺列场景）。
			_ = ierr
		}
	}
	return nil
}

// Append 写入一条日志（tenant_id 由调用方强制赋值）。
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

// Query 按条件检索；结果按时间倒序（最新在前），LIMIT 受 maxQueryLimit 约束。
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
	if q.Offset < 0 {
		q.Offset = 0
	}
	query := "SELECT id, tenant_id, device_id, agent_id, task_id, ts, level, source, message FROM log_entries"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY ts DESC LIMIT ?"
	args = append(args, limit)
	if q.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, q.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Entry, 0, limit)
	for rows.Next() {
		var e Entry
		var ts time.Time
		if err := rows.Scan(&e.ID, &e.TenantID, &e.DeviceID, &e.AgentID, &e.TaskID, &ts, &e.Level, &e.Source, &e.Message); err != nil {
			return nil, err
		}
		e.Timestamp = ts
		out = append(out, e)
	}
	return out, rows.Err()
}

// Close SQL 后端不关闭共享 *sql.DB。
func (s *SQLLogStore) Close() error { return nil }

// nullStr 把空串转为 SQL NULL（避免空串写入后无法被 IS NULL 命中，保持查询语义一致）。
func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
