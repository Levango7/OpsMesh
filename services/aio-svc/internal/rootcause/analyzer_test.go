package rootcause

import (
	"testing"
	"time"
)

func TestAnalyzeRootCause_EmptyEvents(t *testing.T) {
	a := NewAnalyzer()
	result := a.AnalyzeRootCause("alert-1", []Event{})
	if len(result.Causes) != 0 {
		t.Errorf("expected no causes for empty events, got %d", len(result.Causes))
	}
}

func TestAnalyzeRootCause_TimeCorrelation(t *testing.T) {
	a := NewAnalyzer()
	now := time.Now()
	events := []Event{
		{
			Type:        "metric",
			Source:      "cpu-monitor",
			Description: "CPU spike detected",
			Timestamp:   now.Add(-2 * time.Minute),
			Metrics:     map[string]float64{"cpu_usage": 95.0},
		},
		{
			Type:        "alert",
			Source:      "alert-engine",
			Description: "High CPU alert fired",
			Timestamp:   now,
		},
	}
	result := a.AnalyzeRootCause("alert-1", events)
	if len(result.Causes) == 0 {
		t.Error("expected at least one cause from time correlation")
	}
}

func TestAnalyzeRootCause_MetricCorrelation(t *testing.T) {
	a := NewAnalyzer()
	now := time.Now()
	events := []Event{
		{
			Type:        "metric",
			Source:      "mem-monitor",
			Description: "Memory usage critical",
			Timestamp:   now.Add(-1 * time.Minute),
			Metrics:     map[string]float64{"memory_usage": 92.0},
		},
	}
	result := a.AnalyzeRootCause("alert-2", events)
	found := false
	for _, c := range result.Causes {
		if c.Type == "metric_correlation" {
			found = true
			if c.Confidence <= 0.5 {
				t.Errorf("expected confidence > 0.5, got %f", c.Confidence)
			}
		}
	}
	if !found {
		t.Error("expected metric_correlation cause")
	}
}

func TestAnalyzeRootCause_TopologicalCorrelation(t *testing.T) {
	a := NewAnalyzer()
	now := time.Now()
	events := []Event{
		{
			Type:        "network",
			Source:      "net-monitor",
			Description: "Network connection lost to upstream database",
			Timestamp:   now.Add(-3 * time.Minute),
		},
	}
	result := a.AnalyzeRootCause("alert-3", events)
	found := false
	for _, c := range result.Causes {
		if c.Type == "topological" {
			found = true
		}
	}
	if !found {
		t.Error("expected topological cause for network event")
	}
}

func TestAnalyzeRootCause_SortedByConfidence(t *testing.T) {
	a := NewAnalyzer()
	now := time.Now()
	events := []Event{
		{
			Type:        "metric",
			Source:      "cpu-monitor",
			Description: "CPU spike",
			Timestamp:   now.Add(-1 * time.Minute),
			Metrics:     map[string]float64{"cpu_usage": 99.0},
		},
		{
			Type:        "network",
			Source:      "net-monitor",
			Description: "Network upstream dependency degraded",
			Timestamp:   now.Add(-2 * time.Minute),
		},
	}
	result := a.AnalyzeRootCause("alert-4", events)
	if len(result.Causes) >= 2 {
		for i := 1; i < len(result.Causes); i++ {
			if result.Causes[i].Confidence > result.Causes[i-1].Confidence {
				t.Error("causes should be sorted by confidence descending")
			}
		}
	}
}

func TestTimeCorrelation(t *testing.T) {
	a := NewAnalyzer()
	now := time.Now()
	events := []Event{
		{Timestamp: now.Add(-1 * time.Minute), Source: "recent"},
		{Timestamp: now.Add(-10 * time.Minute), Source: "old"},
		{Timestamp: now, Source: "latest"},
	}
	correlated := a.timeCorrelation(events)
	if len(correlated) != 1 {
		t.Errorf("expected 1 correlated event, got %d", len(correlated))
	}
	if len(correlated) > 0 && correlated[0].Source != "recent" {
		t.Errorf("expected 'recent' source, got %s", correlated[0].Source)
	}
}

func TestIsAbnormalMetric(t *testing.T) {
	tests := []struct {
		metric string
		value  float64
		want   bool
	}{
		{"cpu_usage", 95.0, true},
		{"cpu_usage", 50.0, false},
		{"memory_usage", 90.0, true},
		{"memory_usage", 70.0, false},
		{"disk_usage", 95.0, true},
		{"latency_ms", 1500.0, true},
		{"error_rate", 10.0, true},
	}
	for _, tt := range tests {
		got := isAbnormalMetric(tt.metric, tt.value)
		if got != tt.want {
			t.Errorf("isAbnormalMetric(%s, %f) = %v, want %v", tt.metric, tt.value, got, tt.want)
		}
	}
}

func TestComputeConfidence(t *testing.T) {
	conf := computeConfidence(99.0, "cpu_usage")
	if conf <= 0.5 || conf > 0.95 {
		t.Errorf("expected confidence in (0.5, 0.95], got %f", conf)
	}
	confLow := computeConfidence(50.0, "cpu_usage")
	if confLow != 0.0 {
		t.Errorf("expected 0 confidence for normal value, got %f", confLow)
	}
}
