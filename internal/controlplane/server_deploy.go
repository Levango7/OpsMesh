// server_deploy.go 部署对账与工作流调度后台循环。
//
// 从 server.go 拆分而来（task 114：按路由域拆分巨型 server.go）。
// 部署/工作流的 HTTP handler 由 deploy/orchestration 包各自注册（见 server.go Start），
// 此处仅保留控制面侧的后台对账循环，逻辑未做任何修改。
package controlplane

import (
	"context"
	"log"
	"time"

	"opsmesh/internal/cron"
	"opsmesh/internal/logx"
)

// deployReconcileLoop 后台周期对账 M3 部署：把 running 部署按底层任务结果翻成功/失败。
// 仅 leader 执行（避免多副本重复对账写库）。
func (s *Server) deployReconcileLoop(ctx context.Context) {
	const interval = 15 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if s.store.IsLeader() {
			s.deployHandler.ReconcileAll(ctx, "")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// workflowScheduleLoop 后台周期按 cron 触发 active 工作流并 reconcile 运行态。
// 仅 leader 执行（避免多副本重复派发底层任务）。
func (s *Server) workflowScheduleLoop(ctx context.Context) {
	const interval = 30 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if !s.store.IsLeader() {
			continue
		}
		list, err := s.orchHandler.ListActive(ctx)
		if err != nil {
			continue
		}
		now := time.Now()
		nowMin := now.Truncate(time.Minute)
		for _, wf := range list {
			if wf.Cron == "" {
				continue
			}
			ok, err := cron.Match(wf.Cron, now)
			if err != nil || !ok {
				continue
			}
			// 防同分钟重复触发：与上次运行落在本分钟内则跳过。
			if !wf.LastRunAt.IsZero() && wf.LastRunAt.Truncate(time.Minute).Equal(nowMin) {
				continue
			}
			if _, err := s.orchHandler.Trigger(ctx, wf.ID, wf.TenantID); err != nil {
				logx.Error(ctx, "工作流 cron 触发失败", err, "workflowID", wf.ID)
				continue
			}
			if err := s.orchHandler.Reconcile(ctx, wf.ID, wf.TenantID); err != nil {
				log.Printf("controlplane: cronLoop Reconcile 工作流 %d 失败: %v", wf.ID, err)
			}
		}
	}
}
