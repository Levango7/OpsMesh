package agent

import (
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// TestCollectMetrics_Basic 验证 CollectMetrics 能在当前主机上采集到基本指标。
// 不强断言具体数值（跨 CI 环境差异大），只校验结构完整性与字段合理性。
func TestCollectMetrics_Basic(t *testing.T) {
	m := CollectMetrics("dev-test")
	if m == nil {
		t.Fatal("CollectMetrics returned nil")
	}
	if m.DeviceID != "dev-test" {
		t.Fatalf("DeviceID = %q, want dev-test", m.DeviceID)
	}
	// Arch 必填（runtime.GOARCH）。
	if m.Arch == "" {
		t.Fatal("Arch empty, want non-empty (runtime.GOARCH)")
	}
	// CollectedAt 必填。
	if m.CollectedAt.IsZero() {
		t.Fatal("CollectedAt zero, want non-zero")
	}
	// Hostname 必填（os.Hostname 在测试机必有值）。
	if m.Hostname == "" {
		t.Fatal("Hostname empty, want non-empty")
	}
	// OS 必填（gopsutil host.Info 标准化为 windows/linux/darwin）。
	if m.OS == "" {
		t.Fatal("OS empty, want non-empty")
	}
	// CPU 核心数应 >= 1。
	if m.CPU.Cores < 1 {
		t.Fatalf("CPU.Cores = %d, want >= 1", m.CPU.Cores)
	}
	// 内存总量应 > 0。
	if m.Memory.Total == 0 {
		t.Fatal("Memory.Total = 0, want > 0")
	}
	// 进程数应 >= 1（至少自己）。
	if m.ProcessCount < 1 {
		t.Fatalf("ProcessCount = %d, want >= 1", m.ProcessCount)
	}
}

// TestCollectMetrics_NilSafe 各子采集函数在 gopsutil 调用失败时不 panic（降级返回零值）。
// 这里不模拟失败，仅验证正常路径不 panic。
func TestCollectMetrics_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CollectMetrics panicked: %v", r)
		}
	}()
	_ = CollectMetrics("")
}

// TestMonitoredServices_Whitelist 服务白名单非空且含本系统服务。
func TestMonitoredServices_Whitelist(t *testing.T) {
	if len(monitoredServices) == 0 {
		t.Fatal("monitoredServices empty")
	}
	hasOpsmesh := false
	for _, s := range monitoredServices {
		if s == "opsmesh" {
			hasOpsmesh = true
			break
		}
	}
	if !hasOpsmesh {
		t.Fatal("monitoredServices should contain 'opsmesh'")
	}
}

// TestQueryService_Nonexistent 不存在的服务应返回空 status（不 panic）。
func TestQueryService_Nonexistent(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("queryService panicked: %v", r)
		}
	}()
	status, _ := queryService("definitely-nonexistent-service-xyz")
	if status != "" {
		t.Fatalf("nonexistent service status = %q, want empty", status)
	}
}

// TestCollectDeviceMetrics_Throttle 验证 agent.collectDeviceMetrics 的 30s 节流逻辑。
// 首次调用返回非 nil，紧接的第二次返回 nil（距上次不足 30s）。
func TestCollectDeviceMetrics_Throttle(t *testing.T) {
	a := &Agent{agentID: "test-agent", metricsHistory: NewMetricsHistory(MetricsHistoryDefaultCap)}
	// 首次调用应采集（metricsLastCol 为零值，time.Since 必然 >= 30s）。
	m1 := a.collectDeviceMetrics()
	if m1 == nil {
		t.Fatal("首次采集应返回非 nil")
	}
	if m1.DeviceID != "dev-test-agent" {
		t.Fatalf("DeviceID = %q, want dev-test-agent", m1.DeviceID)
	}
	// 紧接的第二次应被节流（距上次不足 30s）。
	m2 := a.collectDeviceMetrics()
	if m2 != nil {
		t.Fatal("距上次不足 30s 应返回 nil（节流）")
	}
	// 采集成功后环形缓冲应有一条记录。
	if a.metricsHistory.Size() != 1 {
		t.Fatalf("metricsHistory.Size() = %d, want 1 after first collect", a.metricsHistory.Size())
	}
}

// 确保 proto 包被引用（避免未使用 import 报错，且明确测试依赖）。
var _ = proto.DeviceMetrics{}

// ============================================================================
// task 223 MetricsHistory 环形缓冲单元测试
// ============================================================================

// TestMetricsHistory_AddAndLatest 验证环形缓冲追加与读取最新值。
func TestMetricsHistory_AddAndLatest(t *testing.T) {
	h := NewMetricsHistory(3)
	if h.Size() != 0 {
		t.Fatalf("初始 Size = %d, want 0", h.Size())
	}
	if got := h.Latest(); got != nil {
		t.Fatalf("空缓冲 Latest = %+v, want nil", got)
	}
	// 追加 3 条。
	base := time.Now()
	for i := 0; i < 3; i++ {
		h.Add(&proto.DeviceMetrics{
			DeviceID:    "dev-1",
			CPU:         proto.CPUMetrics{Cores: i + 1, Usage: float64(i * 10)},
			CollectedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	if h.Size() != 3 {
		t.Fatalf("追加 3 条后 Size = %d, want 3", h.Size())
	}
	// 最新一条应为 Cores=3。
	latest := h.Latest()
	if latest == nil || latest.CPU.Cores != 3 {
		t.Fatalf("Latest = %+v, want Cores=3", latest)
	}
}

// TestMetricsHistory_Overwrite 验证环形缓冲满后覆写最旧（FIFO）。
func TestMetricsHistory_Overwrite(t *testing.T) {
	h := NewMetricsHistory(3)
	base := time.Now()
	for i := 0; i < 5; i++ { // 追加 5 条，容量 3，应只保留最后 3 条（i=2,3,4）。
		h.Add(&proto.DeviceMetrics{
			DeviceID:    "dev-1",
			CPU:         proto.CPUMetrics{Cores: i + 1},
			CollectedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	if h.Size() != 3 {
		t.Fatalf("覆写后 Size = %d, want 3", h.Size())
	}
	all := h.Since(time.Time{})
	if len(all) != 3 {
		t.Fatalf("Since() 返回 %d 条, want 3", len(all))
	}
	// 应为 Cores=3,4,5（升序）。
	for i, s := range all {
		want := i + 3
		if s.CPU.Cores != want {
			t.Fatalf("all[%d].CPU.Cores = %d, want %d", i, s.CPU.Cores, want)
		}
	}
}

// TestMetricsHistory_Since 验证按时间过滤历史快照。
func TestMetricsHistory_Since(t *testing.T) {
	h := NewMetricsHistory(10)
	base := time.Now()
	for i := 0; i < 5; i++ {
		h.Add(&proto.DeviceMetrics{
			DeviceID:    "dev-1",
			CollectedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	// 查询 base+2min 之后的：应有 3 条（i=2,3,4）。
	got := h.Since(base.Add(2 * time.Minute))
	if len(got) != 3 {
		t.Fatalf("Since(base+2m) 返回 %d 条, want 3", len(got))
	}
	// 查询全部：应有 5 条。
	all := h.Since(time.Time{})
	if len(all) != 5 {
		t.Fatalf("Since(zero) 返回 %d 条, want 5", len(all))
	}
	// 查询未来时间：应返回 nil。
	none := h.Since(base.Add(1 * time.Hour))
	if none != nil {
		t.Fatalf("Since(future) 返回 %d 条, want nil", len(none))
	}
}

// TestMetricsHistory_DeepCopy 验证 Add/Latest/Since 深拷贝，外部修改不污染缓冲。
func TestMetricsHistory_DeepCopy(t *testing.T) {
	h := NewMetricsHistory(5)
	m := &proto.DeviceMetrics{DeviceID: "dev-1", CPU: proto.CPUMetrics{Cores: 4}}
	h.Add(m)
	// 修改原对象不影响缓冲。
	m.CPU.Cores = 999
	latest := h.Latest()
	if latest.CPU.Cores != 4 {
		t.Fatalf("Add 深拷贝失效：Latest.Cores = %d, want 4", latest.CPU.Cores)
	}
	// 修改 Latest 返回值不影响缓冲。
	latest.CPU.Cores = 888
	if h.Latest().CPU.Cores != 4 {
		t.Fatalf("Latest 深拷贝失效：内部被外部修改 Cores=%d, want 4", h.Latest().CPU.Cores)
	}
}

// TestMetricsHistory_NilSafe 验证 nil 接收者与 nil 入参不 panic。
func TestMetricsHistory_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("MetricsHistory nil 操作 panic: %v", r)
		}
	}()
	var h *MetricsHistory
	h.Add(nil)
	h.Add(&proto.DeviceMetrics{})
	if h.Latest() != nil {
		t.Fatal("nil MetricsHistory Latest 应返回 nil")
	}
	if h.Since(time.Time{}) != nil {
		t.Fatal("nil MetricsHistory Since 应返回 nil")
	}
	if h.Size() != 0 {
		t.Fatal("nil MetricsHistory Size 应返回 0")
	}
}

// TestMetricsHistory_DefaultCap 验证 capacity<=0 时用默认容量 240。
func TestMetricsHistory_DefaultCap(t *testing.T) {
	h := NewMetricsHistory(0)
	if h.Cap() != MetricsHistoryDefaultCap {
		t.Fatalf("Cap() = %d, want %d", h.Cap(), MetricsHistoryDefaultCap)
	}
	h2 := NewMetricsHistory(-1)
	if h2.Cap() != MetricsHistoryDefaultCap {
		t.Fatalf("Cap() = %d, want %d", h2.Cap(), MetricsHistoryDefaultCap)
	}
}
