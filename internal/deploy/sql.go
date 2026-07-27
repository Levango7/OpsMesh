package deploy

import (
	"context"
	"database/sql"
	"time"
)

// SQLDeployStore MySQL 后端（U-04 数据本地化，私有部署）。
// 复用控制面 SQLStore 的 *sql.DB（共享连接池），自身不关闭该连接。
type SQLDeployStore struct {
	db *sql.DB
}

// NewSQL 构造 MySQL 部署存储并幂等建表。
func NewSQL(db *sql.DB) (*SQLDeployStore, error) {
	s := &SQLDeployStore{db: db}
	if err := s.initSchema(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLDeployStore) initSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS deploy_tasks (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			name VARCHAR(255) NOT NULL,
			type VARCHAR(32) NOT NULL,
			repo_url TEXT,
			content MEDIUMTEXT,
			path VARCHAR(512),
			target_ids TEXT,
			task_ids TEXT,
			created_by VARCHAR(64),
			status VARCHAR(16) DEFAULT 'created',
			created_at DATETIME,
			updated_at DATETIME
		)`)
	return err
}

func (s *SQLDeployStore) Create(ctx context.Context, dt *DeployTask) (*DeployTask, error) {
	if dt == nil {
		return nil, errInvalid("nil")
	}
	if dt.TenantID == "" {
		return nil, errInvalid("tenant_id required")
	}
	if err := dt.Valid(); err != nil {
		return nil, err
	}
	if dt.Status == "" {
		dt.Status = StatusCreated
	}
	now := time.Now()
	if dt.CreatedAt.IsZero() {
		dt.CreatedAt = now
	}
	dt.UpdatedAt = now
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO deploy_tasks
			(tenant_id, name, type, repo_url, content, path, target_ids, task_ids, created_by, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dt.TenantID, dt.Name, dt.Type, nullStr(dt.RepoURL), nullStr(dt.Content),
		nullStr(dt.Path), nullStr(dt.TargetIDs), nullStr(dt.TaskIDs), nullStr(dt.CreatedBy),
		dt.Status, dt.CreatedAt, dt.UpdatedAt)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	dt.ID = id
	cp := *dt
	return &cp, nil
}

func (s *SQLDeployStore) Get(ctx context.Context, id int64, tenantID string) (*DeployTask, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, type, repo_url, content, path, target_ids, task_ids, created_by, status, created_at, updated_at
		FROM deploy_tasks WHERE id = ? AND (? = '' OR tenant_id = ?)`,
		id, tenantID, tenantID)
	return scanDeploy(row)
}

func (s *SQLDeployStore) Update(ctx context.Context, dt *DeployTask) error {
	if dt == nil {
		return errInvalid("nil")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE deploy_tasks
		SET name=?, type=?, repo_url=?, content=?, path=?, target_ids=?, task_ids=?, status=?, updated_at=?
		WHERE id = ? AND (? = '' OR tenant_id = ?)`,
		dt.Name, dt.Type, nullStr(dt.RepoURL), nullStr(dt.Content), nullStr(dt.Path),
		nullStr(dt.TargetIDs), nullStr(dt.TaskIDs), dt.Status, time.Now(),
		dt.ID, dt.TenantID, dt.TenantID)
	return err
}

func (s *SQLDeployStore) List(ctx context.Context, tenantID, status string) ([]DeployTask, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, name, type, repo_url, content, path, target_ids, task_ids, created_by, status, created_at, updated_at
		FROM deploy_tasks WHERE (? = '' OR tenant_id = ?) AND (? = '' OR status = ?)`,
		tenantID, tenantID, status, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DeployTask, 0)
	for rows.Next() {
		dt, err := scanDeployRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *dt)
	}
	return out, rows.Err()
}

func (s *SQLDeployStore) Delete(ctx context.Context, id int64, tenantID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM deploy_tasks WHERE id = ? AND (? = '' OR tenant_id = ?)`,
		id, tenantID, tenantID)
	return err
}

// scanDeploy 扫描单行（*sql.Row）。
func scanDeploy(row *sql.Row) (*DeployTask, error) {
	dt := &DeployTask{}
	var (
		repoURL, content, path, targetIDs, taskIDs, createdBy sql.NullString
		createdAt, updatedAt                                                          time.Time
	)
	err := row.Scan(&dt.ID, &dt.TenantID, &dt.Name, &dt.Type,
		&repoURL, &content, &path, &targetIDs, &taskIDs, &createdBy,
		&dt.Status, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	dt.RepoURL = repoURL.String
	dt.Content = content.String
	dt.Path = path.String
	dt.TargetIDs = targetIDs.String
	dt.TaskIDs = taskIDs.String
	dt.CreatedBy = createdBy.String
	dt.CreatedAt = createdAt
	dt.UpdatedAt = updatedAt
	return dt, nil
}

// scanDeployRow 扫描多行结果的一行（*sql.Rows）。
func scanDeployRow(rows *sql.Rows) (*DeployTask, error) {
	dt := &DeployTask{}
	var (
		repoURL, content, path, targetIDs, taskIDs, createdBy sql.NullString
		createdAt, updatedAt                                                          time.Time
	)
	err := rows.Scan(&dt.ID, &dt.TenantID, &dt.Name, &dt.Type,
		&repoURL, &content, &path, &targetIDs, &taskIDs, &createdBy,
		&dt.Status, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	dt.RepoURL = repoURL.String
	dt.Content = content.String
	dt.Path = path.String
	dt.TargetIDs = targetIDs.String
	dt.TaskIDs = taskIDs.String
	dt.CreatedBy = createdBy.String
	dt.CreatedAt = createdAt
	dt.UpdatedAt = updatedAt
	return dt, nil
}

// nullStr 空串转 SQL NULL（与 logstore 同语义）。
func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
