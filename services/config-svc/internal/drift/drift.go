package drift

import (
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/config-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/store"
)

// ComparisonType defines how the expected value is compared against the actual value.
type ComparisonType string

const (
	ComparisonExact ComparisonType = "exact"
	ComparisonRegex ComparisonType = "regex"
	ComparisonExists ComparisonType = "exists"
)

// DriftRule defines a rule for detecting configuration drift.
type DriftRule struct {
	ID            string         `json:"id"`
	ConfigKey     string         `json:"configKey"`
	ExpectedValue string         `json:"expectedValue"`
	Comparison    ComparisonType `json:"comparison"`
	TenantID      string         `json:"tenantId"`
	Description   string         `json:"description"`
	Enabled       bool           `json:"enabled"`
	CreatedAt     time.Time      `json:"createdAt"`
}

// DriftResult represents the outcome of a drift check for a single rule.
type DriftResult struct {
	RuleID    string    `json:"ruleId"`
	Key       string    `json:"key"`
	Expected  string    `json:"expected"`
	Actual    string    `json:"actual"`
	Drifted   bool      `json:"drifted"`
	Timestamp time.Time `json:"timestamp"`
}

// DriftStatus summarizes the current drift detection state.
type DriftStatus struct {
	TotalRules   int           `json:"totalRules"`
	EnabledRules int           `json:"enabledRules"`
	TotalDrifts  int           `json:"totalDrifts"`
	LastCheck    time.Time     `json:"lastCheck"`
	RecentDrifts []DriftResult `json:"recentDrifts"`
}

// Detector manages drift detection rules and executes checks.
type Detector struct {
	mu       sync.RWMutex
	rules    map[string]*DriftRule
	history  []DriftResult
	store    store.Store
	maxHist  int
}

// NewDetector creates a new DriftDetector.
func NewDetector(st store.Store) *Detector {
	return &Detector{
		rules:   make(map[string]*DriftRule),
		history: make([]DriftResult, 0),
		store:   st,
		maxHist: 1000,
	}
}

// RegisterRule adds a new drift detection rule.
func (d *Detector) RegisterRule(configKey, expectedValue string, comparison ComparisonType, tenantID, description string) *DriftRule {
	d.mu.Lock()
	defer d.mu.Unlock()

	rule := &DriftRule{
		ID:            uuid.New().String(),
		ConfigKey:     configKey,
		ExpectedValue: expectedValue,
		Comparison:    comparison,
		TenantID:      tenantID,
		Description:   description,
		Enabled:       true,
		CreatedAt:     time.Now(),
	}
	d.rules[rule.ID] = rule
	return rule
}

// UnregisterRule removes a drift detection rule by ID.
func (d *Detector) UnregisterRule(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.rules[id]; !ok {
		return false
	}
	delete(d.rules, id)
	return true
}

// ListRules returns all registered rules.
func (d *Detector) ListRules() []*DriftRule {
	d.mu.RLock()
	defer d.mu.RUnlock()
	rules := make([]*DriftRule, 0, len(d.rules))
	for _, r := range d.rules {
		rules = append(rules, r)
	}
	return rules
}

// GetRule returns a rule by ID.
func (d *Detector) GetRule(id string) (*DriftRule, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	r, ok := d.rules[id]
	return r, ok
}

// CheckDrift checks a single rule against the current config state.
func (d *Detector) CheckDrift(rule *DriftRule) (*DriftResult, error) {
	result := &DriftResult{
		RuleID:    rule.ID,
		Key:       rule.ConfigKey,
		Expected:  rule.ExpectedValue,
		Timestamp: time.Now(),
	}

	if rule.TenantID == "" {
		return nil, fmt.Errorf("rule must have a tenant ID")
	}

	entry, found := d.store.GetConfig(rule.TenantID, rule.ConfigKey)

	switch rule.Comparison {
	case ComparisonExists:
		if rule.ExpectedValue == "true" {
			result.Drifted = !found
			result.Actual = "exists"
			if !found {
				result.Actual = "missing"
			}
		} else {
			result.Drifted = found
			result.Actual = "exists"
			if !found {
				result.Actual = "missing"
			}
		}
	case ComparisonExact:
		if !found {
			result.Drifted = true
			result.Actual = ""
		} else {
			result.Actual = entry.Value
			result.Drifted = entry.Value != rule.ExpectedValue
		}
	case ComparisonRegex:
		if !found {
			result.Drifted = true
			result.Actual = ""
		} else {
			result.Actual = entry.Value
			re, err := regexp.Compile(rule.ExpectedValue)
			if err != nil {
				return nil, fmt.Errorf("invalid regex pattern: %w", err)
			}
			result.Drifted = !re.MatchString(entry.Value)
		}
	default:
		return nil, fmt.Errorf("unknown comparison type: %s", rule.Comparison)
	}

	d.appendHistory(*result)
	return result, nil
}

// ScanAll checks all enabled rules and returns results.
func (d *Detector) ScanAll() ([]DriftResult, error) {
	d.mu.RLock()
	rules := make([]*DriftRule, 0, len(d.rules))
	for _, r := range d.rules {
		if r.Enabled {
			rules = append(rules, r)
		}
	}
	d.mu.RUnlock()

	results := make([]DriftResult, 0, len(rules))
	for _, rule := range rules {
		result, err := d.CheckDrift(rule)
		if err != nil {
			return nil, fmt.Errorf("drift check failed for rule %s: %w", rule.ID, err)
		}
		results = append(results, *result)
	}
	return results, nil
}

// GetDriftHistory returns the drift check history.
func (d *Detector) GetDriftHistory() []DriftResult {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]DriftResult, len(d.history))
	copy(out, d.history)
	return out
}

// GetStatus returns a summary of drift detection status.
func (d *Detector) GetStatus() DriftStatus {
	d.mu.RLock()
	defer d.mu.RUnlock()

	status := DriftStatus{
		TotalRules: len(d.rules),
		LastCheck:  time.Now(),
	}
	for _, r := range d.rules {
		if r.Enabled {
			status.EnabledRules++
		}
	}

	recent := make([]DriftResult, 0)
	for i := len(d.history) - 1; i >= 0 && len(recent) < 10; i-- {
		recent = append(recent, d.history[i])
	}
	status.RecentDrifts = recent

	for _, h := range d.history {
		if h.Drifted {
			status.TotalDrifts++
		}
	}
	return status
}

func (d *Detector) appendHistory(result DriftResult) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.history = append(d.history, result)
	if len(d.history) > d.maxHist {
		d.history = d.history[len(d.history)-d.maxHist:]
	}
}

// SeedConfigIfMissing creates a config entry if it does not exist.
// Useful for testing and bootstrapping drift rules.
func SeedConfigIfMissing(st store.Store, tenantID, key, value string) {
	_, found := st.GetConfig(tenantID, key)
	if !found {
		st.SetConfig(&models.ConfigEntry{
			ID:       uuid.New().String(),
			TenantID: tenantID,
			Key:      key,
			Value:    value,
		})
	}
}
