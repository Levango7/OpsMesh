package controlplane

import (
	"time"

	"github.com/Levango7/OpsMesh/internal/alertengine"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// deviceMetricsProvider 基于 store 的设备指标实现 alertengine.MetricsProvider（#101）。
//
// 背景：告警规则引擎此前被注入 nil（server.go 的 NewEngine(nil, nil, nil)）⇒ 内部降级为
// NoopMetricsProvider ⇒ Query 恒返回 ErrMetricUnavailable ⇒ MatchRule 把每个条件都判 false
// ⇒ **任何告警规则都不可能触发**。规则 CRUD、评估循环（alertEngineLoop 逐设备 Evaluate）、
// 聚合/静默/通知 downstream 全都是好的，唯独「取指标」这一步是空的——这也是它此前一直被
// 当成「告警测了不响」而查不出原因的原因。
//
// 指标名与 agent 上报结构的对应（Condition.Metric 注释登记的那五个）：
//
//	cpu_usage  → CPU.Usage（0-100）
//	mem_usage  → Memory.Usage（0-100）
//	disk_usage → 全部 Disks[].Usage 的**最大值**（任一分区打满就该告警；取平均会把
//	             单个满盘稀释掉，恰好掩盖最该告警的那台）
//	net_in     → Network 中 Status=up 的网卡 RxBytes **差分化速率**（字节/秒）
//	net_out    → 同上，TxBytes
//
// 聚合口径遵循 MetricsProvider 契约：window>0 取窗口内**平均值**，window<=0 取即时值。
// 无数据（该设备从未上报 / 窗口内无样本 / 计数器无法差分 / 指标名未登记）统一返回
// ErrMetricUnavailable，引擎据此把该条件视为 false 并继续评估其他条件——与
// 「该设备暂无该指标数据」的正常缺失语义一致，不会让单条规则拖垮整轮评估。
type deviceMetricsProvider struct {
	src metricsSource
}

// metricsSource 只依赖指标查询面。
//
// 刻意用窄接口而非整个 store.Store：单测可注入假实现（脚本化历史/计数器回绕），
// 也表明本 provider 只读这两个方法、不依赖任何别的领域面。
type metricsSource interface {
	DeviceMetrics(deviceID string) *proto.DeviceMetrics
	DeviceMetricsHistory(deviceID string, since time.Time) []proto.DeviceMetrics
}

// newAlertEngine 构造已接入设备指标的告警规则引擎，替换此前的 NewEngine(nil, nil, nil)。
//
// evaluator/now 仍传 nil：前者用默认 60 样本持续时长评估器，后者用 time.Now，
// 与接线前逐字一致——本函数只补上原本缺失的 MetricsProvider。
func newAlertEngine(st store.Store) *alertengine.Engine {
	return alertengine.NewEngine(deviceMetricsProvider{src: st}, nil, nil)
}

// Query 返回 (deviceID, metric) 在 window 内的聚合值。契约见 alertengine.MetricsProvider。
func (p deviceMetricsProvider) Query(metric string, deviceID string, window time.Duration) (float64, error) {
	if extract, ok := metricExtractors[metric]; ok {
		return p.queryGauge(extract, deviceID, window)
	}
	if rateOf, ok := metricRates[metric]; ok {
		return p.queryRate(rateOf, deviceID, window)
	}
	// 未登记的指标名：视同「无数据」，条件判 false。不返回 error——
	// 那会让整条规则评估失败（MatchRule 直接返回 error、Evaluate 跳过该规则），
	// 把「名字打错的规则」放大成「整轮评估缺一条」。
	return 0, alertengine.ErrMetricUnavailable
}

// metricExtractor 从**单个**指标快照里提取瞬时值；ok=false 表示该样本不含此指标
// （如设备没挂磁盘），该样本在窗口平均里被跳过，不算「无数据」。
type metricExtractor func(m *proto.DeviceMetrics) (float64, bool)

var metricExtractors = map[string]metricExtractor{
	"cpu_usage": func(m *proto.DeviceMetrics) (float64, bool) { return m.CPU.Usage, true },
	"mem_usage": func(m *proto.DeviceMetrics) (float64, bool) { return m.Memory.Usage, true },
	// 任一分区使用率最高者——不是平均。平均 80% 可能掩盖一块 99% 的盘。
	"disk_usage": func(m *proto.DeviceMetrics) (float64, bool) {
		var (
			max  float64
			some bool
		)
		for _, d := range m.Disks {
			if !some || d.Usage > max {
				max, some = d.Usage, true
			}
		}
		return max, some
	},
}

// queryGauge 窗口内求平均、窗口外取即时值。
func (p deviceMetricsProvider) queryGauge(extract metricExtractor, deviceID string, window time.Duration) (float64, error) {
	if window <= 0 {
		latest := p.src.DeviceMetrics(deviceID)
		if latest == nil {
			return 0, alertengine.ErrMetricUnavailable
		}
		v, ok := extract(latest)
		if !ok {
			return 0, alertengine.ErrMetricUnavailable
		}
		return v, nil
	}
	// 窗口内平均；全部样本都缺该指标（如整窗无磁盘数据）才算无数据。
	samples := p.src.DeviceMetricsHistory(deviceID, time.Now().Add(-window))
	var (
		sum   float64
		count int
	)
	for i := range samples {
		if v, ok := extract(&samples[i]); ok {
			sum += v
			count++
		}
	}
	if count == 0 {
		return 0, alertengine.ErrMetricUnavailable
	}
	return sum / float64(count), nil
}

// metricRateFn 从单个快照取累计计数器（字节）。
type metricRateFn func(m *proto.DeviceMetrics) uint64

var metricRates = map[string]metricRateFn{
	"net_in": func(m *proto.DeviceMetrics) uint64 {
		var total uint64
		for _, n := range m.Network {
			if n.Status == "up" {
				total += n.RxBytes
			}
		}
		return total
	},
	"net_out": func(m *proto.DeviceMetrics) uint64 {
		var total uint64
		for _, n := range m.Network {
			if n.Status == "up" {
				total += n.TxBytes
			}
		}
		return total
	},
}

// queryRate 把累计计数器差分化成字节/秒。
//
// net_in/net_out 的语义是「速率」，而 agent 上报的 RxBytes/TxBytes 是开机以来累计值
// （agent/metrics_collect.go 直接取 gopsutil 的 IOCountersStat.BytesRecv）——
// 把累计值当阈值比，等于让规则在「累计量跨过阈值」后永久触发，那不是流量告警。
// 所以必须窗口内差分；window<=0（无法差分）一律 ErrMetricUnavailable。
func (p deviceMetricsProvider) queryRate(rateOf metricRateFn, deviceID string, window time.Duration) (float64, error) {
	if window <= 0 {
		return 0, alertengine.ErrMetricUnavailable
	}
	samples := p.src.DeviceMetricsHistory(deviceID, time.Now().Add(-window))
	if len(samples) < 2 {
		return 0, alertengine.ErrMetricUnavailable
	}
	first, last := &samples[0], &samples[len(samples)-1]
	elapsed := last.CollectedAt.Sub(first.CollectedAt)
	if elapsed <= 0 {
		return 0, alertengine.ErrMetricUnavailable
	}
	// 计数器回绕（agent 重启 / 网卡重置）⇒ 差分为负 ⇒ 视同无数据，
	// 不用绝对值伪装成正常流量。
	delta := float64(rateOf(last)) - float64(rateOf(first))
	if delta < 0 {
		return 0, alertengine.ErrMetricUnavailable
	}
	return delta / elapsed.Seconds(), nil
}
