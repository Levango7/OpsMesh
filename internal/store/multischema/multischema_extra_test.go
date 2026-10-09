// multischema_extra_test.go MultiSchemaStore 的构造/路由/空聚合/边界用例（TD-61 末批：
// 自父包 store_extra_test.go 尾段拆分，包声明改 multischema）。
package multischema

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/events"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// ============================================================================
// MultiSchemaStore 的 defaultStoreFactory / globalStore / 反查索引
// ============================================================================

// TestMultiSchemaStore_DefaultStoreFactory 验证 defaultStoreFactory 在无效 DSN 下返回错误。
func TestMultiSchemaStore_DefaultStoreFactory(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	m, err := NewMultiSchemaStore("invalid-dsn", "", "", namer)
	if err != nil {
		t.Fatalf("NewMultiSchemaStore 失败: %v", err)
	}
	// defaultStoreFactory 会尝试创建 SQLStore，无效 DSN 应返回错误
	if _, err := m.defaultStoreFactory("test_schema"); err == nil {
		t.Fatal("defaultStoreFactory 无效 DSN 应返回错误")
	}
}

// TestMultiSchemaStore_GlobalStore_LazyCreate 验证 globalStore 惰性创建 global schema。
func TestMultiSchemaStore_GlobalStore_LazyCreate(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	// 初始无 schema，globalStore 应惰性创建 "global"
	s, err := m.globalStore()
	if err != nil {
		t.Fatalf("globalStore 失败: %v", err)
	}
	if s == nil {
		t.Fatal("globalStore 应返回非 nil store")
	}
}

// TestMultiSchemaStore_ReverseLookup_Unmatched 验证反查索引未命中返回空串。
func TestMultiSchemaStore_ReverseLookup_Unmatched(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if got := m.lookupAgentTenant("no-exist"); got != "" {
		t.Fatalf("lookupAgentTenant 未命中应返回空串, got %q", got)
	}
	if got := m.lookupDeviceTenant("no-exist"); got != "" {
		t.Fatalf("lookupDeviceTenant 未命中应返回空串, got %q", got)
	}
	if got := m.lookupTaskTenant("no-exist"); got != "" {
		t.Fatalf("lookupTaskTenant 未命中应返回空串, got %q", got)
	}
}

// TestMultiSchemaStore_StoreFor_EmptyTenant 验证 storeFor 空租户返回错误。
func TestMultiSchemaStore_StoreFor_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if _, err := m.storeFor(""); err == nil {
		t.Fatal("storeFor 空租户应返回错误")
	}
}

// TestMultiSchemaStore_StoreFor_NamerError 验证 storeFor namer 错误透传。
func TestMultiSchemaStore_StoreFor_NamerError(t *testing.T) {
	// namer 始终返回错误
	namer := func(tenant string) (string, error) {
		return "", errors.New("namer error")
	}
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if _, err := m.storeFor("t1"); err == nil {
		t.Fatal("storeFor namer 错误应透传")
	}
}

// TestMultiSchemaStore_StoreFor_FactoryError 验证 storeFor factory 错误透传。
func TestMultiSchemaStore_StoreFor_FactoryError(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return nil, errors.New("factory error")
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if _, err := m.storeFor("t1"); err == nil {
		t.Fatal("storeFor factory 错误应透传")
	}
}

// TestMultiSchemaStore_AllStores_Empty 验证 allStores 空时返回空 slice。
func TestMultiSchemaStore_AllStores_Empty(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if got := m.allStores(); len(got) != 0 {
		t.Fatalf("allStores 空 = %d, want 0", len(got))
	}
}

// TestMultiSchemaStore_NewMultiSchemaStore_NilNamer 验证 nil namer 返回错误。
func TestMultiSchemaStore_NewMultiSchemaStore_NilNamer(t *testing.T) {
	if _, err := NewMultiSchemaStore("dsn", "", "", nil); err == nil {
		t.Fatal("nil namer 应返回错误")
	}
}

// TestMultiSchemaStore_WithBus_WithSecret 验证 WithBus/WithSecret 链式调用。
func TestMultiSchemaStore_WithBus_WithSecret(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	bus := events.NoopBus{}
	if m.WithBus(bus) != m {
		t.Fatal("WithBus 应返回 m")
	}
	if m.WithSecret("new-secret") != m {
		t.Fatal("WithSecret 应返回 m")
	}
	// 空 secret 不覆盖
	m2 := newMultiSchemaWithFactory(namer, factory)
	origSecret := m2.secret
	m2.WithSecret("")
	if m2.secret != origSecret {
		t.Fatal("WithSecret 空串不应覆盖")
	}
}

// TestMultiSchemaStore_WithDemo_Propagation 验证 WithDemo 传播到已创建 schema。
func TestMultiSchemaStore_WithDemo_Propagation(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	// 先创建一个 schema
	m.storeFor("t1")
	// WithDemo 传播
	if m.WithDemo(true) != m {
		t.Fatal("WithDemo 应返回 m")
	}
}

// TestMultiSchemaStore_Heartbeat_UnknownAgent 验证未知 agent 心跳返回 false。
func TestMultiSchemaStore_Heartbeat_UnknownAgent(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if m.Heartbeat("no-exist", "online", 1) {
		t.Fatal("Heartbeat 未知 agent 应返回 false")
	}
}

// TestMultiSchemaStore_Device_Unknown 验证未知设备返回 nil。
func TestMultiSchemaStore_Device_Unknown(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if m.Device("no-exist") != nil {
		t.Fatal("Device 未知应返回 nil")
	}
	if m.Agent("no-exist") != nil {
		t.Fatal("Agent 未知应返回 nil")
	}
	if m.AgentSecret("no-exist") != "" {
		t.Fatal("AgentSecret 未知应返回空串")
	}
}

// TestMultiSchemaStore_GetTasks_UnknownAgent 验证未知 agent 返回 nil。
func TestMultiSchemaStore_GetTasks_UnknownAgent(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if got := m.GetTasks("no-exist"); got != nil {
		t.Fatalf("GetTasks 未知 agent 应返回 nil, got %+v", got)
	}
	if got := m.TasksByParent("no-exist"); got != nil {
		t.Fatalf("TasksByParent 未知应返回 nil, got %+v", got)
	}
	if got := m.ClaimTask("no-exist"); got != nil {
		t.Fatalf("ClaimTask 未知 agent 应返回 nil, got %+v", got)
	}
	if got := m.TaskByID("no-exist"); got != nil {
		t.Fatalf("TaskByID 未知应返回 nil, got %+v", got)
	}
	if got := m.TaskResult("no-exist"); got != nil {
		t.Fatalf("TaskResult 未知应返回 nil, got %+v", got)
	}
	if got := m.CancelledTaskIDs("no-exist"); got != nil {
		t.Fatalf("CancelledTaskIDs 未知应返回 nil, got %+v", got)
	}
	if got := m.Results("no-exist"); got != nil {
		t.Fatalf("Results 未知应返回 nil, got %+v", got)
	}
}

// TestMultiSchemaStore_EmptyAggregations 验证空 store 聚合方法返回零值。
func TestMultiSchemaStore_EmptyAggregations(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if n := m.PendingDepth(); n != 0 {
		t.Fatalf("PendingDepth 空 = %d, want 0", n)
	}
	if n := m.ReclaimStaleTasks(time.Minute); n != 0 {
		t.Fatalf("ReclaimStaleTasks 空 = %d, want 0", n)
	}
	if n := m.FireDueSchedules(time.Now()); n != 0 {
		t.Fatalf("FireDueSchedules 空 = %d, want 0", n)
	}
	if n := m.RetireStaleDevices(time.Minute); n != 0 {
		t.Fatalf("RetireStaleDevices 空 = %d, want 0", n)
	}
	if n := m.CleanupTokens(10); n != 0 {
		t.Fatalf("CleanupTokens 空 = %d, want 0", n)
	}
	if m.IsLeader() {
		t.Fatal("IsLeader 空应返回 false")
	}
	if m.RenewLeadership(time.Minute) {
		t.Fatal("RenewLeadership 空应返回 false")
	}
}

// TestMultiSchemaStore_Alert_Empty 验证空 store Alert 返回 nil。
func TestMultiSchemaStore_Alert_Empty(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if m.Alert("no-exist") != nil {
		t.Fatal("Alert 空应返回 nil")
	}
}

// TestMultiSchemaStore_Audits_Empty 验证空 store Audits 返回 nil。
func TestMultiSchemaStore_Audits_Empty(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if got := m.Audits(); len(got) != 0 {
		t.Fatalf("Audits 空 = %d, want 0", len(got))
	}
	if got := m.QueryAudits("", "", time.Time{}, time.Time{}, 0); len(got) != 0 {
		t.Fatalf("QueryAudits 空 = %d, want 0", len(got))
	}
}

// TestMultiSchemaStore_ConsumeToken_Empty 验证空 store ConsumeToken 返回 false。
func TestMultiSchemaStore_ConsumeToken_Empty(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if _, _, ok := m.ConsumeToken("tok"); ok {
		t.Fatal("ConsumeToken 空应返回 false")
	}
}

// TestMultiSchemaStore_DeleteAlertRule_Empty 验证空 store DeleteAlertRule 返回 false。
func TestMultiSchemaStore_DeleteAlertRule_Empty(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if m.DeleteAlertRule("no-exist") {
		t.Fatal("DeleteAlertRule 空应返回 false")
	}
	if m.GetAlertRule("no-exist") != nil {
		t.Fatal("GetAlertRule 空应返回 nil")
	}
	if m.UpdateAlertRule(&AlertRule{ID: "no-exist"}) {
		t.Fatal("UpdateAlertRule 空应返回 false")
	}
	if m.UpdateAlertRule(nil) {
		t.Fatal("UpdateAlertRule nil 应返回 false")
	}
}

// TestMultiSchemaStore_CreateAlertRule_Nil 验证 CreateAlertRule nil 返回 nil。
func TestMultiSchemaStore_CreateAlertRule_Nil(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if m.CreateAlertRule(nil) != nil {
		t.Fatal("CreateAlertRule nil 应返回 nil")
	}
}

// TestMultiSchemaStore_AddAlert_Nil 验证 AddAlert nil 不 panic。
func TestMultiSchemaStore_AddAlert_Nil(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	m.AddAlert(nil) // 不应 panic
}

// TestMultiSchemaStore_Audit_Nil 验证 Audit nil 不 panic。
func TestMultiSchemaStore_Audit_Nil(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	m.Audit(nil) // 不应 panic
}

// TestMultiSchemaStore_UpsertDevice_NilOrEmpty 验证 UpsertDevice nil 或空 DeviceID 不 panic。
func TestMultiSchemaStore_UpsertDevice_NilOrEmpty(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	m.UpsertDevice(nil)
	m.UpsertDevice(&proto.DeviceInfo{DeviceID: ""})
}

// TestMultiSchemaStore_StoreDeviceMetrics_NilOrEmpty 验证 StoreDeviceMetrics 边界。
func TestMultiSchemaStore_StoreDeviceMetrics_NilOrEmpty(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	m.StoreDeviceMetrics("", &proto.DeviceMetrics{DeviceID: "d1"})
	m.StoreDeviceMetrics("d1", nil)
	// 未知设备（无反查索引）应丢弃
	m.StoreDeviceMetrics("unknown", &proto.DeviceMetrics{DeviceID: "unknown"})
	if m.DeviceMetrics("unknown") != nil {
		t.Fatal("DeviceMetrics 未知设备应返回 nil")
	}
	if m.DeviceMetricsHistory("unknown", time.Time{}) != nil {
		t.Fatal("DeviceMetricsHistory 未知设备应返回 nil")
	}
}

// TestMultiSchemaStore_Quota_EmptyTenant 验证 Quota 空租户路径。
func TestMultiSchemaStore_Quota_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if cfg, err := m.GetQuota(""); cfg != nil || err != nil {
		t.Fatalf("GetQuota 空租户应返回 (nil,nil), got (%+v,%v)", cfg, err)
	}
	if err := m.SetQuota("", &QuotaConfig{}); err == nil {
		t.Fatal("SetQuota 空租户应返回错误")
	}
}

// TestMultiSchemaStore_Snapshot_EmptyTenant 验证 Snapshot 空租户返回 nil。
func TestMultiSchemaStore_Snapshot_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if got := m.Snapshot(""); got != nil {
		t.Fatalf("Snapshot 空租户应返回 nil, got %+v", got)
	}
}

// TestMultiSchemaStore_Alerts_EmptyTenant 验证 Alerts 空租户返回 nil。
func TestMultiSchemaStore_Alerts_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if got := m.Alerts(""); got != nil {
		t.Fatalf("Alerts 空租户应返回 nil, got %+v", got)
	}
	if got := m.Agents(""); got != nil {
		t.Fatalf("Agents 空租户应返回 nil, got %+v", got)
	}
	if got := m.AllTasks(""); got != nil {
		t.Fatalf("AllTasks 空租户应返回 nil, got %+v", got)
	}
	if got := m.ListAlertRules(""); got != nil {
		t.Fatalf("ListAlertRules 空租户应返回 nil, got %+v", got)
	}
}

// TestMultiSchemaStore_RetireDevice_EmptyTenant 验证 RetireDevice 空租户返回 false。
func TestMultiSchemaStore_RetireDevice_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if m.RetireDevice("d1", "") {
		t.Fatal("RetireDevice 空租户应返回 false")
	}
	if m.AckAlert("a1", "", "u1") {
		t.Fatal("AckAlert 空租户应返回 false")
	}
	if m.SilenceAlert("a1", "", "u1", time.Time{}, "c") {
		t.Fatal("SilenceAlert 空租户应返回 false")
	}
	if m.CancelTask("t1", "") {
		t.Fatal("CancelTask 空租户应返回 false")
	}
	if m.ApproveTask("t1", "", "u1") {
		t.Fatal("ApproveTask 空租户应返回 false")
	}
	if m.RejectTask("t1", "", "u1") {
		t.Fatal("RejectTask 空租户应返回 false")
	}
}

// TestMultiSchemaStore_Provision_EmptyTenant 验证 Provision 空租户返回错误。
func TestMultiSchemaStore_Provision_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if _, _, err := m.Provision("d1", "host", ""); err == nil {
		t.Fatal("Provision 空租户应返回错误")
	}
	if _, err := m.IssueToken("d1", "", time.Minute); err == nil {
		t.Fatal("IssueToken 空租户应返回错误")
	}
}

// TestMultiSchemaStore_SubmitResult_UnknownTask 验证 SubmitResult 未知任务不 panic。
func TestMultiSchemaStore_SubmitResult_UnknownTask(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	m.SubmitResult(&proto.TaskResult{TaskID: "no-exist", AgentID: "no-exist"}) // 不应 panic
}

// TestMultiSchemaStore_CreateTask_EmptyTenant 验证 CreateTask 空租户不 panic。
func TestMultiSchemaStore_CreateTask_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	tk := &proto.Task{AgentID: "a1", TenantID: "", Command: "c1"}
	ret := m.CreateTask(tk)
	if ret == nil {
		t.Fatal("CreateTask 应返回非 nil")
	}
}

// TestMultiSchemaStore_Register_EmptyTenant 验证 Register 空租户不 panic。
func TestMultiSchemaStore_Register_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return newMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	a := m.Register(&proto.AgentInfo{AgentID: "a1", Segment: "s1", TenantID: ""})
	if a == nil {
		t.Fatal("Register 应返回非 nil")
	}
}

// TestSortAuditsDesc 验证 sortAuditsDesc 按时间倒序。
func TestSortAuditsDesc(t *testing.T) {
	now := time.Now()
	in := []*proto.AuditEvent{
		{Action: "a1", CreatedAt: now},
		{Action: "a2", CreatedAt: now.Add(time.Second)},
		{Action: "a3", CreatedAt: now.Add(2 * time.Second)},
	}
	sortAuditsDesc(in)
	if in[0].Action != "a3" || in[1].Action != "a2" || in[2].Action != "a1" {
		t.Fatalf("sortAuditsDesc 顺序错误: %+v", in)
	}
}

// TestSortAuditsDesc_Empty 验证 sortAuditsDesc 空切片不 panic。
func TestSortAuditsDesc_Empty(t *testing.T) {
	sortAuditsDesc(nil)
	sortAuditsDesc([]*proto.AuditEvent{})
}

// TestValidateIdent 验证 validateIdent 各种输入。
func TestValidateIdent(t *testing.T) {
	if err := validateIdent("opsmesh_t1"); err != nil {
		t.Fatalf("合法标识符应通过: %v", err)
	}
	if err := validateIdent(""); err != nil {
		t.Fatalf("空串应通过: %v", err)
	}
	if err := validateIdent("t1; DROP"); err == nil {
		t.Fatal("含 ; 空格应拒绝")
	}
	if err := validateIdent("t'1"); err == nil {
		t.Fatal("含 ' 应拒绝")
	}
	if err := validateIdent("t1--"); err == nil {
		t.Fatal("含 - 应拒绝")
	}
}

// TestDsnForSchema_NoSlash 验证 dsnForSchema 无 / 时原样返回。
func TestDsnForSchema_NoSlash(t *testing.T) {
	if got := dsnForSchema("no-slash-dsn", "schema1"); got != "no-slash-dsn" {
		t.Fatalf("dsnForSchema 无 / 应原样返回, got %q", got)
	}
}

// TestDsnForSchema_WithQuery 验证 dsnForSchema 含查询参数。
func TestDsnForSchema_WithQuery(t *testing.T) {
	got := dsnForSchema("user:pass@tcp(host:3306)/olddb?parseTime=true", "newschema")
	if !strings.Contains(got, "/newschema?") {
		t.Fatalf("dsnForSchema 应替换 db 名, got %q", got)
	}
}

// TestDsnForSchema_NoQuery 验证 dsnForSchema 无查询参数。
func TestDsnForSchema_NoQuery(t *testing.T) {
	got := dsnForSchema("user:pass@tcp(host:3306)/olddb", "newschema")
	if !strings.HasSuffix(got, "/newschema") {
		t.Fatalf("dsnForSchema 应替换 db 名, got %q", got)
	}
}
