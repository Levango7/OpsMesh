package deploy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
			updated_at DATETIME,
			strategy VARCHAR(16) DEFAULT '',
			canary_weight INT DEFAULT 0,
			auto_rollback TINYINT(1) DEFAULT 0,
			gate JSON,
			canary_targets TEXT,
			stable_targets TEXT
		)`)
	if err != nil {
		return err
	}
	// 兼容旧表：幂等添加灰度发布列（已存在则忽略错误）。
	for _, alter := range []string{
		`ALTER TABLE deploy_tasks ADD COLUMN strategy VARCHAR(16) DEFAULT ''`,
		`ALTER TABLE deploy_tasks ADD COLUMN canary_weight INT DEFAULT 0`,
		`ALTER TABLE deploy_tasks ADD COLUMN auto_rollback TINYINT(1) DEFAULT 0`,
		`ALTER TABLE deploy_tasks ADD COLUMN gate JSON`,
		`ALTER TABLE deploy_tasks ADD COLUMN canary_targets TEXT`,
		`ALTER TABLE deploy_tasks ADD COLUMN stable_targets TEXT`,
	} {
		// 忽略 "Duplicate column" 错误（列已存在），其他错误向上返回。
		if _, e := s.db.ExecContext(ctx, alter); e != nil && !isDupColumnErr(e) {
			return e
		}
	}
	return nil
}

// isDupColumnErr 判断是否为"列已存在"错误（MySQL 1060）。
// 用于幂等 ALTER TABLE ADD COLUMN，兼容旧表升级。
func isDupColumnErr(e error) bool {
	return e != nil && containsAny(e.Error(), "1060", "Duplicate column")
}

// containsAny 简化版 strings.ContainsAny（避免在 sql.go 顶部 import strings 仅为此一处）。
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && indexOf(s, sub) >= 0 {
			return true
		}
	}
	return false
}

// indexOf 字符串包含判断（避免 import strings）。
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
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
	gateJSON, err := marshalGate(dt.Gate)
	if err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO deploy_tasks
			(tenant_id, name, type, repo_url, content, path, target_ids, task_ids, created_by, status, created_at, updated_at,
			 strategy, canary_weight, auto_rollback, gate, canary_targets, stable_targets)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dt.TenantID, dt.Name, dt.Type, nullStr(dt.RepoURL), nullStr(dt.Content),
		nullStr(dt.Path), nullStr(dt.TargetIDs), nullStr(dt.TaskIDs), nullStr(dt.CreatedBy),
		dt.Status, dt.CreatedAt, dt.UpdatedAt,
		dt.Strategy, dt.CanaryWeight, dt.AutoRollback, gateJSON, nullStr(dt.CanaryTargets), nullStr(dt.StableTargets))
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
		SELECT id, tenant_id, name, type, repo_url, content, path, target_ids, task_ids, created_by, status, created_at, updated_at,
		       strategy, canary_weight, auto_rollback, gate, canary_targets, stable_targets
		FROM deploy_tasks WHERE id = ? AND (? = '' OR tenant_id = ?)`,
		id, tenantID, tenantID)
	return scanDeploy(row)
}

func (s *SQLDeployStore) Update(ctx context.Context, dt *DeployTask) error {
	if dt == nil {
		return errInvalid("nil")
	}
	gateJSON, err := marshalGate(dt.Gate)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE deploy_tasks
		SET name=?, type=?, repo_url=?, content=?, path=?, target_ids=?, task_ids=?, status=?, updated_at=?,
		    strategy=?, canary_weight=?, auto_rollback=?, gate=?, canary_targets=?, stable_targets=?
		WHERE id = ? AND (? = '' OR tenant_id = ?)`,
		dt.Name, dt.Type, nullStr(dt.RepoURL), nullStr(dt.Content), nullStr(dt.Path),
		nullStr(dt.TargetIDs), nullStr(dt.TaskIDs), dt.Status, time.Now(),
		dt.Strategy, dt.CanaryWeight, dt.AutoRollback, gateJSON, nullStr(dt.CanaryTargets), nullStr(dt.StableTargets),
		dt.ID, dt.TenantID, dt.TenantID)
	return err
}

func (s *SQLDeployStore) List(ctx context.Context, tenantID, status string) ([]DeployTask, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, name, type, repo_url, content, path, target_ids, task_ids, created_by, status, created_at, updated_at,
		       strategy, canary_weight, auto_rollback, gate, canary_targets, stable_targets
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

// marshalGate 把 GateConfig 序列化为 JSON 字符串（nil 返回 NULL）。
// MySQL JSON 列接受字符串字面量，driver 会正确处理。
func marshalGate(g *GateConfig) (interface{}, error) {
	if g == nil {
		return nil, nil
	}
	b, err := json.Marshal(g)
	if err != nil {
		return nil, errInvalid(fmt.Sprintf("gate marshal: %v", err))
	}
	return string(b), nil
}

// unmarshalGate 把数据库 JSON 列反序列化为 GateConfig（NULL/空串返回 nil）。
func unmarshalGate(raw []byte) *GateConfig {
	if len(raw) == 0 {
		return nil
	}
	var g GateConfig
	if json.Unmarshal(raw, &g) == nil {
		return &g
	}
	return nil
}

// scanDeploy 扫描单行（*sql.Row）。
func scanDeploy(row *sql.Row) (*DeployTask, error) {
	dt := &DeployTask{}
	var (
		repoURL, content, path, targetIDs, taskIDs, createdBy sql.NullString
		canaryTargets, stableTargets                          sql.NullString
		strategy                                              sql.NullString
		gateRaw                                               []byte
		canaryWeight                                          int
		autoRollback                                          bool
		createdAt, updatedAt                                  time.Time
	)
	err := row.Scan(&dt.ID, &dt.TenantID, &dt.Name, &dt.Type,
		&repoURL, &content, &path, &targetIDs, &taskIDs, &createdBy,
		&dt.Status, &createdAt, &updatedAt,
		&strategy, &canaryWeight, &autoRollback, &gateRaw, &canaryTargets, &stableTargets)
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
	dt.Strategy = strategy.String
	dt.CanaryWeight = canaryWeight
	dt.AutoRollback = autoRollback
	dt.Gate = unmarshalGate(gateRaw)
	dt.CanaryTargets = canaryTargets.String
	dt.StableTargets = stableTargets.String
	return dt, nil
}

// scanDeployRow 扫描多行结果的一行（*sql.Rows）。
func scanDeployRow(rows *sql.Rows) (*DeployTask, error) {
	dt := &DeployTask{}
	var (
		repoURL, content, path, targetIDs, taskIDs, createdBy sql.NullString
		canaryTargets, stableTargets                          sql.NullString
		strategy                                              sql.NullString
		gateRaw                                               []byte
		canaryWeight                                          int
		autoRollback                                          bool
		createdAt, updatedAt                                  time.Time
	)
	err := rows.Scan(&dt.ID, &dt.TenantID, &dt.Name, &dt.Type,
		&repoURL, &content, &path, &targetIDs, &taskIDs, &createdBy,
		&dt.Status, &createdAt, &updatedAt,
		&strategy, &canaryWeight, &autoRollback, &gateRaw, &canaryTargets, &stableTargets)
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
	dt.Strategy = strategy.String
	dt.CanaryWeight = canaryWeight
	dt.AutoRollback = autoRollback
	dt.Gate = unmarshalGate(gateRaw)
	dt.CanaryTargets = canaryTargets.String
	dt.StableTargets = stableTargets.String
	return dt, nil
}

// nullStr 空串转 SQL NULL（与 logstore 同语义）。
func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
