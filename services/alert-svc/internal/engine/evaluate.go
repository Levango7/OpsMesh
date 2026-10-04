package engine

// evaluate.go 是规则评估的**真实**实现（#60）。
//
// 替换掉的桩实现原文（2026-10-04 之前）：
//
//	for _, c := range r.Conditions {
//	    if c.Operator == ">" && c.Threshold < 100 {
//	        matched = true
//	    }
//	}
//
// 它一个指标都没读。后果不是"少个功能"而是**结论与数据无关**：
//   - 任何 `operator:">"` 且阈值 <100 的规则，每次评估都触发（不看实际值）；
//   - 其它写法（`>=`/`<`/`==`，或阈值 ≥100）永远不触发；
//   - EvaluateRequest.metrics 一直被忽略，调用方给了也没用；
//   - 完全没有租户过滤，B 租户的规则会在 A 租户的设备上"触发"；
//   - 事件 Message 恒为 "rule triggered"、Values 恒空，落库与通知都拿不到现场值；
//   - 该包此前**零测试**，所以这个桩活了很久。
//
// 五态判定，关键在把"没数据"和"配错了"从"没命中"里分出来：
//   - firing       命中且已满足 duration（"for"）
//   - pending      命中但仍在 duration 窗口内，或已在告警状态的重复抑制期
//   - not_matched  有读数且条件不成立
//   - no_data      规则引用的指标本次没有读数——**这不等于"没命中"**；
//     把它算进"没命中"就是"监控断采 = 系统健康"这种假装
//   - invalid      规则本身配错（未知操作符、无条件），同样不能算"没命中"

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// 评估结论标识。字符串即对外口径（proto 字段注释与文档都按这些字面量写）。
const (
	StateFiring     = "firing"
	StatePending    = "pending"
	StateNotMatched = "not_matched"
	StateNoData     = "no_data"
	StateInvalid    = "invalid"
)

// defaultRepeatInterval 用于没有 duration 的规则：已触发后至少隔这么久才再报一次。
//
// 为什么必须有：否则每一轮评估都会新增一条告警并各发一次通知，告警风暴就是这么来的。
// 写成常量而不是藏在 if 分支里，是为了让"为什么又报了一次"有唯一答案。
const defaultRepeatInterval = 5 * time.Minute

// condResult 是单个条件的判定中间结果。
type condResult struct {
	cond    Condition
	value   float64
	hasData bool
	ok      bool
	errMsg  string
}

// Evaluation 是一条规则本轮的结论。
type Evaluation struct {
	RuleID     string
	State      string
	Reason     string
	Values     map[string]float64
	MatchedFor time.Duration
}

// EvaluationReport 是一次 Evaluate 调用的完整结果。
type EvaluationReport struct {
	Events      []*AlertEvent
	Evaluations []Evaluation
}

// Evaluated 返回真正拿到读数并给出结论的规则条数（不含 no_data / invalid）。
func (r *EvaluationReport) Evaluated() int {
	n := 0
	for _, e := range r.Evaluations {
		switch e.State {
		case StateFiring, StatePending, StateNotMatched:
			n++
		}
	}
	return n
}

// IDs 按结论筛规则 ID（供响应的 pending_rules / no_data_rules / invalid_rules 使用）。
func (r *EvaluationReport) IDs(state string) []string {
	out := make([]string, 0)
	for _, e := range r.Evaluations {
		if e.State == state {
			out = append(out, e.RuleID)
		}
	}
	sort.Strings(out)
	return out
}

// ruleKey 是 duration 与重复触发抑制的状态键。
//
// 必须含设备 ID：同一条规则在两台设备上各自有"已命中多久"，共用一个键会让先命中的
// 那台把另一台的告警吞掉。
func ruleKey(deviceID, ruleID string) string { return deviceID + "|" + ruleID }

// Evaluate 按 (租户, 设备, 读数) 评估全部启用规则。
//
// tenantID 为空 ⇒ 不做租户过滤（内部/测试用法）；非空 ⇒ 只评估该租户的规则。
// metrics 的键就是 Condition.Metric；缺键 ⇒ no_data，而不是按 0 参与比较：
// 把"没读数"当 0 会让 `temperature>100` 在断采时永远正常，
// 也会让 `disk_free_gb<10` 在断采时误报。
func (e *Engine) Evaluate(tenantID, deviceID string, metrics map[string]float64) (*EvaluationReport, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()

	ids := make([]string, 0, len(e.rules))
	for id := range e.rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	report := &EvaluationReport{}
	for _, id := range ids {
		r := e.rules[id]
		if !r.Enabled {
			continue
		}
		if tenantID != "" && r.TenantID != tenantID {
			continue
		}
		ev := e.evaluateRuleLocked(r, deviceID, metrics, now)
		report.Evaluations = append(report.Evaluations, ev)
		if ev.State == StateFiring {
			report.Events = append(report.Events, buildEvent(r, deviceID, ev, now))
		}
	}
	return report, nil
}

// evaluateRuleLocked 算出一条规则的结论并推进它的时间状态。
// 调用方必须持有写锁（本函数读写 pendingSince / lastFired）。
func (e *Engine) evaluateRuleLocked(r *AlertRule, deviceID string, metrics map[string]float64, now time.Time) Evaluation {
	key := ruleKey(deviceID, r.ID)
	ev := Evaluation{RuleID: r.ID, Values: make(map[string]float64, len(r.Conditions))}

	// 没有条件的规则给不出任何结论 ⇒ invalid（不是"没命中"）。
	if len(r.Conditions) == 0 {
		delete(e.pendingSince, key)
		ev.State, ev.Reason = StateInvalid, "rule has no conditions"
		return ev
	}

	// 1. 逐条件取读数并比较。
	results := make([]condResult, 0, len(r.Conditions))
	for _, c := range r.Conditions {
		cr := condResult{cond: c}
		if v, has := metrics[c.Metric]; has {
			cr.hasData = true
			cr.value = v
			ev.Values[c.Metric] = v
			ok, err := compareCondition(c.Operator, v, c.Threshold)
			if err != nil {
				cr.errMsg = err.Error()
			}
			cr.ok = ok
		}
		results = append(results, cr)
	}

	// 2. 配置错误优先：任何一条条件的操作符不认识，整条规则判 invalid。
	//    配错的规则不该继续产出"健康"的印象。
	for _, cr := range results {
		if cr.errMsg != "" {
			delete(e.pendingSince, key)
			ev.State = StateInvalid
			ev.Reason = fmt.Sprintf("condition on %q: %s", cr.cond.Metric, cr.errMsg)
			return ev
		}
	}

	matched, noData := combine(r.Logic, results)

	// 3. 只有在"既没成立也无法排除"时才落到 no_data。
	if !matched && noData {
		delete(e.pendingSince, key)
		ev.State = StateNoData
		ev.Reason = noDataReason(results)
		return ev
	}
	if !matched {
		delete(e.pendingSince, key)
		ev.State, ev.Reason = StateNotMatched, "conditions not satisfied"
		return ev
	}

	// 4. duration（"for"）：命中要持续够久才报。
	if r.Duration > 0 {
		since, seen := e.pendingSince[key]
		if !seen {
			e.pendingSince[key] = now
			ev.State = StatePending
			ev.Reason = fmt.Sprintf("matched, needs %s continuous before firing", r.Duration)
			return ev
		}
		ev.MatchedFor = now.Sub(since)
		if ev.MatchedFor < r.Duration {
			ev.State = StatePending
			ev.Reason = fmt.Sprintf("matched for %s, needs %s", ev.MatchedFor.Round(time.Second), r.Duration)
			return ev
		}
	}

	// 5. 重复抑制：仍在告警状态且未到重复间隔时不再报（同一轮故障只留一条告警）。
	if last, fired := e.lastFired[key]; fired && now.Sub(last) < repeatInterval(r) {
		ev.State = StatePending
		ev.Reason = fmt.Sprintf("still firing since %s（已在告警状态，不重复触发）", last.Format(time.RFC3339))
		return ev
	}

	e.lastFired[key] = now
	if _, seen := e.pendingSince[key]; !seen {
		e.pendingSince[key] = now
	}
	ev.State = StateFiring
	ev.Reason = summarize(results)
	return ev
}

// combine 按 Logic 汇总条件结果，返回（是否命中，是否存在缺读数）。
//
// 语义取舍写死在这里，避免散落到各处：
//   - AND：有任一条件明确不成立 ⇒ 不命中（哪怕别的条件缺读数，也不该报）；
//     全部成立且无缺读数 ⇒ 命中；成立 + 缺读数 ⇒ 不命中但标记 noData；
//   - OR：任一明确成立 ⇒ 命中；全部明确不成立 ⇒ 不命中；有缺读数且没任何成立 ⇒ noData；
//   - NOT：只看第一条，缺读数 ⇒ noData。
func combine(logic LogicOp, results []condResult) (matched, noData bool) {
	switch normalizeLogic(logic) {
	case LogicNot:
		cr := results[0]
		if !cr.hasData {
			return false, true
		}
		return !cr.ok, false
	case LogicOr:
		for _, cr := range results {
			if cr.hasData && cr.ok {
				return true, false
			}
			if !cr.hasData {
				noData = true
			}
		}
		return false, noData
	default: // LogicAnd
		allOK := true
		for _, cr := range results {
			if !cr.hasData {
				noData = true
				continue
			}
			if !cr.ok {
				allOK = false
			}
		}
		if !allOK {
			return false, false
		}
		return !noData, noData
	}
}

// repeatInterval 是"已触发后至少隔多久才再报一次"。
func repeatInterval(r *AlertRule) time.Duration {
	if r.Duration > 0 {
		return r.Duration
	}
	return defaultRepeatInterval
}

// buildEvent 用真实现场值构造告警事件。
func buildEvent(r *AlertRule, deviceID string, ev Evaluation, now time.Time) *AlertEvent {
	labels := map[string]string{
		"ruleID":   r.ID,
		"deviceID": deviceID,
		"severity": r.Severity,
		"tenantID": r.TenantID,
	}
	vals := make(map[string]float64, len(ev.Values))
	for k, v := range ev.Values {
		vals[k] = v
	}
	return &AlertEvent{
		RuleID:   r.ID,
		TenantID: r.TenantID,
		DeviceID: deviceID,
		Severity: r.Severity,
		Message:  fmt.Sprintf("%s: %s", r.Name, ev.Reason),
		Labels:   labels,
		FiredAt:  now,
		Values:   vals,
		Metric:   primaryMetric(r),
	}
}

// primaryMetric 取第一个条件的指标名（落库 Alert.Metric 用）。
func primaryMetric(r *AlertRule) string {
	if len(r.Conditions) == 0 {
		return ""
	}
	return r.Conditions[0].Metric
}

// compareCondition 做阈值比较；未知操作符必须报错，绝不能默认"不成立"。
func compareCondition(op string, actual, threshold float64) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case ">", "gt":
		return actual > threshold, nil
	case ">=", "ge":
		return actual >= threshold, nil
	case "<", "lt":
		return actual < threshold, nil
	case "<=", "le":
		return actual <= threshold, nil
	case "==", "=", "eq":
		return actual == threshold, nil
	case "!=", "ne":
		return actual != threshold, nil
	default:
		return false, fmt.Errorf("unsupported operator %q", op)
	}
}

func normalizeLogic(l LogicOp) LogicOp {
	switch strings.ToUpper(strings.TrimSpace(string(l))) {
	case string(LogicOr):
		return LogicOr
	case string(LogicNot):
		return LogicNot
	default:
		return LogicAnd
	}
}

func noDataReason(results []condResult) string {
	names := make([]string, 0, len(results))
	for _, cr := range results {
		if !cr.hasData {
			names = append(names, cr.cond.Metric)
		}
	}
	return fmt.Sprintf("no readings for: %s（没读数不等于没命中）", strings.Join(names, ", "))
}

// summarize 给出带现场值的触发摘要——落库、通知与排障都靠这句话。
func summarize(results []condResult) string {
	parts := make([]string, 0, len(results))
	for _, cr := range results {
		if !cr.hasData {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%g %s %g", cr.cond.Metric, cr.value, cr.cond.Operator, cr.cond.Threshold))
	}
	if len(parts) == 0 {
		return "conditions matched"
	}
	return strings.Join(parts, " and ")
}
