// bench_m4_test.go 性能基准测试：MemoryStore 关键路径并发读写性能。
//
// 覆盖：
//   - BenchmarkMemoryStore_UpsertDevice    并发 UpsertDevice（写写并发）
//   - BenchmarkMemoryStore_ListDevices     设备列表查询性能（100/1000/10000 设备）
//   - BenchmarkMemoryStore_CreateTask      并发创建任务
//   - BenchmarkMemoryStore_DeviceMetrics   指标写入+查询性能
//   - BenchmarkMemoryStore_AlertRule       告警规则 CRUD 性能
//
// 设计要点：
//   - 用 testing.B 而非 testing.T；用 b.RunParallel 测并发；
//   - 用 b.ResetTimer() 排除初始化时间；
//   - 用 b.Run 子基准测试做参数化（不同数据规模）。
package store

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// makeDeviceInfo 构造一个填充完整的 DeviceInfo（避免空字段导致某些路径早退）。
func makeDeviceInfo(idx int) *proto.DeviceInfo {
	return &proto.DeviceInfo{
		DeviceID:  fmt.Sprintf("dev-bench-%d", idx),
		Segment:   "seg-bench",
		TenantID:  "t1",
		IP:        fmt.Sprintf("10.30.0.%d", idx%250+1),
		AgentID:   fmt.Sprintf("agent-bench-%d", idx),
		State:     "online",
		TaskState: "idle",
		Managed:   true,
		Hostname:  fmt.Sprintf("host-%d", idx),
		OS:        "linux",
		Arch:      "amd64",
	}
}

// makeMetrics 构造一个填充完整的 DeviceMetrics。
func makeMetrics(deviceID string, idx int) *proto.DeviceMetrics {
	return &proto.DeviceMetrics{
		DeviceID:    deviceID,
		Hostname:    "host-bench",
		OS:          "linux",
		Arch:        "amd64",
		Uptime:      int64(idx),
		CPU:         proto.CPUMetrics{Cores: 4, Usage: float64(idx % 100), Model: "Intel Xeon"},
		Memory:      proto.MemMetrics{Total: 8192, Used: 2048, Available: 6144, Usage: 25.0},
		CollectedAt: time.Now(),
	}
}

// BenchmarkMemoryStore_UpsertDevice 并发 UpsertDevice 性能。
// 模拟多 agent 并发上报设备状态：每个 goroutine 独立 deviceID，写写并发。
func BenchmarkMemoryStore_UpsertDevice(b *testing.B) {
	m := NewMemoryStore()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		// 每个并发 worker 用独立计数器避免冲突
		var i int64
		for pb.Next() {
			idx := atomic.AddInt64(&i, 1)
			m.UpsertDevice(makeDeviceInfo(int(idx)))
		}
	})
}

// BenchmarkMemoryStore_ListDevices 设备列表查询性能（100/1000/10000 设备）。
// 用 Snapshot("") 全量查询，验证不同规模下的查询吞吐。
func BenchmarkMemoryStore_ListDevices(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("devices=%d", n), func(b *testing.B) {
			m := NewMemoryStore()
			for i := 0; i < n; i++ {
				m.UpsertDevice(makeDeviceInfo(i))
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = m.Snapshot("")
			}
		})
	}
}

// BenchmarkMemoryStore_ListDevices_Concurrent 并发 Snapshot 查询性能（10000 设备，多 goroutine 并发读）。
// 验证 RWMutex 读锁并发不阻塞。
func BenchmarkMemoryStore_ListDevices_Concurrent(b *testing.B) {
	m := NewMemoryStore()
	for i := 0; i < 10000; i++ {
		m.UpsertDevice(makeDeviceInfo(i))
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = m.Snapshot("")
		}
	})
}

// BenchmarkMemoryStore_CreateTask 并发创建任务性能。
// 模拟多控制面副本/多 API 请求并发下发任务到同一 agent。
func BenchmarkMemoryStore_CreateTask(b *testing.B) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.CreateTask(&proto.Task{
				AgentID:  a.AgentID,
				TenantID: "t1",
				Type:     "shell",
				Command:  "echo bench",
			})
		}
	})
}

// BenchmarkMemoryStore_DeviceMetrics 指标写入+查询性能。
// 每次迭代：写入一条指标 + 查询最新值，模拟 agent 心跳上报 + 控制面读路径。
func BenchmarkMemoryStore_DeviceMetrics(b *testing.B) {
	m := NewMemoryStore()
	const deviceID = "dev-metrics-bench"
	// 预创建设备
	m.UpsertDevice(&proto.DeviceInfo{
		DeviceID: deviceID, Segment: "seg-a", TenantID: "t1",
		IP: "10.0.0.1", State: "online", Managed: true,
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.StoreDeviceMetrics(deviceID, makeMetrics(deviceID, i))
		_ = m.DeviceMetrics(deviceID)
	}
}

// BenchmarkMemoryStore_DeviceMetrics_Concurrent 并发指标写入+查询性能。
// 多 agent 并发上报指标 + 多读请求并发查询，验证 metricsRing 在 m.mu 保护下的并发安全。
func BenchmarkMemoryStore_DeviceMetrics_Concurrent(b *testing.B) {
	m := NewMemoryStore()
	// 预创建 100 个设备
	const numDevices = 100
	for i := 0; i < numDevices; i++ {
		id := fmt.Sprintf("dev-concurrent-%d", i)
		m.UpsertDevice(&proto.DeviceInfo{
			DeviceID: id, Segment: "seg-a", TenantID: "t1",
			IP: "10.0.0.1", State: "online", Managed: true,
		})
	}
	var counter int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := atomic.AddInt64(&counter, 1)
			id := fmt.Sprintf("dev-concurrent-%d", idx%numDevices)
			m.StoreDeviceMetrics(id, makeMetrics(id, int(idx)))
			_ = m.DeviceMetrics(id)
		}
	})
}

// BenchmarkMemoryStore_AlertRule 告警规则 CRUD 性能。
// 每次迭代：Create + List + Delete 一条规则，覆盖完整 CRUD 路径。
func BenchmarkMemoryStore_AlertRule(b *testing.B) {
	m := NewMemoryStore()
	var counter int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = atomic.AddInt64(&counter, 1)
			rule := &AlertRule{
				TenantID:  "t1",
				Metric:    "cpu.usage",
				Op:        ">",
				Threshold: 80.0,
				Severity:  "warning",
				Message:   "cpu high",
				Enabled:   true,
			}
			created := m.CreateAlertRule(rule)
			_ = m.ListAlertRules("t1")
			m.DeleteAlertRule(created.ID)
		}
	})
}

// BenchmarkMemoryStore_AlertRule_List 告警规则列表查询性能（不同规则规模）。
// 验证 ListAlertRules 在大量规则下的查询吞吐（含排序）。
func BenchmarkMemoryStore_AlertRule_List(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("rules=%d", n), func(b *testing.B) {
			m := NewMemoryStore()
			for i := 0; i < n; i++ {
				m.CreateAlertRule(&AlertRule{
					TenantID:  "t1",
					Metric:    "cpu.usage",
					Op:        ">",
					Threshold: 80.0,
					Severity:  "warning",
					Message:   "cpu high",
					Enabled:   true,
				})
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = m.ListAlertRules("t1")
			}
		})
	}
}

// BenchmarkMemoryStore_Snapshot_TenantFilter 租户过滤查询性能。
// 多租户场景下 Snapshot(tenantID) 按 tenantID 过滤的性能（验证不退化到全量扫描后过滤）。
func BenchmarkMemoryStore_Snapshot_TenantFilter(b *testing.B) {
	m := NewMemoryStore()
	// 10 个租户，每租户 1000 设备，共 10000 设备
	const numTenants = 10
	const devicesPerTenant = 1000
	for t := 0; t < numTenants; t++ {
		tenant := fmt.Sprintf("tenant-%d", t)
		for i := 0; i < devicesPerTenant; i++ {
			m.UpsertDevice(&proto.DeviceInfo{
				DeviceID: fmt.Sprintf("dev-t%d-%d", t, i),
				Segment:  "seg-a",
				TenantID: tenant,
				IP:       "10.0.0.1",
				State:    "online",
				Managed:  true,
			})
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Snapshot("tenant-5")
	}
}
