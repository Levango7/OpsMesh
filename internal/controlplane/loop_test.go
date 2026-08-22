// Package controlplane 后台 loop 单元测试。
//
// 覆盖 server.go 中 8 个后台 loop：
//   - scheduleLoop          (F4 定时调度)
//   - archiveLoop           (F5 离线超龄归档)
//   - notifyLoop            (M7 告警 Webhook 推送)
//   - reclaimLoop           (任务租约回收)
//   - leaderLoop            (选主续租)
//   - autoProvisionLoop     (自动纳管)
//   - deployReconcileLoop   (M3 部署对账)
//   - workflowScheduleLoop  (M5 作业编排调度)
//
// 测试核心：验证每个 loop 在 context cancel 时能正常退出（不泄漏 goroutine、不 panic）。
// 对存在早退条件（archiveLoop/notifyLoop/autoProvisionLoop）的 loop，额外验证关闭配置时立即返回。
// 对 leaderLoop，额外验证单副本（MemoryStore）下始终为 leader。
//
// 全部测试用 -race 跑，不引入数据竞态。
package controlplane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/deploy"
	"opsmesh/internal/orchestration"
	"opsmesh/internal/store"
)

// newLoopTestServer 构造一个完整的测试控制面（白盒装配），包含 deploy/orchestration handler，
// 供后台 loop 测试使用。默认 cfg 为零值（所有 loop 早退条件生效，无外部依赖）。
func newLoopTestServer() *Server {
	st := store.NewMemoryStore()
	return &Server{
		store:         st,
		cfg:           &config.Config{},
		deployHandler: deploy.NewHandler(deploy.NewMemory(), &storeDispatcher{store: st}),
		orchHandler:   orchestration.NewHandler(orchestration.NewMemory(), st),
	}
}

// runLoopAndWait 启动一个 loop goroutine，等待 setupWait 让其进入稳态，
// 然后 cancel context 并验证 loop 在 5s 内正常退出。用于无限循环型 loop。
func runLoopAndWait(t *testing.T, name string, fn func(context.Context), setupWait time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(ctx)
	}()
	time.Sleep(setupWait)
	cancel()
	select {
	case <-done:
		// 正常退出
	case <-time.After(5 * time.Second):
		t.Fatalf("%s 未在 5s 内退出，疑似 goroutine 泄漏", name)
	}
}

// runLoopExpectImmediateReturn 启动一个 loop goroutine，验证其在 500ms 内立即返回。
// 用于有早退条件（配置关闭）的 loop：archiveLoop(ArchiveAgeMin<=0)、
// notifyLoop(AlertWebhookURL=="")、autoProvisionLoop(!Discover||!AutoProvision)。
func runLoopExpectImmediateReturn(t *testing.T, name string, fn func(context.Context)) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(ctx)
	}()
	select {
	case <-done:
		// 立即返回
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("%s 未立即返回（应早退），疑似阻塞", name)
	}
}

// --- scheduleLoop ---

// TestScheduleLoop_CancelExits 验证 scheduleLoop 在 context cancel 时正常退出。
// scheduleLoop 无早退条件，ticker=30s，测试期间不会触发 FireDueSchedules。
func TestScheduleLoop_CancelExits(t *testing.T) {
	s := newLoopTestServer()
	runLoopAndWait(t, "scheduleLoop", s.scheduleLoop, 100*time.Millisecond)
}

// --- archiveLoop ---

// TestArchiveLoop_DisabledReturns 验证 ArchiveAgeMin<=0 时 archiveLoop 立即返回（关闭自动归档）。
func TestArchiveLoop_DisabledReturns(t *testing.T) {
	s := newLoopTestServer()
	s.cfg.ArchiveAgeMin = 0 // 关闭自动归档
	runLoopExpectImmediateReturn(t, "archiveLoop(disabled)", s.archiveLoop)
}

// TestArchiveLoop_CancelExits 验证 archiveLoop 在 context cancel 时正常退出。
// ArchiveAgeMin=1440 启用归档，ticker=60s，测试期间不会触发 RetireStaleDevices。
func TestArchiveLoop_CancelExits(t *testing.T) {
	s := newLoopTestServer()
	s.cfg.ArchiveAgeMin = 1440
	runLoopAndWait(t, "archiveLoop", s.archiveLoop, 100*time.Millisecond)
}

// --- notifyLoop ---

// TestNotifyLoop_NoWebhookReturns 验证 AlertWebhookURL 为空时 notifyLoop 立即返回。
func TestNotifyLoop_NoWebhookReturns(t *testing.T) {
	s := newLoopTestServer()
	s.cfg.AlertWebhookURL = "" // 未配置 webhook
	runLoopExpectImmediateReturn(t, "notifyLoop(no-webhook)", s.notifyLoop)
}

// TestNotifyLoop_CancelExits 验证 notifyLoop 在 context cancel 时正常退出。
// 用 httptest.Server 作为 webhook 目标（避免外部依赖），ticker=10s 不会触发推送。
func TestNotifyLoop_CancelExits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newLoopTestServer()
	s.cfg.AlertWebhookURL = srv.URL
	s.cfg.AlertNotifierType = "generic"
	runLoopAndWait(t, "notifyLoop", s.notifyLoop, 100*time.Millisecond)
}

// --- reclaimLoop ---

// TestReclaimLoop_CancelExits 验证 reclaimLoop 在 context cancel 时正常退出。
// reclaimLoop 无早退条件，ticker=30s，测试期间不会触发 ReclaimStaleTasks。
func TestReclaimLoop_CancelExits(t *testing.T) {
	s := newLoopTestServer()
	runLoopAndWait(t, "reclaimLoop", s.reclaimLoop, 100*time.Millisecond)
}

// --- leaderLoop ---

// TestLeaderLoop_CancelExits 验证 leaderLoop 在 context cancel 时正常退出。
// leaderLoop 无早退条件，ticker=LeaderTickSec（默认回退 5s）。
func TestLeaderLoop_CancelExits(t *testing.T) {
	s := newLoopTestServer()
	s.cfg.LeaderTickSec = 1 // 加快续租频率便于测试
	s.cfg.LeaderTTLSec = 3
	runLoopAndWait(t, "leaderLoop", s.leaderLoop, 150*time.Millisecond)
}

// TestLeaderLoop_SingleReplicaIsLeader 验证单副本（MemoryStore）下 leaderLoop 始终为 leader。
// MemoryStore.RenewLeadership/IsLeader 恒返回 true，leaderLoop 续租后 reg.IsLeader() 应为 true。
func TestLeaderLoop_SingleReplicaIsLeader(t *testing.T) {
	s := newLoopTestServer()
	s.cfg.LeaderTickSec = 1
	s.cfg.LeaderTTLSec = 3

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.leaderLoop(ctx)
	}()

	// 等待至少一次续租 tick（LeaderTickSec=1s）触发 RenewLeadership
	time.Sleep(1200 * time.Millisecond)
	if !s.store.IsLeader() {
		cancel()
		t.Fatal("单副本 MemoryStore 下 reg.IsLeader()=false，期望始终为 leader")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("leaderLoop 未在 5s 内退出，疑似 goroutine 泄漏")
	}
}

// --- autoProvisionLoop ---

// TestAutoProvisionLoop_DisabledReturns 验证 Discover=false 时 autoProvisionLoop 立即返回。
func TestAutoProvisionLoop_DisabledReturns(t *testing.T) {
	s := newLoopTestServer()
	s.cfg.Discover = false
	s.cfg.AutoProvision = false
	runLoopExpectImmediateReturn(t, "autoProvisionLoop(disabled)", s.autoProvisionLoop)
}

// TestAutoProvisionLoop_CancelExits 验证 autoProvisionLoop 在 context cancel 时正常退出。
// 开启 Discover+AutoProvision 但 SegmentCIDR 留空，避免触发真实网段扫描（provision.AutoProvision）。
// loop 循环条件 `IsLeader() && SegmentCIDR != ""` 为 false，不执行扫描，直接 select 等待。
func TestAutoProvisionLoop_CancelExits(t *testing.T) {
	s := newLoopTestServer()
	s.cfg.Discover = true
	s.cfg.AutoProvision = true
	s.cfg.SegmentCIDR = "" // 留空避免触发真实网段扫描
	runLoopAndWait(t, "autoProvisionLoop", s.autoProvisionLoop, 100*time.Millisecond)
}

// --- deployReconcileLoop ---

// TestDeployReconcileLoop_CancelExits 验证 deployReconcileLoop 在 context cancel 时正常退出。
// deployReconcileLoop 无早退条件，循环开始即执行 ReconcileAll（无数据时快速返回），ticker=15s。
func TestDeployReconcileLoop_CancelExits(t *testing.T) {
	s := newLoopTestServer()
	runLoopAndWait(t, "deployReconcileLoop", s.deployReconcileLoop, 100*time.Millisecond)
}

// --- workflowScheduleLoop ---

// TestWorkflowScheduleLoop_CancelExits 验证 workflowScheduleLoop 在 context cancel 时正常退出。
// workflowScheduleLoop 无早退条件，先 select 等待 ticker=30s，测试期间不会触发 ListActive。
func TestWorkflowScheduleLoop_CancelExits(t *testing.T) {
	s := newLoopTestServer()
	runLoopAndWait(t, "workflowScheduleLoop", s.workflowScheduleLoop, 100*time.Millisecond)
}
