package engine

// evaluate_test.go 是 #60 的防复发门禁——**这个包此前零测试**，所以"根本没读指标"的
// 桩实现（`operator==">" && threshold<100` 即命中）能长期存在：结论与数据无关，
// 任何 `>` 阈值规则每次都触发，其它写法永远不触发，而没人能从测试上看出差别。
//
// 用例按"结论必须由读数决定"来组织，而不是按代码分支覆盖率：
// 每条都问同一个问题——改了实际值，结论会不会跟着变？

import (
	"strings"
	"testing"
	"time"
)

// clock 是可推进的假时钟（duration 与重复抑制必须靠推进时间才能验证）。
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }
func (c *clock) add(d time.Duration) {
	c.t = c.t.Add(d)
}

func newRule(id, tenant string, dur time.Duration, conds ...Condition) *AlertRule {
	return &AlertRule{
		ID:         id,
		Name:       "rule-" + id,
		TenantID:   tenant,
		Enabled:    true,
		Conditions: conds,
		Logic:      LogicAnd,
		Duration:   dur,
		Severity:   "critical",
	}
}

func TestEvaluate_HighCPU(t *testing.T) {
	clk := &clock{t: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
	e := NewEngine(clk.now)
	if err := e.AddRule(newRule("r1", "t1", 0,
		Condition{Metric: "cpu_usage", Operator: ">", Threshold: 80})); err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	// 95 > 80 ⇒ firing，且事件必须带现场值（落库与通知都靠它）。
	rep, err := e.Evaluate("t1", "dev-1", map[string]float64{"cpu_usage": 95})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rep.Events) != 1 {
		t.Fatalf("firing 事件 %d 条，期望 1：%+v", len(rep.Events), rep.Evaluations)
	}
	ev := rep.Events[0]
	if ev.Values["cpu_usage"] != 95 {
		t.Fatalf("事件没带现场读数：%+v", ev.Values)
	}
	if !strings.Contains(ev.Message, "cpu_usage=95") || !strings.Contains(ev.Message, "> 80") {
		t.Fatalf("Message 应含实际值与阈值：%q", ev.Message)
	}
	if ev.Metric != "cpu_usage" {
		t.Fatalf("Metric = %q, want cpu_usage（修前这里被填成 RuleID）", ev.Metric)
	}
	if rep.Evaluated() != 1 {
		t.Fatalf("Evaluated = %d, want 1", rep.Evaluated())
	}

	// 同一规则、同一设备，读数降到 50 ⇒ 必须不再触发（证明结论跟着读数走）。
	clk.add(time.Minute)
	rep2, err := e.Evaluate("t1", "dev-1", map[string]float64{"cpu_usage": 50})
	if err != nil {
		t.Fatalf("Evaluate(2): %v", err)
	}
	if len(rep2.Events) != 0 {
		t.Fatalf("50 不满足 > 80，却触发了：%+v", rep2.Events)
	}
	if got := rep2.Evaluations[0].State; got != StateNotMatched {
		t.Fatalf("state = %q, want %q", got, StateNotMatched)
	}
}

// TestEvaluate_Operators 覆盖全部受支持操作符 + 未知操作符。
// 未知操作符必须 invalid 而不是"静默不成立"——后者会把配错的规则卖成健康系统。
func TestEvaluate_Operators(t *testing.T) {
	cases := []struct {
		op        string
		value     float64
		threshold float64
		wantFire  bool
	}{
		{">", 90, 80, true},
		{">", 80, 80, false},
		{">=", 80, 80, true},
		{"<", 10, 80, true},
		{"<=", 80, 80, true},
		{"==", 42, 42, true},
		{"!=", 42, 43, true},
		{"gt", 90, 80, true},
		{"LE", 5, 80, true}, // 大小写与空格不应影响判定
	}
	for _, tc := range cases {
		clk := &clock{t: time.Unix(1700000000, 0)}
		e := NewEngine(clk.now)
		if err := e.AddRule(newRule("r", "t", 0,
			Condition{Metric: "m", Operator: tc.op, Threshold: tc.threshold})); err != nil {
			t.Fatalf("AddRule: %v", err)
		}
		rep, err := e.Evaluate("t", "d", map[string]float64{"m": tc.value})
		if err != nil {
			t.Fatalf("Evaluate %q: %v", tc.op, err)
		}
		fired := len(rep.Events) == 1
		if fired != tc.wantFire {
			t.Errorf("%g %s %g ⇒ fired=%v, 期望 %v（state=%s）",
				tc.value, tc.op, tc.threshold, fired, tc.wantFire, rep.Evaluations[0].State)
		}
	}

	e := NewEngine(nil)
	if err := e.AddRule(newRule("r", "t", 0,
		Condition{Metric: "m", Operator: "=>", Threshold: 1})); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	rep, _ := e.Evaluate("t", "d", map[string]float64{"m": 100})
	if rep.Evaluations[0].State != StateInvalid {
		t.Fatalf("未知操作符应判 invalid，实际 %q：%s", rep.Evaluations[0].State, rep.Evaluations[0].Reason)
	}
	if len(rep.IDs(StateInvalid)) != 1 {
		t.Fatal("invalid 规则必须回进响应（否则配错无人可见）")
	}
}

// TestEvaluate_NoDataIsNotNotMatched 是 #60 的核心口径：没读数 ≠ 没命中。
func TestEvaluate_NoDataIsNotNotMatched(t *testing.T) {
	e := NewEngine(nil)
	if err := e.AddRule(newRule("r1", "t1", 0,
		Condition{Metric: "cpu_usage", Operator: ">", Threshold: 80})); err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	// 空读数：既不该产告警，也绝不能报成"已评估且正常"。
	rep, err := e.Evaluate("t1", "dev-1", nil)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rep.Events) != 0 {
		t.Fatal("没有读数却触发了")
	}
	if rep.Evaluated() != 0 {
		t.Fatalf("无读数时 Evaluated 必须是 0，实际 %d", rep.Evaluated())
	}
	if got := rep.IDs(StateNoData); len(got) != 1 || got[0] != "r1" {
		t.Fatalf("应报 no_data，实际结论：%+v", rep.Evaluations)
	}
	if !strings.Contains(rep.Evaluations[0].Reason, "不等于") {
		t.Fatalf("no_data 的 reason 应说明口径：%s", rep.Evaluations[0].Reason)
	}

	// 读数里只有别的指标：同样 no_data（按 0 参与比较就是那个经典错误）。
	rep2, _ := e.Evaluate("t1", "dev-1", map[string]float64{"memory_usage": 0.9})
	if rep2.Evaluations[0].State != StateNoData {
		t.Fatalf("缺该指标时应 no_data，实际 %q", rep2.Evaluations[0].State)
	}
}

// TestEvaluate_TenantIsolation 钉住租户过滤：修前 Evaluate 完全不看租户，
// B 租户的规则会在 A 租户的设备上"触发"。
func TestEvaluate_TenantIsolation(t *testing.T) {
	e := NewEngine(nil)
	if err := e.AddRule(newRule("r-b", "tenant-b", 0,
		Condition{Metric: "cpu_usage", Operator: ">", Threshold: 1})); err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	rep, err := e.Evaluate("tenant-a", "dev-1", map[string]float64{"cpu_usage": 99})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rep.Evaluations) != 0 {
		t.Fatalf("跨租户规则被评估了：%+v", rep.Evaluations)
	}
	if len(rep.Events) != 0 {
		t.Fatal("跨租户规则触发了告警")
	}

	// 同租户必须正常评估（排除"过滤过头把一切都滤掉"的假绿）。
	rep2, _ := e.Evaluate("tenant-b", "dev-1", map[string]float64{"cpu_usage": 99})
	if len(rep2.Events) != 1 {
		t.Fatalf("本租户规则未触发：%+v", rep2.Evaluations)
	}
}

// TestEvaluate_Duration 验证 "for" 语义：命中必须持续够久才报。
func TestEvaluate_Duration(t *testing.T) {
	clk := &clock{t: time.Unix(1700000000, 0)}
	e := NewEngine(clk.now)
	if err := e.AddRule(newRule("r1", "t1", 60*time.Second,
		Condition{Metric: "cpu_usage", Operator: ">", Threshold: 80})); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	hot := map[string]float64{"cpu_usage": 95}

	// 第一次命中：进入 pending，不报。
	rep, _ := e.Evaluate("t1", "dev-1", hot)
	if len(rep.Events) != 0 || rep.Evaluations[0].State != StatePending {
		t.Fatalf("duration 未满就该 pending：%+v", rep.Evaluations[0])
	}

	// 30s 后仍命中：继续 pending，并带上已命中时长。
	clk.add(30 * time.Second)
	rep, _ = e.Evaluate("t1", "dev-1", hot)
	if rep.Evaluations[0].State != StatePending || rep.Evaluations[0].MatchedFor < 30*time.Second {
		t.Fatalf("30s 时 %+v，期望 pending 且 MatchedFor≈30s", rep.Evaluations[0])
	}

	// 中间断了读数：状态机必须复位（否则"断采 60s 后突然报"这种假事件就来了）。
	rep, _ = e.Evaluate("t1", "dev-1", nil)
	if rep.Evaluations[0].State != StateNoData {
		t.Fatalf("断采应落 no_data，实际 %q", rep.Evaluations[0].State)
	}

	// 重新命中：又从零开始计时长。
	clk.add(60 * time.Second)
	rep, _ = e.Evaluate("t1", "dev-1", hot)
	if rep.Evaluations[0].State != StatePending {
		t.Fatalf("断采后重新命中应重新计时，实际 %+v", rep.Evaluations[0])
	}

	// 持续命中满 60s ⇒ firing。
	clk.add(60 * time.Second)
	rep, _ = e.Evaluate("t1", "dev-1", hot)
	if rep.Evaluations[0].State != StateFiring || len(rep.Events) != 1 {
		t.Fatalf("持续命中满 duration 应 firing，实际 %+v", rep.Evaluations[0])
	}
}

// TestEvaluate_RepeatSuppression 验证同一轮故障只留一条告警，且超过重复间隔后会再报。
func TestEvaluate_RepeatSuppression(t *testing.T) {
	clk := &clock{t: time.Unix(1700000000, 0)}
	e := NewEngine(clk.now)
	if err := e.AddRule(newRule("r1", "t1", 0,
		Condition{Metric: "cpu_usage", Operator: ">", Threshold: 80})); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	hot := map[string]float64{"cpu_usage": 95}

	if rep, _ := e.Evaluate("t1", "dev-1", hot); len(rep.Events) != 1 {
		t.Fatal("首次命中应触发")
	}
	// 1 分钟后仍命中：抑制重复（风暴就是这么来的）。
	clk.add(time.Minute)
	if rep, _ := e.Evaluate("t1", "dev-1", hot); len(rep.Events) != 0 {
		t.Fatal("重复间隔内不该再报一条")
	} else if rep.Evaluations[0].State != StatePending {
		t.Fatalf("抑制期应报 pending，实际 %q", rep.Evaluations[0].State)
	}
	// 超过默认间隔：允许再报。
	clk.add(5 * time.Minute)
	if rep, _ := e.Evaluate("t1", "dev-1", hot); len(rep.Events) != 1 {
		t.Fatalf("超过重复间隔应再报一次：%+v", rep.Evaluations[0])
	}
}

// TestEvaluate_DeviceStateIndependent 保证 duration/抑制状态按设备分键：
// 共用一个键会让先命中的设备把另一台设备的告警吞掉。
func TestEvaluate_DeviceStateIndependent(t *testing.T) {
	clk := &clock{t: time.Unix(1700000000, 0)}
	e := NewEngine(clk.now)
	if err := e.AddRule(newRule("r1", "t1", 0,
		Condition{Metric: "cpu_usage", Operator: ">", Threshold: 80})); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	hot := map[string]float64{"cpu_usage": 95}

	if rep, _ := e.Evaluate("t1", "dev-1", hot); len(rep.Events) != 1 {
		t.Fatal("dev-1 首次应触发")
	}
	rep, _ := e.Evaluate("t1", "dev-2", hot)
	if len(rep.Events) != 1 {
		t.Fatalf("dev-2 是另一台设备，必须独立触发：%+v", rep.Evaluations[0])
	}
}

func TestEvaluate_LogicCombinations(t *testing.T) {
	c1 := Condition{Metric: "cpu", Operator: ">", Threshold: 80}
	c2 := Condition{Metric: "mem", Operator: ">", Threshold: 90}

	build := func(t *testing.T, logic LogicOp, dur time.Duration, conds ...Condition) *Engine {
		t.Helper()
		e := NewEngine(func() time.Time { return time.Unix(1700000000, 0) })
		r := newRule("r1", "t1", dur, conds...)
		r.Logic = logic
		if err := e.AddRule(r); err != nil {
			t.Fatalf("AddRule: %v", err)
		}
		return e
	}

	tests := []struct {
		name       string
		logic      LogicOp
		metrics    map[string]float64
		wantState  string
		wantEvents int
	}{
		{"AND 全成立", LogicAnd, map[string]float64{"cpu": 90, "mem": 95}, StateFiring, 1},
		{"AND 一条不成立⇒not_matched（即便另一条缺读数也不算 no_data）", LogicAnd, map[string]float64{"cpu": 10}, StateNotMatched, 0},
		{"AND 成立的+缺读数⇒no_data", LogicAnd, map[string]float64{"cpu": 90}, StateNoData, 0},
		{"OR 一条成立⇒firing", LogicOr, map[string]float64{"cpu": 90, "mem": 10}, StateFiring, 1},
		{"OR 全不成立⇒not_matched", LogicOr, map[string]float64{"cpu": 10, "mem": 10}, StateNotMatched, 0},
		{"OR 缺读数+不成立⇒no_data", LogicOr, map[string]float64{"cpu": 10}, StateNoData, 0},
		{"NOT 不成立⇒firing", LogicNot, map[string]float64{"cpu": 10}, StateFiring, 1},
		{"NOT 缺读数⇒no_data", LogicNot, map[string]float64{}, StateNoData, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := build(t, tc.logic, 0, c1, c2)
			rep, err := e.Evaluate("t1", "dev-1", tc.metrics)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if len(rep.Evaluations) != 1 {
				t.Fatalf("评估结论 %d 条", len(rep.Evaluations))
			}
			if rep.Evaluations[0].State != tc.wantState {
				t.Fatalf("state = %q, want %q（reason=%s）",
					rep.Evaluations[0].State, tc.wantState, rep.Evaluations[0].Reason)
			}
			if len(rep.Events) != tc.wantEvents {
				t.Fatalf("事件 %d 条, want %d", len(rep.Events), tc.wantEvents)
			}
		})
	}
}

// TestEvaluate_DisabledRuleSkipped 确认停用的规则完全不参与评估。
func TestEvaluate_DisabledRuleSkipped(t *testing.T) {
	e := NewEngine(nil)
	r := newRule("r1", "t1", 0, Condition{Metric: "cpu", Operator: ">", Threshold: 1})
	r.Enabled = false
	if err := e.AddRule(r); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	rep, _ := e.Evaluate("t1", "dev-1", map[string]float64{"cpu": 99})
	if len(rep.Evaluations) != 0 || len(rep.Events) != 0 {
		t.Fatalf("停用规则仍被评估：%+v", rep.Evaluations)
	}
}

// TestEvaluate_RuleWithoutConditions 无条件规则不能混进"已评估且正常"。
func TestEvaluate_RuleWithoutConditions(t *testing.T) {
	e := NewEngine(nil)
	if err := e.AddRule(newRule("r-empty", "t1", 0)); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	rep, _ := e.Evaluate("t1", "dev-1", map[string]float64{"cpu": 99})
	if rep.Evaluations[0].State != StateInvalid {
		t.Fatalf("无条件规则应判 invalid，实际 %q", rep.Evaluations[0].State)
	}
	if rep.Evaluated() != 0 {
		t.Fatalf("invalid 不该计入 Evaluated，实际 %d", rep.Evaluated())
	}
}
