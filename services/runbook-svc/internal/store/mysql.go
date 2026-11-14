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
			-- tenant_id：行级租户隔离键（D4 / issue #27 裁决②）
			tenant_id   VARCHAR(64)  NOT NULL DEFAULT '',
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
			-- 归属由父 runbook 派生（不接受调用方指定）
			tenant_id    VARCHAR(64) NOT NULL DEFAULT '',
			runbook_id   VARCHAR(64) NOT NULL,
			triggered_by VARCHAR(255) NOT NULL DEFAULT '',
			status       VARCHAR(32) NOT NULL DEFAULT 'running',
			step_results JSON,
			started_at   DATETIME(3) NOT NULL,
			completed_at DATETIME(3) NULL,
			error_message TEXT,
			INDEX idx_exec_runbook (runbook_id),
			INDEX idx_exec_tenant (tenant_id)
		) ENGINE=InnoDB`,
	}
	for _, ddl := range stmts {
		if _, err := m.db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}
	}
	// 既有库补列（tenant_id 由 D4 / issue #27 裁决② 引入）：CREATE TABLE IF NOT EXISTS 对
	// **已存在**的表是 no-op，存量库不补列就会在第一次 INSERT/SELECT 时炸
	// Unknown column 'tenant_id'（服务自建表路径下升级即不可用）。
	// MySQL 8 无 ADD COLUMN IF NOT EXISTS，故先查 information_schema 再决定加不加（幂等）。
	if err := ensureColumn(ctx, m.db, "runbooks", "tenant_id",
		`ALTER TABLE runbooks ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '行级租户隔离键（D4 / issue #27 裁决②）'`); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	if err := ensureColumn(ctx, m.db, "runbook_executions", "tenant_id",
		`ALTER TABLE runbook_executions ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '归属由父 runbook 派生（不接受调用方指定）'`); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}

// ensureColumn 在列不存在时执行给定 DDL 补列；已存在则什么都不做（MySQL 8 无 ADD COLUMN IF NOT EXISTS）。
// 表名/列名只进查询条件，DDL 由调用方以字面量提供（不接受外部输入拼接）。
func ensureColumn(ctx context.Context, db *sql.DB, table, column, ddl string) error {
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`, table, column).Scan(&n); err != nil {
		return fmt.Errorf("检查 %s.%s: %w", table, column, err)
	}
	if n > 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("补列 %s.%s: %w", table, column, err)
	}
	log.Printf("[store] %s 表已补列 %s（既有库迁移）", table, column)
	return nil
}

const runbookCols = `id, tenant_id, name, description, content, triggers, steps, enabled, created_at, updated_at`

func scanRunbook(scan func(dest ...any) error) (*models.Runbook, error) {
	var r models.Runbook
	var triggers, steps []byte
	if err := scan(&r.ID, &r.TenantID, &r.Name, &r.Description, &r.Content, &triggers, &steps, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
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
		`INSERT INTO runbooks (`+runbookCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.TenantID, r.Name, r.Description, r.Content, triggers, steps, r.Enabled, r.CreatedAt, r.UpdatedAt)
	if err != nil {
		log.Printf("[store] CreateRunbook 失败: %v", err)
		return nil
	}
	return r
}

// GetRunbook retrieves a runbook by ID.
// GetRunbook 按 id + 租户取；跨租户与不存在同样返回 nil。
func (m *MySQLStore) GetRunbook(id, tenantID string) *models.Runbook {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx, `SELECT `+runbookCols+` FROM runbooks WHERE id = ? AND tenant_id = ?`, id, tenantID)
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
// ListRunbooks 只返回该租户的 runbook。
func (m *MySQLStore) ListRunbooks(tenantID string) []*models.Runbook {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := m.db.QueryContext(ctx, `SELECT `+runbookCols+` FROM runbooks WHERE tenant_id = ? ORDER BY created_at DESC`, tenantID)
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
// UpdateRunbook 跨租户不可改；SET 不含 tenant_id（归属不可改挂）。
func (m *MySQLStore) UpdateRunbook(r *models.Runbook, tenantID string) bool {
	if r == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r.UpdatedAt = time.Now()
	triggers, _ := json.Marshal(r.Triggers)
	steps, _ := json.Marshal(r.Steps)
	res, err := m.db.ExecContext(ctx,
		`UPDATE runbooks SET name=?, description=?, content=?, triggers=?, steps=?, enabled=?, updated_at=? WHERE id=? AND tenant_id=?`,
		r.Name, r.Description, r.Content, triggers, steps, r.Enabled, r.UpdatedAt, r.ID, tenantID)
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
// DeleteRunbook 跨租户删不掉。
func (m *MySQLStore) DeleteRunbook(id, tenantID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx, `DELETE FROM runbooks WHERE id = ? AND tenant_id = ?`, id, tenantID)
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
// AddExecution 归属由父 runbook 决定：不属于该租户 ⇒ 拒收。
func (m *MySQLStore) AddExecution(e *models.ExecutionRecord) {
	if e == nil {
		return
	}
	if m.GetRunbook(e.RunbookID, e.TenantID) == nil {
		return // 父 runbook 不属于该租户 ⇒ 拒收
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
		`INSERT INTO runbook_executions (id, tenant_id, runbook_id, triggered_by, status, step_results, started_at, completed_at, error_message)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.TenantID, e.RunbookID, e.TriggeredBy, e.Status, results, e.StartedAt, completedAt, e.ErrorMessage); err != nil {
		log.Printf("[store] AddExecution 失败: %v", err)
	}
}

// GetExecutions returns execution history for a runbook (newest first).
// GetExecutions 归属由父 runbook 决定（跨租户 ⇒ 空）。
func (m *MySQLStore) GetExecutions(runbookID, tenantID string) []*models.ExecutionRecord {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, tenant_id, runbook_id, triggered_by, status, step_results, started_at, completed_at, error_message
		 FROM runbook_executions WHERE runbook_id = ? AND tenant_id = ? ORDER BY started_at DESC`, runbookID, tenantID)
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
		if err := rows.Scan(&e.ID, &e.TenantID, &e.RunbookID, &e.TriggeredBy, &e.Status, &results, &e.StartedAt, &completedAt, &errMsg); err != nil {
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
