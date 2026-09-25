// memory_bounds_test.go — P1-4：内存驻留缓冲硬上限（agentLogs / deviceMetrics）。
//
// 覆盖：日志批次数量上限、日志总行数上限、设备指标 map 上限与「最久未写入优先淘汰」语义。
package store

import (
	"fmt"
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// TestSaveLogs_ReportCountBounded 验证日志批次数超过上限后丢弃最旧批次（保留最新）。
func TestSaveLogs_ReportCountBounded(t *testing.T) {
	m := NewMemoryStore()
	total := maxAgentLogReports + 50
	for i := 0; i < total; i++ {
		rep := &proto.LogReport{
			AgentID: "agent-1", LogName: fmt.Sprintf("log-%d", i),
			Lines: []proto.LogLine{{Message: fmt.Sprintf("line-%d", i)}},
		}
		if err := m.SaveLogs("t1", rep); err != nil {
			t.Fatalf("SaveLogs 失败: %v", err)
		}
	}
	got := m.AgentLogs("t1", "agent-1", "")
	if len(got) != maxAgentLogReports {
		t.Fatalf("批次数应被裁剪到 %d，实际 %d", maxAgentLogReports, len(got))
	}
	// 最旧的 50 条应被丢弃：首条保留的应是 log-50。
	if got[0].LogName != "log-50" {
		t.Errorf("应丢弃最旧批次，首条 = %q, want log-50", got[0].LogName)
	}
	// 最新的必须保留。
	if got[len(got)-1].LogName != fmt.Sprintf("log-%d", total-1) {
		t.Errorf("应保留最新批次，末条 = %q", got[len(got)-1].LogName)
	}
}

// TestSaveLogs_LineCountBounded 验证总行数超过上限时同样裁剪最旧批次。
func TestSaveLogs_LineCountBounded(t *testing.T) {
	m := NewMemoryStore()
	const perReport = 1000
	reports := maxAgentLogLines/perReport + 20
	for i := 0; i < reports; i++ {
		lines := make([]proto.LogLine, perReport)
		for j := range lines {
			lines[j] = proto.LogLine{Message: "x"}
		}
		if err := m.SaveLogs("t1", &proto.LogReport{AgentID: "a", LogName: fmt.Sprintf("l-%d", i), Lines: lines}); err != nil {
			t.Fatalf("SaveLogs 失败: %v", err)
		}
	}
	got := m.AgentLogs("t1", "a", "")
	lines := 0
	for _, r := range got {
		lines += len(r.Lines)
	}
	if lines > maxAgentLogLines {
		t.Fatalf("总行数应 <= %d，实际 %d", maxAgentLogLines, lines)
	}
	if len(got) == 0 {
		t.Fatal("不应把所有批次都丢掉")
	}
	// 最新批次必须仍在。
	if got[len(got)-1].LogName != fmt.Sprintf("l-%d", reports-1) {
		t.Errorf("应保留最新批次，末条 = %q", got[len(got)-1].LogName)
	}
}

// TestSaveLogs_SingleOversizedReportKept 验证单批次自身超行数上限时仍保留该批次（不丢最新数据）。
func TestSaveLogs_SingleOversizedReportKept(t *testing.T) {
	m := NewMemoryStore()
	lines := make([]proto.LogLine, maxAgentLogLines+5000)
	if err := m.SaveLogs("t1", &proto.LogReport{AgentID: "a", LogName: "big", Lines: lines}); err != nil {
		t.Fatalf("SaveLogs 失败: %v", err)
	}
	got := m.AgentLogs("t1", "a", "")
	if len(got) != 1 || got[0].LogName != "big" {
		t.Fatalf("超大单批次应被保留，实际 %d 条", len(got))
	}
}

// TestStoreDeviceMetrics_EvictsOldest 验证设备条目超过上限时淘汰最久未更新的设备，map 不再无界增长。
func TestStoreDeviceMetrics_EvictsOldest(t *testing.T) {
	m := NewMemoryStore()
	// 先写 3 个「老」设备。
	for i := 0; i < 3; i++ {
		m.StoreDeviceMetrics(fmt.Sprintf("old-%d", i), &proto.DeviceMetrics{DeviceID: fmt.Sprintf("old-%d", i)})
	}
	// 人为把老设备的写入序号压到最小，模拟长期未上报（序号是单调计数器，比较是严格全序）。
	m.mu.Lock()
	for i := 0; i < 3; i++ {
		m.deviceMetrics[fmt.Sprintf("old-%d", i)].writeSeq = 0
	}
	m.mu.Unlock()

	// 再灌入超限设备，逐个写入（每次只会淘汰 1 个最旧条目）。
	for i := 0; i < maxTrackedDeviceMetrics+2; i++ {
		id := fmt.Sprintf("dev-%d", i)
		m.StoreDeviceMetrics(id, &proto.DeviceMetrics{DeviceID: id})
	}

	m.mu.Lock()
	n := len(m.deviceMetrics)
	_, hasOld := m.deviceMetrics["old-0"]
	_, hasNewest := m.deviceMetrics[fmt.Sprintf("dev-%d", maxTrackedDeviceMetrics+1)]
	// 淘汰顺序必须严格确定：3 个序号=0 的老条目 + 最先写入的 2 个新条目（dev-0/dev-1）被淘汰。
	// 这断言专门防「同刻写入并列 → 随 map 迭代顺序淘汰」的偶发（P1-4 曾因此出现 flake）。
	_, hasFirstNew := m.deviceMetrics["dev-0"]
	_, hasSecondNew := m.deviceMetrics["dev-1"]
	_, hasThirdNew := m.deviceMetrics["dev-2"]
	m.mu.Unlock()

	if n > maxTrackedDeviceMetrics {
		t.Fatalf("设备条目数应 <= %d，实际 %d", maxTrackedDeviceMetrics, n)
	}
	if hasOld {
		t.Error("最久未更新的设备条目应被淘汰")
	}
	if hasFirstNew || hasSecondNew {
		t.Error("先写入的新条目应被淘汰（3 老 + 2 新 = 超限 5 条）")
	}
	if !hasThirdNew {
		t.Error("dev-2 应保留（超限恰为 5 条，淘汰集合是确定的）")
	}
	if !hasNewest {
		t.Error("最新写入的设备条目必须保留")
	}
	// 最新设备的最新指标仍可查（淘汰不破坏保留条目的可读性）。
	if got := m.DeviceMetrics(fmt.Sprintf("dev-%d", maxTrackedDeviceMetrics+1)); got == nil {
		t.Error("保留条目应仍可查询最新指标")
	}
}

// TestStoreDeviceMetrics_RecentlyWrittenSurvives 验证「最近写入」可让早期创建的设备免于淘汰
// （淘汰依据是 store 侧写入序号，而非创建顺序或墙钟）。
//
// 场景设计（务必让「被保护」与「被淘汰」两组在同一轮淘汰里可比）：
//  1. 写入 veteran 并把它的写入序号压到最小（=最久未更新）；
//  2. 灌入 cap-10 个设备（未超限，不触发淘汰）；
//  3. 刷新 veteran —— 此刻它的序号比全部 d-* 都新；
//  4. 再灌 20 个设备 → 触发 11 次淘汰，被淘汰的必须是序号最小的 d-0..d-10，veteran 存活。
//
// 反例（本测试最初的写法）：先刷新 veteran 再灌满，则 veteran 本就是最旧的一批，
// 按 LRU 理应被淘汰——那种写法只能靠墙钟并列偶然通过（同刻写入不可比较），
// 在 CPU 繁忙时变成 flake。
func TestStoreDeviceMetrics_RecentlyWrittenSurvives(t *testing.T) {
	m := NewMemoryStore()
	preload := maxTrackedDeviceMetrics - 10
	m.StoreDeviceMetrics("veteran", &proto.DeviceMetrics{DeviceID: "veteran"})
	m.mu.Lock()
	m.deviceMetrics["veteran"].writeSeq = 0 // 最久未写入
	m.mu.Unlock()
	for i := 0; i < preload; i++ {
		id := fmt.Sprintf("d-%d", i)
		m.StoreDeviceMetrics(id, &proto.DeviceMetrics{DeviceID: id})
	}
	m.StoreDeviceMetrics("veteran", &proto.DeviceMetrics{DeviceID: "veteran"}) // 刷新：比全部 d-* 新
	for i := preload; i < maxTrackedDeviceMetrics+10; i++ {
		id := fmt.Sprintf("d-%d", i)
		m.StoreDeviceMetrics(id, &proto.DeviceMetrics{DeviceID: id})
	}
	m.mu.Lock()
	_, ok := m.deviceMetrics["veteran"]
	_, evicted := m.deviceMetrics["d-0"]
	n := len(m.deviceMetrics)
	m.mu.Unlock()
	if !ok {
		t.Error("刚写入过的条目不应被淘汰（其序号比全部 d-* 新）")
	}
	if evicted {
		t.Error("d-0 是最久未写入的条目，应被淘汰")
	}
	if n > maxTrackedDeviceMetrics {
		t.Errorf("设备条目数应 <= %d，实际 %d", maxTrackedDeviceMetrics, n)
	}
}

// TestAppendAgentLogBounded_CompactsBackingArray 验证长期「丢弃最旧 + 追加」后底层数组会被回收
// （容量不随追加次数无界增长）。
func TestAppendAgentLogBounded_CompactsBackingArray(t *testing.T) {
	var logs []proto.LogReport
	lines := 0
	for i := 0; i < maxAgentLogReports*5; i++ {
		logs, lines = appendAgentLogBounded(logs, lines, proto.LogReport{
			AgentID: "a", Lines: []proto.LogLine{{Message: "x"}},
		})
	}
	if len(logs) != maxAgentLogReports {
		t.Fatalf("长度应稳定在上限 %d，实际 %d", maxAgentLogReports, len(logs))
	}
	if cap(logs) > 2*maxAgentLogReports {
		t.Fatalf("底层数组容量应被压缩（<= %d），实际 %d", 2*maxAgentLogReports, cap(logs))
	}
	if lines != maxAgentLogReports {
		t.Fatalf("行数计数应与保留批次一致 = %d，实际 %d", maxAgentLogReports, lines)
	}
}
