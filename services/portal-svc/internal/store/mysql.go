package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Levango7/OpsMesh/services/portal-svc/internal/models"
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
		`CREATE TABLE IF NOT EXISTS resource_requests (
			id VARCHAR(64) PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			requester VARCHAR(255),
			title VARCHAR(255) NOT NULL,
			description TEXT,
			resource_type VARCHAR(64),
			cpu INT DEFAULT 0,
			memory_gb INT DEFAULT 0,
			storage_gb INT DEFAULT 0,
			cost_estimate DOUBLE DEFAULT 0,
			status VARCHAR(32) DEFAULT 'draft',
			approver VARCHAR(64),
			approval_note TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			INDEX idx_tenant (tenant_id),
			INDEX idx_status (status)
		)`,
		`CREATE TABLE IF NOT EXISTS quotas (
			tenant_id VARCHAR(64) PRIMARY KEY,
			max_cpu INT DEFAULT 0,
			max_memory_gb INT DEFAULT 0,
			max_storage_gb INT DEFAULT 0,
			max_requests INT DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS budgets (
			tenant_id VARCHAR(64) PRIMARY KEY,
			monthly_limit DOUBLE DEFAULT 0,
			current_spend DOUBLE DEFAULT 0,
			alert_threshold DOUBLE DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS cost_recommendations (
			id VARCHAR(64) PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			category VARCHAR(64),
			resource_id VARCHAR(64),
			description TEXT,
			savings DOUBLE DEFAULT 0,
			priority VARCHAR(32),
			INDEX idx_tenant (tenant_id)
		)`,
		`CREATE TABLE IF NOT EXISTS utilizations (
			tenant_id VARCHAR(64) PRIMARY KEY,
			cpu_usage DOUBLE DEFAULT 0,
			memory_usage DOUBLE DEFAULT 0,
			storage_usage DOUBLE DEFAULT 0,
			idle_count INT DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS activities (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			user_id VARCHAR(64),
			action VARCHAR(128),
			target VARCHAR(255),
			detail TEXT,
			timestamp DATETIME,
			INDEX idx_tenant (tenant_id),
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

func (m *MySQLStore) CreateRequest(r *models.ResourceRequest) *models.ResourceRequest {
	if r == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO resource_requests (id, tenant_id, requester, title, description, resource_type, cpu, memory_gb, storage_gb, cost_estimate, status, approver, approval_note, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE tenant_id=VALUES(tenant_id), requester=VALUES(requester), title=VALUES(title),
		   description=VALUES(description), resource_type=VALUES(resource_type), cpu=VALUES(cpu),
		   memory_gb=VALUES(memory_gb), storage_gb=VALUES(storage_gb), cost_estimate=VALUES(cost_estimate),
		   status=VALUES(status), approver=VALUES(approver), approval_note=VALUES(approval_note), updated_at=VALUES(updated_at)`,
		r.ID, r.TenantID, r.Requester, r.Title, r.Description, r.ResourceType, r.CPU, r.MemoryGB, r.StorageGB,
		r.CostEstimate, r.Status, r.Approver, r.ApprovalNote, nullTime(r.CreatedAt), nullTime(r.UpdatedAt))
	if err != nil {
		log.Printf("[store] CreateRequest 失败: %v", err)
	}
	return r
}

func (m *MySQLStore) GetRequest(id string) *models.ResourceRequest {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, requester, title, description, resource_type, cpu, memory_gb, storage_gb, cost_estimate, status, approver, approval_note, created_at, updated_at FROM resource_requests WHERE id=?`, id)
	r := &models.ResourceRequest{}
	var createdAt, updatedAt sql.NullTime
	if err := row.Scan(&r.ID, &r.TenantID, &r.Requester, &r.Title, &r.Description, &r.ResourceType, &r.CPU, &r.MemoryGB, &r.StorageGB, &r.CostEstimate, &r.Status, &r.Approver, &r.ApprovalNote, &createdAt, &updatedAt); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[store] GetRequest 查询失败: %v", err)
		}
		return nil
	}
	if createdAt.Valid {
		r.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		r.UpdatedAt = updatedAt.Time
	}
	return r
}

func (m *MySQLStore) ListRequests(tenantID, status string) []*models.ResourceRequest {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, tenant_id, requester, title, description, resource_type, cpu, memory_gb, storage_gb, cost_estimate, status, approver, approval_note, created_at, updated_at FROM resource_requests`
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
		log.Printf("[store] ListRequests 失败: %v", err)
		return nil
	}
	defer rows.Close()
	var out []*models.ResourceRequest
	for rows.Next() {
		r := &models.ResourceRequest{}
		var createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Requester, &r.Title, &r.Description, &r.ResourceType, &r.CPU, &r.MemoryGB, &r.StorageGB, &r.CostEstimate, &r.Status, &r.Approver, &r.ApprovalNote, &createdAt, &updatedAt); err != nil {
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

func (m *MySQLStore) UpdateRequest(r *models.ResourceRequest) bool {
	if r == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx,
		`UPDATE resource_requests SET tenant_id=?, requester=?, title=?, description=?, resource_type=?, cpu=?, memory_gb=?, storage_gb=?, cost_estimate=?, status=?, approver=?, approval_note=?, updated_at=? WHERE id=?`,
		r.TenantID, r.Requester, r.Title, r.Description, r.ResourceType, r.CPU, r.MemoryGB, r.StorageGB,
		r.CostEstimate, r.Status, r.Approver, r.ApprovalNote, time.Now().UTC(), r.ID)
	if err != nil {
		log.Printf("[store] UpdateRequest 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (m *MySQLStore) DeleteRequest(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.db.ExecContext(ctx, `DELETE FROM resource_requests WHERE id=?`, id)
	if err != nil {
		log.Printf("[store] DeleteRequest 失败: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (m *MySQLStore) SetQuota(tenantID string, q *models.Quota) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO quotas (tenant_id, max_cpu, max_memory_gb, max_storage_gb, max_requests) VALUES (?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE max_cpu=VALUES(max_cpu), max_memory_gb=VALUES(max_memory_gb), max_storage_gb=VALUES(max_storage_gb), max_requests=VALUES(max_requests)`,
		tenantID, q.MaxCPU, q.MaxMemoryGB, q.MaxStorageGB, q.MaxRequests)
	return err
}

func (m *MySQLStore) GetQuota(tenantID string) *models.Quota {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx, `SELECT max_cpu, max_memory_gb, max_storage_gb, max_requests FROM quotas WHERE tenant_id=?`, tenantID)
	q := &models.Quota{}
	if err := row.Scan(&q.MaxCPU, &q.MaxMemoryGB, &q.MaxStorageGB, &q.MaxRequests); err != nil {
		return nil
	}
	return q
}

func (m *MySQLStore) ListQuotas() []*models.Quota {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := m.db.QueryContext(ctx, `SELECT tenant_id, max_cpu, max_memory_gb, max_storage_gb, max_requests FROM quotas`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*models.Quota
	for rows.Next() {
		q := &models.Quota{}
		if err := rows.Scan(&q.TenantID, &q.MaxCPU, &q.MaxMemoryGB, &q.MaxStorageGB, &q.MaxRequests); err != nil {
			continue
		}
		out = append(out, q)
	}
	return out
}

func (m *MySQLStore) SetBudget(b *models.Budget) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO budgets (tenant_id, monthly_limit, current_spend, alert_threshold) VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE monthly_limit=VALUES(monthly_limit), current_spend=VALUES(current_spend), alert_threshold=VALUES(alert_threshold)`,
		b.TenantID, b.MonthlyLimit, b.CurrentSpend, b.AlertThreshold)
	return err
}

func (m *MySQLStore) GetBudget(tenantID string) *models.Budget {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx, `SELECT monthly_limit, current_spend, alert_threshold FROM budgets WHERE tenant_id=?`, tenantID)
	b := &models.Budget{}
	if err := row.Scan(&b.MonthlyLimit, &b.CurrentSpend, &b.AlertThreshold); err != nil {
		return nil
	}
	return b
}

func (m *MySQLStore) AddRecommendation(r *models.CostRecommendation) {
	if r == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO cost_recommendations (id, tenant_id, category, resource_id, description, savings, priority) VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE category=VALUES(category), resource_id=VALUES(resource_id), description=VALUES(description), savings=VALUES(savings), priority=VALUES(priority)`,
		r.ID, r.TenantID, r.Category, r.ResourceID, r.Description, r.Savings, r.Priority)
	if err != nil {
		log.Printf("[store] AddRecommendation 失败: %v", err)
	}
}

func (m *MySQLStore) ListRecommendations(tenantID string) []*models.CostRecommendation {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT id, tenant_id, category, resource_id, description, savings, priority FROM cost_recommendations`
	var args []interface{}
	if tenantID != "" {
		q += " WHERE tenant_id=?"
		args = append(args, tenantID)
	}
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*models.CostRecommendation
	for rows.Next() {
		r := &models.CostRecommendation{}
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Category, &r.ResourceID, &r.Description, &r.Savings, &r.Priority); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (m *MySQLStore) SetUtilization(u *models.Utilization) {
	if u == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO utilizations (tenant_id, cpu_usage, memory_usage, storage_usage, idle_count) VALUES (?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE cpu_usage=VALUES(cpu_usage), memory_usage=VALUES(memory_usage), storage_usage=VALUES(storage_usage), idle_count=VALUES(idle_count)`,
		u.TenantID, u.CPUUsage, u.MemoryUsage, u.StorageUsage, u.IdleCount)
	if err != nil {
		log.Printf("[store] SetUtilization 失败: %v", err)
	}
}

func (m *MySQLStore) GetUtilization(tenantID string) *models.Utilization {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row := m.db.QueryRowContext(ctx, `SELECT cpu_usage, memory_usage, storage_usage, idle_count FROM utilizations WHERE tenant_id=?`, tenantID)
	u := &models.Utilization{}
	if err := row.Scan(&u.CPUUsage, &u.MemoryUsage, &u.StorageUsage, &u.IdleCount); err != nil {
		return nil
	}
	return u
}

func (m *MySQLStore) AddActivity(a *models.ActivityEvent) {
	if a == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if a.Timestamp.IsZero() {
		a.Timestamp = time.Now().UTC()
	}
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO activities (tenant_id, user_id, action, target, detail, timestamp) VALUES (?, ?, ?, ?, ?, ?)`,
		a.TenantID, a.UserID, a.Action, a.Target, a.Detail, nullTime(a.Timestamp))
	if err != nil {
		log.Printf("[store] AddActivity 失败: %v", err)
	}
}

func (m *MySQLStore) ListActivity(tenantID string, limit int) []*models.ActivityEvent {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `SELECT tenant_id, user_id, action, target, detail, timestamp FROM activities`
	var args []interface{}
	if tenantID != "" {
		q += " WHERE tenant_id=?"
		args = append(args, tenantID)
	}
	q += " ORDER BY timestamp DESC"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*models.ActivityEvent
	for rows.Next() {
		a := &models.ActivityEvent{}
		var ts sql.NullTime
		if err := rows.Scan(&a.TenantID, &a.UserID, &a.Action, &a.Target, &a.Detail, &ts); err != nil {
			continue
		}
		if ts.Valid {
			a.Timestamp = ts.Time
		}
		out = append(out, a)
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

var _ = json.Marshal

// 编译期断言：MySQLStore 实现 Store 接口。
var _ Store = (*MySQLStore)(nil)
