// anomaly_test.go 测试 anomaly.go 中的异常检测算法与引擎。
package alertengine

import (
	"math"
	"testing"
)

// ============================================================================
// BaselineDetector 测试
// ============================================================================

// TestBaselineDetector_Normal 正常数据不触发异常。
//
// 构造 50 个均值 50、轻微波动的数据点（49-51 之间），
// 后续相同范围的数据点 Z-Score 应远小于 3.0，不触发异常。
func TestBaselineDetector_Normal(t *testing.T) {
	d := NewBaselineDetector(50, 3.0)
	// 灌入基线数据：50 ± 1
	for i := 0; i < 50; i++ {
		v := 50.0 + float64(i%3-1) // 49, 50, 51 循环
		d.Add(v)
	}
	// 检查统计量
	mean, stdDev, count := d.Stats()
	if count != 50 {
		t.Errorf("count = %d, want 50", count)
	}
	if math.Abs(mean-50) > 0.5 {
		t.Errorf("mean = %v, want ~50", mean)
	}
	if stdDev <= 0 {
		t.Errorf("stdDev = %v, want > 0", stdDev)
	}
	// 正常范围内的值不应触发异常
	for _, v := range []float64{49, 50, 51, 48, 52} {
		if d.IsAnomaly(v) {
			t.Errorf("IsAnomaly(%v) = true, want false（正常范围内不应异常）", v)
		}
	}
}

// TestBaselineDetector_Anomaly 突然飙升触发异常。
//
// 灌入稳定基线（恒定 100），然后突然输入 200，
// Z-Score 应远大于 3.0（实际因 stdDev=0 不会触发，故需引入轻微波动）。
// 引入轻微波动后，200 的 Z-Score 应超过阈值。
func TestBaselineDetector_Anomaly(t *testing.T) {
	d := NewBaselineDetector(100, 3.0)
	// 灌入基线：100 ± 0.5 的轻微波动，使 stdDev > 0
	for i := 0; i < 100; i++ {
		v := 100.5
		if i%2 == 0 {
			v = 100.0
		}
		d.Add(v)
	}
	_, stdDev, _ := d.Stats()
	if stdDev <= 0 {
		t.Fatalf("stdDev = %v, 基线应有波动使 stdDev > 0", stdDev)
	}
	// 突然飙升到 200，应触发异常
	if !d.IsAnomaly(200) {
		t.Errorf("IsAnomaly(200) = false, want true（突然飙升应触发异常）")
	}
	// 突然跌到 0，也应触发异常（双向检测）
	if !d.IsAnomaly(0) {
		t.Errorf("IsAnomaly(0) = false, want true（突然下跌应触发异常）")
	}
}

// TestBaselineDetector_WindowEviction 窗口满后旧数据被淘汰。
//
// 构造窗口大小 5，灌入 10 个数据点，
// 窗口应只保留最后 5 个，统计量应基于最后 5 个计算。
func TestBaselineDetector_WindowEviction(t *testing.T) {
	d := NewBaselineDetector(5, 3.0)
	// 灌入 1..10
	for i := 1; i <= 10; i++ {
		d.Add(float64(i))
	}
	_, _, count := d.Stats()
	if count != 5 {
		t.Errorf("count = %d, want 5（窗口应淘汰旧数据）", count)
	}
	// 最后 5 个为 6,7,8,9,10，均值应为 8
	mean, _, _ := d.Stats()
	if math.Abs(mean-8) > 0.001 {
		t.Errorf("mean = %v, want 8（窗口应只含 6-10）", mean)
	}
}

// TestBaselineDetector_DefaultParams 默认参数构造。
//
// windowSize<=0 默认 100，threshold<=0 默认 3.0。
func TestBaselineDetector_DefaultParams(t *testing.T) {
	d := NewBaselineDetector(0, 0)
	if d.maxSize != 100 {
		t.Errorf("maxSize = %d, want default 100", d.maxSize)
	}
	if d.threshold != 3.0 {
		t.Errorf("threshold = %v, want default 3.0", d.threshold)
	}
}

// TestBaselineDetector_SinglePoint 单点数据不触发异常。
//
// 窗口仅 1 个数据点时 stdDev=0，IsAnomaly 应返回 false。
func TestBaselineDetector_SinglePoint(t *testing.T) {
	d := NewBaselineDetector(10, 3.0)
	d.Add(100)
	if d.IsAnomaly(100) {
		t.Errorf("IsAnomaly(100) = true, want false（单点无法计算 Z-Score）")
	}
	if d.IsAnomaly(1000) {
		t.Errorf("IsAnomaly(1000) = true, want false（单点 stdDev=0 应返回 false）")
	}
}

// ============================================================================
// EWMADetector 测试
// ============================================================================

// TestEWMADetector_Spike 突然飙升检测。
//
// 灌入稳定基线（恒定 100），EWMA 收敛到 100，方差收敛到 0，
// 但因 ewmaVar=0 时 IsAnomaly 返回 false，故需引入轻微波动使 ewmaVar > 0。
// 引入轻微波动后，突然飙升到 200 应触发异常。
func TestEWMADetector_Spike(t *testing.T) {
	e := NewEWMADetector(0.3, 3.0)
	// 灌入基线：100 与 101 交替，使 ewmaVar > 0
	for i := 0; i < 100; i++ {
		if i%2 == 0 {
			e.Add(100)
		} else {
			e.Add(101)
		}
	}
	ewma, stdDev := e.Stats()
	if stdDev <= 0 {
		t.Fatalf("stdDev = %v, 基线应有波动使 stdDev > 0", stdDev)
	}
	if math.Abs(ewma-100.5) > 0.5 {
		t.Errorf("ewma = %v, want ~100.5", ewma)
	}
	// 突然飙升到 200，应触发异常
	if !e.IsAnomaly(200) {
		t.Errorf("IsAnomaly(200) = false, want true（突然飙升应触发异常）")
	}
}

// TestEWMADetector_GradualIncrease 渐进增长不误报。
//
// 从 50 渐进增长到 60（每次 +0.1），EWMA 会跟随上移，
// 每个新值与 ewma 的差应小于 threshold * stdDev，不触发异常。
func TestEWMADetector_GradualIncrease(t *testing.T) {
	e := NewEWMADetector(0.3, 3.0)
	// 渐进增长：50, 50.1, 50.2, ..., 60（共 101 步）
	anomalyCount := 0
	for i := 0; i <= 100; i++ {
		v := 50.0 + 0.1*float64(i)
		e.Add(v)
		if e.IsAnomaly(v) {
			anomalyCount++
		}
	}
	// 渐进增长不应触发异常（允许初期 1-2 次因方差未稳定误报，但不应大量误报）
	if anomalyCount > 5 {
		t.Errorf("渐进增长触发 %d 次异常，应 ≤ 5（渐进变化不应大量误报）", anomalyCount)
	}
}

// TestEWMADetector_DefaultParams 默认参数构造。
//
// alpha<=0 或 >=1 默认 0.3，threshold<=0 默认 3.0。
func TestEWMADetector_DefaultParams(t *testing.T) {
	e := NewEWMADetector(0, 0)
	if e.alpha != 0.3 {
		t.Errorf("alpha = %v, want default 0.3", e.alpha)
	}
	if e.threshold != 3.0 {
		t.Errorf("threshold = %v, want default 3.0", e.threshold)
	}
	// alpha>=1 也应回退默认
	e2 := NewEWMADetector(1.5, 0)
	if e2.alpha != 0.3 {
		t.Errorf("alpha = %v, want default 0.3（alpha>=1 应回退）", e2.alpha)
	}
}

// TestEWMADetector_Uninitialized 未初始化时不触发异常。
func TestEWMADetector_Uninitialized(t *testing.T) {
	e := NewEWMADetector(0.3, 3.0)
	// 未调用 Add，IsAnomaly 应返回 false
	if e.IsAnomaly(100) {
		t.Errorf("IsAnomaly(100) = true, want false（未初始化）")
	}
	ewma, stdDev := e.Stats()
	if ewma != 0 || stdDev != 0 {
		t.Errorf("Stats = (%v, %v), want (0, 0)（未初始化）", ewma, stdDev)
	}
}

// ============================================================================
// AnomalyEngine 测试
// ============================================================================

// TestAnomalyEngine_Evaluate 多规则评估。
//
// 构造两条规则（cpu_usage baseline + mem_usage ewma），
// 灌入基线后突然飙升，应返回对应规则的告警。
func TestAnomalyEngine_Evaluate(t *testing.T) {
	e := NewAnomalyEngine()
	e.AddRule(&AnomalyRule{
		ID:         "rule-cpu-baseline",
		MetricName: "cpu_usage",
		DeviceID:   "", // 所有设备
		Detector:   "baseline",
		WindowSize: 50,
		Threshold:  3.0,
		Severity:   "critical",
		TenantID:   "default",
	})
	e.AddRule(&AnomalyRule{
		ID:         "rule-mem-ewma",
		MetricName: "mem_usage",
		DeviceID:   "",
		Detector:   "ewma",
		Threshold:  3.0,
		Severity:   "warning",
		TenantID:   "default",
	})

	// 灌入基线：cpu_usage 在 50±0.5 波动，mem_usage 在 60/61 交替
	for i := 0; i < 50; i++ {
		cpu := 50.0
		if i%2 == 1 {
			cpu = 50.5
		}
		e.Evaluate("cpu_usage", "dev-1", cpu)

		mem := 60.0
		if i%2 == 1 {
			mem = 61.0
		}
		e.Evaluate("mem_usage", "dev-1", mem)
	}

	// 突然飙升：cpu_usage -> 100，应触发 rule-cpu-baseline
	alert := e.Evaluate("cpu_usage", "dev-1", 100)
	if alert == nil {
		t.Fatalf("Evaluate(cpu_usage, dev-1, 100) = nil, want alert")
	}
	if alert.RuleID != "rule-cpu-baseline" {
		t.Errorf("RuleID = %q, want rule-cpu-baseline", alert.RuleID)
	}
	if alert.MetricName != "cpu_usage" {
		t.Errorf("MetricName = %q, want cpu_usage", alert.MetricName)
	}
	if alert.DeviceID != "dev-1" {
		t.Errorf("DeviceID = %q, want dev-1", alert.DeviceID)
	}
	if alert.Value != 100 {
		t.Errorf("Value = %v, want 100", alert.Value)
	}
	if alert.Severity != "critical" {
		t.Errorf("Severity = %q, want critical", alert.Severity)
	}
	if alert.ZScore <= 3.0 {
		t.Errorf("ZScore = %v, want > 3.0", alert.ZScore)
	}

	// 突然飙升：mem_usage -> 150，应触发 rule-mem-ewma
	alert2 := e.Evaluate("mem_usage", "dev-1", 150)
	if alert2 == nil {
		t.Fatalf("Evaluate(mem_usage, dev-1, 150) = nil, want alert")
	}
	if alert2.RuleID != "rule-mem-ewma" {
		t.Errorf("RuleID = %q, want rule-mem-ewma", alert2.RuleID)
	}
	if alert2.Severity != "warning" {
		t.Errorf("Severity = %q, want warning", alert2.Severity)
	}
}

// TestAnomalyEngine_NoRule 无匹配规则返回 nil。
func TestAnomalyEngine_NoRule(t *testing.T) {
	e := NewAnomalyEngine()
	e.AddRule(&AnomalyRule{
		ID:         "rule-cpu",
		MetricName: "cpu_usage",
		Detector:   "baseline",
		WindowSize: 10,
		Threshold:  3.0,
		Severity:   "warning",
		TenantID:   "default",
	})
	// 不匹配的 metricName
	if alert := e.Evaluate("disk_usage", "dev-1", 99); alert != nil {
		t.Errorf("Evaluate(disk_usage, ...) = %v, want nil（无匹配规则）", alert)
	}
	// 不匹配的 deviceID（规则绑定 dev-2，传入 dev-1）
	e.AddRule(&AnomalyRule{
		ID:         "rule-disk-bound",
		MetricName: "disk_usage",
		DeviceID:   "dev-2",
		Detector:   "baseline",
		WindowSize: 10,
		Threshold:  3.0,
		Severity:   "warning",
		TenantID:   "default",
	})
	if alert := e.Evaluate("disk_usage", "dev-1", 99); alert != nil {
		t.Errorf("Evaluate(disk_usage, dev-1, ...) = %v, want nil（deviceID 不匹配）", alert)
	}
	// 空引擎
	e2 := NewAnomalyEngine()
	if alert := e2.Evaluate("cpu_usage", "dev-1", 99); alert != nil {
		t.Errorf("空引擎 Evaluate = %v, want nil", alert)
	}
}

// TestAnomalyEngine_AddRemoveRule 动态增删规则。
//
// 添加规则后评估应生效；删除后评估应返回 nil。
func TestAnomalyEngine_AddRemoveRule(t *testing.T) {
	e := NewAnomalyEngine()

	// 初始无规则
	if alert := e.Evaluate("cpu_usage", "dev-1", 100); alert != nil {
		t.Errorf("无规则时 Evaluate = %v, want nil", alert)
	}

	// 添加规则
	e.AddRule(&AnomalyRule{
		ID:         "rule-1",
		MetricName: "cpu_usage",
		Detector:   "baseline",
		WindowSize: 20,
		Threshold:  3.0,
		Severity:   "critical",
		TenantID:   "default",
	})
	// 灌入基线
	for i := 0; i < 20; i++ {
		v := 50.0
		if i%2 == 1 {
			v = 50.5
		}
		e.Evaluate("cpu_usage", "dev-1", v)
	}
	// 飙升应触发
	if alert := e.Evaluate("cpu_usage", "dev-1", 200); alert == nil {
		t.Errorf("添加规则后 Evaluate 应触发异常")
	}

	// 删除规则
	e.RemoveRule("rule-1")
	if alert := e.Evaluate("cpu_usage", "dev-1", 200); alert != nil {
		t.Errorf("删除规则后 Evaluate = %v, want nil", alert)
	}

	// 删除不存在的规则不应 panic
	e.RemoveRule("nonexistent")

	// GetRule 验证
	if r := e.GetRule("rule-1"); r != nil {
		t.Errorf("GetRule(rule-1) = %v, want nil（已删除）", r)
	}
}

// TestAnomalyEngine_DeviceIDBinding DeviceID 绑定规则只匹配指定设备。
func TestAnomalyEngine_DeviceIDBinding(t *testing.T) {
	e := NewAnomalyEngine()
	e.AddRule(&AnomalyRule{
		ID:         "rule-bound",
		MetricName: "cpu_usage",
		DeviceID:   "dev-special",
		Detector:   "baseline",
		WindowSize: 20,
		Threshold:  3.0,
		Severity:   "warning",
		TenantID:   "default",
	})
	// 给 dev-special 灌入基线
	for i := 0; i < 20; i++ {
		v := 50.0
		if i%2 == 1 {
			v = 50.5
		}
		e.Evaluate("cpu_usage", "dev-special", v)
	}
	// dev-special 飙升应触发
	if alert := e.Evaluate("cpu_usage", "dev-special", 200); alert == nil {
		t.Errorf("dev-special 飙升应触发")
	}
	// dev-other 飙升不应触发（规则绑定 dev-special）
	if alert := e.Evaluate("cpu_usage", "dev-other", 200); alert != nil {
		t.Errorf("dev-other 飙升不应触发（规则绑定 dev-special）: %v", alert)
	}
}

// TestAnomalyEngine_ListRules 列出所有规则。
func TestAnomalyEngine_ListRules(t *testing.T) {
	e := NewAnomalyEngine()
	e.AddRule(&AnomalyRule{ID: "rule-b", MetricName: "cpu_usage", Detector: "baseline", WindowSize: 10, Threshold: 3.0, Severity: "warning", TenantID: "default"})
	e.AddRule(&AnomalyRule{ID: "rule-a", MetricName: "mem_usage", Detector: "ewma", Threshold: 3.0, Severity: "critical", TenantID: "default"})
	e.AddRule(&AnomalyRule{ID: "rule-c", MetricName: "disk_usage", Detector: "baseline", WindowSize: 10, Threshold: 3.0, Severity: "warning", TenantID: "default"})

	rules := e.ListRules()
	if len(rules) != 3 {
		t.Fatalf("len(rules) = %d, want 3", len(rules))
	}
	// 应按 ID 升序
	want := []string{"rule-a", "rule-b", "rule-c"}
	for i, w := range want {
		if rules[i].ID != w {
			t.Errorf("rules[%d].ID = %q, want %q", i, rules[i].ID, w)
		}
	}
}

// TestAnomalyEngine_AddRuleNil 添加 nil 规则静默返回。
func TestAnomalyEngine_AddRuleNil(t *testing.T) {
	e := NewAnomalyEngine()
	e.AddRule(nil) // 不应 panic
	if len(e.ListRules()) != 0 {
		t.Errorf("AddRule(nil) 后应有 0 条规则")
	}
}

// TestAnomalyEngine_UnknownDetector 未知检测器类型回退 baseline。
//
// 防御式：Detector 字段为非 "ewma" 时均构造 BaselineDetector。
func TestAnomalyEngine_UnknownDetector(t *testing.T) {
	e := NewAnomalyEngine()
	e.AddRule(&AnomalyRule{
		ID:         "rule-unknown",
		MetricName: "cpu_usage",
		Detector:   "unknown-type", // 未知类型
		WindowSize: 20,
		Threshold:  3.0,
		Severity:   "warning",
		TenantID:   "default",
	})
	// 灌入基线
	for i := 0; i < 20; i++ {
		v := 50.0
		if i%2 == 1 {
			v = 50.5
		}
		e.Evaluate("cpu_usage", "dev-1", v)
	}
	// 飙升应触发（回退 baseline 仍能工作）
	if alert := e.Evaluate("cpu_usage", "dev-1", 200); alert == nil {
		t.Errorf("未知检测器类型回退 baseline 后应能触发异常")
	}
}
