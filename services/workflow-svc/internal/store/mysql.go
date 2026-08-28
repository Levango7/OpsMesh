package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/models"
	_ "github.com/go-sql-driver/mysql"
)

// MySQLStore is a MySQL-backed implementation of WorkflowStore.
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
		`CREATE TABLE IF NOT EXISTS workflows (
			id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			status VARCHAR(32) DEFAULT 'draft',
			nodes JSON,
			edges JSON,
			created_at DATETIME,
			updated_at DATETIME,
			INDEX idx_status (status)
		)`,
		`CREATE TABLE IF NOT EXISTS executions (
			id VARCHAR(64) PRIMARY KEY,
			workflow_id VARCHAR(64) NOT NULL,
			status VARCHAR(32) DEFAULT 'running',
			node_states JSON,
			context JSON,
			started_at DATETIME,
			completed_at DATETIME,
			error_message TEXT,
			INDEX idx_workflow (workflow_id),
			INDEX idx_status (status)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLStore) CreateWorkflow(w *models.Workflow) (*models.Workflow, error) {
	if w == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now().UTC()
	}
	w.UpdatedAt = time.Now().UTC()
	nodes, _ := json.Marshal(w.Nodes)
	edges, _ := json.Marshal(w.Edges)
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO workflows (id, name, description, status, nodes, edges, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), description=VALUES(description), status=VALUES(status),
		   nodes=VALUES(nodes), edges=VALUES(edges), updated_at=VALUES(updated_at)`,
		w.ID, w.Name, w.Description, w.Status, nodes, edges, nullTime(w.CreatedAt), nullTime(w.UpdatedAt))
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (m *MySQLStore) GetWorkflow(id string) (*models.Workflow, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT id, name, description, status, nodes, edges, created_at, updated_at FROM workflows WHERE id=?`, id)
	w := &models.Workflow{}
	var nodes, edges []byte
	var createdAt, updatedAt sql.NullTime
	if err := row.Scan(&w.ID, &w.Name, &w.Description, &w.Status, &nodes, &edges, &createdAt, &updatedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetWorkflow 查询失败: %v", err)
		}
		return nil, false
	}
	if len(nodes) > 0 {
		_ = json.Unmarshal(nodes, &w.Nodes)
	}
	if len(edges) > 0 {
		_ = json.Unmarshal(edges, &w.Edges)
	}
	if createdAt.Valid {
		w.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		w.UpdatedAt = updatedAt.Time
	}
	return w, true
}

func (m *MySQLStore) UpdateWorkflow(w *models.Workflow) error {
	if w == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w.UpdatedAt = time.Now().UTC()
	nodes, _ := json.Marshal(w.Nodes)
	edges, _ := json.Marshal(w.Edges)
	_, err := m.db.ExecContext(ctx,
		`UPDATE workflows SET name=?, description=?, status=?, nodes=?, edges=?, updated_at=? WHERE id=?`,
		w.Name, w.Description, w.Status, nodes, edges, nullTime(w.UpdatedAt), w.ID)
	return err
}

func (m *MySQLStore) DeleteWorkflow(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx, `DELETE FROM workflows WHERE id=?`, id)
	if err != nil {
		log.Printf("[store] DeleteWorkflow 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (m *MySQLStore) ListWorkflows(status string) []*models.Workflow {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, name, description, status, nodes, edges, created_at, updated_at FROM workflows`
	var args []interface{}
	if status != "" {
		q += " WHERE status=?"
		args = append(args, status)
	}
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListWorkflows 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.Workflow
	for rows.Next() {
		w := &models.Workflow{}
		var nodes, edges []byte
		var createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&w.ID, &w.Name, &w.Description, &w.Status, &nodes, &edges, &createdAt, &updatedAt); err != nil {
			continue
		}
		if len(nodes) > 0 {
			_ = json.Unmarshal(nodes, &w.Nodes)
		}
		if len(edges) > 0 {
			_ = json.Unmarshal(edges, &w.Edges)
		}
		if createdAt.Valid {
			w.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			w.UpdatedAt = updatedAt.Time
		}
		out = append(out, w)
	}
	return out
}

func (m *MySQLStore) CreateExecution(e *models.Execution) (*models.Execution, error) {
	if e == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e.StartedAt.IsZero() {
		e.StartedAt = time.Now().UTC()
	}
	nodeStates, _ := json.Marshal(e.NodeStates)
	context, _ := json.Marshal(e.Context)
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO executions (id, workflow_id, status, node_states, context, started_at, completed_at, error_message)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE workflow_id=VALUES(workflow_id), status=VALUES(status),
		   node_states=VALUES(node_states), context=VALUES(context), completed_at=VALUES(completed_at), error_message=VALUES(error_message)`,
		e.ID, e.WorkflowID, e.Status, nodeStates, context, nullTime(e.StartedAt), nullTime(e.CompletedAt), nullString(e.ErrorMessage))
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (m *MySQLStore) GetExecution(id string) (*models.Execution, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT id, workflow_id, status, node_states, context, started_at, completed_at, error_message FROM executions WHERE id=?`, id)
	e := &models.Execution{}
	var nodeStates, ctxData []byte
	var startedAt, completedAt sql.NullTime
	if err := row.Scan(&e.ID, &e.WorkflowID, &e.Status, &nodeStates, &ctxData, &startedAt, &completedAt, &e.ErrorMessage); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetExecution 查询失败: %v", err)
		}
		return nil, false
	}
	if len(nodeStates) > 0 {
		_ = json.Unmarshal(nodeStates, &e.NodeStates)
	}
	if len(ctxData) > 0 {
		_ = json.Unmarshal(ctxData, &e.Context)
	}
	if startedAt.Valid {
		e.StartedAt = startedAt.Time
	}
	if completedAt.Valid {
		e.CompletedAt = completedAt.Time
	}
	return e, true
}

func (m *MySQLStore) UpdateExecution(e *models.Execution) error {
	if e == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	nodeStates, _ := json.Marshal(e.NodeStates)
	context, _ := json.Marshal(e.Context)
	_, err := m.db.ExecContext(ctx,
		`UPDATE executions SET workflow_id=?, status=?, node_states=?, context=?, completed_at=?, error_message=? WHERE id=?`,
		e.WorkflowID, e.Status, nodeStates, context, nullTime(e.CompletedAt), nullString(e.ErrorMessage), e.ID)
	return err
}

func (m *MySQLStore) ListExecutions(workflowID string) []*models.Execution {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, workflow_id, status, node_states, context, started_at, completed_at, error_message FROM executions`
	var args []interface{}
	if workflowID != "" {
		q += " WHERE workflow_id=?"
		args = append(args, workflowID)
	}
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListExecutions 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.Execution
	for rows.Next() {
		e := &models.Execution{}
		var nodeStates, ctxData []byte
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(&e.ID, &e.WorkflowID, &e.Status, &nodeStates, &ctxData, &startedAt, &completedAt, &e.ErrorMessage); err != nil {
			continue
		}
		if len(nodeStates) > 0 {
			_ = json.Unmarshal(nodeStates, &e.NodeStates)
		}
		if len(ctxData) > 0 {
			_ = json.Unmarshal(ctxData, &e.Context)
		}
		if startedAt.Valid {
			e.StartedAt = startedAt.Time
		}
		if completedAt.Valid {
			e.CompletedAt = completedAt.Time
		}
		out = append(out, e)
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
