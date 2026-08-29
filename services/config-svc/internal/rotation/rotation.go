package rotation

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/config-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/store"
)

// RotationStatus represents the current state of a rotation policy.
type RotationStatus string

const (
	StatusActive    RotationStatus = "active"
	StatusPaused    RotationStatus = "paused"
	StatusError     RotationStatus = "error"
	StatusCompleted RotationStatus = "completed"
)

// RotationPolicy defines when and how a secret should be rotated.
type RotationPolicy struct {
	ID               string         `json:"id"`
	SecretID         string         `json:"secretId"`
	TenantID         string         `json:"tenantId"`
	SecretKey        string         `json:"secretKey"`
	RotationInterval time.Duration  `json:"rotationInterval"`
	Enabled          bool           `json:"enabled"`
	LastRotation     time.Time      `json:"lastRotation"`
	NextRotation     time.Time      `json:"nextRotation"`
	Status           RotationStatus `json:"status"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

// RotationResult captures the outcome of a secret rotation.
type RotationResult struct {
	SecretID   string         `json:"secretId"`
	OldVersion int            `json:"oldVersion"`
	NewVersion int            `json:"newVersion"`
	RotatedAt  time.Time      `json:"rotatedAt"`
	Status     RotationStatus `json:"status"`
	Error      string         `json:"error,omitempty"`
}

// Manager manages secret rotation policies and executes rotations.
type Manager struct {
	mu       sync.RWMutex
	policies map[string]*RotationPolicy
	store    store.Store
	results  []RotationResult
	maxHist  int
}

// NewManager creates a new rotation Manager.
func NewManager(st store.Store) *Manager {
	return &Manager{
		policies: make(map[string]*RotationPolicy),
		store:    st,
		results:  make([]RotationResult, 0),
		maxHist:  1000,
	}
}

// RegisterPolicy creates a new rotation policy for a secret.
func (m *Manager) RegisterPolicy(tenantID, secretKey string, interval time.Duration) (*RotationPolicy, error) {
	if tenantID == "" || secretKey == "" {
		return nil, fmt.Errorf("tenantID and secretKey are required")
	}
	if interval <= 0 {
		return nil, fmt.Errorf("rotation interval must be positive")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.policies {
		if p.TenantID == tenantID && p.SecretKey == secretKey {
			return nil, fmt.Errorf("rotation policy already exists for tenant=%s key=%s", tenantID, secretKey)
		}
	}

	now := time.Now()
	policy := &RotationPolicy{
		ID:               uuid.New().String(),
		SecretID:         uuid.New().String(),
		TenantID:         tenantID,
		SecretKey:        secretKey,
		RotationInterval: interval,
		Enabled:          true,
		LastRotation:     time.Time{},
		NextRotation:     now.Add(interval),
		Status:           StatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	m.policies[policy.ID] = policy
	return policy, nil
}

// UnregisterPolicy removes a rotation policy by ID.
func (m *Manager) UnregisterPolicy(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.policies[id]; !ok {
		return false
	}
	delete(m.policies, id)
	return true
}

// ListPolicies returns all registered rotation policies.
func (m *Manager) ListPolicies() []*RotationPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	policies := make([]*RotationPolicy, 0, len(m.policies))
	for _, p := range m.policies {
		policies = append(policies, p)
	}
	return policies
}

// GetPolicy returns a rotation policy by ID.
func (m *Manager) GetPolicy(id string) (*RotationPolicy, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.policies[id]
	return p, ok
}

// CheckRotation evaluates whether a policy's secret is due for rotation.
func (m *Manager) CheckRotation(policyID string) (bool, error) {
	m.mu.RLock()
	policy, ok := m.policies[policyID]
	m.mu.RUnlock()

	if !ok {
		return false, fmt.Errorf("rotation policy not found: %s", policyID)
	}

	if !policy.Enabled {
		return false, nil
	}

	return time.Now().After(policy.NextRotation) || time.Now().Equal(policy.NextRotation), nil
}

// RotateSecret performs the rotation of a secret according to its policy.
func (m *Manager) RotateSecret(policyID string) (*RotationResult, error) {
	m.mu.Lock()
	policy, ok := m.policies[policyID]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("rotation policy not found: %s", policyID)
	}

	if !policy.Enabled {
		policy.Status = StatusPaused
		m.mu.Unlock()
		return nil, fmt.Errorf("rotation policy is disabled: %s", policyID)
	}

	existing, found := m.store.GetSecret(policy.TenantID, policy.SecretKey)
	if !found {
		m.mu.Unlock()
		return nil, fmt.Errorf("secret not found for policy %s: tenant=%s key=%s", policyID, policy.TenantID, policy.SecretKey)
	}

	oldVersion := existing.Version
	newValue := generateSecretValue()

	meta := m.store.RotateSecret(policy.TenantID, policy.SecretKey, newValue)
	if meta == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to rotate secret for policy %s", policyID)
	}

	now := time.Now()
	policy.LastRotation = now
	policy.NextRotation = now.Add(policy.RotationInterval)
	policy.Status = StatusActive
	policy.UpdatedAt = now

	result := RotationResult{
		SecretID:   policy.SecretID,
		OldVersion: oldVersion,
		NewVersion: meta.Version,
		RotatedAt:  now,
		Status:     StatusCompleted,
	}
	m.results = append(m.results, result)
	if len(m.results) > m.maxHist {
		m.results = m.results[len(m.results)-m.maxHist:]
	}

	m.mu.Unlock()
	return &result, nil
}

// GetRotationStatus returns the current rotation status for a policy.
func (m *Manager) GetRotationStatus(policyID string) (*RotationPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	policy, ok := m.policies[policyID]
	if !ok {
		return nil, fmt.Errorf("rotation policy not found: %s", policyID)
	}
	return policy, nil
}

// ListDueRotations returns all policies whose secrets are due for rotation.
func (m *Manager) ListDueRotations() []*RotationPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()

	due := make([]*RotationPolicy, 0)
	now := time.Now()
	for _, p := range m.policies {
		if p.Enabled && (now.After(p.NextRotation) || now.Equal(p.NextRotation)) {
			due = append(due, p)
		}
	}
	return due
}

// GetRotationHistory returns the rotation result history.
func (m *Manager) GetRotationHistory() []RotationResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]RotationResult, len(m.results))
	copy(out, m.results)
	return out
}

// GetStatus returns a summary of rotation manager state.
func (m *Manager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.policies)
	enabled := 0
	due := 0
	now := time.Now()
	for _, p := range m.policies {
		if p.Enabled {
			enabled++
		}
		if p.Enabled && (now.After(p.NextRotation) || now.Equal(p.NextRotation)) {
			due++
		}
	}

	return map[string]interface{}{
		"totalPolicies":   total,
		"enabledPolicies": enabled,
		"dueRotations":    due,
		"totalHistory":    len(m.results),
	}
}

// ListSecretsDueForRotation returns secret metadata for all secrets due for rotation.
func (m *Manager) ListSecretsDueForRotation() []*models.SecretMeta {
	policies := m.ListDueRotations()
	secrets := make([]*models.SecretMeta, 0, len(policies))
	for _, p := range policies {
		entry, found := m.store.GetSecret(p.TenantID, p.SecretKey)
		if found {
			secrets = append(secrets, &models.SecretMeta{
				ID:        entry.ID,
				TenantID:  entry.TenantID,
				Key:       entry.Key,
				KeyType:   entry.KeyType,
				Version:   entry.Version,
				CreatedAt: entry.CreatedAt,
				UpdatedAt: entry.UpdatedAt,
			})
		}
	}
	return secrets
}

func generateSecretValue() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
