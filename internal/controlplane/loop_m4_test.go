// Package controlplane 后台 loop 行为断言测试。
//
// 在 loop_test.go（启停验证）基础上，为 8 个后台 loop 补齐"注入测试数据 → 验证 loop 行为"的断言：
//   - leaderLoop          (选主续租)        — 续租后 IsLeader/RenewLeadership 返回 true
//   - notifyLoop          (M7 告警 Webhook)    — 注入 firing 告警，alertChannels.Push 推送到 httptest.Server
//   - autoProvisionLoop   (自动纳管)        — SegmentCIDR 留空时不扫描（无设备被 upsert）
//   - deployReconcileLoop (M3 部署对账)        — 空部署 store 下 ReconcileAll 无错误返回 0
//   - scheduleLoop        (F4 定时调度)        — 注入到点模板任务，FireDueSchedules 派生 pending 实例
//   - archiveLoop         (F5 离线超龄归档)    — 注入超龄设备，RetireStaleDevices 标记 retired
//   - reclaimLoop         (任务租约回收)  — 注入超期 running 任务，ReclaimStaleTasks 复位 pending
//   - cancelLoop          (F3 取消超时任务)    — controlplane 侧对应 workflowScheduleLoop（M5 作业编排）；
//     agent 侧 cancelLoop 位于 internal/agent 包，此处验证 controlplane 第 8 个 loop
//     workflowScheduleLoop 的 ListActive 行为 + 启停。
//
// 测试核心：每个 loop 同时验证 (1) context cancel 正常退出不泄漏 goroutine；(2) 注入数据后底层行为正确。
// 对硬编码长 ticker（10s~60s）的 loop，行为断言直接调用 loop 内调用的 store 方法（loop 行为的核心），
// 避免测试等待长 ticker；对可配置 ticker 的 leaderLoop 等待真实 tick 触发续租。
// 全部测试用 -race 跑，不引入数据竞态。
package controlplane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"opsmesh/internal/deploy"
	"opsmesh/internal/notify"
	"opsmesh/internal/orchestration"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// newLoopM4Server 构造带 alertAggr/alertChannels 的测试控制面（newLoopTestServer 未初始化这两个字段）。
// notifyLoop 行为测试需要 alertChannels.Push 真正推送，故在此补齐。
func newLoopM4Server() *Server {
	s := newLoopTestServer()
	s.alertAggr = notify.NewAlertAggregator()
	return s
}

// --- leaderLoop (选主续租) ---

// TestLoopM4_LeaderLoop_RenewsLeadership 验证 leaderLoop 启动后经一次续租 tick，
// store.IsLeader()==true 且 RenewLeadership 返回 true（单副本 MemoryStore 恒为 leader），
// 随后 context cancel 正常退出。
func TestLoopM4_LeaderLoop_RenewsLeadership(t *testing.T) {
	s := newLoopM4Server()
	s.cfg.LeaderTickSec = 1 // 加快续租频率，1s 内触发一次 RenewLeadership
	s.cfg.LeaderTTLSec = 3

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.leaderLoop(ctx)
	}()

	// 等待至少一次续租 tick（LeaderTickSec=1s）触发 RenewLeadership。
	time.Sleep(1200 * time.Millisecond)

	// 行为断言：单副本 MemoryStore 下续租后始终为 leader。
	if !s.store.IsLeader() {
		cancel()
		t.Fatal("leaderLoop 续租后 store.IsLeader()=false，期望 true（单副本恒为 leader）")
	}
	if !s.store.RenewLeadership(3 * time.Second) {
		cancel()
		t.Fatal("store.RenewLeadership 返回 false，期望 true（单副本续租恒成功）")
	}

	// 启停断言：context cancel 后 5s 内退出。
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("leaderLoop 未在 5s 内退出，疑似 goroutine 泄漏")
	}
}

// --- notifyLoop (M7 告警 Webhook 推送) ---

// TestLoopM4_NotifyLoop_PushesFiringAlert 验证 notifyLoop 推送行为：
// 注入一条 firing 告警，构造 alertChannels 指向 httptest.Server，
// 直接调用 Push（notifyLoop ticker 触发时执行的核心行为）验证 webhook 收到请求。
// 同时验证 notifyLoop 在无任何通道配置时立即返回（早退条件）。
func TestLoopM4_NotifyLoop_PushesFiringAlert(t *testing.T) {
	var received int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&received, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newLoopM4Server()
	// 构造 alertChannels 指向 httptest.Server（绕过 SSRF 校验直接构造 Channels 结构）。
	s.alertChannels = &notify.Channels{
		NotifierType: "generic",
		WebhookURL:   srv.URL,
	}

	// 注入一条 firing 告警（CreatedAt 为当前时间，晚于 lastAlertSent 零值）。
	alert := &proto.Alert{
		AlertID:   "alert-m4-1",
		TenantID:  "t1",
		DeviceID:  "dev-1",
		Severity:  "critical",
		Message:   "cpu usage > 90%",
		Metric:    "cpu.usage",
		CreatedAt: time.Now(),
		Status:    proto.AlertStatusFiring,
	}
	s.store.AddAlert(alert)

	// 行为断言 1：store.Alerts 返回注入的告警（notifyLoop ticker 触发时读取的数据源）。
	alerts := s.store.Alerts("")
	if len(alerts) != 1 {
		t.Fatalf("store.Alerts 返回 %d 条告警，期望 1", len(alerts))
	}
	if alerts[0].AlertID != "alert-m4-1" {
		t.Fatalf("告警 AlertID=%q，期望 alert-m4-1", alerts[0].AlertID)
	}

	// 行为断言 2：alertChannels.Push 推送到 httptest.Server（notifyLoop ticker 触发时执行的核心行为）。
	// 聚合器放行（首次推送同源告警）。
	if !s.alertAggr.Allow(alerts[0], time.Now()) {
		t.Fatal("alertAggr.Allow 返回 false，期望首次推送放行")
	}
	if err := s.alertChannels.Push(alerts[0]); err != nil {
		t.Fatalf("alertChannels.Push 失败: %v", err)
	}
	// 验证 webhook 收到请求（httptest.Server 同步处理，Push 内部 http.Post 同步等待响应）。
	if got := atomic.LoadInt32(&received); got != 1 {
		t.Fatalf("webhook 收到 %d 次请求，期望 1", got)
	}

	// 启停断言：无任何通道配置时 notifyLoop 立即返回（早退条件）。
	s2 := newLoopM4Server()
	s2.cfg.AlertWebhookURL = "" // 无 webhook
	// alertChannels 为 nil（newLoopM4Server 未设置），emailConfigured=false，notifyLoop 立即返回。
	runLoopExpectImmediateReturn(t, "notifyLoop(no-channels)", s2.notifyLoop)
}

// --- autoProvisionLoop (自动纳管) ---

// TestLoopM4_AutoProvisionLoop_NoScanWhenCIDREmpty 验证开启 Discover+AutoProvision 但 SegmentCIDR 留空时，
// autoProvisionLoop 循环条件 `IsLeader() && SegmentCIDR != ""` 为 false，不执行扫描（无设备被 upsert），
// context cancel 正常退出。
func TestLoopM4_AutoProvisionLoop_NoScanWhenCIDREmpty(t *testing.T) {
	s := newLoopM4Server()
	s.cfg.Discover = true
	s.cfg.AutoProvision = true
	s.cfg.SegmentCIDR = "" // 留空避免触发真实网段扫描

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.autoProvisionLoop(ctx)
	}()

	// 等待足够时间让 loop 进入稳态（interval=5min，首次循环立即执行一次空扫描）。
	time.Sleep(200 * time.Millisecond)

	// 行为断言：SegmentCIDR 留空，loop 不扫描，store 中无设备被 upsert。
	snap := s.store.Snapshot("")
	total := 0
	for _, devs := range snap {
		total += len(devs)
	}
	if total != 0 {
		t.Fatalf("SegmentCIDR 留空时 store 中有 %d 台设备，期望 0（不应扫描）", total)
	}

	// 启停断言：context cancel 后 5s 内退出。
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("autoProvisionLoop 未在 5s 内退出，疑似 goroutine 泄漏")
	}
}

// --- deployReconcileLoop (M3 部署对账) ---

// TestLoopM4_DeployReconcileLoop_EmptyStoreNoOp 验证 deployReconcileLoop 在空部署 store 上：
// 循环开始即执行 ReconcileAll（无数据时快速返回 0），不 panic、不报错，context cancel 正常退出。
func TestLoopM4_DeployReconcileLoop_EmptyStoreNoOp(t *testing.T) {
	s := newLoopM4Server()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.deployReconcileLoop(ctx)
	}()

	// 等待 loop 进入稳态（循环开始即执行一次 ReconcileAll）。
	time.Sleep(150 * time.Millisecond)

	// 行为断言：空部署 store 下 ReconcileAll 无错误返回 0（deployReconcileLoop 每次循环调用的核心方法）。
	n := s.deployHandler.ReconcileAll(ctx, "")
	if n != 0 {
		t.Fatalf("空部署 store 下 ReconcileAll 返回 %d，期望 0", n)
	}

	// 启停断言：context cancel 后 5s 内退出。
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deployReconcileLoop 未在 5s 内退出，疑似 goroutine 泄漏")
	}
}

// --- scheduleLoop (F4 定时调度) ---

// TestLoopM4_ScheduleLoop_FiresDueTemplate 验证 scheduleLoop 行为：
// 注入一个到点模板任务（Schedule="* * * * *"），直接调用 store.FireDueSchedules（scheduleLoop ticker 触发时
// 执行的核心方法）验证派生 pending 实例，同时启动 scheduleLoop 验证 context cancel 正常退出。
func TestLoopM4_ScheduleLoop_FiresDueTemplate(t *testing.T) {
	s := newLoopM4Server()

	// 注册一个 agent（CreateTask 要求 agentID 非空；FireDueSchedules 派生实例继承模板 AgentID）。
	agent := s.store.Register(&proto.AgentInfo{Hostname: "host-m4", Segment: "seg-1", TenantID: "t1"})

	// 注入一个到点模板任务（ParentID="" 且 Schedule="* * * * *" 每分钟匹配）。
	tmpl := &proto.Task{
		AgentID:  agent.AgentID,
		TenantID: "t1",
		Type:     proto.TaskTypeShell,
		Command:  "echo m4",
		Schedule: "* * * * *", // 每分钟到点
	}
	s.store.CreateTask(tmpl)

	// 启动 scheduleLoop 验证启停（ticker=30s，测试期间不会触发 FireDueSchedules）。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.scheduleLoop(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	// 行为断言：FireDueSchedules 对到点模板派生 1 个 pending 实例（scheduleLoop ticker 触发时执行的核心行为）。
	now := time.Now()
	fired := s.store.FireDueSchedules(now)
	if fired != 1 {
		t.Fatalf("FireDueSchedules 派生 %d 个实例，期望 1", fired)
	}
	// 验证派生实例状态为 pending 且 ParentID 指向模板。
	children := s.store.TasksByParent(tmpl.TaskID)
	if len(children) != 1 {
		t.Fatalf("模板派生 %d 个子任务，期望 1", len(children))
	}
	if children[0].Status != "pending" {
		t.Fatalf("派生实例 Status=%q，期望 pending", children[0].Status)
	}
	if children[0].ParentID != tmpl.TaskID {
		t.Fatalf("派生实例 ParentID=%q，期望 %q", children[0].ParentID, tmpl.TaskID)
	}

	// 启停断言：context cancel 后 5s 内退出。
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduleLoop 未在 5s 内退出，疑似 goroutine 泄漏")
	}
}

// --- archiveLoop (F5 离线超龄归档) ---

// TestLoopM4_ArchiveLoop_RetiresStaleDevice 验证 archiveLoop 行为：
// 注入一台超龄设备（对应 agent LastSeen 早于归档阈值），直接调用 store.RetireStaleDevices
// （archiveLoop ticker 触发时执行的核心方法）验证标记 retired，同时启动 archiveLoop 验证启停。
func TestLoopM4_ArchiveLoop_RetiresStaleDevice(t *testing.T) {
	s := newLoopM4Server()
	s.cfg.ArchiveAgeMin = 1440 // 启用归档，阈值 24h

	// 注入一台孤儿设备（AgentID 留空，无 agent 关联）。
	// RetireStaleDevices 对孤儿设备直接判定为超龄归档（无需构造超龄 agent，MemoryStore.Register 会重置 LastSeen=now）。
	orphanDev := &proto.DeviceInfo{
		DeviceID: "dev-orphan-m4",
		Segment:  "seg-2",
		TenantID: "t1",
		AgentID:  "", // 孤儿设备（无 agent 关联），RetireStaleDevices 直接归档
		State:    "online",
		Managed:  false,
	}
	s.store.UpsertDevice(orphanDev)

	// 启动 archiveLoop 验证启停（ticker=60s，测试期间不会触发 RetireStaleDevices）。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.archiveLoop(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	// 行为断言：RetireStaleDevices 归档孤儿设备（archiveLoop ticker 触发时执行的核心行为）。
	archived := s.store.RetireStaleDevices(time.Duration(s.cfg.ArchiveAgeMin) * time.Minute)
	if archived < 1 {
		t.Fatalf("RetireStaleDevices 归档 %d 台设备，期望 >=1（含孤儿设备）", archived)
	}
	// 验证孤儿设备已被标记 retired。
	got := s.store.Device("dev-orphan-m4")
	if got == nil {
		t.Fatal("Device(dev-orphan-m4) 返回 nil")
	}
	if !got.Retired {
		t.Fatal("孤儿设备 Retired=false，期望 true（已归档）")
	}

	// 启停断言：context cancel 后 5s 内退出。
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("archiveLoop 未在 5s 内退出，疑似 goroutine 泄漏")
	}
}

// --- reclaimLoop (任务租约回收) ---

// TestLoopM4_ReclaimLoop_ReclaimsStaleTask 验证 reclaimLoop 行为：
// 注入一个超期 running 任务（ClaimedAt 早于租约阈值，ClaimedBy 对应 agent 心跳也超时），
// 直接调用 store.ReclaimStaleTasks（reclaimLoop ticker 触发时执行的核心方法）验证复位 pending，
// 同时启动 reclaimLoop 验证启停。
func TestLoopM4_ReclaimLoop_ReclaimsStaleTask(t *testing.T) {
	s := newLoopM4Server()
	s.cfg.TaskLeaseSec = 300 // 租约 5min

	// 注册一个 agent。
	agent := s.store.Register(&proto.AgentInfo{Hostname: "claim-host", Segment: "seg-1", TenantID: "t1"})

	// 下发一个任务并领取（ClaimTask 翻转 pending→running，设置 ClaimedBy/ClaimedAt）。
	task := s.store.CreateTask(&proto.Task{
		AgentID:  agent.AgentID,
		TenantID: "t1",
		Type:     proto.TaskTypeShell,
		Command:  "sleep 600",
	})
	claimed := s.store.ClaimTask(agent.AgentID)
	if claimed == nil {
		t.Fatal("ClaimTask 返回 nil，期望领取成功")
	}

	// 让任务自然超期：设 TaskLeaseSec=1（1s 租约），sleep 1.2s 后 ClaimedAt 与 agent LastSeen 均超过阈值。
	// （MemoryStore.Register 会重置 LastSeen=now，无公开方法直接回退 LastSeen，故用短租约 + sleep 自然超期。）
	s.cfg.TaskLeaseSec = 1
	time.Sleep(1200 * time.Millisecond) // 超过 1s 租约，ClaimedAt 与 agent LastSeen 均超期

	// 启动 reclaimLoop 验证启停（ticker=30s，测试期间不会触发 ReclaimStaleTasks）。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.reclaimLoop(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	// 行为断言：ReclaimStaleTasks 复位超期 running 任务为 pending（reclaimLoop ticker 触发时执行的核心行为）。
	reclaimed := s.store.ReclaimStaleTasks(time.Duration(s.cfg.TaskLeaseSec) * time.Second)
	if reclaimed < 1 {
		t.Fatalf("ReclaimStaleTasks 复位 %d 个任务，期望 >=1", reclaimed)
	}
	// 验证任务已复位为 pending。
	got := s.store.TaskByID(task.TaskID)
	if got == nil {
		t.Fatal("TaskByID 返回 nil")
	}
	if got.Status != "pending" {
		t.Fatalf("任务 Status=%q，期望 pending（已复位）", got.Status)
	}
	if got.ClaimedBy != "" {
		t.Fatalf("任务 ClaimedBy=%q，期望空（复位后清空领取者）", got.ClaimedBy)
	}

	// 启停断言：context cancel 后 5s 内退出。
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reclaimLoop 未在 5s 内退出，疑似 goroutine 泄漏")
	}
}

// --- cancelLoop → workflowScheduleLoop (M5 作业编排调度) ---

// TestLoopM4_CancelLoop_WorkflowScheduleActive 验证 controlplane 第 8 个 loop workflowScheduleLoop 行为：
// 注入一个 active 工作流，验证 orchHandler.ListActive 返回非空（workflowScheduleLoop ticker 触发时读取的数据源），
// 同时启动 workflowScheduleLoop 验证 context cancel 正常退出。
//
// 说明：任务描述中的 cancelLoop（F3 取消超时任务）位于 internal/agent 包（agent 侧轮询控制面 PollCancels），
// controlplane 包无对应 loop；controlplane 第 8 个后台 loop 为 workflowScheduleLoop（M5 作业编排调度），
// 此处验证其行为 + 启停，覆盖 controlplane 全部 8 个 loop。
func TestLoopM4_CancelLoop_WorkflowScheduleActive(t *testing.T) {
	s := newLoopM4Server()

	// 注入一个 active 工作流（Cron 留空避免触发真实 Trigger）。
	// orchestration.Handler 无公开 Create 方法，直接用 MemoryWorkflowStore.Create 创建后重建 orchHandler。
	wfStore := orchestration.NewMemory()
	wf := &orchestration.WorkflowDef{
		Name:     "wf-m4",
		AgentID:  "agent-wf",
		TenantID: "t1",
		DAG:      `[]`,
		Status:   orchestration.StatusActive,
	}
	if err := wfStore.Create(context.Background(), wf); err != nil {
		t.Fatalf("创建工作流失败: %v", err)
	}
	s.orchHandler = orchestration.NewHandler(wfStore, s.store)

	// 启动 workflowScheduleLoop 验证启停（ticker=30s，先 select 等待，测试期间不会触发 ListActive）。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.workflowScheduleLoop(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	// 行为断言：ListActive 返回注入的 active 工作流（workflowScheduleLoop ticker 触发时读取的数据源）。
	list, err := s.orchHandler.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListActive 返回 %d 个工作流，期望 1", len(list))
	}
	if list[0].Name != "wf-m4" {
		t.Fatalf("工作流 Name=%q，期望 wf-m4", list[0].Name)
	}
	if list[0].Status != orchestration.StatusActive {
		t.Fatalf("工作流 Status=%q，期望 active", list[0].Status)
	}

	// 启停断言：context cancel 后 5s 内退出。
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("workflowScheduleLoop 未在 5s 内退出，疑似 goroutine 泄漏")
	}
}

// --- 编译期断言：确保测试用到的类型/方法存在 ---

var (
	_ *store.MemoryStore
	_ *deploy.Handler
	_ *orchestration.Handler
	_ *notify.Channels
)
