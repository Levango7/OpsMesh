// multi_schema_proxy_test.go 测试 MultiSchemaStore 的代理方法路由。
//
// 覆盖范围（用 MemoryStore 作为 mock factory，无需 MySQL）：
//   - 全局路由（globalStore）：RBAC / K8s / 模板 / 刷新令牌 / 告警治理 / Agent 日志
//   - 租户路由（storeFor）：告警规则 / 配额 / 任务审批 / 设备指标 / 任务查询
//
// 测试策略与 multi_schema_test.go 一致：注入 mockStoreFactory（MemoryStore mock）。
package store

import (
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// newTestMultiSchema 构造测试用 MultiSchemaStore（MemoryStore mock factory）。
// 返回 MultiSchemaStore 和已创建 schema 名列表（供调试）。
func newTestMultiSchema() (*MultiSchemaStore, *[]string) {
	created := []string{}
	m := newMultiSchemaWithFactory(DefaultSchemaNamer("opsmesh_tenant_"), mockStoreFactory(&created))
	return m, &created
}

// ============================================================================
// 全局路由代理：RBAC / K8s / 模板 / 刷新令牌 / 告警治理 / Agent 日志
// ============================================================================

// TestMultiSchemaStore_GlobalProxy_RBAC 验证 RBAC 代理方法路由到全局 store。
func TestMultiSchemaStore_GlobalProxy_RBAC(t *testing.T) {
	m, _ := newTestMultiSchema()

	// 预填充用户应可见
	if got := m.GetUser("user-admin"); got == nil {
		t.Fatal("GetUser 预填充 admin 应存在")
	}
	if got := m.GetUserByUsername("admin"); got == nil {
		t.Fatal("GetUserByUsername 预填充 admin 应存在")
	}
	if got := m.GetUser("nope"); got != nil {
		t.Fatal("GetUser 不存在应返回 nil")
	}

	// CreateUser
	u := m.CreateUser(&User{Username: "proxy-user", PasswordHash: "h", RoleIDs: []string{"role-viewer"}})
	if u == nil || u.ID == "" {
		t.Fatal("CreateUser 代理失败")
	}
	// UpdateUser
	if !m.UpdateUser(&User{ID: u.ID, Email: "proxy@test.com"}) {
		t.Fatal("UpdateUser 代理失败")
	}
	// ChangePassword
	if !m.ChangePassword(u.ID, "newhash") {
		t.Fatal("ChangePassword 代理失败")
	}
	// ListUsers
	if list := m.ListUsers(); len(list) < 4 {
		t.Fatalf("ListUsers = %d, want >= 4", len(list))
	}
	// DeleteUser
	if !m.DeleteUser(u.ID) {
		t.Fatal("DeleteUser 代理失败")
	}

	// Role
	r := m.CreateRole(&Role{Name: "proxy-role", Permissions: []string{"device:read"}})
	if r == nil {
		t.Fatal("CreateRole 代理失败")
	}
	if m.GetRole(r.ID) == nil {
		t.Fatal("GetRole 代理失败")
	}
	if !m.UpdateRole(&Role{ID: r.ID, Description: "updated"}) {
		t.Fatal("UpdateRole 代理失败")
	}
	if list := m.ListRoles(); len(list) < 4 {
		t.Fatalf("ListRoles = %d, want >= 4", len(list))
	}
	if !m.DeleteRole(r.ID) {
		t.Fatal("DeleteRole 代理失败")
	}

	// Permission
	if perms := m.ListPermissions(); len(perms) == 0 {
		t.Fatal("ListPermissions 代理失败")
	}
}

// TestMultiSchemaStore_GlobalProxy_K8s 验证 K8s 集群代理方法路由到全局 store。
func TestMultiSchemaStore_GlobalProxy_K8s(t *testing.T) {
	m, _ := newTestMultiSchema()

	c := &K8sCluster{Name: "proxy-cluster", Server: "https://1.2.3.4:6443", Kubeconfig: "cfg", Status: "online"}
	if err := m.SaveK8sCluster(c); err != nil {
		t.Fatalf("SaveK8sCluster 代理失败: %v", err)
	}
	if got := m.GetK8sCluster(c.ID); got == nil || got.Name != "proxy-cluster" {
		t.Fatalf("GetK8sCluster 代理失败: %+v", got)
	}
	if list := m.ListK8sClusters(""); len(list) != 1 {
		t.Fatalf("ListK8sClusters = %d, want 1", len(list))
	}
	if !m.DeleteK8sCluster(c.ID) {
		t.Fatal("DeleteK8sCluster 代理失败")
	}
}

// TestMultiSchemaStore_GlobalProxy_Templates 验证 OS/中间件模板代理方法路由到全局 store。
func TestMultiSchemaStore_GlobalProxy_Templates(t *testing.T) {
	m, _ := newTestMultiSchema()

	// OS 模板
	ot := &OSTemplate{Name: "proxy-centos", OS: "centos", Version: "7"}
	if err := m.SaveOSTemplate(ot); err != nil {
		t.Fatalf("SaveOSTemplate 代理失败: %v", err)
	}
	if got := m.GetOSTemplate(ot.ID); got == nil || got.Name != "proxy-centos" {
		t.Fatalf("GetOSTemplate 代理失败: %+v", got)
	}
	if list := m.ListOSTemplates(""); len(list) != 1 {
		t.Fatalf("ListOSTemplates = %d, want 1", len(list))
	}
	if !m.DeleteOSTemplate(ot.ID) {
		t.Fatal("DeleteOSTemplate 代理失败")
	}

	// 中间件模板
	mt := &MiddlewareTemplate{Name: "proxy-mysql", Type: "mysql", Version: "8.0"}
	if err := m.SaveMiddlewareTemplate(mt); err != nil {
		t.Fatalf("SaveMiddlewareTemplate 代理失败: %v", err)
	}
	if got := m.GetMiddlewareTemplate(mt.ID); got == nil || got.Name != "proxy-mysql" {
		t.Fatalf("GetMiddlewareTemplate 代理失败: %+v", got)
	}
	if list := m.ListMiddlewareTemplates(""); len(list) != 1 {
		t.Fatalf("ListMiddlewareTemplates = %d, want 1", len(list))
	}
	if !m.DeleteMiddlewareTemplate(mt.ID) {
		t.Fatal("DeleteMiddlewareTemplate 代理失败")
	}
}

// TestMultiSchemaStore_GlobalProxy_RefreshToken 验证刷新令牌代理方法路由到全局 store。
func TestMultiSchemaStore_GlobalProxy_RefreshToken(t *testing.T) {
	m, _ := newTestMultiSchema()

	rt := &RefreshToken{TokenHash: "proxy-hash", UserID: "u1", ExpiresAt: time.Now().Add(24 * time.Hour)}
	if err := m.SaveRefreshToken(rt); err != nil {
		t.Fatalf("SaveRefreshToken 代理失败: %v", err)
	}
	if got := m.GetRefreshToken("proxy-hash"); got == nil || got.UserID != "u1" {
		t.Fatalf("GetRefreshToken 代理失败: %+v", got)
	}
	// ConsumeRefreshToken
	got, ok := m.ConsumeRefreshToken("proxy-hash")
	if !ok || got == nil {
		t.Fatal("ConsumeRefreshToken 代理失败")
	}
	// 二次消费失败
	if _, ok := m.ConsumeRefreshToken("proxy-hash"); ok {
		t.Fatal("二次 ConsumeRefreshToken 应失败")
	}
	// 重新保存后 Delete
	m.SaveRefreshToken(&RefreshToken{TokenHash: "proxy-hash2", UserID: "u2"})
	if !m.DeleteRefreshToken("proxy-hash2") {
		t.Fatal("DeleteRefreshToken 代理失败")
	}
}

// TestMultiSchemaStore_GlobalProxy_AlertGov 验证告警治理代理方法路由到全局 store。
func TestMultiSchemaStore_GlobalProxy_AlertGov(t *testing.T) {
	m, _ := newTestMultiSchema()

	// Silence
	s := m.CreateSilence(&SilenceRule{Reason: "proxy-silence", MatchLabels: map[string]string{"severity": "critical"}})
	if s == nil || s.ID == "" {
		t.Fatal("CreateSilence 代理失败")
	}
	if got := m.GetSilence(s.ID); got == nil || got.Reason != "proxy-silence" {
		t.Fatalf("GetSilence 代理失败: %+v", got)
	}
	if list := m.ListSilences(""); len(list) != 1 {
		t.Fatalf("ListSilences = %d, want 1", len(list))
	}
	if !m.DeleteSilence(s.ID, "") {
		t.Fatal("DeleteSilence 代理失败")
	}

	// NotifyChannel
	c := m.CreateNotifyChannel(&NotifyChannel{Name: "proxy-ch", Type: "dingtalk", Enabled: true})
	if c == nil || c.ID == "" {
		t.Fatal("CreateNotifyChannel 代理失败")
	}
	if !m.UpdateNotifyChannel(&NotifyChannel{ID: c.ID, Name: "updated-ch"}) {
		t.Fatal("UpdateNotifyChannel 代理失败")
	}
	if got := m.GetNotifyChannel(c.ID); got == nil || got.Name != "updated-ch" {
		t.Fatalf("GetNotifyChannel 代理失败: %+v", got)
	}
	if list := m.ListNotifyChannels(""); len(list) != 1 {
		t.Fatalf("ListNotifyChannels = %d, want 1", len(list))
	}
	if !m.DeleteNotifyChannel(c.ID, "") {
		t.Fatal("DeleteNotifyChannel 代理失败")
	}

	// NotifyTemplate
	tpl := m.CreateNotifyTemplate(&NotifyTemplate{Name: "proxy-tpl", Type: "alert", Title: "t", Body: "b"})
	if tpl == nil || tpl.ID == "" {
		t.Fatal("CreateNotifyTemplate 代理失败")
	}
	if !m.UpdateNotifyTemplate(&NotifyTemplate{ID: tpl.ID, Title: "updated"}) {
		t.Fatal("UpdateNotifyTemplate 代理失败")
	}
	if got := m.GetNotifyTemplate(tpl.ID); got == nil || got.Title != "updated" {
		t.Fatalf("GetNotifyTemplate 代理失败: %+v", got)
	}
	if list := m.ListNotifyTemplates(""); len(list) != 1 {
		t.Fatalf("ListNotifyTemplates = %d, want 1", len(list))
	}
	if !m.DeleteNotifyTemplate(tpl.ID, "") {
		t.Fatal("DeleteNotifyTemplate 代理失败")
	}
}

// TestMultiSchemaStore_GlobalProxy_AgentLogs 验证 Agent 日志代理方法路由到全局 store。
func TestMultiSchemaStore_GlobalProxy_AgentLogs(t *testing.T) {
	m, _ := newTestMultiSchema()

	report := &proto.LogReport{
		AgentID: "agent-1", LogName: "/var/log/syslog",
		Lines: []proto.LogLine{{Message: "line1"}},
	}
	if err := m.SaveLogs("t1", report); err != nil {
		t.Fatalf("SaveLogs 代理失败: %v", err)
	}
	got := m.AgentLogs("t1", "", "")
	if len(got) != 1 {
		t.Fatalf("AgentLogs = %d, want 1", len(got))
	}
	if got[0].TenantID != "t1" {
		t.Fatalf("TenantID = %q, want t1", got[0].TenantID)
	}
	// nil 入参
	if err := m.SaveLogs("t1", nil); err != nil {
		t.Fatal("SaveLogs(nil) 应返回 nil")
	}
}

// ============================================================================
// 租户路由代理：告警规则 / 配额 / 任务审批 / 设备指标 / 任务查询
// ============================================================================

// TestMultiSchemaStore_TenantProxy_AlertRule 验证告警规则代理方法按 tenantID 路由。
func TestMultiSchemaStore_TenantProxy_AlertRule(t *testing.T) {
	m, _ := newTestMultiSchema()

	// CreateAlertRule（从 r.TenantID 路由）
	r := m.CreateAlertRule(&AlertRule{TenantID: "t1", Metric: "cpu", Op: ">", Threshold: 90, Enabled: true})
	if r == nil || r.ID == "" {
		t.Fatal("CreateAlertRule 代理失败")
	}
	// ListAlertRules（直接用 tenantID 路由）
	if list := m.ListAlertRules("t1"); len(list) != 1 {
		t.Fatalf("ListAlertRules(t1) = %d, want 1", len(list))
	}
	// GetAlertRule（遍历所有 schema）
	if got := m.GetAlertRule(r.ID); got == nil || got.Metric != "cpu" {
		t.Fatalf("GetAlertRule 代理失败: %+v", got)
	}
	// UpdateAlertRule（遍历所有 schema）
	r.Threshold = 95
	if !m.UpdateAlertRule(r) {
		t.Fatal("UpdateAlertRule 代理失败")
	}
	// DeleteAlertRule（遍历所有 schema）
	if !m.DeleteAlertRule(r.ID) {
		t.Fatal("DeleteAlertRule 代理失败")
	}
	// nil 入参
	if m.CreateAlertRule(nil) != nil {
		t.Fatal("CreateAlertRule(nil) 应返回 nil")
	}
	if m.UpdateAlertRule(nil) {
		t.Fatal("UpdateAlertRule(nil) 应返回 false")
	}
}

// TestMultiSchemaStore_TenantProxy_Quota 验证配额代理方法按 tenantID 路由。
func TestMultiSchemaStore_TenantProxy_Quota(t *testing.T) {
	m, _ := newTestMultiSchema()

	cfg := &QuotaConfig{MaxDevices: 100, MaxTasks: 50}
	if err := m.SetQuota("t1", cfg); err != nil {
		t.Fatalf("SetQuota 代理失败: %v", err)
	}
	got, err := m.GetQuota("t1")
	if err != nil || got == nil || got.MaxDevices != 100 {
		t.Fatalf("GetQuota 代理失败: (%+v, %v)", got, err)
	}
	// 空租户
	if got, _ := m.GetQuota(""); got != nil {
		t.Fatal("GetQuota(\"\") 应返回 nil")
	}
	if err := m.SetQuota("", cfg); err == nil {
		t.Fatal("SetQuota(\"\") 应报错")
	}
}

// TestMultiSchemaStore_TenantProxy_ApproveReject 验证任务审批代理方法按 tenantID 路由。
func TestMultiSchemaStore_TenantProxy_ApproveReject(t *testing.T) {
	m, _ := newTestMultiSchema()

	// 先注册 agent + 创建审批任务
	a := m.Register(&proto.AgentInfo{AgentID: "agent-1", Segment: "seg-a", TenantID: "t1"})
	tk := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "danger", ApprovalRequired: true})
	if tk.Status != "pending_approval" {
		t.Fatalf("审批任务状态 = %q, want pending_approval", tk.Status)
	}
	// ApproveTask
	if !m.ApproveTask(tk.TaskID, "t1", "u1") {
		t.Fatal("ApproveTask 代理失败")
	}
	if got := m.TaskByID(tk.TaskID); got.Status != "pending" {
		t.Fatalf("审批后状态 = %q, want pending", got.Status)
	}
	// RejectTask（创建另一个审批任务）
	tk2 := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "danger2", ApprovalRequired: true})
	if !m.RejectTask(tk2.TaskID, "t1", "u1") {
		t.Fatal("RejectTask 代理失败")
	}
	if got := m.TaskByID(tk2.TaskID); got.Status != "rejected" {
		t.Fatalf("驳回后状态 = %q, want rejected", got.Status)
	}
	// 空租户
	if m.ApproveTask("nope", "", "") {
		t.Fatal("ApproveTask 空租户应返回 false")
	}
	if m.RejectTask("nope", "", "") {
		t.Fatal("RejectTask 空租户应返回 false")
	}
}

// TestMultiSchemaStore_TenantProxy_DeviceMetrics 验证设备指标代理方法经反查索引路由。
func TestMultiSchemaStore_TenantProxy_DeviceMetrics(t *testing.T) {
	m, _ := newTestMultiSchema()

	// 注册 agent（建立 agentTenant 反查索引）
	a := m.Register(&proto.AgentInfo{AgentID: "agent-1", Segment: "seg-a", TenantID: "t1"})
	devID := "dev-" + a.AgentID
	// UpsertDevice 建立 deviceTenant 反查索引（Register 不建 device 索引）
	m.UpsertDevice(&proto.DeviceInfo{DeviceID: devID, Segment: "seg-a", TenantID: "t1", AgentID: a.AgentID, State: "online"})

	// StoreDeviceMetrics
	metrics := &proto.DeviceMetrics{DeviceID: devID, CPU: proto.CPUMetrics{Cores: 4, Usage: 50}}
	m.StoreDeviceMetrics(devID, metrics)
	// DeviceMetrics
	got := m.DeviceMetrics(devID)
	if got == nil || got.CPU.Cores != 4 {
		t.Fatalf("DeviceMetrics 代理失败: %+v", got)
	}
	// DeviceMetricsHistory
	m.StoreDeviceMetrics(devID, &proto.DeviceMetrics{DeviceID: devID, CPU: proto.CPUMetrics{Cores: 8}, CollectedAt: time.Now()})
	if hist := m.DeviceMetricsHistory(devID, time.Time{}); len(hist) != 2 {
		t.Fatalf("DeviceMetricsHistory = %d, want 2", len(hist))
	}
	// 未知设备
	if got := m.DeviceMetrics("unknown-dev"); got != nil {
		t.Fatal("DeviceMetrics 未知设备应返回 nil")
	}
	m.StoreDeviceMetrics("unknown-dev", metrics)       // 不应 panic
	m.DeviceMetricsHistory("unknown-dev", time.Time{}) // 不应 panic
	// AgentSecret
	if got := m.AgentSecret(a.AgentID); got == "" {
		t.Fatal("AgentSecret 代理失败")
	}
	if got := m.AgentSecret("unknown-agent"); got != "" {
		t.Fatal("AgentSecret 未知 agent 应返回空串")
	}
}

// TestMultiSchemaStore_TenantProxy_TaskQueries 验证任务查询代理方法经反查索引路由。
func TestMultiSchemaStore_TenantProxy_TaskQueries(t *testing.T) {
	m, _ := newTestMultiSchema()

	// 注册 agent + 创建任务（建立 taskTenant 反查索引）
	a := m.Register(&proto.AgentInfo{AgentID: "agent-1", Segment: "seg-a", TenantID: "t1"})
	tk := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "echo"})

	// TaskByID
	if got := m.TaskByID(tk.TaskID); got == nil || got.TaskID != tk.TaskID {
		t.Fatalf("TaskByID 代理失败: %+v", got)
	}
	// 未知 taskID
	if m.TaskByID("nope") != nil {
		t.Fatal("TaskByID 未知应返回 nil")
	}
	// GetTasks
	if got := m.GetTasks(a.AgentID); len(got) != 1 {
		t.Fatalf("GetTasks = %d, want 1", len(got))
	}
	// ClaimTask + TaskResult
	m.ClaimTask(a.AgentID)
	m.SubmitResult(&proto.TaskResult{TaskID: tk.TaskID, AgentID: a.AgentID, ExitCode: 0, Stdout: "ok"})
	if got := m.TaskResult(tk.TaskID); got == nil || got.Stdout != "ok" {
		t.Fatalf("TaskResult 代理失败: %+v", got)
	}
	// TasksByParent（创建带 ParentID 的任务）
	m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "child", ParentID: tk.TaskID})
	if got := m.TasksByParent(tk.TaskID); len(got) != 1 {
		t.Fatalf("TasksByParent = %d, want 1", len(got))
	}
	// CancelledTaskIDs
	m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "cancel-me"})
	// 取消最后一个任务
	tasks := m.GetTasks(a.AgentID)
	if len(tasks) > 0 {
		m.CancelTask(tasks[len(tasks)-1].TaskID, "t1")
	}
	// CancelTask 代理
	if m.CancelTask("nope", "t1") {
		t.Fatal("CancelTask 不存在应返回 false")
	}
}

// TestMultiSchemaStore_TenantProxy_Alerts 验证告警代理方法按 tenantID 路由。
func TestMultiSchemaStore_TenantProxy_Alerts(t *testing.T) {
	m, _ := newTestMultiSchema()

	// AddAlert（从 a.TenantID 路由）
	m.AddAlert(&proto.Alert{AlertID: "a1", TenantID: "t1", DeviceID: "d1", Severity: "critical"})
	// Alerts
	if got := m.Alerts("t1"); len(got) != 1 {
		t.Fatalf("Alerts(t1) = %d, want 1", len(got))
	}
	// Alert（遍历所有 schema）
	if got := m.Alert("a1"); got == nil || got.Severity != "critical" {
		t.Fatalf("Alert 代理失败: %+v", got)
	}
	// AckAlert
	if !m.AckAlert("a1", "t1", "u1") {
		t.Fatal("AckAlert 代理失败")
	}
	// SilenceAlert
	if !m.SilenceAlert("a1", "t1", "u1", time.Time{}, "维护") {
		t.Fatal("SilenceAlert 代理失败")
	}
	// nil 入参
	m.AddAlert(nil) // 不应 panic
	// 空租户
	if m.Alerts("") != nil {
		t.Fatal("Alerts(\"\") 应返回 nil（空租户路由失败）")
	}
}

// ============================================================================
// 构造方法：NewMultiSchemaStore / WithBus / WithSecret
// ============================================================================

// TestMultiSchemaStore_NewWithBusSecret 验证 NewMultiSchemaStore 构造 + WithBus/WithSecret 注入。
// NewMultiSchemaStore 不创建 SQLStore（惰性创建），无需数据库连接。
func TestMultiSchemaStore_NewWithBusSecret(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m, err := NewMultiSchemaStore("root:@tcp(127.0.0.1:3306)/base", "", namer)
	if err != nil {
		t.Fatalf("NewMultiSchemaStore err = %v", err)
	}
	if m == nil {
		t.Fatal("NewMultiSchemaStore 返回 nil")
	}
	// nil namer 报错
	if _, err := NewMultiSchemaStore("", "", nil); err == nil {
		t.Fatal("NewMultiSchemaStore nil namer 应报错")
	}
	// WithBus
	bus := &recordingBus{}
	m.WithBus(bus)
	// WithSecret
	m.WithSecret("test-secret")
	m.WithSecret("") // 空密钥不覆盖
	// WithDemo
	m.WithDemo(true)
}

// TestErrString_Error 验证 errString.Error() 方法（session.go 中的自定义错误类型）。
func TestErrString_Error(t *testing.T) {
	// 通过 InProcessSessionStore 触发 errString 错误
	s := NewInProcessSessionStore()
	// CreateChangePasswordToken 空 token 应返回 errChangePasswordTokenRequired (errString 类型)
	err := s.CreateChangePasswordToken("", "u1", 5*time.Minute)
	if err == nil {
		t.Fatal("CreateChangePasswordToken 空 token 应报错")
	}
	// 调用 Error() 方法覆盖 errString.Error()
	msg := err.Error()
	if msg == "" {
		t.Fatal("errString.Error() 返回空串")
	}
}
