package evaluator

import (
	"fmt"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/pkg/metrics"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/models"
)

// maxDecisionHistory 是进程内决策历史的容量上限。
//
// 缺陷背景：decisions 以前只增不减——Evaluate 每轮 append 且从不裁剪，长驻进程的
// 内存与 Decisions() 的拷贝出量都按"每轮每条规则 +1"单调增长到进程生命周期结束。
//
// 取 500 的依据：冷却窗口默认 up=60s、down=300s（见 evaluateRule），外部按约 30s
// 一轮驱动 /api/v1/evaluate，所以单条规则在一个 down 窗口里最多贡献约 10 条决策；
// 500 条相当于"50 条规则各占满一个 down 窗口"，对常规部署留有数倍余量，同时把历史
// 钉在千字节量级。
//
// 超限丢**最旧**而不是丢最新：isInCooldown 从尾部倒序扫、Cooldowns 取每规则最近一次、
// Decisions() 供前端读——三条路径都只关心最近的决策，反向裁剪会表现为"冷却静默失效"。
const maxDecisionHistory = 500

// decisionHistoryGauge 报历史的当前占用条数。当前值语义 → 配 SetBusinessMetric，
// 名字不带 _total：counter 家族里的"当前值"会让 rate()/increase() 失去意义
// （见 pkg/metrics 包注释记录的同名前科）。
const decisionHistoryGauge = "autoscaler_decision_history_entries"

// appendDecisionLocked 追加一条决策并把历史裁剪回上限（调用方须持 e.mu 写锁）。
//
// 裁剪用"整体左移 + 截断"而不是每次新建切片：稳态下 len 回到上限而 cap 保持 append
// 首次扩容出的容量，后续 append 不再分配新底层数组——否则每轮评估都多一次全量复制，
// 让 isInCooldown 的 O(n) 倒序扫描雪上加霜。
func (e *Evaluator) appendDecisionLocked(d *models.ScaleDecision) {
	e.decisions = append(e.decisions, d)
	if over := len(e.decisions) - maxDecisionHistory; over > 0 {
		copy(e.decisions, e.decisions[over:])
		e.decisions = e.decisions[:maxDecisionHistory]
	}
	metrics.SetBusinessMetric(decisionHistoryGauge, float64(len(e.decisions)), nil)
}

// MetricsReader defines the interface for reading metrics.
type MetricsReader interface {
	ReadMetric(deployment, namespace, metric string) (float64, error)
}

// K8sScaler defines the interface for adjusting replica counts.
//
// Executor() 是刻意带上的：一条 action=scale_up 的决策历史必须能说清"这次真的动了
// 集群，还是只写了模拟执行器的内存"。没有这个字段时，两条路径在历史里长得一模一样。
type K8sScaler interface {
	GetReplicas(deployment, namespace string) (int32, error)
	SetReplicas(deployment, namespace string, replicas int32) error
	Executor() string
}

// Evaluator evaluates scaling rules and produces decisions.
type Evaluator struct {
	mu    sync.RWMutex
	rules map[string]*models.ScaleRule
	// decisions 是有界历史（尾部为最新，超限丢最旧），所有写入必须经 appendDecisionLocked。
	decisions []*models.ScaleDecision
	now       func() time.Time
}

// NewEvaluator creates a new Evaluator.
func NewEvaluator(now func() time.Time) *Evaluator {
	if now == nil {
		now = time.Now
	}
	e := &Evaluator{
		rules:     make(map[string]*models.ScaleRule),
		decisions: make([]*models.ScaleDecision, 0),
		now:       now,
	}
	// 起量前先落一个 0：没有这条序列时，面板上"历史为空"和"该版本从没写入过"长得一模一样。
	metrics.SetBusinessMetric(decisionHistoryGauge, 0, nil)
	return e
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
			// 标注执行器身份：模拟执行器改的是进程内 map，真实执行器改的是集群。
			// 缺这一层标注时，GET /api/v1/decisions 里的 scale_up 无法区分两者。
			if decision.Executor == "" {
				decision.Executor = scaler.Executor()
			}
			decisions = append(decisions, decision)
			e.appendDecisionLocked(decision)
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

// Decisions returns the decision history（最近的至多 maxDecisionHistory 条，尾部最新）。
func (e *Evaluator) Decisions() []*models.ScaleDecision {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*models.ScaleDecision, len(e.decisions))
	copy(out, e.decisions)
	return out
}

// DecisionHistoryLen 返回决策历史的当前占用条数（巡检/容量告警用，上限见 maxDecisionHistory）。
func (e *Evaluator) DecisionHistoryLen() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.decisions)
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
	e.appendDecisionLocked(d)
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
