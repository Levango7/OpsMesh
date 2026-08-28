package workload

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
)

// ErrWorkloadNotFound is returned when a workload does not exist.
var ErrWorkloadNotFound = errors.New("workload not found")

// ErrWorkloadInvalid is returned when workload data is invalid.
var ErrWorkloadInvalid = errors.New("workload invalid")

// ErrInvalidStateTransition is returned when an invalid status change is attempted.
var ErrInvalidStateTransition = errors.New("invalid workload state transition")

// validTransitions defines allowed status transitions.
var validTransitions = map[models.WorkloadStatus][]models.WorkloadStatus{
	models.WorkloadStatusPending:    {models.WorkloadStatusScheduling, models.WorkloadStatusRunning, models.WorkloadStatusCompleted, models.WorkloadStatusFailed, models.WorkloadStatusCancelled},
	models.WorkloadStatusScheduling: {models.WorkloadStatusRunning, models.WorkloadStatusFailed, models.WorkloadStatusCancelled},
	models.WorkloadStatusRunning:    {models.WorkloadStatusCompleted, models.WorkloadStatusFailed, models.WorkloadStatusCancelled},
	models.WorkloadStatusCompleted:  {},
	models.WorkloadStatusFailed:     {},
	models.WorkloadStatusCancelled:  {},
}

// Manager handles AI workload lifecycle.
type Manager struct {
	mu        sync.RWMutex
	workloads map[string]*models.Workload
	now       func() time.Time
}

// NewManager creates a new workload Manager.
func NewManager(now func() time.Time) *Manager {
	if now == nil {
		now = time.Now
	}
	return &Manager{
		workloads: make(map[string]*models.Workload),
		now:       now,
	}
}

// Submit creates a new workload.
func (m *Manager) Submit(wl *models.Workload) error {
	if wl == nil || wl.Name == "" || wl.TenantID == "" {
		return ErrWorkloadInvalid
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.workloads[wl.ID]; exists {
		return ErrWorkloadInvalid
	}

	now := m.now()
	if wl.ID == "" {
		wl.ID = uuid.New().String()
	}
	wl.Status = models.WorkloadStatusPending
	wl.CreatedAt = now
	wl.UpdatedAt = now
	if wl.Replicas < 1 {
		wl.Replicas = 1
	}

	cp := *wl
	m.workloads[wl.ID] = &cp
	return nil
}

// Get retrieves a workload by ID.
func (m *Manager) Get(id string) (*models.Workload, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	wl, exists := m.workloads[id]
	if !exists {
		return nil, ErrWorkloadNotFound
	}
	cp := *wl
	return &cp, nil
}

// List returns all workloads, optionally filtered by status.
func (m *Manager) List(status models.WorkloadStatus) []*models.Workload {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Workload, 0, len(m.workloads))
	for _, wl := range m.workloads {
		if status != "" && wl.Status != status {
			continue
		}
		cp := *wl
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Update updates a workload.
func (m *Manager) Update(wl *models.Workload) error {
	if wl == nil || wl.ID == "" {
		return ErrWorkloadInvalid
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	old, exists := m.workloads[wl.ID]
	if !exists {
		return ErrWorkloadNotFound
	}

	if wl.Status != "" && wl.Status != old.Status {
		if !isValidTransition(old.Status, wl.Status) {
			return ErrInvalidStateTransition
		}
		wl.UpdatedAt = m.now()
	} else {
		wl.Status = old.Status
		wl.UpdatedAt = m.now()
	}

	wl.CreatedAt = old.CreatedAt

	cp := *wl
	m.workloads[wl.ID] = &cp
	return nil
}

// Cancel cancels a workload.
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wl, exists := m.workloads[id]
	if !exists {
		return ErrWorkloadNotFound
	}

	if !isValidTransition(wl.Status, models.WorkloadStatusCancelled) {
		return ErrInvalidStateTransition
	}

	now := m.now()
	wl.Status = models.WorkloadStatusCancelled
	wl.UpdatedAt = now
	wl.FinishedAt = &now
	return nil
}

// Scale updates the replicas count for a workload.
func (m *Manager) Scale(id string, replicas int) error {
	if replicas < 0 {
		return ErrWorkloadInvalid
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	wl, exists := m.workloads[id]
	if !exists {
		return ErrWorkloadNotFound
	}

	wl.Replicas = replicas
	wl.UpdatedAt = m.now()
	return nil
}

// AssignNode assigns a node to a workload (scheduling result).
func (m *Manager) AssignNode(id string, nodeIDs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wl, exists := m.workloads[id]
	if !exists {
		return ErrWorkloadNotFound
	}

	if !isValidTransition(wl.Status, models.WorkloadStatusRunning) {
		return ErrInvalidStateTransition
	}

	now := m.now()
	wl.NodeIDs = nodeIDs
	wl.Status = models.WorkloadStatusRunning
	wl.UpdatedAt = now
	wl.StartedAt = &now
	return nil
}

// MarkCompleted marks a workload as completed.
func (m *Manager) MarkCompleted(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wl, exists := m.workloads[id]
	if !exists {
		return ErrWorkloadNotFound
	}

	if !isValidTransition(wl.Status, models.WorkloadStatusCompleted) {
		return ErrInvalidStateTransition
	}

	now := m.now()
	wl.Status = models.WorkloadStatusCompleted
	wl.UpdatedAt = now
	wl.FinishedAt = &now
	return nil
}

// MarkFailed marks a workload as failed with an error message.
func (m *Manager) MarkFailed(id, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wl, exists := m.workloads[id]
	if !exists {
		return ErrWorkloadNotFound
	}

	if !isValidTransition(wl.Status, models.WorkloadStatusFailed) {
		return ErrInvalidStateTransition
	}

	now := m.now()
	wl.Status = models.WorkloadStatusFailed
	wl.ErrorMsg = errMsg
	wl.UpdatedAt = now
	wl.FinishedAt = &now
	return nil
}

// GetRunningWorkloads returns all running workloads.
func (m *Manager) GetRunningWorkloads() []*models.Workload {
	return m.List(models.WorkloadStatusRunning)
}

// GetPendingWorkloads returns all pending workloads.
func (m *Manager) GetPendingWorkloads() []*models.Workload {
	return m.List(models.WorkloadStatusPending)
}

func isValidTransition(from, to models.WorkloadStatus) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}
