package quota

import (
	"errors"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
)

// ErrQuotaNotFound is returned when a quota does not exist.
var ErrQuotaNotFound = errors.New("quota not found")

// ErrQuotaExceeded is returned when a quota limit would be exceeded.
var ErrQuotaExceeded = errors.New("quota exceeded")

// ErrInvalidQuota is returned when quota data is invalid.
var ErrInvalidQuota = errors.New("invalid quota")

// Manager handles GPU quota management per tenant.
type Manager struct {
	mu     sync.RWMutex
	quotas map[string]*models.GPUQuota
	now    func() time.Time
}

// NewManager creates a new quota Manager.
func NewManager(now func() time.Time) *Manager {
	if now == nil {
		now = time.Now
	}
	return &Manager{
		quotas: make(map[string]*models.GPUQuota),
		now:    now,
	}
}

// SetQuota creates or updates a quota for a tenant.
func (m *Manager) SetQuota(quota *models.GPUQuota) error {
	if quota == nil || quota.TenantID == "" {
		return ErrInvalidQuota
	}
	if quota.MaxGPUs < 0 || quota.MaxVRAMMB < 0 || quota.MaxWorkloads < 0 {
		return ErrInvalidQuota
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	quota.UpdatedAt = now

	if existing, ok := m.quotas[quota.TenantID]; ok {
		quota.CreatedAt = existing.CreatedAt
	} else {
		quota.CreatedAt = now
	}

	cp := *quota
	m.quotas[quota.TenantID] = &cp
	return nil
}

// GetQuota retrieves a quota by tenant ID.
func (m *Manager) GetQuota(tenantID string) (*models.GPUQuota, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q, ok := m.quotas[tenantID]
	if !ok {
		return nil, ErrQuotaNotFound
	}
	cp := *q
	return &cp, nil
}

// ListQuotas returns all quotas.
func (m *Manager) ListQuotas() []*models.GPUQuota {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.GPUQuota, 0, len(m.quotas))
	for _, q := range m.quotas {
		cp := *q
		out = append(out, &cp)
	}
	return out
}

// DeleteQuota removes a quota.
func (m *Manager) DeleteQuota(tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.quotas[tenantID]; !ok {
		return ErrQuotaNotFound
	}
	delete(m.quotas, tenantID)
	return nil
}

// CheckAllocation checks if a tenant can allocate the requested resources.
func (m *Manager) CheckAllocation(tenantID string, gpuCount, vramMB, workloads int) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q, ok := m.quotas[tenantID]
	if !ok {
		return nil
	}

	if q.UsedGPUs+gpuCount > q.MaxGPUs {
		return ErrQuotaExceeded
	}
	if q.UsedVRAMMB+vramMB > q.MaxVRAMMB {
		return ErrQuotaExceeded
	}
	if q.UsedWorkloads+workloads > q.MaxWorkloads {
		return ErrQuotaExceeded
	}
	return nil
}

// RecordAllocation records resource usage for a tenant.
func (m *Manager) RecordAllocation(tenantID string, gpuCount, vramMB, workloads int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, ok := m.quotas[tenantID]
	if !ok {
		return nil
	}

	q.UsedGPUs += gpuCount
	q.UsedVRAMMB += vramMB
	q.UsedWorkloads += workloads
	q.UpdatedAt = m.now()
	return nil
}

// ReleaseAllocation releases resources for a tenant.
func (m *Manager) ReleaseAllocation(tenantID string, gpuCount, vramMB, workloads int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, ok := m.quotas[tenantID]
	if !ok {
		return nil
	}

	q.UsedGPUs -= gpuCount
	if q.UsedGPUs < 0 {
		q.UsedGPUs = 0
	}
	q.UsedVRAMMB -= vramMB
	if q.UsedVRAMMB < 0 {
		q.UsedVRAMMB = 0
	}
	q.UsedWorkloads -= workloads
	if q.UsedWorkloads < 0 {
		q.UsedWorkloads = 0
	}
	q.UpdatedAt = m.now()
	return nil
}

// GetUsage returns the current quota usage for a tenant.
func (m *Manager) GetUsage(tenantID string) (*models.QuotaUsage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q, ok := m.quotas[tenantID]
	if !ok {
		return nil, ErrQuotaNotFound
	}

	return &models.QuotaUsage{
		TenantID:      q.TenantID,
		UsedGPUs:      q.UsedGPUs,
		UsedVRAMMB:    q.UsedVRAMMB,
		UsedWorkloads: q.UsedWorkloads,
		MaxGPUs:       q.MaxGPUs,
		MaxVRAMMB:     q.MaxVRAMMB,
		MaxWorkloads:  q.MaxWorkloads,
		UpdatedAt:     q.UpdatedAt,
	}, nil
}
