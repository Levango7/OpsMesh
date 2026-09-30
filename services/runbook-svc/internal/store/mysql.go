package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/models"

	_ "github.com/go-sql-driver/mysql"
)

// ensureParseTime 保证 go-sql-driver 的 DSN 自带 parseTime=true。
// 背景（2026-09-25 线上缺陷的回归守护，与 10 个兄弟服务同款）：自备 DSN 不保证带
// parseTime，缺了 time 列扫描即报错，服务会静默退回内存存储（部署成功、重启丢数据）。
func ensureParseTime(dsn string) string {
	if strings.Contains(dsn, "parseTime=") {
		return dsn
	}
	if strings.Contains(dsn, "?") {
		return dsn + "&parseTime=true"
	}
	return dsn + "?parseTime=true"
}

// MySQLStore is a MySQL-backed implementation of RunbookStore.
type MySQLStore struct {
	db *sql.DB
}

// NewMySQLStore opens the connection, verifies it and creates the schema.
// 任何一步失败返回错误——调用方（main）对声明为 sql 的持久化必须 fail-fast，
// 静默退回内存存储会让 runbook 重启即丢（TD-68 同哲学）。
func NewMySQLStore(dsn string) (*MySQLStore, error) {
	db, err := sql.Open("mysql", ensureParseTime(dsn))
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	m := &MySQLStore{db: db}
	if err := m.initSchema(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return m, nil
}

// Close releases the connection pool.
func (m *MySQLStore) Close() error { return m.db.Close() }

func (m *MySQLStore) initSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS runbooks (
			id          VARCHAR(64)  NOT NULL PRIMARY KEY,
			name        VARCHAR(255) NOT NULL,
			description TEXT,
			content     LONGTEXT,
			triggers    JSON,
			steps       JSON,
			enabled     BOOLEAN      NOT NULL DEFAULT FALSE,
			created_at  DATETIME(3)  NOT NULL,
			updated_at  DATETIME(3)  NOT NULL
		) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS runbook_executions (
			id           VARCHAR(64) NOT NULL PRIMARY KEY,
			runbook_id   VARCHAR(64) NOT NULL,
			triggered_by VARCHAR(255) NOT NULL DEFAULT '',
			status       VARCHAR(32) NOT NULL DEFAULT 'running',
			step_results JSON,
			started_at   DATETIME(3) NOT NULL,
			completed_at DATETIME(3) NULL,
			error_message TEXT,
			INDEX idx_exec_runbook (runbook_id)
		) ENGINE=InnoDB`,
	}
	for _, ddl := range stmts {
		if _, err := m.db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}
	}
	return nil
}

const runbookCols = `id, name, description, content, triggers, steps, enabled, created_at, updated_at`

func scanRunbook(scan func(dest ...any) error) (*models.Runbook, error) {
	var r models.Runbook
	var triggers, steps []byte
	if err := scan(&r.ID, &r.Name, &r.Description, &r.Content, &triggers, &steps, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	// 结构化列损坏按空处理：Runbook 的 Content/Steps 是可再编辑资产，
	// 一条坏行不该让整个列表打不开。
	if len(triggers) > 0 {
		if err := json.Unmarshal(triggers, &r.Triggers); err != nil {
			log.Printf("[store] runbook %s triggers 解码失败（按空处理）: %v", r.ID, err)
		}
	}
	if len(steps) > 0 {
		if err := json.Unmarshal(steps, &r.Steps); err != nil {
			log.Printf("[store] runbook %s steps 解码失败（按空处理）: %v", r.ID, err)
		}
	}
	return &r, nil
}

// CreateRunbook stores a new runbook.
func (m *MySQLStore) CreateRunbook(r *models.Runbook) *models.Runbook {
	if r == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = r.CreatedAt
	}
	triggers, _ := json.Marshal(r.Triggers)
	steps, _ := json.Marshal(r.Steps)
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO runbooks (`+runbookCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Name, r.Description, r.Content, triggers, steps, r.Enabled, r.CreatedAt, r.UpdatedAt)
	if err != nil {
		log.Printf("[store] CreateRunbook 失败: %v", err)
		return nil
	}
	return r
}

// GetRunbook retrieves a runbook by ID.
func (m *MySQLStore) GetRunbook(id string) *models.Runbook {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx, `SELECT `+runbookCols+` FROM runbooks WHERE id = ?`, id)
	r, err := scanRunbook(row.Scan)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetRunbook 失败: %v", err)
		}
		return nil
	}
	return r
}

// ListRunbooks returns all runbooks.
func (m *MySQLStore) ListRunbooks() []*models.Runbook {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := m.db.QueryContext(ctx, `SELECT `+runbookCols+` FROM runbooks ORDER BY created_at DESC`)
	if err != nil {
		log.Printf("[store] ListRunbooks 失败: %v", err)
		return nil
	}
	defer rows.Close()
	out := make([]*models.Runbook, 0)
	for rows.Next() {
		r, err := scanRunbook(rows.Scan)
		if err != nil {
			log.Printf("[store] ListRunbooks 扫描失败: %v", err)
			continue
		}
		out = append(out, r)
	}
	return out
}

// UpdateRunbook updates an existing runbook. Returns false when not found.
func (m *MySQLStore) UpdateRunbook(r *models.Runbook) bool {
	if r == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r.UpdatedAt = time.Now()
	triggers, _ := json.Marshal(r.Triggers)
	steps, _ := json.Marshal(r.Steps)
	res, err := m.db.ExecContext(ctx,
		`UPDATE runbooks SET name=?, description=?, content=?, triggers=?, steps=?, enabled=?, updated_at=? WHERE id=?`,
		r.Name, r.Description, r.Content, triggers, steps, r.Enabled, r.UpdatedAt, r.ID)
	if err != nil {
		log.Printf("[store] UpdateRunbook 失败: %v", err)
		return false
	}
	n, err := res.RowsAffected()
	if err != nil {
		log.Printf("[store] UpdateRunbook RowsAffected: %v", err)
		return false
	}
	return n > 0
}

// DeleteRunbook removes a runbook by ID (连同其执行历史）。
func (m *MySQLStore) DeleteRunbook(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx, `DELETE FROM runbooks WHERE id = ?`, id)
	if err != nil {
		log.Printf("[store] DeleteRunbook 失败: %v", err)
		return false
	}
	n, err := res.RowsAffected()
	if err != nil {
		log.Printf("[store] DeleteRunbook RowsAffected: %v", err)
		return false
	}
	if n > 0 {
		// 执行历史随 runbook 一并清理；失败只记日志（下个删除周期无重放入口，
		// 但 runbook 本体已删，孤儿执行记录不影响正确性）。
		if _, err := m.db.ExecContext(ctx, `DELETE FROM runbook_executions WHERE runbook_id = ?`, id); err != nil {
			log.Printf("[store] 清理执行历史失败: %v", err)
		}
	}
	return n > 0
}

// AddExecution records a runbook execution.
func (m *MySQLStore) AddExecution(e *models.ExecutionRecord) {
	if e == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e.StartedAt.IsZero() {
		e.StartedAt = time.Now()
	}
	results, _ := json.Marshal(e.StepResults)
	var completedAt any
	if !e.CompletedAt.IsZero() {
		completedAt = e.CompletedAt
	}
	if _, err := m.db.ExecContext(ctx,
		`INSERT INTO runbook_executions (id, runbook_id, triggered_by, status, step_results, started_at, completed_at, error_message)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.RunbookID, e.TriggeredBy, e.Status, results, e.StartedAt, completedAt, e.ErrorMessage); err != nil {
		log.Printf("[store] AddExecution 失败: %v", err)
	}
}

// GetExecutions returns execution history for a runbook (newest first).
func (m *MySQLStore) GetExecutions(runbookID string) []*models.ExecutionRecord {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, runbook_id, triggered_by, status, step_results, started_at, completed_at, error_message
		 FROM runbook_executions WHERE runbook_id = ? ORDER BY started_at DESC`, runbookID)
	if err != nil {
		log.Printf("[store] GetExecutions 失败: %v", err)
		return nil
	}
	defer rows.Close()
	out := make([]*models.ExecutionRecord, 0)
	for rows.Next() {
		var e models.ExecutionRecord
		var results []byte
		var completedAt sql.NullTime
		var errMsg sql.NullString
		if err := rows.Scan(&e.ID, &e.RunbookID, &e.TriggeredBy, &e.Status, &results, &e.StartedAt, &completedAt, &errMsg); err != nil {
			log.Printf("[store] GetExecutions 扫描失败: %v", err)
			continue
		}
		if completedAt.Valid {
			e.CompletedAt = completedAt.Time
		}
		e.ErrorMessage = errMsg.String
		if len(results) > 0 {
			if err := json.Unmarshal(results, &e.StepResults); err != nil {
				log.Printf("[store] execution %s step_results 解码失败（按空处理）: %v", e.ID, err)
			}
		}
		out = append(out, &e)
	}
	return out
}
