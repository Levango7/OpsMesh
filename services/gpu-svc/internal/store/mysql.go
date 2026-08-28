package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
	_ "github.com/go-sql-driver/mysql"
)

// MySQLStore is a MySQL-backed implementation of GPUStore.
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
		`CREATE TABLE IF NOT EXISTS gpu_nodes (
			id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			address VARCHAR(255),
			gpus JSON,
			status VARCHAR(32) DEFAULT 'offline',
			labels JSON,
			total_vram_mb INT DEFAULT 0,
			used_vram_mb INT DEFAULT 0,
			gpu_errors INT DEFAULT 0,
			last_heartbeat DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			INDEX idx_status (status)
		)`,
		`CREATE TABLE IF NOT EXISTS gpu_workloads (
			id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			tenant_id VARCHAR(64) NOT NULL,
			type VARCHAR(32),
			status VARCHAR(32) DEFAULT 'pending',
			gpu_request JSON,
			node_ids JSON,
			priority INT DEFAULT 0,
			image VARCHAR(255),
			command JSON,
			env JSON,
			replicas INT DEFAULT 1,
			model_name VARCHAR(255),
			created_at DATETIME,
			updated_at DATETIME,
			started_at DATETIME,
			finished_at DATETIME,
			error_message TEXT,
			INDEX idx_tenant (tenant_id),
			INDEX idx_status (status)
		)`,
		`CREATE TABLE IF NOT EXISTS gpu_models (
			name VARCHAR(255) PRIMARY KEY,
			size_bytes BIGINT DEFAULT 0,
			parameter_count VARCHAR(64),
			quantized TINYINT(1) DEFAULT 0,
			serving TINYINT(1) DEFAULT 0,
			port INT DEFAULT 0,
			node_id VARCHAR(64),
			replicas INT DEFAULT 0,
			last_pulled DATETIME,
			INDEX idx_node (node_id)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLStore) UpsertNode(node *models.GPUNode) error {
	if node == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if node.CreatedAt.IsZero() {
		node.CreatedAt = time.Now().UTC()
	}
	node.UpdatedAt = time.Now().UTC()
	gpus, _ := json.Marshal(node.GPUs)
	labels, _ := json.Marshal(node.Labels)
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO gpu_nodes (id, name, address, gpus, status, labels, total_vram_mb, used_vram_mb, gpu_errors, last_heartbeat, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), address=VALUES(address), gpus=VALUES(gpus),
		   status=VALUES(status), labels=VALUES(labels), total_vram_mb=VALUES(total_vram_mb),
		   used_vram_mb=VALUES(used_vram_mb), gpu_errors=VALUES(gpu_errors),
		   last_heartbeat=VALUES(last_heartbeat), updated_at=VALUES(updated_at)`,
		node.ID, node.Name, node.Address, gpus, node.Status, labels, node.TotalVRAMMB, node.UsedVRAMMB,
		node.GPUErrors, nullTime(node.LastHeartbeat), nullTime(node.CreatedAt), nullTime(node.UpdatedAt))
	return err
}

func (m *MySQLStore) GetNode(id string) (*models.GPUNode, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT id, name, address, gpus, status, labels, total_vram_mb, used_vram_mb, gpu_errors, last_heartbeat, created_at, updated_at FROM gpu_nodes WHERE id=?`, id)
	n := &models.GPUNode{}
	var gpus, labels []byte
	var lastHeartbeat, createdAt, updatedAt sql.NullTime
	if err := row.Scan(&n.ID, &n.Name, &n.Address, &gpus, &n.Status, &labels, &n.TotalVRAMMB, &n.UsedVRAMMB, &n.GPUErrors, &lastHeartbeat, &createdAt, &updatedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetNode 查询失败: %v", err)
		}
		return nil, false
	}
	if len(gpus) > 0 {
		_ = json.Unmarshal(gpus, &n.GPUs)
	}
	if len(labels) > 0 {
		_ = json.Unmarshal(labels, &n.Labels)
	}
	if lastHeartbeat.Valid {
		n.LastHeartbeat = lastHeartbeat.Time
	}
	if createdAt.Valid {
		n.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		n.UpdatedAt = updatedAt.Time
	}
	return n, true
}

func (m *MySQLStore) ListNodes(status string) []*models.GPUNode {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, name, address, gpus, status, labels, total_vram_mb, used_vram_mb, gpu_errors, last_heartbeat, created_at, updated_at FROM gpu_nodes`
	var args []interface{}
	if status != "" {
		q += " WHERE status=?"
		args = append(args, status)
	}
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListNodes 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.GPUNode
	for rows.Next() {
		n := &models.GPUNode{}
		var gpus, labels []byte
		var lastHeartbeat, createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&n.ID, &n.Name, &n.Address, &gpus, &n.Status, &labels, &n.TotalVRAMMB, &n.UsedVRAMMB, &n.GPUErrors, &lastHeartbeat, &createdAt, &updatedAt); err != nil {
			continue
		}
		if len(gpus) > 0 {
			_ = json.Unmarshal(gpus, &n.GPUs)
		}
		if len(labels) > 0 {
			_ = json.Unmarshal(labels, &n.Labels)
		}
		if lastHeartbeat.Valid {
			n.LastHeartbeat = lastHeartbeat.Time
		}
		if createdAt.Valid {
			n.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			n.UpdatedAt = updatedAt.Time
		}
		out = append(out, n)
	}
	return out
}

func (m *MySQLStore) DeleteNode(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx, `DELETE FROM gpu_nodes WHERE id=?`, id)
	if err != nil {
		log.Printf("[store] DeleteNode 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (m *MySQLStore) CreateWorkload(w *models.Workload) (*models.Workload, error) {
	if w == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now().UTC()
	}
	w.UpdatedAt = time.Now().UTC()
	gpuReq, _ := json.Marshal(w.GPURequest)
	nodeIDs, _ := json.Marshal(w.NodeIDs)
	cmd, _ := json.Marshal(w.Command)
	env, _ := json.Marshal(w.Env)
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO gpu_workloads (id, name, tenant_id, type, status, gpu_request, node_ids, priority, image, command, env, replicas, model_name, created_at, updated_at, started_at, finished_at, error_message)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), tenant_id=VALUES(tenant_id), type=VALUES(type),
		   status=VALUES(status), gpu_request=VALUES(gpu_request), node_ids=VALUES(node_ids),
		   priority=VALUES(priority), image=VALUES(image), command=VALUES(command), env=VALUES(env),
		   replicas=VALUES(replicas), model_name=VALUES(model_name), updated_at=VALUES(updated_at),
		   started_at=VALUES(started_at), finished_at=VALUES(finished_at), error_message=VALUES(error_message)`,
		w.ID, w.Name, w.TenantID, w.Type, w.Status, gpuReq, nodeIDs, w.Priority, w.Image, cmd, env,
		w.Replicas, w.ModelName, nullTime(w.CreatedAt), nullTime(w.UpdatedAt), nullTimePtr(w.StartedAt), nullTimePtr(w.FinishedAt), nullString(w.ErrorMsg))
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (m *MySQLStore) GetWorkload(id string) (*models.Workload, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT id, name, tenant_id, type, status, gpu_request, node_ids, priority, image, command, env, replicas, model_name, created_at, updated_at, started_at, finished_at, error_message FROM gpu_workloads WHERE id=?`, id)
	w := &models.Workload{}
	var gpuReq, nodeIDs, cmd, env []byte
	var createdAt, updatedAt, startedAt, finishedAt sql.NullTime
	if err := row.Scan(&w.ID, &w.Name, &w.TenantID, &w.Type, &w.Status, &gpuReq, &nodeIDs, &w.Priority, &w.Image, &cmd, &env, &w.Replicas, &w.ModelName, &createdAt, &updatedAt, &startedAt, &finishedAt, &w.ErrorMsg); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetWorkload 查询失败: %v", err)
		}
		return nil, false
	}
	if len(gpuReq) > 0 {
		_ = json.Unmarshal(gpuReq, &w.GPURequest)
	}
	if len(nodeIDs) > 0 {
		_ = json.Unmarshal(nodeIDs, &w.NodeIDs)
	}
	if len(cmd) > 0 {
		_ = json.Unmarshal(cmd, &w.Command)
	}
	if len(env) > 0 {
		_ = json.Unmarshal(env, &w.Env)
	}
	if createdAt.Valid {
		w.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		w.UpdatedAt = updatedAt.Time
	}
	if startedAt.Valid {
		w.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		w.FinishedAt = &finishedAt.Time
	}
	return w, true
}

func (m *MySQLStore) UpdateWorkload(w *models.Workload) error {
	if w == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w.UpdatedAt = time.Now().UTC()
	gpuReq, _ := json.Marshal(w.GPURequest)
	nodeIDs, _ := json.Marshal(w.NodeIDs)
	cmd, _ := json.Marshal(w.Command)
	env, _ := json.Marshal(w.Env)
	_, err := m.db.ExecContext(ctx,
		`UPDATE gpu_workloads SET name=?, tenant_id=?, type=?, status=?, gpu_request=?, node_ids=?, priority=?, image=?, command=?, env=?, replicas=?, model_name=?, updated_at=?, started_at=?, finished_at=?, error_message=? WHERE id=?`,
		w.Name, w.TenantID, w.Type, w.Status, gpuReq, nodeIDs, w.Priority, w.Image, cmd, env,
		w.Replicas, w.ModelName, nullTime(w.UpdatedAt), nullTimePtr(w.StartedAt), nullTimePtr(w.FinishedAt), nullString(w.ErrorMsg), w.ID)
	return err
}

func (m *MySQLStore) ListWorkloads(status string) []*models.Workload {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, name, tenant_id, type, status, gpu_request, node_ids, priority, image, command, env, replicas, model_name, created_at, updated_at, started_at, finished_at, error_message FROM gpu_workloads`
	var args []interface{}
	if status != "" {
		q += " WHERE status=?"
		args = append(args, status)
	}
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListWorkloads 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.Workload
	for rows.Next() {
		w := &models.Workload{}
		var gpuReq, nodeIDs, cmd, env []byte
		var createdAt, updatedAt, startedAt, finishedAt sql.NullTime
		if err := rows.Scan(&w.ID, &w.Name, &w.TenantID, &w.Type, &w.Status, &gpuReq, &nodeIDs, &w.Priority, &w.Image, &cmd, &env, &w.Replicas, &w.ModelName, &createdAt, &updatedAt, &startedAt, &finishedAt, &w.ErrorMsg); err != nil {
			continue
		}
		if len(gpuReq) > 0 {
			_ = json.Unmarshal(gpuReq, &w.GPURequest)
		}
		if len(nodeIDs) > 0 {
			_ = json.Unmarshal(nodeIDs, &w.NodeIDs)
		}
		if len(cmd) > 0 {
			_ = json.Unmarshal(cmd, &w.Command)
		}
		if len(env) > 0 {
			_ = json.Unmarshal(env, &w.Env)
		}
		if createdAt.Valid {
			w.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			w.UpdatedAt = updatedAt.Time
		}
		if startedAt.Valid {
			w.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			w.FinishedAt = &finishedAt.Time
		}
		out = append(out, w)
	}
	return out
}

func (m *MySQLStore) UpsertModel(model *models.GPUModel) error {
	if model == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO gpu_models (name, size_bytes, parameter_count, quantized, serving, port, node_id, replicas, last_pulled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE size_bytes=VALUES(size_bytes), parameter_count=VALUES(parameter_count),
		   quantized=VALUES(quantized), serving=VALUES(serving), port=VALUES(port),
		   node_id=VALUES(node_id), replicas=VALUES(replicas), last_pulled=VALUES(last_pulled)`,
		model.Name, model.SizeBytes, model.ParameterCount, boolToInt(model.Quantized), boolToInt(model.Serving),
		model.Port, nullString(model.NodeID), model.Replicas, nullTime(model.LastPulled))
	return err
}

func (m *MySQLStore) GetModel(name string) (*models.GPUModel, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT name, size_bytes, parameter_count, quantized, serving, port, node_id, replicas, last_pulled FROM gpu_models WHERE name=?`, name)
	mod := &models.GPUModel{}
	var lastPulled sql.NullTime
	if err := row.Scan(&mod.Name, &mod.SizeBytes, &mod.ParameterCount, &mod.Quantized, &mod.Serving, &mod.Port, &mod.NodeID, &mod.Replicas, &lastPulled); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetModel 查询失败: %v", err)
		}
		return nil, false
	}
	if lastPulled.Valid {
		mod.LastPulled = lastPulled.Time
	}
	return mod, true
}

func (m *MySQLStore) ListModels() []*models.GPUModel {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := m.db.QueryContext(ctx, `SELECT name, size_bytes, parameter_count, quantized, serving, port, node_id, replicas, last_pulled FROM gpu_models`)
	if err != nil {
		log.Printf("[store] ListModels 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.GPUModel
	for rows.Next() {
		mod := &models.GPUModel{}
		var lastPulled sql.NullTime
		if err := rows.Scan(&mod.Name, &mod.SizeBytes, &mod.ParameterCount, &mod.Quantized, &mod.Serving, &mod.Port, &mod.NodeID, &mod.Replicas, &lastPulled); err != nil {
			continue
		}
		if lastPulled.Valid {
			mod.LastPulled = lastPulled.Time
		}
		out = append(out, mod)
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

func nullTimePtr(t *time.Time) interface{} {
	if t == nil || t.IsZero() {
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
