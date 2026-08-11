
// sla.go 提供 cron 任务 SLA 监控能力：
//   - SLA 定义任务预期完成时间（Deadline）与告警阈值；
//   - Monitor 跟踪任务执行时长，超时触发告警回调；
//   - 周期由外部调度器调用 Monitor.Check(now) 评估所有进行中任务。
//
// 设计原则：本包不依赖 notify/alertengine 等外部模块，告警通过 OnBreach 回调注入。
package cron

import (
	"sync"
	"time"
)

// SLAConfig 单任务 SLA 配置。
type SLAConfig struct {
	TaskID    string        // 任务 ID
	Deadline  time.Duration // 预期完成时长（从 StartedAt 起算）；<=0 表示不限制
	WarnAt    time.Duration // 告警阈值（应 <= Deadline）；<=0 表示不告警
	StartedAt time.Time     // 任务开始时间（由 Monitor.Track 设置）
}

// SLABreachEvent SLA 违规事件（传给 OnBreach 回调）。
type SLABreachEvent struct {
	TaskID    string    // 任务 ID
	Kind      string    // "warn"（达到 WarnAt）或 "breach"（超过 Deadline）
	StartedAt time.Time // 开始时间
	At        time.Time // 检测时间
	Elapsed   time.Duration
	Threshold time.Duration // 触发的阈值（WarnAt 或 Deadline）
}

// SLAMonitor SLA 监控器：跟踪任务执行时长并触发告警。
//
// 线程安全：通过 mu 保护 tasks 与 fired 索引。
type SLAMonitor struct {
	mu     sync.Mutex
	tasks  map[string]*SLAConfig // taskID -> 配置（含 StartedAt）
	fired  map[string]string     // taskID -> 已触发事件（"warn"/"breach"），避免重复告警
	onBreach func(SLABreachEvent)
	now    func() time.Time
}

// NewSLAMonitor 构造 SLA 监控器。onBreach 为告警回调（nil=不告警）。
func NewSLAMonitor(onBreach func(SLABreachEvent)) *SLAMonitor {
	return &SLAMonitor{
		tasks:    make(map[string]*SLAConfig),
		fired:    make(map[string]string),
		onBreach: onBreach,
		now:      time.Now,
	}
}

// SetNow 注入时间函数（测试用）。
func (m *SLAMonitor) SetNow(fn func() time.Time) {
	m.mu.Lock()
	m.now = fn
	m.mu.Unlock()
}

// Track 开始跟踪一个任务的 SLA。若 taskID 已存在则覆盖。
func (m *SLAMonitor) Track(cfg *SLAConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *cfg
	if cp.StartedAt.IsZero() {
		cp.StartedAt = m.now()
	}
	m.tasks[cfg.TaskID] = &cp
	delete(m.fired, cfg.TaskID) // 重置已触发标记
}

// Complete 标记任务完成，停止 SLA 跟踪。返回是否曾违规（warn 或 breach）。
func (m *SLAMonitor) Complete(taskID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, hadFired := m.fired[taskID]
	delete(m.tasks, taskID)
	delete(m.fired, taskID)
	return hadFired
}

// Check 评估所有进行中任务，对达到 WarnAt/Deadline 的任务触发 OnBreach 回调。
// 返回本次触发的违规事件数。回调在锁外执行，避免回调内调用 Track/Complete 死锁。
func (m *SLAMonitor) Check(now time.Time) []SLABreachEvent {
	m.mu.Lock()
	var events []SLABreachEvent
	for id, cfg := range m.tasks {
		if cfg.WarnAt <= 0 && cfg.Deadline <= 0 {
			continue
		}
		elapsed := now.Sub(cfg.StartedAt)
		// 优先触发 breach（Deadline），再触发 warn（WarnAt）。
		if cfg.Deadline > 0 && elapsed > cfg.Deadline && m.fired[id] != "breach" {
			m.fired[id] = "breach"
			events = append(events, SLABreachEvent{
				TaskID: id, Kind: "breach", StartedAt: cfg.StartedAt, At: now,
				Elapsed: elapsed, Threshold: cfg.Deadline,
			})
			continue
		}
		if cfg.WarnAt > 0 && elapsed > cfg.WarnAt && m.fired[id] == "" {
			m.fired[id] = "warn"
			events = append(events, SLABreachEvent{
				TaskID: id, Kind: "warn", StartedAt: cfg.StartedAt, At: now,
				Elapsed: elapsed, Threshold: cfg.WarnAt,
			})
		}
	}
	m.mu.Unlock()
	// 锁外触发回调。
	if m.onBreach != nil {
		for _, e := range events {
			m.onBreach(e)
		}
	}
	return events
}

// Tracked 返回当前被跟踪的任务 ID 列表（排序后）。
func (m *SLAMonitor) Tracked() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.tasks))
	for id := range m.tasks {
		out = append(out, id)
	}
	// 排序以保证测试稳定。
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// Get 返回指定任务的 SLA 配置快照（不存在返回 nil）。
func (m *SLAMonitor) Get(taskID string) *SLAConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, ok := m.tasks[taskID]
	if !ok {
		return nil
	}
	cp := *cfg
	return &cp
}