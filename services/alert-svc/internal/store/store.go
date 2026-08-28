package store

import (
	"sync"
	"time"
)

// Alert represents an alert event in the store.
type Alert struct {
	AlertID   string
	TenantID  string
	DeviceID  string
	AgentID   string
	Severity  string
	Message   string
	Metric    string
	CreatedAt time.Time
	Status    string
	AcknowledgedBy string
	SilencedUntil  time.Time
	Comment        string
	UpdatedAt      time.Time
}

// AlertRule represents an alert rule in the store.
type AlertRule struct {
	ID          string
	TenantID    string
	Metric      string
	Op          string
	Threshold   float64
	ForDuration int
	Severity    string
	Message     string
	Enabled     bool
	CreatedAt   time.Time
	CreatedBy   string
}

// AlertStore is the interface for alert persistence.
type AlertStore interface {
	Alerts(tenantID string) []*Alert
	AddAlert(*Alert)
	Alert(id string) *Alert
	AckAlert(id, tenantID, by string) bool
	SilenceAlert(id, tenantID, by string, until time.Time, comment string) bool
	ResolveAlert(id, tenantID, by string) bool
	CreateAlertRule(*AlertRule) *AlertRule
	ListAlertRules(tenantID string) []*AlertRule
	DeleteAlertRule(id string) bool
	GetAlertRule(id string) *AlertRule
	UpdateAlertRule(*AlertRule) bool
}

// MemoryStore is an in-memory implementation of AlertStore.
type MemoryStore struct {
	mu          sync.RWMutex
	alerts      []*Alert
	alertRules  map[string]*AlertRule
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		alerts:     make([]*Alert, 0),
		alertRules: make(map[string]*AlertRule),
	}
}

// Alerts returns alerts, optionally filtered by tenant.
func (m *MemoryStore) Alerts(tenantID string) []*Alert {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Alert, 0, len(m.alerts))
	for _, a := range m.alerts {
		if tenantID != "" && a.TenantID != tenantID {
			continue
		}
		cp := *a
		out = append(out, &cp)
	}
	return out
}

// AddAlert adds an alert.
func (m *MemoryStore) AddAlert(a *Alert) {
	if a == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	if a.Status == "" {
		a.Status = "firing"
	}
	m.alerts = append(m.alerts, a)
}

// Alert returns an alert by ID.
func (m *MemoryStore) Alert(id string) *Alert {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, a := range m.alerts {
		if a.AlertID == id {
			cp := *a
			return &cp
		}
	}
	return nil
}

// AckAlert acknowledges an alert.
func (m *MemoryStore) AckAlert(id, tenantID, by string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.alerts {
		if a.AlertID == id && (tenantID == "" || a.TenantID == tenantID) {
			a.Status = "acknowledged"
			a.AcknowledgedBy = by
			a.UpdatedAt = time.Now()
			return true
		}
	}
	return false
}

// SilenceAlert silences an alert.
func (m *MemoryStore) SilenceAlert(id, tenantID, by string, until time.Time, comment string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if until.IsZero() {
		until = time.Now().Add(24 * time.Hour)
	}
	for _, a := range m.alerts {
		if a.AlertID == id && (tenantID == "" || a.TenantID == tenantID) {
			a.Status = "silenced"
			a.AcknowledgedBy = by
			a.SilencedUntil = until
			a.Comment = comment
			a.UpdatedAt = time.Now()
			return true
		}
	}
	return false
}

// ResolveAlert resolves an alert.
func (m *MemoryStore) ResolveAlert(id, tenantID, by string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.alerts {
		if a.AlertID == id && (tenantID == "" || a.TenantID == tenantID) {
			a.Status = "resolved"
			a.AcknowledgedBy = by
			a.UpdatedAt = time.Now()
			return true
		}
	}
	return false
}

// CreateAlertRule creates a rule.
func (m *MemoryStore) CreateAlertRule(r *AlertRule) *AlertRule {
	if r == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	cp := *r
	m.alertRules[r.ID] = &cp
	return r
}

// ListAlertRules returns rules, optionally filtered by tenant.
func (m *MemoryStore) ListAlertRules(tenantID string) []*AlertRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*AlertRule, 0, len(m.alertRules))
	for _, r := range m.alertRules {
		if tenantID != "" && r.TenantID != tenantID {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	return out
}

// DeleteAlertRule deletes a rule.
func (m *MemoryStore) DeleteAlertRule(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.alertRules[id]; !ok {
		return false
	}
	delete(m.alertRules, id)
	return true
}

// GetAlertRule returns a rule by ID.
func (m *MemoryStore) GetAlertRule(id string) *AlertRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.alertRules[id]
	if !ok {
		return nil
	}
	cp := *r
	return &cp
}

// UpdateAlertRule updates a rule.
func (m *MemoryStore) UpdateAlertRule(r *AlertRule) bool {
	if r == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.alertRules[r.ID]; !ok {
		return false
	}
	cp := *r
	m.alertRules[r.ID] = &cp
	return true
}
