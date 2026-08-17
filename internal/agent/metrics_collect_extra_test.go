// metrics_collect_extra_test.go 补充 metrics_collect.go 中未覆盖的辅助函数单元测试。
//
// 覆盖：
//   - isLoopback / isFlagUp / firstIPv4 各分支
//   - queryServiceWindows / queryServiceLinux 失败路径
//   - collectHost / collectCPU / collectMem / collectDisks / collectNet / collectServices / collectProcessCount
//   - MetricsHistory.Cap / Since 边界
package agent

import (
	"testing"
	"time"

	gnet "github.com/shirou/gopsutil/v3/net"

	"opsmesh/internal/proto"
)

// --- isLoopback ---

func TestIsLoopback_True(t *testing.T) {
	iface := gnet.InterfaceStat{Flags: []string{"up", "loopback", "running"}}
	if !isLoopback(iface) {
		t.Fatal("含 loopback flag 应返回 true")
	}
}

func TestIsLoopback_False(t *testing.T) {
	iface := gnet.InterfaceStat{Flags: []string{"up", "running"}}
	if isLoopback(iface) {
		t.Fatal("不含 loopback flag 应返回 false")
	}
}

func TestIsLoopback_EmptyFlags(t *testing.T) {
	iface := gnet.InterfaceStat{Flags: nil}
	if isLoopback(iface) {
		t.Fatal("空 flags 应返回 false")
	}
}

func TestIsLoopback_CaseInsensitive(t *testing.T) {
	iface := gnet.InterfaceStat{Flags: []string{"LOOPBACK"}}
	if !isLoopback(iface) {
		t.Fatal("LOOPBACK 大写应匹配（大小写不敏感）")
	}
}

// --- isFlagUp ---

func TestIsFlagUp_True(t *testing.T) {
	iface := gnet.InterfaceStat{Flags: []string{"up", "broadcast"}}
	if !isFlagUp(iface) {
		t.Fatal("含 up flag 应返回 true")
	}
}

func TestIsFlagUp_False(t *testing.T) {
	iface := gnet.InterfaceStat{Flags: []string{"down", "broadcast"}}
	if isFlagUp(iface) {
		t.Fatal("不含 up flag 应返回 false")
	}
}

func TestIsFlagUp_EmptyFlags(t *testing.T) {
	iface := gnet.InterfaceStat{Flags: nil}
	if isFlagUp(iface) {
		t.Fatal("空 flags 应返回 false")
	}
}

// --- firstIPv4 ---

func TestFirstIPv4_WithCIDR(t *testing.T) {
	iface := gnet.InterfaceStat{
		Addrs: []gnet.InterfaceAddr{
			{Addr: "192.168.1.10/24"},
		},
	}
	if got := firstIPv4(iface); got != "192.168.1.10" {
		t.Fatalf("firstIPv4 = %q, want 192.168.1.10", got)
	}
}

func TestFirstIPv4_NoIPv4(t *testing.T) {
	iface := gnet.InterfaceStat{
		Addrs: []gnet.InterfaceAddr{
			{Addr: "::1/128"},
			{Addr: "fe80::1/64"},
		},
	}
	if got := firstIPv4(iface); got != "" {
		t.Fatalf("无 IPv4 应返回空串，得到 %q", got)
	}
}

func TestFirstIPv4_MultipleAddrs(t *testing.T) {
	iface := gnet.InterfaceStat{
		Addrs: []gnet.InterfaceAddr{
			{Addr: "::1/128"},
			{Addr: "10.0.0.5/8"},
			{Addr: "192.168.1.1/24"},
		},
	}
	if got := firstIPv4(iface); got != "10.0.0.5" {
		t.Fatalf("应返回第一个 IPv4，得到 %q", got)
	}
}

func TestFirstIPv4_EmptyAddrs(t *testing.T) {
	iface := gnet.InterfaceStat{Addrs: nil}
	if got := firstIPv4(iface); got != "" {
		t.Fatalf("空 Addrs 应返回空串，得到 %q", got)
	}
}

func TestFirstIPv4_PlainIPv4(t *testing.T) {
	iface := gnet.InterfaceStat{
		Addrs: []gnet.InterfaceAddr{
			{Addr: "172.16.0.1"},
		},
	}
	if got := firstIPv4(iface); got != "172.16.0.1" {
		t.Fatalf("无 CIDR 的 IPv4 应返回，得到 %q", got)
	}
}

// --- queryServiceWindows / queryServiceLinux ---

func TestQueryServiceWindows_Nonexistent(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("queryServiceWindows panic: %v", r)
		}
	}()
	status, _ := queryServiceWindows("definitely-nonexistent-service-xyz")
	// 不存在的服务应返回空 status（sc query 失败）
	if status == "running" {
		t.Fatal("不存在的服务不应返回 running")
	}
}

func TestQueryServiceLinux_Nonexistent(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("queryServiceLinux panic: %v", r)
		}
	}()
	status, _ := queryServiceLinux("definitely-nonexistent-service-xyz")
	if status == "running" {
		t.Fatal("不存在的服务不应返回 running")
	}
}

// --- collectHost / collectCPU / collectMem / collectDisks / collectNet / collectServices / collectProcessCount ---

func TestCollectHost_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("collectHost panic: %v", r)
		}
	}()
	m := &proto.DeviceMetrics{}
	collectHost(m)
	// 不强断言具体值（跨平台差异），只验证不 panic
}

func TestCollectCPU_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("collectCPU panic: %v", r)
		}
	}()
	m := &proto.DeviceMetrics{}
	collectCPU(m)
	if m.CPU.Cores < 0 {
		t.Fatalf("CPU.Cores 不应为负，得到 %d", m.CPU.Cores)
	}
}

func TestCollectMem_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("collectMem panic: %v", r)
		}
	}()
	m := &proto.DeviceMetrics{}
	collectMem(m)
}

func TestCollectDisks_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("collectDisks panic: %v", r)
		}
	}()
	var disks []proto.DiskMetrics
	collectDisks(&disks)
}

func TestCollectNet_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("collectNet panic: %v", r)
		}
	}()
	m := &proto.DeviceMetrics{}
	collectNet(m)
}

func TestCollectServices_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("collectServices panic: %v", r)
		}
	}()
	m := &proto.DeviceMetrics{}
	collectServices(m)
}

func TestCollectProcessCount_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("collectProcessCount panic: %v", r)
		}
	}()
	m := &proto.DeviceMetrics{}
	collectProcessCount(m)
	if m.ProcessCount < 0 {
		t.Fatalf("ProcessCount 不应为负，得到 %d", m.ProcessCount)
	}
}

// --- MetricsHistory.Cap nil 接收者 ---

func TestMetricsHistory_Cap_Nil(t *testing.T) {
	var h *MetricsHistory
	if h.Cap() != 0 {
		t.Fatal("nil MetricsHistory Cap 应返回 0")
	}
}

func TestMetricsHistory_Cap_Normal(t *testing.T) {
	h := NewMetricsHistory(50)
	if h.Cap() != 50 {
		t.Fatalf("Cap = %d, want 50", h.Cap())
	}
}

// --- MetricsHistory.Since 边界 ---

func TestMetricsHistory_Since_Empty(t *testing.T) {
	h := NewMetricsHistory(10)
	if got := h.Since(time.Time{}); got != nil {
		t.Fatalf("空缓冲 Since 应返回 nil，得到 %v", got)
	}
}

func TestMetricsHistory_Latest_Empty(t *testing.T) {
	h := NewMetricsHistory(10)
	if got := h.Latest(); got != nil {
		t.Fatalf("空缓冲 Latest 应返回 nil，得到 %v", got)
	}
}

// --- CollectMetrics 完整性补充 ---

func TestCollectMetrics_DisksAndNetwork(t *testing.T) {
	m := CollectMetrics("dev-full")
	if m == nil {
		t.Fatal("CollectMetrics returned nil")
	}
	// Disks 与 Network 字段应被填充（具体数量跨平台差异大，不强断言）
	// 只验证不 panic 且结构完整
	_ = m.Disks
	_ = m.Network
	_ = m.Services
}

// --- queryService 分发 ---

func TestQueryService_Dispatch(t *testing.T) {
	// 验证 queryService 不 panic（跨平台分发到 queryServiceWindows/queryServiceLinux）
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("queryService panic: %v", r)
		}
	}()
	status, enabled := queryService("opsmesh")
	_ = status
	_ = enabled
}
