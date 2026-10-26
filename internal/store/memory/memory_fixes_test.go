// memory_fixes_test.go — P0/P1 修复的行为回归测试（store/state-machine 批次）。
//
// 覆盖：
//   - FIX 3：ListTasksByPriority 排序确定性（priority DESC, createdAt ASC, taskID ASC）
//     —— 修前只按 priority 排，同优先级行序取自 Go map 迭代（随机），offset 分页重复/漏行。
//   - FIX 5：上游失败/取消时，下游 blocked 任务被置 failed + DeadLetter（进死信视图），
//     而不是永久 blocked；全部 done 时照常释放为 pending。
//   - FIX 6：ReclaimStaleTasks 计入重试预算，耗尽后按死信规则置 failed + DeadLetter。
//   - FIX 2：SubmitResult 拒绝 ClaimEpoch 与任务当前 ClaimEpoch 不等的越权上报
//     （含攻击者发 ClaimEpoch=0 伪造他人正在执行任务的结果）。
//
// 风格与 memory_crud_test.go 一致：白盒（package memory）。
package memory

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// newFixAgent 注册一个 tenant=t1 的 agent 供任务下发。
func newFixAgent(t *testing.T, m *MemoryStore) *proto.AgentInfo {
	t.Helper()
	return m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
}

// ---------------------------------------------------------------------------
// FIX 3：分页排序确定性
// ---------------------------------------------------------------------------

// TestListTasksByPriority_DeterministicAcrossPages 验证 5 条混合优先级任务
// 以 limit=2 分页时：页与页之间不重叠、不遗漏、顺序稳定，且同优先级保持插入序。
//
// 修前：排序键只有 Priority，同优先级行的相对顺序来自 m.tasks 的 map 迭代（随机），
// 于是每次调用 ListTasksByPriority 都得到不同顺序 → offset 分页重复/漏行。
func TestListTasksByPriority_DeterministicAcrossPages(t *testing.T) {
	m := NewMemoryStore()
	a := newFixAgent(t, m)

	// 混合优先级：两条 p5、一条 p1、一条 p9、一条 p9；同优先级的 p9 有明确的插入先后。
	specs := []struct {
		id       string
		priority int
	}{
		{"t-p5-a", 5},
		{"t-p1", 1},
		{"t-p9-a", 9},
		{"t-p5-b", 5},
		{"t-p9-b", 9},
	}
	base := time.Now().Add(-time.Hour)
	for i, s := range specs {
		// CreatedAt 递增但与 Priority 不相关，确保排序键真正由 Priority 主导、
		// 同 Priority 内由 CreatedAt / TaskID 决胜（而不是碰巧与插入序一致）。
		if _, err := m.CreateTask(&proto.Task{
			TaskID: s.id, AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo",
			Priority: s.priority, CreatedAt: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("CreateTask %s: %v", s.id, err)
		}
	}

	// 期望全量顺序：priority DESC → createdAt ASC → taskID ASC。
	// p9：t-p9-a（早于 t-p9-b）；p5：t-p5-a（早于 t-p5-b）；p1：t-p1。
	wantAll := []string{"t-p9-a", "t-p9-b", "t-p5-a", "t-p5-b", "t-p1"}

	// 连续翻页（limit=2）收集全部，且逐页比对顺序。
	var got []string
	for offset := 0; offset < len(wantAll); offset += 2 {
		page, total, err := m.ListTasksByPriority("", offset, 2)
		if err != nil {
			t.Fatalf("ListTasksByPriority(offset=%d): %v", offset, err)
		}
		if total != len(wantAll) {
			t.Fatalf("total = %d, want %d", total, len(wantAll))
		}
		if len(page) > 2 {
			t.Fatalf("limit=2 但返回 %d 条", len(page))
		}
		for _, tk := range page {
			got = append(got, tk.TaskID)
		}
	}
	if len(got) != len(wantAll) {
		t.Fatalf("分页取回 %d 条，want %d（%v）", len(got), len(wantAll), got)
	}
	for i := range wantAll {
		if got[i] != wantAll[i] {
			t.Fatalf("分页顺序 = %v, want %v（第 %d 位 %s != %s）", got, wantAll, i, got[i], wantAll[i])
		}
	}

	// 重复调用必须给出同一序列（修前 map 迭代随机，此处会偶发失败）。
	for round := 0; round < 20; round++ {
		first, _, err := m.ListTasksByPriority("", 0, 10)
		if err != nil {
			t.Fatalf("ListTasksByPriority: %v", err)
		}
		for i, tk := range first {
			if tk.TaskID != wantAll[i] {
				t.Fatalf("第 %d 轮顺序漂移：pos %d = %s, want %s", round, i, tk.TaskID, wantAll[i])
			}
		}
	}
}

// TestListTasksByPriority_EqualPriorityKeepsInsertionOrder 专门钉住同优先级行的
// 相对顺序稳定（不重复、不交换）。
func TestListTasksByPriority_EqualPriorityKeepsInsertionOrder(t *testing.T) {
	m := NewMemoryStore()
	a := newFixAgent(t, m)
	base := time.Now().Add(-time.Hour)
	const n = 6
	for i := 0; i < n; i++ {
		id := "same-" + string(rune('a'+i))
		if _, err := m.CreateTask(&proto.Task{
			TaskID: id, AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo",
			Priority: 3, CreatedAt: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
	}
	// 逐条翻（limit=1）累计顺序。
	var got []string
	for offset := 0; offset < n; offset++ {
		page, total, err := m.ListTasksByPriority("", offset, 1)
		if err != nil {
			t.Fatalf("ListTasksByPriority(offset=%d): %v", offset, err)
		}
		if total != n {
			t.Fatalf("total = %d, want %d", total, n)
		}
		if len(page) != 1 {
			t.Fatalf("limit=1 返回 %d 条", len(page))
		}
		got = append(got, page[0].TaskID)
	}
	seen := map[string]bool{}
	for i, id := range got {
		if seen[id] {
			t.Fatalf("同优先级任务 %s 在第 %d 页重复出现（offset 分页漂移）", id, i)
		}
		seen[id] = true
		if id != "same-"+string(rune('a'+i)) {
			t.Fatalf("插入序未保持：pos %d = %s, want %c", i, id, 'a'+i)
		}
	}
}

// ---------------------------------------------------------------------------
// FIX 5：上游失败/取消时下游的处置
// ---------------------------------------------------------------------------

// submitDepOutcome 构造 A→B（B dependsOn A），让 A 到达 outcome，随后上报结果。
// 返回最终状态下的 B。
func submitDepOutcome(t *testing.T, m *MemoryStore, a *proto.AgentInfo, outcome string) *proto.Task {
	t.Helper()
	up, err := m.CreateTask(&proto.Task{
		TaskID: "dep-up", AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "up",
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatalf("CreateTask up: %v", err)
	}
	down, err := m.CreateTask(&proto.Task{
		TaskID: "dep-down", AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "down",
		DependsOn: []string{"dep-up"},
	})
	if err != nil {
		t.Fatalf("CreateTask down: %v", err)
	}
	if down.Status != "blocked" {
		t.Fatalf("下游初始状态 = %q, want blocked", down.Status)
	}

	// 让上游 A 到达指定终局。
	switch outcome {
	case "done", "failed":
		// MaxRetries=0 ⇒ 一次失败即死信（与 SubmitResult 的预算规则一致）。
		claimed := m.ClaimTask(a.AgentID)
		if claimed == nil || claimed.TaskID != up.TaskID {
			t.Fatalf("ClaimTask 未领到上游任务")
		}
		exit := 1
		if outcome == "done" {
			exit = 0
		}
		m.SubmitResult(&proto.TaskResult{
			TaskID: up.TaskID, AgentID: a.AgentID, ExitCode: exit,
			ClaimEpoch: claimed.ClaimEpoch,
		})
		upFinal := m.TaskByID("dep-up")
		wantUp := "done"
		if outcome == "failed" {
			wantUp = "failed"
		}
		if upFinal.Status != wantUp {
			t.Fatalf("上游终态 = %q, want %q", upFinal.Status, wantUp)
		}
	case "cancelled":
		// 取消属于终态变更：CancelTask 内部重评估下游依赖（与 SubmitResult 同源路径）。
		if !m.CancelTask(up.TaskID, "t1") {
			t.Fatal("CancelTask 上游失败")
		}
	default:
		t.Fatalf("unknown outcome %q", outcome)
	}
	return m.TaskByID("dep-down")
}

// TestDepDownstreamFailedWhenUpstreamFails 验证 A→B 且 A FAILS ⇒ B 为 failed（不卡 blocked）
// 且出现在死信查询里。
func TestDepDownstreamFailedWhenUpstreamFails(t *testing.T) {
	m := NewMemoryStore()
	a := newFixAgent(t, m)
	down := submitDepOutcome(t, m, a, "failed")
	if down.Status != "failed" {
		t.Fatalf("下游状态 = %q, want failed", down.Status)
	}
	if !down.DeadLetter {
		t.Fatal("下游未标记 DeadLetter：不会出现在死信视图，等于静默消失")
	}
	// 死信查询（与 handleDeadLetterTasks 同口径）必须能看见它。
	found := false
	for _, tk := range m.AllTasks("") {
		if tk.TaskID == "dep-down" && tk.DeadLetter && tk.Status == "failed" {
			found = true
		}
	}
	if !found {
		t.Fatal("下游未出现在死信查询结果中")
	}
	// 失败的下游不可再被领取。
	if claimed := m.ClaimTask(a.AgentID); claimed != nil && claimed.TaskID == "dep-down" {
		t.Fatal("failed 下游不应可被领取")
	}
}

// TestDepDownstreamFailedWhenUpstreamCancelled 验证 A→B 且 A CANCELLED ⇒ B 同样
// failed + 死信（修前永远 blocked）。
func TestDepDownstreamFailedWhenUpstreamCancelled(t *testing.T) {
	m := NewMemoryStore()
	a := newFixAgent(t, m)
	down := submitDepOutcome(t, m, a, "cancelled")
	if down.Status != "failed" {
		t.Fatalf("下游状态 = %q, want failed", down.Status)
	}
	if !down.DeadLetter {
		t.Fatal("下游未标记 DeadLetter：不会出现在死信视图，等于静默消失")
	}
}

// TestDepDownstreamReleasedWhenUpstreamDone 验证 happy path：A done ⇒ B 为可领取的 pending。
func TestDepDownstreamReleasedWhenUpstreamDone(t *testing.T) {
	m := NewMemoryStore()
	a := newFixAgent(t, m)
	down := submitDepOutcome(t, m, a, "done")
	if down.Status != "pending" {
		t.Fatalf("下游状态 = %q, want pending（上游 done 应释放）", down.Status)
	}
	if down.DeadLetter {
		t.Fatal("happy path 下游不应进死信")
	}
	claimed := m.ClaimTask(a.AgentID)
	if claimed == nil || claimed.TaskID != "dep-down" {
		t.Fatalf("上游 done 后下游应可领取；got %+v", claimed)
	}
}

// TestDepDownstreamStaysBlockedWhileUpstreamRunning 验证上游仍在 running 时，
// 下游保持 blocked（不被提前释放，也不被误判失败）。
func TestDepDownstreamStaysBlockedWhileUpstreamRunning(t *testing.T) {
	m := NewMemoryStore()
	a := newFixAgent(t, m)
	if _, err := m.CreateTask(&proto.Task{
		TaskID: "run-up", AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "up",
		MaxRetries: 3,
	}); err != nil {
		t.Fatalf("CreateTask up: %v", err)
	}
	down, err := m.CreateTask(&proto.Task{
		TaskID: "run-down", AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "down",
		DependsOn: []string{"run-up"},
	})
	if err != nil {
		t.Fatalf("CreateTask down: %v", err)
	}
	if down.Status != "blocked" {
		t.Fatalf("下游初始 = %q, want blocked", down.Status)
	}
	// 上游被领取（running）但尚未上报：ClaimTask 只作用于 pending，不触发依赖重算；
	// 下游必须继续保持 blocked（ClaimTask 领取上游不应顺带释放下游）。
	claimed := m.ClaimTask(a.AgentID)
	if claimed == nil || claimed.TaskID != "run-up" {
		t.Fatalf("ClaimTask 未领到上游；got %+v", claimed)
	}
	if claimed.Status != "running" {
		t.Fatalf("上游状态 = %q, want running", claimed.Status)
	}
	// 上游失败但仍有重试余量（回到 pending，未达终态）：下游仍须 blocked。
	m.SubmitResult(&proto.TaskResult{
		TaskID: "run-up", AgentID: a.AgentID, ExitCode: 1, ClaimEpoch: claimed.ClaimEpoch,
	})
	if got := m.TaskByID("run-up"); got.Status != "pending" {
		t.Fatalf("有重试余量时上游应回 pending；got %q", got.Status)
	}
	got := m.TaskByID("run-down")
	if got.Status != "blocked" {
		t.Fatalf("上游 running/pending(未达终态) 时下游 = %q, want blocked", got.Status)
	}
	if got.DeadLetter {
		t.Fatal("上游未达终态时下游不应进死信")
	}
}

// ---------------------------------------------------------------------------
// FIX 6：stale reclaim 计入重试预算
// ---------------------------------------------------------------------------

// staleAgent 注册一个心跳超时的 agent（Register 会把 LastSeen 置为 now，此处拨回过去）。
func staleAgent(t *testing.T, m *MemoryStore) *proto.AgentInfo {
	t.Helper()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	m.mu.Lock()
	m.agents[a.AgentID].LastSeen = time.Now().Add(-time.Hour)
	m.mu.Unlock()
	return a
}

// claimAndStale 领取任务并把 ClaimedAt 拨到 maxAge 之前，使其满足回收条件
// （agent 心跳已超时 + claimed_at 超期）。
func claimAndStale(t *testing.T, m *MemoryStore, agentID, taskID string, maxAge time.Duration) {
	t.Helper()
	claimed := m.ClaimTask(agentID)
	if claimed == nil || claimed.TaskID != taskID {
		t.Fatalf("ClaimTask 未领到 %s；got %+v", taskID, claimed)
	}
	m.mu.Lock()
	ts := m.tasks[agentID]
	for _, x := range ts {
		if x.TaskID == taskID {
			x.ClaimedAt = time.Now().Add(-2 * maxAge)
		}
	}
	m.mu.Unlock()
}

// TestReclaimStaleTasks_CountsAgainstRetryBudget 验证 MaxRetries=2 时，
// 回收计满预算后任务为 failed + DeadLetter 且不再可领取。
func TestReclaimStaleTasks_CountsAgainstRetryBudget(t *testing.T) {
	m := NewMemoryStore()
	a := staleAgent(t, m)
	tk, err := m.CreateTask(&proto.Task{
		TaskID: "poison", AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "hang",
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	const maxAge = time.Minute

	// 第一次回收：budget 0→1，有余量 ⇒ 回到 pending，可再次领取。
	claimAndStale(t, m, a.AgentID, "poison", maxAge)
	if n := m.ReclaimStaleTasks(maxAge); n != 1 {
		t.Fatalf("第一次 ReclaimStaleTasks = %d, want 1", n)
	}
	after1 := m.TaskByID("poison")
	if after1.Status != "pending" {
		t.Fatalf("第一次回收后状态 = %q, want pending", after1.Status)
	}
	if after1.RetryCount != 1 {
		t.Fatalf("第一次回收后 RetryCount = %d, want 1（旧实现不计数，恒为 0）", after1.RetryCount)
	}

	// 第二次回收：budget 1→2，达上限 ⇒ 置 failed + DeadLetter。
	claimAndStale(t, m, a.AgentID, "poison", maxAge)
	if n := m.ReclaimStaleTasks(maxAge); n != 1 {
		t.Fatalf("第二次 ReclaimStaleTasks = %d, want 1", n)
	}
	after2 := m.TaskByID("poison")
	if after2.RetryCount != 2 {
		t.Fatalf("第二次回收后 RetryCount = %d, want 2", after2.RetryCount)
	}
	if after2.Status != "failed" || !after2.DeadLetter {
		t.Fatalf("达上限后应为 failed + DeadLetter；got status=%q deadLetter=%v", after2.Status, after2.DeadLetter)
	}

	// 第三次回收：已是 failed（非 running）⇒ 无任务可回收（0）。
	if n := m.ReclaimStaleTasks(time.Minute); n != 0 {
		t.Fatalf("failed 任务不应再被回收；got %d", n)
	}

	// 不可领取 + 出现在死信查询。
	if claimed := m.ClaimTask(a.AgentID); claimed != nil {
		t.Fatalf("死信任务不应可被领取；got %+v", claimed)
	}
	found := false
	for _, x := range m.AllTasks("") {
		if x.TaskID == tk.TaskID && x.DeadLetter && x.Status == "failed" {
			found = true
		}
	}
	if !found {
		t.Fatal("耗尽预算的任务未出现在死信查询中")
	}
}

// TestReclaimStaleTasks_HeadroomKeepsPending 验证 MaxRetries 有裕量时，
// 回收只计数并回到 pending（不误死信）。
func TestReclaimStaleTasks_HeadroomKeepsPending(t *testing.T) {
	m := NewMemoryStore()
	a := staleAgent(t, m)
	if _, err := m.CreateTask(&proto.Task{
		TaskID: "roomy", AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "hang",
		MaxRetries: 5,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	claimAndStale(t, m, a.AgentID, "roomy", time.Minute)
	if n := m.ReclaimStaleTasks(time.Minute); n != 1 {
		t.Fatalf("ReclaimStaleTasks = %d, want 1", n)
	}
	got := m.TaskByID("roomy")
	if got.Status != "pending" {
		t.Fatalf("有裕量时应回 pending；got %q", got.Status)
	}
	if got.RetryCount != 1 {
		t.Fatalf("RetryCount = %d, want 1", got.RetryCount)
	}
	if got.DeadLetter {
		t.Fatal("有裕量时不应进死信")
	}
}

// TestReclaimStaleTasks_HealthyAgentNotReclaimed 回归：心跳正常的 agent 不被回收
// （防长任务被误回收导致双跑）。
func TestReclaimStaleTasks_HealthyAgentNotReclaimed(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1", LastSeen: time.Now()})
	if _, err := m.CreateTask(&proto.Task{
		TaskID: "slow", AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "sleep 999",
		MaxRetries: 2,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	m.ClaimTask(a.AgentID)
	if n := m.ReclaimStaleTasks(time.Minute); n != 0 {
		t.Fatalf("心跳正常的 agent 不应被回收；got %d", n)
	}
}

// ---------------------------------------------------------------------------
// FIX 2：越权上报拒绝
// ---------------------------------------------------------------------------

// TestSubmitResult_RejectsForgedClaimEpoch 验证 ClaimEpoch 与任务当前值不等的上报被拒绝。
// 攻击场景：agent-a 已领取任务（epoch=1），攻击者对同一 TaskID 发 ClaimEpoch=0
// 试图把任务直接标成 done/failed。修前 res.ClaimEpoch>0 才校验 ⇒ 0 直接绕过。
func TestSubmitResult_RejectsForgedClaimEpoch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		forged []int64
	}{
		{"zero token", []int64{0}},
		{"negative token", []int64{-1}},
		{"stale holder", []int64{2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMemoryStore()
			a := newFixAgent(t, m)
			if _, err := m.CreateTask(&proto.Task{
				TaskID: "owned", AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo",
			}); err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			claimed := m.ClaimTask(a.AgentID)
			if claimed == nil || claimed.ClaimEpoch != 1 {
				t.Fatalf("领取应得 epoch=1；got %+v", claimed)
			}
			for _, forged := range tc.forged {
				m.SubmitResult(&proto.TaskResult{
					TaskID: "owned", AgentID: a.AgentID, ExitCode: 0,
					ClaimEpoch: forged,
				})
				got := m.TaskByID("owned")
				if got.Status != "running" {
					t.Fatalf("越权上报(ClaimEpoch=%d)篡改了任务状态：%q", forged, got.Status)
				}
			}
		})
	}
}

// TestSubmitResult_AcceptsLegitimateEpoch 回归：持正确令牌的上报被接受。
func TestSubmitResult_AcceptsLegitimateEpoch(t *testing.T) {
	m := NewMemoryStore()
	a := newFixAgent(t, m)
	if _, err := m.CreateTask(&proto.Task{
		TaskID: "ok", AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	claimed := m.ClaimTask(a.AgentID)
	if claimed == nil {
		t.Fatal("ClaimTask 未领到任务")
	}
	m.SubmitResult(&proto.TaskResult{
		TaskID: "ok", AgentID: a.AgentID, ExitCode: 0, ClaimEpoch: claimed.ClaimEpoch,
	})
	if got := m.TaskByID("ok"); got.Status != "done" {
		t.Fatalf("合法上报后状态 = %q, want done", got.Status)
	}
}

// TestSubmitResult_ZeroEpochAllowedOnlyWhenNeverClaimed 验证零值令牌只在
// "任务从未被领取"（ClaimEpoch==0）时放行——兼容直接置 Status=running 的构造路径。
func TestSubmitResult_ZeroEpochAllowedOnlyWhenNeverClaimed(t *testing.T) {
	m := NewMemoryStore()
	a := newFixAgent(t, m)
	tk, err := m.CreateTask(&proto.Task{
		TaskID: "never-claimed", AgentID: a.AgentID, TenantID: "t1",
		Type: "shell", Command: "echo", Status: "running",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if tk.ClaimEpoch != 0 {
		t.Fatalf("未领取任务 ClaimEpoch = %d, want 0", tk.ClaimEpoch)
	}
	m.SubmitResult(&proto.TaskResult{TaskID: tk.TaskID, AgentID: a.AgentID, ExitCode: 0})
	if got := m.TaskByID(tk.TaskID); got.Status != "done" {
		t.Fatalf("零值令牌+未领取应被放行；got %q", got.Status)
	}
}
