// shadow.go — task-svc 影子分析模式（A-2 阶段 2 的观察舱）。
//
// 设计目标（TD-60 A-2 阶段影子策略，选项 A 的双轨切流前置验证）：
//   1. **只观察不动作**：shadow loop 周期性读取 store（走 store.TaskStore 接口），
//      用 task-svc 自身引擎（与 controlplane 4 循环相同判定逻辑）评估：该派生多少个
//      定时任务实例？该回收多少失联任务？但**不写入任何东西**。
//   2. **产出观察指标**：将影子评估结果写入 Prometheus gauge，日志输出评估明细；
//      运维对比同期 controlplane 的 FireDueSchedules/ReclaimStaleTasks 产出，
//      判断 task-svc 引擎与 controlplane 行为是否一致。
//   3. **零风险**：不引入任务写操作；业务行为不变；关闭开关即回归 A-1 行为。
//
// 用法：main 启动时如果 cfg.ShadowMode=true，调 NewShadowLoop(ts).Start(ctx)；
// 生产环境长期开着（无写入开销），通过 metrics/日志判断 cutover 就绪度。
package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/Levango7/OpsMesh/services/task-svc/internal/store"
	"opsmesh/pkg/cron"
	"opsmesh/pkg/metrics"
)

// shadow 常量：与 controlplane 4 循环对照表参数一致。
const (
	shadowInterval      = 5 * time.Minute // 影子评估周期
	reclaimMaxAge       = 5 * time.Minute // 失联判定阈值（放宽到 5min 防误报）
	shadowMetricPrefix  = "opsmesh_task_shadow"
)

// ShadowLoop 持有影子状态 + 评价引擎。
type ShadowLoop struct {
	store store.TaskStore
}

// NewShadowLoop 构造一个影子评估循环实例。
func NewShadowLoop(store store.TaskStore) *ShadowLoop {
	return &ShadowLoop{store: store}
}

// Start 在后台 goroutine 中启动影子循环；ctx 取消即退出（与 scheduler 三循环同生命周期）。
func (s *ShadowLoop) Start(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	go s.loop(ctx)
	log.Printf("[shadow] 影子模式启动 interval=%s", shadowInterval.String())
}

// loop 周期性执行影子评估；ctx.Done() 退出。
func (s *ShadowLoop) loop(ctx context.Context) {
	ticker := time.NewTicker(shadowInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.evaluate(time.Now())
		}
	}
}

// evaluate 单 tick 评估：拉全量任务 → 计算 fire/reclaim 期望 → 写 gauge + 记 log。
//
// 仅观察不写：不调用 store.CreateTask/UpdateTask，只读 AllTasks。
// A-1 阶段：产出指标供运维对比 controlplane 同期行为；A-2 切流前连续 N 天指标趋零即表示双轨行为一致。
func (s *ShadowLoop) evaluate(now time.Time) {
	if s == nil || s.store == nil {
		return
	}
	tasks := s.store.AllTasks()

	// fire 期望：所有 ParentID=="" + Schedule!="" 的模板任务，如果 cron 匹配且本分钟未触发过。
	fireWouldFire := 0
	minuteStart := now.Truncate(time.Minute)
	for _, t := range tasks {
		if t == nil || t.ParentID != "" || t.Schedule == "" {
			continue
		}
		ok, err := cron.Match(t.Schedule, now)
		if err != nil || !ok {
			continue
		}
		// 本分钟已触发过则跳过（CreatedAt 近似 LastFiredAt：派生实例 CreatedAt 在本分钟区间内即认为已触发）。
		if !t.CreatedAt.IsZero() && !t.CreatedAt.Before(minuteStart) {
			continue
		}
		fireWouldFire++
	}

	// reclaim 期望：running 且 ClaimedAt 早于 now-reclaimMaxAge 的任务——失联回收候选。
	reclaimWouldID := 0
	cutoff := now.Add(-reclaimMaxAge)
	for _, t := range tasks {
		if t == nil || t.Status != "running" || t.ClaimedAt.IsZero() || !t.ClaimedAt.Before(cutoff) {
			continue
		}
		reclaimWouldID++
	}

	// 当前实际状态：已派生实例数（ParentID!="" 且 status=pending/running）。
	derivedActive := 0
	for _, t := range tasks {
		if t == nil || t.ParentID == "" {
			continue
		}
		if t.Status == "pending" || t.Status == "running" {
			derivedActive++
		}
	}

	// 写 Prometheus 观察点。
	metrics.RecordBusinessMetric(shadowMetricPrefix+"_fire_would_fire", float64(fireWouldFire), nil)
	metrics.RecordBusinessMetric(shadowMetricPrefix+"_reclaim_would_id", float64(reclaimWouldID), nil)
	metrics.RecordBusinessMetric(shadowMetricPrefix+"_derived_active", float64(derivedActive), nil)

	if fireWouldFire > 0 || reclaimWouldID > 0 {
		log.Printf("[shadow] fire_would_fire=%d reclaim_would_id=%d derived_active=%d",
			fireWouldFire, reclaimWouldID, derivedActive)
	}
}
