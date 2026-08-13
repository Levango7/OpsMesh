package orchestration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SQLWorkflowStore MySQL 后端。
type SQLWorkflowStore struct {
	db *sql.DB
}

// NewSQL 构造 SQL 工作流存储并建表。
func NewSQL(db *sql.DB) (*SQLWorkflowStore, error) {
	s := &SQLWorkflowStore{db: db}
	if err := s.initSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("workflow schema: %w", err)
	}
	return s, nil
}

func (s *SQLWorkflowStore) initSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS workflow_defs (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			agent_id VARCHAR(64) NOT NULL,
			tenant_id VARCHAR(64) NOT NULL,
			dag MEDIUMTEXT,
			cron VARCHAR(64),
			status VARCHAR(16) DEFAULT 'draft',
			last_run_at DATETIME,
			last_run_status VARCHAR(16),
			created_at DATETIME,
			updated_at DATETIME
		)`); err != nil {
		return fmt.Errorf("建 workflow_defs 表失败: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS workflow_runs (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			workflow_id BIGINT NOT NULL,
			tenant_id VARCHAR(64) NOT NULL,
			started_at DATETIME NOT NULL,
			finished_at DATETIME,
			status VARCHAR(16) NOT NULL,
			node_states MEDIUMTEXT
		)`); err != nil {
		return fmt.Errorf("建 workflow_runs 表失败: %w", err)
	}
	return nil
}

const wfCols = "id, name, agent_id, tenant_id, dag, cron, status, last_run_at, last_run_status, created_at, updated_at"

func (s *SQLWorkflowStore) Create(ctx context.Context, wf *WorkflowDef) error {
	dagJSON := wf.DAG
	if dagJSON == "" {
		dagJSON = "[]"
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_defs (name, agent_id, tenant_id, dag, cron, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		wf.Name, wf.AgentID, wf.TenantID, dagJSON, wf.Cron,
		orDefault(wf.Status, StatusDraft), now, now)
	if err != nil {
		return fmt.Errorf("WorkflowStore.Create: %w", err)
	}
	id, _ := res.LastInsertId()
	wf.ID = id
	wf.CreatedAt = now
	wf.UpdatedAt = now
	return nil
}

func (s *SQLWorkflowStore) Get(ctx context.Context, id int64, tenantID string) (*WorkflowDef, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+wfCols+` FROM workflow_defs WHERE id=?`, id)
	wf, err := scanWorkflow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrWFNotFound
		}
		return nil, fmt.Errorf("WorkflowStore.Get: %w", err)
	}
	if tenantID != "" && wf.TenantID != tenantID {
		return nil, ErrWFTenantMismatch
	}
	return wf, nil
}

func (s *SQLWorkflowStore) List(ctx context.Context, tenantID string) ([]WorkflowDef, error) {
	q := `SELECT ` + wfCols + ` FROM workflow_defs`
	args := []interface{}{}
	if tenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("WorkflowStore.List: %w", err)
	}
	defer rows.Close()
	var out []WorkflowDef
	for rows.Next() {
		wf, e := scanWorkflow(rows)
		if e != nil {
			return nil, fmt.Errorf("WorkflowStore.List scan: %w", e)
		}
		out = append(out, *wf)
	}
	return out, rows.Err()
}

func (s *SQLWorkflowStore) Update(ctx context.Context, wf *WorkflowDef) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE workflow_defs SET name=?, agent_id=?, dag=?, cron=?, status=?,
			last_run_at=?, last_run_status=?, updated_at=NOW()
		WHERE id=? AND tenant_id=?`,
		wf.Name, wf.AgentID, wf.DAG, wf.Cron, wf.Status,
		nullTime(wf.LastRunAt), nullStr(wf.LastRunStatus), wf.ID, wf.TenantID)
	if err != nil {
		return fmt.Errorf("WorkflowStore.Update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrWFNotFound
	}
	wf.UpdatedAt = time.Now()
	return nil
}

func (s *SQLWorkflowStore) Delete(ctx context.Context, id int64, tenantID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM workflow_defs WHERE id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		return fmt.Errorf("WorkflowStore.Delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrWFNotFound
	}
	return nil
}

// scanner 兼容 *sql.Row 与 *sql.Rows。
type scanner interface {
	Scan(dest ...interface{}) error
}

func scanWorkflow(s scanner) (*WorkflowDef, error) {
	var wf WorkflowDef
	var dag, cron sql.NullString
	var lastRunAt sql.NullTime
	var lastRunStatus sql.NullString
	if err := s.Scan(&wf.ID, &wf.Name, &wf.AgentID, &wf.TenantID, &dag, &cron,
		&wf.Status, &lastRunAt, &lastRunStatus, &wf.CreatedAt, &wf.UpdatedAt); err != nil {
		return nil, err
	}
	wf.DAG = dag.String
	wf.Cron = cron.String
	if lastRunAt.Valid {
		wf.LastRunAt = lastRunAt.Time
	}
	wf.LastRunStatus = lastRunStatus.String
	if wf.Status == "" {
		wf.Status = StatusDraft
	}
	return &wf, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func nullStr(v string) interface{} {
	if v == "" {
		return nil
	}
	return v
}

func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

// CreateRun 创建一条工作流运行记录（MySQL 后端）。NodeStates 序列化为 JSON 存入 node_states 列。
func (s *SQLWorkflowStore) CreateRun(ctx context.Context, run *WorkflowRun) error {
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	if run.Status == "" {
		run.Status = StatusRunning
	}
	statesJSON, err := json.Marshal(run.NodeStates)
	if err != nil {
		return fmt.Errorf("WorkflowStore.CreateRun marshal nodeStates: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_runs (workflow_id, tenant_id, started_at, finished_at, status, node_states)
		VALUES (?, ?, ?, ?, ?, ?)`,
		run.WorkflowID, run.TenantID, run.StartedAt, nullTime(run.FinishedAt), run.Status, string(statesJSON))
	if err != nil {
		return fmt.Errorf("WorkflowStore.CreateRun: %w", err)
	}
	id, _ := res.LastInsertId()
	run.ID = id
	return nil
}

// ListRuns 列出指定工作流的运行历史（按 ID 升序），支持租户隔离（MySQL 后端）。
func (s *SQLWorkflowStore) ListRuns(ctx context.Context, workflowID int64, tenantID string) ([]WorkflowRun, error) {
	q := `SELECT id, workflow_id, tenant_id, started_at, finished_at, status, node_states FROM workflow_runs WHERE workflow_id=?`
	args := []interface{}{workflowID}
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("WorkflowStore.ListRuns: %w", err)
	}
	defer rows.Close()
	var out []WorkflowRun
	for rows.Next() {
		var r WorkflowRun
		var finishedAt sql.NullTime
		var nodeStates sql.NullString
		if err := rows.Scan(&r.ID, &r.WorkflowID, &r.TenantID, &r.StartedAt, &finishedAt, &r.Status, &nodeStates); err != nil {
			return nil, fmt.Errorf("WorkflowStore.ListRuns scan: %w", err)
		}
		if finishedAt.Valid {
			r.FinishedAt = finishedAt.Time
		}
		if nodeStates.Valid && nodeStates.String != "" {
			if err := json.Unmarshal([]byte(nodeStates.String), &r.NodeStates); err != nil {
				return nil, fmt.Errorf("WorkflowStore.ListRuns unmarshal nodeStates: %w", err)
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateRun 更新一条已存在的工作流运行记录（MySQL 后端）。不存在时返回 ErrWFNotFound。
func (s *SQLWorkflowStore) UpdateRun(ctx context.Context, run *WorkflowRun) error {
	statesJSON, err := json.Marshal(run.NodeStates)
	if err != nil {
		return fmt.Errorf("WorkflowStore.UpdateRun marshal nodeStates: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE workflow_runs SET started_at=?, finished_at=?, status=?, node_states=? WHERE id=?`,
		run.StartedAt, nullTime(run.FinishedAt), run.Status, string(statesJSON), run.ID)
	if err != nil {
		return fmt.Errorf("WorkflowStore.UpdateRun: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrWFNotFound
	}
	return nil
}
