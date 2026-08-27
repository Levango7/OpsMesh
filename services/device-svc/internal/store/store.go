package store

import (
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/device-svc/internal/models"
)

// DeviceStore is the interface for device persistence.
type DeviceStore interface {
	RegisterDevice(*models.Device) *models.Device
	Device(id string) *models.Device
	ListDevices(tenantID, status, group string, limit int) []*models.Device
	UpdateDevice(*models.Device) (*models.Device, bool)
	DeleteDevice(id string) bool
	Heartbeat(deviceID, status string) bool
	GetDeviceStatus(deviceID string) *models.DeviceStatus
	DevicesByAgent(agentID string) []*models.Device
}

// AgentStore is the interface for agent persistence.
type AgentStore interface {
	RegisterAgent(*models.Agent) *models.Agent
	Agent(id string) *models.Agent
	ListAgents(tenantID, status string, limit int) []*models.Agent
	UpdateAgentStatus(agentID, status string, load int) (*models.Agent, bool)
	AgentHeartbeat(agentID, status string, load int) bool
}

// CiStore is the interface for CMDB persistence.
type CiStore interface {
	CreateCI(*models.CI) *models.CI
	GetCI(id, tenantID string) *models.CI
	UpdateCI(*models.CI) (*models.CI, bool)
	DeleteCI(id, tenantID string) bool
	ListCIs(tenantID, ciType, status string, limit int) []*models.CI
	CreateRelation(*models.CIRelation) *models.CIRelation
	GetCIRelations(ciID, tenantID string) []*models.CIRelation
}

// DiscoveryStore is the interface for discovery job persistence.
type DiscoveryStore interface {
	CreateJob(*models.DiscoveryJob) *models.DiscoveryJob
	GetJob(id string) *models.DiscoveryJob
	ListJobs(tenantID string) []*models.DiscoveryJob
	UpdateJob(*models.DiscoveryJob) (*models.DiscoveryJob, bool)
}

// MemoryStore is an in-memory implementation of all stores.
type MemoryStore struct {
	devices   map[string]*models.Device
	agents    map[string]*models.Agent
	cis       map[string]*models.CI
	relations map[int64]*models.CIRelation
	jobs      map[string]*models.DiscoveryJob
	relSeq    int64
	mu        sync.RWMutex
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		devices:   make(map[string]*models.Device),
		agents:    make(map[string]*models.Agent),
		cis:       make(map[string]*models.CI),
		relations: make(map[int64]*models.CIRelation),
		jobs:      make(map[string]*models.DiscoveryJob),
	}
}

// === DeviceStore implementation ===

// RegisterDevice registers a new device.
func (m *MemoryStore) RegisterDevice(d *models.Device) *models.Device {
	if d == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	if d.Status == "" {
		d.Status = "online"
	}
	d.UpdatedAt = now
	cp := *d
	m.devices[d.ID] = &cp
	return d
}

// Device returns a device by ID.
func (m *MemoryStore) Device(id string) *models.Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.devices[id]
	if !ok {
		return nil
	}
	cp := *d
	return &cp
}

// ListDevices returns devices with optional filtering.
func (m *MemoryStore) ListDevices(tenantID, status, group string, limit int) []*models.Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Device, 0, len(m.devices))
	for _, d := range m.devices {
		if tenantID != "" && d.TenantID != tenantID {
			continue
		}
		if status != "" && d.Status != status {
			continue
		}
		if group != "" && d.Group != group {
			continue
		}
		cp := *d
		out = append(out, &cp)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// UpdateDevice updates an existing device.
func (m *MemoryStore) UpdateDevice(d *models.Device) (*models.Device, bool) {
	if d == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.devices[d.ID]; !ok {
		return nil, false
	}
	d.UpdatedAt = time.Now()
	cp := *d
	m.devices[d.ID] = &cp
	return d, true
}

// DeleteDevice removes a device.
func (m *MemoryStore) DeleteDevice(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.devices[id]; !ok {
		return false
	}
	delete(m.devices, id)
	return true
}

// Heartbeat updates device heartbeat timestamp.
func (m *MemoryStore) Heartbeat(deviceID, status string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[deviceID]
	if !ok {
		return false
	}
	d.LastHeartbeat = time.Now()
	if status != "" {
		d.Status = status
	}
	d.UpdatedAt = time.Now()
	return true
}

// GetDeviceStatus returns device status.
func (m *MemoryStore) GetDeviceStatus(deviceID string) *models.DeviceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.devices[deviceID]
	if !ok {
		return nil
	}
	return &models.DeviceStatus{
		DeviceID:      d.ID,
		Status:        d.Status,
		Reachable:     d.Status == "online",
		LastHeartbeat: d.LastHeartbeat,
	}
}

// DevicesByAgent returns devices managed by a specific agent.
func (m *MemoryStore) DevicesByAgent(agentID string) []*models.Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Device, 0)
	for _, d := range m.devices {
		if d.AgentID == agentID {
			cp := *d
			out = append(out, &cp)
		}
	}
	return out
}

// === AgentStore implementation ===

// RegisterAgent registers a new agent.
func (m *MemoryStore) RegisterAgent(a *models.Agent) *models.Agent {
	if a == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	if a.Status == "" {
		a.Status = "online"
	}
	a.UpdatedAt = now
	cp := *a
	m.agents[a.ID] = &cp
	return a
}

// Agent returns an agent by ID.
func (m *MemoryStore) Agent(id string) *models.Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.agents[id]
	if !ok {
		return nil
	}
	cp := *a
	return &cp
}

// ListAgents returns agents with optional filtering.
func (m *MemoryStore) ListAgents(tenantID, status string, limit int) []*models.Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Agent, 0, len(m.agents))
	for _, a := range m.agents {
		if tenantID != "" && a.TenantID != tenantID {
			continue
		}
		if status != "" && a.Status != status {
			continue
		}
		cp := *a
		out = append(out, &cp)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// UpdateAgentStatus updates agent status and load.
func (m *MemoryStore) UpdateAgentStatus(agentID, status string, load int) (*models.Agent, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[agentID]
	if !ok {
		return nil, false
	}
	a.Status = status
	a.Load = load
	a.LastHeartbeat = time.Now()
	a.UpdatedAt = time.Now()
	cp := *a
	return &cp, true
}

// AgentHeartbeat updates agent heartbeat (distinct name to avoid conflict with DeviceStore.Heartbeat).
func (m *MemoryStore) AgentHeartbeat(agentID, status string, load int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[agentID]
	if !ok {
		return false
	}
	a.LastHeartbeat = time.Now()
	a.Load = load
	if status != "" {
		a.Status = status
	}
	a.UpdatedAt = time.Now()
	return true
}

// === CiStore implementation ===

// CreateCI creates a new CI.
func (m *MemoryStore) CreateCI(ci *models.CI) *models.CI {
	if ci == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if ci.CreatedAt.IsZero() {
		ci.CreatedAt = now
	}
	if ci.Status == "" {
		ci.Status = "active"
	}
	ci.Version = 1
	ci.UpdatedAt = now
	cp := *ci
	m.cis[ci.ID] = &cp
	return ci
}

// GetCI returns a CI by ID.
func (m *MemoryStore) GetCI(id, tenantID string) *models.CI {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ci, ok := m.cis[id]
	if !ok {
		return nil
	}
	if tenantID != "" && ci.TenantID != tenantID {
		return nil
	}
	cp := *ci
	return &cp
}

// UpdateCI updates an existing CI.
func (m *MemoryStore) UpdateCI(ci *models.CI) (*models.CI, bool) {
	if ci == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.cis[ci.ID]
	if !ok {
		return nil, false
	}
	ci.Version = old.Version + 1
	ci.CreatedAt = old.CreatedAt
	ci.UpdatedAt = time.Now()
	cp := *ci
	m.cis[ci.ID] = &cp
	return ci, true
}

// DeleteCI removes a CI.
func (m *MemoryStore) DeleteCI(id, tenantID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	ci, ok := m.cis[id]
	if !ok {
		return false
	}
	if tenantID != "" && ci.TenantID != tenantID {
		return false
	}
	delete(m.cis, id)
	return true
}

// ListCIs returns CIs with optional filtering.
func (m *MemoryStore) ListCIs(tenantID, ciType, status string, limit int) []*models.CI {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.CI, 0, len(m.cis))
	for _, ci := range m.cis {
		if tenantID != "" && ci.TenantID != tenantID {
			continue
		}
		if ciType != "" && ci.CiType != ciType {
			continue
		}
		if status != "" && ci.Status != status {
			continue
		}
		cp := *ci
		out = append(out, &cp)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// CreateRelation creates a CI relationship.
func (m *MemoryStore) CreateRelation(rel *models.CIRelation) *models.CIRelation {
	if rel == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.relSeq++
	rel.ID = m.relSeq
	rel.CreatedAt = time.Now()
	cp := *rel
	m.relations[rel.ID] = &cp
	return rel
}

// GetCIRelations returns relations for a CI.
func (m *MemoryStore) GetCIRelations(ciID, tenantID string) []*models.CIRelation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.CIRelation, 0)
	for _, rel := range m.relations {
		if rel.SourceCIID == ciID || rel.TargetCIID == ciID {
			if tenantID != "" && rel.TenantID != tenantID {
				continue
			}
			cp := *rel
			out = append(out, &cp)
		}
	}
	return out
}

// === DiscoveryStore implementation ===

// CreateJob creates a new discovery job.
func (m *MemoryStore) CreateJob(job *models.DiscoveryJob) *models.DiscoveryJob {
	if job == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if job.StartedAt.IsZero() {
		job.StartedAt = now
	}
	if job.Status == "" {
		job.Status = "pending"
	}
	cp := *job
	m.jobs[job.ID] = &cp
	return job
}

// GetJob returns a discovery job by ID.
func (m *MemoryStore) GetJob(id string) *models.DiscoveryJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	if !ok {
		return nil
	}
	cp := *job
	return &cp
}

// ListJobs returns discovery jobs for a tenant.
func (m *MemoryStore) ListJobs(tenantID string) []*models.DiscoveryJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.DiscoveryJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		if tenantID != "" && job.TenantID != tenantID {
			continue
		}
		cp := *job
		out = append(out, &cp)
	}
	return out
}

// UpdateJob updates a discovery job.
func (m *MemoryStore) UpdateJob(job *models.DiscoveryJob) (*models.DiscoveryJob, bool) {
	if job == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.jobs[job.ID]; !ok {
		return nil, false
	}
	cp := *job
	m.jobs[job.ID] = &cp
	return job, true
}
