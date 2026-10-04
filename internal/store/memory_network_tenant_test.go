package store

// memory_network_tenant_test.go 钉住网络指标聚合的租户隔离口径（2026-10-04 补）。
//
// 为什么要单独测：SQL 后端这条聚合是 `WHERE tenant_id=? AND timestamp>=?`
// （sql_network.go:QueryNetworkMetrics），内存后端原先只看"设备登记信息"的 tenantID，
// 设备未登记时样本自带租户被忽略 ⇒ 同一样本会进**所有**租户的均值桶。
// 消费方是 canary 自动评估（controlplane/canary_enhance.go:QueryNetworkMetrics）与
// SLIStatus，跨租户串数据意味着 B 租户的负载会影响 A 租户的发布/回滚判定。

import (
	"testing"
	"time"
)

func TestMemoryStore_QueryNetworkMetrics_TenantIsolation(t *testing.T) {
	m := NewMemoryStore()
	now := time.Now()

	// 两个**未登记设备**各自上报一条样本，租户不同。
	// 设备未登记正是原实现过滤失效的那条路径（只查 networkDevices 就放行）。
	m.StoreNetworkMetrics("dev-a", &NetworkMetrics{TenantID: "t-a", CPUUsage: 0.2, MemoryUsage: 0.3, Temperature: 40, Timestamp: now})
	m.StoreNetworkMetrics("dev-b", &NetworkMetrics{TenantID: "t-b", CPUUsage: 0.8, MemoryUsage: 0.9, Temperature: 60, Timestamp: now})

	agg := m.QueryNetworkMetrics("t-a", now.Add(-time.Minute))
	if got := agg["cpu_usage"]; got < 0.19 || got > 0.21 {
		t.Fatalf("t-a 的 cpu_usage 均值 = %v，期望只含自己的样本 0.2（串到 t-b 就是隔离失效）", got)
	}
	if got := agg["memory_usage"]; got < 0.29 || got > 0.31 {
		t.Fatalf("t-a 的 memory_usage 均值 = %v，期望 0.3", got)
	}
	if got := agg["temperature"]; got < 39.9 || got > 40.1 {
		t.Fatalf("t-a 的 temperature 均值 = %v，期望 40", got)
	}

	// 反向：t-b 也不能看到 t-a 的样本。
	if got := m.QueryNetworkMetrics("t-b", now.Add(-time.Minute))["cpu_usage"]; got < 0.79 || got > 0.81 {
		t.Fatalf("t-b 的 cpu_usage 均值 = %v，期望 0.8", got)
	}

	// 时间窗口必须生效：窗口起点晚于样本 ⇒ 无数据（返回 0，与 SQL 后端同一口径）。
	if got := m.QueryNetworkMetrics("t-a", now.Add(time.Minute))["cpu_usage"]; got != 0 {
		t.Fatalf("窗口外仍返回 %v，期望 0", got)
	}
}

func TestMemoryStore_SLIStatus_TenantIsolation(t *testing.T) {
	m := NewMemoryStore()
	now := time.Now()

	slo := m.CreateSLO("t-a", &SLO{Name: "slo-a", Target: 99.9, Window: "7d",
		SLIs: []SLI{{Name: "cpu-low", Metric: "cpu_usage", Target: 0.5, Operator: "<"}}})

	// 只有别的租户有样本：本租户必须是 nodata，不能拿别人的观测值判自己的 SLO。
	m.StoreNetworkMetrics("dev-b", &NetworkMetrics{TenantID: "t-b", CPUUsage: 0.1, Timestamp: now})
	if got := sliStatusValues(m.SLIStatus("t-a", slo.ID)); len(got) != 1 || got[0].Status != "nodata" {
		t.Fatalf("他租户有样本、本租户无样本时 = %+v，期望 nodata", got)
	}

	// 本租户样本进来后结论翻成 met：证明判定用的是自己的观测值。
	m.StoreNetworkMetrics("dev-a", &NetworkMetrics{TenantID: "t-a", CPUUsage: 0.1, Timestamp: now})
	if got := sliStatusValues(m.SLIStatus("t-a", slo.ID)); len(got) != 1 || got[0].Status != "met" {
		t.Fatalf("本租户 0.1 对 \"< 0.5\" 应为 met，实际 %+v", got)
	}
}
