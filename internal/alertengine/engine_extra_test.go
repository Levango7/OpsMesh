// engine_extra_test.go 针对 go tool cover -func 检出的残余低覆盖分支补全测试。
//
// 覆盖目标（与 engine_test.go / anomaly_test.go / inhibitor_test.go 互补，不重复）：
//   - anomaly.go: BaselineDetector stdDev=0 分支、recomputeLocked 空窗口早退、
//     AnomalyEngine.GetRule 拷贝路径、Evaluate 缺失检测器防御与多规则排序确定性
//   - inhibitor.go: newAlertInhibitor 参数回退、IsInhibited nil 父告警防御、RemoveActive 空 ID
//   - silencer.go: UpdateRule MatchLabels 深拷贝、IsSilenced EndAt 过期分支
//   - 端到端联动：Engine.Evaluate → Aggregator 分组 → Silencer 抑制判定
package alertengine

import (
	"testing"
	"time"

)

// ============================================================================
// anomaly.go 补充分支
// ============================================================================

// TestBaselineDetector_ZeroStdDevMultiPoints 多点恒定值基线 stdDev=0 时 IsAnomaly 返回 false。
//
// 窗口内有 >=2 个样本但全部相等时 stdDev=0，Z-Score 无法计算，
// 应防御式返回 false 而非除零 panic。
func TestBaselineDetector_ZeroStdDevMultiPoints(t *testing.T) {
	d := NewBaselineDetector(10, 3.0)
	d.Add(100)
	d.Add(100)
	d.Add(100)

	mean, stdDev, count := d.Stats()
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	if mean != 100 {
		t.Errorf("mean = %v, want 100", mean)
	}
	if stdDev != 0 {
		t.Errorf("stdDev = %v, want 0（恒定值基线）", stdDev)
	}
	// stdDev=0：无论输入偏离多大都不应判异常，也不应 panic
	if d.IsAnomaly(200) {
		t.Errorf("IsAnomaly(200) = true, want false（stdDev=0 无法计算 Z-Score）")
	}
	if d.IsAnomaly(-500) {
		t.Errorf("IsAnomaly(-500) = true, want false（stdDev=0 无法计算 Z-Score）")
	}
}

// TestBaselineDetector_RecomputeLockedEmptyWindow 空窗口时 recomputeLocked 归零统计量。
//
// 该分支仅在生产代码防御路径出现（Add 后窗口至少含 1 个元素），
// 同包测试直接构造零值结构体验证其正确归零而非 panic。
func TestBaselineDetector_RecomputeLockedEmptyWindow(t *testing.T) {
	b := &BaselineDetector{
		mean:      42, // 预置脏值，验证被归零
		stdDev:    7,
		maxSize:   5,
		threshold: 3.0,
	}
	b.recomputeLocked()
	if b.mean != 0 || b.stdDev != 0 {
		t.Errorf("空窗口 recomputeLocked 后 mean/stdDev = (%v, %v), want (0, 0)", b.mean, b.stdDev)
	}
	mean, stdDev, count := b.Stats()
	if mean != 0 || stdDev != 0 || count != 0 {
		t.Errorf("Stats = (%v, %v, %d), want (0, 0, 0)", mean, stdDev, count)
	}
}

// TestAnomalyEngine_GetRuleReturnsCopy 存在的规则返回深拷贝，外部修改不影响内部。
//
// 补齐 GetRule 的"命中"路径（既有测试只覆盖了"不存在返回 nil"）。
func TestAnomalyEngine_GetRuleReturnsCopy(t *testing.T) {
	e := NewAnomalyEngine()
	orig := &AnomalyRule{
		ID:         "rule-copy",
		MetricName: "cpu_usage",
		Detector:   "baseline",
		WindowSize: 10,
		Threshold:  3.0,
		Severity:   SeverityWarning,
		TenantID:   "t1",
	}
	e.AddRule(orig)

	got := e.GetRule("rule-copy")
	if got == nil {
		t.Fatal("GetRule(rule-copy) = nil, want 非 nil")
	}
	if got.ID != "rule-copy" || got.MetricName != "cpu_usage" {
		t.Errorf("GetRule 内容不一致: %+v", got)
	}

	// 修改返回值不应影响引擎内部
	got.Severity = "critical"
	got.MetricName = "mem_usage"
	got.Threshold = 99

	got2 := e.GetRule("rule-copy")
	if got2 == nil {
		t.Fatal("第二次 GetRule = nil")
	}
	if got2.Severity != SeverityWarning {
		t.Errorf("Severity = %q, want %q（应返回拷贝）", got2.Severity, SeverityWarning)
	}
	if got2.MetricName != "cpu_usage" {
		t.Errorf("MetricName = %q, want cpu_usage（应返回拷贝）", got2.MetricName)
	}
	if got2.Threshold != 3.0 {
		t.Errorf("Threshold = %v, want 3.0（应返回拷贝）", got2.Threshold)
	}
}

// TestAnomalyEngine_EvaluateDetectorMissing 规则存在但检测器缺失时跳过该规则。
//
// 正常路径 AddRule 总是同步创建 detector，此防御分支需直接注入内部状态触发：
// 验证 Evaluate 遇到 detector 缺失时跳过该规则且返回 nil，不 panic。
func TestAnomalyEngine_EvaluateDetectorMissing(t *testing.T) {
	e := NewAnomalyEngine()
	e.mu.Lock()
	e.rules["ghost"] = &AnomalyRule{
		ID:         "ghost",
		MetricName: "cpu_usage",
		DeviceID:   "", // 匹配所有设备
		Severity:   SeverityWarning,
	}
	// 故意不注册 e.detectors["ghost"]
	e.mu.Unlock()

	if alert := e.Evaluate("cpu_usage", "dev-1", 999); alert != nil {
		t.Errorf("Evaluate = %v, want nil（detector 缺失应跳过规则）", alert)
	}
}

// TestAnomalyEngine_EvaluateMultiRuleLowestIDWins 多条规则同时命中时返回 ID 最小者。
//
// map 遍历顺序不确定，Evaluate 内部按 ruleID 升序插入排序后取首个触发告警，
// 保证多规则并发命中的确定性。注册 5 条规则提高排序交换路径的触发概率。
func TestAnomalyEngine_EvaluateMultiRuleLowestIDWins(t *testing.T) {
	e := NewAnomalyEngine()
	ids := []string{"r-e", "r-b", "r-d", "r-a", "r-c"} // 乱序注册
	for _, id := range ids {
		e.AddRule(&AnomalyRule{
			ID:         id,
			MetricName: "cpu_usage",
			Detector:   "baseline",
			WindowSize: 20,
			Threshold:  3.0,
			Severity:   SeverityCritical,
			TenantID:   "t1",
		})
	}
	// 灌入稳定基线：50 ± 0.5 交替
	for i := 0; i < 20; i++ {
		v := 50.0
		if i%2 == 1 {
			v = 50.5
		}
		if alert := e.Evaluate("cpu_usage", "dev-1", v); alert != nil {
			t.Fatalf("基线阶段不应触发异常: %+v", alert)
		}
	}
	// 统一飙升：所有规则均满足异常条件，应返回 ID 最小的 r-a
	alert := e.Evaluate("cpu_usage", "dev-1", 200)
	if alert == nil {
		t.Fatal("飙升后应触发异常")
	}
	if alert.RuleID != "r-a" {
		t.Errorf("RuleID = %q, want %q（多条命中时应返回 ID 最小者）", alert.RuleID, "r-a")
	}
}

// ============================================================================
// inhibitor.go 补充分支
// ============================================================================

// TestNewAlertInhibitorFallbackDefaults ttl<=0 与 nil 时钟回退默认值。
//
// 公开构造器 NewAlertInhibitor 固定传 defaultInhibitTTL/time.Now，
// 回退分支需经内部构造器直接验证。
func TestNewAlertInhibitorFallbackDefaults(t *testing.T) {
	in := newAlertInhibitor(nil, 0, nil)
	if in.ttl != defaultInhibitTTL {
		t.Errorf("ttl = %v, want default %v（ttl<=0 应回退默认）", in.ttl, defaultInhibitTTL)
	}
	if in.now == nil {
		t.Errorf("now 不应为 nil（nil 应回退 time.Now）")
	}
	// 负 TTL 同样回退
	in2 := newAlertInhibitor(nil, -1*time.Second, nil)
	if in2.ttl != defaultInhibitTTL {
		t.Errorf("ttl = %v, want default %v（负 TTL 应回退默认）", in2.ttl, defaultInhibitTTL)
	}
}

// TestInhibitorNilParentEntrySkipped 活跃表中 nil 条目被跳过。
//
// TrackActive 防御了 nil 输入，但内部 map 若因故存在 nil 条目，
// IsInhibited 应跳过而非解引用 panic。
func TestInhibitorNilParentEntrySkipped(t *testing.T) {
	in := NewAlertInhibitor([]InhibitRule{hostDownRule()})
	in.mu.Lock()
	in.activeAlerts["ghost"] = nil // 注入非法条目
	in.trackedAt["ghost"] = in.now()
	in.mu.Unlock()

	child := newTestAlert("c1", "service_status", "dev-1", SeverityWarning, "firing")
	if in.IsInhibited(child) {
		t.Errorf("nil 父告警不应导致抑制")
	}
	if in.ActiveCount() != 1 {
		t.Errorf("ActiveCount = %d, want 1（nil 条目仍在表中，由 Cleanup 统一清理）", in.ActiveCount())
	}
}

// TestInhibitorRemoveActiveEmptyID 空 AlertID 时 RemoveActive 幂等早退。
func TestInhibitorRemoveActiveEmptyID(t *testing.T) {
	in := NewAlertInhibitor([]InhibitRule{hostDownRule()})
	parent := newTestAlert("p1", "host_status", "dev-1", SeverityCritical, "firing")
	in.TrackActive(parent)

	in.RemoveActive("") // 不应 panic，也不应影响已有活跃告警
	if in.ActiveCount() != 1 {
		t.Errorf("ActiveCount = %d, want 1（空 ID 不应误删活跃告警）", in.ActiveCount())
	}
	child := newTestAlert("c1", "service_status", "dev-1", SeverityWarning, "firing")
	if !in.IsInhibited(child) {
		t.Errorf("空 ID RemoveActive 后父告警应仍然生效")
	}
}

// ============================================================================
// silencer.go 补充分支
// ============================================================================

// TestSilencerUpdateRuleMatchLabelsDeepCopy UpdateRule 深拷贝 MatchLabels。
//
// 补齐 UpdateRule 的非空 MatchLabels 分支（既有测试仅用 nil 标签）。
func TestSilencerUpdateRuleMatchLabelsDeepCopy(t *testing.T) {
	s := NewSilencer(time.Now)
	if err := s.AddRule(&SilenceRule{ID: "s1", TenantID: "t1"}); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}

	up := &SilenceRule{
		ID:          "s1",
		TenantID:    "t1",
		MatchLabels: map[string]string{"severity": "critical"},
	}
	if err := s.UpdateRule(up); err != nil {
		t.Fatalf("UpdateRule 失败: %v", err)
	}

	// 外部修改传入的 map 不应影响内部
	up.MatchLabels["severity"] = "info"

	got, err := s.GetRule("s1")
	if err != nil {
		t.Fatalf("GetRule 失败: %v", err)
	}
	if got.MatchLabels["severity"] != "critical" {
		t.Errorf("MatchLabels[severity] = %q, want critical（UpdateRule 应深拷贝）", got.MatchLabels["severity"])
	}

	// 行为级验证：内部规则仍按 critical 匹配
	evCrit := &AlertEvent{TenantID: "t1", Labels: map[string]string{"severity": "critical"}}
	if !s.IsSilenced(evCrit) {
		t.Errorf("critical 事件应被抑制（内部标签未被污染）")
	}
}

// TestSilencerIsSilencedEndAtExpired EndAt 过期后不再抑制（双向窗口边界）。
//
// 使用注入时钟精确控制"窗内→窗外"迁移，覆盖 now.After(EndAt) 分支，
// 避免 wall-clock sleep 造成 flaky。
func TestSilencerIsSilencedEndAtExpired(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	s := NewSilencer(clock.now)

	rule := &SilenceRule{
		ID:          "s-window",
		TenantID:    "t1",
		MatchLabels: map[string]string{"severity": SeverityCritical},
		StartAt:     now.Add(-1 * time.Hour),
		EndAt:       now.Add(1 * time.Hour),
	}
	if err := s.AddRule(rule); err != nil {
		t.Fatalf("AddRule 失败: %v", err)
	}

	ev := &AlertEvent{TenantID: "t1", Labels: map[string]string{"severity": SeverityCritical}}

	// 窗口内：抑制生效
	if !s.IsSilenced(ev) {
		t.Errorf("窗口内事件应被抑制")
	}
	// 推进时钟越过 EndAt（now > EndAt）
	clock.advance(2 * time.Hour)
	if s.IsSilenced(ev) {
		t.Errorf("EndAt 过期后不应抑制（now.After(EndAt) 分支）")
	}
}

// ============================================================================
// 端到端联动：Evaluate → Aggregate → Silence
// ============================================================================

// TestPipelineEvaluateGroupSilence 引擎评估产出事件后，聚合器按 severity 分组、
// 静默器按标签抑制 critical 事件的完整链路。
//
// 验证三者的 Labels 契约（buildEvent 写入的 severity/deviceID 标签
// 可直接被 Aggregator.groupKey 与 Silencer.IsSilenced 消费）。
func TestPipelineEvaluateGroupSilence(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	m := newFakeMetrics().set("cpu_usage", "dev-1", 95)
	e := NewEngine(m, nil, clock.now)

	// 两条规则同一指标、不同严重度，均立即触发
	rCrit := makeRule("r-crit", "t1", true, LogicAnd, cpuCond(">", 90))
	rCrit.Severity = SeverityCritical
	rWarn := makeRule("r-warn", "t1", true, LogicAnd, cpuCond(">", 50))
	rWarn.Severity = SeverityWarning
	if err := e.AddRule(rCrit); err != nil {
		t.Fatalf("AddRule r-crit 失败: %v", err)
	}
	if err := e.AddRule(rWarn); err != nil {
		t.Fatalf("AddRule r-warn 失败: %v", err)
	}

	events, err := e.Evaluate("dev-1")
	if err != nil {
		t.Fatalf("Evaluate 失败: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2（两条规则均触发）", len(events))
	}

	// 聚合：按 severity 分组，输出按 Key 升序
	agg := NewAggregator([]string{"severity"}, 100)
	groups := agg.Aggregate(events)
	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2", len(groups))
	}
	if groups[0].Key != "severity=critical" || groups[1].Key != "severity=warning" {
		t.Errorf("groups keys = (%q, %q), want (severity=critical, severity=warning)",
			groups[0].Key, groups[1].Key)
	}
	for _, g := range groups {
		if len(g.Events) != 1 {
			t.Errorf("group %q 应含 1 个事件, got %d", g.Key, len(g.Events))
		}
	}

	// 静默：抑制 critical、放行 warning
	clock.advance(30 * time.Second) // 静默窗口 [now, now+1m]
	sil := NewSilencer(clock.now)
	if err := sil.AddRule(&SilenceRule{
		ID:          "s1",
		TenantID:    "t1",
		MatchLabels: map[string]string{"severity": SeverityCritical},
		StartAt:     clock.t,
		EndAt:       clock.t.Add(1 * time.Minute),
	}); err != nil {
		t.Fatalf("AddRule(silence) 失败: %v", err)
	}

	var critEv, warnEv *AlertEvent
	for _, ev := range events {
		switch ev.Severity {
		case SeverityCritical:
			critEv = ev
		case SeverityWarning:
			warnEv = ev
		}
	}
	if critEv == nil || warnEv == nil {
		t.Fatalf("事件严重度缺失: %+v", events)
	}
	if !sil.IsSilenced(critEv) {
		t.Errorf("critical 事件应被静默规则抑制")
	}
	if sil.IsSilenced(warnEv) {
		t.Errorf("warning 事件不应被静默规则抑制")
	}
}