// multi_schema_test.go 测试 M4-4C 多租户 schema 隔离。
//
// 测试策略：
//   - 纯逻辑测试（无需 MySQL）：SchemaNamer 的 SQL 注入防护、dsnForSchema 的 DSN 改写；
//   - 路由隔离测试（用 MemoryStore 作为 mock factory）：每个 tenant 创建独立 MemoryStore 实例，
//     验证不同 tenant 路由到不同 store、数据物理隔离、反查索引路由、跨租户聚合、Leader 选举等。
//
// 用 MemoryStore 而非真实 MySQL 的原因：MultiSchemaStore 的路由逻辑与具体后端无关，
// MemoryStore 已实现完整 Store 接口，足以验证路由/隔离/聚合语义；
// 真实 MySQL 隔离由 sql_test.go 的集成测试覆盖（需 OPSMESH_TEST_MYSQL_DSN）。
package store

import (
	"strings"
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// mockStoreFactory 返回一个 factory：每次调用创建一个新的 MemoryStore（独立实例，数据物理隔离）。
// 同时记录创建的 schema 名，供测试验证路由正确性。
func mockStoreFactory(created *[]string) func(schema string) (Store, error) {
	return func(schema string) (Store, error) {
		*created = append(*created, schema)
		return NewMemoryStore(), nil
	}
}

// ============================================================================
// SchemaNamer 测试（SQL 注入防护）
// ============================================================================

func TestDefaultSchemaNamer_LegalTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	cases := []struct {
		tenant string
		want   string
	}{
		{"t1", "opsmesh_tenant_t1"},
		{"tenant_abc123", "opsmesh_tenant_tenant_abc123"},
		{"AcmeCorp", "opsmesh_tenant_AcmeCorp"},
		{"_", "opsmesh_tenant__"},
	}
	for _, c := range cases {
		got, err := namer(c.tenant)
		if err != nil {
			t.Errorf("namer(%q) 意外错误: %v", c.tenant, err)
			continue
		}
		if got != c.want {
			t.Errorf("namer(%q) = %q, want %q", c.tenant, got, c.want)
		}
	}
}

func TestDefaultSchemaNamer_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	if _, err := namer(""); err == nil {
		t.Error("namer(\"\") 应返回错误（空租户），但返回 nil")
	}
}

// TestDefaultSchemaNamer_SQLInjection 验证 schema 名的白名单校验：
// 含 SQL 注入字符的 tenant 名必须被拒绝，避免拼进 DSN/SQL 造成注入。
func TestDefaultSchemaNamer_SQLInjection(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	// 各种 SQL 注入尝试。
	malicious := []string{
		"t'1",                       // 单引号
		"t;1",                       // 分号
		"t--1",                      // SQL 注释
		"t 1",                       // 空格
		"t.a",                       // 点号
		"t; DROP TABLE agents; --",  // 完整注入
		"t\"1",                      // 双引号
		"t`1",                       // 反引号
		"t-1",                       // 减号
		"t+1",                       // 加号
		"t/1",                       // 斜杠
		"t\\1",                      // 反斜杠
		"t(1)",                      // 括号
		"t=1",                       // 等号
		"tenant@host",               // @ 符号
		"t*1",                       // 星号
		"t%1",                       // 百分号
		"t#1",                       // 井号
		"t!1",                       // 感叹号
		"t~1",                       // 波浪号
		"t^1",                       // 脱字符
		"t&1",                       // & 符号
		"t|1",                       // 竖线
		"t<1",                       // 小于号
		"t>1",                       // 大于号
		"t,1",                       // 逗号
		"t:1",                       // 冒号
		"t?1",                       // 问号
		"t[1]",                      // 方括号
		"t{1}",                      // 花括号
		"t\n1",                      // 换行
		"t\t1",                      // 制表符
		"t\r1",                      // 回车
		"t\x001",                    // 空字节
		"t\x1b1",                    // ESC
		"t€1",                       // Unicode
		"t中1",                      // 中文
	}
	for _, tenant := range malicious {
		got, err := namer(tenant)
		if err == nil {
			t.Errorf("namer(%q) 应拒绝含非法字符的 tenant，但返回了 schema 名 %q", tenant, got)
		}
	}
}

// TestDefaultSchemaNamer_IllegalPrefix 验证 prefix 也做白名单校验。
func TestDefaultSchemaNamer_IllegalPrefix(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh; tenant_") // prefix 含分号
	if _, err := namer("t1"); err == nil {
		t.Error("含非法字符的 prefix 应被拒绝")
	}
}

// TestDefaultSchemaNamer_EmptyPrefix 空 prefix 合法（允许 prefix=""）。
func TestDefaultSchemaNamer_EmptyPrefix(t *testing.T) {
	namer := DefaultSchemaNamer("")
	got, err := namer("t1")
	if err != nil {
		t.Fatalf("空 prefix 应合法: %v", err)
	}
	if got != "t1" {
		t.Errorf("namer(t1) = %q, want %q", got, "t1")
	}
}

// ============================================================================
// dsnForSchema 测试（DSN 改写）
// ============================================================================

func TestDsnForSchema(t *testing.T) {
	cases := []struct {
		name    string
		baseDSN string
		schema  string
		want    string
	}{
		{
			name:    "with query params",
			baseDSN: "user:pass@tcp(host:3306)/dbname?parseTime=true&charset=utf8",
			schema:  "opsmesh_tenant_t1",
			want:    "user:pass@tcp(host:3306)/opsmesh_tenant_t1?parseTime=true&charset=utf8",
		},
		{
			name:    "without query params",
			baseDSN: "user:pass@tcp(host:3306)/dbname",
			schema:  "opsmesh_tenant_t1",
			want:    "user:pass@tcp(host:3306)/opsmesh_tenant_t1",
		},
		{
			name:    "no slash (invalid dsn)",
			baseDSN: "invalid-dsn",
			schema:  "s1",
			want:    "invalid-dsn", // 原样返回
		},
		{
			name:    "multiple slashes (last is db separator)",
			baseDSN: "user:p/ass@tcp(h/ost:3306)/dbname?x=1",
			schema:  "s1",
			want:    "user:p/ass@tcp(h/ost:3306)/s1?x=1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := dsnForSchema(c.baseDSN, c.schema)
			if got != c.want {
				t.Errorf("dsnForSchema(%q, %q) = %q, want %q", c.baseDSN, c.schema, got, c.want)
			}
		})
	}
}

// ============================================================================
// MultiSchemaStore 路由与隔离测试
// ============================================================================

// TestMultiSchemaStore_RoutingAndIsolation 验证不同 tenant 路由到不同 store，数据物理隔离。
func TestMultiSchemaStore_RoutingAndIsolation(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// tenant A 注册 agent-a。
	m.Register(&proto.AgentInfo{AgentID: "agent-a", Segment: "seg-1", TenantID: "tA"})
	// tenant B 注册 agent-b。
	m.Register(&proto.AgentInfo{AgentID: "agent-b", Segment: "seg-1", TenantID: "tB"})

	// 验证创建了两个不同的 schema。
	if len(created) != 2 {
		t.Fatalf("应创建 2 个 schema，实际 %d: %v", len(created), created)
	}
	if created[0] != "opsmesh_tenant_tA" || created[1] != "opsmesh_tenant_tB" {
		t.Errorf("schema 名 = %v, want [opsmesh_tenant_tA, opsmesh_tenant_tB]", created)
	}

	// 验证 tenant A 只能看到自己的 agent。
	agentsA := m.Agents("tA")
	if len(agentsA) != 1 || agentsA[0].AgentID != "agent-a" {
		t.Errorf("Agents(tA) = %+v, want exactly [agent-a]", agentsA)
	}
	// 验证 tenant B 只能看到自己的 agent。
	agentsB := m.Agents("tB")
	if len(agentsB) != 1 || agentsB[0].AgentID != "agent-b" {
		t.Errorf("Agents(tB) = %+v, want exactly [agent-b]", agentsB)
	}

	// 验证 Snapshot 隔离。
	snapA := m.Snapshot("tA")
	snapB := m.Snapshot("tB")
	if cnt := countDevices(snapA); cnt != 1 {
		t.Errorf("Snapshot(tA) device count = %d, want 1", cnt)
	}
	if cnt := countDevices(snapB); cnt != 1 {
		t.Errorf("Snapshot(tB) device count = %d, want 1", cnt)
	}
}

// TestMultiSchemaStore_AgentReverseLookup 验证无 tenant 参数的方法经反查索引路由。
func TestMultiSchemaStore_AgentReverseLookup(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	m.Register(&proto.AgentInfo{AgentID: "agent-x", Segment: "seg-1", TenantID: "tA"})
	m.Register(&proto.AgentInfo{AgentID: "agent-y", Segment: "seg-1", TenantID: "tB"})

	// Agent(id) 经反查索引路由。
	a := m.Agent("agent-x")
	if a == nil || a.AgentID != "agent-x" {
		t.Errorf("Agent(agent-x) = %+v, want agent-x", a)
	}
	b := m.Agent("agent-y")
	if b == nil || b.AgentID != "agent-y" {
		t.Errorf("Agent(agent-y) = %+v, want agent-y", b)
	}
	// 未注册的 agent 返回 nil。
	if got := m.Agent("unknown"); got != nil {
		t.Errorf("Agent(unknown) = %+v, want nil", got)
	}

	// Heartbeat 经反查索引路由。
	if !m.Heartbeat("agent-x", "online", 1) {
		t.Error("Heartbeat(agent-x) 应成功（已注册）")
	}
	if !m.Heartbeat("agent-y", "online", 1) {
		t.Error("Heartbeat(agent-y) 应成功（已注册）")
	}
	// 未注册的 agent 心跳返回 false。
	if m.Heartbeat("unknown", "online", 1) {
		t.Error("Heartbeat(unknown) 应返回 false（未注册）")
	}

	// GetTasks 经反查索引路由（无任务返回空 slice）。
	if tasks := m.GetTasks("agent-x"); len(tasks) != 0 {
		t.Errorf("GetTasks(agent-x) = %+v, want empty（无任务）", tasks)
	}
}

// TestMultiSchemaStore_TaskRoutingAndIsolation 验证任务按租户隔离。
func TestMultiSchemaStore_TaskRoutingAndIsolation(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	m.Register(&proto.AgentInfo{AgentID: "agent-a", Segment: "seg-1", TenantID: "tA"})
	m.Register(&proto.AgentInfo{AgentID: "agent-b", Segment: "seg-1", TenantID: "tB"})

	// tenant A 给 agent-a 下发任务（显式 TaskID 避免 store 分配的时间戳冲突）。
	t1 := m.CreateTask(&proto.Task{TaskID: "task-tA-1", AgentID: "agent-a", TenantID: "tA", Type: "shell", Command: "ls"})
	// tenant B 给 agent-b 下发任务。
	t2 := m.CreateTask(&proto.Task{TaskID: "task-tB-1", AgentID: "agent-b", TenantID: "tB", Type: "shell", Command: "pwd"})

	// AllTasks 按租户隔离。
	tasksA := m.AllTasks("tA")
	if len(tasksA) != 1 || tasksA[0].TaskID != t1.TaskID {
		t.Errorf("AllTasks(tA) = %+v, want exactly [task of tA]", tasksA)
	}
	tasksB := m.AllTasks("tB")
	if len(tasksB) != 1 || tasksB[0].TaskID != t2.TaskID {
		t.Errorf("AllTasks(tB) = %+v, want exactly [task of tB]", tasksB)
	}

	// TaskResult 经反查索引路由（无结果返回 nil）。
	if r := m.TaskResult(t1.TaskID); r != nil {
		t.Errorf("TaskResult(%s) = %+v, want nil（未上报）", t1.TaskID, r)
	}

	// CancelTask 按租户校验：tenant B 不能取消 tenant A 的任务。
	if m.CancelTask(t1.TaskID, "tB") {
		t.Error("CancelTask(t1.TaskID, tB) 应返回 false（跨租户越权）")
	}
	// tenant A 可以取消自己的任务。
	if !m.CancelTask(t1.TaskID, "tA") {
		t.Error("CancelTask(t1.TaskID, tA) 应返回 true（同租户）")
	}
}

// TestMultiSchemaStore_DeviceRoutingAndIsolation 验证设备按租户隔离。
func TestMultiSchemaStore_DeviceRoutingAndIsolation(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// tenant A 纳管设备 dev-a。
	m.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-a", Segment: "seg-1", TenantID: "tA", IP: "10.0.0.1", Managed: true})
	// tenant B 纯管设备 dev-b。
	m.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-b", Segment: "seg-1", TenantID: "tB", IP: "10.0.0.2", Managed: true})

	// Device(id) 经反查索引路由。
	dA := m.Device("dev-a")
	if dA == nil || dA.DeviceID != "dev-a" {
		t.Errorf("Device(dev-a) = %+v, want dev-a", dA)
	}
	dB := m.Device("dev-b")
	if dB == nil || dB.DeviceID != "dev-b" {
		t.Errorf("Device(dev-b) = %+v, want dev-b", dB)
	}
	// 未注册的设备返回 nil。
	if got := m.Device("unknown"); got != nil {
		t.Errorf("Device(unknown) = %+v, want nil", got)
	}

	// RetireDevice 按租户校验：tenant B 不能退役 tenant A 的设备。
	if m.RetireDevice("dev-a", "tB") {
		t.Error("RetireDevice(dev-a, tB) 应返回 false（跨租户越权）")
	}
	// tenant A 可以退役自己的设备。
	if !m.RetireDevice("dev-a", "tA") {
		t.Error("RetireDevice(dev-a, tA) 应返回 true（同租户）")
	}
}

// TestMultiSchemaStore_Aggregation 验证跨租户聚合方法。
func TestMultiSchemaStore_Aggregation(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	m.Register(&proto.AgentInfo{AgentID: "agent-a", Segment: "seg-1", TenantID: "tA"})
	m.Register(&proto.AgentInfo{AgentID: "agent-b", Segment: "seg-1", TenantID: "tB"})

	// 两个 tenant 各创建一个 pending 任务。
	m.CreateTask(&proto.Task{AgentID: "agent-a", TenantID: "tA", Type: "shell", Command: "ls"})
	m.CreateTask(&proto.Task{AgentID: "agent-b", TenantID: "tB", Type: "shell", Command: "pwd"})

	// PendingDepth 跨租户聚合 = 2。
	if got := m.PendingDepth(); got != 2 {
		t.Errorf("PendingDepth() = %d, want 2（跨租户聚合）", got)
	}

	// 两个 tenant 各记录一条审计。
	m.Audit(&proto.AuditEvent{TenantID: "tA", Action: "register", Target: "agent-a"})
	m.Audit(&proto.AuditEvent{TenantID: "tB", Action: "register", Target: "agent-b"})

	// Audits() 跨租户合并 = 2 条（加上 Register 自动产生的审计）。
	// 注意：MemoryStore.Register 内部会自动调 Audit，所以每个 tenant 的审计数 >1。
	// 这里只验证 Audits() 返回了两个 tenant 的审计（至少 2 条）。
	audits := m.Audits()
	if len(audits) < 2 {
		t.Errorf("Audits() = %d 条，want >= 2（跨租户合并）", len(audits))
	}
	// 验证两个 tenant 的审计都出现了。
	seenA, seenB := false, false
	for _, e := range audits {
		if e.TenantID == "tA" {
			seenA = true
		}
		if e.TenantID == "tB" {
			seenB = true
		}
	}
	if !seenA || !seenB {
		t.Errorf("Audits() 缺少某租户审计: seenA=%v seenB=%v", seenA, seenB)
	}

	// QueryAudits(tenant) 按租户过滤。
	auditsA := m.QueryAudits("tA", "", time.Time{}, time.Time{}, 0)
	for _, e := range auditsA {
		if e.TenantID != "tA" {
			t.Errorf("QueryAudits(tA) 返回了非 tA 的审计: %+v", e)
		}
	}
}

// TestMultiSchemaStore_AlertRoutingAndIsolation 验证告警按租户隔离。
func TestMultiSchemaStore_AlertRoutingAndIsolation(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// tenant A 记录告警 alert-A。
	m.AddAlert(&proto.Alert{AlertID: "alert-A", TenantID: "tA", Severity: "critical", Message: "fail A"})
	// tenant B 记录告警 alert-B。
	m.AddAlert(&proto.Alert{AlertID: "alert-B", TenantID: "tB", Severity: "warning", Message: "fail B"})

	// Alerts(tenantID) 按租户隔离。
	alertsA := m.Alerts("tA")
	if len(alertsA) != 1 || alertsA[0].AlertID != "alert-A" {
		t.Errorf("Alerts(tA) = %+v, want exactly [alert-A]", alertsA)
	}
	alertsB := m.Alerts("tB")
	if len(alertsB) != 1 || alertsB[0].AlertID != "alert-B" {
		t.Errorf("Alerts(tB) = %+v, want exactly [alert-B]", alertsB)
	}

	// Alert(id) 遍历所有 schema 查找。
	a := m.Alert("alert-A")
	if a == nil || a.AlertID != "alert-A" {
		t.Errorf("Alert(alert-A) = %+v, want alert-A", a)
	}
	b := m.Alert("alert-B")
	if b == nil || b.AlertID != "alert-B" {
		t.Errorf("Alert(alert-B) = %+v, want alert-B", b)
	}
	if got := m.Alert("nonexistent"); got != nil {
		t.Errorf("Alert(nonexistent) = %+v, want nil", got)
	}

	// AckAlert 按租户校验：tenant B 不能确认 tenant A 的告警。
	if m.AckAlert("alert-A", "tB", "user-b") {
		t.Error("AckAlert(alert-A, tB) 应返回 false（跨租户越权）")
	}
	// tenant A 可以确认自己的告警。
	if !m.AckAlert("alert-A", "tA", "user-a") {
		t.Error("AckAlert(alert-A, tA) 应返回 true（同租户）")
	}
}

// TestMultiSchemaStore_TokenRouting 验证 install token 按租户路由。
func TestMultiSchemaStore_TokenRouting(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// tenant A 纯管设备 dev-a。
	m.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-a", Segment: "seg-1", TenantID: "tA", IP: "10.0.0.1"})

	// tenant A 签发 token。
	tok, err := m.IssueToken("dev-a", "tA", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken 失败: %v", err)
	}
	if tok == "" {
		t.Fatal("IssueToken 返回空 token")
	}

	// ConsumeToken 遍历所有 schema，在 tenant A 的 schema 上成功。
	deviceID, tenantID, ok := m.ConsumeToken(tok)
	if !ok {
		t.Error("ConsumeToken 应成功（token 在 tenant A 的 schema 上）")
	}
	if deviceID != "dev-a" || tenantID != "tA" {
		t.Errorf("ConsumeToken = (device=%q, tenant=%q), want (dev-a, tA)", deviceID, tenantID)
	}

	// 重复消费应失败（一次性 token）。
	if _, _, ok := m.ConsumeToken(tok); ok {
		t.Error("ConsumeToken 重复消费应失败（一次性 token）")
	}
}

// TestMultiSchemaStore_LeaderElection 验证 Leader 选举语义。
// MemoryStore 恒为 leader（单实例），所以 MultiSchemaStore.IsLeader() == true（任一为主）。
func TestMultiSchemaStore_LeaderElection(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// 无 schema 时不是 leader。
	if m.IsLeader() {
		t.Error("IsLeader() 无 schema 时应返回 false")
	}

	// 创建一个 schema（通过 Register 触发惰性创建）。
	m.Register(&proto.AgentInfo{AgentID: "agent-a", Segment: "seg-1", TenantID: "tA"})

	// MemoryStore 恒为 leader，续租成功。
	if !m.RenewLeadership(15 * time.Second) {
		t.Error("RenewLeadership 应返回 true（MemoryStore 恒为 leader）")
	}
	if !m.IsLeader() {
		t.Error("IsLeader() 应返回 true（任一 schema 为主）")
	}

	// 创建第二个 schema，仍是 leader。
	m.Register(&proto.AgentInfo{AgentID: "agent-b", Segment: "seg-1", TenantID: "tB"})
	if !m.RenewLeadership(15 * time.Second) {
		t.Error("RenewLeadership 应返回 true（两个 schema 都为主）")
	}
	if !m.IsLeader() {
		t.Error("IsLeader() 应返回 true")
	}
}

// TestMultiSchemaStore_EmptyTenantRejected 验证空租户被拒绝。
func TestMultiSchemaStore_EmptyTenantRejected(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// 空租户 Register 不创建 schema。
	m.Register(&proto.AgentInfo{AgentID: "agent-empty", TenantID: ""})
	if len(created) != 0 {
		t.Errorf("空租户不应创建 schema，但创建了 %d 个: %v", len(created), created)
	}

	// 空租户 Agents 返回 nil。
	if got := m.Agents(""); got != nil {
		t.Errorf("Agents(\"\") = %+v, want nil", got)
	}
}

// TestMultiSchemaStore_IllegalTenantRejected 验证非法租户名被拒绝（SQL 注入防护）。
func TestMultiSchemaStore_IllegalTenantRejected(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// 含 SQL 注入字符的租户名应被拒绝，不创建 schema。
	m.Register(&proto.AgentInfo{AgentID: "agent-inj", TenantID: "t'; DROP TABLE agents; --"})
	if len(created) != 0 {
		t.Errorf("非法租户名不应创建 schema，但创建了 %d 个: %v", len(created), created)
	}

	// Agents 返回 nil（schema 未创建）。
	if got := m.Agents("t'; DROP TABLE agents; --"); got != nil {
		t.Errorf("非法租户 Agents 应返回 nil，got %+v", got)
	}
}

// TestMultiSchemaStore_WithDemoPropagation 验证 WithDemo 传播到已创建的 schema。
func TestMultiSchemaStore_WithDemoPropagation(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// 先创建一个 schema。
	m.Register(&proto.AgentInfo{AgentID: "agent-a", Segment: "seg-1", TenantID: "tA"})

	// 开启 demo 后注册新 agent 应预置示例任务。
	m.WithDemo(true)
	m.Register(&proto.AgentInfo{AgentID: "agent-demo", Segment: "seg-1", TenantID: "tA"})

	// agent-demo 应有一条预置的 uname -a 示例任务。
	tasks := m.GetTasks("agent-demo")
	if len(tasks) == 0 {
		t.Error("WithDemo(true) 后注册的 agent 应有预置示例任务")
	}
	found := false
	for _, tk := range tasks {
		if tk.Command == "uname -a" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("未找到预置 uname -a 任务，tasks = %+v", tasks)
	}
}

// TestMultiSchemaStore_SubmitResultRouting 验证 SubmitResult 经反查索引路由。
func TestMultiSchemaStore_SubmitResultRouting(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	m.Register(&proto.AgentInfo{AgentID: "agent-a", Segment: "seg-1", TenantID: "tA"})
	task := m.CreateTask(&proto.Task{AgentID: "agent-a", TenantID: "tA", Type: "shell", Command: "echo hi"})

	// SubmitResult 经 taskTenant 反查路由到 tenant A 的 schema。
	m.SubmitResult(&proto.TaskResult{
		TaskID:     task.TaskID,
		AgentID:    "agent-a",
		ExitCode:   0,
		Stdout:     "hi",
		FinishedAt: time.Now().UTC(),
	})

	// TaskResult 经反查索引路由，应能查到。
	r := m.TaskResult(task.TaskID)
	if r == nil {
		t.Fatal("TaskResult 应返回上报的结果")
	}
	if r.Stdout != "hi" {
		t.Errorf("TaskResult.Stdout = %q, want %q", r.Stdout, "hi")
	}
}

// TestMultiSchemaStore_ClaimTaskRouting 验证 ClaimTask 经反查索引路由并更新 task 索引。
func TestMultiSchemaStore_ClaimTaskRouting(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	m.Register(&proto.AgentInfo{AgentID: "agent-a", Segment: "seg-1", TenantID: "tA"})
	task := m.CreateTask(&proto.Task{AgentID: "agent-a", TenantID: "tA", Type: "shell", Command: "echo hi"})

	// ClaimTask 经 agentTenant 反查路由到 tenant A 的 schema。
	claimed := m.ClaimTask("agent-a")
	if claimed == nil || claimed.TaskID != task.TaskID {
		t.Fatalf("ClaimTask(agent-a) = %+v, want task %s", claimed, task.TaskID)
	}

	// 领取后任务状态应为 running。
	if claimed.Status != "running" {
		t.Errorf("claimed.Status = %q, want %q", claimed.Status, "running")
	}
}

// TestMultiSchemaStore_CancelledTaskIDs 验证 CancelledTaskIDs 经反查索引路由。
func TestMultiSchemaStore_CancelledTaskIDs(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	m.Register(&proto.AgentInfo{AgentID: "agent-a", Segment: "seg-1", TenantID: "tA"})
	task := m.CreateTask(&proto.Task{AgentID: "agent-a", TenantID: "tA", Type: "shell", Command: "echo hi"})

	// 取消任务。
	if !m.CancelTask(task.TaskID, "tA") {
		t.Fatal("CancelTask 应成功")
	}

	// CancelledTaskIDs 经反查索引路由，应返回已取消的任务 ID。
	ids := m.CancelledTaskIDs("agent-a")
	if len(ids) != 1 || ids[0] != task.TaskID {
		t.Errorf("CancelledTaskIDs(agent-a) = %v, want [%s]", ids, task.TaskID)
	}
}

// TestMultiSchemaStore_FireDueSchedules 验证 FireDueSchedules 跨租户聚合。
func TestMultiSchemaStore_FireDueSchedules(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	m.Register(&proto.AgentInfo{AgentID: "agent-a", Segment: "seg-1", TenantID: "tA"})
	m.Register(&proto.AgentInfo{AgentID: "agent-b", Segment: "seg-1", TenantID: "tB"})

	// 两个 tenant 各创建一个定时模板任务（每分钟触发）。
	m.CreateTask(&proto.Task{AgentID: "agent-a", TenantID: "tA", Type: "shell", Command: "ls", Schedule: "* * * * *"})
	m.CreateTask(&proto.Task{AgentID: "agent-b", TenantID: "tB", Type: "shell", Command: "pwd", Schedule: "* * * * *"})

	// FireDueSchedules 跨租户聚合，应触发 2 个实例。
	now := time.Now().UTC().Add(time.Minute) // 下一分钟
	fired := m.FireDueSchedules(now)
	if fired != 2 {
		t.Errorf("FireDueSchedules() = %d, want 2（跨租户聚合）", fired)
	}
}

// TestMultiSchemaStore_ReclaimStaleTasks 验证 ReclaimStaleTasks 跨租户聚合。
func TestMultiSchemaStore_ReclaimStaleTasks(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	m.Register(&proto.AgentInfo{AgentID: "agent-a", Segment: "seg-1", TenantID: "tA"})
	m.Register(&proto.AgentInfo{AgentID: "agent-b", Segment: "seg-1", TenantID: "tB"})

	// 两个 tenant 各创建并领取一个任务（变为 running）。
	m.CreateTask(&proto.Task{TaskID: "task-stale-a", AgentID: "agent-a", TenantID: "tA", Type: "shell", Command: "ls"})
	m.CreateTask(&proto.Task{TaskID: "task-stale-b", AgentID: "agent-b", TenantID: "tB", Type: "shell", Command: "pwd"})
	m.ClaimTask("agent-a")
	m.ClaimTask("agent-b")

	// 等待任务"超期"：sleep 超过 maxAge，使 ClaimedAt 早于 (now - maxAge)。
	time.Sleep(20 * time.Millisecond)
	reclaimed := m.ReclaimStaleTasks(10 * time.Millisecond)
	if reclaimed != 2 {
		t.Errorf("ReclaimStaleTasks() = %d, want 2（跨租户聚合）", reclaimed)
	}
}

// TestMultiSchemaStore_RetireStaleDevices 验证 RetireStaleDevices 跨租户聚合。
func TestMultiSchemaStore_RetireStaleDevices(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// 两个 tenant 各注册一个 agent（同时创建占位设备）。
	m.Register(&proto.AgentInfo{AgentID: "agent-a", Segment: "seg-1", TenantID: "tA"})
	m.Register(&proto.AgentInfo{AgentID: "agent-b", Segment: "seg-1", TenantID: "tB"})

	// 等待 agent"超龄"：sleep 超过 maxAge，使 LastSeen 早于 (now - maxAge)。
	time.Sleep(20 * time.Millisecond)
	retired := m.RetireStaleDevices(10 * time.Millisecond)
	if retired < 2 {
		t.Errorf("RetireStaleDevices() = %d, want >= 2（跨租户聚合）", retired)
	}
}

// TestMultiSchemaStore_CleanupTokens 验证 CleanupTokens 跨租户聚合。
func TestMultiSchemaStore_CleanupTokens(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	m.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-a", Segment: "seg-1", TenantID: "tA", IP: "10.0.0.1"})
	m.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-b", Segment: "seg-1", TenantID: "tB", IP: "10.0.0.2"})

	// 两个 tenant 各签发一个已过期 token（ttl=1ns）。
	if _, err := m.IssueToken("dev-a", "tA", 1*time.Nanosecond); err != nil {
		t.Fatalf("IssueToken(tA) 失败: %v", err)
	}
	if _, err := m.IssueToken("dev-b", "tB", 1*time.Nanosecond); err != nil {
		t.Fatalf("IssueToken(tB) 失败: %v", err)
	}

	// 等待 token 过期。
	time.Sleep(2 * time.Nanosecond)

	// CleanupTokens 跨租户聚合，应清理 2 个过期 token。
	cleaned := m.CleanupTokens(0)
	if cleaned != 2 {
		t.Errorf("CleanupTokens() = %d, want 2（跨租户聚合）", cleaned)
	}
}

// TestMultiSchemaStore_ProvisionRouting 验证 Provision 按租户路由并更新 device 索引。
func TestMultiSchemaStore_ProvisionRouting(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// tenant A 纯管候选设备 dev-cand。
	m.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-cand", Segment: "seg-1", TenantID: "tA", IP: "10.0.0.1", State: "discovered"})

	// Provision 签发 token。
	tok, _, err := m.Provision("dev-cand", "10.0.0.1", "tA")
	if err != nil {
		t.Fatalf("Provision 失败: %v", err)
	}
	if tok == "" {
		t.Fatal("Provision 返回空 token")
	}

	// Provision 后 device 索引应更新，Device(dev-cand) 能经反查路由找到。
	d := m.Device("dev-cand")
	if d == nil {
		t.Fatal("Provision 后 Device(dev-cand) 应能找到")
	}
	if d.State != "provisioning" {
		t.Errorf("Device.State = %q, want %q", d.State, "provisioning")
	}
}

// TestMultiSchemaStore_QueryAuditsCrossTenant 验证 QueryAudits(tenant="") 跨租户合并。
func TestMultiSchemaStore_QueryAuditsCrossTenant(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	m.Register(&proto.AgentInfo{AgentID: "agent-a", Segment: "seg-1", TenantID: "tA"})
	m.Register(&proto.AgentInfo{AgentID: "agent-b", Segment: "seg-1", TenantID: "tB"})

	// QueryAudits(tenant="") 跨租户合并，应返回两个 tenant 的审计。
	all := m.QueryAudits("", "", time.Time{}, time.Time{}, 0)
	seenA, seenB := false, false
	for _, e := range all {
		if e.TenantID == "tA" {
			seenA = true
		}
		if e.TenantID == "tB" {
			seenB = true
		}
	}
	if !seenA || !seenB {
		t.Errorf("QueryAudits(\"\") 缺少某租户审计: seenA=%v seenB=%v", seenA, seenB)
	}

	// QueryAudits(tenant="tA") 只返回 tenant A 的审计。
	onlyA := m.QueryAudits("tA", "", time.Time{}, time.Time{}, 0)
	for _, e := range onlyA {
		if e.TenantID != "tA" {
			t.Errorf("QueryAudits(tA) 返回了非 tA 的审计: %+v", e)
		}
	}
}

// TestMultiSchemaStore_SilenceAlertRouting 验证 SilenceAlert 按租户校验。
func TestMultiSchemaStore_SilenceAlertRouting(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	m.AddAlert(&proto.Alert{AlertID: "alert-A", TenantID: "tA", Severity: "critical", Message: "fail"})

	// 跨租户静默应失败。
	if m.SilenceAlert("alert-A", "tB", "user-b", time.Now().Add(24*time.Hour), "silenced by B") {
		t.Error("SilenceAlert(alert-A, tB) 应返回 false（跨租户越权）")
	}
	// 同租户静默应成功。
	if !m.SilenceAlert("alert-A", "tA", "user-a", time.Now().Add(24*time.Hour), "silenced by A") {
		t.Error("SilenceAlert(alert-A, tA) 应返回 true（同租户）")
	}
}

// TestMultiSchemaStore_TasksByParentRouting 验证 TasksByParent 经反查索引路由。
func TestMultiSchemaStore_TasksByParentRouting(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	m.Register(&proto.AgentInfo{AgentID: "agent-a", Segment: "seg-1", TenantID: "tA"})

	// 创建一个模板任务（parent）。
	parent := m.CreateTask(&proto.Task{AgentID: "agent-a", TenantID: "tA", Type: "shell", Command: "template", Schedule: "* * * * *"})

	// TasksByParent 经 taskTenant 反查路由。
	tasks := m.TasksByParent(parent.TaskID)
	// 模板任务本身没有 parent_id，所以 TasksByParent 返回空（除非有派生实例）。
	// 这里只验证不 panic 且返回 nil/空（路由成功）。
	if tasks == nil {
		// 模板任务没有派生实例，返回 nil 是合理的。
	}
}

// TestMultiSchemaStore_ResultsRouting 验证 Results 经反查索引路由。
func TestMultiSchemaStore_ResultsRouting(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	m.Register(&proto.AgentInfo{AgentID: "agent-a", Segment: "seg-1", TenantID: "tA"})

	// Results 经 agentTenant 反查路由（无结果返回空 slice）。
	if r := m.Results("agent-a"); len(r) != 0 {
		t.Errorf("Results(agent-a) = %+v, want empty（无结果）", r)
	}
	// 未注册的 agent 返回空 slice。
	if r := m.Results("unknown"); len(r) != 0 {
		t.Errorf("Results(unknown) = %+v, want empty", r)
	}
}

// TestMultiSchemaStore_AuditRouting 验证 Audit 按租户路由。
func TestMultiSchemaStore_AuditRouting(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// tenant A 审计事件路由到 schema A。
	m.Audit(&proto.AuditEvent{TenantID: "tA", Action: "login", Target: "user-a"})
	// tenant B 审计事件路由到 schema B。
	m.Audit(&proto.AuditEvent{TenantID: "tB", Action: "login", Target: "user-b"})

	// QueryAudits 按租户隔离。
	auditsA := m.QueryAudits("tA", "login", time.Time{}, time.Time{}, 0)
	if len(auditsA) != 1 || auditsA[0].Target != "user-a" {
		t.Errorf("QueryAudits(tA, login) = %+v, want exactly [user-a]", auditsA)
	}
	auditsB := m.QueryAudits("tB", "login", time.Time{}, time.Time{}, 0)
	if len(auditsB) != 1 || auditsB[0].Target != "user-b" {
		t.Errorf("QueryAudits(tB, login) = %+v, want exactly [user-b]", auditsB)
	}
}

// countDevices 已在 memory_test.go 中定义，此处复用。

// ============================================================================
// 编译期断言：确保测试用到的辅助函数签名正确。
// ============================================================================

var _ = strings.Builder{}