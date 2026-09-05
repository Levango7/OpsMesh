// Package scheduler 周期性后台任务，task-svc 进程内运行。
//
// 阶段 2 第一批（TD-60 选项 A 第一批 = A-1）：与 internal/controlplane/server_tasks.go 的
// 4 循环字节级等价（scheduleLoop 30s + reclaimLoop 30s + leaderLoop 5s；archiveLoop 不属于
// task-svc 责任——设备归档/过期 token 清理是 controlplane/device-svc 职责，task-svc 不承担）。
//
// A-1 阶段设计原则（双轨对照基线）：
//   - leaderLoop 假实现（IsLeader/RenewLeadership 永真）：task-svc 单进程单实例不需要真选主；
//     多副本切流后 A-2 阶段接 SQL leader_lease 升级为真实现；
//   - 3 循环全部按 controlplane 的 tick 周期与 ctx 取消退出模式 1:1 复制；
//   - 调度动作全部走 store 层（ReclaimStaleTasks/FireDueSchedules），与 controlplane 保持
//     行为一致是双轨对照的字节级前提；
//   - IsLeader gate 在循环顶部（与 controlplane 一致），当前 stub 永真下永远进；
//   - ticker.Stop() 与 defer 配对（controlplane 风格），避免 ticker 泄漏；
//   - ctx 取消即退出（controlplane 行为），无独立 Stop() 接口——生命周期与 main 进程绑定。
package scheduler

import (
	"context"
	"log"
	"time"
)

// Ticker 周期（与 controlplane 4 循环同值；A-1 阶段锁定不变，保证双轨行为一致）。
const (
	scheduleInterval = 30 * time.Second // scheduleLoop 周期
	reclaimInterval  = 30 * time.Second // reclaimLoop 周期
	leaderInterval   = 5 * time.Second  // leaderLoop 续租周期
	leaderTTL        = 15 * time.Second // leader 续租 TTL（与 controlplane 锁 1:1）
)

// ReclaimFunc 是 scheduler 调用的任务租约回收回调——返回回收数。
//
// 函数签名而不是 interface：避免 1:1 模拟 TaskStore 等大型接口（task-svc store 暂无
// IsLeader/ReclaimStaleTasks 等方法；通过最小函数签名注入）。
type ReclaimFunc func(ctx context.Context, maxAge time.Duration) int

// FireFunc 是 scheduler 调用的定时任务派生回调——返回本批次派生数。
type FireFunc func(ctx context.Context, now time.Time) int

// RenewFunc 是 scheduler 调用的 leader 续租回调——返回当前实例是否仍为 leader。
//
// A-1 阶段 task-svc store 假实现（永远返回 true），A-2 切流后接 SQLStore 真选主。
type RenewFunc func(ctx context.Context, ttl time.Duration) bool

// Scheduler 持有 3 循环依赖的 store 回调 + 运行控制。
type Scheduler struct {
	reclaim ReclaimFunc
	fire    FireFunc
	renew   RenewFunc
	// 跨循环共享 ctx（main 进程 signal.NotifyContext 注入，cancel 时 3 循环全退出）。
	ctx context.Context
}

// New 构造 scheduler。reclaim/fire/renew 任一为 nil 时该循环跳过（防御性设计，但 A-1 阶段
// 必传全部——便于单元测试时只跑部分循环）。
func New(ctx context.Context, reclaim ReclaimFunc, fire FireFunc, renew RenewFunc) *Scheduler {
	return &Scheduler{ctx: ctx, reclaim: reclaim, fire: fire, renew: renew}
}

// Start 在后台启 3 goroutine（scheduleLoop + reclaimLoop + leaderLoop）；
// 无独立 Stop()——生命周期与 ctx 绑定，cancel 时 select 的 ctx.Done() 分支全部退出。
//
// 调用方负责构造 ctx（建议用 signal.NotifyContext 包 os.Interrupt + syscall.SIGTERM）。
func (s *Scheduler) Start() {
	if s.fire != nil {
		go s.scheduleLoop()
		log.Printf("scheduler.scheduleLoop 启动 interval=%s", scheduleInterval.String())
	}
	if s.reclaim != nil {
		go s.reclaimLoop()
		log.Printf("scheduler.reclaimLoop 启动 interval=%s", reclaimInterval.String())
	}
	if s.renew != nil {
		go s.leaderLoop()
		log.Printf("scheduler.leaderLoop 启动 interval=%s ttl=%s", leaderInterval.String(), leaderTTL.String())
	}
}

// scheduleLoop 周期性扫描带 Schedule 的模板任务并派生实例——与 controlplane server_tasks.go:259
// 行为等价（30s tick、IsLeader gate、FireDueSchedules 回调、命中数 > 0 记日志）。
func (s *Scheduler) scheduleLoop() {
	ticker := time.NewTicker(scheduleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			n := s.fire(s.ctx, time.Now())
			if n > 0 {
				log.Printf("定时任务派生 fired=%d", n)
			}
		}
	}
}

// reclaimLoop 周期性复位超期 running 任务（任务必达）——与 controlplane server_tasks.go:308
// 行为等价（30s tick、IsLeader gate 通过 store.ReclaimStaleTasks 回调、命中数 > 0 记日志）。
//
// A-1 阶段不携带 batch/canary/refresh token 清理（这些是 controlplane 责任，
// task-svc 不承担——Q1 决策范围）。
func (s *Scheduler) reclaimLoop() {
	ticker := time.NewTicker(reclaimInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			n := s.reclaim(s.ctx, 0) // maxAge 由 store 内部 cfg.TaskLeaseSec 决定（0=使用默认）
			if n > 0 {
				log.Printf("任务租约回收 reclaimed=%d", n)
			}
		}
	}
}

// leaderLoop 选主续租循环——与 controlplane server_tasks.go:337 行为等价（5s tick、
// RenewLeadership 回调、状态变化时记日志——避免每 tick 刷屏）。
func (s *Scheduler) leaderLoop() {
	ticker := time.NewTicker(leaderInterval)
	defer ticker.Stop()
	var wasLeader bool
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			isLeader := s.renew(s.ctx, leaderTTL)
			if isLeader != wasLeader {
				if isLeader {
					log.Printf("晋升为 leader，开始执行周期协调任务 ttl=%s", leaderTTL.String())
				} else {
					log.Printf("失去 leader 身份，暂停周期协调任务")
				}
				wasLeader = isLeader
			}
		}
	}
}
