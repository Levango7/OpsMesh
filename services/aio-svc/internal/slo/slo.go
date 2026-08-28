package slo

import (
	"math"
	"time"
)

// SLIType represents the type of Service Level Indicator.
type SLIType string

const (
	SLIAvailability SLIType = "availability"
	SLILatency      SLIType = "latency"
	SLIErrorRate    SLIType = "error_rate"
)

// SLOStatus represents the health status of an SLO.
type SLOStatus string

const (
	StatusHealthy  SLOStatus = "healthy"
	StatusWarning  SLOStatus = "warning"
	StatusBreached SLOStatus = "breached"
)

// SLORule defines a Service Level Objective with its target and evaluation parameters.
type SLORule struct {
	Name       string  `json:"name"`
	Target     float64 `json:"target"`     // e.g., 99.9 for 99.9%
	Window     string  `json:"window"`     // e.g., "30d", "7d"
	SLIType    SLIType `json:"sli_type"`   // availability, latency, error_rate
	Threshold  float64 `json:"threshold"`  // latency threshold in ms, or error rate as fraction
}

// SLOResult contains the evaluation result of an SLO rule.
type SLOResult struct {
	RuleName         string    `json:"rule_name"`
	CurrentValue     float64   `json:"current_value"`
	Target           float64   `json:"target"`
	ErrorBudget      float64   `json:"error_budget_remaining"` // percentage points remaining
	BurnRate         float64   `json:"burn_rate"`              // multiple of budget consumed per window
	Status           SLOStatus `json:"status"`
	Window           string    `json:"window"`
	SLIType          SLIType   `json:"sli_type"`
	EvaluatedAt      time.Time `json:"evaluated_at"`
}

// Manager handles SLO rule evaluation and tracking.
type Manager struct {
	rules []SLORule
}

// NewManager creates a new SLO Manager.
func NewManager() *Manager {
	return &Manager{}
}

// AddRule registers an SLO rule.
func (m *Manager) AddRule(rule SLORule) {
	m.rules = append(m.rules, rule)
}

// ListSLORules returns all registered SLO rules.
func (m *Manager) ListSLORules() []SLORule {
	return m.rules
}

// ListSLOMetrics returns a summary metric for each registered rule.
func (m *Manager) ListSLOMetrics() []map[string]interface{} {
	metrics := make([]map[string]interface{}, 0, len(m.rules))
	for _, rule := range m.rules {
		metrics = append(metrics, map[string]interface{}{
			"name":      rule.Name,
			"target":    rule.Target,
			"window":    rule.Window,
			"sli_type":  rule.SLIType,
			"threshold": rule.Threshold,
		})
	}
	return metrics
}

// EvaluateSLO evaluates a single SLO rule against observed data.
// For availability: goodCount/totalCount * 100
// For latency: percentage of requests below threshold
// For errorRate: (1 - errors/total) * 100
func (m *Manager) EvaluateSLO(rule SLORule, goodCount, totalCount int, errorCount int) SLOResult {
	currentValue := computeCurrentValue(rule, goodCount, totalCount, errorCount)
	target := rule.Target
	errorBudget := target - 100.0 // negative value representing allowed downtime pct
	burnRate := calculateBurnRate(currentValue, target, rule.Window)
	status := determineStatus(currentValue, target, burnRate)

	return SLOResult{
		RuleName:     rule.Name,
		CurrentValue: roundTo(currentValue, 4),
		Target:       target,
		ErrorBudget:  roundTo((100.0-currentValue)+errorBudget, 4),
		BurnRate:     roundTo(burnRate, 2),
		Status:       status,
		Window:       rule.Window,
		SLIType:      rule.SLIType,
		EvaluatedAt:  time.Now().UTC(),
	}
}

// EvaluateAll evaluates all registered rules.
func (m *Manager) EvaluateAll(goodCount, totalCount, errorCount int) []SLOResult {
	results := make([]SLOResult, 0, len(m.rules))
	for _, rule := range m.rules {
		results = append(results, m.EvaluateSLO(rule, goodCount, totalCount, errorCount))
	}
	return results
}

// CalculateErrorBudget computes the remaining error budget percentage.
// Returns how many percentage points of error budget remain.
// Positive means budget remains, negative means budget exhausted.
func (m *Manager) CalculateErrorBudget(currentValue, target float64) float64 {
	return roundTo(target-currentValue, 4)
}

// CalculateBurnRate computes the burn rate: how fast the error budget is being consumed
// relative to the allowable rate over the window.
// Burn rate > 1 means budget will be exhausted before the window ends.
func (m *Manager) CalculateBurnRate(currentValue, target float64, window string) float64 {
	return calculateBurnRate(currentValue, target, window)
}

// GetStatusOverview returns a summary of all SLO statuses.
func (m *Manager) GetStatusOverview() map[string]interface{} {
	healthy := 0
	warning := 0
	breached := 0

	for _, result := range m.evaluateDefault() {
		switch result.Status {
		case StatusHealthy:
			healthy++
		case StatusWarning:
			warning++
		case StatusBreached:
			breached++
		}
	}

	return map[string]interface{}{
		"total_rules":    len(m.rules),
		"healthy":        healthy,
		"warning":        warning,
		"breached":       breached,
		"evaluated_at":   time.Now().UTC(),
	}
}

// GetBurnRateTrends returns burn rate trends for all rules.
func (m *Manager) GetBurnRateTrends() []map[string]interface{} {
	trends := make([]map[string]interface{}, 0, len(m.rules))
	for _, rule := range m.rules {
		result := m.EvaluateSLO(rule, 999, 1000, 1)
		trends = append(trends, map[string]interface{}{
			"rule_name":  rule.Name,
			"burn_rate":  result.BurnRate,
			"status":     result.Status,
			"window":     rule.Window,
			"sli_type":   rule.SLIType,
		})
	}
	return trends
}

// evaluateDefault provides a default evaluation for status overview.
func (m *Manager) evaluateDefault() []SLOResult {
	results := make([]SLOResult, 0, len(m.rules))
	for _, rule := range m.rules {
		results = append(results, m.EvaluateSLO(rule, 999, 1000, 1))
	}
	return results
}

// Helper functions

func computeCurrentValue(rule SLORule, goodCount, totalCount, errorCount int) float64 {
	switch rule.SLIType {
	case SLIAvailability:
		if totalCount == 0 {
			return 100.0
		}
		return float64(goodCount) / float64(totalCount) * 100.0
	case SLILatency:
		if totalCount == 0 {
			return 100.0
		}
		return float64(goodCount) / float64(totalCount) * 100.0
	case SLIErrorRate:
		if totalCount == 0 {
			return 100.0
		}
		return (1.0 - float64(errorCount)/float64(totalCount)) * 100.0
	default:
		return 100.0
	}
}

func calculateBurnRate(currentValue, target float64, window string) float64 {
	windowDays := parseWindowDays(window)
	if windowDays <= 0 {
		windowDays = 30.0
	}

	allowableError := 100.0 - target
	if allowableError <= 0 {
		return math.MaxFloat64
	}

	actualError := 100.0 - currentValue
	if actualError <= 0 {
		return 0.0
	}

	// Burn rate = (actual error rate) / (allowable error rate)
	return actualError / allowableError
}

func determineStatus(currentValue, target, burnRate float64) SLOStatus {
	if currentValue < target {
		return StatusBreached
	}
	if burnRate > 1.0 {
		return StatusWarning
	}
	if currentValue < target+(100.0-target)*0.5 {
		return StatusWarning
	}
	return StatusHealthy
}

func parseWindowDays(window string) float64 {
	if len(window) < 2 {
		return 30.0
	}
	num := 0.0
	for i := 0; i < len(window); i++ {
		c := window[i]
		if c >= '0' && c <= '9' {
			num = num*10 + float64(c-'0')
		} else {
			break
		}
	}
	if len(window) > 0 {
		unit := window[len(window)-1]
		switch unit {
		case 'd':
			return num
		case 'h':
			return num / 24.0
		case 'w':
			return num * 7.0
		case 'M':
			return num * 30.0
		}
	}
	return 30.0
}

func roundTo(val float64, precision int) float64 {
	p := math.Pow(10, float64(precision))
	return math.Round(val*p) / p
}
