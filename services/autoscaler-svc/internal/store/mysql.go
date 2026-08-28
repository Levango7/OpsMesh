package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/models"
	_ "github.com/go-sql-driver/mysql"
)

// MySQLStore is a MySQL-backed implementation of AutoscalerStore.
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
		`CREATE TABLE IF NOT EXISTS scaling_rules (
			id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			deployment VARCHAR(255),
			namespace VARCHAR(255),
			metric VARCHAR(128),
			scale_up_threshold DOUBLE,
			scale_down_threshold DOUBLE,
			min_replicas INT,
			max_replicas INT,
			cooldown_up BIGINT,
			cooldown_down BIGINT,
			enabled TINYINT(1) DEFAULT 1,
			created_at DATETIME,
			updated_at DATETIME,
			INDEX idx_deployment (deployment)
		)`,
		`CREATE TABLE IF NOT EXISTS scaling_decisions (
			id VARCHAR(64) PRIMARY KEY,
			rule_id VARCHAR(64) NOT NULL,
			deployment VARCHAR(255),
			namespace VARCHAR(255),
			action VARCHAR(32),
			from_replicas INT,
			to_replicas INT,
			reason TEXT,
			metric_value DOUBLE,
			timestamp DATETIME,
			INDEX idx_rule (rule_id),
			INDEX idx_timestamp (timestamp)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLStore) CreateRule(rule *models.ScaleRule) (*models.ScaleRule, error) {
	if rule == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = time.Now().UTC()
	}
	rule.UpdatedAt = time.Now().UTC()
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO scaling_rules (id, name, deployment, namespace, metric, scale_up_threshold, scale_down_threshold, min_replicas, max_replicas, cooldown_up, cooldown_down, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), deployment=VALUES(deployment), namespace=VALUES(namespace),
		   metric=VALUES(metric), scale_up_threshold=VALUES(scale_up_threshold), scale_down_threshold=VALUES(scale_down_threshold),
		   min_replicas=VALUES(min_replicas), max_replicas=VALUES(max_replicas), cooldown_up=VALUES(cooldown_up),
		   cooldown_down=VALUES(cooldown_down), enabled=VALUES(enabled), updated_at=VALUES(updated_at)`,
		rule.ID, rule.Name, rule.Deployment, rule.Namespace, rule.Metric, rule.ScaleUpThreshold, rule.ScaleDownThreshold,
		rule.MinReplicas, rule.MaxReplicas, int64(rule.CooldownUp), int64(rule.CooldownDown),
		boolToInt(rule.Enabled), nullTime(rule.CreatedAt), nullTime(rule.UpdatedAt))
	if err != nil {
		return nil, err
	}
	return rule, nil
}

func (m *MySQLStore) GetRule(id string) (*models.ScaleRule, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT id, name, deployment, namespace, metric, scale_up_threshold, scale_down_threshold, min_replicas, max_replicas, cooldown_up, cooldown_down, enabled, created_at, updated_at FROM scaling_rules WHERE id=?`, id)
	r := &models.ScaleRule{}
	var createdAt, updatedAt sql.NullTime
	if err := row.Scan(&r.ID, &r.Name, &r.Deployment, &r.Namespace, &r.Metric, &r.ScaleUpThreshold, &r.ScaleDownThreshold,
		&r.MinReplicas, &r.MaxReplicas, &r.CooldownUp, &r.CooldownDown, &r.Enabled, &createdAt, &updatedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetRule 查询失败: %v", err)
		}
		return nil, false
	}
	if createdAt.Valid {
		r.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		r.UpdatedAt = updatedAt.Time
	}
	return r, true
}

func (m *MySQLStore) UpdateRule(rule *models.ScaleRule) error {
	if rule == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rule.UpdatedAt = time.Now().UTC()
	_, err := m.db.ExecContext(ctx,
		`UPDATE scaling_rules SET name=?, deployment=?, namespace=?, metric=?, scale_up_threshold=?, scale_down_threshold=?, min_replicas=?, max_replicas=?, cooldown_up=?, cooldown_down=?, enabled=?, updated_at=? WHERE id=?`,
		rule.Name, rule.Deployment, rule.Namespace, rule.Metric, rule.ScaleUpThreshold, rule.ScaleDownThreshold,
		rule.MinReplicas, rule.MaxReplicas, int64(rule.CooldownUp), int64(rule.CooldownDown),
		boolToInt(rule.Enabled), nullTime(rule.UpdatedAt), rule.ID)
	return err
}

func (m *MySQLStore) DeleteRule(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx, `DELETE FROM scaling_rules WHERE id=?`, id)
	if err != nil {
		log.Printf("[store] DeleteRule 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (m *MySQLStore) ListRules() []*models.ScaleRule {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, name, deployment, namespace, metric, scale_up_threshold, scale_down_threshold, min_replicas, max_replicas, cooldown_up, cooldown_down, enabled, created_at, updated_at FROM scaling_rules`)
	if err != nil {
		log.Printf("[store] ListRules 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.ScaleRule
	for rows.Next() {
		r := &models.ScaleRule{}
		var createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Name, &r.Deployment, &r.Namespace, &r.Metric, &r.ScaleUpThreshold, &r.ScaleDownThreshold,
			&r.MinReplicas, &r.MaxReplicas, &r.CooldownUp, &r.CooldownDown, &r.Enabled, &createdAt, &updatedAt); err != nil {
			continue
		}
		if createdAt.Valid {
			r.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			r.UpdatedAt = updatedAt.Time
		}
		out = append(out, r)
	}
	return out
}

func (m *MySQLStore) CreateDecision(d *models.ScaleDecision) (*models.ScaleDecision, error) {
	if d == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if d.Timestamp.IsZero() {
		d.Timestamp = time.Now().UTC()
	}
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO scaling_decisions (id, rule_id, deployment, namespace, action, from_replicas, to_replicas, reason, metric_value, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.RuleID, d.Deployment, d.Namespace, d.Action, d.FromReplicas, d.ToReplicas, d.Reason, d.MetricValue, nullTime(d.Timestamp))
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (m *MySQLStore) ListDecisions(ruleID string, limit int) []*models.ScaleDecision {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, rule_id, deployment, namespace, action, from_replicas, to_replicas, reason, metric_value, timestamp FROM scaling_decisions`
	var args []interface{}
	if ruleID != "" {
		q += " WHERE rule_id=?"
		args = append(args, ruleID)
	}
	q += " ORDER BY timestamp DESC"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		log.Printf("[store] ListDecisions 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.ScaleDecision
	for rows.Next() {
		d := &models.ScaleDecision{}
		var ts sql.NullTime
		if err := rows.Scan(&d.ID, &d.RuleID, &d.Deployment, &d.Namespace, &d.Action, &d.FromReplicas, &d.ToReplicas, &d.Reason, &d.MetricValue, &ts); err != nil {
			continue
		}
		if ts.Valid {
			d.Timestamp = ts.Time
		}
		out = append(out, d)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func nullTime(t time.Time) interface{} {
	if t.IsZero() {
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
