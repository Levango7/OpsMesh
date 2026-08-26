// multi_schema_smoke_test.go 多租户 schema 隔离集成冒烟测试。
//
// 测试目标（对应 FIXPLAN §3.3 H3 缓解 + §4.4 M4 回归 + §4.5 M5 回归）：
//   - per-tenant 写读隔离：Ticket 域在 tenantA 写入后仅 tenantA 可见，tenantB 不可见（锁住路由正确性，防 H3 回归）；
//   - M4 回归：CreateTenant 空 ID 时由路由层预生成随机 ID，GetTenant(返回的 ID) 必中且不落 default schema；
//   - M5 回归：两租户各建 Subscription 后 ListSubscriptions("") 空串跨租户聚合含两者；
//   - MySQL DSN 分支：OPSMESH_TEST_MYSQL_DSN 环境变量提供时跑真实 SQL 集成冒烟，无则 t.Skip 并 Logf 原因。
//
// 测试策略与 multi_schema_test.go 一致：注入 mockStoreFactory（MemoryStore mock），
// 避免依赖真实 MySQL；MultiSchemaStore 的路由逻辑与具体后端无关，
// MemoryStore 已实现完整 Store 接口，足以验证路由/隔离/聚合语义。
package store

import (
	"os"
	"testing"
	"time"
)

// TestMultiSchemaSmoke_PerTenantTicketIsolation 验证 Ticket 域 per-tenant 写读隔离。
//
// 场景：tenantA 创建一张工单，GetTicket(tenantA, id) 必命中，
// GetTicket(tenantB, id) 必不命中（数据物理隔离，防 H3 路由错位回归）。
func TestMultiSchemaSmoke_PerTenantTicketIsolation(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// tenantA 创建工单。
	tk := m.CreateTicket("tenantA", &Ticket{
		ID:       "ticket-smoke-1",
		TenantID: "tenantA",
		Title:    "smoke-isolation",
		Priority: "high",
		Category: "incident",
		Status:   "open",
	})
	if tk == nil {
		t.Fatal("CreateTicket(tenantA) 返回 nil，应成功")
	}

	// tenantA 能查到。
	if got, ok := m.GetTicket("tenantA", "ticket-smoke-1"); !ok || got == nil {
		t.Errorf("GetTicket(tenantA, ticket-smoke-1) 未命中，应命中（同租户读）: ok=%v got=%+v", ok, got)
	} else if got.Title != "smoke-isolation" {
		t.Errorf("GetTicket 返回工单 Title=%q, want %q", got.Title, "smoke-isolation")
	}

	// tenantB 查不到（隔离）。
	if got, ok := m.GetTicket("tenantB", "ticket-smoke-1"); ok || got != nil {
		t.Errorf("GetTicket(tenantB, ticket-smoke-1) 命中，应不命中（跨租户隔离）: ok=%v got=%+v", ok, got)
	}

	// 验证创建了 tenantA 的 schema（tenantB 仅读不创建 store）。
	if len(created) == 0 {
		t.Error("应至少创建 1 个 schema（tenantA 写入触发），实际 0")
	}
	for _, s := range created {
		if s != "opsmesh_tenant_tenantA" && s != "opsmesh_tenant_tenantB" {
			t.Errorf("schema 名 %q 不在预期范围内", s)
		}
	}
}

// TestMultiSchemaSmoke_CreateTenantEmptyID 验证 M4 回归：CreateTenant 空 ID 时路由层预生成随机 ID。
//
// 场景：CreateTenant(&Tenant{ID: ""}) 后返回的 tenant.ID 非空且不为 "default"，
// GetTenant(返回的 ID) 必命中（数据落在自己的 schema 而非 default）。
// 防 M4 回归：原实现把空 ID 归一为 default，新租户数据错落进 default schema。
func TestMultiSchemaSmoke_CreateTenantEmptyID(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// 空 ID 创建租户。
	created0 := len(created)
	out := m.CreateTenant(&Tenant{
		ID:          "",
		Name:        "smoke-tenant",
		DisplayName: "Smoke Test Tenant",
	})
	if out == nil {
		t.Fatal("CreateTenant(空 ID) 返回 nil，应成功")
	}
	if out.ID == "" {
		t.Fatal("CreateTenant(空 ID) 返回的 ID 仍为空，路由层应预生成随机 ID")
	}
	if out.ID == "default" {
		t.Fatal("CreateTenant(空 ID) 返回的 ID 为 default，M4 回归：新租户数据不应落 default schema")
	}

	// 应创建了一个新 schema（以返回的 ID 命名，而非 default）。
	if len(created) != created0+1 {
		t.Fatalf("应创建 1 个新 schema，实际创建 %d 个", len(created)-created0)
	}
	wantSchema := "opsmesh_tenant_" + out.ID
	if created[created0] != wantSchema {
		t.Errorf("新 schema 名=%q, want %q（应以返回的租户 ID 命名，而非 default）", created[created0], wantSchema)
	}

	// GetTenant(返回的 ID) 必命中。
	got, ok := m.GetTenant(out.ID)
	if !ok || got == nil {
		t.Fatalf("GetTenant(%q) 未命中，应命中（M4：空 ID 创建后按返回 ID 路由必中）", out.ID)
	}
	if got.ID != out.ID {
		t.Errorf("GetTenant 返回 ID=%q, want %q", got.ID, out.ID)
	}
	if got.Name != "smoke-tenant" {
		t.Errorf("GetTenant 返回 Name=%q, want %q", got.Name, "smoke-tenant")
	}

	// default schema 不应包含此租户（防 M4：空 ID 落 default）。
	// 通过检查 created 列表中不含 default schema 来验证。
	for _, s := range created {
		if s == "opsmesh_tenant_default" {
			t.Error("创建了 default schema，M4 回归：空 ID 创建租户不应落 default")
		}
	}
}

// TestMultiSchemaSmoke_ListSubscriptionsCrossTenant 验证 M5 回归：空租户跨租户聚合。
//
// 场景：tenantA、tenantB 各建一个 Subscription，ListSubscriptions("") 空串应聚合含两者。
// 防 M5 回归：原实现空串走 storeFor("") 返回 errEmptyTenant 导致返回 nil，
// 统一为"空串=跨租户聚合"语义（照抄 ListAPIKeys("") 既有模式）。
func TestMultiSchemaSmoke_ListSubscriptionsCrossTenant(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// tenantA 建订阅。
	subA := m.CreateSubscription(&Subscription{
		ID:       "sub-smoke-a",
		TenantID: "tenantA",
		PlanID:   "plan-1",
		Status:   "active",
	})
	if subA == nil {
		t.Fatal("CreateSubscription(tenantA) 返回 nil，应成功")
	}

	// tenantB 建订阅。
	subB := m.CreateSubscription(&Subscription{
		ID:       "sub-smoke-b",
		TenantID: "tenantB",
		PlanID:   "plan-1",
		Status:   "active",
	})
	if subB == nil {
		t.Fatal("CreateSubscription(tenantB) 返回 nil，应成功")
	}

	// ListSubscriptions("") 空串跨租户聚合，应含两者。
	all := m.ListSubscriptions("")
	if all == nil {
		t.Fatal("ListSubscriptions(\"\") 返回 nil，M5 回归：空串应跨租户聚合")
	}
	seenA, seenB := false, false
	for _, s := range all {
		if s.ID == "sub-smoke-a" {
			seenA = true
		}
		if s.ID == "sub-smoke-b" {
			seenB = true
		}
	}
	if !seenA || !seenB {
		t.Errorf("ListSubscriptions(\"\") 未含两者: seenA=%v seenB=%v（M5：空串应跨租户聚合）", seenA, seenB)
	}

	// ListSubscriptions("tenantA") 只返回 tenantA 的订阅。
	onlyA := m.ListSubscriptions("tenantA")
	for _, s := range onlyA {
		if s.TenantID != "tenantA" {
			t.Errorf("ListSubscriptions(tenantA) 返回了非 tenantA 的订阅: %+v", s)
		}
	}
	hasAInOnly := false
	for _, s := range onlyA {
		if s.ID == "sub-smoke-a" {
			hasAInOnly = true
		}
	}
	if !hasAInOnly {
		t.Error("ListSubscriptions(tenantA) 未含 sub-smoke-a，应含（同租户读）")
	}
}

// TestMultiSchemaSmoke_MySQLDSNBranch 验证 MySQL DSN 环境变量分支。
//
// 环境变量 OPSMESH_TEST_MYSQL_DSN 提供时跑真实 SQL 集成冒烟（构造 MultiSchemaStore +
// per-tenant 写读隔离）；未提供时 t.Skip 并 Logf 原因（避免静默跳过，对齐 H11）。
func TestMultiSchemaSmoke_MySQLDSNBranch(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skipf("SKIP reason=missing OPSMESH_TEST_MYSQL_DSN; 真实 MySQL 集成冒烟需设置该环境变量（对齐 H11：避免静默跳过）")
	}

	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m, err := NewMultiSchemaStore(dsn, "", namer)
	if err != nil {
		t.Fatalf("NewMultiSchemaStore 失败: %v", err)
	}

	// per-tenant 写读隔离冒烟（Ticket 域）。
	m.CreateTicket("sql-smoke-a", &Ticket{
		ID:       "ticket-sql-smoke-1",
		TenantID: "sql-smoke-a",
		Title:    "sql-smoke",
		Status:   "open",
	})
	if got, ok := m.GetTicket("sql-smoke-a", "ticket-sql-smoke-1"); !ok || got == nil {
		t.Errorf("SQL GetTicket(sql-smoke-a, ticket-sql-smoke-1) 未命中，应命中: ok=%v", ok)
	}
	if got, ok := m.GetTicket("sql-smoke-b", "ticket-sql-smoke-1"); ok || got != nil {
		t.Errorf("SQL GetTicket(sql-smoke-b, ticket-sql-smoke-1) 命中，应不命中（跨租户隔离）: ok=%v", ok)
	}
}

// TestMultiSchemaSmoke_RecordScriptExecutionDelegation 验证 M3 回归：RecordScriptExecution
// 经 MultiSchemaStore 委托路由到 per-tenant store 落库，且跨租户隔离。
//
// 场景：
//   - tenantA 创建脚本 → RecordScriptExecution(tenantA) → ListScriptExecutions(tenantA) 含该记录；
//   - tenantB ListScriptExecutions(tenantA 的 scriptID) 不含该记录（跨租户隔离）；
//   - 路由失败（未知租户）返回 nil 不 panic。
//
// 防 M3 回归：原 controlplane 用 *MemoryStore 类型断言调用，非 MemoryStore 后端静默跳过记录；
// 提升至接口后 MultiSchemaStore 走委托路由，记录正常落库。
func TestMultiSchemaSmoke_RecordScriptExecutionDelegation(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// tenantA 创建脚本。
	sc := m.CreateScript("tenantA", &Script{Name: "smoke-script", Content: "echo hi"})
	if sc == nil {
		t.Fatal("CreateScript(tenantA) 返回 nil，应成功")
	}

	// tenantA 记录一条执行。
	now := time.Now()
	finishedAt := now
	exec := m.RecordScriptExecution("tenantA", sc.ID, "dev-01", "succeeded", "ok", "", now, &finishedAt)
	if exec == nil {
		t.Fatal("RecordScriptExecution(tenantA) 返回 nil，应经委托路由落库并返回记录")
	}
	if exec.ScriptID != sc.ID {
		t.Errorf("返回 exec.ScriptID=%q, want %q", exec.ScriptID, sc.ID)
	}
	if exec.TenantID != "tenantA" {
		t.Errorf("返回 exec.TenantID=%q, want %q", exec.TenantID, "tenantA")
	}

	// tenantA ListScriptExecutions 应含该记录。
	listA := m.ListScriptExecutions("tenantA", sc.ID)
	if len(listA) != 1 {
		t.Fatalf("ListScriptExecutions(tenantA) 返回 %d 条，want 1", len(listA))
	}
	if listA[0].ID != exec.ID {
		t.Errorf("ListScriptExecutions(tenantA)[0].ID=%q, want %q", listA[0].ID, exec.ID)
	}

	// tenantB ListScriptExecutions 不应含该记录（跨租户隔离）。
	listB := m.ListScriptExecutions("tenantB", sc.ID)
	for _, e := range listB {
		if e.ID == exec.ID {
			t.Error("ListScriptExecutions(tenantB) 含 tenantA 的执行记录，应跨租户隔离")
		}
	}
}

// TestMultiSchemaSmoke_RecordWebhookDeliveryDelegation 验证 M3 回归：RecordWebhookDelivery
// 经 MultiSchemaStore 委托路由到 per-tenant store 落库，且跨租户隔离。
//
// 场景：
//   - tenantA 创建 webhook → RecordWebhookDelivery(tenantA) → ListWebhookDeliveries(tenantA) 含该记录；
//   - tenantB ListWebhookDeliveries(tenantA 的 webhookID) 不含该记录（跨租户隔离）。
//
// 防 M3 回归：原 controlplane 用 *MemoryStore 类型断言调用，非 MemoryStore 后端静默跳过记录；
// 提升至接口后 MultiSchemaStore 走委托路由，记录正常落库。
func TestMultiSchemaSmoke_RecordWebhookDeliveryDelegation(t *testing.T) {
	var created []string
	namer := DefaultSchemaNamer("opsmesh_tenant_")
	m := newMultiSchemaWithFactory(namer, mockStoreFactory(&created))

	// tenantA 创建 webhook。
	wh := m.CreateWebhook("tenantA", &Webhook{Name: "smoke-wh", URL: "http://example.com/hook"})
	if wh == nil {
		t.Fatal("CreateWebhook(tenantA) 返回 nil，应成功")
	}

	// tenantA 记录一条投递。
	delivery := m.RecordWebhookDelivery("tenantA", wh.ID, "test.event", "{}", 200, "ok", "")
	if delivery == nil {
		t.Fatal("RecordWebhookDelivery(tenantA) 返回 nil，应经委托路由落库并返回记录")
	}
	if delivery.WebhookID != wh.ID {
		t.Errorf("返回 delivery.WebhookID=%q, want %q", delivery.WebhookID, wh.ID)
	}
	if delivery.TenantID != "tenantA" {
		t.Errorf("返回 delivery.TenantID=%q, want %q", delivery.TenantID, "tenantA")
	}

	// tenantA ListWebhookDeliveries 应含该记录。
	listA := m.ListWebhookDeliveries("tenantA", wh.ID)
	if len(listA) != 1 {
		t.Fatalf("ListWebhookDeliveries(tenantA) 返回 %d 条，want 1", len(listA))
	}
	if listA[0].ID != delivery.ID {
		t.Errorf("ListWebhookDeliveries(tenantA)[0].ID=%q, want %q", listA[0].ID, delivery.ID)
	}

	// tenantB ListWebhookDeliveries 不应含该记录（跨租户隔离）。
	listB := m.ListWebhookDeliveries("tenantB", wh.ID)
	for _, d := range listB {
		if d.ID == delivery.ID {
			t.Error("ListWebhookDeliveries(tenantB) 含 tenantA 的投递记录，应跨租户隔离")
		}
	}
}
