package alertengine

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ErrMetricUnavailable 指标提供者返回此错误时，对应条件视为不满足（false），
// 而非整条规则评估失败。用于"该设备暂无该指标数据"等正常缺失场景。
var ErrMetricUnavailable = errors.New("metric unavailable")

// MetricsProvider 指标提供者接口。
//
// Query 返回 (deviceID, metric) 在 window 窗口内的聚合值（通常为平均值）。
//   - window<=0 时返回即时值。
//   - 指标暂无数据应返回 ErrMetricUnavailable，引擎据此将该条件视为 false。
//   - 其他错误（如底层故障）将使 MatchRule 返回该错误，Evaluate 跳过该规则。
type MetricsProvider interface {
	Query(metric string, deviceID string, window time.Duration) (float64, error)
}

// Engine 规则引擎。
//
// 持有所有告警规则（按 ID 索引），提供 CRUD 与设备级评估。
// 线程安全：rules 经 mu 保护；评估时取快照后释放锁，再调用 MatchRule/ShouldFire
// （二者各自管理自己的锁），避免长时持锁与重入死锁。
type Engine struct {
	mu        sync.RWMutex
	rules     map[string]*AlertRule // 按 ID 索引
	metrics   MetricsProvider
	evaluator *Evaluator
	now       func() time.Time
}

// NewEngine 构造引擎。
//
//   - metrics：指标提供者，nil 时使用 NoopMetricsProvider（始终返回不可用）。
//   - evaluator：持续时长评估器，nil 时内部构造默认实例（60 样本、time.Now）。
//   - now：可注入时钟，nil 时使用 time.Now。
func NewEngine(metrics MetricsProvider, evaluator *Evaluator, now func() time.Time) *Engine {
	if metrics == nil {
		metrics = NoopMetricsProvider{}
	}
	if evaluator == nil {
		evaluator = NewEvaluator(60, now)
	}
	if now == nil {
		now = time.Now
	}
	return &Engine{
		rules:     make(map[string]*AlertRule),
		metrics:   metrics,
		evaluator: evaluator,
		now:       now,
	}
}

// NoopMetricsProvider 空实现，始终返回 ErrMetricUnavailable。
// 用于无指标源场景（如纯规则管理）。
type NoopMetricsProvider struct{}

func (NoopMetricsProvider) Query(string, string, time.Duration) (float64, error) {
	return 0, ErrMetricUnavailable
}

// Evaluator 返回引擎关联的评估器（供外部 RecordSample 等）。
func (e *Engine) Evaluator() *Evaluator { return e.evaluator }

// AddRule 新增规则。若 ID 已存在返回 ErrRuleInvalid。
//
// 调用前会 Validate（规范化 Logic/Severity）。CreatedAt/UpdatedAt 为零时填入当前时间。
func (e *Engine) AddRule(rule *AlertRule) error {
	if rule == nil {
		return ErrRuleInvalid
	}
	if err := rule.Validate(); err != nil {
		return err
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
	// 拷贝一份存入，隔离外部修改
	cp := *rule
	e.rules[rule.ID] = &cp
	return nil
}

// UpdateRule 更新规则。不存在返回 ErrRuleNotFound。
//
// 保留原 CreatedAt；UpdatedAt 刷新为当前时间。
func (e *Engine) UpdateRule(rule *AlertRule) error {
	if rule == nil {
		return ErrRuleInvalid
	}
	if err := rule.Validate(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	old, exists := e.rules[rule.ID]
	if !exists {
		return ErrRuleNotFound
	}
	cp := *rule
	cp.CreatedAt = old.CreatedAt // 保留原创建时间
	cp.UpdatedAt = e.now()
	e.rules[rule.ID] = &cp
	return nil
}

// DeleteRule 删除规则。不存在返回 ErrRuleNotFound。
//
// 同时清空评估器中该规则的持续满足记录，避免残留状态。
func (e *Engine) DeleteRule(id string) error {
	e.mu.Lock()
	if _, exists := e.rules[id]; !exists {
		e.mu.Unlock()
		return ErrRuleNotFound
	}
	delete(e.rules, id)
	e.mu.Unlock()
	if e.evaluator != nil {
		e.evaluator.ResetRule(id)
	}
	return nil
}

// GetRule 返回规则的拷贝（修改返回值不影响引擎内部状态）。
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

// ListRules 返回指定租户下所有规则的拷贝，按 ID 升序。
// tenantID 为空时返回所有租户的规则。
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

// MatchRule 评估单条规则对指定设备是否命中（不含持续时长判断）。
//
// 流程：
//  1. 对每个 Condition 调用 metrics.Query 拿到窗口聚合值 actual。
//  2. Query 返回 ErrMetricUnavailable → 该条件视为 false，继续评估其他条件。
//  3. Query 返回其他错误 → 立即返回 (false, err)。
//  4. 按 Operator 比较 actual 与 Threshold 得到条件布尔结果。
//  5. 按 Logic 组合所有条件结果返回 matched。
//
// 该方法不持 Engine 锁，可被外部独立调用（如手工评估某条规则）。
func (e *Engine) MatchRule(rule *AlertRule, deviceID string) (bool, error) {
	if rule == nil {
		return false, ErrRuleInvalid
	}
	results := make([]bool, len(rule.Conditions))
	for i, c := range rule.Conditions {
		actual, err := e.metrics.Query(c.Metric, deviceID, c.Window)
		if err != nil {
			if !errors.Is(err, ErrMetricUnavailable) {
				return false, fmt.Errorf("query metric %s: %w", c.Metric, err)
			}
			// 指标不可用 → 条件视为不满足
			results[i] = false
			continue
		}
		results[i] = compare(c.Operator, actual, c.Threshold)
	}
	return combine(rule.Logic, results), nil
}

// Evaluate 评估指定设备上所有启用规则，返回触发的告警事件列表。
//
// 流程：
//  1. 取启用规则快照（持 RLock 后释放）。
//  2. 逐条 MatchRule 得到 matched。
//  3. 调用 Evaluator.ShouldFire 判断持续时长是否满足。
//  4. 触发则构造 AlertEvent（含 Labels/Values/Message）。
//  5. 单条规则 MatchRule 出错时跳过该规则（不影响其他规则评估）。
//
// 事件按 RuleID 升序返回，便于调用方稳定处理。
func (e *Engine) Evaluate(deviceID string) ([]*AlertEvent, error) {
	snapshot := e.enabledSnapshot()
	now := e.now()
	events := make([]*AlertEvent, 0, len(snapshot))
	for _, r := range snapshot {
		matched, err := e.MatchRule(r, deviceID)
		if err != nil {
			// 单条规则评估失败跳过，不中断整体
			continue
		}
		if !e.evaluator.ShouldFire(deviceID, r.ID, matched, r.Duration) {
			continue
		}
		events = append(events, e.buildEvent(r, deviceID, now))
	}
	sort.Slice(events, func(i, j int) bool { return events[i].RuleID < events[j].RuleID })
	return events, nil
}

// enabledSnapshot 返回所有启用规则的拷贝快照（持 RLock 后释放）。
func (e *Engine) enabledSnapshot() []*AlertRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*AlertRule, 0, len(e.rules))
	for _, r := range e.rules {
		if !r.Enabled {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	return out
}

// buildEvent 构造触发的告警事件。
//
// Labels 默认包含 ruleID/deviceID/severity/tenantID，便于 Aggregator/Silencer 使用。
// Values 填入各 Condition.Metric 的实际值（查询失败的不放入）。
func (e *Engine) buildEvent(r *AlertRule, deviceID string, now time.Time) *AlertEvent {
	values := make(map[string]float64, len(r.Conditions))
	for _, c := range r.Conditions {
		if v, err := e.metrics.Query(c.Metric, deviceID, c.Window); err == nil {
			values[c.Metric] = v
		}
	}
	return &AlertEvent{
		RuleID:   r.ID,
		TenantID: r.TenantID,
		DeviceID: deviceID,
		Severity: r.Severity,
		Message:  fmt.Sprintf("rule %q triggered on device %s", r.Name, deviceID),
		Labels: map[string]string{
			"ruleID":   r.ID,
			"deviceID": deviceID,
			"severity": r.Severity,
			"tenantID": r.TenantID,
		},
		FiredAt: now,
		Values:  values,
	}
}
