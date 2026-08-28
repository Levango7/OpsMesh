package drift

import (
	"testing"

	"github.com/Levango7/OpsMesh/services/config-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/store"
)

func newTestDetector() *Detector {
	st := store.NewMemoryStore("test-key", 50)
	return NewDetector(st)
}

func seedConfig(st store.Store, tenantID, key, value string) {
	st.SetConfig(&models.ConfigEntry{
		ID:       "test-id",
		TenantID: tenantID,
		Key:      key,
		Value:    value,
	})
}

func TestRegisterRule(t *testing.T) {
	d := newTestDetector()
	rule := d.RegisterRule("app/db/host", "localhost", ComparisonExact, "tenant-1", "DB host check")

	if rule.ID == "" {
		t.Error("expected rule ID to be set")
	}
	if rule.ConfigKey != "app/db/host" {
		t.Errorf("expected config key 'app/db/host', got %s", rule.ConfigKey)
	}
	if !rule.Enabled {
		t.Error("expected rule to be enabled by default")
	}

	rules := d.ListRules()
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}
}

func TestUnregisterRule(t *testing.T) {
	d := newTestDetector()
	rule := d.RegisterRule("app/db/host", "localhost", ComparisonExact, "tenant-1", "")

	if !d.UnregisterRule(rule.ID) {
		t.Error("expected UnregisterRule to return true")
	}

	rules := d.ListRules()
	if len(rules) != 0 {
		t.Errorf("expected 0 rules after unregister, got %d", len(rules))
	}

	if d.UnregisterRule("nonexistent") {
		t.Error("expected UnregisterRule to return false for nonexistent rule")
	}
}

func TestCheckDriftExactMatch(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	d := NewDetector(st)
	seedConfig(st, "tenant-1", "app/db/host", "localhost")

	rule := d.RegisterRule("app/db/host", "localhost", ComparisonExact, "tenant-1", "")
	result, err := d.CheckDrift(rule)
	if err != nil {
		t.Fatalf("CheckDrift failed: %v", err)
	}
	if result.Drifted {
		t.Error("expected no drift when values match exactly")
	}
	if result.Actual != "localhost" {
		t.Errorf("expected actual 'localhost', got %s", result.Actual)
	}
}

func TestCheckDriftExactMismatch(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	d := NewDetector(st)
	seedConfig(st, "tenant-1", "app/db/host", "production-server")

	rule := d.RegisterRule("app/db/host", "localhost", ComparisonExact, "tenant-1", "")
	result, err := d.CheckDrift(rule)
	if err != nil {
		t.Fatalf("CheckDrift failed: %v", err)
	}
	if !result.Drifted {
		t.Error("expected drift when values differ")
	}
	if result.Expected != "localhost" {
		t.Errorf("expected 'localhost', got %s", result.Expected)
	}
	if result.Actual != "production-server" {
		t.Errorf("expected 'production-server', got %s", result.Actual)
	}
}

func TestCheckDriftRegexMatch(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	d := NewDetector(st)
	seedConfig(st, "tenant-1", "app/db/port", "5432")

	rule := d.RegisterRule("app/db/port", "^[0-9]+$", ComparisonRegex, "tenant-1", "")
	result, err := d.CheckDrift(rule)
	if err != nil {
		t.Fatalf("CheckDrift failed: %v", err)
	}
	if result.Drifted {
		t.Error("expected no drift when regex matches")
	}
}

func TestCheckDriftRegexMismatch(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	d := NewDetector(st)
	seedConfig(st, "tenant-1", "app/db/port", "not-a-number")

	rule := d.RegisterRule("app/db/port", "^[0-9]+$", ComparisonRegex, "tenant-1", "")
	result, err := d.CheckDrift(rule)
	if err != nil {
		t.Fatalf("CheckDrift failed: %v", err)
	}
	if !result.Drifted {
		t.Error("expected drift when regex does not match")
	}
}

func TestCheckDriftExists(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	d := NewDetector(st)
	seedConfig(st, "tenant-1", "app/api/key", "secret-value")

	rule := d.RegisterRule("app/api/key", "true", ComparisonExists, "tenant-1", "")
	result, err := d.CheckDrift(rule)
	if err != nil {
		t.Fatalf("CheckDrift failed: %v", err)
	}
	if result.Drifted {
		t.Error("expected no drift when config exists and expected is true")
	}
}

func TestCheckDriftExistsMissing(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	d := NewDetector(st)

	rule := d.RegisterRule("app/api/key", "true", ComparisonExists, "tenant-1", "")
	result, err := d.CheckDrift(rule)
	if err != nil {
		t.Fatalf("CheckDrift failed: %v", err)
	}
	if !result.Drifted {
		t.Error("expected drift when config is missing and expected is true")
	}
}

func TestScanAll(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	d := NewDetector(st)
	seedConfig(st, "tenant-1", "app/db/host", "localhost")
	seedConfig(st, "tenant-1", "app/db/port", "9999")

	d.RegisterRule("app/db/host", "localhost", ComparisonExact, "tenant-1", "")
	d.RegisterRule("app/db/port", "5432", ComparisonExact, "tenant-1", "")

	results, err := d.ScanAll()
	if err != nil {
		t.Fatalf("ScanAll failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	driftCount := 0
	for _, r := range results {
		if r.Drifted {
			driftCount++
		}
	}
	if driftCount != 1 {
		t.Errorf("expected 1 drift, got %d", driftCount)
	}
}

func TestGetDriftHistory(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	d := NewDetector(st)
	seedConfig(st, "tenant-1", "app/db/host", "changed")

	rule := d.RegisterRule("app/db/host", "original", ComparisonExact, "tenant-1", "")
	_, err := d.CheckDrift(rule)
	if err != nil {
		t.Fatalf("CheckDrift failed: %v", err)
	}

	history := d.GetDriftHistory()
	if len(history) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(history))
	}
	if !history[0].Drifted {
		t.Error("expected history entry to show drift")
	}
}

func TestGetStatus(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	d := NewDetector(st)
	seedConfig(st, "tenant-1", "app/db/host", "changed")

	d.RegisterRule("app/db/host", "original", ComparisonExact, "tenant-1", "")
	_, _ = d.ScanAll()

	status := d.GetStatus()
	if status.TotalRules != 1 {
		t.Errorf("expected 1 total rule, got %d", status.TotalRules)
	}
	if status.EnabledRules != 1 {
		t.Errorf("expected 1 enabled rule, got %d", status.EnabledRules)
	}
	if status.TotalDrifts != 1 {
		t.Errorf("expected 1 total drift, got %d", status.TotalDrifts)
	}
}

func TestCheckDriftExactMissingConfig(t *testing.T) {
	st := store.NewMemoryStore("test-key", 50)
	d := NewDetector(st)

	rule := d.RegisterRule("app/missing/key", "expected", ComparisonExact, "tenant-1", "")
	result, err := d.CheckDrift(rule)
	if err != nil {
		t.Fatalf("CheckDrift failed: %v", err)
	}
	if !result.Drifted {
		t.Error("expected drift when config does not exist")
	}
	if result.Actual != "" {
		t.Errorf("expected empty actual value, got %s", result.Actual)
	}
}
