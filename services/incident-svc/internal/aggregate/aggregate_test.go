package aggregate

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/incident-svc/internal/models"
)

func TestNewEngine(t *testing.T) {
	eng := NewEngine(0)
	if eng.window != 5*time.Minute {
		t.Errorf("expected default window 5m, got %v", eng.window)
	}

	eng2 := NewEngine(10 * time.Second)
	if eng2.window != 10*time.Second {
		t.Errorf("expected window 10s, got %v", eng2.window)
	}
}

func TestAddRule(t *testing.T) {
	eng := NewEngine(0)

	rule := &AggregationRule{
		ID:             "rule-1",
		Name:           "CPU alerts",
		MetricPatterns: []string{"cpu_usage"},
		Enabled:        true,
	}
	eng.AddRule(rule)

	rules := eng.Rules()
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}
}

func TestAddRuleNil(t *testing.T) {
	eng := NewEngine(0)
	eng.AddRule(nil)
	eng.AddRule(&AggregationRule{ID: ""})

	if len(eng.Rules()) != 0 {
		t.Error("expected no rules added for nil/empty ID")
	}
}

func TestRemoveRule(t *testing.T) {
	eng := NewEngine(0)
	eng.AddRule(&AggregationRule{ID: "rule-1", Enabled: true})

	eng.RemoveRule("rule-1")
	if len(eng.Rules()) != 0 {
		t.Error("expected rule to be removed")
	}
}

func TestAggregateMatched(t *testing.T) {
	eng := NewEngine(5 * time.Minute)
	eng.AddRule(&AggregationRule{
		ID:             "rule-1",
		DeviceIDs:      []string{"device-1"},
		MetricPatterns: []string{"cpu_usage"},
		Enabled:        true,
	})

	alert := &models.Alert{
		ID:        "alert-1",
		DeviceID:  "device-1",
		Metric:    "cpu_usage",
		Timestamp: time.Now(),
		Severity:  models.SeverityHigh,
	}

	result := eng.Aggregate(alert)
	if !result.Matched {
		t.Error("expected alert to match rule")
	}
	if result.RuleID != "rule-1" {
		t.Errorf("expected rule-1, got %s", result.RuleID)
	}
}

func TestAggregateNoMatch(t *testing.T) {
	eng := NewEngine(5 * time.Minute)
	eng.AddRule(&AggregationRule{
		ID:             "rule-1",
		DeviceIDs:      []string{"device-1"},
		MetricPatterns: []string{"cpu_usage"},
		Enabled:        true,
	})

	alert := &models.Alert{
		ID:        "alert-2",
		DeviceID:  "device-2",
		Metric:    "mem_usage",
		Timestamp: time.Now(),
	}

	result := eng.Aggregate(alert)
	if result.Matched {
		t.Error("expected alert not to match any rule")
	}
}

func TestAggregateExpiredAlert(t *testing.T) {
	eng := NewEngine(1 * time.Minute)
	eng.AddRule(&AggregationRule{
		ID:             "rule-1",
		MetricPatterns: []string{"cpu_usage"},
		Enabled:        true,
	})

	alert := &models.Alert{
		ID:        "alert-old",
		DeviceID:  "device-1",
		Metric:    "cpu_usage",
		Timestamp: time.Now().Add(-10 * time.Minute),
	}

	result := eng.Aggregate(alert)
	if result.Matched {
		t.Error("expected expired alert not to match")
	}
}

func TestAggregateDisabledRule(t *testing.T) {
	eng := NewEngine(5 * time.Minute)
	eng.AddRule(&AggregationRule{
		ID:             "rule-1",
		MetricPatterns: []string{"cpu_usage"},
		Enabled:        false,
	})

	alert := &models.Alert{
		ID:        "alert-1",
		DeviceID:  "device-1",
		Metric:    "cpu_usage",
		Timestamp: time.Now(),
	}

	result := eng.Aggregate(alert)
	if result.Matched {
		t.Error("expected disabled rule not to match")
	}
}

func TestAggregateNilAlert(t *testing.T) {
	eng := NewEngine(5 * time.Minute)
	result := eng.Aggregate(nil)
	if result.Matched {
		t.Error("expected nil alert not to match")
	}
}

func TestShouldEscalate(t *testing.T) {
	tests := []struct {
		name       string
		alertCount int
		severity   models.Severity
		expected   models.Severity
	}{
		{"critical-by-count", 10, models.SeverityMedium, models.SeverityCritical},
		{"critical-by-severity", 1, models.SeverityCritical, models.SeverityCritical},
		{"high-by-count", 5, models.SeverityLow, models.SeverityHigh},
		{"high-by-severity", 2, models.SeverityHigh, models.SeverityHigh},
		{"medium-by-count", 3, models.SeverityLow, models.SeverityMedium},
		{"low", 1, models.SeverityLow, models.SeverityLow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldEscalate(tt.alertCount, tt.severity)
			if got != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestAggregateNoRules(t *testing.T) {
	eng := NewEngine(5 * time.Minute)
	alert := &models.Alert{
		ID:        "alert-1",
		DeviceID:  "device-1",
		Metric:    "cpu_usage",
		Timestamp: time.Now(),
	}

	result := eng.Aggregate(alert)
	if result.Matched {
		t.Error("expected no match with no rules")
	}
}
