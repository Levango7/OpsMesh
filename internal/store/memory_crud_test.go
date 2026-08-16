// memory_crud_test.go 补全 MemoryStore 的 CRUD 单元测试，目标覆盖率 70%+。
//
// 覆盖范围：
//   - 告警：AddAlert / Alerts / Alert / AckAlert / SilenceAlert
//   - 任务：Heartbeat / TasksByParent / TaskResult / TaskByID / ApproveTask / RejectTask / CancelTask / RetireDevice
//   - 令牌：IssueToken / ConsumeToken 过期 / CleanupTokens
//   - 审计：Audits
//   - 配额：GetQuota / SetQuota
//   - 告警规则：CreateAlertRule / ListAlertRules / DeleteAlertRule / GetAlertRule / UpdateAlertRule
//   - RBAC：User / Role / Permission CRUD
//   - OS 模板 / 中间件模板 CRUD
//   - 告警治理：Silence / NotifyChannel / NotifyTemplate CRUD
//   - 刷新令牌 CRUD / Agent 日志
//   - SeedDemoTopology / FireDueSchedules
//
// 测试风格与 memory_test.go / memory_k8s_test.go 一致：白盒（package store）。
package store

import (
	"strings"
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// ============================================================================
// 告警相关：AddAlert / Alerts / Alert / AckAlert / SilenceAlert
// ============================================================================

// TestMemoryStore_AddAlert_Alerts_Query 验证 AddAlert 写入、Alerts 租户过滤、Alert 单条查询。
func TestMemoryStore_AddAlert_Alerts_Query(t *testing.T) {
	m := NewMemoryStore()
	m.AddAlert(&proto.Alert{AlertID: "a1", TenantID: "t1", DeviceID: "d1", Severity: "critical", Message: "m1"})
	m.AddAlert(&proto.Alert{AlertID: "a2", TenantID: "t2", DeviceID: "d2", Severity: "warning", Message: "m2"})

	// Alerts 全量
	if got := m.Alerts(""); len(got) != 2 {
		t.Fatalf("Alertes() = %d, want 2", len(got))
	}
	// Alerts 租户过滤
	if got := m.Alerts("t1"); len(got) != 1 || got[0].AlertID != "a1" {
		t.Fatalf("Alerts(t1) = %+v, want a1", got)
	}
	// Alert 单条命中
	got := m.Alert("a2")
	if got == nil || got.Severity != "warning" {
		t.Fatalf("Alert(a2) = %+v, want warning", got)
	}
	// Alert 未命中
	if m.Alert("nope") != nil {
		t.Fatal("Alert(nope) should be nil")
	}
	// 默认状态为 firing
	if got := m.Alert("a1"); got.Status != proto.AlertStatusFiring {
		t.Fatalf("default status = %q, want firing", got.Status)
	}
}

// TestMemoryStore_AckAlert_TenantGuard 验证 AckAlert 确认告警 + 租户越权返回 false。
func TestMemoryStore_AckAlert_TenantGuard(t *testing.T) {
	m := NewMemoryStore()
	m.AddAlert(&proto.Alert{AlertID: "a1", TenantID: "t1", DeviceID: "d1", Severity: "critical"})

	// 越权：t2 不可确认 t1 的告警
	if m.AckAlert("a1", "t2", "u2") {
		t.Fatal("AckAlert 跨租户应返回 false")
	}
	// 正常确认
	if !m.AckAlert("a1", "t1", "u1") {
		t.Fatal("AckAlert 同租户应返回 true")
	}
	got := m.Alert("a1")
	if got.Status != proto.AlertStatusAcknowledged || got.AcknowledgedBy != "u1" {
		t.Fatalf("ack 后状态错误: %+v", got)
	}
	// 不存在
	if m.AckAlert("nope", "", "") {
		t.Fatal("AckAlert 不存在应返回 false")
	}
	// 空租户可确认任意
	m.AddAlert(&proto.Alert{AlertID: "a2", TenantID: "tX", DeviceID: "d2"})
	if !m.AckAlert("a2", "", "admin") {
		t.Fatal("AckAlert 空租户应可确认")
	}
}

// TestMemoryStore_SilenceAlert_Default24h 验证 SilenceAlert 静默告警，until 零值默认 24h。
func TestMemoryStore_SilenceAlert_Default24h(t *testing.T) {
	m := NewMemoryStore()
	m.AddAlert(&proto.Alert{AlertID: "a1", TenantID: "t1", DeviceID: "d1"})
	before := time.Now()

	if !m.SilenceAlert("a1", "t1", "u1", time.Time{}, "维护中") {
		t.Fatal("SilenceAlert 应返回 true")
	}
	got := m.Alert("a1")
	if got.Status != proto.AlertStatusSilenced {
		t.Fatalf("status = %q, want silenced", got.Status)
	}
	if got.AcknowledgedBy != "u1" || got.Comment != "维护中" {
		t.Fatalf("silence 元信息错误: %+v", got)
	}
	// 默认 24h
	if got.SilencedUntil.Before(before.Add(23 * time.Hour)) {
		t.Fatalf("SilencedUntil = %v, want ~24h after now", got.SilencedUntil)
	}
	// 越权
	m.AddAlert(&proto.Alert{AlertID: "a2", TenantID: "t2", DeviceID: "d2"})
	if m.SilenceAlert("a2", "t1", "u1", time.Time{}, "") {
		t.Fatal("SilenceAlert 跨租户应返回 false")
	}
	// 不存在
	if m.SilenceAlert("nope", "", "", time.Time{}, "") {
		t.Fatal("SilenceAlert 不存在应返回 false")
	}
}

// ============================================================================
// 任务相关：Heartbeat / TasksByParent / TaskResult / TaskByID / ApproveTask / RejectTask / CancelTask / RetireDevice
// ============================================================================

// TestMemoryStore_Heartbeat_UnknownAgent 验证 Heartbeat 已知/未知 agent。
func TestMemoryStore_Heartbeat_UnknownAgent(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})

	// 已知 agent
	if !m.Heartbeat(a.AgentID, "online", 42) {
		t.Fatal("Heartbeat 已知 agent 应返回 true")
	}
	got := m.Agent(a.AgentID)
	if got.Status != "online" || got.Load != 42 {
		t.Fatalf("Heartbeat 未更新: status=%q load=%d", got.Status, got.Load)
	}
	// 未知 agent
	if m.Heartbeat("agent-nope", "online", 1) {
		t.Fatal("Heartbeat 未知 agent 应返回 false")
	}
}

// TestMemoryStore_TasksByParent 验证按 parentID 查询派生任务实例。
func TestMemoryStore_TasksByParent(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})

	// 模板任务（ParentID 空）
	parent := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "cron-job", Schedule: "* * * * *"})
	// 派生实例（ParentID 指向模板）
	m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "cron-job", ParentID: parent.TaskID})

	got := m.TasksByParent(parent.TaskID)
	if len(got) != 1 {
		t.Fatalf("TasksByParent = %d, want 1", len(got))
	}
	// 不存在的 parent
	if got := m.TasksByParent("nope"); len(got) != 0 {
		t.Fatalf("TasksByParent(nope) = %d, want 0", len(got))
	}
}

// TestMemoryStore_TaskResult_Query 验证 TaskResult 按 taskID 查询执行结果。
func TestMemoryStore_TaskResult_Query(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	tk := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo hi"})
	m.ClaimTask(a.AgentID)
	m.SubmitResult(&proto.TaskResult{TaskID: tk.TaskID, AgentID: a.AgentID, ExitCode: 0, Stdout: "hi"})

	got := m.TaskResult(tk.TaskID)
	if got == nil || got.Stdout != "hi" {
		t.Fatalf("TaskResult = %+v, want stdout=hi", got)
	}
	// 不存在
	if m.TaskResult("nope") != nil {
		t.Fatal("TaskResult(nope) should be nil")
	}
}

// TestMemoryStore_TaskByID_NotFound 验证 TaskByID 命中/未命中 + 深拷贝。
func TestMemoryStore_TaskByID_NotFound(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	tk := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo"})

	got := m.TaskByID(tk.TaskID)
	if got == nil || got.TaskID != tk.TaskID {
		t.Fatalf("TaskByID 命中失败: %+v", got)
	}
	// 深拷贝
	got.Status = "MUTATED"
	if inner := m.TaskByID(tk.TaskID); inner.Status == "MUTATED" {
		t.Fatal("TaskByID 未深拷贝")
	}
	// 不存在
	if m.TaskByID("nope") != nil {
		t.Fatal("TaskByID(nope) should be nil")
	}
}

// TestMemoryStore_ApproveTask_Flow 验证任务审批通过流程。
func TestMemoryStore_ApproveTask_Flow(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	tk := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "danger", ApprovalRequired: true})

	if tk.Status != "pending_approval" {
		t.Fatalf("审批任务初始状态 = %q, want pending_approval", tk.Status)
	}
	// 审批前不可领取
	if m.ClaimTask(a.AgentID) != nil {
		t.Fatal("pending_approval 任务不应被领取")
	}
	// 越权审批
	if m.ApproveTask(tk.TaskID, "t2", "u2") {
		t.Fatal("跨租户审批应返回 false")
	}
	// 正常审批
	if !m.ApproveTask(tk.TaskID, "t1", "u1") {
		t.Fatal("ApproveTask 应返回 true")
	}
	updated := m.TaskByID(tk.TaskID)
	if updated.Status != "pending" || updated.ApprovedBy != "u1" {
		t.Fatalf("审批后状态错误: %+v", updated)
	}
	// 审批后可领取
	if m.ClaimTask(a.AgentID) == nil {
		t.Fatal("审批后应可领取")
	}
	// 重复审批返回 false
	if m.ApproveTask(tk.TaskID, "t1", "u1") {
		t.Fatal("重复审批应返回 false")
	}
	// 不存在
	if m.ApproveTask("nope", "", "") {
		t.Fatal("ApproveTask 不存在应返回 false")
	}
}

// TestMemoryStore_RejectTask_Flow 验证任务驳回流程。
func TestMemoryStore_RejectTask_Flow(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	tk := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "danger", ApprovalRequired: true})

	// 越权驳回
	if m.RejectTask(tk.TaskID, "t2", "u2") {
		t.Fatal("跨租户驳回应返回 false")
	}
	// 正常驳回
	if !m.RejectTask(tk.TaskID, "t1", "u1") {
		t.Fatal("RejectTask 应返回 true")
	}
	updated := m.TaskByID(tk.TaskID)
	if updated.Status != "rejected" || updated.ApprovedBy != "u1" {
		t.Fatalf("驳回后状态错误: %+v", updated)
	}
	// 驳回后不可领取
	if m.ClaimTask(a.AgentID) != nil {
		t.Fatal("rejected 任务不应被领取")
	}
	// 重复驳回返回 false
	if m.RejectTask(tk.TaskID, "t1", "u1") {
		t.Fatal("重复驳回应返回 false")
	}
	// 不存在
	if m.RejectTask("nope", "", "") {
		t.Fatal("RejectTask 不存在应返回 false")
	}
}

// TestMemoryStore_CancelTask_AlreadyDone 验证 CancelTask 对已 done 任务返回 false。
func TestMemoryStore_CancelTask_AlreadyDone(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	tk := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo"})
	m.ClaimTask(a.AgentID)
	m.SubmitResult(&proto.TaskResult{TaskID: tk.TaskID, AgentID: a.AgentID, ExitCode: 0})

	// 已 done 不可取消
	if m.CancelTask(tk.TaskID, "t1") {
		t.Fatal("CancelTask 对 done 任务应返回 false")
	}
	// 不存在
	if m.CancelTask("nope", "") {
		t.Fatal("CancelTask 不存在应返回 false")
	}
	// 越租户
	tk2 := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo2"})
	if m.CancelTask(tk2.TaskID, "t2") {
		t.Fatal("CancelTask 跨租户应返回 false")
	}
	// 正常取消 pending
	if !m.CancelTask(tk2.TaskID, "t1") {
		t.Fatal("CancelTask pending 应返回 true")
	}
}

// TestMemoryStore_RetireDevice_TenantGuard 验证 RetireDevice 退役设备 + 租户校验。
func TestMemoryStore_RetireDevice_TenantGuard(t *testing.T) {
	m := NewMemoryStore()
	m.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-1", Segment: "seg-a", TenantID: "t1", State: "online"})

	// 越权
	if m.RetireDevice("dev-1", "t2") {
		t.Fatal("RetireDevice 跨租户应返回 false")
	}
	// 正常退役
	if !m.RetireDevice("dev-1", "t1") {
		t.Fatal("RetireDevice 应返回 true")
	}
	dev := m.Device("dev-1")
	if !dev.Retired || dev.State != "offline" {
		t.Fatalf("退役后状态错误: %+v", dev)
	}
	// 退役后不出现在 Snapshot
	snap := m.Snapshot("t1")
	for _, devs := range snap {
		for _, d := range devs {
			if d.DeviceID == "dev-1" {
				t.Fatal("退役设备不应出现在 Snapshot")
			}
		}
	}
	// 不存在
	if m.RetireDevice("nope", "") {
		t.Fatal("RetireDevice 不存在应返回 false")
	}
	// 空租户可退役
	m.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-2", Segment: "seg-a", TenantID: "t2", State: "online"})
	if !m.RetireDevice("dev-2", "") {
		t.Fatal("RetireDevice 空租户应可退役")
	}
}

// ============================================================================
// 令牌相关：IssueToken / ConsumeToken 过期 / CleanupTokens
// ============================================================================

// TestMemoryStore_IssueToken_EmptyDeviceID 验证 IssueToken 空设备 ID 报错。
func TestMemoryStore_IssueToken_EmptyDeviceID(t *testing.T) {
	m := NewMemoryStore().WithSecret("test-secret")
	if _, err := m.IssueToken("", "t1", 15*time.Minute); err == nil {
		t.Fatal("IssueToken 空设备 ID 应报错")
	}
	tok, err := m.IssueToken("dev-1", "t1", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken err = %v", err)
	}
	if tok == "" || !strings.Contains(tok, ".") {
		t.Fatalf("token 格式错误: %q", tok)
	}
}

// TestMemoryStore_ConsumeToken_Expired 验证 ConsumeToken 过期 token 返回 false。
func TestMemoryStore_ConsumeToken_Expired(t *testing.T) {
	m := NewMemoryStore().WithSecret("test-secret")
	tok, err := m.IssueToken("dev-1", "t1", 1*time.Millisecond)
	if err != nil {
		t.Fatalf("IssueToken err = %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, _, ok := m.ConsumeToken(tok); ok {
		t.Fatal("过期 token 应返回 false")
	}
	// 篡改的 token（HMAC 校验失败）
	if _, _, ok := m.ConsumeToken("fake.sig.payload"); ok {
		t.Fatal("篡改 token 应返回 false")
	}
	// 空 token
	if _, _, ok := m.ConsumeToken(""); ok {
		t.Fatal("空 token 应返回 false")
	}
}

// TestMemoryStore_CleanupTokens 验证 CleanupTokens 清理已消费/过期 token。
func TestMemoryStore_CleanupTokens(t *testing.T) {
	m := NewMemoryStore().WithSecret("test-secret")
	// 已消费的 token
	tok1, _ := m.IssueToken("dev-1", "t1", 15*time.Minute)
	m.ConsumeToken(tok1)
	// 过期的 token
	m.IssueToken("dev-2", "t1", 1*time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	// 未过期未消费的 token
	m.IssueToken("dev-3", "t1", 15*time.Minute)

	// 清理全部（batch=0 表示不限制）
	n := m.CleanupTokens(0)
	if n != 2 {
		t.Fatalf("CleanupTokens(0) = %d, want 2（已消费+过期）", n)
	}
	// 再次清理应为 0
	if n := m.CleanupTokens(0); n != 0 {
		t.Fatalf("二次 CleanupTokens = %d, want 0", n)
	}
	// batch 限制
	tok2, _ := m.IssueToken("dev-4", "t1", 15*time.Minute)
	m.ConsumeToken(tok2)
	tok3, _ := m.IssueToken("dev-5", "t1", 15*time.Minute)
	m.ConsumeToken(tok3)
	if n := m.CleanupTokens(1); n != 1 {
		t.Fatalf("CleanupTokens(1) = %d, want 1（batch 限制）", n)
	}
}

// ============================================================================
// 审计相关：Audits
// ============================================================================

// TestMemoryStore_Audits_List 验证 Audits 返回审计事件列表。
func TestMemoryStore_Audits_List(t *testing.T) {
	m := NewMemoryStore()
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "register", Target: "a1"})
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "create_task", Target: "x"})

	got := m.Audits()
	if len(got) != 2 {
		t.Fatalf("Audits = %d, want 2", len(got))
	}
	if got[0].Action != "register" || got[1].Action != "create_task" {
		t.Fatalf("Audits 内容错误: %+v", got)
	}
	// 空存储返回空切片
	m2 := NewMemoryStore()
	if got := m2.Audits(); len(got) != 0 {
		t.Fatalf("空 Audits = %d, want 0", len(got))
	}
}

// ============================================================================
// 配额相关：GetQuota / SetQuota
// ============================================================================

// TestMemoryStore_Quota_GetSet 验证 GetQuota / SetQuota CRUD + 深拷贝。
func TestMemoryStore_Quota_GetSet(t *testing.T) {
	m := NewMemoryStore()
	// 未设置返回 nil
	got, err := m.GetQuota("t1")
	if err != nil || got != nil {
		t.Fatalf("GetQuota 未设置 = (%+v, %v), want (nil, nil)", got, err)
	}
	// 空租户返回 nil
	if got, _ := m.GetQuota(""); got != nil {
		t.Fatal("GetQuota(\"\") 应返回 nil")
	}
	// 设置配额
	cfg := &QuotaConfig{MaxDevices: 100, MaxTasks: 500, MaxAlerts: 50}
	if err := m.SetQuota("t1", cfg); err != nil {
		t.Fatalf("SetQuota err = %v", err)
	}
	got, _ = m.GetQuota("t1")
	if got == nil || got.MaxDevices != 100 || got.MaxTasks != 500 {
		t.Fatalf("GetQuota after set = %+v, want {100,500,50}", got)
	}
	// 深拷贝
	got.MaxDevices = 999
	if inner, _ := m.GetQuota("t1"); inner.MaxDevices != 100 {
		t.Fatal("GetQuota 未深拷贝")
	}
	// 更新
	m.SetQuota("t1", &QuotaConfig{MaxDevices: 200})
	if got, _ := m.GetQuota("t1"); got.MaxDevices != 200 || got.MaxTasks != 0 {
		t.Fatalf("更新后 = %+v, want {200,0,0}", got)
	}
	// nil cfg 等价删除
	m.SetQuota("t1", nil)
	if got, _ := m.GetQuota("t1"); got != nil {
		t.Fatal("SetQuota nil 应删除配额")
	}
	// 空租户报错
	if err := m.SetQuota("", cfg); err == nil {
		t.Fatal("SetQuota 空租户应报错")
	}
}

// ============================================================================
// 告警规则：CreateAlertRule / ListAlertRules / DeleteAlertRule / GetAlertRule / UpdateAlertRule
// ============================================================================

// TestMemoryStore_AlertRule_CRUD 验证告警规则完整 CRUD + 租户过滤 + 深拷贝。
func TestMemoryStore_AlertRule_CRUD(t *testing.T) {
	m := NewMemoryStore()
	// nil 入参
	if m.CreateAlertRule(nil) != nil {
		t.Fatal("CreateAlertRule(nil) 应返回 nil")
	}
	// 创建（ID 自动分配，TenantID 归一 default）
	r1 := m.CreateAlertRule(&AlertRule{Metric: "cpu_usage", Op: ">", Threshold: 90, Severity: "critical", Enabled: true})
	if r1 == nil || r1.ID == "" {
		t.Fatal("CreateAlertRule 应分配 ID")
	}
	if r1.TenantID != "default" {
		t.Fatalf("TenantID = %q, want default", r1.TenantID)
	}
	if r1.CreatedAt.IsZero() {
		t.Fatal("CreatedAt 应被填充")
	}
	// 指定 ID + TenantID
	r2 := m.CreateAlertRule(&AlertRule{ID: "rule-2", TenantID: "t1", Metric: "disk_usage", Op: ">", Threshold: 85, Enabled: true})

	// Get 命中
	got := m.GetAlertRule(r1.ID)
	if got == nil || got.Metric != "cpu_usage" {
		t.Fatalf("GetAlertRule 命中失败: %+v", got)
	}
	// Get 未命中
	if m.GetAlertRule("nope") != nil {
		t.Fatal("GetAlertRule(nope) should be nil")
	}
	// Get 深拷贝
	got.Threshold = 0
	if m.GetAlertRule(r1.ID).Threshold != 90 {
		t.Fatal("GetAlertRule 未深拷贝")
	}

	// List 全量
	if list := m.ListAlertRules(""); len(list) != 2 {
		t.Fatalf("ListAlertRules 全量 = %d, want 2", len(list))
	}
	// List 租户过滤
	if list := m.ListAlertRules("t1"); len(list) != 1 || list[0].ID != "rule-2" {
		t.Fatalf("ListAlertRules(t1) = %+v, want rule-2", list)
	}

	// Update
	r2.Threshold = 95
	if !m.UpdateAlertRule(r2) {
		t.Fatal("UpdateAlertRule 应返回 true")
	}
	if got := m.GetAlertRule("rule-2"); got.Threshold != 95 {
		t.Fatalf("更新后 Threshold = %v, want 95", got.Threshold)
	}
	// 保留原 CreatedAt
	origCreatedAt := r2.CreatedAt
	r2.CreatedAt = time.Time{}
	m.UpdateAlertRule(r2)
	if got := m.GetAlertRule("rule-2"); !got.CreatedAt.Equal(origCreatedAt) {
		t.Fatal("UpdateAlertRule 应保留原 CreatedAt")
	}
	// Update 不存在
	if m.UpdateAlertRule(&AlertRule{ID: "nope"}) {
		t.Fatal("UpdateAlertRule 不存在应返回 false")
	}
	// Update nil
	if m.UpdateAlertRule(nil) {
		t.Fatal("UpdateAlertRule(nil) 应返回 false")
	}

	// Delete
	if !m.DeleteAlertRule(r1.ID) {
		t.Fatal("DeleteAlertRule 应返回 true")
	}
	if m.GetAlertRule(r1.ID) != nil {
		t.Fatal("删除后 GetAlertRule 应返回 nil")
	}
	// Delete 不存在
	if m.DeleteAlertRule("nope") {
		t.Fatal("DeleteAlertRule 不存在应返回 false")
	}
}

// ============================================================================
// SeedDemoTopology / FireDueSchedules
// ============================================================================

// TestMemoryStore_SeedDemoTopology 验证演示拓扑播种。
func TestMemoryStore_SeedDemoTopology(t *testing.T) {
	m := NewMemoryStore()
	m.WithDemo(true)
	m.SeedDemoTopology()

	agents := m.Agents("")
	if len(agents) != 3 {
		t.Fatalf("SeedDemoTopology agents = %d, want 3", len(agents))
	}
	// 幂等：再次调用不重复播种
	m.SeedDemoTopology()
	if len(m.Agents("")) != 3 {
		t.Fatal("SeedDemoTopology 应幂等")
	}
	// 非 demo 模式不播种
	m2 := NewMemoryStore()
	m2.SeedDemoTopology()
	if len(m2.Agents("")) != 0 {
		t.Fatal("非 demo 模式不应播种")
	}
}

// TestMemoryStore_FireDueSchedules 验证定时任务派生实例。
func TestMemoryStore_FireDueSchedules(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	// 模板任务：每分钟触发
	m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "cron", Schedule: "* * * * *"})

	now := time.Now()
	n := m.FireDueSchedules(now)
	if n != 1 {
		t.Fatalf("FireDueSchedules = %d, want 1", n)
	}
	// 同一分钟内再次触发不应重复派生
	if n := m.FireDueSchedules(now); n != 0 {
		t.Fatalf("同一分钟二次 FireDueSchedules = %d, want 0", n)
	}
	// 下一分钟可再次触发
	nextMin := now.Add(time.Minute)
	if n := m.FireDueSchedules(nextMin); n != 1 {
		t.Fatalf("下一分钟 FireDueSchedules = %d, want 1", n)
	}
	// 派生实例可被领取
	if m.ClaimTask(a.AgentID) == nil {
		t.Fatal("派生实例应可领取")
	}
}

// ============================================================================
// RBAC User：GetUser / GetUserByUsername / ListUsers / CreateUser / UpdateUser / ChangePassword / DeleteUser
// ============================================================================

// TestMemoryStore_User_CRUD 验证用户完整 CRUD + 深拷贝。
func TestMemoryStore_User_CRUD(t *testing.T) {
	m := NewMemoryStore()
	// 预填充用户存在
	if got := m.GetUserByUsername("admin"); got == nil {
		t.Fatal("预填充 admin 用户应存在")
	}

	// CreateUser
	u := m.CreateUser(&User{Username: "newuser", Email: "new@test.com", PasswordHash: "hash", RoleIDs: []string{"role-viewer"}})
	if u == nil || u.ID == "" {
		t.Fatal("CreateUser 应分配 ID")
	}
	if u.Status != "active" {
		t.Fatalf("默认 Status = %q, want active", u.Status)
	}
	// GetUser
	got := m.GetUser(u.ID)
	if got == nil || got.Username != "newuser" {
		t.Fatalf("GetUser 失败: %+v", got)
	}
	// GetUser 深拷贝 RoleIDs
	got.RoleIDs[0] = "MUTATED"
	if inner := m.GetUser(u.ID); inner.RoleIDs[0] != "role-viewer" {
		t.Fatal("GetUser 未深拷贝 RoleIDs")
	}
	// GetUserByUsername
	if got := m.GetUserByUsername("newuser"); got == nil || got.ID != u.ID {
		t.Fatalf("GetUserByUsername 失败: %+v", got)
	}
	// GetUser 未命中
	if m.GetUser("nope") != nil {
		t.Fatal("GetUser(nope) should be nil")
	}
	if m.GetUserByUsername("nope") != nil {
		t.Fatal("GetUserByUsername(nope) should be nil")
	}

	// UpdateUser
	if !m.UpdateUser(&User{ID: u.ID, Email: "updated@test.com", Status: "disabled", RoleIDs: []string{"role-admin"}}) {
		t.Fatal("UpdateUser 应返回 true")
	}
	updated := m.GetUser(u.ID)
	if updated.Email != "updated@test.com" || updated.Status != "disabled" {
		t.Fatalf("UpdateUser 失败: %+v", updated)
	}
	// Username 和 PasswordHash 不可经 UpdateUser 修改
	if updated.Username != "newuser" {
		t.Fatalf("Username 被误改: %q", updated.Username)
	}
	// UpdateUser 不存在
	if m.UpdateUser(&User{ID: "nope"}) {
		t.Fatal("UpdateUser 不存在应返回 false")
	}
	// UpdateUser nil
	if m.UpdateUser(nil) {
		t.Fatal("UpdateUser(nil) 应返回 false")
	}

	// ChangePassword
	if !m.ChangePassword(u.ID, "newhash") {
		t.Fatal("ChangePassword 应返回 true")
	}
	if got := m.GetUser(u.ID); got.PasswordHash != "newhash" || got.MustChangePassword {
		t.Fatalf("ChangePassword 失败: hash=%q mcp=%v", got.PasswordHash, got.MustChangePassword)
	}
	// ChangePassword 不存在
	if m.ChangePassword("nope", "x") {
		t.Fatal("ChangePassword 不存在应返回 false")
	}

	// ListUsers（含预填充 3 + 新建 1 = 4）
	list := m.ListUsers()
	if len(list) < 4 {
		t.Fatalf("ListUsers = %d, want >= 4", len(list))
	}

	// DeleteUser
	if !m.DeleteUser(u.ID) {
		t.Fatal("DeleteUser 应返回 true")
	}
	if m.GetUser(u.ID) != nil {
		t.Fatal("删除后 GetUser 应返回 nil")
	}
	// 删除后同名用户可重新创建
	if m.CreateUser(&User{Username: "newuser", PasswordHash: "h"}) == nil {
		t.Fatal("删除后应可重新创建同名用户")
	}
	// DeleteUser 不存在
	if m.DeleteUser("nope") {
		t.Fatal("DeleteUser 不存在应返回 false")
	}
}

// TestMemoryStore_User_DuplicateName 验证 CreateUser 用户名重复返回 nil。
func TestMemoryStore_User_DuplicateName(t *testing.T) {
	m := NewMemoryStore()
	// nil 入参
	if m.CreateUser(nil) != nil {
		t.Fatal("CreateUser(nil) 应返回 nil")
	}
	// 空用户名
	if m.CreateUser(&User{Username: ""}) != nil {
		t.Fatal("CreateUser 空用户名应返回 nil")
	}
	// 重复用户名
	m.CreateUser(&User{Username: "dup", PasswordHash: "h"})
	if m.CreateUser(&User{Username: "dup", PasswordHash: "h2"}) != nil {
		t.Fatal("CreateUser 重复用户名应返回 nil")
	}
}

// ============================================================================
// RBAC Role：GetRole / ListRoles / CreateRole / UpdateRole / DeleteRole
// ============================================================================

// TestMemoryStore_Role_CRUD 验证角色完整 CRUD + 深拷贝。
func TestMemoryStore_Role_CRUD(t *testing.T) {
	m := NewMemoryStore()
	// 预填充角色存在
	if got := m.GetRole("role-admin"); got == nil {
		t.Fatal("预填充 admin 角色应存在")
	}

	// CreateRole
	r := m.CreateRole(&Role{Name: "custom-role", Description: "自定义角色", Permissions: []string{"device:read", "task:read"}})
	if r == nil || r.ID == "" {
		t.Fatal("CreateRole 应分配 ID")
	}
	// GetRole 深拷贝 Permissions
	got := m.GetRole(r.ID)
	got.Permissions[0] = "MUTATED"
	if inner := m.GetRole(r.ID); inner.Permissions[0] != "device:read" {
		t.Fatal("GetRole 未深拷贝 Permissions")
	}
	// GetRole 未命中
	if m.GetRole("nope") != nil {
		t.Fatal("GetRole(nope) should be nil")
	}

	// UpdateRole
	if !m.UpdateRole(&Role{ID: r.ID, Description: "更新描述", Permissions: []string{"alert:read"}}) {
		t.Fatal("UpdateRole 应返回 true")
	}
	updated := m.GetRole(r.ID)
	if updated.Description != "更新描述" || len(updated.Permissions) != 1 {
		t.Fatalf("UpdateRole 失败: %+v", updated)
	}
	// Name 不可经 UpdateRole 修改
	if updated.Name != "custom-role" {
		t.Fatalf("Name 被误改: %q", updated.Name)
	}
	// UpdateRole 不存在
	if m.UpdateRole(&Role{ID: "nope"}) {
		t.Fatal("UpdateRole 不存在应返回 false")
	}
	if m.UpdateRole(nil) {
		t.Fatal("UpdateRole(nil) 应返回 false")
	}

	// ListRoles（含预填充 3 + 新建 1 = 4）
	if list := m.ListRoles(); len(list) < 4 {
		t.Fatalf("ListRoles = %d, want >= 4", len(list))
	}

	// DeleteRole
	if !m.DeleteRole(r.ID) {
		t.Fatal("DeleteRole 应返回 true")
	}
	if m.GetRole(r.ID) != nil {
		t.Fatal("删除后 GetRole 应返回 nil")
	}
	if m.DeleteRole("nope") {
		t.Fatal("DeleteRole 不存在应返回 false")
	}
}

// TestMemoryStore_Role_DuplicateName 验证 CreateRole 角色名重复返回 nil。
func TestMemoryStore_Role_DuplicateName(t *testing.T) {
	m := NewMemoryStore()
	if m.CreateRole(nil) != nil {
		t.Fatal("CreateRole(nil) 应返回 nil")
	}
	if m.CreateRole(&Role{Name: ""}) != nil {
		t.Fatal("CreateRole 空名应返回 nil")
	}
	m.CreateRole(&Role{Name: "dup-role"})
	if m.CreateRole(&Role{Name: "dup-role"}) != nil {
		t.Fatal("CreateRole 重复名应返回 nil")
	}
}

// ============================================================================
// RBAC Permission：ListPermissions
// ============================================================================

// TestMemoryStore_ListPermissions 验证 ListPermissions 返回预定义权限。
func TestMemoryStore_ListPermissions(t *testing.T) {
	m := NewMemoryStore()
	perms := m.ListPermissions()
	if len(perms) == 0 {
		t.Fatal("ListPermissions 不应为空")
	}
	// 应包含 device:read
	found := false
	for _, p := range perms {
		if p.Name == "device:read" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("ListPermissions 应包含 device:read")
	}
	// 深拷贝
	perms[0].Name = "MUTATED"
	inner := m.ListPermissions()
	if inner[0].Name == "MUTATED" {
		t.Fatal("ListPermissions 未深拷贝")
	}
}

// ============================================================================
// OS 模板：SaveOSTemplate / ListOSTemplates / GetOSTemplate / DeleteOSTemplate
// ============================================================================

// TestMemoryStore_OSTemplate_CRUD 验证 OS 安装模板完整 CRUD。
func TestMemoryStore_OSTemplate_CRUD(t *testing.T) {
	m := NewMemoryStore()
	// nil 入参
	if err := m.SaveOSTemplate(nil); err != nil {
		t.Fatal("SaveOSTemplate(nil) 应返回 nil")
	}
	// 创建（ID 自动分配，TenantID 归一 default）
	t1 := &OSTemplate{Name: "centos-7", OS: "centos", Version: "7", Arch: "amd64", InstallURL: "http://ks"}
	if err := m.SaveOSTemplate(t1); err != nil {
		t.Fatalf("SaveOSTemplate err = %v", err)
	}
	if t1.ID == "" || t1.TenantID != "default" {
		t.Fatalf("ID/TenantID 未填充: %+v", t1)
	}
	if t1.CreatedAt.IsZero() || t1.UpdatedAt.IsZero() {
		t.Fatal("CreatedAt/UpdatedAt 应被填充")
	}
	// 指定 ID + TenantID
	t2 := &OSTemplate{ID: "os-2", TenantID: "t1", Name: "ubuntu-22", OS: "ubuntu", Version: "22.04"}
	m.SaveOSTemplate(t2)

	// Get 命中
	got := m.GetOSTemplate(t1.ID)
	if got == nil || got.Name != "centos-7" {
		t.Fatalf("GetOSTemplate 命中失败: %+v", got)
	}
	// Get 未命中
	if m.GetOSTemplate("nope") != nil {
		t.Fatal("GetOSTemplate(nope) should be nil")
	}
	// Get 深拷贝
	got.Name = "MUTATED"
	if m.GetOSTemplate(t1.ID).Name != "centos-7" {
		t.Fatal("GetOSTemplate 未深拷贝")
	}

	// List 全量
	if list := m.ListOSTemplates(""); len(list) != 2 {
		t.Fatalf("ListOSTemplates 全量 = %d, want 2", len(list))
	}
	// List 租户过滤
	if list := m.ListOSTemplates("t1"); len(list) != 1 || list[0].ID != "os-2" {
		t.Fatalf("ListOSTemplates(t1) = %+v, want os-2", list)
	}

	// 更新（按 ID 幂等）
	t1.Version = "7.9"
	m.SaveOSTemplate(t1)
	if got := m.GetOSTemplate(t1.ID); got.Version != "7.9" {
		t.Fatalf("更新后 Version = %q, want 7.9", got.Version)
	}

	// Delete
	if !m.DeleteOSTemplate(t1.ID) {
		t.Fatal("DeleteOSTemplate 应返回 true")
	}
	if m.GetOSTemplate(t1.ID) != nil {
		t.Fatal("删除后 GetOSTemplate 应返回 nil")
	}
	if m.DeleteOSTemplate("nope") {
		t.Fatal("DeleteOSTemplate 不存在应返回 false")
	}
}

// ============================================================================
// 中间件模板：SaveMiddlewareTemplate / ListMiddlewareTemplates / GetMiddlewareTemplate / DeleteMiddlewareTemplate
// ============================================================================

// TestMemoryStore_MiddlewareTemplate_CRUD 验证中间件部署模板完整 CRUD。
func TestMemoryStore_MiddlewareTemplate_CRUD(t *testing.T) {
	m := NewMemoryStore()
	if err := m.SaveMiddlewareTemplate(nil); err != nil {
		t.Fatal("SaveMiddlewareTemplate(nil) 应返回 nil")
	}
	// 创建
	t1 := &MiddlewareTemplate{Name: "mysql-single", Type: "mysql", Version: "8.0", Config: "{\"port\":3306}"}
	if err := m.SaveMiddlewareTemplate(t1); err != nil {
		t.Fatalf("SaveMiddlewareTemplate err = %v", err)
	}
	if t1.ID == "" || t1.TenantID != "default" {
		t.Fatalf("ID/TenantID 未填充: %+v", t1)
	}
	t2 := &MiddlewareTemplate{ID: "mw-2", TenantID: "t1", Name: "redis-cluster", Type: "redis", Version: "7.0"}
	m.SaveMiddlewareTemplate(t2)

	// Get
	got := m.GetMiddlewareTemplate(t1.ID)
	if got == nil || got.Name != "mysql-single" {
		t.Fatalf("GetMiddlewareTemplate 命中失败: %+v", got)
	}
	if m.GetMiddlewareTemplate("nope") != nil {
		t.Fatal("GetMiddlewareTemplate(nope) should be nil")
	}
	// 深拷贝
	got.Config = "MUTATED"
	if m.GetMiddlewareTemplate(t1.ID).Config != "{\"port\":3306}" {
		t.Fatal("GetMiddlewareTemplate 未深拷贝")
	}

	// List
	if list := m.ListMiddlewareTemplates(""); len(list) != 2 {
		t.Fatalf("ListMiddlewareTemplates 全量 = %d, want 2", len(list))
	}
	if list := m.ListMiddlewareTemplates("t1"); len(list) != 1 || list[0].ID != "mw-2" {
		t.Fatalf("ListMiddlewareTemplates(t1) = %+v, want mw-2", list)
	}

	// 更新
	t1.Version = "8.0.35"
	m.SaveMiddlewareTemplate(t1)
	if got := m.GetMiddlewareTemplate(t1.ID); got.Version != "8.0.35" {
		t.Fatalf("更新后 Version = %q, want 8.0.35", got.Version)
	}

	// Delete
	if !m.DeleteMiddlewareTemplate(t1.ID) {
		t.Fatal("DeleteMiddlewareTemplate 应返回 true")
	}
	if m.GetMiddlewareTemplate(t1.ID) != nil {
		t.Fatal("删除后应返回 nil")
	}
	if m.DeleteMiddlewareTemplate("nope") {
		t.Fatal("DeleteMiddlewareTemplate 不存在应返回 false")
	}
}

// ============================================================================
// 告警治理 Silence：CreateSilence / DeleteSilence / GetSilence / ListSilences
// ============================================================================

// TestMemoryStore_Silence_CRUD 验证静默规则完整 CRUD + MatchLabels 深拷贝。
func TestMemoryStore_Silence_CRUD(t *testing.T) {
	m := NewMemoryStore()
	// nil 入参
	if m.CreateSilence(nil) != nil {
		t.Fatal("CreateSilence(nil) 应返回 nil")
	}
	// 创建（ID 自动分配，TenantID 归一 default）
	s1 := m.CreateSilence(&SilenceRule{MatchLabels: map[string]string{"severity": "critical"}, Reason: "维护", CreatedBy: "u1"})
	if s1 == nil || s1.ID == "" {
		t.Fatal("CreateSilence 应分配 ID")
	}
	if s1.TenantID != "default" {
		t.Fatalf("TenantID = %q, want default", s1.TenantID)
	}
	s2 := m.CreateSilence(&SilenceRule{ID: "sil-2", TenantID: "t1", Reason: "升级"})

	// Get 命中 + MatchLabels 深拷贝
	got := m.GetSilence(s1.ID)
	if got == nil || got.Reason != "维护" {
		t.Fatalf("GetSilence 命中失败: %+v", got)
	}
	got.MatchLabels["severity"] = "MUTATED"
	if m.GetSilence(s1.ID).MatchLabels["severity"] != "critical" {
		t.Fatal("GetSilence 未深拷贝 MatchLabels")
	}
	// Get 未命中
	if m.GetSilence("nope") != nil {
		t.Fatal("GetSilence(nope) should be nil")
	}

	// List
	if list := m.ListSilences(""); len(list) != 2 {
		t.Fatalf("ListSilences 全量 = %d, want 2", len(list))
	}
	if list := m.ListSilences("t1"); len(list) != 1 || list[0].ID != "sil-2" {
		t.Fatalf("ListSilences(t1) = %+v, want sil-2", list)
	}

	// Delete + 租户校验
	if m.DeleteSilence(s1.ID, "t1") {
		t.Fatal("DeleteSilence 跨租户应返回 false")
	}
	if !m.DeleteSilence(s1.ID, "default") {
		t.Fatal("DeleteSilence 同租户应返回 true")
	}
	if m.GetSilence(s1.ID) != nil {
		t.Fatal("删除后应返回 nil")
	}
	// 空租户可删除
	if !m.DeleteSilence(s2.ID, "") {
		t.Fatal("DeleteSilence 空租户应可删除")
	}
	if m.DeleteSilence("nope", "") {
		t.Fatal("DeleteSilence 不存在应返回 false")
	}
}

// ============================================================================
// 告警治理 NotifyChannel：CreateNotifyChannel / UpdateNotifyChannel / DeleteNotifyChannel / GetNotifyChannel / ListNotifyChannels
// ============================================================================

// TestMemoryStore_NotifyChannel_CRUD 验证通知渠道完整 CRUD。
func TestMemoryStore_NotifyChannel_CRUD(t *testing.T) {
	m := NewMemoryStore()
	if m.CreateNotifyChannel(nil) != nil {
		t.Fatal("CreateNotifyChannel(nil) 应返回 nil")
	}
	// 创建
	c1 := m.CreateNotifyChannel(&NotifyChannel{Name: "钉钉群", Type: "dingtalk", Config: "{\"webhook\":\"url\"}", Enabled: true})
	if c1 == nil || c1.ID == "" || c1.TenantID != "default" {
		t.Fatalf("CreateNotifyChannel 失败: %+v", c1)
	}
	if c1.CreatedAt.IsZero() || c1.UpdatedAt.IsZero() {
		t.Fatal("CreatedAt/UpdatedAt 应被填充")
	}
	c2 := m.CreateNotifyChannel(&NotifyChannel{ID: "ch-2", TenantID: "t1", Name: "飞书", Type: "feishu"})

	// Get
	got := m.GetNotifyChannel(c1.ID)
	if got == nil || got.Name != "钉钉群" {
		t.Fatalf("GetNotifyChannel 命中失败: %+v", got)
	}
	if m.GetNotifyChannel("nope") != nil {
		t.Fatal("GetNotifyChannel(nope) should be nil")
	}
	// 深拷贝
	got.Config = "MUTATED"
	if m.GetNotifyChannel(c1.ID).Config != "{\"webhook\":\"url\"}" {
		t.Fatal("GetNotifyChannel 未深拷贝")
	}

	// List
	if list := m.ListNotifyChannels(""); len(list) != 2 {
		t.Fatalf("ListNotifyChannels 全量 = %d, want 2", len(list))
	}
	if list := m.ListNotifyChannels("t1"); len(list) != 1 || list[0].ID != "ch-2" {
		t.Fatalf("ListNotifyChannels(t1) = %+v, want ch-2", list)
	}

	// Update
	c1.Name = "钉钉群2"
	if !m.UpdateNotifyChannel(c1) {
		t.Fatal("UpdateNotifyChannel 应返回 true")
	}
	if got := m.GetNotifyChannel(c1.ID); got.Name != "钉钉群2" {
		t.Fatalf("更新后 Name = %q, want 钉钉群2", got.Name)
	}
	// 保留原 CreatedAt
	origCreatedAt := c1.CreatedAt
	c1.CreatedAt = time.Time{}
	m.UpdateNotifyChannel(c1)
	if got := m.GetNotifyChannel(c1.ID); !got.CreatedAt.Equal(origCreatedAt) {
		t.Fatal("UpdateNotifyChannel 应保留原 CreatedAt")
	}
	// Update 不存在
	if m.UpdateNotifyChannel(&NotifyChannel{ID: "nope"}) {
		t.Fatal("UpdateNotifyChannel 不存在应返回 false")
	}
	if m.UpdateNotifyChannel(nil) {
		t.Fatal("UpdateNotifyChannel(nil) 应返回 false")
	}

	// Delete + 租户校验
	if m.DeleteNotifyChannel(c2.ID, "t2") {
		t.Fatal("DeleteNotifyChannel 跨租户应返回 false")
	}
	if !m.DeleteNotifyChannel(c2.ID, "t1") {
		t.Fatal("DeleteNotifyChannel 同租户应返回 true")
	}
	if m.DeleteNotifyChannel("nope", "") {
		t.Fatal("DeleteNotifyChannel 不存在应返回 false")
	}
}

// ============================================================================
// 告警治理 NotifyTemplate：CreateNotifyTemplate / UpdateNotifyTemplate / DeleteNotifyTemplate / GetNotifyTemplate / ListNotifyTemplates
// ============================================================================

// TestMemoryStore_NotifyTemplate_CRUD 验证通知模板完整 CRUD。
func TestMemoryStore_NotifyTemplate_CRUD(t *testing.T) {
	m := NewMemoryStore()
	if m.CreateNotifyTemplate(nil) != nil {
		t.Fatal("CreateNotifyTemplate(nil) 应返回 nil")
	}
	// 创建
	t1 := m.CreateNotifyTemplate(&NotifyTemplate{Name: "告警模板", Type: "alert", Title: "{{.AlertID}}", Body: "{{.Message}}", Format: "markdown"})
	if t1 == nil || t1.ID == "" || t1.TenantID != "default" {
		t.Fatalf("CreateNotifyTemplate 失败: %+v", t1)
	}
	t2 := m.CreateNotifyTemplate(&NotifyTemplate{ID: "tpl-2", TenantID: "t1", Name: "任务模板", Type: "task"})

	// Get
	got := m.GetNotifyTemplate(t1.ID)
	if got == nil || got.Name != "告警模板" {
		t.Fatalf("GetNotifyTemplate 命中失败: %+v", got)
	}
	if m.GetNotifyTemplate("nope") != nil {
		t.Fatal("GetNotifyTemplate(nope) should be nil")
	}
	// 深拷贝
	got.Body = "MUTATED"
	if m.GetNotifyTemplate(t1.ID).Body != "{{.Message}}" {
		t.Fatal("GetNotifyTemplate 未深拷贝")
	}

	// List
	if list := m.ListNotifyTemplates(""); len(list) != 2 {
		t.Fatalf("ListNotifyTemplates 全量 = %d, want 2", len(list))
	}
	if list := m.ListNotifyTemplates("t1"); len(list) != 1 || list[0].ID != "tpl-2" {
		t.Fatalf("ListNotifyTemplates(t1) = %+v, want tpl-2", list)
	}

	// Update
	t1.Title = "新标题"
	if !m.UpdateNotifyTemplate(t1) {
		t.Fatal("UpdateNotifyTemplate 应返回 true")
	}
	if got := m.GetNotifyTemplate(t1.ID); got.Title != "新标题" {
		t.Fatalf("更新后 Title = %q, want 新标题", got.Title)
	}
	// 保留原 CreatedAt
	origCreatedAt := t1.CreatedAt
	t1.CreatedAt = time.Time{}
	m.UpdateNotifyTemplate(t1)
	if got := m.GetNotifyTemplate(t1.ID); !got.CreatedAt.Equal(origCreatedAt) {
		t.Fatal("UpdateNotifyTemplate 应保留原 CreatedAt")
	}
	if m.UpdateNotifyTemplate(&NotifyTemplate{ID: "nope"}) {
		t.Fatal("UpdateNotifyTemplate 不存在应返回 false")
	}
	if m.UpdateNotifyTemplate(nil) {
		t.Fatal("UpdateNotifyTemplate(nil) 应返回 false")
	}

	// Delete + 租户校验
	if m.DeleteNotifyTemplate(t2.ID, "t2") {
		t.Fatal("DeleteNotifyTemplate 跨租户应返回 false")
	}
	if !m.DeleteNotifyTemplate(t2.ID, "t1") {
		t.Fatal("DeleteNotifyTemplate 同租户应返回 true")
	}
	if m.DeleteNotifyTemplate("nope", "") {
		t.Fatal("DeleteNotifyTemplate 不存在应返回 false")
	}
}

// ============================================================================
// 刷新令牌：SaveRefreshToken / GetRefreshToken / DeleteRefreshToken / ConsumeRefreshToken
// ============================================================================

// TestMemoryStore_RefreshToken_CRUD 验证刷新令牌完整 CRUD + 深拷贝。
func TestMemoryStore_RefreshToken_CRUD(t *testing.T) {
	m := NewMemoryStore()
	// nil 入参
	if err := m.SaveRefreshToken(nil); err != nil {
		t.Fatal("SaveRefreshToken(nil) 应返回 nil")
	}
	// 空 TokenHash 报错
	if err := m.SaveRefreshToken(&RefreshToken{UserID: "u1"}); err == nil {
		t.Fatal("SaveRefreshToken 空 TokenHash 应报错")
	}
	// 保存（TenantID 归一 default）
	rt := &RefreshToken{TokenHash: "hash-1", UserID: "u1", DeviceFP: "fp-1", ExpiresAt: time.Now().Add(24 * time.Hour)}
	if err := m.SaveRefreshToken(rt); err != nil {
		t.Fatalf("SaveRefreshToken err = %v", err)
	}
	if rt.TenantID != "default" {
		t.Fatalf("TenantID = %q, want default", rt.TenantID)
	}
	if rt.CreatedAt.IsZero() {
		t.Fatal("CreatedAt 应被填充")
	}
	// Get 命中
	got := m.GetRefreshToken("hash-1")
	if got == nil || got.UserID != "u1" {
		t.Fatalf("GetRefreshToken 命中失败: %+v", got)
	}
	// Get 深拷贝
	got.DeviceFP = "MUTATED"
	if m.GetRefreshToken("hash-1").DeviceFP != "fp-1" {
		t.Fatal("GetRefreshToken 未深拷贝")
	}
	// Get 未命中 + 空 hash
	if m.GetRefreshToken("nope") != nil {
		t.Fatal("GetRefreshToken(nope) should be nil")
	}
	if m.GetRefreshToken("") != nil {
		t.Fatal("GetRefreshToken(\"\") should be nil")
	}

	// 幂等 upsert
	rt2 := &RefreshToken{TokenHash: "hash-1", UserID: "u2", TenantID: "t1"}
	m.SaveRefreshToken(rt2)
	if got := m.GetRefreshToken("hash-1"); got.UserID != "u2" {
		t.Fatalf("upsert 后 UserID = %q, want u2", got.UserID)
	}

	// Delete
	if !m.DeleteRefreshToken("hash-1") {
		t.Fatal("DeleteRefreshToken 应返回 true")
	}
	if m.GetRefreshToken("hash-1") != nil {
		t.Fatal("删除后应返回 nil")
	}
	// Delete 不存在 + 空 hash
	if m.DeleteRefreshToken("nope") {
		t.Fatal("DeleteRefreshToken 不存在应返回 false")
	}
	if m.DeleteRefreshToken("") {
		t.Fatal("DeleteRefreshToken(\"\") 应返回 false")
	}
}

// TestMemoryStore_RefreshToken_Consume 验证 ConsumeRefreshToken 原子消费（读取+删除）。
func TestMemoryStore_RefreshToken_Consume(t *testing.T) {
	m := NewMemoryStore()
	m.SaveRefreshToken(&RefreshToken{TokenHash: "hash-c", UserID: "u1", TenantID: "t1"})

	// 首次消费成功
	got, ok := m.ConsumeRefreshToken("hash-c")
	if !ok || got == nil || got.UserID != "u1" {
		t.Fatalf("ConsumeRefreshToken 首次 = (%+v, %v), want (non-nil, true)", got, ok)
	}
	// 二次消费失败（已删除）
	if got, ok := m.ConsumeRefreshToken("hash-c"); ok || got != nil {
		t.Fatal("ConsumeRefreshToken 二次应返回 (nil, false)")
	}
	// 不存在
	if got, ok := m.ConsumeRefreshToken("nope"); ok || got != nil {
		t.Fatal("ConsumeRefreshToken 不存在应返回 (nil, false)")
	}
	// 空 hash
	if got, ok := m.ConsumeRefreshToken(""); ok || got != nil {
		t.Fatal("ConsumeRefreshToken(\"\") 应返回 (nil, false)")
	}
}

// ============================================================================
// Agent 日志：SaveLogs / AgentLogs
// ============================================================================

// TestMemoryStore_AgentLogs_SaveQuery 验证 Agent 日志保存与查询 + 租户隔离 + 深拷贝。
func TestMemoryStore_AgentLogs_SaveQuery(t *testing.T) {
	m := NewMemoryStore()
	// nil 入参
	if err := m.SaveLogs("t1", nil); err != nil {
		t.Fatal("SaveLogs(nil) 应返回 nil")
	}
	// 保存
	report := &proto.LogReport{
		AgentID:     "agent-1",
		TenantID:    "fake", // 应被控制面回填的 tenantID 覆盖
		Hostname:    "web-01",
		LogName:     "/var/log/syslog",
		CollectedAt: time.Now(),
		Lines: []proto.LogLine{
			{Timestamp: time.Now(), Level: "INFO", Message: "line1"},
			{Timestamp: time.Now(), Level: "ERROR", Message: "line2"},
		},
	}
	if err := m.SaveLogs("t1", report); err != nil {
		t.Fatalf("SaveLogs err = %v", err)
	}
	// 查询全部
	got := m.AgentLogs("", "", "")
	if len(got) != 1 {
		t.Fatalf("AgentLogs 全量 = %d, want 1", len(got))
	}
	// 租户隔离：强制以控制面回填为准
	if got[0].TenantID != "t1" {
		t.Fatalf("TenantID = %q, want t1（控制面回填）", got[0].TenantID)
	}
	if len(got[0].Lines) != 2 {
		t.Fatalf("Lines = %d, want 2", len(got[0].Lines))
	}
	// 深拷贝 Lines
	got[0].Lines[0].Message = "MUTATED"
	inner := m.AgentLogs("", "", "")
	if inner[0].Lines[0].Message != "line1" {
		t.Fatal("AgentLogs 未深拷贝 Lines")
	}

	// 按租户过滤
	if got := m.AgentLogs("t1", "", ""); len(got) != 1 {
		t.Fatalf("AgentLogs(t1) = %d, want 1", len(got))
	}
	if got := m.AgentLogs("t2", "", ""); len(got) != 0 {
		t.Fatalf("AgentLogs(t2) = %d, want 0", len(got))
	}
	// 按 agent 过滤
	if got := m.AgentLogs("", "agent-1", ""); len(got) != 1 {
		t.Fatalf("AgentLogs(agent-1) = %d, want 1", len(got))
	}
	if got := m.AgentLogs("", "agent-nope", ""); len(got) != 0 {
		t.Fatalf("AgentLogs(agent-nope) = %d, want 0", len(got))
	}
	// 按 logName 过滤
	if got := m.AgentLogs("", "", "/var/log/syslog"); len(got) != 1 {
		t.Fatalf("AgentLogs(syslog) = %d, want 1", len(got))
	}
	if got := m.AgentLogs("", "", "/var/log/nope"); len(got) != 0 {
		t.Fatalf("AgentLogs(nope) = %d, want 0", len(got))
	}

	// 多批次
	m.SaveLogs("t1", &proto.LogReport{AgentID: "agent-1", LogName: "/var/log/app", Lines: []proto.LogLine{{Message: "app1"}}})
	if got := m.AgentLogs("t1", "", ""); len(got) != 2 {
		t.Fatalf("多批次 AgentLogs = %d, want 2", len(got))
	}
}
