package rotation

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/config-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/store"
)

func newTestManager() *Manager {
	st := store.NewMemoryStore("test-encryption-key", 50)
	return NewManager(st)
}

func seedSecret(st store.Store, tenantID, key, value string) {
	st.CreateSecret(&models.SecretEntry{
		ID:       "test-secret-id",
		TenantID: tenantID,
		Key:      key,
		Value:    value,
		KeyType:  "passphrase",
	})
}

func TestRegisterPolicy(t *testing.T) {
	m := newTestManager()

	policy, err := m.RegisterPolicy("tenant-1", "app/db/password", 24*time.Hour)
	if err != nil {
		t.Fatalf("RegisterPolicy failed: %v", err)
	}

	if policy.ID == "" {
		t.Error("expected policy ID to be set")
	}
	if policy.TenantID != "tenant-1" {
		t.Errorf("expected tenantID 'tenant-1', got %s", policy.TenantID)
	}
	if policy.SecretKey != "app/db/password" {
		t.Errorf("expected secretKey 'app/db/password', got %s", policy.SecretKey)
	}
	if !policy.Enabled {
		t.Error("expected policy to be enabled by default")
	}
	if policy.Status != StatusActive {
		t.Errorf("expected status 'active', got %s", policy.Status)
	}
	if !policy.NextRotation.After(policy.CreatedAt) {
		t.Error("expected nextRotation to be after createdAt")
	}
}

func TestRegisterPolicyDuplicate(t *testing.T) {
	m := newTestManager()

	_, err := m.RegisterPolicy("tenant-1", "app/db/password", 24*time.Hour)
	if err != nil {
		t.Fatalf("First RegisterPolicy failed: %v", err)
	}

	_, err = m.RegisterPolicy("tenant-1", "app/db/password", 48*time.Hour)
	if err == nil {
		t.Error("expected error for duplicate policy, got nil")
	}
}

func TestRegisterPolicyValidation(t *testing.T) {
	m := newTestManager()

	_, err := m.RegisterPolicy("", "app/db/password", 24*time.Hour)
	if err == nil {
		t.Error("expected error for empty tenantID")
	}

	_, err = m.RegisterPolicy("tenant-1", "", 24*time.Hour)
	if err == nil {
		t.Error("expected error for empty secretKey")
	}

	_, err = m.RegisterPolicy("tenant-1", "app/db/password", 0)
	if err == nil {
		t.Error("expected error for zero interval")
	}

	_, err = m.RegisterPolicy("tenant-1", "app/db/password", -1*time.Hour)
	if err == nil {
		t.Error("expected error for negative interval")
	}
}

func TestListPolicies(t *testing.T) {
	m := newTestManager()

	m.RegisterPolicy("tenant-1", "app/db/password", 24*time.Hour)
	m.RegisterPolicy("tenant-1", "app/api/key", 48*time.Hour)
	m.RegisterPolicy("tenant-2", "app/db/password", 12*time.Hour)

	policies := m.ListPolicies()
	if len(policies) != 3 {
		t.Errorf("expected 3 policies, got %d", len(policies))
	}
}

func TestCheckRotationDue(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	m := NewManager(st)
	seedSecret(st, "tenant-1", "app/db/password", "old-value")

	policy, err := m.RegisterPolicy("tenant-1", "app/db/password", 1*time.Nanosecond)
	if err != nil {
		t.Fatalf("RegisterPolicy failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	due, err := m.CheckRotation(policy.ID)
	if err != nil {
		t.Fatalf("CheckRotation failed: %v", err)
	}
	if !due {
		t.Error("expected rotation to be due after interval elapsed")
	}
}

func TestCheckRotationNotDue(t *testing.T) {
	m := newTestManager()

	policy, err := m.RegisterPolicy("tenant-1", "app/db/password", 24*time.Hour)
	if err != nil {
		t.Fatalf("RegisterPolicy failed: %v", err)
	}

	due, err := m.CheckRotation(policy.ID)
	if err != nil {
		t.Fatalf("CheckRotation failed: %v", err)
	}
	if due {
		t.Error("expected rotation to not be due when interval hasn't elapsed")
	}
}

func TestCheckRotationDisabled(t *testing.T) {
	m := newTestManager()

	policy, err := m.RegisterPolicy("tenant-1", "app/db/password", 1*time.Nanosecond)
	if err != nil {
		t.Fatalf("RegisterPolicy failed: %v", err)
	}

	m.mu.Lock()
	policy.Enabled = false
	m.mu.Unlock()

	due, err := m.CheckRotation(policy.ID)
	if err != nil {
		t.Fatalf("CheckRotation failed: %v", err)
	}
	if due {
		t.Error("expected rotation to not be due when policy is disabled")
	}
}

func TestRotateSecret(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	m := NewManager(st)
	seedSecret(st, "tenant-1", "app/db/password", "old-value")

	policy, err := m.RegisterPolicy("tenant-1", "app/db/password", 24*time.Hour)
	if err != nil {
		t.Fatalf("RegisterPolicy failed: %v", err)
	}

	result, err := m.RotateSecret(policy.ID)
	if err != nil {
		t.Fatalf("RotateSecret failed: %v", err)
	}

	if result.Status != StatusCompleted {
		t.Errorf("expected status 'completed', got %s", result.Status)
	}
	if result.OldVersion != 1 {
		t.Errorf("expected old version 1, got %d", result.OldVersion)
	}
	if result.NewVersion != 2 {
		t.Errorf("expected new version 2, got %d", result.NewVersion)
	}
	if result.RotatedAt.IsZero() {
		t.Error("expected rotatedAt to be set")
	}

	entry, found := st.GetSecret("tenant-1", "app/db/password")
	if !found {
		t.Fatal("expected secret to exist after rotation")
	}
	if entry.Value == "old-value" {
		t.Error("expected secret value to be changed after rotation")
	}
	if entry.Version != 2 {
		t.Errorf("expected version 2 after rotation, got %d", entry.Version)
	}
}

func TestRotateSecretPolicyNotFound(t *testing.T) {
	m := newTestManager()

	_, err := m.RotateSecret("nonexistent-policy")
	if err == nil {
		t.Error("expected error for nonexistent policy")
	}
}

func TestRotateSecretSecretNotFound(t *testing.T) {
	m := newTestManager()

	policy, err := m.RegisterPolicy("tenant-1", "app/db/password", 24*time.Hour)
	if err != nil {
		t.Fatalf("RegisterPolicy failed: %v", err)
	}

	_, err = m.RotateSecret(policy.ID)
	if err == nil {
		t.Error("expected error when secret does not exist in store")
	}
}

func TestGetRotationStatus(t *testing.T) {
	m := newTestManager()

	policy, err := m.RegisterPolicy("tenant-1", "app/db/password", 24*time.Hour)
	if err != nil {
		t.Fatalf("RegisterPolicy failed: %v", err)
	}

	got, err := m.GetRotationStatus(policy.ID)
	if err != nil {
		t.Fatalf("GetRotationStatus failed: %v", err)
	}

	if got.ID != policy.ID {
		t.Errorf("expected policy ID %s, got %s", policy.ID, got.ID)
	}
	if got.Status != StatusActive {
		t.Errorf("expected status 'active', got %s", got.Status)
	}
}

func TestListDueRotations(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	m := NewManager(st)
	seedSecret(st, "tenant-1", "app/db/password", "value1")
	seedSecret(st, "tenant-1", "app/api/key", "value2")
	seedSecret(st, "tenant-2", "app/db/password", "value3")

	m.RegisterPolicy("tenant-1", "app/db/password", 1*time.Nanosecond)
	m.RegisterPolicy("tenant-1", "app/api/key", 48*time.Hour)
	m.RegisterPolicy("tenant-2", "app/db/password", 1*time.Nanosecond)

	time.Sleep(10 * time.Millisecond)

	due := m.ListDueRotations()
	if len(due) != 2 {
		t.Errorf("expected 2 due rotations, got %d", len(due))
	}
}

func TestUnregisterPolicy(t *testing.T) {
	m := newTestManager()

	policy, err := m.RegisterPolicy("tenant-1", "app/db/password", 24*time.Hour)
	if err != nil {
		t.Fatalf("RegisterPolicy failed: %v", err)
	}

	if !m.UnregisterPolicy(policy.ID) {
		t.Error("expected UnregisterPolicy to return true")
	}

	policies := m.ListPolicies()
	if len(policies) != 0 {
		t.Errorf("expected 0 policies after unregister, got %d", len(policies))
	}

	if m.UnregisterPolicy("nonexistent") {
		t.Error("expected UnregisterPolicy to return false for nonexistent policy")
	}
}

func TestGetStatus(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	m := NewManager(st)
	seedSecret(st, "tenant-1", "app/db/password", "value1")
	seedSecret(st, "tenant-1", "app/api/key", "value2")

	m.RegisterPolicy("tenant-1", "app/db/password", 1*time.Nanosecond)
	m.RegisterPolicy("tenant-1", "app/api/key", 48*time.Hour)

	time.Sleep(10 * time.Millisecond)

	status := m.GetStatus()
	if status["totalPolicies"] != 2 {
		t.Errorf("expected 2 total policies, got %d", status["totalPolicies"])
	}
	if status["enabledPolicies"] != 2 {
		t.Errorf("expected 2 enabled policies, got %d", status["enabledPolicies"])
	}
	if status["dueRotations"] != 1 {
		t.Errorf("expected 1 due rotation, got %d", status["dueRotations"])
	}
}

func TestRotationHistory(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	m := NewManager(st)
	seedSecret(st, "tenant-1", "app/db/password", "old-value")

	policy, err := m.RegisterPolicy("tenant-1", "app/db/password", 24*time.Hour)
	if err != nil {
		t.Fatalf("RegisterPolicy failed: %v", err)
	}

	_, err = m.RotateSecret(policy.ID)
	if err != nil {
		t.Fatalf("RotateSecret failed: %v", err)
	}

	history := m.GetRotationHistory()
	if len(history) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(history))
	}
	if history[0].Status != StatusCompleted {
		t.Errorf("expected history status 'completed', got %s", history[0].Status)
	}
}
