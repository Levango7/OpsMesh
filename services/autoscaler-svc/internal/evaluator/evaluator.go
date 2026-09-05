package evaluator

import (
	"fmt"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/models"
)

// MetricsReader defines the interface for reading metrics.
type MetricsReader interface {
	ReadMetric(deployment, namespace, metric string) (float64, error)
}

// K8sScaler defines the interface for adjusting replica counts.
type K8sScaler interface {
	GetReplicas(deployment, namespace string) (int32, error)
	SetReplicas(deployment, namespace string, replicas int32) error
}

// Evaluator evaluates scaling rules and produces decisions.
type Evaluator struct {
	mu        sync.RWMutex
	rules     map[string]*models.ScaleRule
	decisions []*models.ScaleDecision
	now       func() time.Time
}

// NewEvaluator creates a new Evaluator.
func NewEvaluator(now func() time.Time) *Evaluator {
	if now == nil {
		now = time.Now
	}
	return &Evaluator{
		rules:     make(map[string]*models.ScaleRule),
		decisions: make([]*models.ScaleDecision, 0),
		now:       now,
	}
}

// AddRule adds a new scaling rule.
func (e *Evaluator) AddRule(rule *models.ScaleRule) error {
	if rule == nil || rule.ID == "" {
		return fmt.Errorf("rule must have a valid ID")
	}
	if rule.MinReplicas < 0 {
		return fmt.Errorf("minReplicas cannot be negative")
	}
	if rule.MaxReplicas <= rule.MinReplicas {
		return fmt.Errorf("maxReplicas must be greater than minReplicas")
	}
	if rule.ScaleUpThreshold <= rule.ScaleDownThreshold {
		return fmt.Errorf("scaleUpThreshold must be greater than scaleDownThreshold")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
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
func (e *Evaluator) UpdateRule(rule *models.ScaleRule) error {
	if rule == nil || rule.ID == "" {
		return fmt.Errorf("rule must have a valid ID")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	old, exists := e.rules[rule.ID]
	if !exists {
		return fmt.Errorf("rule not found: %s", rule.ID)
	}
	cp := *rule
	cp.CreatedAt = old.CreatedAt
	cp.UpdatedAt = e.now()
	e.rules[rule.ID] = &cp
	return nil
}

// DeleteRule deletes a rule by ID.
func (e *Evaluator) DeleteRule(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.rules[id]; !exists {
		return fmt.Errorf("rule not found: %s", id)
	}
	delete(e.rules, id)
	return nil
}

// GetRule returns a rule by ID.
func (e *Evaluator) GetRule(id string) (*models.ScaleRule, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	r, exists := e.rules[id]
	if !exists {
		return nil, fmt.Errorf("rule not found: %s", id)
	}
	cp := *r
	return &cp, nil
}

// ListRules returns all rules.
func (e *Evaluator) ListRules() []*models.ScaleRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*models.ScaleRule, 0, len(e.rules))
	for _, r := range e.rules {
		cp := *r
		out = append(out, &cp)
	}
	return out
}

// Evaluate evaluates rules and returns scaling decisions.
func (e *Evaluator) Evaluate(reader MetricsReader, scaler K8sScaler, ruleID string) ([]*models.ScaleDecision, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.now()
	decisions := make([]*models.ScaleDecision, 0)

	rulesToEvaluate := make([]*models.ScaleRule, 0)
	for _, r := range e.rules {
		if !r.Enabled {
			continue
		}
		if ruleID != "" && r.ID != ruleID {
			continue
		}
		cp := *r
		rulesToEvaluate = append(rulesToEvaluate, &cp)
	}

	for _, rule := range rulesToEvaluate {
		decision := e.evaluateRule(rule, reader, scaler, now)
		if decision != nil {
			decisions = append(decisions, decision)
			e.decisions = append(e.decisions, decision)
		}
	}

	return decisions, nil
}

func (e *Evaluator) evaluateRule(rule *models.ScaleRule, reader MetricsReader, scaler K8sScaler, now time.Time) *models.ScaleDecision {
	ns := rule.Namespace
	if ns == "" {
		ns = "default"
	}

	currentReplicas, err := scaler.GetReplicas(rule.Deployment, ns)
	if err != nil {
		return &models.ScaleDecision{
			RuleID:       rule.ID,
			Deployment:   rule.Deployment,
			Namespace:    ns,
			Action:       "no_action",
			FromReplicas: 0,
			ToReplicas:   0,
			Reason:       fmt.Sprintf("failed to get replicas: %v", err),
			Timestamp:    now,
		}
	}

	metricValue, err := reader.ReadMetric(rule.Deployment, ns, rule.Metric)
	if err != nil {
		return &models.ScaleDecision{
			RuleID:       rule.ID,
			Deployment:   rule.Deployment,
			Namespace:    ns,
			Action:       "no_action",
			FromReplicas: currentReplicas,
			ToReplicas:   currentReplicas,
			Reason:       fmt.Sprintf("failed to read metric: %v", err),
			Timestamp:    now,
		}
	}

	cooldownUp := rule.CooldownUp
	if cooldownUp == 0 {
		cooldownUp = 60 * time.Second
	}
	cooldownDown := rule.CooldownDown
	if cooldownDown == 0 {
		cooldownDown = 300 * time.Second
	}

	if metricValue > rule.ScaleUpThreshold {
		if e.isInCooldown(rule.ID, "scale_up", cooldownUp, now) {
			return &models.ScaleDecision{
				RuleID:       rule.ID,
				Deployment:   rule.Deployment,
				Namespace:    ns,
				Action:       "no_action",
				FromReplicas: currentReplicas,
				ToReplicas:   currentReplicas,
				Reason:       "scale up in cooldown period",
				MetricValue:  metricValue,
				Timestamp:    now,
			}
		}
		newReplicas := currentReplicas + 1
		if newReplicas > rule.MaxReplicas {
			newReplicas = rule.MaxReplicas
		}
		if newReplicas != currentReplicas {
			if err := scaler.SetReplicas(rule.Deployment, ns, newReplicas); err != nil {
				return &models.ScaleDecision{
					RuleID:       rule.ID,
					Deployment:   rule.Deployment,
					Namespace:    ns,
					Action:       "no_action",
					FromReplicas: currentReplicas,
					ToReplicas:   currentReplicas,
					Reason:       fmt.Sprintf("failed to set replicas: %v", err),
					MetricValue:  metricValue,
					Timestamp:    now,
				}
			}
			return &models.ScaleDecision{
				RuleID:       rule.ID,
				Deployment:   rule.Deployment,
				Namespace:    ns,
				Action:       "scale_up",
				FromReplicas: currentReplicas,
				ToReplicas:   newReplicas,
				Reason:       fmt.Sprintf("metric %s=%.2f > threshold %.2f", rule.Metric, metricValue, rule.ScaleUpThreshold),
				MetricValue:  metricValue,
				Timestamp:    now,
			}
		}
		return &models.ScaleDecision{
			RuleID:       rule.ID,
			Deployment:   rule.Deployment,
			Namespace:    ns,
			Action:       "no_action",
			FromReplicas: currentReplicas,
			ToReplicas:   currentReplicas,
			Reason:       "already at max replicas",
			MetricValue:  metricValue,
			Timestamp:    now,
		}
	}

	if metricValue < rule.ScaleDownThreshold {
		if e.isInCooldown(rule.ID, "scale_down", cooldownDown, now) {
			return &models.ScaleDecision{
				RuleID:       rule.ID,
				Deployment:   rule.Deployment,
				Namespace:    ns,
				Action:       "no_action",
				FromReplicas: currentReplicas,
				ToReplicas:   currentReplicas,
				Reason:       "scale down in cooldown period",
				MetricValue:  metricValue,
				Timestamp:    now,
			}
		}
		newReplicas := currentReplicas - 1
		if newReplicas < rule.MinReplicas {
			newReplicas = rule.MinReplicas
		}
		if newReplicas != currentReplicas {
			if err := scaler.SetReplicas(rule.Deployment, ns, newReplicas); err != nil {
				return &models.ScaleDecision{
					RuleID:       rule.ID,
					Deployment:   rule.Deployment,
					Namespace:    ns,
					Action:       "no_action",
					FromReplicas: currentReplicas,
					ToReplicas:   currentReplicas,
					Reason:       fmt.Sprintf("failed to set replicas: %v", err),
					MetricValue:  metricValue,
					Timestamp:    now,
				}
			}
			return &models.ScaleDecision{
				RuleID:       rule.ID,
				Deployment:   rule.Deployment,
				Namespace:    ns,
				Action:       "scale_down",
				FromReplicas: currentReplicas,
				ToReplicas:   newReplicas,
				Reason:       fmt.Sprintf("metric %s=%.2f < threshold %.2f", rule.Metric, metricValue, rule.ScaleDownThreshold),
				MetricValue:  metricValue,
				Timestamp:    now,
			}
		}
		return &models.ScaleDecision{
			RuleID:       rule.ID,
			Deployment:   rule.Deployment,
			Namespace:    ns,
			Action:       "no_action",
			FromReplicas: currentReplicas,
			ToReplicas:   currentReplicas,
			Reason:       "already at min replicas",
			MetricValue:  metricValue,
			Timestamp:    now,
		}
	}

	return &models.ScaleDecision{
		RuleID:       rule.ID,
		Deployment:   rule.Deployment,
		Namespace:    ns,
		Action:       "no_action",
		FromReplicas: currentReplicas,
		ToReplicas:   currentReplicas,
		Reason:       fmt.Sprintf("metric %s=%.2f within thresholds [%.2f, %.2f]", rule.Metric, metricValue, rule.ScaleDownThreshold, rule.ScaleUpThreshold),
		MetricValue:  metricValue,
		Timestamp:    now,
	}
}

// isInCooldown checks if a scaling action is within the cooldown period.
func (e *Evaluator) isInCooldown(ruleID, action string, cooldown time.Duration, now time.Time) bool {
	cutoff := now.Add(-cooldown)
	for i := len(e.decisions) - 1; i >= 0; i-- {
		d := e.decisions[i]
		if d.RuleID == ruleID && d.Action == action && d.Timestamp.After(cutoff) {
			return true
		}
	}
	return false
}

// Decisions returns the decision history.
func (e *Evaluator) Decisions() []*models.ScaleDecision {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*models.ScaleDecision, len(e.decisions))
	copy(out, e.decisions)
	return out
}

// RecordDecision appends a decision to the history (for manual scaling etc. to record via service.Scale,
// front-end decisions/cooldowns queries can then see it; it does not participate in cooldown checks —
// isInCooldown only looks at scale_up/scale_down actions).
func (e *Evaluator) RecordDecision(d *models.ScaleDecision) {
	if d == nil {
		return
	}
	if d.Timestamp.IsZero() {
		d.Timestamp = e.now()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.decisions = append(e.decisions, d)
}

// CooldownStatus 是单条规则的冷却状态快照（只读查询用，前端契约字段
// ruleId/ruleName/remaining/expiresAt，单位秒）。
type CooldownStatus struct {
	RuleID    string `json:"ruleId"`
	RuleName  string `json:"ruleName"`
	Remaining int64  `json:"remaining"`
	ExpiresAt int64  `json:"expiresAt"`
}

// Cooldowns 计算每条规则当前的冷却剩余时间：取该规则最近一次
// scale_up / scale_down 决策，按其对应冷却窗口（缺省 up=60s、down=300s）
// 推导剩余秒数与到期时刻（Unix 秒）；无历史扩缩决策的规则返回 0 剩余。
func (e *Evaluator) Cooldowns() []CooldownStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	now := e.now()
	ruleByID := make(map[string]*models.ScaleRule, len(e.rules))
	for id, r := range e.rules {
		ruleByID[id] = r
	}

	// 每条规则只取最近一次 scale_up / scale_down 决策。
	last := make(map[string]map[string]*models.ScaleDecision, len(e.rules))
	for _, d := range e.decisions {
		if d.Action != "scale_up" && d.Action != "scale_down" {
			continue
		}
		if _, ok := last[d.RuleID]; !ok {
			last[d.RuleID] = map[string]*models.ScaleDecision{}
		}
		if prev, ok := last[d.RuleID][d.Action]; !ok || d.Timestamp.After(prev.Timestamp) {
			last[d.RuleID][d.Action] = d
		}
	}

	out := make([]CooldownStatus, 0, len(e.rules))
	for id, r := range ruleByID {
		remaining := int64(0)
		expiresAt := int64(0)
		for action, d := range last[id] {
			var window time.Duration
			if action == "scale_down" {
				window = r.CooldownDown
				if window == 0 {
					window = 300 * time.Second
				}
			} else {
				window = r.CooldownUp
				if window == 0 {
					window = 60 * time.Second
				}
			}
			expire := d.Timestamp.Add(window)
			if remain := expire.Sub(now).Seconds(); remain > 0 {
				secs := int64(remain)
				if secs > remaining {
					remaining = secs
					expiresAt = expire.Unix()
				}
			}
		}
		out = append(out, CooldownStatus{
			RuleID:    id,
			RuleName:  r.Name,
			Remaining: remaining,
			ExpiresAt: expiresAt,
		})
	}
	return out
}
