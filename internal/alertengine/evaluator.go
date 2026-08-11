package alertengine


import (
	"sync"
	"time"
)

// sample 一条指标采样。
type sample struct {
	ts    time.Time
	value float64
}

// ringBuffer 环形缓冲，按时间顺序保留最近 cap 条样本。
//
// 写满后覆盖最旧样本；查询时按时间窗口过滤。线程不安全，
// 由 Evaluator.mu 保护。
type ringBuffer struct {
	buf   []sample
	head  int // 下一个写入位置
	count int // 已写入数量（<= cap）
	cap   int
}

func newRingBuffer(capacity int) *ringBuffer {
	if capacity <= 0 {
		capacity = 1
	}
	return &ringBuffer{
		buf: make([]sample, capacity),
		cap: capacity,
	}
}

// push 写入一条样本，覆盖最旧的。
func (r *ringBuffer) push(s sample) {
	r.buf[r.head] = s
	r.head = (r.head + 1) % r.cap
	if r.count < r.cap {
		r.count++
	}
}

// avgSince 返回 ts >= since 的样本平均值。
// 返回 ok=false 表示窗口内无样本。
func (r *ringBuffer) avgSince(since time.Time) (float64, bool) {
	if r.count == 0 {
		return 0, false
	}
	var sum float64
	var n int
	// 从最旧样本开始遍历：若 count<cap，最旧在 0；否则最旧在 head（即将被覆盖的位置）。
	start := 0
	if r.count == r.cap {
		start = r.head
	}
	for i := 0; i < r.count; i++ {
		idx := (start + i) % r.cap
		s := r.buf[idx]
		if !s.ts.Before(since) { // s.ts >= since
			sum += s.value
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

// Evaluator 持续时长评估器。
//
// 职责：
//   - 维护设备指标历史样本（环形缓冲），支持滑动窗口平均值查询。
//   - 维护规则"持续满足开始时间"，条件需连续满足 Duration 才触发告警；
//     条件中断（matched=false）即清空记录、重新计时。
//
// 线程安全：所有读写经 mu 保护。
//
// 时钟可注入（now），便于单元测试；nil 时使用 time.Now。
type Evaluator struct {
	mu           sync.RWMutex
	samples      map[string]map[string]*ringBuffer // deviceID -> metric -> ring
	satisfaction map[string]map[string]time.Time   // deviceID -> ruleID -> 持续满足开始时间
	maxSamples   int
	now          func() time.Time
}

// NewEvaluator 构造评估器。
//
//   - maxSamples：每个 (device,metric) 最多保留的样本数，<=0 时默认 60。
//   - now：可注入时钟，nil 时使用 time.Now。
func NewEvaluator(maxSamples int, now func() time.Time) *Evaluator {
	if maxSamples <= 0 {
		maxSamples = 60
	}
	if now == nil {
		now = time.Now
	}
	return &Evaluator{
		samples:      make(map[string]map[string]*ringBuffer),
		satisfaction: make(map[string]map[string]time.Time),
		maxSamples:   maxSamples,
		now:          now,
	}
}

// RecordSample 记入一条指标样本。
//
// ts 为样本产生时刻（外部传入便于测试回放）；同一 (device,metric) 超过 maxSamples
// 后覆盖最旧样本。
func (e *Evaluator) RecordSample(deviceID, metric string, value float64, ts time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.samples[deviceID] == nil {
		e.samples[deviceID] = make(map[string]*ringBuffer)
	}
	ring := e.samples[deviceID][metric]
	if ring == nil {
		ring = newRingBuffer(e.maxSamples)
		e.samples[deviceID][metric] = ring
	}
	ring.push(sample{ts: ts, value: value})
}

// AvgInWindow 返回 (device,metric) 在 [now-window, now] 内样本的平均值。
//
// window<=0 时退化为最近一条样本的值。
// 返回 ok=false 表示无可用样本（调用方应视为条件不满足）。
func (e *Evaluator) AvgInWindow(deviceID, metric string, window time.Duration) (float64, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ring := e.samples[deviceID][metric]
	if ring == nil || ring.count == 0 {
		return 0, false
	}
	if window <= 0 {
		// 取最近一条样本
		idx := (ring.head - 1 + ring.cap) % ring.cap
		return ring.buf[idx].value, true
	}
	since := e.now().Add(-window)
	return ring.avgSince(since)
}

// ShouldFire 判断规则是否应触发告警。
//
// 语义：
//   - matched=false：清空持续满足记录，返回 false（条件中断重新计时）。
//   - matched=true 且 duration<=0：立即触发，返回 true。
//   - matched=true 且 duration>0：
//     首次满足时记录开始时间并返回 false；后续若 now-start>=duration 则触发并清空
//     记录（避免重复触发，下次触发需重新持续 duration）；否则返回 false。
//
// 该方法持有写锁，调用方不应在 MatchRule 等已持锁路径内调用。
func (e *Evaluator) ShouldFire(deviceID, ruleID string, matched bool, duration time.Duration) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !matched {
		if m := e.satisfaction[deviceID]; m != nil {
			delete(m, ruleID)
		}
		return false
	}

	// matched=true
	if duration <= 0 {
		return true
	}

	now := e.now()
	if e.satisfaction[deviceID] == nil {
		e.satisfaction[deviceID] = make(map[string]time.Time)
	}
	start, exists := e.satisfaction[deviceID][ruleID]
	if !exists {
		e.satisfaction[deviceID][ruleID] = now
		return false
	}
	if now.Sub(start) >= duration {
		delete(e.satisfaction[deviceID], ruleID)
		return true
	}
	return false
}

// Reset 清空指定设备的所有样本与持续满足记录（设备下线/规则全删时调用）。
func (e *Evaluator) Reset(deviceID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.samples, deviceID)
	delete(e.satisfaction, deviceID)
}

// ResetRule 清空所有设备上指定规则的持续满足记录（规则被删除/禁用时调用）。
func (e *Evaluator) ResetRule(ruleID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, m := range e.satisfaction {
		delete(m, ruleID)
	}
}