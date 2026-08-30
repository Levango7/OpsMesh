package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/Levango7/OpsMesh/services/task-svc/internal/models"
)

// MySQLStore is a MySQL-backed implementation of all task-svc stores.
type MySQLStore struct {
	db *sql.DB
}

// NewMySQLStore creates a new MySQLStore with the given DSN.
func NewMySQLStore(dsn string) (*MySQLStore, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open mysql connection: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping mysql: %w", err)
	}

	return &MySQLStore{db: db}, nil
}

// Close closes the database connection.
func (s *MySQLStore) Close() error {
	return s.db.Close()
}

// NewStore creates a store based on configuration.
// If dbDSN is non-empty, it returns a MySQLStore; otherwise a MemoryStore.
func NewStore(dbDSN string) (*MySQLStore, error) {
	if dbDSN != "" {
		return NewMySQLStore(dbDSN)
	}
	return nil, nil
}

// jsonStringSlice marshals a string slice to JSON.
func jsonStringSlice(v []string) string {
	if v == nil {
		return "[]"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// scanStringSlice scans a JSON string into a string slice.
func scanStringSlice(data []byte) []string {
	var s []string
	if len(data) == 0 {
		return nil
	}
	_ = json.Unmarshal(data, &s)
	return s
}

// === TaskStore implementation ===

// CreateTask creates a task.
func (s *MySQLStore) CreateTask(t *models.Task) *models.Task {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	if t.Status == "" {
		t.Status = models.TaskStatusPending
	}

	_, err := s.db.Exec(
		"INSERT INTO tasks (task_id, agent_id, tenant_id, type, command, content, path, status, claimed_by, claimed_at, claim_epoch, created_at, retry_count, max_retries, dead_letter, timeout, retry_delay, schedule, parent_id, depends_on, approval_required, approved_by, approved_at, batch_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		t.TaskID, t.AgentID, t.TenantID, t.Type, t.Command, t.Content, t.Path, t.Status,
		t.ClaimedBy, t.ClaimedAt, t.ClaimEpoch, t.CreatedAt, t.RetryCount, t.MaxRetries,
		t.DeadLetter, t.Timeout, t.RetryDelay, t.Schedule, t.ParentID,
		jsonStringSlice(t.DependsOn), t.ApprovalRequired, t.ApprovedBy, t.ApprovedAt, t.BatchID,
	)
	if err != nil {
		return nil
	}
	return t
}

// GetTask returns a task by ID.
func (s *MySQLStore) GetTask(taskID string) *models.Task {
	row := s.db.QueryRow(
		"SELECT task_id, agent_id, tenant_id, type, command, content, path, status, claimed_by, claimed_at, claim_epoch, created_at, retry_count, max_retries, dead_letter, timeout, retry_delay, schedule, parent_id, depends_on, approval_required, approved_by, approved_at, batch_id FROM tasks WHERE task_id = ?",
		taskID,
	)
	return s.scanTask(row)
}

func (s *MySQLStore) scanTask(row *sql.Row) *models.Task {
	var t models.Task
	var dependsOn sql.RawBytes
	var deadLetter, approvalRequired int
	err := row.Scan(&t.TaskID, &t.AgentID, &t.TenantID, &t.Type, &t.Command, &t.Content, &t.Path, &t.Status,
		&t.ClaimedBy, &t.ClaimedAt, &t.ClaimEpoch, &t.CreatedAt, &t.RetryCount, &t.MaxRetries,
		&deadLetter, &t.Timeout, &t.RetryDelay, &t.Schedule, &t.ParentID, &dependsOn,
		&approvalRequired, &t.ApprovedBy, &t.ApprovedAt, &t.BatchID)
	if err != nil {
		return nil
	}
	t.DeadLetter = deadLetter != 0
	t.ApprovalRequired = approvalRequired != 0
	t.DependsOn = scanStringSlice(dependsOn)
	return &t
}

// ListTasks returns tasks with optional filtering.
func (s *MySQLStore) ListTasks(tenantID, status, agentID string, limit int) []*models.Task {
	query := "SELECT task_id, agent_id, tenant_id, type, command, content, path, status, claimed_by, claimed_at, claim_epoch, created_at, retry_count, max_retries, dead_letter, timeout, retry_delay, schedule, parent_id, depends_on, approval_required, approved_by, approved_at, batch_id FROM tasks WHERE 1=1"
	args := []interface{}{}
	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	if agentID != "" {
		query += " AND agent_id = ?"
		args = append(args, agentID)
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return s.scanTasks(rows)
}

func (s *MySQLStore) scanTasks(rows *sql.Rows) []*models.Task {
	var tasks []*models.Task
	for rows.Next() {
		var t models.Task
		var dependsOn sql.RawBytes
		var deadLetter, approvalRequired int
		if err := rows.Scan(&t.TaskID, &t.AgentID, &t.TenantID, &t.Type, &t.Command, &t.Content, &t.Path, &t.Status,
			&t.ClaimedBy, &t.ClaimedAt, &t.ClaimEpoch, &t.CreatedAt, &t.RetryCount, &t.MaxRetries,
			&deadLetter, &t.Timeout, &t.RetryDelay, &t.Schedule, &t.ParentID, &dependsOn,
			&approvalRequired, &t.ApprovedBy, &t.ApprovedAt, &t.BatchID); err != nil {
			continue
		}
		t.DeadLetter = deadLetter != 0
		t.ApprovalRequired = approvalRequired != 0
		t.DependsOn = scanStringSlice(dependsOn)
		tasks = append(tasks, &t)
	}
	return tasks
}

// ClaimTask atomically claims a pending task for an agent.
func (s *MySQLStore) ClaimTask(agentID string) *models.Task {
	tx, err := s.db.Begin()
	if err != nil {
		return nil
	}
	defer tx.Rollback()

	row := tx.QueryRow(
		"SELECT task_id, agent_id, tenant_id, type, command, content, path, status, claimed_by, claimed_at, claim_epoch, created_at, retry_count, max_retries, dead_letter, timeout, retry_delay, schedule, parent_id, depends_on, approval_required, approved_by, approved_at, batch_id FROM tasks WHERE status = 'pending' AND (agent_id = ? OR agent_id = '') ORDER BY created_at ASC LIMIT 1 FOR UPDATE",
		agentID,
	)
	t := s.scanTask(row)
	if t == nil {
		return nil
	}

	now := time.Now()
	_, err = tx.Exec(
		"UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, claim_epoch = claim_epoch + 1 WHERE task_id = ?",
		models.TaskStatusClaimed, agentID, now, t.TaskID,
	)
	if err != nil {
		return nil
	}

	if err := tx.Commit(); err != nil {
		return nil
	}

	t.Status = models.TaskStatusClaimed
	t.ClaimedBy = agentID
	t.ClaimedAt = now
	t.ClaimEpoch++
	return t
}

// ReportResult reports a task result.
func (s *MySQLStore) ReportResult(result *models.TaskResult) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var t models.Task
	var dependsOn sql.RawBytes
	var deadLetter, approvalRequired int
	err = tx.QueryRow(
		"SELECT task_id, agent_id, tenant_id, type, command, content, path, status, claimed_by, claimed_at, claim_epoch, created_at, retry_count, max_retries, dead_letter, timeout, retry_delay, schedule, parent_id, depends_on, approval_required, approved_by, approved_at, batch_id FROM tasks WHERE task_id = ? FOR UPDATE",
		result.TaskID,
	).Scan(&t.TaskID, &t.AgentID, &t.TenantID, &t.Type, &t.Command, &t.Content, &t.Path, &t.Status,
		&t.ClaimedBy, &t.ClaimedAt, &t.ClaimEpoch, &t.CreatedAt, &t.RetryCount, &t.MaxRetries,
		&deadLetter, &t.Timeout, &t.RetryDelay, &t.Schedule, &t.ParentID, &dependsOn,
		&approvalRequired, &t.ApprovedBy, &t.ApprovedAt, &t.BatchID)
	if err != nil {
		return ErrTaskNotFound
	}
	t.DeadLetter = deadLetter != 0
	t.ApprovalRequired = approvalRequired != 0

	if result.ClaimEpoch != 0 && result.ClaimEpoch != t.ClaimEpoch {
		return ErrClaimEpochMismatch
	}

	if result.FinishedAt.IsZero() {
		result.FinishedAt = time.Now()
	}

	_, err = tx.Exec(
		"INSERT INTO task_results (task_id, agent_id, exit_code, stdout, stderr, duration_ms, finished_at, claim_epoch) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE agent_id = ?, exit_code = ?, stdout = ?, stderr = ?, duration_ms = ?, finished_at = ?, claim_epoch = ?",
		result.TaskID, result.AgentID, result.ExitCode, result.Stdout, result.Stderr, result.DurationMs, result.FinishedAt, result.ClaimEpoch,
		result.AgentID, result.ExitCode, result.Stdout, result.Stderr, result.DurationMs, result.FinishedAt, result.ClaimEpoch,
	)
	if err != nil {
		return fmt.Errorf("insert result: %w", err)
	}

	if result.ExitCode == 0 {
		t.Status = models.TaskStatusDone
	} else {
		t.RetryCount++
		if t.RetryCount >= t.MaxRetries {
			t.Status = models.TaskStatusFailed
			t.DeadLetter = true
		} else {
			t.Status = models.TaskStatusPending
			t.ClaimedBy = ""
			t.ClaimedAt = time.Time{}
		}
	}

	_, err = tx.Exec(
		"UPDATE tasks SET status = ?, retry_count = ?, dead_letter = ?, claimed_by = ?, claimed_at = ? WHERE task_id = ?",
		t.Status, t.RetryCount, t.DeadLetter, t.ClaimedBy, t.ClaimedAt, t.TaskID,
	)
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}

	return tx.Commit()
}

// CancelTask cancels a task.
func (s *MySQLStore) CancelTask(taskID, tenantID string) bool {
	query := "UPDATE tasks SET status = ? WHERE task_id = ? AND status NOT IN (?, ?)"
	args := []interface{}{models.TaskStatusCancelled, taskID, models.TaskStatusDone, models.TaskStatusFailed}
	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// ApproveTask approves a pending_approval task.
func (s *MySQLStore) ApproveTask(taskID, tenantID, approvedBy string) bool {
	now := time.Now()
	query := "UPDATE tasks SET status = ?, approved_by = ?, approved_at = ? WHERE task_id = ? AND status = ?"
	args := []interface{}{models.TaskStatusPending, approvedBy, now, taskID, models.TaskStatusPendingApproval}
	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// RejectTask rejects a pending_approval task.
func (s *MySQLStore) RejectTask(taskID, tenantID, rejectedBy string) bool {
	now := time.Now()
	query := "UPDATE tasks SET status = ?, approved_by = ?, approved_at = ? WHERE task_id = ? AND status = ?"
	args := []interface{}{models.TaskStatusRejected, rejectedBy, now, taskID, models.TaskStatusPendingApproval}
	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// GetTaskStatus returns a task by ID (alias for GetTask).
func (s *MySQLStore) GetTaskStatus(taskID string) *models.Task {
	return s.GetTask(taskID)
}

// AllTasks returns all tasks.
func (s *MySQLStore) AllTasks() []*models.Task {
	rows, err := s.db.Query(
		"SELECT task_id, agent_id, tenant_id, type, command, content, path, status, claimed_by, claimed_at, claim_epoch, created_at, retry_count, max_retries, dead_letter, timeout, retry_delay, schedule, parent_id, depends_on, approval_required, approved_by, approved_at, batch_id FROM tasks",
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return s.scanTasks(rows)
}

// === ScheduleStore implementation ===

// CreateSchedule creates a schedule.
func (s *MySQLStore) CreateSchedule(sch *models.Schedule) *models.Schedule {
	if sch.CreatedAt.IsZero() {
		sch.CreatedAt = time.Now()
	}
	if sch.UpdatedAt.IsZero() {
		sch.UpdatedAt = time.Now()
	}

	_, err := s.db.Exec(
		"INSERT INTO schedules (id, tenant_id, name, cron_expr, task_type, command, content, path, agent_id, enabled, last_fired_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		sch.ID, sch.TenantID, sch.Name, sch.CronExpr, sch.TaskType, sch.Command, sch.Content,
		sch.Path, sch.AgentID, sch.Enabled, sch.LastFiredAt, sch.CreatedAt, sch.UpdatedAt,
	)
	if err != nil {
		return nil
	}
	return sch
}

// GetSchedule returns a schedule by ID.
func (s *MySQLStore) GetSchedule(id string) *models.Schedule {
	var sch models.Schedule
	var enabled int
	err := s.db.QueryRow(
		"SELECT id, tenant_id, name, cron_expr, task_type, command, content, path, agent_id, enabled, last_fired_at, created_at, updated_at FROM schedules WHERE id = ?",
		id,
	).Scan(&sch.ID, &sch.TenantID, &sch.Name, &sch.CronExpr, &sch.TaskType, &sch.Command, &sch.Content,
		&sch.Path, &sch.AgentID, &enabled, &sch.LastFiredAt, &sch.CreatedAt, &sch.UpdatedAt)
	if err != nil {
		return nil
	}
	sch.Enabled = enabled != 0
	return &sch
}

// UpdateSchedule updates a schedule.
func (s *MySQLStore) UpdateSchedule(sch *models.Schedule) (*models.Schedule, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM schedules WHERE id = ?", sch.ID).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("check schedule: %w", err)
	}
	if count == 0 {
		return nil, ErrScheduleNotFound
	}

	sch.UpdatedAt = time.Now()
	_, err = s.db.Exec(
		"UPDATE schedules SET tenant_id = ?, name = ?, cron_expr = ?, task_type = ?, command = ?, content = ?, path = ?, agent_id = ?, enabled = ?, last_fired_at = ?, updated_at = ? WHERE id = ?",
		sch.TenantID, sch.Name, sch.CronExpr, sch.TaskType, sch.Command, sch.Content,
		sch.Path, sch.AgentID, sch.Enabled, sch.LastFiredAt, sch.UpdatedAt, sch.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("update schedule: %w", err)
	}
	return sch, nil
}

// DeleteSchedule deletes a schedule.
func (s *MySQLStore) DeleteSchedule(id string) bool {
	res, err := s.db.Exec("DELETE FROM schedules WHERE id = ?", id)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// ListSchedules returns schedules, optionally filtered by tenant.
func (s *MySQLStore) ListSchedules(tenantID string) []*models.Schedule {
	query := "SELECT id, tenant_id, name, cron_expr, task_type, command, content, path, agent_id, enabled, last_fired_at, created_at, updated_at FROM schedules"
	args := []interface{}{}
	if tenantID != "" {
		query += " WHERE tenant_id = ?"
		args = append(args, tenantID)
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var schedules []*models.Schedule
	for rows.Next() {
		var sch models.Schedule
		var enabled int
		if err := rows.Scan(&sch.ID, &sch.TenantID, &sch.Name, &sch.CronExpr, &sch.TaskType, &sch.Command, &sch.Content,
			&sch.Path, &sch.AgentID, &enabled, &sch.LastFiredAt, &sch.CreatedAt, &sch.UpdatedAt); err != nil {
			continue
		}
		sch.Enabled = enabled != 0
		schedules = append(schedules, &sch)
	}
	return schedules
}

// === ResultStore implementation ===

// SaveResult saves a task result.
func (s *MySQLStore) SaveResult(r *models.TaskResult) {
	_, _ = s.db.Exec(
		"INSERT INTO task_results (task_id, agent_id, exit_code, stdout, stderr, duration_ms, finished_at, claim_epoch) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE agent_id = ?, exit_code = ?, stdout = ?, stderr = ?, duration_ms = ?, finished_at = ?, claim_epoch = ?",
		r.TaskID, r.AgentID, r.ExitCode, r.Stdout, r.Stderr, r.DurationMs, r.FinishedAt, r.ClaimEpoch,
		r.AgentID, r.ExitCode, r.Stdout, r.Stderr, r.DurationMs, r.FinishedAt, r.ClaimEpoch,
	)
}

// GetTaskResult returns a task result by task ID.
func (s *MySQLStore) GetTaskResult(taskID string) *models.TaskResult {
	var r models.TaskResult
	err := s.db.QueryRow(
		"SELECT task_id, agent_id, exit_code, stdout, stderr, duration_ms, finished_at, claim_epoch FROM task_results WHERE task_id = ?",
		taskID,
	).Scan(&r.TaskID, &r.AgentID, &r.ExitCode, &r.Stdout, &r.Stderr, &r.DurationMs, &r.FinishedAt, &r.ClaimEpoch)
	if err != nil {
		return nil
	}
	return &r
}

// ListTaskResults returns task results with optional filtering.
func (s *MySQLStore) ListTaskResults(tenantID, agentID string, limit int) []*models.TaskResult {
	query := "SELECT r.task_id, r.agent_id, r.exit_code, r.stdout, r.stderr, r.duration_ms, r.finished_at, r.claim_epoch FROM task_results r"
	args := []interface{}{}
	if tenantID != "" {
		query += " JOIN tasks t ON r.task_id = t.task_id WHERE t.tenant_id = ?"
		args = append(args, tenantID)
	}
	if agentID != "" {
		if tenantID != "" {
			query += " AND r.agent_id = ?"
		} else {
			query += " WHERE r.agent_id = ?"
		}
		args = append(args, agentID)
	}
	query += " ORDER BY r.finished_at DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []*models.TaskResult
	for rows.Next() {
		var r models.TaskResult
		if err := rows.Scan(&r.TaskID, &r.AgentID, &r.ExitCode, &r.Stdout, &r.Stderr, &r.DurationMs, &r.FinishedAt, &r.ClaimEpoch); err != nil {
			continue
		}
		results = append(results, &r)
	}
	return results
}

// SaveLogs saves logs for a task.
func (s *MySQLStore) SaveLogs(taskID string, logs []models.LogLine) {
	if len(logs) == 0 {
		return
	}
	_, _ = s.db.Exec("DELETE FROM task_logs WHERE task_id = ?", taskID)
	for _, line := range logs {
		_, _ = s.db.Exec(
			"INSERT INTO task_logs (task_id, log_timestamp, level, message) VALUES (?, ?, ?, ?)",
			taskID, line.Timestamp, line.Level, line.Message,
		)
	}
}

// GetTaskLogs returns logs for a task.
func (s *MySQLStore) GetTaskLogs(taskID string) []models.LogLine {
	rows, err := s.db.Query(
		"SELECT log_timestamp, level, message FROM task_logs WHERE task_id = ? ORDER BY id ASC",
		taskID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var logs []models.LogLine
	for rows.Next() {
		var line models.LogLine
		if err := rows.Scan(&line.Timestamp, &line.Level, &line.Message); err != nil {
			continue
		}
		logs = append(logs, line)
	}
	return logs
}

// === BatchStore implementation ===

// CreateBatch creates a batch.
func (s *MySQLStore) CreateBatch(b *models.BatchTask) *models.BatchTask {
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now()
	}
	_, err := s.db.Exec(
		"INSERT INTO batches (batch_id, tenant_id, name, total_count, success_count, failed_count, pending_count, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		b.BatchID, b.TenantID, b.Name, b.TotalCount, b.SuccessCount, b.FailedCount, b.PendingCount, b.Status, b.CreatedAt,
	)
	if err != nil {
		return nil
	}
	return b
}

// GetBatch returns a batch by ID.
func (s *MySQLStore) GetBatch(batchID string) *models.BatchTask {
	var b models.BatchTask
	err := s.db.QueryRow(
		"SELECT batch_id, tenant_id, name, total_count, success_count, failed_count, pending_count, status, created_at FROM batches WHERE batch_id = ?",
		batchID,
	).Scan(&b.BatchID, &b.TenantID, &b.Name, &b.TotalCount, &b.SuccessCount, &b.FailedCount, &b.PendingCount, &b.Status, &b.CreatedAt)
	if err != nil {
		return nil
	}
	return &b
}

// UpdateBatch updates a batch.
func (s *MySQLStore) UpdateBatch(b *models.BatchTask) {
	_, _ = s.db.Exec(
		"UPDATE batches SET tenant_id = ?, name = ?, total_count = ?, success_count = ?, failed_count = ?, pending_count = ?, status = ? WHERE batch_id = ?",
		b.TenantID, b.Name, b.TotalCount, b.SuccessCount, b.FailedCount, b.PendingCount, b.Status, b.BatchID,
	)
}

// ListBatches returns batches, optionally filtered by tenant.
func (s *MySQLStore) ListBatches(tenantID string) []*models.BatchTask {
	query := "SELECT batch_id, tenant_id, name, total_count, success_count, failed_count, pending_count, status, created_at FROM batches"
	args := []interface{}{}
	if tenantID != "" {
		query += " WHERE tenant_id = ?"
		args = append(args, tenantID)
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var batches []*models.BatchTask
	for rows.Next() {
		var b models.BatchTask
		if err := rows.Scan(&b.BatchID, &b.TenantID, &b.Name, &b.TotalCount, &b.SuccessCount, &b.FailedCount, &b.PendingCount, &b.Status, &b.CreatedAt); err != nil {
			continue
		}
		batches = append(batches, &b)
	}
	return batches
}

// AddTaskToBatch adds a task to a batch.
func (s *MySQLStore) AddTaskToBatch(batchID, taskID string) {
	_, _ = s.db.Exec(
		"INSERT IGNORE INTO batch_tasks (batch_id, task_id) VALUES (?, ?)",
		batchID, taskID,
	)
}

// GetBatchTasks returns task IDs in a batch.
func (s *MySQLStore) GetBatchTasks(batchID string) []string {
	rows, err := s.db.Query("SELECT task_id FROM batch_tasks WHERE batch_id = ?", batchID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// 编译期断言：MySQLStore 实现全部四个 store 接口。
var (
	_ TaskStore     = (*MySQLStore)(nil)
	_ ScheduleStore = (*MySQLStore)(nil)
	_ ResultStore   = (*MySQLStore)(nil)
	_ BatchStore    = (*MySQLStore)(nil)
)
