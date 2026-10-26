// sql_tasks_fixes_test.go — tasks 读/写侧真库往返守卫（DSN 门控），覆盖 store/state-machine P0/P1 修复。
//
// 为什么放在这里：内存后端能覆盖的只有"逻辑对了"，而这些缺陷只在真库/生产形态暴露——
//   - FIX 1（P0）：CreateTask 的 INSERT 漏 approval_required 列、且不把高风险任务翻成
//     pending_approval ⇒ MySQL 上审批工作流整条链路静默失效（ApproveTask/RejectTask
//     的 WHERE status='pending_approval' 匹配不到行）。本文件把"落库 → 读回 → 领取"钉死。
//   - FIX 4（P1）：tasks 读路径（AllTasks→monitorTaskTimeouts）必须带出 claimed_at，
//     否则超时判定退化为 CreatedAt，排队久的任务一开跑就被取消。
//   - FIX 2（P1）：SubmitResult 对 ClaimEpoch 不匹配的上报必须拒绝（不写终态）。
//   - FIX 5（P1）：上游 failed ⇒ 下游由 blocked 变 failed + dead_letter（可被死信端点看见），
//     而不是永久 blocked。
//   - FIX 6（P1）：ReclaimStaleTasks 必须 retry_count+1，预算耗尽后置 failed + dead_letter。
//
// 无 MySQL 时整体 t.Skip（与 migration_test.go / sql_tasks_mysql_test.go 同惯例）。
package sqlstore

import (
	"os"
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// fixesTestStore 构造真库 store，未设 OPSMESH_TEST_MYSQL_DSN 时跳过。
func fixesTestStore(t *testing.T) *SQLStore {
	t.Helper()
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping tasks fixes integration test")
	}
	s, err := NewSQLStore(dsn, "", "")
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	return s
}

// cleanupFixTasks 删除本文件用的任务与结果，保证可重复运行。
func cleanupFixTasks(t *testing.T, s *SQLStore, agentID string) {
	t.Helper()
	_, _ = s.db.Exec("DELETE FROM task_results WHERE agent_id=?", agentID)
	_, _ = s.db.Exec("DELETE FROM tasks WHERE agent_id=?", agentID)
	_, _ = s.db.Exec("DELETE FROM agents WHERE agent_id=?", agentID)
}

// registerFixAgent 注册 agent 并把 last_seen 拨到过去（ReclaimStaleTasks 的心跳守卫
// 要求 claiming agent 失联；Register 会把 last_seen 置为当前时间）。
func registerFixAgent(t *testing.T, s *SQLStore, agentID, tenantID string) {
	t.Helper()
	if s.Register(&proto.AgentInfo{AgentID: agentID, Segment: "seg-a", TenantID: tenantID}) == nil {
		t.Fatalf("Register agent %s 失败", agentID)
	}
	if _, err := s.db.Exec(
		`UPDATE agents SET last_seen = DATE_SUB(UTC_TIMESTAMP(), INTERVAL 1 HOUR) WHERE agent_id=?`, agentID); err != nil {
		t.Fatalf("拨快 last_seen: %v", err)
	}
}

// ---------------------------------------------------------------------------
// FIX 1（P0）：approval_required 落库 + 状态翻 pending_approval + 不可领取
// ---------------------------------------------------------------------------

// TestCreateTask_ApprovalRequired_MySQL 验证高风险任务：
//  1. CreateTask 返回 Status=pending_approval；
//  2. 读回时 ApprovalRequired=true 且 Status 仍是 pending_approval（列真的落库了）；
//  3. ClaimTask 领不到（不进入可下发队列）。
//
// 修前：INSERT 无 approval_required 列、状态恒为 pending ⇒ (2)/(3) 全失败，
// 生产上审批工作流等于不存在。
func TestCreateTask_ApprovalRequired_MySQL(t *testing.T) {
	s := fixesTestStore(t)
	defer s.db.Close()
	const agentID, tenantID = "agent-fix-approval", "tenant-fix"
	cleanupFixTasks(t, s, agentID)
	defer cleanupFixTasks(t, s, agentID)
	registerFixAgent(t, s, agentID, tenantID)

	created, err := s.CreateTask(&proto.Task{
		TaskID: "task-fix-approval", AgentID: agentID, TenantID: tenantID,
		Type: "shell", Command: "dangerous", ApprovalRequired: true,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if created == nil || created.Status != "pending_approval" {
		t.Fatalf("CreateTask 后状态 = %+v, want pending_approval", created)
	}

	// 读回（走 scanTaskListRow 的 AllTasks 路径）：列必须真的落库。
	var got *proto.Task
	for _, tk := range s.AllTasks(tenantID) {
		if tk.TaskID == "task-fix-approval" {
			got = tk
		}
	}
	if got == nil {
		t.Fatal("AllTasks 未读回 approval 任务（列缺失会让读侧静默失败）")
	}
	if !got.ApprovalRequired {
		t.Fatal("读回 ApprovalRequired=false：列未落库/未回扫")
	}
	if got.Status != "pending_approval" {
		t.Fatalf("读回 Status = %q, want pending_approval", got.Status)
	}

	// pending_approval 不可领取。
	if c := s.ClaimTask(agentID); c != nil {
		t.Fatalf("pending_approval 任务不应被领取；got %+v", c)
	}

	// ApproveTask 翻 pending 后才可领取（审批闭环端到端）。
	if !s.ApproveTask("task-fix-approval", tenantID, "u1") {
		t.Fatal("ApproveTask 应返回 true")
	}
	if c := s.ClaimTask(agentID); c == nil || c.TaskID != "task-fix-approval" {
		t.Fatalf("审批后应可领取；got %+v", c)
	}
}

// TestCreateTask_NonApprovalTask_MySQL 验证普通任务不受影响：仍为 pending 且可领取。
func TestCreateTask_NonApprovalTask_MySQL(t *testing.T) {
	s := fixesTestStore(t)
	defer s.db.Close()
	const agentID, tenantID = "agent-fix-normal", "tenant-fix"
	cleanupFixTasks(t, s, agentID)
	defer cleanupFixTasks(t, s, agentID)
	registerFixAgent(t, s, agentID, tenantID)

	created, err := s.CreateTask(&proto.Task{
		TaskID: "task-fix-normal", AgentID: agentID, TenantID: tenantID,
		Type: "shell", Command: "echo hi",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if created.Status != "pending" {
		t.Fatalf("普通任务状态 = %q, want pending", created.Status)
	}
	if created.ApprovalRequired {
		t.Fatal("普通任务 ApprovalRequired 应为 false")
	}
	if c := s.ClaimTask(agentID); c == nil || c.TaskID != "task-fix-normal" {
		t.Fatalf("普通任务应可领取；got %+v", c)
	}
}

// ---------------------------------------------------------------------------
// FIX 4（P1）：读路径必须带出 claimed_at，否则超时判定退化为 CreatedAt
// ---------------------------------------------------------------------------

// TestAllTasks_CarriesClaimedAt_MySQL 验证 ClaimTask 后 AllTasks 读回的 ClaimedAt 非零。
// 这是 monitorTaskTimeouts 用 ClaimedAt 计超时的前提；列漏选时后端恒为零值而无声退化。
func TestAllTasks_CarriesClaimedAt_MySQL(t *testing.T) {
	s := fixesTestStore(t)
	defer s.db.Close()
	const agentID, tenantID = "agent-fix-claimedat", "tenant-fix"
	cleanupFixTasks(t, s, agentID)
	defer cleanupFixTasks(t, s, agentID)
	registerFixAgent(t, s, agentID, tenantID)

	if _, err := s.CreateTask(&proto.Task{
		TaskID: "task-fix-claimedat", AgentID: agentID, TenantID: tenantID,
		Type: "shell", Command: "sleep",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	claimed := s.ClaimTask(agentID)
	if claimed == nil {
		t.Fatal("ClaimTask 未领到任务")
	}
	if claimed.ClaimedAt.IsZero() {
		t.Fatal("ClaimTask 返回值 ClaimedAt 为零")
	}

	var got *proto.Task
	for _, tk := range s.AllTasks("") {
		if tk.TaskID == "task-fix-claimedat" {
			got = tk
		}
	}
	if got == nil {
		t.Fatal("AllTasks 未读回任务")
	}
	if got.ClaimedAt.IsZero() {
		t.Fatal("AllTasks 读回 ClaimedAt 为零：超时监控会退化为按 CreatedAt 计时而误杀长排队任务")
	}
	// TaskByID 直查路径同样要带出（handleDeadLetterTasks 等按 ID 读）。
	byID := s.TaskByID("task-fix-claimedat")
	if byID == nil || byID.ClaimedAt.IsZero() {
		t.Fatal("TaskByID 读回 ClaimedAt 为零")
	}
}

// ---------------------------------------------------------------------------
// FIX 2（P1）：ClaimEpoch 不匹配的上报必须被拒绝
// ---------------------------------------------------------------------------

// TestSubmitResult_RejectsForgedClaimEpoch_MySQL 验证已领取任务收到零值/过期令牌时
// 状态不被改写（修前 epoch==0 跳过校验 ⇒ 任意客户端可越权上报终态）。
func TestSubmitResult_RejectsForgedClaimEpoch_MySQL(t *testing.T) {
	s := fixesTestStore(t)
	defer s.db.Close()
	const agentID, tenantID = "agent-fix-forge", "tenant-fix"
	cleanupFixTasks(t, s, agentID)
	defer cleanupFixTasks(t, s, agentID)
	registerFixAgent(t, s, agentID, tenantID)

	if _, err := s.CreateTask(&proto.Task{
		TaskID: "task-fix-forge", AgentID: agentID, TenantID: tenantID,
		Type: "shell", Command: "echo",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	claimed := s.ClaimTask(agentID)
	if claimed == nil || claimed.ClaimEpoch != 1 {
		t.Fatalf("ClaimTask 应得 epoch=1；got %+v", claimed)
	}

	// 伪造：零值令牌 + 过期令牌都应被拒绝（任务保持 running）。
	for _, forged := range []int64{0, -1, claimed.ClaimEpoch + 1} {
		s.SubmitResult(&proto.TaskResult{
			TaskID: "task-fix-forge", AgentID: agentID, ExitCode: 0, ClaimEpoch: forged,
		})
		got := s.TaskByID("task-fix-forge")
		if got == nil || got.Status != "running" {
			t.Fatalf("越权上报(ClaimEpoch=%d)篡改了状态：%+v", forged, got)
		}
	}

	// 合法令牌被接受。
	s.SubmitResult(&proto.TaskResult{
		TaskID: "task-fix-forge", AgentID: agentID, ExitCode: 0, ClaimEpoch: claimed.ClaimEpoch,
	})
	if got := s.TaskByID("task-fix-forge"); got == nil || got.Status != "done" {
		t.Fatalf("合法上报后应为 done；got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// FIX 5（P1）：上游 failed ⇒ 下游 failed + dead_letter（不再永久 blocked）
// ---------------------------------------------------------------------------

// TestDepDownstreamFailedWhenUpstreamFails_MySQL 验证 A→B 且 A 死信失败后，
// B 从 blocked 变 failed 且 dead_letter=1（死信端点可见）。
func TestDepDownstreamFailedWhenUpstreamFails_MySQL(t *testing.T) {
	s := fixesTestStore(t)
	defer s.db.Close()
	const agentID, tenantID = "agent-fix-depfail", "tenant-fix"
	cleanupFixTasks(t, s, agentID)
	defer cleanupFixTasks(t, s, agentID)
	registerFixAgent(t, s, agentID, tenantID)

	// A：MaxRetries=0 ⇒ 一次失败即死信失败。
	if _, err := s.CreateTask(&proto.Task{
		TaskID: "task-fix-depA", AgentID: agentID, TenantID: tenantID,
		Type: "shell", Command: "boom", MaxRetries: 0,
	}); err != nil {
		t.Fatalf("CreateTask A: %v", err)
	}
	if _, err := s.CreateTask(&proto.Task{
		TaskID: "task-fix-depB", AgentID: agentID, TenantID: tenantID,
		Type: "shell", Command: "down", DependsOn: []string{"task-fix-depA"},
	}); err != nil {
		t.Fatalf("CreateTask B: %v", err)
	}
	if got := s.TaskByID("task-fix-depB"); got == nil || got.Status != "blocked" {
		t.Fatalf("B 初始应为 blocked；got %+v", got)
	}

	claimed := s.ClaimTask(agentID)
	if claimed == nil || claimed.TaskID != "task-fix-depA" {
		t.Fatalf("ClaimTask 未领到 A；got %+v", claimed)
	}
	s.SubmitResult(&proto.TaskResult{
		TaskID: "task-fix-depA", AgentID: agentID, ExitCode: 1,
		ClaimEpoch: claimed.ClaimEpoch,
	})
	if got := s.TaskByID("task-fix-depA"); got == nil || got.Status != "failed" {
		t.Fatalf("A 应 failed；got %+v", got)
	}

	b := s.TaskByID("task-fix-depB")
	if b == nil || b.Status != "failed" {
		t.Fatalf("上游失败后 B 应为 failed；got %+v（blocked 即修复未生效）", b)
	}
	if !b.DeadLetter {
		t.Fatal("B 未标记 dead_letter：不会出现在死信视图，等于静默消失")
	}
	// 死信查询（handleDeadLetterTasks 同口径）必须能看见它。
	found := false
	for _, tk := range s.AllTasks(tenantID) {
		if tk.TaskID == "task-fix-depB" && tk.DeadLetter && tk.Status == "failed" {
			found = true
		}
	}
	if !found {
		t.Fatal("B 未出现在死信查询结果中")
	}
	// B 不再可被领取。
	if c := s.ClaimTask(agentID); c != nil && c.TaskID == "task-fix-depB" {
		t.Fatal("failed 的 B 不应可被领取")
	}
}

// TestDepDownstreamReleasedWhenUpstreamDone_MySQL 回归 happy path：A done ⇒ B pending 可领取。
func TestDepDownstreamReleasedWhenUpstreamDone_MySQL(t *testing.T) {
	s := fixesTestStore(t)
	defer s.db.Close()
	const agentID, tenantID = "agent-fix-depok", "tenant-fix"
	cleanupFixTasks(t, s, agentID)
	defer cleanupFixTasks(t, s, agentID)
	registerFixAgent(t, s, agentID, tenantID)

	if _, err := s.CreateTask(&proto.Task{
		TaskID: "task-fix-okA", AgentID: agentID, TenantID: tenantID,
		Type: "shell", Command: "ok", MaxRetries: 0,
	}); err != nil {
		t.Fatalf("CreateTask A: %v", err)
	}
	if _, err := s.CreateTask(&proto.Task{
		TaskID: "task-fix-okB", AgentID: agentID, TenantID: tenantID,
		Type: "shell", Command: "down", DependsOn: []string{"task-fix-okA"},
	}); err != nil {
		t.Fatalf("CreateTask B: %v", err)
	}
	claimed := s.ClaimTask(agentID)
	if claimed == nil {
		t.Fatal("ClaimTask 未领到 A")
	}
	s.SubmitResult(&proto.TaskResult{
		TaskID: "task-fix-okA", AgentID: agentID, ExitCode: 0, ClaimEpoch: claimed.ClaimEpoch,
	})
	b := s.TaskByID("task-fix-okB")
	if b == nil || b.Status != "pending" {
		t.Fatalf("A done 后 B 应为 pending；got %+v", b)
	}
	if b.DeadLetter {
		t.Fatal("happy path 的 B 不应进死信")
	}
	if c := s.ClaimTask(agentID); c == nil || c.TaskID != "task-fix-okB" {
		t.Fatalf("A done 后 B 应可领取；got %+v", c)
	}
}

// ---------------------------------------------------------------------------
// FIX 6（P1）：ReclaimStaleTasks 计入重试预算，耗尽后死信
// ---------------------------------------------------------------------------

// TestReclaimStaleTasks_DeadLettersAfterBudget_MySQL 验证 MaxRetries=2 时，
// 两次回收后任务 failed + dead_letter 且不再可领取（修前无限复活且永不可见）。
func TestReclaimStaleTasks_DeadLettersAfterBudget_MySQL(t *testing.T) {
	s := fixesTestStore(t)
	defer s.db.Close()
	const agentID, tenantID = "agent-fix-poison", "tenant-fix"
	cleanupFixTasks(t, s, agentID)
	defer cleanupFixTasks(t, s, agentID)
	registerFixAgent(t, s, agentID, tenantID)

	if _, err := s.CreateTask(&proto.Task{
		TaskID: "task-fix-poison", AgentID: agentID, TenantID: tenantID,
		Type: "shell", Command: "hang", MaxRetries: 2,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	const maxAge = 60 // 秒；claimed_at 拨到 1 小时前确保超期
	// 第一次回收：预算 0→1，回 pending。
	claimAndAge(t, s, agentID, "task-fix-poison")
	if n := s.ReclaimStaleTasks(maxAge); n != 1 {
		t.Fatalf("第一次 ReclaimStaleTasks = %d, want 1", n)
	}
	got := s.TaskByID("task-fix-poison")
	if got == nil || got.Status != "pending" || got.RetryCount != 1 {
		t.Fatalf("第一次回收后应 pending 且 retry_count=1；got %+v", got)
	}

	// 第二次回收：预算 1→2 达上限 ⇒ failed + dead_letter。
	claimAndAge(t, s, agentID, "task-fix-poison")
	if n := s.ReclaimStaleTasks(maxAge); n != 1 {
		t.Fatalf("第二次 ReclaimStaleTasks = %d, want 1", n)
	}
	got = s.TaskByID("task-fix-poison")
	if got == nil || got.Status != "failed" || !got.DeadLetter {
		t.Fatalf("预算耗尽后应 failed + dead_letter；got status=%q deadLetter=%v retryCount=%d",
			got.Status, got.DeadLetter, got.RetryCount)
	}
	if got.RetryCount != 2 {
		t.Fatalf("retry_count = %d, want 2（旧实现不计数）", got.RetryCount)
	}

	// 不再可领取 + 出现在死信查询。
	if c := s.ClaimTask(agentID); c != nil {
		t.Fatalf("死信任务不应可被领取；got %+v", c)
	}
	found := false
	for _, tk := range s.AllTasks(tenantID) {
		if tk.TaskID == "task-fix-poison" && tk.DeadLetter && tk.Status == "failed" {
			found = true
		}
	}
	if !found {
		t.Fatal("耗尽预算的任务未出现在死信查询中")
	}
}

// TestReclaimStaleTasks_HeadroomStaysPending_MySQL 回归：MaxRetries 有裕量时
// 回收只计数并回 pending（不误死信）。
func TestReclaimStaleTasks_HeadroomStaysPending_MySQL(t *testing.T) {
	s := fixesTestStore(t)
	defer s.db.Close()
	const agentID, tenantID = "agent-fix-roomy", "tenant-fix"
	cleanupFixTasks(t, s, agentID)
	defer cleanupFixTasks(t, s, agentID)
	registerFixAgent(t, s, agentID, tenantID)

	if _, err := s.CreateTask(&proto.Task{
		TaskID: "task-fix-roomy", AgentID: agentID, TenantID: tenantID,
		Type: "shell", Command: "hang", MaxRetries: 5,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	claimAndAge(t, s, agentID, "task-fix-roomy")
	if n := s.ReclaimStaleTasks(60); n != 1 {
		t.Fatalf("ReclaimStaleTasks = %d, want 1", n)
	}
	got := s.TaskByID("task-fix-roomy")
	if got == nil || got.Status != "pending" || got.DeadLetter {
		t.Fatalf("有裕量时应回 pending 且不进死信；got %+v", got)
	}
	if got.RetryCount != 1 {
		t.Fatalf("retry_count = %d, want 1", got.RetryCount)
	}
}

// claimAndAge 领取指定任务并把 claimed_at 拨到 1 小时前，使其满足回收条件。
func claimAndAge(t *testing.T, s *SQLStore, agentID, taskID string) {
	t.Helper()
	claimed := s.ClaimTask(agentID)
	if claimed == nil || claimed.TaskID != taskID {
		t.Fatalf("ClaimTask 未领到 %s；got %+v", taskID, claimed)
	}
	if _, err := s.db.Exec(
		`UPDATE tasks SET claimed_at = DATE_SUB(UTC_TIMESTAMP(), INTERVAL 1 HOUR) WHERE task_id=?`, taskID); err != nil {
		t.Fatalf("拨快 claimed_at: %v", err)
	}
}
