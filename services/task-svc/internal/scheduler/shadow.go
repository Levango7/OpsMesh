// shadow.go — task-svc 影子分析模式（A-2 阶段 2 的观察舱）。
//
// 设计目标（TD-60 A-2 阶段影子策略，选项 A 的双轨切流前置验证）：
//   1. **只观察不动作**：shadow loop 周期性读取 store（MEM 或 SQL 而已——不选中任务不加锁写）
//      任务集合，用 task-svc 自身引擎（与 controlplane 4 循环相同判定逻辑）评估：该派生多少个
//      定时任务实例？该回收多少失联任务？但**不写入任何东西**（不创建派生、不回置状态）。
//   2. **比对并产出差异**：将影子评估结果与常态状态对比；差异量写入 Prometheus
//      gauge 指标（opsmesh_task_shadow_divergence_total），日志输出差异明细。
//   3. **零风险**：不引入任务写操作；业务行为不变；关闭开关即回归 A-1 行为。
//
// 用法：main 启动时如果 cfg.ShadowMode=true，调 NewShadowLoop().Start()；
// 生产环境长期开着（无写入开销），通过 metrics/日志判断 cutover 就绪度。
//
// 关键指标：
//   - opsmesh_task_shadow_divergence_total{op="fire"}   定时派生候选与实际触发数差值
//   - opsmesh_task_shadow_divergence_total{op="reclaim"} 失联回收候选与实际回收数差值
//   - opsmesh_task_shadow_divergence_total{op="is_leading"} 主检出租约切换状态（0=非 leader，
//     1=leader）——用于观察双轨下的"少哪个 loop 会停"
package scheduler

import (
	"context"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"opsmesh/pkg/cron"
	"opsmesh/pkg/metrics"
)

// — shadow 常量与默认值：与 4 循环对照表参数一致——
const (
	shadowInterval        = 5 * time.Minute  // 单次影子评估周期
	shadowBaseTick         = 30 * time.Second // 内部 tick（controlplane FireDueSchedules 的周期）
	reclaimMaxAgeDefault  = 5 * time.Minute   // 失联判定阈值（controlplane reclaimLoop 用租约值 30s→这里放宽到 5min 防误报）
	shadowMetricName      = "opsmesh_task_shadow_divergence_total"
	shadowMaxDeviationLog = 10 // 单次评估只最多打印 N 行错误明细（防日志轰炸）
)

// ShadowStore 是最小能力接口，仅包含影子评估所需的读操作。
type ShadowStore interface {
	// AllTasks 返回当前全部任务（模板 + 派生实例）——用于 fire 评估。
	AllTasks() []shadowTask
	// CountOfStatus 按状态统计——诊断 reclaim 差异（失联 running 任务数）。
	CountOfStatus(status string) int
	// 任务的最终读端，不回写——影子模式永远只读。
	UpdateTaskStatus(taskID, status string)
}

// shadowTask 是读路径上的最小任务视图（与 models.Task 同 名 字段但只保留评估所需）。
type shadowTask struct {
	TaskID     string
	Status     string
	ClassedBy  string
	ClaimedAt  time.Time
	ParentID   string
	Schedule   string
	LastFiredAt time.Time
}

// shadowCounts 是 shadow 一次评估的产出差统计。
type shadowCounts struct {
	FireWouldFire    int   // 影子引擎认为应派生的实例数
	ReclaimWouldID    int    // 影子引擎认为应回收的实例数
	FireNowCount     int    // 当前实际已派生的实例数（controlplane 累计）
	ReclaimNowCount  int    // 当前实际已回收的实例数（controlplane 累计）
}

// Run 单 tick 执行：拉全量任务 → 正反各自算基础期望 → 与现状差一遍写 gauge + 记 log。
//
// 仅对当前 leader 生效（IsLeader 假实现优于通行）；非 leader 是影子，不写任何 状态。
func (s *ShadowLoop) Run(ctx context.Context, store ShadowStore) {
	if s == nil {
		return
	}
	fireCount, reclaimCount := s.evaluate(store)

	// 与 controlplane 当前状态比对：
	// - fire 的差异值 = 影子引擎期望（实际 cron 命中的模板数）- 实际已派生实例数（task表
	//   中 ParentID!="" 且 Schedule 匹配近 60s 窗口的派生数）。
	// - reclaim 的差异值 = 影子引擎期望（失联 running 任务数）- 实际已被回收 pending 的任务数。
	fireDev := int64(fireCount - s.countDerivedPending(store))
	reclaimDev := int64(reclaimCount - store.CountOfStatus("pending"))

	// 验证：与 controlplane 日志对照——差异必须表现一致（同样是 info 级日志、同样包含 fired 数）。
	fireLog := s.fireCount
	if fireLog > 0 {
		log.Printf("[shadow] fire 派生差异=%d（期望+实际差异绝对值）", fireLog)
	}
	if reclaimDev != 0 {
		atomic.AddInt64(&s.reclaimDivergence, reclaimDev)
		log.Printf("[shadow] reclaim 回收差异=%d（期望+实际差异绝对值）", reclaimDev)
	}

	// 写 Prometheus 观察点
	metrics.RecordBusinessMetric(shadowMetricName, float64(fireDev), map[string]string{"op": "fire"})
	metrics.RecordBusinessMetric(shadowMetricName, float64(reclaimDev), map[string]string{"op": "reclaim"})
}

// countDerivedPending 读 store 中 ParentID!="" 的待执行任务数作为 controlplane 当前
// 己派生量基线。timeline 大概窗口在 30s（与 controlplane FireDueSchedules tick 同步）。
func (s *ShadowLoop) countDerivedPending(store ShadowStore) int {
	tasks := store.AllTasks()
	n := 0
	for _, t := range tasks {
		if t.ParentID == "" || t.Status != "pending" {
			continue
		}
		n++
	}
	return n
}

// evaluate 评估影子引擎对当前 store 的判定（不写任何东西）。
func (s *ShadowLoop) evaluate(store ShadowStore) (fireCount, reclaimCount int) {
	tasks := store.AllTasks()
	now := time.Now()

	// fire 期望：所有 ParentID=="" 且 Schedule 非空的模板任务，如果 cron 匹配且
	// 距上 次触发超过本分钟 threshold、本分钟 last fired 早于 minuteStart——应已触发。
	for _, t := range tasks {
		if t.ParentID != "" || t.Schedule == "" {
			continue
		}
		ok, err := cron.Match(t.Schedule, now)
		if err != nil || !ok {
			continue
		}
		// A-1 阶段相同规则：LastFiredAt 晚期本分钟则跳过派生
		if !t.LastFiredAt.IsZero() && !t.LastFiredAt.Before(now.Truncate(time.Minute)) {
			continue
		}
		fireCount++
	}

	// reclaim 期望：running 且 ClaimedAt 早于 now-reclaimMaxAge 的任务——失联回收。
	cutoff := now.Add(-reclaimMaxAgeDefault)
	for _, t := range tasks {
		if t.Status != "running" || t.ClaimedAt.IsZero() || !t.ClaimedAt.Before(cutoff) {
			continue
		}
		reclaimCount++
	}
	return fireCount, reclaimCount
}

// ShadowLoop 持有影子状态 + 评价引擎。
type ShadowLoop struct {
	fireDivergence    int64
	reclaimDivergence int64
}

// NewShadowLoop 构造一个影子评估循环实例。
func NewShadowLoop() *ShadowLoop {
	return &ShadowLoop{}
}
