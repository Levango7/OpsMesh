package store

import (
	"errors"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/models"
	"github.com/google/uuid"
)

// ErrNotFound is returned when a record is not found.
var ErrNotFound = errors.New("not found")

// ErrTenantMismatch is returned when tenant access is unauthorized.
var ErrTenantMismatch = errors.New("tenant mismatch")

// ErrInvalidInput is returned when input validation fails.
var ErrInvalidInput = errors.New("invalid input")

// Store is the interface for all persistence operations.
type Store interface {
	// Deployment operations
	CreateDeployment(d *models.Deployment) (*models.Deployment, error)
	GetDeployment(id, tenantID string) (*models.Deployment, error)
	ListDeployments(tenantID, status string) ([]*models.Deployment, error)
	UpdateDeployment(d *models.Deployment) error

	// Template operations
	CreateTemplate(t *models.Template) (*models.Template, error)
	GetTemplate(id, tenantID string) (*models.Template, error)
	UpdateTemplate(t *models.Template) error
	DeleteTemplate(id, tenantID string) error
	ListTemplates(tenantID string) ([]*models.Template, error)

	// Strategy operations
	CreateStrategy(s *models.Strategy) (*models.Strategy, error)
	GetStrategy(id, tenantID string) (*models.Strategy, error)
	UpdateStrategy(s *models.Strategy) error
	DeleteStrategy(id, tenantID string) error
	ListStrategies(tenantID string) ([]*models.Strategy, error)

	// Canary operations
	CreateCanary(c *models.Canary) (*models.Canary, error)
	GetCanary(id, tenantID string) (*models.Canary, error)
	UpdateCanary(c *models.Canary) error
	ListCanaries(tenantID, status string) ([]*models.Canary, error)
}

// MemoryStore is an in-memory implementation of Store.
type MemoryStore struct {
	mu          sync.RWMutex
	deployments map[string]*models.Deployment
	templates   map[string]*models.Template
	strategies  map[string]*models.Strategy
	canaries    map[string]*models.Canary
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		deployments: make(map[string]*models.Deployment),
		templates:   make(map[string]*models.Template),
		strategies:  make(map[string]*models.Strategy),
		canaries:    make(map[string]*models.Canary),
	}
}

// Deployment operations

func (m *MemoryStore) CreateDeployment(d *models.Deployment) (*models.Deployment, error) {
	if d == nil {
		return nil, errors.Join(ErrInvalidInput, errors.New("nil deployment"))
	}
	if d.Name == "" {
		return nil, errors.Join(ErrInvalidInput, errors.New("name required"))
	}
	if !models.IsValidDeploymentType(d.Type) {
		return nil, errors.Join(ErrInvalidInput, errors.New("invalid type"))
	}
	if len(d.TargetIDs) == 0 {
		return nil, errors.Join(ErrInvalidInput, errors.New("target_ids required"))
	}
	if d.TenantID == "" {
		return nil, errors.Join(ErrInvalidInput, errors.New("tenant_id required"))
	}

	now := time.Now()
	d.ID = uuid.New().String()
	d.Status = models.DeploymentStatusPending
	d.CreatedAt = now
	d.UpdatedAt = now
	if d.Strategy == "" {
		d.Strategy = models.StrategyRolling
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.deployments[d.ID] = d
	return d, nil
}

func (m *MemoryStore) GetDeployment(id, tenantID string) (*models.Deployment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.deployments[id]
	if !ok {
		return nil, ErrNotFound
	}
	if tenantID != "" && d.TenantID != tenantID {
		return nil, ErrTenantMismatch
	}
	return d, nil
}

func (m *MemoryStore) ListDeployments(tenantID, status string) ([]*models.Deployment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Deployment, 0)
	for _, d := range m.deployments {
		if tenantID != "" && d.TenantID != tenantID {
			continue
		}
		if status != "" && d.Status != status {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

func (m *MemoryStore) UpdateDeployment(d *models.Deployment) error {
	if d == nil {
		return errors.Join(ErrInvalidInput, errors.New("nil deployment"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.deployments[d.ID]
	if !ok {
		return ErrNotFound
	}
	if d.TenantID != "" && old.TenantID != d.TenantID {
		return ErrTenantMismatch
	}
	d.UpdatedAt = time.Now()
	m.deployments[d.ID] = d
	return nil
}

// Template operations

func (m *MemoryStore) CreateTemplate(t *models.Template) (*models.Template, error) {
	if t == nil {
		return nil, errors.Join(ErrInvalidInput, errors.New("nil template"))
	}
	if t.Name == "" {
		return nil, errors.Join(ErrInvalidInput, errors.New("name required"))
	}
	if t.TenantID == "" {
		return nil, errors.Join(ErrInvalidInput, errors.New("tenant_id required"))
	}

	now := time.Now()
	t.ID = uuid.New().String()
	t.CreatedAt = now
	t.UpdatedAt = now
	if t.Parameters == nil {
		t.Parameters = make(map[string]string)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.templates[t.ID] = t
	return t, nil
}

func (m *MemoryStore) GetTemplate(id, tenantID string) (*models.Template, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.templates[id]
	if !ok {
		return nil, ErrNotFound
	}
	if tenantID != "" && t.TenantID != tenantID {
		return nil, ErrTenantMismatch
	}
	return t, nil
}

func (m *MemoryStore) UpdateTemplate(t *models.Template) error {
	if t == nil {
		return errors.Join(ErrInvalidInput, errors.New("nil template"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.templates[t.ID]
	if !ok {
		return ErrNotFound
	}
	if t.TenantID != "" && old.TenantID != t.TenantID {
		return ErrTenantMismatch
	}
	t.UpdatedAt = time.Now()
	m.templates[t.ID] = t
	return nil
}

func (m *MemoryStore) DeleteTemplate(id, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.templates[id]
	if !ok {
		return ErrNotFound
	}
	if tenantID != "" && t.TenantID != tenantID {
		return ErrTenantMismatch
	}
	delete(m.templates, id)
	return nil
}

func (m *MemoryStore) ListTemplates(tenantID string) ([]*models.Template, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Template, 0)
	for _, t := range m.templates {
		if tenantID != "" && t.TenantID != tenantID {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// Strategy operations

func (m *MemoryStore) CreateStrategy(s *models.Strategy) (*models.Strategy, error) {
	if s == nil {
		return nil, errors.Join(ErrInvalidInput, errors.New("nil strategy"))
	}
	if s.Name == "" {
		return nil, errors.Join(ErrInvalidInput, errors.New("name required"))
	}
	if !models.IsValidStrategy(s.Type) {
		return nil, errors.Join(ErrInvalidInput, errors.New("invalid strategy type"))
	}
	if s.TenantID == "" {
		return nil, errors.Join(ErrInvalidInput, errors.New("tenant_id required"))
	}

	now := time.Now()
	s.ID = uuid.New().String()
	s.CreatedAt = now
	s.UpdatedAt = now

	m.mu.Lock()
	defer m.mu.Unlock()
	m.strategies[s.ID] = s
	return s, nil
}

func (m *MemoryStore) GetStrategy(id, tenantID string) (*models.Strategy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.strategies[id]
	if !ok {
		return nil, ErrNotFound
	}
	if tenantID != "" && s.TenantID != tenantID {
		return nil, ErrTenantMismatch
	}
	return s, nil
}

func (m *MemoryStore) UpdateStrategy(s *models.Strategy) error {
	if s == nil {
		return errors.Join(ErrInvalidInput, errors.New("nil strategy"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.strategies[s.ID]
	if !ok {
		return ErrNotFound
	}
	if s.TenantID != "" && old.TenantID != s.TenantID {
		return ErrTenantMismatch
	}
	s.UpdatedAt = time.Now()
	m.strategies[s.ID] = s
	return nil
}

func (m *MemoryStore) DeleteStrategy(id, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.strategies[id]
	if !ok {
		return ErrNotFound
	}
	if tenantID != "" && s.TenantID != tenantID {
		return ErrTenantMismatch
	}
	delete(m.strategies, id)
	return nil
}

func (m *MemoryStore) ListStrategies(tenantID string) ([]*models.Strategy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Strategy, 0)
	for _, s := range m.strategies {
		if tenantID != "" && s.TenantID != tenantID {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// Canary operations

func (m *MemoryStore) CreateCanary(c *models.Canary) (*models.Canary, error) {
	if c == nil {
		return nil, errors.Join(ErrInvalidInput, errors.New("nil canary"))
	}
	if c.Name == "" {
		return nil, errors.Join(ErrInvalidInput, errors.New("name required"))
	}
	if c.TenantID == "" {
		return nil, errors.Join(ErrInvalidInput, errors.New("tenant_id required"))
	}

	now := time.Now()
	c.ID = uuid.New().String()
	c.Status = models.CanaryStatusPending
	c.SuccessCount = 0
	c.FailureCount = 0
	c.CreatedAt = now
	c.UpdatedAt = now

	m.mu.Lock()
	defer m.mu.Unlock()
	m.canaries[c.ID] = c
	return c, nil
}

func (m *MemoryStore) GetCanary(id, tenantID string) (*models.Canary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.canaries[id]
	if !ok {
		return nil, ErrNotFound
	}
	if tenantID != "" && c.TenantID != tenantID {
		return nil, ErrTenantMismatch
	}
	return c, nil
}

func (m *MemoryStore) UpdateCanary(c *models.Canary) error {
	if c == nil {
		return errors.Join(ErrInvalidInput, errors.New("nil canary"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.canaries[c.ID]
	if !ok {
		return ErrNotFound
	}
	if c.TenantID != "" && old.TenantID != c.TenantID {
		return ErrTenantMismatch
	}
	c.UpdatedAt = time.Now()
	m.canaries[c.ID] = c
	return nil
}

func (m *MemoryStore) ListCanaries(tenantID, status string) ([]*models.Canary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.Canary, 0)
	for _, c := range m.canaries {
		if tenantID != "" && c.TenantID != tenantID {
			continue
		}
		if status != "" && c.Status != status {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}
