package agent

import (
	"testing"

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
	a := &Agent{agentID: "test-agent"}
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
}

// 确保 proto 包被引用（避免未使用 import 报错，且明确测试依赖）。
var _ = proto.DeviceMetrics{}
