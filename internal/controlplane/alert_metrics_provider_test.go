package controlplane

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/alertengine"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// fakeMetricsSource 脚本化指标源：history 按 CollectedAt 升序返回（与 store 契约一致），
// since 为零值时返回全部。
type fakeMetricsSource struct {
	latest  *proto.DeviceMetrics
	history []proto.DeviceMetrics
}

func (f fakeMetricsSource) DeviceMetrics(string) *proto.DeviceMetrics { return f.latest }

func (f fakeMetricsSource) DeviceMetricsHistory(_ string, since time.Time) []proto.DeviceMetrics {
	if since.IsZero() {
		return f.history
	}
	out := make([]proto.DeviceMetrics, 0, len(f.history))
	for i := range f.history {
		if !f.history[i].CollectedAt.Before(since) {
			out = append(out, f.history[i])
		}
	}
	return out
}

func TestAlertMetricsProvider_GaugeInstant(t *testing.T) {
	p := deviceMetricsProvider{src: fakeMetricsSource{latest: &proto.DeviceMetrics{
		CPU:    proto.CPUMetrics{Usage: 91.5},
		Memory: proto.MemMetrics{Usage: 62},
	}}}
	if v, err := p.Query("cpu_usage", "dev-1", 0); err != nil || v != 91.5 {
		t.Fatalf("cpu_usage 即时值: got (%v, %v), want (91.5, nil)", v, err)
	}
	if v, err := p.Query("mem_usage", "dev-1", -1*time.Second); err != nil || v != 62 {
		t.Fatalf("mem_usage 即时值（window<0）: got (%v, %v), want (62, nil)", v, err)
	}
}

func TestAlertMetricsProvider_NoData(t *testing.T) {
	p := deviceMetricsProvider{src: fakeMetricsSource{}}
	// 从不上报设备：瞬时与窗口两条路径都必须 ErrMetricUnavailable。
	for _, w := range []time.Duration{0, time.Minute} {
		if _, err := p.Query("cpu_usage", "ghost", w); err != alertengine.ErrMetricUnavailable {
			t.Fatalf("无数据应 ErrMetricUnavailable, window=%v: got %v", w, err)
		}
	}
}

func TestAlertMetricsProvider_GaugeWindowAverage(t *testing.T) {
	now := time.Now()
	// 窗口 2m：4 个样本 50/60/70/80（均值 65），另留一个 5 分钟前的样本应被排除。
	p := deviceMetricsProvider{src: fakeMetricsSource{history: []proto.DeviceMetrics{
		{CollectedAt: now.Add(-5 * time.Minute), CPU: proto.CPUMetrics{Usage: 999}},
		{CollectedAt: now.Add(-90 * time.Second), CPU: proto.CPUMetrics{Usage: 50}},
		{CollectedAt: now.Add(-60 * time.Second), CPU: proto.CPUMetrics{Usage: 60}},
		{CollectedAt: now.Add(-30 * time.Second), CPU: proto.CPUMetrics{Usage: 70}},
		{CollectedAt: now.Add(-10 * time.Second), CPU: proto.CPUMetrics{Usage: 80}},
	}}}
	v, err := p.Query("cpu_usage", "dev-1", 2*time.Minute)
	if err != nil {
		t.Fatalf("窗口平均出错: %v", err)
	}
	if v < 64.9 || v > 65.1 {
		t.Fatalf("窗口平均应约 65（且不含 5 分钟前的 999）: got %v", v)
	}
}

func TestAlertMetricsProvider_DiskUsageTakesMax(t *testing.T) {
	p := deviceMetricsProvider{src: fakeMetricsSource{latest: &proto.DeviceMetrics{
		Disks: []proto.DiskMetrics{
			{Mount: "/", Usage: 50},
			{Mount: "/data", Usage: 99},
			{Mount: "/backup", Usage: 61},
		},
	}}}
	if v, err := p.Query("disk_usage", "dev-1", 0); err != nil || v != 99 {
		t.Fatalf("disk_usage 应取最大值 99: got (%v, %v)", v, err)
	}
	// 一台没有磁盘信息的设备 ⇒ 无数据，而不是 0（0 会让「磁盘使用率 < 阈值」类规则恒真）。
	empty := deviceMetricsProvider{src: fakeMetricsSource{latest: &proto.DeviceMetrics{}}}
	if _, err := empty.Query("disk_usage", "dev-1", 0); err != alertengine.ErrMetricUnavailable {
		t.Fatalf("无磁盘应 ErrMetricUnavailable: got %v", err)
	}
}

func TestAlertMetricsProvider_NetRateDifferential(t *testing.T) {
	now := time.Now()
	p := deviceMetricsProvider{src: fakeMetricsSource{history: []proto.DeviceMetrics{
		{CollectedAt: now.Add(-20 * time.Second), Network: []proto.NetMetrics{{Name: "eth0", Status: "up", RxBytes: 1000}, {Name: "lo", Status: "down", RxBytes: 10}}},
		{CollectedAt: now.Add(-10 * time.Second), Network: []proto.NetMetrics{{Name: "eth0", Status: "up", RxBytes: 6000}, {Name: "lo", Status: "down", RxBytes: 10}}},
	}}}
	// 30s 窗口内首尾两点差分：(6000-1000)/10s = 500 B/s；down 的网卡不计入。
	v, err := p.Query("net_in", "dev-1", 30*time.Second)
	if err != nil {
		t.Fatalf("net_in 差速出错: %v", err)
	}
	if v < 499.9 || v > 500.1 {
		t.Fatalf("net_in 应 500 B/s: got %v", v)
	}
	// window<=0 无法差分 ⇒ 无数据（不能把累计值当阈值比）。
	if _, err := p.Query("net_in", "dev-1", 0); err != alertengine.ErrMetricUnavailable {
		t.Fatalf("窗口为 0 的速率指标应 ErrMetricUnavailable: got %v", err)
	}
	// 单点无速率。
	one := deviceMetricsProvider{src: fakeMetricsSource{history: []proto.DeviceMetrics{
		{CollectedAt: now, Network: []proto.NetMetrics{{Status: "up", RxBytes: 10}}}}}}
	if _, err := one.Query("net_in", "dev-1", time.Minute); err != alertengine.ErrMetricUnavailable {
		t.Fatalf("单样本速率应 ErrMetricUnavailable: got %v", err)
	}
	// 计数器回绕（agent 重启）⇒ 差分为负 ⇒ 视同无数据，不用绝对值伪装。
	reset := deviceMetricsProvider{src: fakeMetricsSource{history: []proto.DeviceMetrics{
		{CollectedAt: now.Add(-20 * time.Second), Network: []proto.NetMetrics{{Status: "up", RxBytes: 9_000_000}}},
		{CollectedAt: now.Add(-10 * time.Second), Network: []proto.NetMetrics{{Status: "up", RxBytes: 500}}},
	}}}
	if _, err := reset.Query("net_in", "dev-1", time.Minute); err != alertengine.ErrMetricUnavailable {
		t.Fatalf("计数器回绕应 ErrMetricUnavailable: got %v", err)
	}
}

func TestAlertMetricsProvider_UnknownMetric(t *testing.T) {
	p := deviceMetricsProvider{src: fakeMetricsSource{latest: &proto.DeviceMetrics{CPU: proto.CPUMetrics{Usage: 95}}}}
	if _, err := p.Query("no_such_metric", "dev-1", 0); err != alertengine.ErrMetricUnavailable {
		t.Fatalf("未登记指标名应 ErrMetricUnavailable（不是 error，否则整条规则评估被跳过）: got %v", err)
	}
}

// —— 接线钉子：规则引擎经真 memory store 触发告警（此前恒不可能触发） ——

func TestNewAlertEngineFiresOnDeviceMetrics(t *testing.T) {
	st := store.NewMemoryStore()
	st.StoreDeviceMetrics("dev-1", &proto.DeviceMetrics{DeviceID: "dev-1", CPU: proto.CPUMetrics{Usage: 95}})
	eng := newAlertEngine(st)
	if err := eng.AddRule(&alertengine.AlertRule{
		ID:         "r-cpu",
		TenantID:   "tenant-a",
		Name:       "cpu 高",
		Enabled:    true,
		Logic:      alertengine.LogicAnd,
		Severity:   alertengine.SeverityCritical,
		Conditions: []alertengine.Condition{{Metric: "cpu_usage", Operator: alertengine.OpGT, Threshold: 90}},
	}); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	events, err := eng.Evaluate("dev-1")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("cpu 95%% 应触发 1 条告警: got %d 条", len(events))
	}
	if events[0].Values["cpu_usage"] != 95 {
		t.Fatalf("告警事件应带实际值 cpu_usage=95: got %v", events[0].Values)
	}
	// 从不上报设备：不触发。
	events2, _ := eng.Evaluate("ghost-dev")
	if len(events2) != 0 {
		t.Fatalf("无数据设备不应触发: got %d 条", len(events2))
	}
}

func TestNewAlertEngineFiresOnNetRateRule(t *testing.T) {
	st := store.NewMemoryStore()
	now := time.Now()
	st.StoreDeviceMetrics("dev-2", &proto.DeviceMetrics{
		DeviceID: "dev-2", CollectedAt: now.Add(-20 * time.Second),
		Network: []proto.NetMetrics{{Name: "eth0", Status: "up", RxBytes: 1_000_000}},
	})
	st.StoreDeviceMetrics("dev-2", &proto.DeviceMetrics{
		DeviceID: "dev-2", CollectedAt: now.Add(-10 * time.Second),
		Network: []proto.NetMetrics{{Name: "eth0", Status: "up", RxBytes: 6_000_000}},
	})
	eng := newAlertEngine(st)
	if err := eng.AddRule(&alertengine.AlertRule{
		ID:       "r-net",
		TenantID: "tenant-a",
		Name:     "入向流量",
		Enabled:  true,
		Logic:    alertengine.LogicAnd,
		Conditions: []alertengine.Condition{
			{Metric: "net_in", Operator: alertengine.OpGT, Threshold: 100_000, Window: 60 * time.Second},
		},
	}); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	events, err := eng.Evaluate("dev-2")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("入向 500000 B/s 应触发 1 条告警: got %d 条", len(events))
	}
}

// —— 变异电池：把 provider 退回 nil（Noop）⇒ 上面两条接线钉子必须转红 ——

func TestNoopProviderWouldNotFire(t *testing.T) {
	st := store.NewMemoryStore()
	st.StoreDeviceMetrics("dev-1", &proto.DeviceMetrics{DeviceID: "dev-1", CPU: proto.CPUMetrics{Usage: 95}})
	// 形态与接线前逐字相同（NewEngine(nil, nil, nil)）——证明钉子绑的是「接不接线」，而非判据恒真。
	noop := alertengine.NewEngine(nil, nil, nil)
	if err := noop.AddRule(&alertengine.AlertRule{
		ID:         "r-cpu",
		TenantID:   "tenant-a",
		Enabled:    true,
		Logic:      alertengine.LogicAnd,
		Conditions: []alertengine.Condition{{Metric: "cpu_usage", Operator: alertengine.OpGT, Threshold: 90}},
	}); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	events, _ := noop.Evaluate("dev-1")
	if len(events) != 0 {
		t.Fatalf("Noop 形态下不应触发（供变异对照）: got %d 条", len(events))
	}
}
