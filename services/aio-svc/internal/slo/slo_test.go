package slo

import (
	"testing"
)

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if len(m.rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(m.rules))
	}
}

func TestAddRule(t *testing.T) {
	m := NewManager()
	rule := SLORule{
		Name:    "api-availability",
		Target:  99.9,
		Window:  "30d",
		SLIType: SLIAvailability,
	}
	m.AddRule(rule)
	if len(m.rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(m.rules))
	}
}

func TestEvaluateSLO_AvailabilityHealthy(t *testing.T) {
	m := NewManager()
	rule := SLORule{
		Name:    "api-availability",
		Target:  99.9,
		Window:  "30d",
		SLIType: SLIAvailability,
	}
	result := m.EvaluateSLO(rule, 999, 1000, 0)
	if result.Status != StatusHealthy && result.Status != StatusWarning {
		t.Errorf("expected healthy or warning status, got %s", result.Status)
	}
	if result.CurrentValue != 99.9 {
		t.Errorf("expected current value 99.9, got %f", result.CurrentValue)
	}
}

func TestEvaluateSLO_AvailabilityBreached(t *testing.T) {
	m := NewManager()
	rule := SLORule{
		Name:    "api-availability",
		Target:  99.9,
		Window:  "30d",
		SLIType: SLIAvailability,
	}
	result := m.EvaluateSLO(rule, 950, 1000, 0)
	if result.Status != StatusBreached {
		t.Errorf("expected breached status, got %s", result.Status)
	}
	if result.CurrentValue != 95.0 {
		t.Errorf("expected current value 95.0, got %f", result.CurrentValue)
	}
}

func TestEvaluateSLO_ErrorRateHealthy(t *testing.T) {
	m := NewManager()
	rule := SLORule{
		Name:    "api-error-rate",
		Target:  99.0,
		Window:  "7d",
		SLIType: SLIErrorRate,
	}
	result := m.EvaluateSLO(rule, 1000, 1000, 5)
	expectedValue := (1.0 - 5.0/1000.0) * 100.0
	if result.CurrentValue != expectedValue {
		t.Errorf("expected current value %f, got %f", expectedValue, result.CurrentValue)
	}
	if result.SLIType != SLIErrorRate {
		t.Errorf("expected error_rate type, got %s", result.SLIType)
	}
}

func TestCalculateErrorBudget_Positive(t *testing.T) {
	m := NewManager()
	budget := m.CalculateErrorBudget(99.95, 99.9)
	expected := 99.9 - 99.95
	if budget != expected {
		t.Errorf("expected budget %f, got %f", expected, budget)
	}
}

func TestCalculateErrorBudget_Negative(t *testing.T) {
	m := NewManager()
	budget := m.CalculateErrorBudget(99.0, 99.9)
	if budget != 0.9 {
		t.Errorf("expected budget 0.9, got %f", budget)
	}
}

func TestCalculateBurnRate_Zero(t *testing.T) {
	m := NewManager()
	rate := m.CalculateBurnRate(100.0, 99.9, "30d")
	if rate != 0.0 {
		t.Errorf("expected burn rate 0.0 for perfect availability, got %f", rate)
	}
}

func TestCalculateBurnRate_High(t *testing.T) {
	m := NewManager()
	rate := m.CalculateBurnRate(99.0, 99.9, "30d")
	expected := 10.0
	diff := rate - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 0.001 {
		t.Errorf("expected burn rate ~10.0, got %f", rate)
	}
}

func TestListSLOMetrics(t *testing.T) {
	m := NewManager()
	m.AddRule(SLORule{Name: "rule1", Target: 99.9, Window: "30d", SLIType: SLIAvailability})
	m.AddRule(SLORule{Name: "rule2", Target: 99.0, Window: "7d", SLIType: SLIErrorRate})
	metrics := m.ListSLOMetrics()
	if len(metrics) != 2 {
		t.Errorf("expected 2 metrics, got %d", len(metrics))
	}
}

func TestGetStatusOverview(t *testing.T) {
	m := NewManager()
	m.AddRule(SLORule{Name: "rule1", Target: 99.9, Window: "30d", SLIType: SLIAvailability})
	overview := m.GetStatusOverview()
	if overview["total_rules"].(int) != 1 {
		t.Errorf("expected 1 total rule, got %d", overview["total_rules"])
	}
}

func TestEvaluateAll(t *testing.T) {
	m := NewManager()
	m.AddRule(SLORule{Name: "rule1", Target: 99.9, Window: "30d", SLIType: SLIAvailability})
	m.AddRule(SLORule{Name: "rule2", Target: 99.0, Window: "7d", SLIType: SLIErrorRate})
	results := m.EvaluateAll(995, 1000, 3)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}
