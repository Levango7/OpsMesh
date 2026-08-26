// stub_semantics_test.go 桩语义锁定（H11）。
//
// 验证 SQLStore 对未持久化领域（P1-P6 共 15 个 StubDomains）的桩方法返回约定零值，
// 防止桩被误改成"返回填充后的假对象"导致「201 假成功 → GET 404 → 审计已记成功」链路
// （见 stub_guard.go 头注释与 docs/design/FIXPLAN-phase1-6.md §2.2.1）。
//
// 桩语义契约（统一约定）：
//   - Create 类 → nil（绝不返回填充后的假对象）；
//   - Get/Update 类 → (nil, false)；
//   - List 类 → 非 nil 空切片（防上层 range panic）；
//   - Delete 类 → false。
//
// 本测试构造零值 SQLStore（不连 DB），因桩方法仅调 StubNotImplemented + 返回零值，
// 不访问 db/rdb 等字段。覆盖代表性域：ticket（带 tenantID）/ plugin（无 tenantID）/
// billing（Plan+Subscription+Invoice）/ apikey（带 tenantID）/ tenant（无 tenantID）。
// 其余域（slo/traffic/pipeline/argocd/compliance/backup/network/automation/webhook/script）
// 桩模式同构，由 stub_guard.go 的 StubDomains 清单与各 sql_*.go 头注释保证。
package store

import "testing"

// newStubSQLStore 构造零值 SQLStore 用于桩语义测试（不连 DB）。
// 桩方法仅调 StubNotImplemented + 返回零值，不访问 db/rdb 等字段，故零值安全。
func newStubSQLStore() *SQLStore {
	return &SQLStore{}
}

// ============================================================================
// ticket 域（带 tenantID 参数，Phase 1）
// ============================================================================

func TestStub_Ticket_CreateReturnsNil(t *testing.T) {
	s := newStubSQLStore()
	if got := s.CreateTicket("t1", &Ticket{Title: "x"}); got != nil {
		t.Fatalf("CreateTicket = %+v, want nil", got)
	}
}

func TestStub_Ticket_GetReturnsNilFalse(t *testing.T) {
	s := newStubSQLStore()
	got, ok := s.GetTicket("t1", "id-1")
	if got != nil || ok {
		t.Fatalf("GetTicket = (%+v, %v), want (nil, false)", got, ok)
	}
}

func TestStub_Ticket_UpdateReturnsNilFalse(t *testing.T) {
	s := newStubSQLStore()
	got, ok := s.UpdateTicket("t1", &Ticket{ID: "id-1"})
	if got != nil || ok {
		t.Fatalf("UpdateTicket = (%+v, %v), want (nil, false)", got, ok)
	}
}

func TestStub_Ticket_ListReturnsEmptySlice(t *testing.T) {
	s := newStubSQLStore()
	got := s.ListTickets("t1", TicketFilter{})
	if got == nil {
		t.Fatal("ListTickets = nil, want non-nil empty slice (防 range panic)")
	}
	if len(got) != 0 {
		t.Fatalf("ListTickets len = %d, want 0", len(got))
	}
}

func TestStub_Ticket_CloseReturnsNilFalse(t *testing.T) {
	s := newStubSQLStore()
	got, ok := s.CloseTicket("t1", "id-1")
	if got != nil || ok {
		t.Fatalf("CloseTicket = (%+v, %v), want (nil, false)", got, ok)
	}
}

// ============================================================================
// plugin 域（无 tenantID 参数，Phase 6 全局插件市场）
// ============================================================================

func TestStub_Plugin_CreateReturnsNil(t *testing.T) {
	s := newStubSQLStore()
	if got := s.CreatePlugin(&Plugin{Name: "p1"}); got != nil {
		t.Fatalf("CreatePlugin = %+v, want nil", got)
	}
}

func TestStub_Plugin_GetReturnsNilFalse(t *testing.T) {
	s := newStubSQLStore()
	got, ok := s.GetPlugin("id-1")
	if got != nil || ok {
		t.Fatalf("GetPlugin = (%+v, %v), want (nil, false)", got, ok)
	}
}

func TestStub_Plugin_UpdateReturnsNilFalse(t *testing.T) {
	s := newStubSQLStore()
	got, ok := s.UpdatePlugin(&Plugin{ID: "id-1"})
	if got != nil || ok {
		t.Fatalf("UpdatePlugin = (%+v, %v), want (nil, false)", got, ok)
	}
}

func TestStub_Plugin_ListReturnsEmptySlice(t *testing.T) {
	s := newStubSQLStore()
	got := s.ListPlugins()
	if got == nil {
		t.Fatal("ListPlugins = nil, want non-nil empty slice (防 range panic)")
	}
	if len(got) != 0 {
		t.Fatalf("ListPlugins len = %d, want 0", len(got))
	}
}

func TestStub_Plugin_DeleteReturnsFalse(t *testing.T) {
	s := newStubSQLStore()
	if ok := s.DeletePlugin("id-1"); ok {
		t.Fatal("DeletePlugin = true, want false")
	}
}

// ============================================================================
// billing 域（Plan + Subscription + Invoice，Phase 6）
// ============================================================================

func TestStub_BillingPlan_CRUDSemantics(t *testing.T) {
	s := newStubSQLStore()
	if got := s.CreateBillingPlan(&SubscriptionPlan{Name: "basic"}); got != nil {
		t.Fatalf("CreateBillingPlan = %+v, want nil", got)
	}
	got, ok := s.GetBillingPlan("id-1")
	if got != nil || ok {
		t.Fatalf("GetBillingPlan = (%+v, %v), want (nil, false)", got, ok)
	}
	got2, ok2 := s.UpdateBillingPlan(&SubscriptionPlan{ID: "id-1"})
	if got2 != nil || ok2 {
		t.Fatalf("UpdateBillingPlan = (%+v, %v), want (nil, false)", got2, ok2)
	}
	plans := s.ListBillingPlans()
	if plans == nil || len(plans) != 0 {
		t.Fatalf("ListBillingPlans = %+v, want non-nil empty slice", plans)
	}
	if ok := s.DeleteBillingPlan("id-1"); ok {
		t.Fatal("DeleteBillingPlan = true, want false")
	}
}

func TestStub_Subscription_CRUDSemantics(t *testing.T) {
	s := newStubSQLStore()
	if got := s.CreateSubscription(&Subscription{TenantID: "t1"}); got != nil {
		t.Fatalf("CreateSubscription = %+v, want nil", got)
	}
	got, ok := s.GetSubscription("id-1")
	if got != nil || ok {
		t.Fatalf("GetSubscription = (%+v, %v), want (nil, false)", got, ok)
	}
	got2, ok2 := s.UpdateSubscription(&Subscription{ID: "id-1"})
	if got2 != nil || ok2 {
		t.Fatalf("UpdateSubscription = (%+v, %v), want (nil, false)", got2, ok2)
	}
	subs := s.ListSubscriptions("t1")
	if subs == nil || len(subs) != 0 {
		t.Fatalf("ListSubscriptions = %+v, want non-nil empty slice", subs)
	}
	if ok := s.DeleteSubscription("id-1"); ok {
		t.Fatal("DeleteSubscription = true, want false")
	}
}

func TestStub_Invoice_CRUDSemantics(t *testing.T) {
	s := newStubSQLStore()
	if got := s.CreateInvoice(&Invoice{TenantID: "t1"}); got != nil {
		t.Fatalf("CreateInvoice = %+v, want nil", got)
	}
	got, ok := s.GetInvoice("id-1")
	if got != nil || ok {
		t.Fatalf("GetInvoice = (%+v, %v), want (nil, false)", got, ok)
	}
	invs := s.ListInvoices("t1")
	if invs == nil || len(invs) != 0 {
		t.Fatalf("ListInvoices = %+v, want non-nil empty slice", invs)
	}
}

// ============================================================================
// apikey 域（带 tenantID 参数，Phase 6）
// ============================================================================

func TestStub_APIKey_CRUDSemantics(t *testing.T) {
	s := newStubSQLStore()
	if got := s.CreateAPIKey("t1", &APIKey{Name: "k1"}); got != nil {
		t.Fatalf("CreateAPIKey = %+v, want nil", got)
	}
	got, ok := s.GetAPIKey("t1", "id-1")
	if got != nil || ok {
		t.Fatalf("GetAPIKey = (%+v, %v), want (nil, false)", got, ok)
	}
	got2, ok2 := s.UpdateAPIKey("t1", &APIKey{ID: "id-1"})
	if got2 != nil || ok2 {
		t.Fatalf("UpdateAPIKey = (%+v, %v), want (nil, false)", got2, ok2)
	}
	keys := s.ListAPIKeys("t1")
	if keys == nil || len(keys) != 0 {
		t.Fatalf("ListAPIKeys = %+v, want non-nil empty slice", keys)
	}
	if ok := s.DeleteAPIKey("t1", "id-1"); ok {
		t.Fatal("DeleteAPIKey = true, want false")
	}
}

// ============================================================================
// tenant 域（无 tenantID 参数，Phase 6 平台级）
// ============================================================================

func TestStub_Tenant_CRUDSemantics(t *testing.T) {
	s := newStubSQLStore()
	if got := s.CreateTenant(&Tenant{Name: "t1"}); got != nil {
		t.Fatalf("CreateTenant = %+v, want nil", got)
	}
	got, ok := s.GetTenant("id-1")
	if got != nil || ok {
		t.Fatalf("GetTenant = (%+v, %v), want (nil, false)", got, ok)
	}
	got2, ok2 := s.UpdateTenant(&Tenant{ID: "id-1"})
	if got2 != nil || ok2 {
		t.Fatalf("UpdateTenant = (%+v, %v), want (nil, false)", got2, ok2)
	}
	tenants := s.ListTenants()
	if tenants == nil || len(tenants) != 0 {
		t.Fatalf("ListTenants = %+v, want non-nil empty slice", tenants)
	}
	if ok := s.DeleteTenant("id-1"); ok {
		t.Fatal("DeleteTenant = true, want false")
	}
}

// ============================================================================
// StubDomains 清单完整性（防新增领域漏注册桩告警）
// ============================================================================

// TestStubDomains_ContainsPhase6Domains 验证 StubDomains 清单包含 P6 四域。
// 防止新增领域漏注册导致 SQL 后端静默失效（H3 缓解：让空壳可见）。
func TestStubDomains_ContainsPhase6Domains(t *testing.T) {
	wantDomains := map[string]bool{
		"ticket": true, "slo": true, "traffic": true, "pipeline": true, "argocd": true,
		"compliance": true, "backup": true, "network": true, "automation": true, "webhook": true,
		"script": true, "tenant": true, "apikey": true, "plugin": true, "billing": true,
	}
	got := make(map[string]bool, len(StubDomains))
	for _, d := range StubDomains {
		got[d] = true
	}
	for d := range wantDomains {
		if !got[d] {
			t.Fatalf("StubDomains 缺少领域 %q", d)
		}
	}
	if len(StubDomains) != len(wantDomains) {
		t.Fatalf("StubDomains 数量 = %d, want %d（新增领域须同步更新清单）", len(StubDomains), len(wantDomains))
	}
}