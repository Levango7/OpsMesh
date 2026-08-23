package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

func TestMemoryStore_RegisterAssignsID(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a"})
	if a.AgentID == "" {
		t.Fatal("expected server-assigned agentID")
	}
	if a.Status != "online" {
		t.Fatalf("Status = %q, want online", a.Status)
	}
}

// TestMemoryStore_LeaderAlwaysTrue 单实例（MemoryStore）恒为 leader，
// RenewLeadership 任意 ttl 均返回 true，IsLeader 恒 true。
func TestMemoryStore_LeaderAlwaysTrue(t *testing.T) {
	m := NewMemoryStore()
	if !m.RenewLeadership(0) {
		t.Fatal("MemoryStore.RenewLeadership(0) = false, want true")
	}
	if !m.RenewLeadership(15 * time.Second) {
		t.Fatal("MemoryStore.RenewLeadership(15s) = false, want true")
	}
	if !m.IsLeader() {
		t.Fatal("MemoryStore.IsLeader() = false, want true")
	}
}

// TestMemoryStore_CancelledTaskIDs F3 取消信号下发：取消一个任务后，
// CancelledTaskIDs 仅返回该任务，未取消任务不出现。
func TestMemoryStore_CancelledTaskIDs(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	t1 := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo 1"})
	_ = m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo 2"})

	if got := m.CancelledTaskIDs(a.AgentID); len(got) != 0 {
		t.Fatalf("before cancel: %v, want empty", got)
	}
	if !m.CancelTask(t1.TaskID, "t1") {
		t.Fatal("CancelTask(t1) = false")
	}
	got := m.CancelledTaskIDs(a.AgentID)
	if len(got) != 1 || got[0] != t1.TaskID {
		t.Fatalf("after cancel: %v, want [%s]", got, t1.TaskID)
	}
	// 仅查询本 agent；其他 agent 不受影响
	a2 := m.Register(&proto.AgentInfo{Segment: "seg-b", TenantID: "t2"})
	if got := m.CancelledTaskIDs(a2.AgentID); len(got) != 0 {
		t.Fatalf("other agent CancelledTaskIDs = %v, want empty", got)
	}
}

// TestMemoryStore_RetireStaleDevices F5 离线超龄自动归档：最后心跳早于阈值的
// agent 对应设备被批量 retired；在线设备与已退役设备不受影响。
func TestMemoryStore_RetireStaleDevices(t *testing.T) {
	m := NewMemoryStore()
	// 在线 agent（心跳刚刚）
	live := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1", Status: "online"})
	// 离线 agent（心跳已过去 2 小时）
	dead := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1", Status: "online"})
	m.Heartbeat(dead.AgentID, "offline", 1)
	m.mu.Lock()
	m.agents[dead.AgentID].LastSeen = time.Now().Add(-2 * time.Hour)
	m.mu.Unlock()

	n := m.RetireStaleDevices(1 * time.Hour)
	if n != 1 {
		t.Fatalf("archived = %d, want 1 (仅离线 agent 的设备）", n)
	}
	// 离线设备已 retired
	if dev := m.Device("dev-" + dead.AgentID); !dev.Retired {
		t.Fatal("离线设备未被 retired")
	}
	// 在线设备仍在活跃清单
	if dev := m.Device("dev-" + live.AgentID); dev.Retired {
		t.Fatal("在线设备不应被归档")
	}
	// <=0 阈值关闭归档
	if m.RetireStaleDevices(0) != 0 {
		t.Fatal("maxAge<=0 应返回 0（关闭）")
	}
}

func TestMemoryStore_TenantFilter_Agents(t *testing.T) {
	m := NewMemoryStore()
	m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t2"})

	if got := m.Agents("t1"); len(got) != 1 || got[0].TenantID != "t1" {
		t.Fatalf("Agents(t1) = %+v, want exactly 1 with tenant t1", got)
	}
	if got := m.Agents("t2"); len(got) != 1 || got[0].TenantID != "t2" {
		t.Fatalf("Agents(t2) = %+v, want exactly 1 with tenant t2", got)
	}
	if got := m.Agents(""); len(got) != 2 {
		t.Fatalf("Agents(\"\") = %d, want 2 (no filter)", len(got))
	}
}

func TestMemoryStore_TenantFilter_Snapshot(t *testing.T) {
	m := NewMemoryStore()
	m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t2"})

	snap := m.Snapshot("t1")
	total := 0
	for _, devs := range snap {
		for _, d := range devs {
			total++
			if d.TenantID != "t1" {
				t.Fatalf("Snapshot(t1) leaked device tenant=%q", d.TenantID)
			}
		}
	}
	if total != 1 {
		t.Fatalf("Snapshot(t1) device count = %d, want 1", total)
	}
	if total2 := countDevices(m.Snapshot("")); total2 != 2 {
		t.Fatalf("Snapshot(\"\") device count = %d, want 2", total2)
	}
}

func countDevices(m map[string][]proto.DeviceInfo) int {
	n := 0
	for _, ds := range m {
		n += len(ds)
	}
	return n
}

func TestMemoryStore_PresetTask(t *testing.T) {
	m := NewMemoryStore().WithDemo(true)
	a := m.Register(&proto.AgentInfo{Segment: "seg-a"})
	ts := m.GetTasks(a.AgentID)
	if len(ts) != 1 {
		t.Fatalf("preset tasks = %d, want 1", len(ts))
	}
	if ts[0].Command != "uname -a" {
		t.Fatalf("Command = %q, want uname -a", ts[0].Command)
	}
}

// TestMemoryStore_DAGReleaseBlockedToPending 验证 M5 作业编排的 blocked→release 调度：
// 含前置依赖的任务初始为 block ed，当前置依赖 done 后由 SubmitResult 自动释放为 pending。
// 这是 dag.AllDepsDone + store.releaseDeps 端到端链路的唯一覆盖测试（dag_test.go 仅测纯函数）。
func TestMemoryStore_DAGReleaseBlockedToPending(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})

	ta := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "step1"})
	if ta.Status != "pending" {
		t.Fatalf("task A status = %q, want pending", ta.Status)
	}

	tb := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell",
		Command: "step2", DependsOn: []string{ta.TaskID}})
	if tb.Status != "blocked" {
		t.Fatalf("task B (has deps) status = %q, want blocked", tb.Status)
	}
	// 依赖未达成前，B 必须保持 blocked（不能被提前下发）。
	if tb.Status == "pending" {
		t.Fatal("task B released before dependency done")
	}

	// A 完成 → B 应被自动释放为 pending（进入可下发队列）。
	// 状态守卫：SubmitResult 仅接受 running 任务，须先领取。
	m.ClaimTask(a.AgentID)
	m.SubmitResult(&proto.TaskResult{TaskID: ta.TaskID, AgentID: a.AgentID, ExitCode: 0})
	if ta.Status != "done" {
		t.Fatalf("task A status = %q, want done", ta.Status)
	}
	if tb.Status != "pending" {
		t.Fatalf("after A done: task B status = %q, want pending (release failed)", tb.Status)
	}
}

func TestMemoryStore_SubmitResult(t *testing.T) {
	m := NewMemoryStore().WithDemo(true)
	a := m.Register(&proto.AgentInfo{Segment: "seg-a"})
	ts := m.GetTasks(a.AgentID)
	m.ClaimTask(a.AgentID) // 状态守卫：上报前须先领取（pending→running）
	m.SubmitResult(&proto.TaskResult{TaskID: ts[0].TaskID, AgentID: a.AgentID, ExitCode: 0})

	for _, ds := range m.Snapshot("") {
		for _, d := range ds {
			if d.AgentID == a.AgentID && d.TaskState != "done" {
				t.Fatalf("device TaskState = %q, want done", d.TaskState)
			}
		}
	}
}

// TestMemoryStore_TaskLifecycle 验证 修复：SubmitResult 后任务不再被重复下发。
func TestMemoryStore_TaskLifecycle(t *testing.T) {
	m := NewMemoryStore().WithDemo(true)
	a := m.Register(&proto.AgentInfo{Segment: "seg-a"})
	if got := m.GetTasks(a.AgentID); len(got) != 1 {
		t.Fatalf("before submit: GetTasks = %d, want 1", len(got))
	}
	m.ClaimTask(a.AgentID) // 状态守卫：上报前须先领取
	m.SubmitResult(&proto.TaskResult{TaskID: "task-" + a.AgentID + "-1", AgentID: a.AgentID, ExitCode: 0})
	if got := m.GetTasks(a.AgentID); len(got) != 0 {
		t.Fatalf("after submit: GetTasks = %d, want 0 (no re-run)", len(got))
	}
}

// TestMemoryStore_CreateTask 验证 内部下发入口：分配 ID、status=pending、可被拉取。
func TestMemoryStore_CreateTask(t *testing.T) {
	m := NewMemoryStore().WithDemo(true)
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	created := m.CreateTask(&proto.Task{AgentID: a.AgentID, Type: "shell", Command: "echo hi", TenantID: "t1"})
	if created.TaskID == "" {
		t.Fatal("expected server-assigned taskID")
	}
	if created.Status != "pending" {
		t.Fatalf("Status = %q, want pending", created.Status)
	}
	if got := m.GetTasks(a.AgentID); len(got) != 2 { // 预置 1 + 下发 1
		t.Fatalf("GetTasks after CreateTask = %d, want 2", len(got))
	}
}

// TestMemoryStore_Audit 验证 审计产出：事件被记录且补默时间。
func TestMemoryStore_Audit(t *testing.T) {
	m := NewMemoryStore()
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "register", Target: "agent-1"})
	if len(m.audits) != 1 {
		t.Fatalf("audits = %d, want 1", len(m.audits))
	}
	if m.audits[0].CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
}

// TestMemoryStore_ClaimTask 验证 原子领取：首次领取翻转 running，二次返回 nil（不双领）。
func TestMemoryStore_ClaimTask(t *testing.T) {
	m := NewMemoryStore().WithDemo(true)
	a := m.Register(&proto.AgentInfo{Segment: "seg-a"})
	got := m.ClaimTask(a.AgentID)
	if got == nil {
		t.Fatal("first ClaimTask = nil, want task")
	}
	if got.Status != "running" {
		t.Fatalf("claimed Status = %q, want running", got.Status)
	}
	if m.ClaimTask(a.AgentID) != nil {
		t.Fatal("second ClaimTask should be nil (no double-claim)")
	}
	// 下发新 pending 任务后可被领取
	m.CreateTask(&proto.Task{AgentID: a.AgentID, Type: "shell", Command: "echo hi", TenantID: "t1"})
	if m.ClaimTask(a.AgentID) == nil {
		t.Fatal("expected newly created pending task to be claimable")
	}
}

// TestMemoryStore_UpsertDevice 验证 真实纳管：设备可写入并按 deviceID 幂等更新。
func TestMemoryStore_UpsertDevice(t *testing.T) {
	m := NewMemoryStore()
	m.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-10.30.0.5", Segment: "seg-a", TenantID: "t1", IP: "10.30.0.5", AgentID: "agent-x", State: "online", TaskState: "idle"})
	m.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-10.30.0.5", Segment: "seg-a", TenantID: "t1", IP: "10.30.0.5", AgentID: "agent-x", State: "online", TaskState: "done"})
	snap := m.Snapshot("t1")
	n := 0
	for _, devs := range snap {
		for _, d := range devs {
			n++
			if d.TaskState != "done" {
				t.Fatalf("UpsertDevice 未幂等更新，TaskState=%q", d.TaskState)
			}
		}
	}
	if n != 1 {
		t.Fatalf("UpsertDevice 设备数 = %d, want 1（幂等）", n)
	}
}

// TestMemoryStore_PendingDepth 验证 队列深度：注册后=1，领取后=0。
func TestMemoryStore_PendingDepth(t *testing.T) {
	m := NewMemoryStore().WithDemo(true)
	a := m.Register(&proto.AgentInfo{Segment: "seg-a"})
	if d := m.PendingDepth(); d != 1 {
		t.Fatalf("PendingDepth = %d, want 1", d)
	}
	m.ClaimTask(a.AgentID)
	if d := m.PendingDepth(); d != 0 {
		t.Fatalf("PendingDepth after claim = %d, want 0", d)
	}
}

// recordingBus 是测试用的内存事件总线，记录所有发布的事件。
type recordingBus struct {
	events []events.Event
}

func (b *recordingBus) Publish(_ context.Context, e events.Event) error {
	b.events = append(b.events, e)
	return nil
}

// TestMemoryStore_QueryMethods 验证功能补全：AllTasks / Device / Results 三个查询方法。
func TestMemoryStore_QueryMethods(t *testing.T) {
	m := NewMemoryStore().WithDemo(true)
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})

	// 下发一条任务并上报结果，构造可查询的数据。
	created := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo hi"})
	// 状态守卫：上报前须先领取；ClaimTask 按创建顺序返回，先领走 demo 任务、再领到本条下发任务。
	m.ClaimTask(a.AgentID)
	m.ClaimTask(a.AgentID)
	m.SubmitResult(&proto.TaskResult{TaskID: created.TaskID, AgentID: a.AgentID, ExitCode: 0, Stdout: "hi"})

	all := m.AllTasks("t1")
	if len(all) != 2 { // 预置 1 + 下发 1
		t.Fatalf("AllTasks(t1) = %d, want 2", len(all))
	}
	if got := m.AllTasks("other-tenant"); len(got) != 0 {
		t.Fatalf("AllTasks(other) 应被租户隔离，得到 %d 条", len(got))
	}

	dev := m.Device("dev-" + a.AgentID)
	if dev == nil || dev.AgentID != a.AgentID {
		t.Fatalf("Device 查询失败: %+v", dev)
	}
	if m.Device("nope") != nil {
		t.Fatal("未知 deviceID 应返回 nil")
	}

	results := m.Results(a.AgentID)
	if len(results) != 1 || results[0].Stdout != "hi" {
		t.Fatalf("Results 查询失败: %+v", results)
	}
}

// TestMemoryStore_Agent 验证 ：O(1) 直查 + 深拷贝隔离。
// 返回副本被篡改不应影响内部存储，未知 id 必须返回 nil。
func TestMemoryStore_Agent(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-x", TenantID: "t9"})

	got := m.Agent(a.AgentID)
	if got == nil {
		t.Fatalf("Agent(%q) 应为非 nil", a.AgentID)
	}
	if got.AgentID != a.AgentID || got.Segment != "seg-x" {
		t.Fatalf("Agent 字段不匹配: %+v", got)
	}

	// 深拷贝隔离：修改返回值不影响内部。
	got.Segment = "MUTATED"
	inner := m.Agent(a.AgentID)
	if inner == nil || inner.Segment != "seg-x" {
		t.Fatalf("Agent(id) 未做深拷贝：内部 Segment 被改成 %q", inner.Segment)
	}

	if m.Agent("no-such-id") != nil {
		t.Fatal("未知 id 的 Agent 应返回 nil")
	}
}

// TestMemoryStore_EventPublish 验证事件总线接入：Register/CreateTask/SubmitResult 均经总线发布。
func TestMemoryStore_EventPublish(t *testing.T) {
	bus := &recordingBus{}
	m := NewMemoryStore().WithBus(bus)
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	created := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo hi"})
	m.ClaimTask(a.AgentID) // 状态守卫：上报前须先领取
	m.SubmitResult(&proto.TaskResult{TaskID: created.TaskID, AgentID: a.AgentID, ExitCode: 0})

	actions := map[string]bool{}
	for _, e := range bus.events {
		actions[e.Action] = true
	}
	for _, want := range []string{"register", "create_task", "report_result"} {
		if !actions[want] {
			t.Fatalf("缺失事件 %q；已发布: %+v", want, bus.events)
		}
	}
}

// TestMemoryStore_ReclaimStaleTasks 验证 任务必达：超期 running 任务被复位 pending 重调度。
// 心跳守卫：仅当 claimed_by 对应的 agent 也心跳超时时才回收，防止长任务被误回收双跑。
func TestMemoryStore_ReclaimStaleTasks(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	// 显式下发一条任务供领取（不依赖演示预置）
	m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo hi"})
	// 领取使其变成 running（ClaimedAt=now，ClaimedBy=agentID）
	got := m.ClaimTask(a.AgentID)
	if got == nil || got.Status != "running" {
		t.Fatalf("claim = %+v, want running", got)
	}
	if got.ClaimedBy != a.AgentID {
		t.Fatalf("ClaimedBy = %q, want agentID %q", got.ClaimedBy, a.AgentID)
	}

	// 情况 1：agent 心跳正常（LastSeen 最近），任务 ClaimedAt 超期 —— 不应回收（防双跑）
	m.mu.Lock()
	ts := m.tasks[a.AgentID]
	ts[0].ClaimedAt = time.Now().Add(-time.Hour)
	// LastSeen 保持为默认（注册时的 now），不修改
	m.mu.Unlock()
	if n := m.ReclaimStaleTasks(5 * time.Minute); n != 0 {
		t.Fatalf("healthy agent: ReclaimStaleTasks = %d, want 0 (heartbeat guard)", n)
	}

	// 情况 2：agent 真正失联（LastSeen 超时），任务 ClaimedAt 也超期 —— 应回收
	m.mu.Lock()
	// 把 agent 的 LastSeen 也拨到 1h 前
	if ag, ok := m.agents[a.AgentID]; ok {
		ag.LastSeen = time.Now().Add(-time.Hour)
	}
	m.mu.Unlock()
	if n := m.ReclaimStaleTasks(5 * time.Minute); n != 1 {
		t.Fatalf("offline agent: ReclaimStaleTasks = %d, want 1", n)
	}
	if got := m.GetTasks(a.AgentID); len(got) != 1 || got[0].Status != "pending" {
		t.Fatalf("after reclaim GetTasks=%+v, want 1 pending", got)
	}

	// 情况 3：重新领取后 ClaimedAt=now，agent 在线 —— 不回收
	m.ClaimTask(a.AgentID)
	if n := m.ReclaimStaleTasks(5 * time.Minute); n != 0 {
		t.Fatalf("fresh claim reclaimed = %d, want 0", n)
	}
}

// TestMemoryStore_QueryAudits 验证 审计可查：租户隔离 + 动作过滤 + 倒序 + limit。
func TestMemoryStore_QueryAudits(t *testing.T) {
	m := NewMemoryStore()
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "register", Target: "a1", CreatedAt: time.Now().Add(-time.Hour)})
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "create_task", Target: "x", CreatedAt: time.Now()})
	m.Audit(&proto.AuditEvent{TenantID: "t2", Action: "register", Target: "a2", CreatedAt: time.Now()})
	// 租户隔离
	if got := m.QueryAudits("t1", "", time.Time{}, time.Time{}, 0); len(got) != 2 {
		t.Fatalf("tenant t1 = %d, want 2", len(got))
	}
	// 动作过滤
	if got := m.QueryAudits("", "register", time.Time{}, time.Time{}, 0); len(got) != 2 {
		t.Fatalf("action register = %d, want 2", len(got))
	}
	// 倒序：最新在前
	all := m.QueryAudits("", "", time.Time{}, time.Time{}, 0)
	if len(all) != 3 || all[0].TenantID != "t2" {
		t.Fatalf("order = %+v, want t2 first", all)
	}
	// limit
	if got := m.QueryAudits("", "", time.Time{}, time.Time{}, 1); len(got) != 1 {
		t.Fatalf("limit 1 = %d, want 1", len(got))
	}
}

// TestMemoryStore_Provision 自动纳管：签发一次性 install token，标记设备 provisioning。
func TestMemoryStore_Provision(t *testing.T) {
	m := NewMemoryStore().WithSecret("opsmesh-test-secret")
	m.UpsertDevice(&proto.DeviceInfo{
		DeviceID: "dev-test-1", Segment: "default", TenantID: "t1",
		IP: "10.0.0.1", AgentID: "", State: "discovered", Managed: false,
	})
	tok, boot, err := m.Provision("dev-test-1", "10.0.0.1", "t1")
	if err != nil {
		t.Fatalf("Provision err = %v", err)
	}
	if tok == "" {
		t.Fatal("token empty")
	}
	if boot == "" {
		t.Fatal("bootstrap empty")
	}
	// bootstrap 应包含 token
	if !strings.Contains(boot, "--token=") {
		t.Fatalf("bootstrap missing --token=: %s", boot)
	}
	// 设备状态应变成 provisioning
	if dev := m.Device("dev-test-1"); dev == nil || dev.State != "provisioning" {
		t.Fatalf("device state = %q, want provisioning", dev.State)
	}
	// 不存在的设备应报错
	if _, _, err2 := m.Provision("dev-nonexistent", "", ""); err2 == nil {
		t.Fatal("expected error for nonexistent device")
	}
}

// TestMemoryStore_ConsumeToken_OneTime B1 token 一次性校验：首次 ok，二次 fail；过期 fail。
func TestMemoryStore_ConsumeToken_OneTime(t *testing.T) {
	m := NewMemoryStore().WithSecret("opsmesh-test-secret")
	m.UpsertDevice(&proto.DeviceInfo{
		DeviceID: "dev-consume", Segment: "default", TenantID: "t1",
		IP: "10.0.0.2", State: "discovered", Managed: false,
	})
	tok, _, err := m.Provision("dev-consume", "10.0.0.2", "t1")
	if err != nil {
		t.Fatalf("Provision err = %v", err)
	}
	// 首次消费：应当成功，返回设备与租户
	devID, tenID, ok := m.ConsumeToken(tok)
	if !ok {
		t.Fatal("ConsumeToken first call = false, want true")
	}
	if devID != "dev-consume" || tenID != "t1" {
		t.Fatalf("ConsumeToken = (%q, %q), want (dev-consume, t1)", devID, tenID)
	}
	// 二次消费：应当失败（已 consumed）
	if _, _, ok2 := m.ConsumeToken(tok); ok2 {
		t.Fatal("ConsumeToken second call = true, want false")
	}
	// 不存在的 token
	if _, _, ok3 := m.ConsumeToken("fake-token"); ok3 {
		t.Fatal("ConsumeToken fake = true, want false")
	}
}

// TestMemoryStore_Register_Onboard B1 Register 带 OnboardDeviceID 翻转候选设备为已纳管。
func TestMemoryStore_Register_Onboard(t *testing.T) {
	m := NewMemoryStore()
	// 创建候选设备
	m.UpsertDevice(&proto.DeviceInfo{
		DeviceID: "dev-candidate", Segment: "seg-a", TenantID: "t1",
		IP: "10.0.0.3", State: "discovered", Managed: false,
	})
	// Register 时携带 OnboardDeviceID → 应翻转该设备而非新建占位设备
	a := m.Register(&proto.AgentInfo{
		AgentID: "agent-onboard-1", Hostname: "h1", Segment: "seg-a",
		Addr: "10.0.0.3", TenantID: "t1",
		OnboardDeviceID: "dev-candidate",
	})
	if a.AgentID == "" {
		t.Fatal("expected non-empty AgentID")
	}
	// 候选设备现在应该已纳管
	dev := m.Device("dev-candidate")
	if dev == nil {
		t.Fatal("dev-candidate not found")
	}
	if !dev.Managed {
		t.Fatal("dev-candidate.Managed = false, want true")
	}
	if dev.State != "online" {
		t.Fatalf("dev-candidate.State = %q, want online", dev.State)
	}
	if dev.AgentID != a.AgentID {
		t.Fatalf("dev-candidate.AgentID = %q, want %q", dev.AgentID, a.AgentID)
	}
	// 不应新增占位设备 dev-<agentID>
	if placeholder := m.Device("dev-" + a.AgentID); placeholder != nil {
		t.Fatalf("unexpected placeholder device dev-%s", a.AgentID)
	}
}

// TestMemoryStore_Register_Onboard_TenantMismatch 安全回归（纵深防御）：
// 候选设备租户与 agent 租户不一致时，store 层拒绝翻转（即便上层漏校验也拦得住）。
func TestMemoryStore_Register_Onboard_TenantMismatch(t *testing.T) {
	m := NewMemoryStore()
	// 租户 tB 的候选设备
	m.UpsertDevice(&proto.DeviceInfo{
		DeviceID: "dev-victim", Segment: "seg-a", TenantID: "tB",
		IP: "10.0.0.9", State: "discovered", Managed: false,
	})
	// 租户 tA 的 agent 试图翻转 tB 的设备（绕过上层校验的场景）
	a := m.Register(&proto.AgentInfo{
		AgentID: "agent-evil", Hostname: "h1", Segment: "seg-a",
		Addr: "10.0.0.9", TenantID: "tA",
		OnboardDeviceID: "dev-victim",
	})
	dev := m.Device("dev-victim")
	if dev == nil {
		t.Fatal("dev-victim not found")
	}
	if dev.Managed {
		t.Fatal("跨租户设备被错误翻转 Managed=true（应拒绝）")
	}
	if dev.AgentID == a.AgentID {
		t.Fatal("跨租户设备被错误绑定到攻击者 agent")
	}
	if dev.TenantID != "tB" {
		t.Fatalf("受害设备租户被改写: %q, want tB", dev.TenantID)
	}
}

// TestMemoryStore_DeviceMetrics 存储与查询设备最新监控指标。
// 覆盖：写入后可读到、nil 入参安全、空 deviceID 安全、返回深拷贝（外部修改不影响内部）。
func TestMemoryStore_DeviceMetrics(t *testing.T) {
	m := NewMemoryStore()
	// 初始无数据。
	if got := m.DeviceMetrics("dev-1"); got != nil {
		t.Fatalf("DeviceMetrics(dev-1) = %+v, want nil", got)
	}
	metrics := &proto.DeviceMetrics{
		DeviceID:    "dev-1",
		Hostname:    "web-01",
		OS:          "linux",
		Arch:        "amd64",
		CPU:         proto.CPUMetrics{Cores: 4, Usage: 12.5, Model: "Intel Xeon"},
		Memory:      proto.MemMetrics{Total: 8192, Used: 2048, Available: 6144, Usage: 25.0},
		CollectedAt: time.Now(),
	}
	m.StoreDeviceMetrics("dev-1", metrics)
	got := m.DeviceMetrics("dev-1")
	if got == nil {
		t.Fatal("DeviceMetrics(dev-1) = nil, want non-nil")
	}
	if got.CPU.Cores != 4 || got.CPU.Usage != 12.5 {
		t.Fatalf("CPU = %+v, want cores=4 usage=12.5", got.CPU)
	}
	if got.Memory.Total != 8192 {
		t.Fatalf("Memory.Total = %d, want 8192", got.Memory.Total)
	}
	// 深拷贝：外部修改不应影响 store 内部缓存。
	got.CPU.Cores = 999
	if got2 := m.DeviceMetrics("dev-1"); got2.CPU.Cores != 4 {
		t.Fatalf("深拷贝失效：内部缓存被外部修改 cores=%d, want 4", got2.CPU.Cores)
	}
	// 安全：nil 入参与空 deviceID 不 panic。
	m.StoreDeviceMetrics("", metrics)
	m.StoreDeviceMetrics("dev-1", nil)
	// 上述空 deviceID 不应写入新键，dev-1 仍可读。
	if got := m.DeviceMetrics("dev-1"); got == nil {
		t.Fatal("StoreDeviceMetrics(dev-1, nil) 误清了已有缓存")
	}
}

// TestMemoryStore_DeviceMetricsHistory 环形缓冲历史时序查询。
// 覆盖：多次写入后历史按时间升序返回、since 过滤、覆写最旧、深拷贝、无数据返回 nil。
func TestMemoryStore_DeviceMetricsHistory(t *testing.T) {
	m := NewMemoryStore()
	// 初始无数据。
	if got := m.DeviceMetricsHistory("dev-1", time.Time{}); got != nil {
		t.Fatalf("初始 DeviceMetricsHistory = %+v, want nil", got)
	}
	base := time.Now()
	// 写入 3 条历史。
	for i := 0; i < 3; i++ {
		m.StoreDeviceMetrics("dev-1", &proto.DeviceMetrics{
			DeviceID:    "dev-1",
			CPU:         proto.CPUMetrics{Cores: i + 1, Usage: float64(i * 10)},
			CollectedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	// 查全部：3 条升序。
	all := m.DeviceMetricsHistory("dev-1", time.Time{})
	if len(all) != 3 {
		t.Fatalf("History 全量 = %d 条, want 3", len(all))
	}
	for i, s := range all {
		if s.CPU.Cores != i+1 {
			t.Fatalf("all[%d].CPU.Cores = %d, want %d", i, s.CPU.Cores, i+1)
		}
	}
	// since 过滤：base+1s 之后应有 2 条（i=1,2）。
	got := m.DeviceMetricsHistory("dev-1", base.Add(1*time.Second))
	if len(got) != 2 {
		t.Fatalf("Since(base+1s) = %d 条, want 2", len(got))
	}
	// DeviceMetrics() 仍返回最新值（向后兼容）。
	latest := m.DeviceMetrics("dev-1")
	if latest == nil || latest.CPU.Cores != 3 {
		t.Fatalf("DeviceMetrics() Latest = %+v, want Cores=3", latest)
	}
	// 深拷贝：修改返回值不影响内部。
	all[0].CPU.Cores = 999
	if m.DeviceMetrics("dev-1").CPU.Cores != 3 {
		t.Fatal("深拷贝失效：外部修改污染了内部缓存")
	}
}

// TestMemoryStore_DeviceMetricsHistory_Overwrite 环形缓冲满后覆写最旧。
func TestMemoryStore_DeviceMetricsHistory_Overwrite(t *testing.T) {
	// 直接构造小容量环形缓冲验证覆写逻辑。
	r := newMetricsRing(3)
	base := time.Now()
	for i := 0; i < 5; i++ {
		r.add(&proto.DeviceMetrics{
			DeviceID:    "dev-1",
			CPU:         proto.CPUMetrics{Cores: i + 1},
			CollectedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	all := r.since(time.Time{})
	if len(all) != 3 {
		t.Fatalf("覆写后 History = %d 条, want 3", len(all))
	}
	// 应保留最后 3 条：Cores=3,4,5。
	for i, s := range all {
		if s.CPU.Cores != i+3 {
			t.Fatalf("all[%d].CPU.Cores = %d, want %d", i, s.CPU.Cores, i+3)
		}
	}
}

// TestMemoryStore_Register_FillsDeviceMeta 注册时上报 OS/Arch 应填充到 DeviceInfo。
func TestMemoryStore_Register_FillsDeviceMeta(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{
		AgentID: "agent-meta", Hostname: "web-01", Segment: "seg-a",
		OS: "linux", Arch: "amd64",
	})
	dev := m.Device("dev-" + a.AgentID)
	if dev == nil {
		t.Fatal("占位设备未创建")
	}
	if dev.Hostname != "web-01" {
		t.Fatalf("dev.Hostname = %q, want web-01", dev.Hostname)
	}
	if dev.OS != "linux" {
		t.Fatalf("dev.OS = %q, want linux", dev.OS)
	}
	if dev.Arch != "amd64" {
		t.Fatalf("dev.Arch = %q, want amd64", dev.Arch)
	}
}

// TestMemoryStore_AgentSecret ：Register 时为每个 agent 生成 HMAC 签名密钥，
// AgentSecret 可查到；不同 agent 密钥不同；复用已注册 agent 不重置密钥。
func TestMemoryStore_AgentSecret(t *testing.T) {
	m := NewMemoryStore()
	a1 := m.Register(&proto.AgentInfo{AgentID: "agent-sec-1", Segment: "seg-a"})
	a2 := m.Register(&proto.AgentInfo{AgentID: "agent-sec-2", Segment: "seg-a"})

	s1 := m.AgentSecret(a1.AgentID)
	s2 := m.AgentSecret(a2.AgentID)
	if s1 == "" {
		t.Fatal("agent-sec-1 should have non-empty secret")
	}
	if s2 == "" {
		t.Fatal("agent-sec-2 should have non-empty secret")
	}
	if s1 == s2 {
		t.Fatal("different agents should have different secrets")
	}
	if len(s1) != 64 { // 32 bytes hex = 64 chars
		t.Fatalf("secret length = %d, want 64 (32 bytes hex)", len(s1))
	}

	// 未注册 agent 返回空串
	if got := m.AgentSecret("agent-nonexistent"); got != "" {
		t.Fatalf("AgentSecret(nonexistent) = %q, want empty", got)
	}

	// 复用已注册 agent 不重置密钥
	m.Register(&proto.AgentInfo{AgentID: "agent-sec-1", Segment: "seg-a"})
	if got := m.AgentSecret(a1.AgentID); got != s1 {
		t.Fatalf("re-register should not reset secret: got %q, want %q", got, s1)
	}
}

// TestMemoryStore_SubmitResult_StateGuard 验证 状态守卫（幂等）：
// 仅 running 任务接受上报；pending/cancelled 的迟到/重复上报被忽略（结果记录保留），
// 防止 cancelled 被翻回 done、防止重复失败上报累计重试造成假死信。
func TestMemoryStore_SubmitResult_StateGuard(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	tk := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo hi", MaxRetries: 3})

	// 1) 未领取的 pending 任务收到迟到上报：忽略，不累计重试。
	m.SubmitResult(&proto.TaskResult{TaskID: tk.TaskID, AgentID: a.AgentID, ExitCode: 1})
	if tk.Status != "pending" || tk.RetryCount != 0 {
		t.Fatalf("pending 任务应忽略上报: status=%q retryCount=%d", tk.Status, tk.RetryCount)
	}

	// 2) 领取后的正常上报被接受（失败→重试）。
	m.ClaimTask(a.AgentID)
	m.SubmitResult(&proto.TaskResult{TaskID: tk.TaskID, AgentID: a.AgentID, ExitCode: 1})
	if tk.Status != "pending" || tk.RetryCount != 1 {
		t.Fatalf("running 任务失败上报应重试: status=%q retryCount=%d", tk.Status, tk.RetryCount)
	}

	// 3) 同一失败的重复上报（此时已复位 pending）：忽略，retryCount 保持 1。
	m.SubmitResult(&proto.TaskResult{TaskID: tk.TaskID, AgentID: a.AgentID, ExitCode: 1})
	if tk.RetryCount != 1 {
		t.Fatalf("重复迟到上报不应累计重试, retryCount=%d", tk.RetryCount)
	}

	// 4) cancelled 不被迟到成功上报翻回 done。
	m.ClaimTask(a.AgentID)
	m.CancelTask(tk.TaskID, "")
	m.SubmitResult(&proto.TaskResult{TaskID: tk.TaskID, AgentID: a.AgentID, ExitCode: 0})
	if tk.Status != "cancelled" {
		t.Fatalf("cancelled 任务不得被翻回, status=%q", tk.Status)
	}

	// 结果记录全部保留（守卫不影响 results 留痕）。
	if rs := m.Results(a.AgentID); len(rs) != 4 {
		t.Fatalf("Results = %d, want 4（全部保留）", len(rs))
	}
}
