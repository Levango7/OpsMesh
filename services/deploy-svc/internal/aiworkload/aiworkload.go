package aiworkload

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Workload type constants.
const (
	WorkloadTypeInference  = "inference"
	WorkloadTypeTraining   = "training"
	WorkloadTypeFineTuning = "fine-tuning"
)

// Workload status constants.
const (
	WorkloadStatusPending    = "pending"
	WorkloadStatusDeploying  = "deploying"
	WorkloadStatusRunning    = "running"
	WorkloadStatusFailed     = "failed"
	WorkloadStatusStopped    = "stopped"
	WorkloadStatusScaling    = "scaling"
	WorkloadStatusRolledBack = "rolled_back"
)

// GPURequirements specifies the GPU requirements for an AI workload.
type GPURequirements struct {
	MinVRAMGB            int    `json:"min_vram_gb"`
	ComputeCapability    string `json:"compute_capability"`
	GPUCount             int    `json:"gpu_count"`
	GPUModel             string `json:"gpu_model"`
	MultiGPU             bool   `json:"multi_gpu"`
	NVLink               bool   `json:"nvlink"`
	MinMemoryBandwidthGB int    `json:"min_memory_bandwidth_gb"`
}

// AIWorkload represents an AI workload deployment.
type AIWorkload struct {
	ID               string            `json:"id"`
	TenantID         string            `json:"tenant_id"`
	Name             string            `json:"name"`
	Type             string            `json:"type"`
	ModelName        string            `json:"model_name"`
	GPURequirements  GPURequirements   `json:"gpu_requirements"`
	Status           string            `json:"status"`
	Replicas         int               `json:"replicas"`
	MaxReplicas      int               `json:"max_replicas"`
	ContainerImage   string            `json:"container_image"`
	EnvVars          map[string]string `json:"env_vars"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	ErrorMessage     string            `json:"error_message"`
	DeploymentTarget string            `json:"deployment_target"`
	ReplicasBefore   int               `json:"replicas_before,omitempty"`
}

// Validation errors.
var (
	ErrInvalidWorkload        = errors.New("invalid workload")
	ErrWorkloadNotFound       = errors.New("workload not found")
	ErrTenantMismatch         = errors.New("tenant mismatch")
	ErrInvalidGPURequirements = errors.New("invalid GPU requirements")
	ErrInvalidScaleTarget     = errors.New("invalid scale target")
	ErrCannotRollback         = errors.New("cannot rollback workload")
)

// Manager manages AI workload deployments.
type Manager struct {
	mu        sync.RWMutex
	workloads map[string]*AIWorkload
	counter   uint64
}

// NewManager creates a new AI workload Manager.
func NewManager() *Manager {
	return &Manager{
		workloads: make(map[string]*AIWorkload),
	}
}

// Validate checks if the AI workload is valid.
func (w *AIWorkload) Validate() error {
	if w.Name == "" {
		return fmt.Errorf("%w: name required", ErrInvalidWorkload)
	}
	if !IsValidWorkloadType(w.Type) {
		return fmt.Errorf("%w: invalid workload type %q", ErrInvalidWorkload, w.Type)
	}
	if w.ModelName == "" {
		return fmt.Errorf("%w: model_name required", ErrInvalidWorkload)
	}
	if err := w.GPURequirements.Validate(); err != nil {
		return err
	}
	if w.Replicas < 0 {
		return fmt.Errorf("%w: replicas cannot be negative", ErrInvalidWorkload)
	}
	if w.MaxReplicas < w.Replicas {
		return fmt.Errorf("%w: max_replicas must be >= replicas", ErrInvalidWorkload)
	}
	return nil
}

// Validate checks if the GPU requirements are valid.
func (g GPURequirements) Validate() error {
	if g.GPUCount <= 0 {
		return fmt.Errorf("%w: gpu_count must be positive", ErrInvalidGPURequirements)
	}
	if g.MinVRAMGB <= 0 {
		return fmt.Errorf("%w: min_vram_gb must be positive", ErrInvalidGPURequirements)
	}
	if g.MultiGPU && g.GPUCount < 2 {
		return fmt.Errorf("%w: multi-GPU requires gpu_count >= 2", ErrInvalidGPURequirements)
	}
	if g.NVLink && !g.MultiGPU {
		return fmt.Errorf("%w: NVLink requires multi-GPU", ErrInvalidGPURequirements)
	}
	if g.MinMemoryBandwidthGB < 0 {
		return fmt.Errorf("%w: min_memory_bandwidth_gb cannot be negative", ErrInvalidGPURequirements)
	}
	validCC := map[string]bool{
		"7.0": true, "7.5": true, "8.0": true, "8.6": true, "8.9": true, "9.0": true,
	}
	if g.ComputeCapability != "" && !validCC[g.ComputeCapability] {
		return fmt.Errorf("%w: unsupported compute capability %q", ErrInvalidGPURequirements, g.ComputeCapability)
	}
	return nil
}

// IsValidWorkloadType checks if the workload type is valid.
func IsValidWorkloadType(t string) bool {
	switch t {
	case WorkloadTypeInference, WorkloadTypeTraining, WorkloadTypeFineTuning:
		return true
	}
	return false
}

// IsTerminalStatus checks if the workload status is terminal.
func IsTerminalStatus(status string) bool {
	switch status {
	case WorkloadStatusFailed, WorkloadStatusStopped, WorkloadStatusRolledBack:
		return true
	}
	return false
}

// Deploy deploys the AI workload.
func (m *Manager) Deploy(w *AIWorkload) (*AIWorkload, error) {
	if err := w.Validate(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	w.ID = generateID()
	w.Status = WorkloadStatusDeploying
	w.CreatedAt = now
	w.UpdatedAt = now
	if w.EnvVars == nil {
		w.EnvVars = make(map[string]string)
	}
	if w.MaxReplicas < w.Replicas {
		w.MaxReplicas = w.Replicas
	}

	m.workloads[w.ID] = w

	w.Status = WorkloadStatusRunning
	w.UpdatedAt = time.Now()

	return w, nil
}

// GetStatus retrieves the current status of a workload.
func (m *Manager) GetStatus(id, tenantID string) (*AIWorkload, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	w, ok := m.workloads[id]
	if !ok {
		return nil, ErrWorkloadNotFound
	}
	if tenantID != "" && w.TenantID != tenantID {
		return nil, ErrTenantMismatch
	}
	return w, nil
}

// Scale scales the workload to the target replica count.
func (m *Manager) Scale(id, tenantID string, targetReplicas int) (*AIWorkload, error) {
	if targetReplicas < 0 {
		return nil, fmt.Errorf("%w: target replicas cannot be negative", ErrInvalidScaleTarget)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	w, ok := m.workloads[id]
	if !ok {
		return nil, ErrWorkloadNotFound
	}
	if tenantID != "" && w.TenantID != tenantID {
		return nil, ErrTenantMismatch
	}
	if IsTerminalStatus(w.Status) {
		return nil, fmt.Errorf("%w: cannot scale workload in status %s", ErrInvalidScaleTarget, w.Status)
	}
	if targetReplicas > w.MaxReplicas {
		return nil, fmt.Errorf("%w: target replicas %d exceeds max_replicas %d", ErrInvalidScaleTarget, targetReplicas, w.MaxReplicas)
	}

	w.ReplicasBefore = w.Replicas
	w.Replicas = targetReplicas
	w.Status = WorkloadStatusScaling
	w.UpdatedAt = time.Now()

	w.Status = WorkloadStatusRunning
	w.UpdatedAt = time.Now()

	return w, nil
}

// Rollback rolls back the workload to the previous replica count.
func (m *Manager) Rollback(id, tenantID string) (*AIWorkload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, ok := m.workloads[id]
	if !ok {
		return nil, ErrWorkloadNotFound
	}
	if tenantID != "" && w.TenantID != tenantID {
		return nil, ErrTenantMismatch
	}
	if w.ReplicasBefore == 0 {
		return nil, fmt.Errorf("%w: no previous state to rollback to", ErrCannotRollback)
	}

	w.Replicas = w.ReplicasBefore
	w.ReplicasBefore = 0
	w.Status = WorkloadStatusRolledBack
	w.UpdatedAt = time.Now()

	return w, nil
}

// Stop stops the workload.
func (m *Manager) Stop(id, tenantID string) (*AIWorkload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, ok := m.workloads[id]
	if !ok {
		return nil, ErrWorkloadNotFound
	}
	if tenantID != "" && w.TenantID != tenantID {
		return nil, ErrTenantMismatch
	}
	if IsTerminalStatus(w.Status) {
		return nil, fmt.Errorf("%w: workload already in terminal status %s", ErrInvalidWorkload, w.Status)
	}

	w.Status = WorkloadStatusStopped
	w.UpdatedAt = time.Now()

	return w, nil
}

// List returns all workloads for a tenant, optionally filtered by status.
func (m *Manager) List(tenantID, status string) []*AIWorkload {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*AIWorkload, 0)
	for _, w := range m.workloads {
		if tenantID != "" && w.TenantID != tenantID {
			continue
		}
		if status != "" && w.Status != status {
			continue
		}
		out = append(out, w)
	}
	return out
}

// GetLogs returns simulated logs for a workload.
func (m *Manager) GetLogs(id, tenantID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	w, ok := m.workloads[id]
	if !ok {
		return "", ErrWorkloadNotFound
	}
	if tenantID != "" && w.TenantID != tenantID {
		return "", ErrTenantMismatch
	}

	return fmt.Sprintf(
		"[%s] AI Workload: %s\nModel: %s\nType: %s\nStatus: %s\nGPUs: %d x %s (%d GB VRAM)\nReplicas: %d/%d\n",
		w.UpdatedAt.Format(time.RFC3339),
		w.Name,
		w.ModelName,
		w.Type,
		w.Status,
		w.GPURequirements.GPUCount,
		w.GPURequirements.GPUModel,
		w.GPURequirements.MinVRAMGB,
		w.Replicas,
		w.MaxReplicas,
	), nil
}

func generateID() string {
	n := atomic.AddUint64(&idCounter, 1)
	return fmt.Sprintf("aiw_%d_%d", time.Now().UnixNano(), n)
}

var idCounter uint64
