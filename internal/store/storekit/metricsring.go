package storekit

import (
	"sync/atomic"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// MetricsRingDefaultCap 环形缓冲默认容量：2h * 120 samples/h（30s 采样间隔）= 240 条。
// 每条 DeviceMetrics 约 1KB，总 ~240KB/设备。
const MetricsRingDefaultCap = 240

// MaxTrackedDeviceMetrics 设备指标驻留设备数上限；超限按「最久未写入」淘汰整条设备条目。
// 内存量级：单设备 240 条样本 × ~1KB ≈ 240KB，2000 设备 ≈ 480MB 上限。
// 规模化部署的历史时序应以外部时序库（部署栈内已含 Prometheus/Grafana）为准，
// 此处缓冲仅作控制面内快速回看。
const MaxTrackedDeviceMetrics = 2000

// MetricsRing 设备监控指标环形缓冲：保留最近 N 条历史快照。
// 用 slice + head index 实现，O(1) 追加 O(n) 读取。
// 自身无线程安全，由外层 store 的 mu 统一保护并发。
type MetricsRing struct {
	samples  []proto.DeviceMetrics // 环形缓冲 slice（固定容量，覆写最旧）
	head     int                   // 下一个写入位置（0..capacity-1）
	size     int                   // 当前已写入数量（<= capacity）
	capacity int                   // 缓冲容量
	// writeSeq 本条目最近一次写入的单调序号，用于 map 超上限时按「最久未更新」淘汰设备条目（P1-4）。
	// 刻意用 store 侧写入序号而非样本里的 CollectedAt：后者由 agent 上报，可被伪造成未来时间从而永不被淘汰。
	// 也不用墙钟：Windows 时间粒度约 15.6ms，同刻写入会有并列，淘汰对象随 map 迭代顺序随机。
	writeSeq uint64
}

// deviceMetricsSeq 设备指标写入序号（单调递增，进程内唯一）。
//
// 为什么不用墙钟：淘汰顺序必须全序确定。Windows 的 time.Now() 粒度约 15.6ms，
// 高负载下同一刻会落入多条写入，按时刻比较会出现「并列」，此刻淘汰哪个取决于 map
// 迭代顺序（随机）——实测表现为 `TestStoreDeviceMetrics_RecentlyWrittenSurvives`
// 偶发失败。单调序号既保证严格全序，又保留了原始设计意图：排序依据是 store 侧写入
// 顺序，不受 agent 上报时间（可被伪造到未来）影响。
var deviceMetricsSeq atomic.Uint64

// nextDeviceMetricsSeq 返回下一个写入序号（由调用方在持锁下调用）。
func nextDeviceMetricsSeq() uint64 { return deviceMetricsSeq.Add(1) }

// NewMetricsRing 创建环形缓冲。capacity<=0 时用 MetricsRingDefaultCap（240）。
func NewMetricsRing(capacity int) *MetricsRing {
	if capacity <= 0 {
		capacity = MetricsRingDefaultCap
	}
	return &MetricsRing{
		samples:  make([]proto.DeviceMetrics, capacity),
		capacity: capacity,
	}
}

// Add 追加一条指标快照到环形缓冲（O(1)）。m 为 nil 时直接返回。
// 深拷贝入参避免外部并发修改污染缓冲。
func (r *MetricsRing) Add(m *proto.DeviceMetrics) {
	if r == nil || m == nil {
		return
	}
	cp := *m
	r.samples[r.head] = cp
	r.head = (r.head + 1) % r.capacity
	if r.size < r.capacity {
		r.size++
	}
	r.writeSeq = nextDeviceMetricsSeq() // 供设备条目淘汰排序用（P1-4，单调序号保证全序）
}

// Latest 返回最近一条指标快照（无数据时返回 nil）。返回深拷贝。
func (r *MetricsRing) Latest() *proto.DeviceMetrics {
	if r == nil || r.size == 0 {
		return nil
	}
	// head 指向下一个写入位置，最新一条在 (head-1+capacity)%capacity。
	idx := (r.head - 1 + r.capacity) % r.capacity
	cp := r.samples[idx]
	return &cp
}

// Since 返回 CollectedAt >= since 的所有快照（按时间升序）。
// since 为零值时返回全部已存储快照。无数据时返回 nil。返回深拷贝。
func (r *MetricsRing) Since(t time.Time) []proto.DeviceMetrics {
	if r == nil || r.size == 0 {
		return nil
	}
	// 最早一条的位置：若 size<capacity，从 0 开始；否则从 head 开始（head 指向最旧）。
	start := 0
	if r.size == r.capacity {
		start = r.head
	}
	out := make([]proto.DeviceMetrics, 0, r.size)
	for i := 0; i < r.size; i++ {
		idx := (start + i) % r.capacity
		s := r.samples[idx]
		if !t.IsZero() && s.CollectedAt.Before(t) {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EvictDeviceMetricsIfNeeded 在设备条目数超过上限时淘汰最久未写入的条目，返回淘汰数。
// 超出量通常为 1（每次心跳最多新增 1 个设备），循环代价可忽略。
// 排序依据是 writeSeq（单调递增写入序号）而非墙钟：严格全序，同刻写入亦可确定淘汰对象。
func EvictDeviceMetricsIfNeeded(m map[string]*MetricsRing) int {
	if len(m) <= MaxTrackedDeviceMetrics {
		return 0
	}
	evicted := 0
	for len(m) > MaxTrackedDeviceMetrics {
		oldestKey := ""
		var oldestSeq uint64
		for k, r := range m {
			if r == nil {
				oldestKey = k
				break
			}
			if oldestKey == "" || r.writeSeq < oldestSeq {
				oldestKey, oldestSeq = k, r.writeSeq
			}
		}
		if oldestKey == "" {
			return evicted
		}
		delete(m, oldestKey)
		evicted++
	}
	return evicted
}
