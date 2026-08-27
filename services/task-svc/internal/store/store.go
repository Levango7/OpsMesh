package store

import (
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/task-svc/internal/models"
)

// TaskStore is the interface for task persistence.
type TaskStore interface {
	CreateTask(t *models.Task) *models.Task
	GetTask(taskID string) *models.Task
	ListTasks(tenantID, status, agentID string, limit int) []*models.Task
	ClaimTask(agentID string) *models.Task
	ReportResult(result *models.TaskResult) error
	CancelTask(taskID, tenantID string) bool
	ApproveTask(taskID, tenantID, approvedBy string) bool
	RejectTask(taskID, tenantID, rejectedBy string) bool
	GetTaskStatus(taskID string) *models.Task
	AllTasks() []*models.Task
}

// ScheduleStore is the interface for schedule persistence.
type ScheduleStore interface {
	CreateSchedule(s *models.Schedule) *models.Schedule
	GetSchedule(id string) *models.Schedule
	UpdateSchedule(s *models.Schedule) (*models.Schedule, error)
	DeleteSchedule(id string) bool
	ListSchedules(tenantID string) []*models.Schedule
}

// ResultStore is the interface for result persistence.
type ResultStore interface {
	SaveResult(r *models.TaskResult)
	GetTaskResult(taskID string) *models.TaskResult
	ListTaskResults(tenantID, agentID string, limit int) []*models.TaskResult
	SaveLogs(taskID string, logs []models.LogLine)
	GetTaskLogs(taskID string) []models.LogLine
}

// BatchStore is the interface for batch persistence.
type BatchStore interface {
	CreateBatch(b *models.BatchTask) *models.BatchTask
	GetBatch(batchID string) *models.BatchTask
	UpdateBatch(b *models.BatchTask)
	ListBatches(tenantID string) []*models.BatchTask
	AddTaskToBatch(batchID string, taskID string)
	GetBatchTasks(batchID string) []string
}

// MemoryStore is an in-memory implementation of all stores.
type MemoryStore struct {
	mu        sync.RWMutex
	tasks     map[string]*models.Task
	results   map[string]*models.TaskResult
	logs      map[string][]models.LogLine
	schedules map[string]*models.Schedule
	batches   map[string]*models.BatchTask
	batchTask map[string][]string
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tasks:     make(map[string]*models.Task),
		results:   make(map[string]*models.TaskResult),
		logs:      make(map[string][]models.LogLine),
		schedules: make(map[string]*models.Schedule),
		batches:   make(map[string]*models.BatchTask),
		batchTask: make(map[string][]string),
	}
}

// CreateTask creates a task.
func (m *MemoryStore) CreateTask(t *models.Task) *models.Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	if t.Status == "" {
		t.Status = models.TaskStatusPending
	}
	cp := *t
	m.tasks[t.TaskID] = &cp
	return t
}

// GetTask returns a task by ID.
func (m *MemoryStore) GetTask(taskID string) *models.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return nil
	}
	cp := *t
	return &cp
}

// ListTasks returns tasks with optional filtering.
func (m *MemoryStore) ListTasks(tenantID, status, agentID string, limit int) []*models.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		if tenantID != "" && t.TenantID != tenantID {
			continue
		}
		if status != "" && t.Status != status {
			continue
		}
		if agentID != "" && t.AgentID != agentID {
			continue
		}
		cp := *t
		out = append(out, &cp)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// ClaimTask atomically claims a pending task for an agent.
func (m *MemoryStore) ClaimTask(agentID string) *models.Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks {
		if t.Status == models.TaskStatusPending && (t.AgentID == agentID || t.AgentID == "") {
			t.Status = models.TaskStatusClaimed
			t.ClaimedBy = agentID
			t.ClaimedAt = time.Now()
			t.ClaimEpoch++
			cp := *t
			return &cp
		}
	}
	return nil
}

// ReportResult reports a task result.
func (m *MemoryStore) ReportResult(result *models.TaskResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[result.TaskID]
	if !ok {
		return ErrTaskNotFound
	}
	if result.ClaimEpoch != 0 && result.ClaimEpoch != t.ClaimEpoch {
		return ErrClaimEpochMismatch
	}
	m.results[result.TaskID] = result
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
	return nil
}

// CancelTask cancels a task.
func (m *MemoryStore) CancelTask(taskID, tenantID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return false
	}
	if tenantID != "" && t.TenantID != tenantID {
		return false
	}
	if t.Status == models.TaskStatusDone || t.Status == models.TaskStatusFailed {
		return false
	}
	t.Status = models.TaskStatusCancelled
	return true
}

// ApproveTask approves a pending_approval task.
func (m *MemoryStore) ApproveTask(taskID, tenantID, approvedBy string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return false
	}
	if tenantID != "" && t.TenantID != tenantID {
		return false
	}
	if t.Status != models.TaskStatusPendingApproval {
		return false
	}
	t.Status = models.TaskStatusPending
	t.ApprovedBy = approvedBy
	t.ApprovedAt = time.Now()
	return true
}

// RejectTask rejects a pending_approval task.
func (m *MemoryStore) RejectTask(taskID, tenantID, rejectedBy string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return false
	}
	if tenantID != "" && t.TenantID != tenantID {
		return false
	}
	if t.Status != models.TaskStatusPendingApproval {
		return false
	}
	t.Status = models.TaskStatusRejected
	t.ApprovedBy = rejectedBy
	t.ApprovedAt = time.Now()
	return true
}

// GetTaskStatus returns a task by ID (alias for GetTask).
func (m *MemoryStore) GetTaskStatus(taskID string) *models.Task {
	return m.GetTask(taskID)
}

// AllTasks returns all tasks.
func (m *MemoryStore) AllTasks() []*models.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		cp := *t
		out = append(out, &cp)
	}
	return out
}

// CreateSchedule creates a schedule.
func (m *MemoryStore) CreateSchedule(s *models.Schedule) *models.Schedule {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = time.Now()
	}
	cp := *s
	m.schedules[s.ID] = &cp
	return s
}

// GetSchedule returns a schedule by ID.
func (m *MemoryStore) GetSchedule(id string) *models.Schedule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.schedules[id]
	if !ok {
		return nil
	}
	cp := *s
	return &cp
}

// UpdateSchedule updates a schedule.
func (m *MemoryStore) UpdateSchedule(s *models.Schedule) (*models.Schedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.schedules[s.ID]; !ok {
		return nil, ErrScheduleNotFound
	}
	s.UpdatedAt = time.Now()
	cp := *s
	m.schedules[s.ID] = &cp
	return s, nil
}

// DeleteSchedule deletes a schedule.
func (m *MemoryStore) DeleteSchedule(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.schedules[id]; !ok {
		return false
	}
	delete(m.schedules, id)
	return true
}

// ListSchedules returns schedules, optionally filtered by tenant.
func (m *MemoryStore) ListSchedules(tenantID string) []*models.Schedule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Schedule, 0, len(m.schedules))
	for _, s := range m.schedules {
		if tenantID != "" && s.TenantID != tenantID {
			continue
		}
		cp := *s
		out = append(out, &cp)
	}
	return out
}

// SaveResult saves a task result.
func (m *MemoryStore) SaveResult(r *models.TaskResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[r.TaskID] = r
}

// GetTaskResult returns a task result by task ID.
func (m *MemoryStore) GetTaskResult(taskID string) *models.TaskResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.results[taskID]
	if !ok {
		return nil
	}
	cp := *r
	return &cp
}

// ListTaskResults returns task results with optional filtering.
func (m *MemoryStore) ListTaskResults(tenantID, agentID string, limit int) []*models.TaskResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.TaskResult, 0, len(m.results))
	for _, r := range m.results {
		if agentID != "" && r.AgentID != agentID {
			continue
		}
		cp := *r
		out = append(out, &cp)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// SaveLogs saves logs for a task.
func (m *MemoryStore) SaveLogs(taskID string, logs []models.LogLine) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs[taskID] = logs
}

// GetTaskLogs returns logs for a task.
func (m *MemoryStore) GetTaskLogs(taskID string) []models.LogLine {
	m.mu.RLock()
	defer m.mu.RUnlock()
	logs, ok := m.logs[taskID]
	if !ok {
		return nil
	}
	out := make([]models.LogLine, len(logs))
	copy(out, logs)
	return out
}

// CreateBatch creates a batch.
func (m *MemoryStore) CreateBatch(b *models.BatchTask) *models.BatchTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now()
	}
	cp := *b
	m.batches[b.BatchID] = &cp
	return b
}

// GetBatch returns a batch by ID.
func (m *MemoryStore) GetBatch(batchID string) *models.BatchTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.batches[batchID]
	if !ok {
		return nil
	}
	cp := *b
	return &cp
}

// UpdateBatch updates a batch.
func (m *MemoryStore) UpdateBatch(b *models.BatchTask) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batches[b.BatchID] = b
}

// ListBatches returns batches, optionally filtered by tenant.
func (m *MemoryStore) ListBatches(tenantID string) []*models.BatchTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.BatchTask, 0, len(m.batches))
	for _, b := range m.batches {
		if tenantID != "" && b.TenantID != tenantID {
			continue
		}
		cp := *b
		out = append(out, &cp)
	}
	return out
}

// AddTaskToBatch adds a task to a batch.
func (m *MemoryStore) AddTaskToBatch(batchID, taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batchTask[batchID] = append(m.batchTask[batchID], taskID)
}

// GetBatchTasks returns task IDs in a batch.
func (m *MemoryStore) GetBatchTasks(batchID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids, ok := m.batchTask[batchID]
	if !ok {
		return nil
	}
	out := make([]string, len(ids))
	copy(out, ids)
	return out
}
