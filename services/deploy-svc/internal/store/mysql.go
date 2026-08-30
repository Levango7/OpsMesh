package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/models"
	_ "github.com/go-sql-driver/mysql"
)

// MySQLStore is a MySQL-backed implementation of Store.
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
		`CREATE TABLE IF NOT EXISTS deployments (
			id VARCHAR(64) PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			name VARCHAR(255) NOT NULL,
			type VARCHAR(32),
			repo_url VARCHAR(512),
			content MEDIUMTEXT,
			path VARCHAR(512),
			target_ids JSON,
			status VARCHAR(32) DEFAULT 'pending',
			strategy VARCHAR(32),
			canary_weight INT DEFAULT 0,
			auto_rollback TINYINT(1) DEFAULT 0,
			created_by VARCHAR(64),
			error_message TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			INDEX idx_tenant (tenant_id),
			INDEX idx_status (status)
		)`,
		`CREATE TABLE IF NOT EXISTS deploy_templates (
			id VARCHAR(64) PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			type VARCHAR(32),
			repo_url VARCHAR(512),
			content MEDIUMTEXT,
			path VARCHAR(512),
			parameters JSON,
			created_by VARCHAR(64),
			created_at DATETIME,
			updated_at DATETIME,
			INDEX idx_tenant (tenant_id)
		)`,
		`CREATE TABLE IF NOT EXISTS canaries (
			id VARCHAR(64) PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			deployment_id VARCHAR(64) NOT NULL,
			name VARCHAR(255) NOT NULL,
			weight INT DEFAULT 0,
			status VARCHAR(32) DEFAULT 'pending',
			success_count INT DEFAULT 0,
			failure_count INT DEFAULT 0,
			created_by VARCHAR(64),
			created_at DATETIME,
			updated_at DATETIME,
			INDEX idx_tenant (tenant_id),
			INDEX idx_deployment (deployment_id),
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

func (m *MySQLStore) insertDeployment(ctx context.Context, d *models.Deployment) error {
	targetIDs, _ := json.Marshal(d.TargetIDs)
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO deployments (id, tenant_id, name, type, repo_url, content, path, target_ids, status, strategy, canary_weight, auto_rollback, created_by, error_message, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE tenant_id=VALUES(tenant_id), name=VALUES(name), type=VALUES(type),
		   repo_url=VALUES(repo_url), content=VALUES(content), path=VALUES(path), target_ids=VALUES(target_ids),
		   status=VALUES(status), strategy=VALUES(strategy), canary_weight=VALUES(canary_weight),
		   auto_rollback=VALUES(auto_rollback), created_by=VALUES(created_by), error_message=VALUES(error_message), updated_at=VALUES(updated_at)`,
		d.ID, d.TenantID, d.Name, d.Type, d.RepoURL, d.Content, d.Path,
		targetIDs, d.Status, d.Strategy, d.CanaryWeight, d.AutoRollback,
		d.CreatedBy, d.ErrorMessage, nullTime(d.CreatedAt), nullTime(d.UpdatedAt))
	return err
}

func (m *MySQLStore) scanDeployment(row rowScanner) *models.Deployment {
	d := &models.Deployment{}
	var createdAt, updatedAt sql.NullTime
	var targetIDs []byte
	if err := row.Scan(&d.ID, &d.TenantID, &d.Name, &d.Type, &d.RepoURL, &d.Content, &d.Path,
		&targetIDs, &d.Status, &d.Strategy, &d.CanaryWeight, &d.AutoRollback, &d.CreatedBy,
		&d.ErrorMessage, &createdAt, &updatedAt); err != nil {
		return nil
	}
	if len(targetIDs) > 0 {
		_ = json.Unmarshal(targetIDs, &d.TargetIDs)
	}
	if createdAt.Valid {
		d.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		d.UpdatedAt = updatedAt.Time
	}
	return d
}

func (m *MySQLStore) CreateDeployment(d *models.Deployment) (*models.Deployment, error) {
	if d == nil {
		return nil, fmt.Errorf("nil deployment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.insertDeployment(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (m *MySQLStore) GetDeployment(id, tenantID string) (*models.Deployment, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, type, repo_url, content, path, target_ids, status, strategy, canary_weight, auto_rollback, created_by, error_message, created_at, updated_at FROM deployments WHERE id=?`, id)
	d := m.scanDeployment(row)
	if d == nil {
		return nil, ErrNotFound
	}
	if tenantID != "" && d.TenantID != tenantID {
		return nil, ErrTenantMismatch
	}
	return d, nil
}

func (m *MySQLStore) ListDeployments(tenantID, status string) ([]*models.Deployment, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, tenant_id, name, type, repo_url, content, path, target_ids, status, strategy, canary_weight, auto_rollback, created_by, error_message, created_at, updated_at FROM deployments`
	var args []interface{}
	var where []string
	if tenantID != "" {
		where = append(where, "tenant_id=?")
		args = append(args, tenantID)
	}
	if status != "" {
		where = append(where, "status=?")
		args = append(args, status)
	}
	if len(where) > 0 {
		q += " WHERE " + joinWhere(where)
	}
	q += " ORDER BY created_at DESC"
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Deployment
	for rows.Next() {
		if d := m.scanDeployment(rows); d != nil {
			out = append(out, d)
		}
	}
	return out, nil
}

func (m *MySQLStore) UpdateDeployment(d *models.Deployment) error {
	if d == nil {
		return fmt.Errorf("nil deployment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx,
		`UPDATE deployments SET tenant_id=?, name=?, type=?, repo_url=?, content=?, path=?, target_ids=?, status=?, strategy=?, canary_weight=?, auto_rollback=?, created_by=?, error_message=?, updated_at=? WHERE id=?`,
		d.TenantID, d.Name, d.Type, d.RepoURL, d.Content, d.Path,
		mustJSON(d.TargetIDs), d.Status, d.Strategy, d.CanaryWeight, d.AutoRollback,
		d.CreatedBy, d.ErrorMessage, time.Now().UTC(), d.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (m *MySQLStore) CreateTemplate(t *models.Template) (*models.Template, error) {
	if t == nil {
		return nil, fmt.Errorf("nil template")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	params, _ := json.Marshal(t.Parameters)
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO deploy_templates (id, tenant_id, name, description, type, repo_url, content, path, parameters, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE tenant_id=VALUES(tenant_id), name=VALUES(name), description=VALUES(description),
		   type=VALUES(type), repo_url=VALUES(repo_url), content=VALUES(content), path=VALUES(path),
		   parameters=VALUES(parameters), created_by=VALUES(created_by), updated_at=VALUES(updated_at)`,
		t.ID, t.TenantID, t.Name, t.Description, t.Type, t.RepoURL, t.Content, t.Path,
		params, t.CreatedBy, nullTime(t.CreatedAt), nullTime(t.UpdatedAt))
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (m *MySQLStore) GetTemplate(id, tenantID string) (*models.Template, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, description, type, repo_url, content, path, parameters, created_by, created_at, updated_at FROM deploy_templates WHERE id=?`, id)
	t := m.scanTemplate(row)
	if t == nil {
		return nil, ErrNotFound
	}
	if tenantID != "" && t.TenantID != tenantID {
		return nil, ErrTenantMismatch
	}
	return t, nil
}

func (m *MySQLStore) scanTemplate(row rowScanner) *models.Template {
	t := &models.Template{}
	var createdAt, updatedAt sql.NullTime
	var params []byte
	if err := row.Scan(&t.ID, &t.TenantID, &t.Name, &t.Description, &t.Type, &t.RepoURL, &t.Content, &t.Path, &params, &t.CreatedBy, &createdAt, &updatedAt); err != nil {
		return nil
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &t.Parameters)
	}
	if createdAt.Valid {
		t.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		t.UpdatedAt = updatedAt.Time
	}
	return t
}

func (m *MySQLStore) UpdateTemplate(t *models.Template) error {
	if t == nil {
		return fmt.Errorf("nil template")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx,
		`UPDATE deploy_templates SET tenant_id=?, name=?, description=?, type=?, repo_url=?, content=?, path=?, parameters=?, created_by=?, updated_at=? WHERE id=?`,
		t.TenantID, t.Name, t.Description, t.Type, t.RepoURL, t.Content, t.Path,
		mustJSON(t.Parameters), t.CreatedBy, time.Now().UTC(), t.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (m *MySQLStore) DeleteTemplate(id, tenantID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var res sql.Result
	var err error
	if tenantID != "" {
		res, err = m.db.ExecContext(ctx, `DELETE FROM deploy_templates WHERE id=? AND tenant_id=?`, id, tenantID)
	} else {
		res, err = m.db.ExecContext(ctx, `DELETE FROM deploy_templates WHERE id=?`, id)
	}
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (m *MySQLStore) ListTemplates(tenantID string) ([]*models.Template, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, tenant_id, name, description, type, repo_url, content, path, parameters, created_by, created_at, updated_at FROM deploy_templates`
	var args []interface{}
	if tenantID != "" {
		q += " WHERE tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY created_at DESC"
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Template
	for rows.Next() {
		if t := m.scanTemplate(rows); t != nil {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *MySQLStore) CreateStrategy(s *models.Strategy) (*models.Strategy, error) {
	if s == nil {
		return nil, fmt.Errorf("nil strategy")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO deploy_templates (id, tenant_id, name, description, type, parameters, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE tenant_id=VALUES(tenant_id), name=VALUES(name), description=VALUES(description),
		   type=VALUES(type), parameters=VALUES(parameters), created_by=VALUES(created_by), updated_at=VALUES(updated_at)`,
		s.ID, s.TenantID, s.Name, s.Description, s.Type, mustJSON(map[string]interface{}{
			"canary_weight":   s.CanaryWeight,
			"max_unavailable": s.MaxUnavailable,
			"max_surge":       s.MaxSurge,
			"auto_rollback":   s.AutoRollback,
			"timeout_seconds": s.TimeoutSeconds,
		}), s.CreatedBy, nullTime(s.CreatedAt), nullTime(s.UpdatedAt))
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (m *MySQLStore) GetStrategy(id, tenantID string) (*models.Strategy, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, description, type, parameters, created_by, created_at, updated_at FROM deploy_templates WHERE id=? AND type IN ('rolling','canary','bluegreen')`, id)
	s := m.scanStrategy(row)
	if s == nil {
		return nil, ErrNotFound
	}
	if tenantID != "" && s.TenantID != tenantID {
		return nil, ErrTenantMismatch
	}
	return s, nil
}

func (m *MySQLStore) scanStrategy(row rowScanner) *models.Strategy {
	s := &models.Strategy{}
	var createdAt, updatedAt sql.NullTime
	var params []byte
	if err := row.Scan(&s.ID, &s.TenantID, &s.Name, &s.Description, &s.Type, &params, &s.CreatedBy, &createdAt, &updatedAt); err != nil {
		return nil
	}
	if len(params) > 0 {
		var m map[string]interface{}
		_ = json.Unmarshal(params, &m)
		if v, ok := m["canary_weight"].(float64); ok {
			s.CanaryWeight = int(v)
		}
		if v, ok := m["max_unavailable"].(float64); ok {
			s.MaxUnavailable = int(v)
		}
		if v, ok := m["max_surge"].(float64); ok {
			s.MaxSurge = int(v)
		}
		if v, ok := m["auto_rollback"].(bool); ok {
			s.AutoRollback = v
		}
		if v, ok := m["timeout_seconds"].(float64); ok {
			s.TimeoutSeconds = int(v)
		}
	}
	if createdAt.Valid {
		s.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		s.UpdatedAt = updatedAt.Time
	}
	return s
}

func (m *MySQLStore) UpdateStrategy(s *models.Strategy) error {
	if s == nil {
		return fmt.Errorf("nil strategy")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx,
		`UPDATE deploy_templates SET tenant_id=?, name=?, description=?, type=?, parameters=?, created_by=?, updated_at=? WHERE id=? AND type IN ('rolling','canary','bluegreen')`,
		s.TenantID, s.Name, s.Description, s.Type, mustJSON(map[string]interface{}{
			"canary_weight":   s.CanaryWeight,
			"max_unavailable": s.MaxUnavailable,
			"max_surge":       s.MaxSurge,
			"auto_rollback":   s.AutoRollback,
			"timeout_seconds": s.TimeoutSeconds,
		}), s.CreatedBy, time.Now().UTC(), s.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (m *MySQLStore) DeleteStrategy(id, tenantID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var res sql.Result
	var err error
	if tenantID != "" {
		res, err = m.db.ExecContext(ctx, `DELETE FROM deploy_templates WHERE id=? AND tenant_id=? AND type IN ('rolling','canary','bluegreen')`, id, tenantID)
	} else {
		res, err = m.db.ExecContext(ctx, `DELETE FROM deploy_templates WHERE id=? AND type IN ('rolling','canary','bluegreen')`, id)
	}
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (m *MySQLStore) ListStrategies(tenantID string) ([]*models.Strategy, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, tenant_id, name, description, type, parameters, created_by, created_at, updated_at FROM deploy_templates WHERE type IN ('rolling','canary','bluegreen')`
	var args []interface{}
	if tenantID != "" {
		q += " AND tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY created_at DESC"
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Strategy
	for rows.Next() {
		if s := m.scanStrategy(rows); s != nil {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *MySQLStore) CreateCanary(c *models.Canary) (*models.Canary, error) {
	if c == nil {
		return nil, fmt.Errorf("nil canary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO canaries (id, tenant_id, deployment_id, name, weight, status, success_count, failure_count, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE tenant_id=VALUES(tenant_id), deployment_id=VALUES(deployment_id), name=VALUES(name),
		   weight=VALUES(weight), status=VALUES(status), success_count=VALUES(success_count), failure_count=VALUES(failure_count),
		   created_by=VALUES(created_by), updated_at=VALUES(updated_at)`,
		c.ID, c.TenantID, c.DeploymentID, c.Name, c.Weight, c.Status, c.SuccessCount, c.FailureCount,
		c.CreatedBy, nullTime(c.CreatedAt), nullTime(c.UpdatedAt))
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (m *MySQLStore) GetCanary(id, tenantID string) (*models.Canary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, deployment_id, name, weight, status, success_count, failure_count, created_by, created_at, updated_at FROM canaries WHERE id=?`, id)
	c := m.scanCanary(row)
	if c == nil {
		return nil, ErrNotFound
	}
	if tenantID != "" && c.TenantID != tenantID {
		return nil, ErrTenantMismatch
	}
	return c, nil
}

func (m *MySQLStore) scanCanary(row rowScanner) *models.Canary {
	c := &models.Canary{}
	var createdAt, updatedAt sql.NullTime
	if err := row.Scan(&c.ID, &c.TenantID, &c.DeploymentID, &c.Name, &c.Weight, &c.Status, &c.SuccessCount, &c.FailureCount, &c.CreatedBy, &createdAt, &updatedAt); err != nil {
		return nil
	}
	if createdAt.Valid {
		c.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		c.UpdatedAt = updatedAt.Time
	}
	return c
}

func (m *MySQLStore) UpdateCanary(c *models.Canary) error {
	if c == nil {
		return fmt.Errorf("nil canary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx,
		`UPDATE canaries SET tenant_id=?, deployment_id=?, name=?, weight=?, status=?, success_count=?, failure_count=?, created_by=?, updated_at=? WHERE id=?`,
		c.TenantID, c.DeploymentID, c.Name, c.Weight, c.Status, c.SuccessCount, c.FailureCount,
		c.CreatedBy, time.Now().UTC(), c.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (m *MySQLStore) ListCanaries(tenantID, status string) ([]*models.Canary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, tenant_id, deployment_id, name, weight, status, success_count, failure_count, created_by, created_at, updated_at FROM canaries`
	var args []interface{}
	var where []string
	if tenantID != "" {
		where = append(where, "tenant_id=?")
		args = append(args, tenantID)
	}
	if status != "" {
		where = append(where, "status=?")
		args = append(args, status)
	}
	if len(where) > 0 {
		q += " WHERE " + joinWhere(where)
	}
	q += " ORDER BY created_at DESC"
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Canary
	for rows.Next() {
		if c := m.scanCanary(rows); c != nil {
			out = append(out, c)
		}
	}
	return out, nil
}

// rowScanner is the interface used by scanXxx helpers.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
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

// 编译期断言：MySQLStore 实现 Store 接口。
var _ Store = (*MySQLStore)(nil)
