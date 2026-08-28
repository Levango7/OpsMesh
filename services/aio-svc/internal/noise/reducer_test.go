package noise

import (
	"testing"
	"time"
)

func TestClusterAlerts_Empty(t *testing.T) {
	r := NewReducer()
	clusters := r.ClusterAlerts([]Alert{})
	if len(clusters) != 0 {
		t.Errorf("expected 0 clusters, got %d", len(clusters))
	}
}

func TestClusterAlerts_GroupsByRule(t *testing.T) {
	r := NewReducer()
	now := time.Now()
	alerts := []Alert{
		{Id: "1", RuleId: "rule-a", DeviceId: "srv-01-web", Severity: "critical", FiredAt: now},
		{Id: "2", RuleId: "rule-a", DeviceId: "srv-01-db", Severity: "critical", FiredAt: now},
		{Id: "3", RuleId: "rule-b", DeviceId: "srv-01-web", Severity: "warning", FiredAt: now},
	}
	clusters := r.ClusterAlerts(alerts)
	if len(clusters) != 2 {
		t.Errorf("expected 2 clusters, got %d", len(clusters))
	}
}

func TestClusterAlerts_CountSorted(t *testing.T) {
	r := NewReducer()
	now := time.Now()
	alerts := []Alert{
		{Id: "1", RuleId: "rule-a", DeviceId: "srv-01-web", Severity: "critical", FiredAt: now},
		{Id: "2", RuleId: "rule-a", DeviceId: "srv-01-db", Severity: "critical", FiredAt: now},
		{Id: "3", RuleId: "rule-a", DeviceId: "srv-01-api", Severity: "critical", FiredAt: now},
		{Id: "4", RuleId: "rule-b", DeviceId: "srv-01-web", Severity: "warning", FiredAt: now},
	}
	clusters := r.ClusterAlerts(alerts)
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}
	// First cluster should have more alerts
	if clusters[0].Count < clusters[1].Count {
		t.Error("clusters should be sorted by count descending")
	}
}

func TestDetectFlapping_NotFlapping(t *testing.T) {
	r := NewReducer()
	now := time.Now()
	states := []AlertState{
		{Status: "firing", Timestamp: now.Add(-5 * time.Minute)},
		{Status: "resolved", Timestamp: now},
	}
	result := r.DetectFlapping("alert-1", 10*time.Minute, states)
	if result.IsFlapping {
		t.Error("expected not flapping for single state change")
	}
	if result.StateChanges != 1 {
		t.Errorf("expected 1 state change, got %d", result.StateChanges)
	}
}

func TestDetectFlapping_IsFlapping(t *testing.T) {
	r := NewReducer()
	now := time.Now()
	states := []AlertState{
		{Status: "firing", Timestamp: now.Add(-10 * time.Minute)},
		{Status: "resolved", Timestamp: now.Add(-8 * time.Minute)},
		{Status: "firing", Timestamp: now.Add(-6 * time.Minute)},
		{Status: "resolved", Timestamp: now.Add(-4 * time.Minute)},
		{Status: "firing", Timestamp: now.Add(-2 * time.Minute)},
		{Status: "resolved", Timestamp: now},
	}
	result := r.DetectFlapping("alert-2", 15*time.Minute, states)
	if !result.IsFlapping {
		t.Error("expected flapping for repeated state changes")
	}
	if result.StateChanges != 5 {
		t.Errorf("expected 5 state changes, got %d", result.StateChanges)
	}
}

func TestDetectFlapping_EmptyStates(t *testing.T) {
	r := NewReducer()
	result := r.DetectFlapping("alert-3", 10*time.Minute, []AlertState{})
	if result.IsFlapping {
		t.Error("expected not flapping for empty states")
	}
}

func TestCompressAlerts_Deduplicates(t *testing.T) {
	r := NewReducer()
	now := time.Now()
	alerts := []Alert{
		{Id: "1", RuleId: "rule-a", DeviceId: "srv-01", Message: "CPU high", FiredAt: now},
		{Id: "2", RuleId: "rule-a", DeviceId: "srv-01", Message: "CPU high", FiredAt: now.Add(10 * time.Second)},
		{Id: "3", RuleId: "rule-b", DeviceId: "srv-02", Message: "Memory low", FiredAt: now},
	}
	compressed, orig, comp, err := r.CompressAlerts(alerts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if orig != 3 {
		t.Errorf("expected original count 3, got %d", orig)
	}
	if comp != 2 {
		t.Errorf("expected compressed count 2, got %d", comp)
	}
	if len(compressed) != 2 {
		t.Errorf("expected 2 compressed alerts, got %d", len(compressed))
	}
}

func TestCompressAlerts_Empty(t *testing.T) {
	r := NewReducer()
	compressed, orig, comp, err := r.CompressAlerts([]Alert{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if orig != 0 || comp != 0 {
		t.Errorf("expected 0 counts, got orig=%d comp=%d", orig, comp)
	}
	if len(compressed) != 0 {
		t.Errorf("expected empty compressed, got %d", len(compressed))
	}
}

func TestDevicePrefix(t *testing.T) {
	tests := []struct {
		deviceId string
		want     string
	}{
		{"srv-01-web", "srv-01"},
		{"db-master-01", "db-master"},
		{"single", "single"},
	}
	for _, tt := range tests {
		got := devicePrefix(tt.deviceId)
		if got != tt.want {
			t.Errorf("devicePrefix(%s) = %s, want %s", tt.deviceId, got, tt.want)
		}
	}
}
