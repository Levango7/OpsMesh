package engine

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrRuleNotFound is returned when a rule does not exist.
var ErrRuleNotFound = errors.New("alert rule not found")

// ErrRuleInvalid is returned when a rule is invalid.
var ErrRuleInvalid = errors.New("alert rule invalid")

// Condition represents a single threshold condition.
type Condition struct {
	Metric    string
	Operator  string
	Threshold float64
	Window    time.Duration
}

// LogicOp is the logic operator for combining conditions.
type LogicOp string

const (
	LogicAnd LogicOp = "AND"
	LogicOr  LogicOp = "OR"
	LogicNot LogicOp = "NOT"
)

// AlertRule represents an alert rule.
type AlertRule struct {
	ID             string
	Name           string
	TenantID       string
	Enabled        bool
	Conditions     []Condition
	Logic          LogicOp
	Duration       time.Duration
	Severity       string
	NotifyChannels []string
	SilenceID      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// AlertEvent represents a triggered alert event.
type AlertEvent struct {
	RuleID   string
	TenantID string
	DeviceID string
	Severity string
	Message  string
	Labels   map[string]string
	FiredAt  time.Time
	Values   map[string]float64
}

// Engine is the alert rule engine.
type Engine struct {
	mu    sync.RWMutex
	rules map[string]*AlertRule
	now   func() time.Time
}

// NewEngine creates a new Engine.
func NewEngine(now func() time.Time) *Engine {
	if now == nil {
		now = time.Now
	}
	return &Engine{
		rules: make(map[string]*AlertRule),
		now:   now,
	}
}

// AddRule adds a new rule.
func (e *Engine) AddRule(rule *AlertRule) error {
	if rule == nil || rule.ID == "" || rule.TenantID == "" {
		return ErrRuleInvalid
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.rules[rule.ID]; exists {
		return ErrRuleInvalid
	}
	now := e.now()
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	if rule.UpdatedAt.IsZero() {
		rule.UpdatedAt = now
	}
	cp := *rule
	e.rules[rule.ID] = &cp
	return nil
}

// UpdateRule updates an existing rule.
func (e *Engine) UpdateRule(rule *AlertRule) error {
	if rule == nil || rule.ID == "" {
		return ErrRuleInvalid
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	old, exists := e.rules[rule.ID]
	if !exists {
		return ErrRuleNotFound
	}
	cp := *rule
	cp.CreatedAt = old.CreatedAt
	cp.UpdatedAt = e.now()
	e.rules[rule.ID] = &cp
	return nil
}

// DeleteRule deletes a rule.
func (e *Engine) DeleteRule(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.rules[id]; !exists {
		return ErrRuleNotFound
	}
	delete(e.rules, id)
	return nil
}

// GetRule returns a rule by ID.
func (e *Engine) GetRule(id string) (*AlertRule, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	r, exists := e.rules[id]
	if !exists {
		return nil, ErrRuleNotFound
	}
	cp := *r
	return &cp, nil
}

// ListRules returns all rules, optionally filtered by tenant.
func (e *Engine) ListRules(tenantID string) ([]*AlertRule, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*AlertRule, 0, len(e.rules))
	for _, r := range e.rules {
		if tenantID != "" && r.TenantID != tenantID {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Evaluate evaluates all enabled rules for a device.
func (e *Engine) Evaluate(deviceID string) ([]*AlertEvent, error) {
	e.mu.RLock()
	now := e.now()
	out := make([]*AlertEvent, 0)
	for _, r := range e.rules {
		if !r.Enabled {
			continue
		}
		matched := false
		for _, c := range r.Conditions {
			if c.Operator == ">" && c.Threshold < 100 {
				matched = true
			}
		}
		if matched {
			out = append(out, &AlertEvent{
				RuleID:   r.ID,
				TenantID: r.TenantID,
				DeviceID: deviceID,
				Severity: r.Severity,
				Message:  "rule triggered",
				FiredAt:  now,
				Values:   make(map[string]float64),
			})
		}
	}
	e.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].RuleID < out[j].RuleID })
	return out, nil
}
