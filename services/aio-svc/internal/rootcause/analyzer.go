package rootcause

import (
	"sort"
	"strings"
	"time"
)

// Analyzer performs root cause analysis on alert events.
type Analyzer struct{}

// NewAnalyzer creates a new Analyzer.
func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

// Event represents a system event for correlation analysis.
type Event struct {
	Type        string
	Source      string
	Description string
	Timestamp   time.Time
	Metrics     map[string]float64
}

// RootCause represents a identified root cause.
type RootCause struct {
	Type        string
	Source      string
	Description string
	Confidence  float64
}

// AnalysisResult contains the root cause analysis output.
type AnalysisResult struct {
	AlertId    string
	Causes     []RootCause
	AnalyzedAt time.Time
}

// AnalyzeRootCause performs root cause analysis for an alert given its context events.
func (a *Analyzer) AnalyzeRootCause(alertId string, events []Event) AnalysisResult {
	result := AnalysisResult{
		AlertId:    alertId,
		Causes:     make([]RootCause, 0),
		AnalyzedAt: time.Now(),
	}

	if len(events) == 0 {
		return result
	}

	// Time-based correlation: find events that happened just before the alert
	timeCorrelated := a.timeCorrelation(events)
	for _, e := range timeCorrelated {
		result.Causes = append(result.Causes, RootCause{
			Type:        "time_correlation",
			Source:      e.Source,
			Description: e.Description,
			Confidence:  0.7,
		})
	}

	// Metric correlation: find metrics with abnormal changes
	metricCorrelated := a.metricCorrelation(events)
	for source, confidence := range metricCorrelated {
		result.Causes = append(result.Causes, RootCause{
			Type:        "metric_correlation",
			Source:      source,
			Description: "Metric anomaly detected from " + source,
			Confidence:  confidence,
		})
	}

	// Topological correlation: identify upstream/downstream patterns
	topoCorrelated := a.topologicalCorrelation(events)
	for _, e := range topoCorrelated {
		result.Causes = append(result.Causes, RootCause{
			Type:        "topological",
			Source:      e.Source,
			Description: "Topological dependency: " + e.Description,
			Confidence:  0.6,
		})
	}

	// Sort by confidence descending
	sort.Slice(result.Causes, func(i, j int) bool {
		return result.Causes[i].Confidence > result.Causes[j].Confidence
	})

	return result
}

// timeCorrelation finds events that occurred within a time window before the latest event.
func (a *Analyzer) timeCorrelation(events []Event) []Event {
	if len(events) < 2 {
		return nil
	}

	// Find the latest event time
	latest := events[0].Timestamp
	for _, e := range events {
		if e.Timestamp.After(latest) {
			latest = e.Timestamp
		}
	}

	// Events within 5 minutes before the latest event
	window := 5 * time.Minute
	correlated := make([]Event, 0)
	for _, e := range events {
		diff := latest.Sub(e.Timestamp)
		if diff > 0 && diff <= window {
			correlated = append(correlated, e)
		}
	}

	// Sort by timestamp descending (most recent first)
	sort.Slice(correlated, func(i, j int) bool {
		return correlated[i].Timestamp.After(correlated[j].Timestamp)
	})

	return correlated
}

// metricCorrelation identifies sources with abnormal metric changes.
func (a *Analyzer) metricCorrelation(events []Event) map[string]float64 {
	result := make(map[string]float64)

	for _, e := range events {
		for metric, value := range e.Metrics {
			// Simple heuristic: values above certain thresholds are suspicious
			if isAbnormalMetric(metric, value) {
				source := e.Source + "/" + metric
				result[source] = computeConfidence(value, metric)
			}
		}
	}

	return result
}

// topologicalCorrelation identifies upstream/downstream patterns.
func (a *Analyzer) topologicalCorrelation(events []Event) []Event {
	correlated := make([]Event, 0)
	for _, e := range events {
		// Look for events that mention network, dependency, or connection issues
		desc := strings.ToLower(e.Description)
		if strings.Contains(desc, "network") ||
			strings.Contains(desc, "connection") ||
			strings.Contains(desc, "dependency") ||
			strings.Contains(desc, "upstream") ||
			strings.Contains(desc, "downstream") {
			correlated = append(correlated, e)
		}
	}
	return correlated
}

// isAbnormalMetric checks if a metric value is abnormal.
func isAbnormalMetric(metric string, value float64) bool {
	metricLower := strings.ToLower(metric)
	switch {
	case strings.Contains(metricLower, "cpu"):
		return value > 90.0
	case strings.Contains(metricLower, "memory"):
		return value > 85.0
	case strings.Contains(metricLower, "disk"):
		return value > 90.0
	case strings.Contains(metricLower, "latency"):
		return value > 1000.0
	case strings.Contains(metricLower, "error"):
		return value > 5.0
	default:
		return value > 95.0
	}
}

// computeConfidence returns a confidence score based on how extreme the value is.
func computeConfidence(value float64, metric string) float64 {
	metricLower := strings.ToLower(metric)
	var threshold float64
	switch {
	case strings.Contains(metricLower, "cpu"):
		threshold = 90.0
	case strings.Contains(metricLower, "memory"):
		threshold = 85.0
	case strings.Contains(metricLower, "disk"):
		threshold = 90.0
	case strings.Contains(metricLower, "latency"):
		threshold = 1000.0
	case strings.Contains(metricLower, "error"):
		threshold = 5.0
	default:
		threshold = 95.0
	}

	if value <= threshold {
		return 0.0
	}

	ratio := value / threshold
	confidence := 0.5 + 0.1*(ratio-1.0)
	if confidence > 0.95 {
		confidence = 0.95
	}
	return confidence
}
