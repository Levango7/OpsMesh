package aggregate

import (
	"sort"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/incident-svc/internal/models"
)

// AggregationRule defines how alerts are grouped into incidents.
type AggregationRule struct {
	ID             string
	Name           string
	DeviceIDs      []string
	MetricPatterns []string
	Severity       models.Severity
	Window         time.Duration
	Enabled        bool
}

// Engine is the alert aggregation engine.
type Engine struct {
	mu     sync.RWMutex
	rules  map[string]*AggregationRule
	window time.Duration
	now    func() time.Time
}

// NewEngine creates a new aggregation Engine.
func NewEngine(window time.Duration) *Engine {
	if window <= 0 {
		window = 5 * time.Minute
	}
	return &Engine{
		rules:  make(map[string]*AggregationRule),
		window: window,
		now:    time.Now,
	}
}

// AddRule adds an aggregation rule.
func (e *Engine) AddRule(rule *AggregationRule) {
	if rule == nil || rule.ID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if rule.Window <= 0 {
		rule.Window = e.window
	}
	e.rules[rule.ID] = rule
}

// RemoveRule removes an aggregation rule.
func (e *Engine) RemoveRule(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.rules, id)
}

// Rules returns all aggregation rules.
func (e *Engine) Rules() []*AggregationRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*AggregationRule, 0, len(e.rules))
	for _, r := range e.rules {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// AggregateResult holds the result of alert aggregation.
type AggregateResult struct {
	IncidentID string
	Matched    bool
	RuleID     string
}

// Aggregate processes an alert and determines if it should be aggregated into an incident.
func (e *Engine) Aggregate(alert *models.Alert) *AggregateResult {
	if alert == nil {
		return &AggregateResult{Matched: false}
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	now := e.now()
	for _, rule := range e.rules {
		if !rule.Enabled {
			continue
		}
		if e.matchesRule(rule, alert, now) {
			return &AggregateResult{
				IncidentID: alert.DeviceID + "-" + rule.ID,
				Matched:    true,
				RuleID:     rule.ID,
			}
		}
	}
	return &AggregateResult{Matched: false}
}

func (e *Engine) matchesRule(rule *AggregationRule, alert *models.Alert, now time.Time) bool {
	if alert.Timestamp.After(now) || now.Sub(alert.Timestamp) > rule.Window {
		return false
	}

	deviceMatch := len(rule.DeviceIDs) == 0
	for _, d := range rule.DeviceIDs {
		if d == alert.DeviceID {
			deviceMatch = true
			break
		}
	}
	if !deviceMatch {
		return false
	}

	metricMatch := len(rule.MetricPatterns) == 0
	for _, p := range rule.MetricPatterns {
		if p == alert.Metric {
			metricMatch = true
			break
		}
	}
	if !metricMatch {
		return false
	}

	return true
}

// ShouldEscalate determines if an incident should be escalated based on alert count and severity.
func ShouldEscalate(alertCount int, maxSeverity models.Severity) models.Severity {
	if alertCount >= 10 || maxSeverity == models.SeverityCritical {
		return models.SeverityCritical
	}
	if alertCount >= 5 || maxSeverity == models.SeverityHigh {
		return models.SeverityHigh
	}
	if alertCount >= 3 || maxSeverity == models.SeverityMedium {
		return models.SeverityMedium
	}
	return models.SeverityLow
}
