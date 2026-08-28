package store

import (
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/portal-svc/internal/models"
)

// Store is the interface for portal data persistence.
type Store interface {
	// Resource requests
	CreateRequest(*models.ResourceRequest) *models.ResourceRequest
	GetRequest(id string) *models.ResourceRequest
	ListRequests(tenantID, status string) []*models.ResourceRequest
	UpdateRequest(*models.ResourceRequest) bool
	DeleteRequest(id string) bool

	// Quotas
	SetQuota(tenantID string, q *models.Quota) error
	GetQuota(tenantID string) *models.Quota
	ListQuotas() []*models.Quota

	// Budgets
	SetBudget(b *models.Budget) error
	GetBudget(tenantID string) *models.Budget

	// Recommendations
	AddRecommendation(*models.CostRecommendation)
	ListRecommendations(tenantID string) []*models.CostRecommendation

	// Utilization
	SetUtilization(*models.Utilization)
	GetUtilization(tenantID string) *models.Utilization

	// Activity log
	AddActivity(*models.ActivityEvent)
	ListActivity(tenantID string, limit int) []*models.ActivityEvent
}

// MemoryStore is an in-memory implementation of Store.
type MemoryStore struct {
	mu             sync.RWMutex
	requests       map[string]*models.ResourceRequest
	quotas         map[string]*models.Quota
	budgets        map[string]*models.Budget
	recommendations map[string]*models.CostRecommendation
	utilizations   map[string]*models.Utilization
	activities     []*models.ActivityEvent
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		requests:        make(map[string]*models.ResourceRequest),
		quotas:          make(map[string]*models.Quota),
		budgets:         make(map[string]*models.Budget),
		recommendations: make(map[string]*models.CostRecommendation),
		utilizations:    make(map[string]*models.Utilization),
		activities:      make([]*models.ActivityEvent, 0),
	}
}

// CreateRequest adds a new resource request.
func (m *MemoryStore) CreateRequest(r *models.ResourceRequest) *models.ResourceRequest {
	if r == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	r.UpdatedAt = time.Now()
	cp := *r
	m.requests[r.ID] = &cp
	return r
}

// GetRequest retrieves a request by ID.
func (m *MemoryStore) GetRequest(id string) *models.ResourceRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.requests[id]
	if !ok {
		return nil
	}
	cp := *r
	return &cp
}

// ListRequests returns requests filtered by tenant and status.
func (m *MemoryStore) ListRequests(tenantID, status string) []*models.ResourceRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.ResourceRequest, 0, len(m.requests))
	for _, r := range m.requests {
		if tenantID != "" && r.TenantID != tenantID {
			continue
		}
		if status != "" && string(r.Status) != status {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	return out
}

// UpdateRequest updates an existing request.
func (m *MemoryStore) UpdateRequest(r *models.ResourceRequest) bool {
	if r == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.requests[r.ID]; !ok {
		return false
	}
	r.UpdatedAt = time.Now()
	cp := *r
	m.requests[r.ID] = &cp
	return true
}

// DeleteRequest removes a request.
func (m *MemoryStore) DeleteRequest(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.requests[id]; !ok {
		return false
	}
	delete(m.requests, id)
	return true
}

// SetQuota sets a tenant's quota.
func (m *MemoryStore) SetQuota(tenantID string, q *models.Quota) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if q == nil {
		delete(m.quotas, tenantID)
		return nil
	}
	cp := *q
	m.quotas[tenantID] = &cp
	return nil
}

// GetQuota retrieves a tenant's quota.
func (m *MemoryStore) GetQuota(tenantID string) *models.Quota {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q, ok := m.quotas[tenantID]
	if !ok {
		return nil
	}
	cp := *q
	return &cp
}

// ListQuotas returns all quotas.
func (m *MemoryStore) ListQuotas() []*models.Quota {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Quota, 0, len(m.quotas))
	for _, q := range m.quotas {
		cp := *q
		out = append(out, &cp)
	}
	return out
}

// SetBudget sets a tenant's budget.
func (m *MemoryStore) SetBudget(b *models.Budget) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b == nil {
		return nil
	}
	cp := *b
	m.budgets[b.TenantID] = &cp
	return nil
}

// GetBudget retrieves a tenant's budget.
func (m *MemoryStore) GetBudget(tenantID string) *models.Budget {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.budgets[tenantID]
	if !ok {
		return nil
	}
	cp := *b
	return &cp
}

// AddRecommendation adds a cost recommendation.
func (m *MemoryStore) AddRecommendation(r *models.CostRecommendation) {
	if r == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *r
	m.recommendations[r.ID] = &cp
}

// ListRecommendations returns recommendations for a tenant.
func (m *MemoryStore) ListRecommendations(tenantID string) []*models.CostRecommendation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.CostRecommendation, 0, len(m.recommendations))
	for _, r := range m.recommendations {
		if tenantID != "" && r.TenantID != tenantID {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	return out
}

// SetUtilization sets utilization metrics.
func (m *MemoryStore) SetUtilization(u *models.Utilization) {
	if u == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *u
	m.utilizations[u.TenantID] = &cp
}

// GetUtilization retrieves utilization metrics.
func (m *MemoryStore) GetUtilization(tenantID string) *models.Utilization {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.utilizations[tenantID]
	if !ok {
		return nil
	}
	cp := *u
	return &cp
}

// AddActivity adds an activity event.
func (m *MemoryStore) AddActivity(a *models.ActivityEvent) {
	if a == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.Timestamp.IsZero() {
		a.Timestamp = time.Now()
	}
	cp := *a
	m.activities = append(m.activities, &cp)
}

// ListActivity returns recent activity events.
func (m *MemoryStore) ListActivity(tenantID string, limit int) []*models.ActivityEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.ActivityEvent, 0)
	for i := len(m.activities) - 1; i >= 0; i-- {
		a := m.activities[i]
		if tenantID != "" && a.TenantID != tenantID {
			continue
		}
		cp := *a
		out = append(out, &cp)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	// Reverse to chronological order
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
