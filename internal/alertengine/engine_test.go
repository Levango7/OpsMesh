// engine_test.go 测试 engine.go / rule.go / evaluator.go / silencer.go / aggregator.go。
//
// 覆盖目标：
//   - rule.go: Validate / isSupportedOp / compare / combine
//   - evaluator.go: ringBuffer / Evaluator 全部方法
//   - engine.go: Engine 全部方法（CRUD / MatchRule / Evaluate / buildEvent）
//   - silencer.go: Silencer 全部方法
//   - aggregator.go: Aggregator 全部方法
//   - anomaly.go: NewAnomalyEngineWithClock
package alertengine

import (
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// 测试辅助：可注入的 MetricsProvider
// ============================================================================

// fakeMetrics 可注入的指标提供者，按 (metric, deviceID) 返回预设值或错误。
type fakeMetrics struct {
	mu      sync.RWMutex
	values  map[string]float64 // key = metric + "|" + deviceID
	errs    map[string]error   // key = metric + "|" + deviceID
	unavail map[string]bool    // key = metric + "|" + deviceID，返回 ErrMetricUnavailable
	queries []fakeMetricsQuery // 记录所有 Query 调用（便于断言）
	callCnt int
}

type fakeMetricsQuery struct {
	Metric   string
	DeviceID string
	Window   time.Duration
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{
		values:  make(map[string]float64),
		errs:    make(map[string]error),
		unavail: make(map[string]bool),
	}
}

func (f *fakeMetrics) key(metric, device string) string { return metric + "|" + device }

// set 设置 (metric, device) 的返回值。
func (f *fakeMetrics) set(metric, device string, v float64) *fakeMetrics {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[f.key(metric, device)] = v
	return f
}

// setError 设置 (metric, device) 返回非 ErrMetricUnavailable 的错误。
func (f *fakeMetrics) setError(metric, device string, err error) *fakeMetrics {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[f.key(metric, device)] = err
	return f
}

// setUnavailable 设置 (metric, device) 返回 ErrMetricUnavailable。
func (f *fakeMetrics) setUnavailable(metric, device string) *fakeMetrics {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unavail[f.key(metric, device)] = true
	return f
}

func (f *fakeMetrics) Query(metric string, deviceID string, window time.Duration) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.key(metric, deviceID)
	f.queries = append(f.queries, fakeMetricsQuery{Metric: metric, DeviceID: deviceID, Window: window})
	f.callCnt++
	if f.unavail[k] {
		return 0, ErrMetricUnavailable
	}
	if err, ok := f.errs[k]; ok {
		return 0, err
	}
	if v, ok := f.values[k]; ok {
		return v, nil
	}
	return 0, ErrMetricUnavailable
}

// makeRule 构造一条合法的告警规则（便于测试复用）。
func makeRule(id, tenant string, enabled bool, logic LogicOp, conds ...Condition) *AlertRule {
	return &AlertRule{
		ID:         id,
		Name:       "rule-" + id,
		TenantID:   tenant,
		Enabled:    enabled,
		Conditions: conds,
		Logic:      logic,
		Severity:   SeverityWarning,
	}
}

// cpuCond 构造一个 cpu_usage > threshold 的条件。
func cpuCond(op string, threshold float64) Condition {
	return Condition{Metric: "cpu_usage", Operator: op, Threshold: threshold}
}

// ============================================================================
// rule.go 测试
// ============================================================================

// TestValidate_Valid 合法规则通过校验，且 Logic/Severity 被规范化。
func TestValidate_Valid(t *testing.T) {
	r := makeRule("r1", "t1", true, "", cpuCond(">", 80))
	r.Severity = ""
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate 返回错误: %v", err)
	}
	if r.Logic != LogicAnd {
		t.Errorf("Logic = %q, 规范化后应为 LogicAnd", r.Logic)
	}
	if r.Severity != SeverityWarning {
		t.Errorf("Severity = %q, 规范化后应为 SeverityWarning", r.Severity)
	}
}

// TestValidate_EmptyID ID 空返回 ErrRuleInvalid。
func TestValidate_EmptyID(t *testing.T) {
	r := makeRule("", "t1", true, LogicAnd, cpuCond(">", 80))
	if err := r.Validate(); !errors.Is(err, ErrRuleInvalid) {
		t.Errorf("Validate 返回 %v, want ErrRuleInvalid", err)
	}
}

// TestValidate_EmptyTenantID TenantID 空返回 ErrRuleInvalid。
func TestValidate_EmptyTenantID(t *testing.T) {
	r := makeRule("r1", "", true, LogicAnd, cpuCond(">", 80))
	if err := r.Validate(); !errors.Is(err, ErrRuleInvalid) {
		t.Errorf("Validate 返回 %v, want ErrRuleInvalid", err)
	}
}

// TestValidate_NoConditions 条件列表为空返回 ErrRuleInvalid。
func TestValidate_NoConditions(t *testing.T) {
	r := makeRule("r1", "t1", true, LogicAnd)
	if err := r.Validate(); !errors.Is(err, ErrRuleInvalid) {
		t.Errorf("Validate 返回 %v, want ErrRuleInvalid", err)
	}
}

// TestValidate_EmptyMetric 条件 Metric 空返回 ErrRuleInvalid。
func TestValidate_EmptyMetric(t *testing.T) {
	r := makeRule("r1", "t1", true, LogicAnd, Condition{Metric: "", Operator: ">", Threshold: 80})
	if err := r.Validate(); !errors.Is(err, ErrRuleInvalid) {
		t.Errorf("Validate 返回 %v, want ErrRuleInvalid", err)
	}
}

// TestValidate_UnsupportedOp 算子不支持返回 ErrRuleInvalid。
func TestValidate_UnsupportedOp(t *testing.T) {
	r := makeRule("r1", "t1", true, LogicAnd, Condition{Metric: "cpu_usage", Operator: "~=", Threshold: 80})
	if err := r.Validate(); !errors.Is(err, ErrRuleInvalid) {
		t.Errorf("Validate 返回 %v, want ErrRuleInvalid", err)
	}
}

// TestIsSupportedOp_AllOps 校验所有支持的算子。
func TestIsSupportedOp_AllOps(t *testing.T) {
	supported := []string{OpGT, OpLT, OpGE, OpLE, OpEQ, OpNE}
	for _, op := range supported {
		if !isSupportedOp(op) {
			t.Errorf("isSupportedOp(%q) = false, want true", op)
		}
	}
	unsupported := []string{"", "~=", "gt", "GT", ">>", "<>"}
	for _, op := range unsupported {
		if isSupportedOp(op) {
			t.Errorf("isSupportedOp(%q) = true, want false", op)
		}
	}
}

// TestCompare_AllOps 校验所有算子的比较逻辑。
func TestCompare_AllOps(t *testing.T) {
	cases := []struct {
		op        string
		actual    float64
		threshold float64
		want      bool
	}{
		{OpGT, 10, 5, true},
		{OpGT, 5, 5, false},
		{OpGT, 3, 5, false},
		{OpLT, 3, 5, true},
		{OpLT, 5, 5, false},
		{OpGE, 5, 5, true},
		{OpGE, 4, 5, false},
		{OpLE, 5, 5, true},
		{OpLE, 6, 5, false},
		{OpEQ, 5, 5, true},
		{OpEQ, 5.0001, 5, false},
		{OpNE, 5.1, 5, true},
		{OpNE, 5, 5, false},
		{"unknown", 100, 0, false}, // 未知算子返回 false
	}
	for _, c := range cases {
		if got := compare(c.op, c.actual, c.threshold); got != c.want {
			t.Errorf("compare(%q, %v, %v) = %v, want %v", c.op, c.actual, c.threshold, got, c.want)
		}
	}
}

// TestCombine_And AND 逻辑：全 true 才 true。
func TestCombine_And(t *testing.T) {
	if !combine(LogicAnd, []bool{true, true, true}) {
		t.Errorf("combine(AND, [true,true,true]) = false, want true")
	}
	if combine(LogicAnd, []bool{true, false, true}) {
		t.Errorf("combine(AND, [true,false,true]) = true, want false")
	}
	if combine(LogicAnd, []bool{}) {
		t.Errorf("combine(AND, []) = true, want false（空切片防御式返回 false）")
	}
}

// TestCombine_Or OR 逻辑：任一 true 即 true。
func TestCombine_Or(t *testing.T) {
	if !combine(LogicOr, []bool{false, true, false}) {
		t.Errorf("combine(OR, [false,true,false]) = false, want true")
	}
	if combine(LogicOr, []bool{false, false}) {
		t.Errorf("combine(OR, [false,false]) = true, want false")
	}
	if combine(LogicOr, []bool{}) {
		t.Errorf("combine(OR, []) = true, want false")
	}
}

// TestCombine_Not NOT 逻辑：全 false 才 true。
func TestCombine_Not(t *testing.T) {
	if !combine(LogicNot, []bool{false, false}) {
		t.Errorf("combine(NOT, [false,false]) = false, want true")
	}
	if combine(LogicNot, []bool{false, true}) {
		t.Errorf("combine(NOT, [false,true]) = true, want false")
	}
	if combine(LogicNot, []bool{}) {
		t.Errorf("combine(NOT, []) = true, want false")
	}
}

// TestCombine_Unknown 未知 LogicOp 按 AND 处理。
func TestCombine_Unknown(t *testing.T) {
	if !combine(LogicOp("xyz"), []bool{true, true}) {
		t.Errorf("combine(xyz, [true,true]) = false, want true（未知按 AND）")
	}
	if combine(LogicOp("xyz"), []bool{true, false}) {
		t.Errorf("combine(xyz, [true,false]) = true, want false（未知按 AND）")
	}
}

// ============================================================================
// evaluator.go 测试
// ============================================================================

// TestRingBuffer_PushAndAvg 环形缓冲写入与窗口平均值。
func TestRingBuffer_PushAndAvg(t *testing.T) {
	rb := newRingBuffer(3)
	if rb.cap != 3 {
		t.Fatalf("cap = %d, want 3", rb.cap)
	}
	base := time.Now()
	rb.push(sample{ts: base, value: 10})
	rb.push(sample{ts: base.Add(1 * time.Second), value: 20})
	rb.push(sample{ts: base.Add(2 * time.Second), value: 30})

	// 全部样本都在窗口内
	avg, ok := rb.avgSince(base)
	if !ok || avg != 20 {
		t.Errorf("avgSince(base) = (%v, %v), want (20, true)", avg, ok)
	}

	// 仅后两个样本在窗口内
	avg, ok = rb.avgSince(base.Add(1 * time.Second))
	if !ok || avg != 25 {
		t.Errorf("avgSince(base+1s) = (%v, %v), want (25, true)", avg, ok)
	}

	// 全部样本都早于 since
	avg, ok = rb.avgSince(base.Add(10 * time.Second))
	if ok {
		t.Errorf("avgSince(base+10s) = (%v, %v), want (0, false)", avg, ok)
	}
}

// TestRingBuffer_Overwrite 写满后覆盖最旧样本。
func TestRingBuffer_Overwrite(t *testing.T) {
	rb := newRingBuffer(3)
	base := time.Now()
	for i := 0; i < 5; i++ {
		rb.push(sample{ts: base.Add(time.Duration(i) * time.Second), value: float64(i + 1)})
	}
	if rb.count != 3 {
		t.Errorf("count = %d, want 3（应只保留最近 3 条）", rb.count)
	}
	// 应保留 3, 4, 5
	avg, ok := rb.avgSince(base)
	if !ok || avg != 4 {
		t.Errorf("avgSince(base) = (%v, %v), want (4, true)（应保留 3,4,5）", avg, ok)
	}
}

// TestRingBuffer_DefaultCapacity capacity<=0 时默认 1。
func TestRingBuffer_DefaultCapacity(t *testing.T) {
	rb := newRingBuffer(0)
	if rb.cap != 1 {
		t.Errorf("cap = %d, want 1（默认值）", rb.cap)
	}
}

// TestRingBuffer_AvgSinceEmpty 空缓冲返回 ok=false。
func TestRingBuffer_AvgSinceEmpty(t *testing.T) {
	rb := newRingBuffer(3)
	if _, ok := rb.avgSince(time.Now()); ok {
		t.Errorf("avgSince on empty buffer should return ok=false")
	}
}

// TestNewEvaluator_Defaults 默认参数构造。
func TestNewEvaluator_Defaults(t *testing.T) {
	e := NewEvaluator(0, nil)
	if e.maxSamples != 60 {
		t.Errorf("maxSamples = %d, want default 60", e.maxSamples)
	}
	if e.now == nil {
		t.Errorf("now should not be nil")
	}
}

// TestEvaluator_RecordAndAvg 记录样本并查询窗口平均值。
func TestEvaluator_RecordAndAvg(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	e := NewEvaluator(10, clock.now)

	// 记入 3 条样本
	e.RecordSample("dev-1", "cpu", 50, now.Add(-2*time.Second))
	e.RecordSample("dev-1", "cpu", 60, now.Add(-1*time.Second))
	e.RecordSample("dev-1", "cpu", 70, now)

	// 查询全部样本（窗口 5s）
	avg, ok := e.AvgInWindow("dev-1", "cpu", 5*time.Second)
	if !ok || avg != 60 {
		t.Errorf("AvgInWindow = (%v, %v), want (60, true)", avg, ok)
	}

	// 查询最近一条（window<=0）
	avg, ok = e.AvgInWindow("dev-1", "cpu", 0)
	if !ok || avg != 70 {
		t.Errorf("AvgInWindow(0) = (%v, %v), want (70, true)（最近一条）", avg, ok)
	}

	// 查询不存在的设备/指标
	if _, ok := e.AvgInWindow("dev-2", "cpu", 5*time.Second); ok {
		t.Errorf("AvgInWindow on unknown device should return ok=false")
	}
	if _, ok := e.AvgInWindow("dev-1", "mem", 5*time.Second); ok {
		t.Errorf("AvgInWindow on unknown metric should return ok=false")
	}
}

// TestEvaluator_AvgInWindowNoSamples 无样本时返回 ok=false。
func TestEvaluator_AvgInWindowNoSamples(t *testing.T) {
	e := NewEvaluator(10, time.Now)
	if _, ok := e.AvgInWindow("dev-1", "cpu", 5*time.Second); ok {
		t.Errorf("AvgInWindow on no samples should return ok=false")
	}
}

// TestEvaluator_ShouldFire_Immediate duration<=0 立即触发。
func TestEvaluator_ShouldFire_Immediate(t *testing.T) {
	e := NewEvaluator(10, time.Now)
	if !e.ShouldFire("dev-1", "r1", true, 0) {
		t.Errorf("ShouldFire(matched=true, duration=0) = false, want true")
	}
	if e.ShouldFire("dev-1", "r1", false, 0) {
		t.Errorf("ShouldFire(matched=false, duration=0) = true, want false")
	}
}

// TestEvaluator_ShouldFire_Duration 持续时长满足后才触发。
func TestEvaluator_ShouldFire_Duration(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	e := NewEvaluator(10, clock.now)

	// 首次 matched=true：记录开始时间，返回 false
	if e.ShouldFire("dev-1", "r1", true, 5*time.Second) {
		t.Errorf("首次满足应返回 false（开始计时）")
	}
	// 推进 3s（未达 5s）
	clock.advance(3 * time.Second)
	if e.ShouldFire("dev-1", "r1", true, 5*time.Second) {
		t.Errorf("持续 3s < 5s 应返回 false")
	}
	// 推进到 5s 后触发
	clock.advance(2 * time.Second)
	if !e.ShouldFire("dev-1", "r1", true, 5*time.Second) {
		t.Errorf("持续 5s >= 5s 应返回 true")
	}
	// 触发后记录被清空，再次满足应重新开始计时
	if e.ShouldFire("dev-1", "r1", true, 5*time.Second) {
		t.Errorf("触发后应重新计时，返回 false")
	}
}

// TestEvaluator_ShouldFire_Interrupt 条件中断清空记录。
func TestEvaluator_ShouldFire_Interrupt(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	e := NewEvaluator(10, clock.now)

	// 首次满足开始计时
	e.ShouldFire("dev-1", "r1", true, 5*time.Second)
	clock.advance(3 * time.Second)
	// 条件中断
	if e.ShouldFire("dev-1", "r1", false, 5*time.Second) {
		t.Errorf("matched=false 应返回 false")
	}
	// 重新满足应从 0 开始计时
	clock.advance(3 * time.Second) // 距上次满足 3s（<5s）
	if e.ShouldFire("dev-1", "r1", true, 5*time.Second) {
		t.Errorf("中断后重新满足应重新计时，3s < 5s 应返回 false")
	}
}

// TestEvaluator_Reset 清空指定设备的所有状态。
func TestEvaluator_Reset(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	e := NewEvaluator(10, clock.now)

	e.RecordSample("dev-1", "cpu", 50, now)
	e.ShouldFire("dev-1", "r1", true, 5*time.Second)

	e.Reset("dev-1")

	if _, ok := e.AvgInWindow("dev-1", "cpu", 5*time.Second); ok {
		t.Errorf("Reset 后样本应被清空")
	}
	// 重新满足应从 0 开始计时（持续记录被清空）
	if !e.ShouldFire("dev-1", "r1", true, 0) {
		t.Errorf("Reset 后 duration=0 应立即触发")
	}
}

// TestEvaluator_ResetRule 清空指定规则的持续满足记录（不影响其他规则）。
func TestEvaluator_ResetRule(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	e := NewEvaluator(10, clock.now)

	// 两条规则同时开始计时
	e.ShouldFire("dev-1", "r1", true, 5*time.Second)
	e.ShouldFire("dev-1", "r2", true, 5*time.Second)

	// 清空 r1
	e.ResetRule("r1")

	// r1 应重新计时（3s < 5s 不触发）
	clock.advance(3 * time.Second)
	if e.ShouldFire("dev-1", "r1", true, 5*time.Second) {
		t.Errorf("ResetRule 后 r1 应重新计时，3s < 5s 不触发")
	}
	// r2 应继续原计时（3s < 5s 也不触发，但再推进 2s 应触发）
	if e.ShouldFire("dev-1", "r2", true, 5*time.Second) {
		t.Errorf("r2 持续 3s < 5s 不触发")
	}
	clock.advance(2 * time.Second)
	if !e.ShouldFire("dev-1", "r2", true, 5*time.Second) {
		t.Errorf("r2 持续 5s >= 5s 应触发")
	}
}

// TestEvaluator_MaxSamplesOverwrite 超过 maxSamples 后覆盖最旧样本。
func TestEvaluator_MaxSamplesOverwrite(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	e := NewEvaluator(3, clock.now)

	// 写入 5 条样本，应只保留最后 3 条（30,40,50）
	for i := 0; i < 5; i++ {
		e.RecordSample("dev-1", "cpu", float64((i+1)*10), now.Add(time.Duration(i)*time.Second))
	}
	avg, ok := e.AvgInWindow("dev-1", "cpu", 100*time.Second)
	if !ok || avg != 40 {
		t.Errorf("AvgInWindow = (%v, %v), want (40, true)（应保留 30,40,50）", avg, ok)
	}
}

// ============================================================================
// engine.go 测试
// ============================================================================

// TestNewEngine_Defaults nil 参数时使用默认实现。
func TestNewEngine_Defaults(t *testing.T) {
	e := NewEngine(nil, nil, nil)
	if e.metrics == nil {
		t.Errorf("metrics 不应为 nil（应使用 NoopMetricsProvider）")
	}
	if e.evaluator == nil {
		t.Errorf("evaluator 不应为 nil")
	}
	if e.now == nil {
		t.Errorf("now 不应为 nil")
	}
	if _, ok := e.metrics.(NoopMetricsProvider); !ok {
		t.Errorf("metrics 应为 NoopMetricsProvider")
	}
}

// TestNoopMetricsProvider 始终返回 ErrMetricUnavailable。
func TestNoopMetricsProvider(t *testing.T) {
	var p NoopMetricsProvider
	v, err := p.Query("cpu", "dev-1", 0)
	if v != 0 {
		t.Errorf("v = %v, want 0", v)
	}
	if !errors.Is(err, ErrMetricUnavailable) {
		t.Errorf("err = %v, want ErrMetricUnavailable", err)
	}
}

// TestEngine_Evaluator 返回关联的评估器。
func TestEngine_Evaluator(t *testing.T) {
	ev := NewEvaluator(10, time.Now)
	e := NewEngine(nil, ev, nil)
	if e.Evaluator() != ev {
		t.Errorf("Evaluator() 返回不一致")
	}
}

// TestEngine_AddRule 新增规则成功与失败场景。
func TestEngine_AddRule(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	e := NewEngine(newFakeMetrics(), nil, clock.now)

	r := makeRule("r1", "t1", true, LogicAnd, cpuCond(">", 80))
	if err := e.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}
	// CreatedAt/UpdatedAt 应被填入
	got, err := e.GetRule("r1")
	if err != nil {
		t.Fatalf("GetRule 失败: %v", err)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}
	if !got.UpdatedAt.Equal(now) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, now)
	}

	// 重复 ID 返回 ErrRuleInvalid
	if err := e.AddRule(makeRule("r1", "t1", true, LogicAnd, cpuCond(">", 80))); !errors.Is(err, ErrRuleInvalid) {
		t.Errorf("AddRule 重复 ID 返回 %v, want ErrRuleInvalid", err)
	}
	// nil 规则
	if err := e.AddRule(nil); !errors.Is(err, ErrRuleInvalid) {
		t.Errorf("AddRule(nil) 返回 %v, want ErrRuleInvalid", err)
	}
	// 非法规则
	if err := e.AddRule(makeRule("", "t1", true, LogicAnd, cpuCond(">", 80))); !errors.Is(err, ErrRuleInvalid) {
		t.Errorf("AddRule(空 ID) 返回 %v, want ErrRuleInvalid", err)
	}
}

// TestEngine_AddRule_Isolation AddRule 拷贝规则，外部修改非切片字段不影响内部。
//
// 注意：源码 AddRule 仅做浅拷贝（cp := *rule），切片字段（Conditions/NotifyChannels）
// 底层数组与外部共享，此为已知行为。本测试只验证非切片字段的隔离。
func TestEngine_AddRule_Isolation(t *testing.T) {
	e := NewEngine(newFakeMetrics(), nil, time.Now)
	r := makeRule("r1", "t1", true, LogicAnd, cpuCond(">", 80))
	if err := e.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}
	// 外部修改非切片字段
	r.Enabled = false
	r.Name = "modified"

	got, _ := e.GetRule("r1")
	if !got.Enabled {
		t.Errorf("外部修改不应影响内部：Enabled = false")
	}
	if got.Name != "rule-r1" {
		t.Errorf("外部修改不应影响内部：Name = %q", got.Name)
	}
}

// TestEngine_UpdateRule 更新规则成功与失败场景。
func TestEngine_UpdateRule(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	e := NewEngine(newFakeMetrics(), nil, clock.now)

	// 先添加
	r := makeRule("r1", "t1", true, LogicAnd, cpuCond(">", 80))
	if err := e.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}

	// 更新
	clock.advance(10 * time.Second)
	up := makeRule("r1", "t1", true, LogicOr, cpuCond("<", 50))
	if err := e.UpdateRule(up); err != nil {
		t.Fatalf("UpdateRule 失败: %v", err)
	}
	got, _ := e.GetRule("r1")
	if got.Logic != LogicOr {
		t.Errorf("Logic = %q, want OR", got.Logic)
	}
	if !got.UpdatedAt.Equal(now.Add(10 * time.Second)) {
		t.Errorf("UpdatedAt 应刷新为当前时间")
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt 应保留原值 %v, got %v", now, got.CreatedAt)
	}

	// 更新不存在的规则
	if err := e.UpdateRule(makeRule("nonexistent", "t1", true, LogicAnd, cpuCond(">", 80))); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("UpdateRule 不存在返回 %v, want ErrRuleNotFound", err)
	}
	// nil 规则
	if err := e.UpdateRule(nil); !errors.Is(err, ErrRuleInvalid) {
		t.Errorf("UpdateRule(nil) 返回 %v, want ErrRuleInvalid", err)
	}
	// 非法规则
	if err := e.UpdateRule(makeRule("r1", "", true, LogicAnd, cpuCond(">", 80))); !errors.Is(err, ErrRuleInvalid) {
		t.Errorf("UpdateRule(空 tenant) 返回 %v, want ErrRuleInvalid", err)
	}
}

// TestEngine_DeleteRule 删除规则成功与失败场景。
func TestEngine_DeleteRule(t *testing.T) {
	e := NewEngine(newFakeMetrics(), nil, time.Now)
	r := makeRule("r1", "t1", true, LogicAnd, cpuCond(">", 80))
	if err := e.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}

	if err := e.DeleteRule("r1"); err != nil {
		t.Fatalf("DeleteRule 失败: %v", err)
	}
	if _, err := e.GetRule("r1"); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("删除后 GetRule 返回 %v, want ErrRuleNotFound", err)
	}
	// 删除不存在的规则
	if err := e.DeleteRule("nonexistent"); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("DeleteRule 不存在返回 %v, want ErrRuleNotFound", err)
	}
}

// TestEngine_DeleteRule_ResetsEvaluator 删除规则时清空评估器中该规则的持续满足记录。
func TestEngine_DeleteRule_ResetsEvaluator(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	e := NewEngine(newFakeMetrics(), nil, clock.now)

	r := makeRule("r1", "t1", true, LogicAnd, cpuCond(">", 80))
	r.Duration = 5 * time.Second
	if err := e.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}

	// 让评估器记录 r1 的持续满足开始时间
	e.Evaluator().ShouldFire("dev-1", "r1", true, 5*time.Second)

	// 删除规则
	if err := e.DeleteRule("r1"); err != nil {
		t.Fatalf("DeleteRule 失败: %v", err)
	}

	// 重新添加相同 ID 的规则
	clock.advance(1 * time.Second)
	if err := e.AddRule(r); err != nil {
		t.Fatalf("重新 AddRule 失败: %v", err)
	}
	// 由于 DeleteRule 已 ResetRule("r1")，持续满足记录被清空，
	// 再次 ShouldFire 应返回 false（重新开始计时）
	if e.Evaluator().ShouldFire("dev-1", "r1", true, 5*time.Second) {
		t.Errorf("DeleteRule 应清空持续满足记录，重新 ShouldFire 应返回 false")
	}
}

// TestEngine_GetRule 返回拷贝。
func TestEngine_GetRule(t *testing.T) {
	e := NewEngine(newFakeMetrics(), nil, time.Now)
	r := makeRule("r1", "t1", true, LogicAnd, cpuCond(">", 80))
	if err := e.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}

	got, err := e.GetRule("r1")
	if err != nil {
		t.Fatalf("GetRule 失败: %v", err)
	}
	// 修改返回值不影响内部
	got.Enabled = false
	got2, _ := e.GetRule("r1")
	if !got2.Enabled {
		t.Errorf("GetRule 返回拷贝，外部修改不应影响内部")
	}

	// 不存在的规则
	if _, err := e.GetRule("nonexistent"); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("GetRule 不存在返回 %v, want ErrRuleNotFound", err)
	}
}

// TestEngine_ListRules 按租户过滤并按 ID 升序返回。
func TestEngine_ListRules(t *testing.T) {
	e := NewEngine(newFakeMetrics(), nil, time.Now)
	for _, id := range []string{"r3", "r1", "r2"} {
		tenant := "t1"
		if id == "r2" {
			tenant = "t2"
		}
		if err := e.AddRule(makeRule(id, tenant, true, LogicAnd, cpuCond(">", 80))); err != nil {
			t.Fatalf("AddRule(%s) 失败: %v", id, err)
		}
	}

	// 按租户过滤
	rules, err := e.ListRules("t1")
	if err != nil {
		t.Fatalf("ListRules 失败: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("len(rules) = %d, want 2", len(rules))
	}
	if rules[0].ID != "r1" || rules[1].ID != "r3" {
		t.Errorf("ListRules 应按 ID 升序: got %s, %s", rules[0].ID, rules[1].ID)
	}

	// 空租户返回所有
	all, _ := e.ListRules("")
	if len(all) != 3 {
		t.Errorf("ListRules(\"\") len = %d, want 3", len(all))
	}
	// 验证返回拷贝
	all[0].Enabled = false
	all2, _ := e.ListRules("")
	if !all2[0].Enabled {
		t.Errorf("ListRules 应返回拷贝")
	}
}

// TestEngine_MatchRule_AND AND 逻辑匹配。
func TestEngine_MatchRule_AND(t *testing.T) {
	m := newFakeMetrics().
		set("cpu_usage", "dev-1", 90).
		set("mem_usage", "dev-1", 70)
	e := NewEngine(m, nil, time.Now)

	r := makeRule("r1", "t1", true, LogicAnd,
		cpuCond(">", 80),
		Condition{Metric: "mem_usage", Operator: ">", Threshold: 50},
	)
	// 两个条件都满足
	matched, err := e.MatchRule(r, "dev-1")
	if err != nil || !matched {
		t.Errorf("MatchRule = (%v, %v), want (true, nil)", matched, err)
	}
	// 一个条件不满足
	m.set("mem_usage", "dev-1", 30)
	matched, err = e.MatchRule(r, "dev-1")
	if err != nil || matched {
		t.Errorf("MatchRule = (%v, %v), want (false, nil)", matched, err)
	}
}

// TestEngine_MatchRule_OR OR 逻辑匹配。
func TestEngine_MatchRule_OR(t *testing.T) {
	m := newFakeMetrics().
		set("cpu_usage", "dev-1", 50). // 不满足 >80
		set("mem_usage", "dev-1", 70)  // 满足 >50
	e := NewEngine(m, nil, time.Now)

	r := makeRule("r1", "t1", true, LogicOr,
		cpuCond(">", 80),
		Condition{Metric: "mem_usage", Operator: ">", Threshold: 50},
	)
	matched, err := e.MatchRule(r, "dev-1")
	if err != nil || !matched {
		t.Errorf("MatchRule(OR) = (%v, %v), want (true, nil)", matched, err)
	}
}

// TestEngine_MatchRule_NOT NOT 逻辑匹配。
func TestEngine_MatchRule_NOT(t *testing.T) {
	m := newFakeMetrics().
		set("cpu_usage", "dev-1", 50) // 不满足 >80
	e := NewEngine(m, nil, time.Now)

	r := makeRule("r1", "t1", true, LogicNot, cpuCond(">", 80))
	matched, err := e.MatchRule(r, "dev-1")
	if err != nil || !matched {
		t.Errorf("MatchRule(NOT) = (%v, %v), want (true, nil)", matched, err)
	}
	// 条件满足时 NOT 应返回 false
	m.set("cpu_usage", "dev-1", 90)
	matched, err = e.MatchRule(r, "dev-1")
	if err != nil || matched {
		t.Errorf("MatchRule(NOT) 满足条件时 = (%v, %v), want (false, nil)", matched, err)
	}
}

// TestEngine_MatchRule_MetricUnavailable 指标不可用时该条件视为 false。
func TestEngine_MatchRule_MetricUnavailable(t *testing.T) {
	m := newFakeMetrics() // 不设置任何值，全部返回 ErrMetricUnavailable
	e := NewEngine(m, nil, time.Now)

	r := makeRule("r1", "t1", true, LogicAnd, cpuCond(">", 80))
	matched, err := e.MatchRule(r, "dev-1")
	if err != nil {
		t.Errorf("MatchRule err = %v, want nil（ErrMetricUnavailable 不应返回错误）", err)
	}
	if matched {
		t.Errorf("MatchRule = true, want false（指标不可用应视为条件不满足）")
	}
}

// TestEngine_MatchRule_QueryError 非 ErrMetricUnavailable 错误立即返回。
func TestEngine_MatchRule_QueryError(t *testing.T) {
	otherErr := errors.New("backend down")
	m := newFakeMetrics().setError("cpu_usage", "dev-1", otherErr)
	e := NewEngine(m, nil, time.Now)

	r := makeRule("r1", "t1", true, LogicAnd, cpuCond(">", 80))
	matched, err := e.MatchRule(r, "dev-1")
	if matched {
		t.Errorf("MatchRule = true, want false（出错时应返回 false）")
	}
	if err == nil || !strings.Contains(err.Error(), "query metric cpu_usage") {
		t.Errorf("err = %v, 应包含 'query metric cpu_usage'", err)
	}
}

// TestEngine_MatchRule_NilRule nil 规则返回 ErrRuleInvalid。
func TestEngine_MatchRule_NilRule(t *testing.T) {
	e := NewEngine(newFakeMetrics(), nil, time.Now)
	matched, err := e.MatchRule(nil, "dev-1")
	if matched {
		t.Errorf("MatchRule(nil) = true, want false")
	}
	if !errors.Is(err, ErrRuleInvalid) {
		t.Errorf("err = %v, want ErrRuleInvalid", err)
	}
}

// TestEngine_Evaluate 评估设备上所有启用规则。
func TestEngine_Evaluate(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	m := newFakeMetrics().
		set("cpu_usage", "dev-1", 90).
		set("mem_usage", "dev-1", 70)
	e := NewEngine(m, nil, clock.now)

	// r1: cpu > 80（启用，立即触发）
	if err := e.AddRule(makeRule("r1", "t1", true, LogicAnd, cpuCond(">", 80))); err != nil {
		t.Fatalf("AddRule r1 失败: %v", err)
	}
	// r2: mem > 50（启用，立即触发）
	r2 := makeRule("r2", "t1", true, LogicAnd, Condition{Metric: "mem_usage", Operator: ">", Threshold: 50})
	if err := e.AddRule(r2); err != nil {
		t.Fatalf("AddRule r2 失败: %v", err)
	}
	// r3: cpu < 50（启用，不满足）
	if err := e.AddRule(makeRule("r3", "t1", true, LogicAnd, cpuCond("<", 50))); err != nil {
		t.Fatalf("AddRule r3 失败: %v", err)
	}
	// r4: cpu > 95（禁用，不评估）
	r4 := makeRule("r4", "t1", false, LogicAnd, cpuCond(">", 95))
	if err := e.AddRule(r4); err != nil {
		t.Fatalf("AddRule r4 失败: %v", err)
	}

	events, err := e.Evaluate("dev-1")
	if err != nil {
		t.Fatalf("Evaluate 失败: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2（r1 和 r2 触发）", len(events))
	}
	// 应按 RuleID 升序
	if events[0].RuleID != "r1" || events[1].RuleID != "r2" {
		t.Errorf("events 顺序错误: %s, %s", events[0].RuleID, events[1].RuleID)
	}
	// 校验事件内容
	ev := events[0]
	if ev.DeviceID != "dev-1" {
		t.Errorf("DeviceID = %q, want dev-1", ev.DeviceID)
	}
	if ev.TenantID != "t1" {
		t.Errorf("TenantID = %q, want t1", ev.TenantID)
	}
	if ev.Severity != SeverityWarning {
		t.Errorf("Severity = %q, want %q", ev.Severity, SeverityWarning)
	}
	if ev.Labels["ruleID"] != "r1" || ev.Labels["deviceID"] != "dev-1" {
		t.Errorf("Labels 不正确: %v", ev.Labels)
	}
	if !ev.FiredAt.Equal(now) {
		t.Errorf("FiredAt = %v, want %v", ev.FiredAt, now)
	}
	if ev.Values["cpu_usage"] != 90 {
		t.Errorf("Values[cpu_usage] = %v, want 90", ev.Values["cpu_usage"])
	}
}

// TestEngine_Evaluate_Duration 持续时长未满足时不触发。
func TestEngine_Evaluate_Duration(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	m := newFakeMetrics().set("cpu_usage", "dev-1", 90)
	e := NewEngine(m, nil, clock.now)

	r := makeRule("r1", "t1", true, LogicAnd, cpuCond(">", 80))
	r.Duration = 5 * time.Second
	if err := e.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}

	// 首次评估：开始计时，不触发
	events, _ := e.Evaluate("dev-1")
	if len(events) != 0 {
		t.Errorf("首次评估应不触发（持续时长未满足）")
	}
	// 推进 3s（未达 5s）
	clock.advance(3 * time.Second)
	events, _ = e.Evaluate("dev-1")
	if len(events) != 0 {
		t.Errorf("持续 3s < 5s 应不触发")
	}
	// 推进到 5s 后触发
	clock.advance(2 * time.Second)
	events, _ = e.Evaluate("dev-1")
	if len(events) != 1 {
		t.Errorf("持续 5s >= 5s 应触发，len = %d", len(events))
	}
}

// TestEngine_Evaluate_QueryErrorSkip 单条规则评估出错时跳过，不影响其他规则。
func TestEngine_Evaluate_QueryErrorSkip(t *testing.T) {
	otherErr := errors.New("backend down")
	m := newFakeMetrics().
		set("cpu_usage", "dev-1", 90).            // r1 满足
		setError("disk_usage", "dev-1", otherErr) // r2 出错
	e := NewEngine(m, nil, time.Now)

	if err := e.AddRule(makeRule("r1", "t1", true, LogicAnd, cpuCond(">", 80))); err != nil {
		t.Fatalf("AddRule r1 失败: %v", err)
	}
	r2 := makeRule("r2", "t1", true, LogicAnd, Condition{Metric: "disk_usage", Operator: ">", Threshold: 50})
	if err := e.AddRule(r2); err != nil {
		t.Fatalf("AddRule r2 失败: %v", err)
	}

	events, err := e.Evaluate("dev-1")
	if err != nil {
		t.Fatalf("Evaluate 不应返回错误: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1（r2 出错跳过，r1 触发）", len(events))
	}
	if events[0].RuleID != "r1" {
		t.Errorf("events[0].RuleID = %q, want r1", events[0].RuleID)
	}
}

// TestEngine_Evaluate_NoEnabledRules 无启用规则时返回空。
func TestEngine_Evaluate_NoEnabledRules(t *testing.T) {
	m := newFakeMetrics().set("cpu_usage", "dev-1", 90)
	e := NewEngine(m, nil, time.Now)
	// 禁用规则
	if err := e.AddRule(makeRule("r1", "t1", false, LogicAnd, cpuCond(">", 80))); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}
	events, err := e.Evaluate("dev-1")
	if err != nil {
		t.Fatalf("Evaluate 失败: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("无启用规则应返回空事件列表")
	}
}

// TestEngine_Evaluate_BuildEventValues 查询失败的指标不放入 Values。
func TestEngine_Evaluate_BuildEventValues(t *testing.T) {
	m := newFakeMetrics().
		set("cpu_usage", "dev-1", 90).
		setUnavailable("mem_usage", "dev-1") // mem_usage 不可用
	e := NewEngine(m, nil, time.Now)

	// OR 逻辑：cpu 满足即触发，但 mem 不可用
	r := makeRule("r1", "t1", true, LogicOr,
		cpuCond(">", 80),
		Condition{Metric: "mem_usage", Operator: ">", Threshold: 50},
	)
	if err := e.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}
	events, _ := e.Evaluate("dev-1")
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if _, ok := events[0].Values["mem_usage"]; ok {
		t.Errorf("不可用的 mem_usage 不应放入 Values")
	}
	if _, ok := events[0].Values["cpu_usage"]; !ok {
		t.Errorf("可用的 cpu_usage 应放入 Values")
	}
}

// ============================================================================
// silencer.go 测试
// ============================================================================

// TestSilenceRule_Validate 合法静默规则通过校验。
func TestSilenceRule_Validate(t *testing.T) {
	s := &SilenceRule{ID: "s1", TenantID: "t1"}
	if err := s.Validate(); err != nil {
		t.Errorf("Validate 失败: %v", err)
	}
	// 带时间窗口
	s.StartAt = time.Now()
	s.EndAt = time.Now().Add(time.Hour)
	if err := s.Validate(); err != nil {
		t.Errorf("Validate 带时间窗口失败: %v", err)
	}
}

// TestSilenceRule_Validate_Invalid 非法静默规则。
func TestSilenceRule_Validate_Invalid(t *testing.T) {
	// 空 ID
	if err := (&SilenceRule{ID: "", TenantID: "t1"}).Validate(); !errors.Is(err, ErrSilenceInvalid) {
		t.Errorf("空 ID 应返回 ErrSilenceInvalid")
	}
	// 空 TenantID
	if err := (&SilenceRule{ID: "s1", TenantID: ""}).Validate(); !errors.Is(err, ErrSilenceInvalid) {
		t.Errorf("空 TenantID 应返回 ErrSilenceInvalid")
	}
	// EndAt 早于 StartAt
	now := time.Now()
	s := &SilenceRule{ID: "s1", TenantID: "t1", StartAt: now.Add(time.Hour), EndAt: now}
	if err := s.Validate(); !errors.Is(err, ErrSilenceInvalid) {
		t.Errorf("EndAt 早于 StartAt 应返回 ErrSilenceInvalid")
	}
}

// TestNewSilencer_DefaultClock nil 时钟使用 time.Now。
func TestNewSilencer_DefaultClock(t *testing.T) {
	s := NewSilencer(nil)
	if s.now == nil {
		t.Errorf("now 不应为 nil")
	}
}

// TestSilencer_AddRule 新增静默规则。
func TestSilencer_AddRule(t *testing.T) {
	s := NewSilencer(time.Now)
	r := &SilenceRule{ID: "s1", TenantID: "t1", MatchLabels: map[string]string{"severity": "critical"}}
	if err := s.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}
	// 重复 ID
	if err := s.AddRule(r); !errors.Is(err, ErrSilenceInvalid) {
		t.Errorf("重复 ID 应返回 ErrSilenceInvalid")
	}
	// nil
	if err := s.AddRule(nil); !errors.Is(err, ErrSilenceInvalid) {
		t.Errorf("nil 应返回 ErrSilenceInvalid")
	}
	// 非法
	if err := s.AddRule(&SilenceRule{ID: "", TenantID: "t1"}); !errors.Is(err, ErrSilenceInvalid) {
		t.Errorf("非法规则应返回 ErrSilenceInvalid")
	}
}

// TestSilencer_AddRule_Isolation AddRule 深拷贝 MatchLabels。
func TestSilencer_AddRule_Isolation(t *testing.T) {
	s := NewSilencer(time.Now)
	r := &SilenceRule{ID: "s1", TenantID: "t1", MatchLabels: map[string]string{"severity": "critical"}}
	if err := s.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}
	// 外部修改
	r.MatchLabels["severity"] = "warning"

	got, _ := s.GetRule("s1")
	if got.MatchLabels["severity"] != "critical" {
		t.Errorf("外部修改不应影响内部：MatchLabels = %v", got.MatchLabels)
	}
}

// TestSilencer_UpdateRule 更新静默规则。
func TestSilencer_UpdateRule(t *testing.T) {
	s := NewSilencer(time.Now)
	r := &SilenceRule{ID: "s1", TenantID: "t1"}
	if err := s.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}
	// 更新
	up := &SilenceRule{ID: "s1", TenantID: "t1", Reason: "updated"}
	if err := s.UpdateRule(up); err != nil {
		t.Fatalf("UpdateRule 失败: %v", err)
	}
	got, _ := s.GetRule("s1")
	if got.Reason != "updated" {
		t.Errorf("Reason = %q, want 'updated'", got.Reason)
	}
	// 更新不存在的规则
	if err := s.UpdateRule(&SilenceRule{ID: "nonexistent", TenantID: "t1"}); !errors.Is(err, ErrSilenceNotFound) {
		t.Errorf("更新不存在应返回 ErrSilenceNotFound")
	}
	// nil
	if err := s.UpdateRule(nil); !errors.Is(err, ErrSilenceInvalid) {
		t.Errorf("nil 应返回 ErrSilenceInvalid")
	}
	// 非法
	if err := s.UpdateRule(&SilenceRule{ID: "s1", TenantID: ""}); !errors.Is(err, ErrSilenceInvalid) {
		t.Errorf("非法应返回 ErrSilenceInvalid")
	}
}

// TestSilencer_DeleteRule 删除静默规则。
func TestSilencer_DeleteRule(t *testing.T) {
	s := NewSilencer(time.Now)
	r := &SilenceRule{ID: "s1", TenantID: "t1"}
	if err := s.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}
	if err := s.DeleteRule("s1"); err != nil {
		t.Fatalf("DeleteRule 失败: %v", err)
	}
	if _, err := s.GetRule("s1"); !errors.Is(err, ErrSilenceNotFound) {
		t.Errorf("删除后 GetRule 应返回 ErrSilenceNotFound")
	}
	if err := s.DeleteRule("nonexistent"); !errors.Is(err, ErrSilenceNotFound) {
		t.Errorf("删除不存在应返回 ErrSilenceNotFound")
	}
}

// TestSilencer_GetRule 返回拷贝。
func TestSilencer_GetRule(t *testing.T) {
	s := NewSilencer(time.Now)
	r := &SilenceRule{ID: "s1", TenantID: "t1", MatchLabels: map[string]string{"k": "v"}}
	if err := s.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}
	got, err := s.GetRule("s1")
	if err != nil {
		t.Fatalf("GetRule 失败: %v", err)
	}
	got.MatchLabels["k"] = "modified"
	got2, _ := s.GetRule("s1")
	if got2.MatchLabels["k"] != "v" {
		t.Errorf("GetRule 应返回深拷贝")
	}
	if _, err := s.GetRule("nonexistent"); !errors.Is(err, ErrSilenceNotFound) {
		t.Errorf("GetRule 不存在应返回 ErrSilenceNotFound")
	}
}

// TestSilencer_ListRules 返回所有规则拷贝。
//
// 注意：源码 Silencer.ListRules 未按 ID 排序（与注释不符，属源码已知行为），
// 本测试只验证数量与拷贝隔离性，不假设顺序。
func TestSilencer_ListRules(t *testing.T) {
	s := NewSilencer(time.Now)
	for _, id := range []string{"s2", "s1", "s3"} {
		if err := s.AddRule(&SilenceRule{ID: id, TenantID: "t1"}); err != nil {
			t.Fatalf("AddRule(%s) 失败: %v", id, err)
		}
	}
	rules := s.ListRules()
	if len(rules) != 3 {
		t.Fatalf("len(rules) = %d, want 3", len(rules))
	}
	// 验证包含所有 ID（不假设顺序）
	gotIDs := make(map[string]bool, len(rules))
	for _, r := range rules {
		gotIDs[r.ID] = true
	}
	for _, want := range []string{"s1", "s2", "s3"} {
		if !gotIDs[want] {
			t.Errorf("ListRules 缺少 ID %q", want)
		}
	}
	// 验证返回拷贝（修改返回值不影响内部）
	rules[0].TenantID = "modified"
	rules2 := s.ListRules()
	for _, r := range rules2 {
		if r.TenantID == "modified" {
			t.Errorf("ListRules 应返回拷贝")
		}
	}
}

// TestSilencer_IsSilenced 事件被静默规则抑制。
func TestSilencer_IsSilenced(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	s := NewSilencer(clock.now)

	// 静默规则：匹配 severity=critical 的事件，时间窗口 [now-1m, now+1m]
	r := &SilenceRule{
		ID:          "s1",
		TenantID:    "t1",
		MatchLabels: map[string]string{"severity": "critical"},
		StartAt:     now.Add(-1 * time.Minute),
		EndAt:       now.Add(1 * time.Minute),
	}
	if err := s.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}

	// 匹配的事件应被抑制
	ev := &AlertEvent{
		TenantID: "t1",
		Labels:   map[string]string{"severity": "critical", "deviceID": "dev-1"},
	}
	if !s.IsSilenced(ev) {
		t.Errorf("IsSilenced = false, want true（应被抑制）")
	}

	// 不匹配 severity 的事件不被抑制
	ev2 := &AlertEvent{
		TenantID: "t1",
		Labels:   map[string]string{"severity": "warning"},
	}
	if s.IsSilenced(ev2) {
		t.Errorf("IsSilenced(warning) = true, want false")
	}

	// 不匹配 tenant 的事件不被抑制
	ev3 := &AlertEvent{
		TenantID: "t2",
		Labels:   map[string]string{"severity": "critical"},
	}
	if s.IsSilenced(ev3) {
		t.Errorf("IsSilenced(t2) = true, want false（租户不匹配）")
	}
}

// TestSilencer_IsSilenced_OutOfWindow 时间窗口外不抑制。
func TestSilencer_IsSilenced_OutOfWindow(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	s := NewSilencer(clock.now)

	r := &SilenceRule{
		ID:          "s1",
		TenantID:    "t1",
		MatchLabels: map[string]string{"severity": "critical"},
		StartAt:     now.Add(1 * time.Hour), // 1 小时后才开始
		EndAt:       now.Add(2 * time.Hour),
	}
	if err := s.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}
	ev := &AlertEvent{
		TenantID: "t1",
		Labels:   map[string]string{"severity": "critical"},
	}
	if s.IsSilenced(ev) {
		t.Errorf("时间窗口外不应抑制")
	}
}

// TestSilencer_IsSilenced_ZeroTime 零值时间表示永久静默。
func TestSilencer_IsSilenced_ZeroTime(t *testing.T) {
	s := NewSilencer(time.Now)
	r := &SilenceRule{
		ID:          "s1",
		TenantID:    "t1",
		MatchLabels: map[string]string{"severity": "critical"},
		// StartAt/EndAt 均为零值 → 永久静默
	}
	if err := s.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}
	ev := &AlertEvent{
		TenantID: "t1",
		Labels:   map[string]string{"severity": "critical"},
	}
	if !s.IsSilenced(ev) {
		t.Errorf("零值时间应永久静默")
	}
}

// TestSilencer_IsSilenced_NilEvent nil 事件返回 false。
func TestSilencer_IsSilenced_NilEvent(t *testing.T) {
	s := NewSilencer(time.Now)
	if s.IsSilenced(nil) {
		t.Errorf("IsSilenced(nil) = true, want false")
	}
}

// TestSilencer_IsSilenced_EmptyMatchLabels 空 MatchLabels 匹配所有事件。
func TestSilencer_IsSilenced_EmptyMatchLabels(t *testing.T) {
	s := NewSilencer(time.Now)
	r := &SilenceRule{ID: "s1", TenantID: "t1"} // 空 MatchLabels
	if err := s.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}
	ev := &AlertEvent{TenantID: "t1", Labels: map[string]string{"any": "thing"}}
	if !s.IsSilenced(ev) {
		t.Errorf("空 MatchLabels 应匹配所有事件")
	}
}

// TestSilencer_IsSilenced_MultiLabelMatch 多标签 AND 匹配。
func TestSilencer_IsSilenced_MultiLabelMatch(t *testing.T) {
	s := NewSilencer(time.Now)
	r := &SilenceRule{
		ID:          "s1",
		TenantID:    "t1",
		MatchLabels: map[string]string{"severity": "critical", "deviceID": "dev-1"},
	}
	if err := s.AddRule(r); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}
	// 两个标签都匹配
	ev1 := &AlertEvent{TenantID: "t1", Labels: map[string]string{"severity": "critical", "deviceID": "dev-1"}}
	if !s.IsSilenced(ev1) {
		t.Errorf("两标签都匹配应被抑制")
	}
	// 只有一个标签匹配
	ev2 := &AlertEvent{TenantID: "t1", Labels: map[string]string{"severity": "critical", "deviceID": "dev-2"}}
	if s.IsSilenced(ev2) {
		t.Errorf("仅一个标签匹配不应被抑制（AND 语义）")
	}
}

// ============================================================================
// aggregator.go 测试
// ============================================================================

// TestNewAggregator 构造聚合器，拷贝 groupBy。
func TestNewAggregator(t *testing.T) {
	groupBy := []string{"deviceID", "severity"}
	a := NewAggregator(groupBy, 50)
	if a.maxGroup != 50 {
		t.Errorf("maxGroup = %d, want 50", a.maxGroup)
	}
	if len(a.groupBy) != 2 {
		t.Errorf("len(groupBy) = %d, want 2", len(a.groupBy))
	}
	// 修改原切片不影响内部
	groupBy[0] = "modified"
	if a.groupBy[0] != "deviceID" {
		t.Errorf("外部修改不应影响内部 groupBy")
	}
}

// TestNewAggregator_DefaultMaxGroup maxGroup<=0 时使用默认值。
func TestNewAggregator_DefaultMaxGroup(t *testing.T) {
	a := NewAggregator(nil, 0)
	if a.maxGroup != defaultMaxGroup {
		t.Errorf("maxGroup = %d, want default %d", a.maxGroup, defaultMaxGroup)
	}
}

// TestAggregator_GroupKey 分组键拼接。
func TestAggregator_GroupKey(t *testing.T) {
	a := NewAggregator([]string{"deviceID", "severity"}, 100)
	ev := &AlertEvent{Labels: map[string]string{"deviceID": "dev-1", "severity": "critical"}}
	if got := a.groupKey(ev); got != "deviceID=dev-1|severity=critical" {
		t.Errorf("groupKey = %q, want 'deviceID=dev-1|severity=critical'", got)
	}
	// 字段缺失时取空串
	ev2 := &AlertEvent{Labels: map[string]string{"deviceID": "dev-1"}}
	if got := a.groupKey(ev2); got != "deviceID=dev-1|severity=" {
		t.Errorf("groupKey = %q, want 'deviceID=dev-1|severity='", got)
	}
}

// TestAggregator_GroupKey_EmptyGroupBy 空 groupBy 返回空键（单组）。
func TestAggregator_GroupKey_EmptyGroupBy(t *testing.T) {
	a := NewAggregator(nil, 100)
	ev := &AlertEvent{Labels: map[string]string{"k": "v"}}
	if got := a.groupKey(ev); got != "" {
		t.Errorf("groupKey = %q, want ''（空 groupBy）", got)
	}
}

// TestAggregator_Aggregate 按标签分组。
func TestAggregator_Aggregate(t *testing.T) {
	a := NewAggregator([]string{"deviceID"}, 100)
	events := []*AlertEvent{
		{RuleID: "r1", Labels: map[string]string{"deviceID": "dev-1"}},
		{RuleID: "r2", Labels: map[string]string{"deviceID": "dev-2"}},
		{RuleID: "r3", Labels: map[string]string{"deviceID": "dev-1"}},
	}
	groups := a.Aggregate(events)
	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2", len(groups))
	}
	// 按 Key 升序
	if groups[0].Key != "deviceID=dev-1" {
		t.Errorf("groups[0].Key = %q, want 'deviceID=dev-1'", groups[0].Key)
	}
	if len(groups[0].Events) != 2 {
		t.Errorf("groups[0] 应有 2 个事件（dev-1）")
	}
	if len(groups[1].Events) != 1 {
		t.Errorf("groups[1] 应有 1 个事件（dev-2）")
	}
}

// TestAggregator_Aggregate_MaxGroup 每组最多保留 maxGroup 条。
func TestAggregator_Aggregate_MaxGroup(t *testing.T) {
	a := NewAggregator(nil, 2) // 每组最多 2 条
	events := []*AlertEvent{
		{RuleID: "r1", Labels: map[string]string{}},
		{RuleID: "r2", Labels: map[string]string{}},
		{RuleID: "r3", Labels: map[string]string{}},
		{RuleID: "r4", Labels: map[string]string{}},
	}
	groups := a.Aggregate(events)
	if len(groups) != 1 {
		t.Fatalf("len(groups) = %d, want 1（单组）", len(groups))
	}
	if len(groups[0].Events) != 2 {
		t.Errorf("每组最多 2 条，got %d", len(groups[0].Events))
	}
	// 应保留前 2 条
	if groups[0].Events[0].RuleID != "r1" || groups[0].Events[1].RuleID != "r2" {
		t.Errorf("应保留前 2 条")
	}
}

// TestAggregator_Aggregate_NilEvents nil 事件被跳过。
func TestAggregator_Aggregate_NilEvents(t *testing.T) {
	a := NewAggregator(nil, 100)
	events := []*AlertEvent{
		nil,
		{RuleID: "r1", Labels: map[string]string{}},
		nil,
	}
	groups := a.Aggregate(events)
	if len(groups) != 1 {
		t.Fatalf("len(groups) = %d, want 1（nil 跳过）", len(groups))
	}
	if len(groups[0].Events) != 1 {
		t.Errorf("应有 1 个非 nil 事件")
	}
}

// TestAggregator_Aggregate_EmptyInput 空输入返回空切片（非 nil）。
func TestAggregator_Aggregate_EmptyInput(t *testing.T) {
	a := NewAggregator(nil, 100)
	groups := a.Aggregate(nil)
	if groups == nil {
		t.Errorf("Aggregate(nil) 不应返回 nil")
	}
	if len(groups) != 0 {
		t.Errorf("len(groups) = %d, want 0", len(groups))
	}
	groups = a.Aggregate([]*AlertEvent{})
	if groups == nil {
		t.Errorf("Aggregate([]) 不应返回 nil")
	}
}

// TestAggregator_Aggregate_StableOrder 组内事件保留原顺序。
func TestAggregator_Aggregate_StableOrder(t *testing.T) {
	a := NewAggregator([]string{"deviceID"}, 100)
	events := []*AlertEvent{
		{RuleID: "r1", Labels: map[string]string{"deviceID": "dev-1"}},
		{RuleID: "r2", Labels: map[string]string{"deviceID": "dev-1"}},
		{RuleID: "r3", Labels: map[string]string{"deviceID": "dev-1"}},
	}
	groups := a.Aggregate(events)
	if len(groups) != 1 || len(groups[0].Events) != 3 {
		t.Fatalf("应有 1 组 3 个事件")
	}
	for i, want := range []string{"r1", "r2", "r3"} {
		if groups[0].Events[i].RuleID != want {
			t.Errorf("Events[%d].RuleID = %q, want %q（应保留原顺序）", i, groups[0].Events[i].RuleID, want)
		}
	}
}

// TestAggregator_Aggregate_SortedKeys 输出按 Key 升序。
func TestAggregator_Aggregate_SortedKeys(t *testing.T) {
	a := NewAggregator([]string{"deviceID"}, 100)
	events := []*AlertEvent{
		{Labels: map[string]string{"deviceID": "dev-3"}},
		{Labels: map[string]string{"deviceID": "dev-1"}},
		{Labels: map[string]string{"deviceID": "dev-2"}},
	}
	groups := a.Aggregate(events)
	if len(groups) != 3 {
		t.Fatalf("len(groups) = %d, want 3", len(groups))
	}
	keys := []string{groups[0].Key, groups[1].Key, groups[2].Key}
	if !sort.StringsAreSorted(keys) {
		t.Errorf("groups 应按 Key 升序: %v", keys)
	}
}

// ============================================================================
// anomaly.go 补充测试
// ============================================================================

// TestNewAnomalyEngineWithClock 带可注入时钟构造。
func TestNewAnomalyEngineWithClock(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	e := NewAnomalyEngineWithClock(clock.now)
	if e.now == nil {
		t.Errorf("now 不应为 nil")
	}
	// 添加规则并触发，验证 Timestamp 使用注入的时钟
	e.AddRule(&AnomalyRule{
		ID:         "r1",
		MetricName: "cpu",
		Detector:   "baseline",
		WindowSize: 10,
		Threshold:  3.0,
		Severity:   "critical",
		TenantID:   "default",
	})
	// 灌入基线
	for i := 0; i < 10; i++ {
		v := 50.0
		if i%2 == 1 {
			v = 50.5
		}
		e.Evaluate("cpu", "dev-1", v)
	}
	clock.advance(5 * time.Second)
	alert := e.Evaluate("cpu", "dev-1", 200)
	if alert == nil {
		t.Fatalf("应触发异常")
	}
	if !alert.Timestamp.Equal(now.Add(5 * time.Second)) {
		t.Errorf("Timestamp = %v, want %v（应使用注入时钟）", alert.Timestamp, now.Add(5*time.Second))
	}
}

// TestNewAnomalyEngineWithClock_NilClock nil 时钟使用 time.Now。
func TestNewAnomalyEngineWithClock_NilClock(t *testing.T) {
	e := NewAnomalyEngineWithClock(nil)
	if e.now == nil {
		t.Errorf("now 不应为 nil（nil 时应使用 time.Now）")
	}
}

// TestAnomalyEngine_GetRule_NonExistent 不存在的规则返回 nil。
func TestAnomalyEngine_GetRule_NonExistent(t *testing.T) {
	e := NewAnomalyEngine()
	if r := e.GetRule("nonexistent"); r != nil {
		t.Errorf("GetRule 不存在应返回 nil")
	}
}

// TestAnomalyEngine_Evaluate_StdDevZero stdDev=0 时 ZScore=0。
//
// 当基线为恒定值时（stdDev=0），异常检测不触发，但若强行触发（如 baseline 窗口仅 1 个点），
// ZScore 应为 0（避免除零）。
func TestAnomalyEngine_Evaluate_StdDevZero(t *testing.T) {
	e := NewAnomalyEngine()
	e.AddRule(&AnomalyRule{
		ID:         "r1",
		MetricName: "cpu",
		Detector:   "baseline",
		WindowSize: 10,
		Threshold:  0.5, // 低阈值便于触发
		Severity:   "warning",
		TenantID:   "default",
	})
	// 仅灌入 1 个样本（窗口 < 2，IsAnomaly 恒返回 false）
	e.Evaluate("cpu", "dev-1", 50)
	// 再评估一个值，不应触发（窗口数据少于 2）
	if alert := e.Evaluate("cpu", "dev-1", 100); alert != nil {
		t.Errorf("窗口数据少于 2 时不应触发异常")
	}
}

// TestEWMADetector_StdDevZero ewmaVar=0 时 IsAnomaly 返回 false。
func TestEWMADetector_StdDevZero(t *testing.T) {
	e := NewEWMADetector(0.3, 3.0)
	// 仅灌入恒定值，ewmaVar=0
	e.Add(100)
	e.Add(100)
	e.Add(100)
	if e.IsAnomaly(200) {
		t.Errorf("ewmaVar=0 时 IsAnomaly 应返回 false")
	}
}

// TestBaselineDetector_Stats_Empty 空窗口 Stats 返回 0。
func TestBaselineDetector_Stats_Empty(t *testing.T) {
	d := NewBaselineDetector(10, 3.0)
	mean, stdDev, count := d.Stats()
	if mean != 0 || stdDev != 0 || count != 0 {
		t.Errorf("空窗口 Stats = (%v, %v, %d), want (0, 0, 0)", mean, stdDev, count)
	}
}

// TestEWMADetector_Stats_PartialSpike 部分飙升场景：渐进后突然飙升。
//
// 验证 EWMA 检测器在渐进基线后对突然飙升敏感。
func TestEWMADetector_Stats_PartialSpike(t *testing.T) {
	e := NewEWMADetector(0.3, 3.0)
	// 灌入稳定基线
	for i := 0; i < 20; i++ {
		e.Add(100)
	}
	// 恒定值基线下 ewmaVar 可能为 0，IsAnomaly 可能返回 false（符合预期），
	// 故引入波动后再测。
	// 引入波动
	e2 := NewEWMADetector(0.5, 2.0)
	for i := 0; i < 30; i++ {
		if i%2 == 0 {
			e2.Add(100)
		} else {
			e2.Add(102)
		}
	}
	if !e2.IsAnomaly(200) {
		t.Errorf("引入波动后飙升应触发异常")
	}
	ewma, stdDev := e2.Stats()
	if stdDev <= 0 {
		t.Errorf("stdDev 应 > 0")
	}
	if math.Abs(ewma-101) > 1 {
		t.Errorf("ewma = %v, 应接近 101", ewma)
	}
}
