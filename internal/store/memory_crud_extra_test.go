package store

// memory_crud_extra_test.go 任务 #299 补充测试：
// 覆盖此前零覆盖的领域（apikey/argocd/automation/backup/billing/compliance/
// network/pipeline/plugin/slo/traffic），以及 script/tenant/ticket/webhook/
// config/secret/discovery 的缺口方法。
//
// 断言维度（每个领域统一）：
//   1. 正路径 CRUD；
//   2. 错误路径：nil 入参 / 未知 ID / 跨租户访问 / 空 ID；
//   3. 默认值填充（自动 ID / 默认状态 / 时间戳）；
//   4. 深拷贝隔离——读路径锁内 clone 后返回，外部修改副本不得污染内部状态
//      （匹配第四轮审查修复后的语义，参照 memory_config_secret_test.go）。
// 排序类断言一律预填 CreatedAt/StartedAt，避免同刻时间戳导致顺序不稳定。

import (
	"strings"
	"testing"
	"time"
)

// ============================================================================
// API Key 管理（memory_apikey.go）
// ============================================================================

func TestMemoryStore_APIKey_CRUD(t *testing.T) {
	m := NewMemoryStore()

	if got := m.CreateAPIKey("t1", nil); got != nil {
		t.Fatalf("CreateAPIKey(nil) = %+v, want nil", got)
	}

	base := time.Now().Add(-2 * time.Hour)
	k := m.CreateAPIKey("t1", &APIKey{Name: "ci-key", Key: "hash-1",
		Scopes: []string{"device:read"}, CreatedAt: base})
	if k.ID == "" || !strings.HasPrefix(k.ID, "apikey-") {
		t.Fatalf("auto ID = %q, want apikey- prefix", k.ID)
	}
	if k.TenantID != "t1" {
		t.Fatalf("TenantID = %q, want t1", k.TenantID)
	}

	// 空租户归一 default。
	d := m.CreateAPIKey("", &APIKey{Name: "anon"})
	if d.TenantID != "default" {
		t.Fatalf("empty tenant normalized = %q, want default", d.TenantID)
	}

	// Get 正路径 + Scopes 切片深拷贝隔离。
	got, ok := m.GetAPIKey("t1", k.ID)
	if !ok {
		t.Fatal("GetAPIKey miss for existing key")
	}
	got.Scopes[0] = "MUTATED"
	got2, _ := m.GetAPIKey("t1", k.ID)
	if got2.Scopes[0] != "device:read" {
		t.Fatal("Scopes deep copy broken")
	}

	// 错误路径：跨租户 / 未知 ID。
	if _, ok := m.GetAPIKey("t2", k.ID); ok {
		t.Fatal("cross-tenant get should fail")
	}
	if _, ok := m.GetAPIKey("t1", "nope"); ok {
		t.Fatal("unknown id get should fail")
	}

	// Update：正路径 + CreatedAt 保留；跨租户与空 ID 拒绝。
	u, ok := m.UpdateAPIKey("t1", &APIKey{ID: k.ID, Name: "renamed", Enabled: true})
	if !ok || u.Name != "renamed" || !u.Enabled {
		t.Fatalf("update = (%+v,%v)", u, ok)
	}
	if !u.CreatedAt.Equal(base) {
		t.Fatal("CreatedAt should be preserved on update")
	}
	if _, ok := m.UpdateAPIKey("t2", &APIKey{ID: k.ID}); ok {
		t.Fatal("cross-tenant update should fail")
	}
	if _, ok := m.UpdateAPIKey("t1", &APIKey{}); ok {
		t.Fatal("empty-ID update should fail")
	}
	if _, ok := m.UpdateAPIKey("t1", &APIKey{ID: "ghost"}); ok {
		t.Fatal("unknown-ID update should fail")
	}

	// List：租户过滤 + 空串全量 + 创建时间降序。
	m.CreateAPIKey("t1", &APIKey{Name: "older-key", CreatedAt: base.Add(-time.Hour)})
	lst := m.ListAPIKeys("t1")
	if len(lst) != 2 {
		t.Fatalf("ListAPIKeys(t1) = %d, want 2", len(lst))
	}
	if lst[0].Name != "renamed" {
		t.Fatalf("desc order broken: first=%s, want renamed(较新)", lst[0].Name)
	}
	if got := m.ListAPIKeys(""); len(got) != 3 { // t1×2 + default×1
		t.Fatalf("ListAPIKeys(all) = %d, want 3", len(got))
	}

	// Delete：跨租户拒绝 → 成功 → 二次删除 false。
	if m.DeleteAPIKey("t2", k.ID) {
		t.Fatal("cross-tenant delete should fail")
	}
	if !m.DeleteAPIKey("t1", k.ID) {
		t.Fatal("delete should succeed")
	}
	if m.DeleteAPIKey("t1", k.ID) {
		t.Fatal("double delete should fail")
	}
}

// ============================================================================
// ArgoCD 应用（memory_argocd.go）
// ============================================================================

func TestMemoryStore_ArgoCDApp_CRUD_Sync(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreateApp("t1", nil); got != nil {
		t.Fatalf("CreateApp(nil) = %+v, want nil", got)
	}

	a := m.CreateApp("t1", &ArgoCDApp{Name: "guestbook", RepoURL: "https://git.example.com/x.git"})
	if a.ID == "" || !strings.HasPrefix(a.ID, "argocd-") {
		t.Fatalf("auto ID = %q, want argocd- prefix", a.ID)
	}
	if a.Status != "unknown" || a.HealthStatus != "unknown" {
		t.Fatalf("defaults = (%q,%q), want unknown/unknown", a.Status, a.HealthStatus)
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		t.Fatal("timestamps should be filled on create")
	}

	// SyncApp 正路径 + 跨租户/未知拒绝。
	s, ok := m.SyncApp("t1", a.ID)
	if !ok || s.Status != "synced" || s.HealthStatus != "healthy" {
		t.Fatalf("SyncApp = (%+v,%v), want synced/healthy", s, ok)
	}
	if _, ok := m.SyncApp("t2", a.ID); ok {
		t.Fatal("cross-tenant sync should fail")
	}
	if _, ok := m.SyncApp("t1", "nope"); ok {
		t.Fatal("sync unknown app should fail")
	}

	// Get + 深拷贝 + 错误路径。
	g, ok := m.GetApp("t1", a.ID)
	if !ok || g.Name != "guestbook" {
		t.Fatalf("GetApp = (%+v,%v)", g, ok)
	}
	g.Name = "MUTATED"
	if g2, _ := m.GetApp("t1", a.ID); g2.Name != "guestbook" {
		t.Fatal("ArgoCDApp deep copy broken")
	}
	if _, ok := m.GetApp("t2", a.ID); ok {
		t.Fatal("cross-tenant get should fail")
	}

	// Update：CreatedAt/TenantID 保留；空 ID 与未知 ID 拒绝。
	u, ok := m.UpdateApp("t1", &ArgoCDApp{ID: a.ID, Name: "gb2", TargetRevision: "main"})
	if !ok || u.Name != "gb2" || u.TargetRevision != "main" {
		t.Fatalf("UpdateApp = (%+v,%v)", u, ok)
	}
	if _, ok := m.UpdateApp("t2", &ArgoCDApp{ID: a.ID}); ok {
		t.Fatal("cross-tenant update should fail")
	}
	if _, ok := m.UpdateApp("t1", &ArgoCDApp{}); ok {
		t.Fatal("empty-ID update should fail")
	}

	// List：租户过滤 + 降序。
	m.CreateApp("t1", &ArgoCDApp{Name: "second", CreatedAt: time.Now().Add(-time.Hour)})
	if got := m.ListApps("t1"); len(got) != 2 {
		t.Fatalf("ListApps(t1) = %d, want 2", len(got))
	}
	if first := m.ListApps("t1")[0].Name; first != "gb2" {
		t.Fatalf("desc order broken: first=%s, want gb2", first)
	}
	if got := m.ListApps("t9"); len(got) != 0 {
		t.Fatalf("ListApps(other) = %d, want 0", len(got))
	}

	// Delete：跨租户拒绝 → 成功 → 二次 false。
	if m.DeleteApp("t2", a.ID) {
		t.Fatal("cross-tenant delete should fail")
	}
	if !m.DeleteApp("t1", a.ID) || m.DeleteApp("t1", a.ID) {
		t.Fatal("delete semantics broken")
	}
}

// ============================================================================
// 自动化闭环（memory_automation.go）
// ============================================================================

func TestMemoryStore_AutomationRule_Lifecycle(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreateAutomationRule("t1", nil); got != nil {
		t.Fatalf("CreateAutomationRule(nil) = %+v, want nil", got)
	}

	r := m.CreateAutomationRule("t1", &AutomationRule{
		Name:          "restart-on-cpu",
		TriggerType:   "metric_threshold",
		TriggerParams: map[string]string{"metric": "cpu", "op": ">"},
		Actions:       []AutomationAction{{Type: "restart", Params: map[string]string{"host": "web-01"}}},
	})
	if r.ID == "" || !strings.HasPrefix(r.ID, "rule-") {
		t.Fatalf("auto ID = %q, want rule- prefix", r.ID)
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		t.Fatal("timestamps should be filled on create")
	}

	// TriggerParams/Actions 深拷贝：修改副本不影响后续读取。
	r.TriggerParams["metric"] = "MUTATED"
	r.Actions[0].Params["host"] = "MUTATED"
	got, ok := m.GetAutomationRule("t1", r.ID)
	if !ok || got.TriggerParams["metric"] != "cpu" || got.Actions[0].Params["host"] != "web-01" {
		t.Fatalf("nested deep copy broken: %+v", got)
	}
	if _, ok := m.GetAutomationRule("t2", r.ID); ok {
		t.Fatal("cross-tenant get should fail")
	}
	if _, ok := m.GetAutomationRule("t1", "nope"); ok {
		t.Fatal("unknown rule get should fail")
	}

	// Enable/Disable 正路径 + 跨租户/未知拒绝。
	en, ok := m.EnableAutomationRule("t1", r.ID)
	if !ok || !en.Enabled {
		t.Fatal("EnableAutomationRule failed")
	}
	dis, ok := m.DisableAutomationRule("t1", r.ID)
	if !ok || dis.Enabled {
		t.Fatal("DisableAutomationRule failed")
	}
	if _, ok := m.EnableAutomationRule("t9", r.ID); ok {
		t.Fatal("cross-tenant enable should fail")
	}
	if _, ok := m.DisableAutomationRule("t1", "nope"); ok {
		t.Fatal("unknown disable should fail")
	}

	// Update：CreatedAt/TenantID 保留；错误路径。
	u, ok := m.UpdateAutomationRule("t1", &AutomationRule{ID: r.ID, Name: "v2"})
	if !ok || u.Name != "v2" {
		t.Fatalf("update = (%+v,%v)", u, ok)
	}
	if _, ok := m.UpdateAutomationRule("t2", &AutomationRule{ID: r.ID}); ok {
		t.Fatal("cross-tenant update should fail")
	}
	if _, ok := m.UpdateAutomationRule("t1", &AutomationRule{}); ok {
		t.Fatal("empty-ID update should fail")
	}

	// List 租户过滤。
	m.CreateAutomationRule("t2", &AutomationRule{Name: "foreign-rule"})
	if got := m.ListAutomationRules("t1"); len(got) != 1 {
		t.Fatalf("ListRules(t1) = %d, want 1", len(got))
	}
	if got := m.ListAutomationRules(""); len(got) != 2 {
		t.Fatalf("ListRules(all) = %d, want 2", len(got))
	}

	// Delete：跨租户拒绝 → 成功 → 二次 false。
	if m.DeleteAutomationRule("t2", r.ID) || !m.DeleteAutomationRule("t1", r.ID) || m.DeleteAutomationRule("t1", r.ID) {
		t.Fatal("delete semantics broken")
	}
}

func TestMemoryStore_AutomationExecutions_ListLimit(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreateAutomationExecution("t1", nil); got != nil {
		t.Fatalf("CreateAutomationExecution(nil) = %+v, want nil", got)
	}

	base := time.Now().Add(-3 * time.Hour)
	var lastID string
	for i := 0; i < 3; i++ {
		e := m.CreateAutomationExecution("t1", &AutomationExecution{
			RuleName:  "r",
			Status:    "",
			StartedAt: base.Add(time.Duration(i) * time.Hour),
		})
		lastID = e.ID
		if e.Status != "pending" {
			t.Fatalf("default status = %q, want pending", e.Status)
		}
	}
	m.CreateAutomationExecution("t2", &AutomationExecution{}) // 其他租户

	got, ok := m.GetAutomationExecution("t1", lastID)
	if !ok || got.RuleName != "r" {
		t.Fatalf("get = (%+v,%v)", got, ok)
	}
	if _, ok := m.GetAutomationExecution("t2", lastID); ok {
		t.Fatal("cross-tenant get should fail")
	}
	if _, ok := m.GetAutomationExecution("t1", "nope"); ok {
		t.Fatal("unknown execution get should fail")
	}

	all := m.ListAutomationExecutions("t1", 0)
	if len(all) != 3 {
		t.Fatalf("all = %d, want 3", len(all))
	}
	if all[0].ID != lastID { // 按开始时间降序：最新在前
		t.Fatalf("order wrong: first=%s want %s", all[0].ID, lastID)
	}
	if lim := m.ListAutomationExecutions("t1", 2); len(lim) != 2 {
		t.Fatalf("limit = %d, want 2", len(lim))
	}
	if other := m.ListAutomationExecutions("t2", 0); len(other) != 1 {
		t.Fatalf("tenant t2 = %d, want 1", len(other))
	}
}

// ============================================================================
// 灾备备份（memory_backup.go）
// ============================================================================

func TestMemoryStore_Backup_CRUD(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreateBackup("t1", nil); got != nil {
		t.Fatalf("CreateBackup(nil) = %+v, want nil", got)
	}

	b := m.CreateBackup("t1", &BackupRecord{Type: "full", Size: 1024, Path: "/backup/a.tar"})
	if b.ID == "" || !strings.HasPrefix(b.ID, "backup-") {
		t.Fatalf("auto ID = %q, want backup- prefix", b.ID)
	}
	if b.Status != "creating" {
		t.Fatalf("default status = %q, want creating", b.Status)
	}
	if b.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be filled")
	}

	if g, ok := m.GetBackup("t1", b.ID); !ok || g.Size != 1024 {
		t.Fatalf("Get = (%+v,%v)", g, ok)
	}
	if _, ok := m.GetBackup("t2", b.ID); ok {
		t.Fatal("cross-tenant get should fail")
	}
	if _, ok := m.GetBackup("t1", "nope"); ok {
		t.Fatal("unknown backup get should fail")
	}

	m.CreateBackup("t1", &BackupRecord{Type: "config", CreatedAt: time.Now().Add(-time.Hour)})
	if got := m.ListBackups("t1"); len(got) != 2 {
		t.Fatalf("list(t1) = %d, want 2", len(got))
	}
	if first := m.ListBackups("t1")[0].Type; first != "full" { // 降序：最新在前
		t.Fatalf("desc order broken: first=%s", first)
	}
	if got := m.ListBackups(""); len(got) != 2 {
		t.Fatalf("list(all) = %d, want 2", len(got))
	}

	// ListBackups 返回深拷贝：修改副本不影响内部。
	lst := m.ListBackups("t1")
	lst[0].Path = "MUTATED"
	if g, _ := m.GetBackup("t1", lst[0].ID); g.Path == "MUTATED" {
		t.Fatal("BackupRecord deep copy broken")
	}

	if m.DeleteBackup("t2", b.ID) || !m.DeleteBackup("t1", b.ID) || m.DeleteBackup("t1", b.ID) {
		t.Fatal("delete semantics broken")
	}
}

// ============================================================================
// 计费（memory_billing.go）
// ============================================================================

func TestMemoryStore_BillingPlans_CRUD(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreateBillingPlan(nil); got != nil {
		t.Fatalf("CreateBillingPlan(nil) = %+v, want nil", got)
	}

	p := m.CreateBillingPlan(&SubscriptionPlan{Name: "pro", Price: 9900, Interval: "monthly",
		Features: []string{"sso", "audit"}})
	if p.ID == "" || !strings.HasPrefix(p.ID, "plan-") {
		t.Fatalf("auto ID = %q, want plan- prefix", p.ID)
	}

	// Features 深拷贝隔离。
	p.Features[0] = "MUTATED"
	g, ok := m.GetBillingPlan(p.ID)
	if !ok || g.Features[0] != "sso" {
		t.Fatal("Features deep copy broken")
	}
	if _, ok := m.GetBillingPlan("nope"); ok {
		t.Fatal("unknown plan get should fail")
	}

	u, ok := m.UpdateBillingPlan(&SubscriptionPlan{ID: p.ID, Name: "pro-v2", Price: 12900})
	if !ok || u.Name != "pro-v2" || u.Price != 12900 {
		t.Fatalf("update = (%+v,%v)", u, ok)
	}
	if u.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be preserved on update")
	}
	if _, ok := m.UpdateBillingPlan(&SubscriptionPlan{}); ok {
		t.Fatal("empty-ID update should fail")
	}
	if _, ok := m.UpdateBillingPlan(&SubscriptionPlan{ID: "ghost"}); ok {
		t.Fatal("unknown plan update should fail")
	}

	m.CreateBillingPlan(&SubscriptionPlan{Name: "free", CreatedAt: time.Now().Add(-time.Hour)})
	plans := m.ListBillingPlans()
	if len(plans) != 2 {
		t.Fatalf("plans = %d, want 2", len(plans))
	}
	if plans[0].Name != "free" { // 升序：最早创建在前
		t.Fatalf("asc order broken: first=%s", plans[0].Name)
	}

	if !m.DeleteBillingPlan(p.ID) || m.DeleteBillingPlan(p.ID) {
		t.Fatal("delete semantics broken")
	}
}

func TestMemoryStore_Subscriptions_CRUD(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreateSubscription(nil); got != nil {
		t.Fatalf("CreateSubscription(nil) = %+v, want nil", got)
	}

	s := m.CreateSubscription(&Subscription{TenantID: "t1", PlanID: "plan-1"})
	if s.ID == "" || !strings.HasPrefix(s.ID, "sub-") {
		t.Fatalf("auto ID = %q, want sub- prefix", s.ID)
	}
	if s.Status != "active" {
		t.Fatalf("default status = %q, want active", s.Status)
	}

	if g, ok := m.GetSubscription(s.ID); !ok || g.PlanID != "plan-1" {
		t.Fatalf("get = (%+v,%v)", g, ok)
	}
	if _, ok := m.GetSubscription("nope"); ok {
		t.Fatal("unknown sub get should fail")
	}

	m.CreateSubscription(&Subscription{TenantID: "t2"})
	m.CreateSubscription(&Subscription{TenantID: "t1", CreatedAt: time.Now().Add(-time.Hour)})
	lst := m.ListSubscriptions("t1")
	if len(lst) != 2 {
		t.Fatalf("subs(t1) = %d, want 2", len(lst))
	}
	if lst[0].ID != s.ID { // 降序：最新在前
		t.Fatal("desc order broken")
	}
	if other := m.ListSubscriptions("t9"); len(other) != 0 {
		t.Fatalf("subs(t9) = %d, want 0", len(other))
	}

	// Update：TenantID 不可改（防越权改归属）。
	u, ok := m.UpdateSubscription(&Subscription{ID: s.ID, Status: "canceled", TenantID: "evil"})
	if !ok || u.Status != "canceled" {
		t.Fatalf("update = (%+v,%v)", u, ok)
	}
	if u.TenantID != "t1" {
		t.Fatalf("TenantID overwritten to %q, want t1", u.TenantID)
	}
	if _, ok := m.UpdateSubscription(&Subscription{}); ok {
		t.Fatal("empty-ID update should fail")
	}
	if _, ok := m.UpdateSubscription(&Subscription{ID: "ghost"}); ok {
		t.Fatal("unknown sub update should fail")
	}

	if !m.DeleteSubscription(s.ID) || m.DeleteSubscription(s.ID) {
		t.Fatal("delete semantics broken")
	}
}

func TestMemoryStore_Invoices_CreateGetList(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreateInvoice(nil); got != nil {
		t.Fatalf("CreateInvoice(nil) = %+v, want nil", got)
	}

	inv := m.CreateInvoice(&Invoice{TenantID: "t1", Amount: 500,
		Items: []InvoiceItem{{Name: "license", Quantity: 1, UnitPrice: 500, Amount: 500}}})
	if inv.ID == "" || !strings.HasPrefix(inv.ID, "inv-") {
		t.Fatalf("auto ID = %q, want inv- prefix", inv.ID)
	}
	if inv.Status != "pending" {
		t.Fatalf("default status = %q, want pending", inv.Status)
	}

	// Items 深拷贝隔离。
	inv.Items[0].Name = "MUTATED"
	g, ok := m.GetInvoice(inv.ID)
	if !ok || g.Items[0].Name != "license" {
		t.Fatal("Items deep copy broken")
	}
	if _, ok := m.GetInvoice("nope"); ok {
		t.Fatal("unknown invoice get should fail")
	}

	m.CreateInvoice(&Invoice{TenantID: "t2"})
	if got := m.ListInvoices("t1"); len(got) != 1 {
		t.Fatalf("invoices(t1) = %d, want 1", len(got))
	}
	if got := m.ListInvoices(""); len(got) != 2 {
		t.Fatalf("invoices(all) = %d, want 2", len(got))
	}
	if got := m.ListInvoices("t9"); len(got) != 0 {
		t.Fatalf("invoices(t9) = %d, want 0", len(got))
	}
}

// ============================================================================
// 安全合规（memory_compliance.go）
// ============================================================================

func TestMemoryStore_ComplianceReports_CRUD(t *testing.T) {
	m := NewMemoryStore()
	if got := m.SaveReport("t1", nil); got != nil {
		t.Fatalf("SaveReport(nil) = %+v, want nil", got)
	}

	r := m.SaveReport("t1", &ComplianceReport{DeviceID: "dev-1", Score: 95,
		Results: []ComplianceResult{{RuleID: "cis-1.1", Passed: true}}})
	if r.ID == "" || !strings.HasPrefix(r.ID, "compliance-") {
		t.Fatalf("auto ID = %q, want compliance- prefix", r.ID)
	}
	if r.TenantID != "t1" {
		t.Fatalf("TenantID = %q, want t1", r.TenantID)
	}

	// Results 深拷贝隔离。
	r.Results[0].RuleID = "MUTATED"
	g, ok := m.GetReport("t1", r.ID)
	if !ok || g.Results[0].RuleID != "cis-1.1" {
		t.Fatal("Results deep copy broken")
	}
	if _, ok := m.GetReport("t2", r.ID); ok {
		t.Fatal("cross-tenant get should fail")
	}
	if _, ok := m.GetReport("t1", "nope"); ok {
		t.Fatal("unknown report get should fail")
	}

	m.SaveReport("t1", &ComplianceReport{DeviceID: "dev-2", Score: 80, CreatedAt: time.Now().Add(-time.Hour)})
	lst := m.ListReports("t1")
	if len(lst) != 2 {
		t.Fatalf("reports = %d, want 2", len(lst))
	}
	if lst[0].DeviceID != "dev-1" { // 降序：最新在前
		t.Fatalf("desc order broken: first=%s", lst[0].DeviceID)
	}
	if got := m.ListReports("t9"); len(got) != 0 {
		t.Fatalf("reports(t9) = %d, want 0", len(got))
	}

	if m.DeleteReport("t2", r.ID) || !m.DeleteReport("t1", r.ID) || m.DeleteReport("t1", r.ID) {
		t.Fatal("delete semantics broken")
	}
}

// ============================================================================
// 网络管理（memory_network.go）
// ============================================================================

func TestMemoryStore_NetworkDevices_CRUD_Metrics(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreateNetworkDevice("t1", nil); got != nil {
		t.Fatalf("CreateNetworkDevice(nil) = %+v, want nil", got)
	}

	d := m.CreateNetworkDevice("t1", &NetworkDevice{Name: "sw-1", Type: "switch", IP: "10.0.0.2"})
	if d.ID == "" || !strings.HasPrefix(d.ID, "netdev-") {
		t.Fatalf("auto ID = %q, want netdev- prefix", d.ID)
	}
	if d.Status != "unknown" || d.CreatedAt.IsZero() || d.UpdatedAt.IsZero() {
		t.Fatalf("defaults missing: %+v", d)
	}

	if g, ok := m.GetNetworkDevice("t1", d.ID); !ok || g.IP != "10.0.0.2" {
		t.Fatalf("get = (%+v,%v)", g, ok)
	}
	if _, ok := m.GetNetworkDevice("t2", d.ID); ok {
		t.Fatal("cross-tenant get should fail")
	}

	// Metrics：nil 与空 deviceID 安全；写入可读；未知设备 nil。
	m.StoreNetworkMetrics("", nil)
	m.StoreNetworkMetrics(d.ID, nil)
	if m.GetNetworkMetrics("no-such-dev") != nil {
		t.Fatal("metrics for unknown device should be nil")
	}
	ts := time.Now().Add(-time.Minute)
	m.StoreNetworkMetrics(d.ID, &NetworkMetrics{CPUUsage: 12.5, Timestamp: ts})
	metric := m.GetNetworkMetrics(d.ID)
	if metric == nil || metric.CPUUsage != 12.5 || metric.DeviceID != d.ID {
		t.Fatalf("metrics = %+v", metric)
	}
	if !metric.Timestamp.Equal(ts) {
		t.Fatalf("timestamp = %v, want provided %v (非零不应被覆盖)", metric.Timestamp, ts)
	}
	// Get 返回副本：修改不影响内部。
	metric.CPUUsage = -1
	if m.GetNetworkMetrics(d.ID).CPUUsage != 12.5 {
		t.Fatal("NetworkMetrics deep copy broken")
	}

	// UpdateNetworkConfig 正路径 + 错误路径。
	c, ok := m.UpdateNetworkConfig("t1", d.ID, "vlan 10")
	if !ok || c.Config != "vlan 10" {
		t.Fatalf("config update = (%+v,%v)", c, ok)
	}
	if _, ok := m.UpdateNetworkConfig("t2", d.ID, "x"); ok {
		t.Fatal("cross-tenant config update should fail")
	}
	if _, ok := m.UpdateNetworkConfig("t1", "nope", "x"); ok {
		t.Fatal("unknown device config update should fail")
	}

	m.CreateNetworkDevice("t1", &NetworkDevice{Name: "sw-2", CreatedAt: time.Now().Add(-time.Hour)})
	if got := m.ListNetworkDevices("t1"); len(got) != 2 {
		t.Fatalf("devices(t1) = %d, want 2", len(got))
	}
	if first := m.ListNetworkDevices("t1")[0].Name; first != "sw-1" { // 降序：最新在前
		t.Fatalf("desc order broken: first=%s", first)
	}
	if got := m.ListNetworkDevices("t9"); len(got) != 0 {
		t.Fatalf("devices(t9) = %d, want 0", len(got))
	}

	// Update（整体替换语义：未提供的字段清空，CreatedAt/TenantID 保留）。
	u, ok := m.UpdateNetworkDevice("t1", &NetworkDevice{ID: d.ID, Vendor: "huawei"})
	if !ok || u.Vendor != "huawei" {
		t.Fatalf("update = (%+v,%v)", u, ok)
	}
	if _, ok := m.UpdateNetworkDevice("t2", &NetworkDevice{ID: d.ID}); ok {
		t.Fatal("cross-tenant update should fail")
	}
	if _, ok := m.UpdateNetworkDevice("t1", &NetworkDevice{}); ok {
		t.Fatal("empty-ID update should fail")
	}

	if m.DeleteNetworkDevice("t2", d.ID) || !m.DeleteNetworkDevice("t1", d.ID) || m.DeleteNetworkDevice("t1", d.ID) {
		t.Fatal("delete semantics broken")
	}
}

// ============================================================================
// CI/CD 流水线（memory_pipeline.go）
// ============================================================================

func TestMemoryStore_PipelineTemplates_CRUD(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreateTemplate("t1", nil); got != nil {
		t.Fatalf("CreateTemplate(nil) = %+v, want nil", got)
	}

	tpl := m.CreateTemplate("t1", &PipelineTemplate{Name: "build", Type: "tekton",
		YAML: "apiVersion: v1", Parameters: []PipelineParam{{Name: "branch", Required: true}}})
	if tpl.ID == "" || !strings.HasPrefix(tpl.ID, "pipeline-") {
		t.Fatalf("auto ID = %q, want pipeline- prefix", tpl.ID)
	}
	if tpl.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt should be filled on create")
	}

	// Parameters 深拷贝隔离。
	tpl.Parameters[0].Name = "MUTATED"
	g, ok := m.GetTemplate("t1", tpl.ID)
	if !ok || g.Parameters[0].Name != "branch" {
		t.Fatal("Parameters deep copy broken")
	}
	if _, ok := m.GetTemplate("t2", tpl.ID); ok {
		t.Fatal("cross-tenant get should fail")
	}
	if _, ok := m.GetTemplate("t1", "nope"); ok {
		t.Fatal("unknown template get should fail")
	}

	m.CreateTemplate("t1", &PipelineTemplate{Name: "deploy", CreatedAt: time.Now().Add(-time.Hour)})
	lst := m.ListTemplates("t1")
	if len(lst) != 2 {
		t.Fatalf("templates = %d, want 2", len(lst))
	}
	if lst[0].Name != "build" { // 降序：最新在前
		t.Fatal("desc order broken")
	}
	if got := m.ListTemplates("t9"); len(got) != 0 {
		t.Fatalf("templates(t9) = %d, want 0", len(got))
	}

	if m.DeleteTemplate("t2", tpl.ID) || !m.DeleteTemplate("t1", tpl.ID) || m.DeleteTemplate("t1", tpl.ID) {
		t.Fatal("delete semantics broken")
	}
}

func TestMemoryStore_PipelineRuns_Lifecycle(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreateRun("t1", nil); got != nil {
		t.Fatalf("CreateRun(nil) = %+v, want nil", got)
	}

	start := time.Now().Add(-time.Minute)
	r := m.CreateRun("t1", &PipelineRun{TemplateID: "tpl-1", TemplateName: "build",
		Status: "running", Parameters: map[string]string{"branch": "main"}, StartedAt: &start})
	if r.ID == "" || !strings.HasPrefix(r.ID, "run-") {
		t.Fatalf("auto ID = %q, want run- prefix", r.ID)
	}

	// Parameters map 与 StartedAt 指针的深拷贝隔离。
	r.Parameters["branch"] = "MUTATED"
	*r.StartedAt = start.Add(time.Hour)
	g, ok := m.GetRun("t1", r.ID)
	if !ok {
		t.Fatal("GetRun miss")
	}
	if g.Parameters["branch"] != "main" {
		t.Fatal("Parameters deep copy broken")
	}
	if g.StartedAt == nil || !g.StartedAt.Equal(start) {
		t.Fatalf("StartedAt mutated through pointer: %v, want %v", g.StartedAt, start)
	}
	if _, ok := m.GetRun("t2", r.ID); ok {
		t.Fatal("cross-tenant get should fail")
	}
	if _, ok := m.GetRun("t1", "nope"); ok {
		t.Fatal("unknown run get should fail")
	}

	// ListRuns：租户过滤 + templateID 过滤 + 降序。
	m.CreateRun("t1", &PipelineRun{TemplateID: "tpl-2", TemplateName: "deploy"})
	m.CreateRun("t2", &PipelineRun{TemplateID: "tpl-1"})
	if got := m.ListRuns("t1", ""); len(got) != 2 {
		t.Fatalf("runs(t1) = %d, want 2", len(got))
	}
	filtered := m.ListRuns("t1", "tpl-1")
	if len(filtered) != 1 || filtered[0].TemplateID != "tpl-1" {
		t.Fatalf("filtered runs = %+v", filtered)
	}
	if got := m.ListRuns("t9", ""); len(got) != 0 {
		t.Fatalf("runs(t9) = %d, want 0", len(got))
	}

	// UpdateRun：保留 TemplateID 关联与未提供的 StartedAt/FinishedAt。
	fin := time.Now()
	u, ok := m.UpdateRun("t1", &PipelineRun{ID: r.ID, Status: "succeeded", Logs: "ok", FinishedAt: &fin})
	if !ok || u.Status != "succeeded" {
		t.Fatalf("update = (%+v,%v)", u, ok)
	}
	if u.TemplateID != "tpl-1" {
		t.Fatalf("TemplateID lost on update: %q", u.TemplateID)
	}
	if u.StartedAt == nil || !u.StartedAt.Equal(start) {
		t.Fatal("StartedAt not preserved on update")
	}
	if u.FinishedAt == nil || !u.FinishedAt.Equal(fin) {
		t.Fatal("FinishedAt not set on update")
	}
	if _, ok := m.UpdateRun("t2", &PipelineRun{ID: r.ID}); ok {
		t.Fatal("cross-tenant update should fail")
	}
	if _, ok := m.UpdateRun("t1", &PipelineRun{}); ok {
		t.Fatal("empty-ID update should fail")
	}
}

// ============================================================================
// 插件市场（memory_plugin.go）
// ============================================================================

func TestMemoryStore_Plugin_CRUD(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreatePlugin(nil); got != nil {
		t.Fatalf("CreatePlugin(nil) = %+v, want nil", got)
	}

	p := m.CreatePlugin(&Plugin{Name: "nvidia-gpu", Version: "1.0.0", Type: "agent"})
	if p.ID == "" || !strings.HasPrefix(p.ID, "plugin-") {
		t.Fatalf("auto ID = %q, want plugin- prefix", p.ID)
	}

	if g, ok := m.GetPlugin(p.ID); !ok || g.Version != "1.0.0" {
		t.Fatalf("get = (%+v,%v)", g, ok)
	}
	if _, ok := m.GetPlugin("nope"); ok {
		t.Fatal("unknown plugin get should fail")
	}

	u, ok := m.UpdatePlugin(&Plugin{ID: p.ID, Installed: true, Enabled: true})
	if !ok || !u.Installed || !u.Enabled {
		t.Fatalf("update = (%+v,%v)", u, ok)
	}
	if u.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be preserved on update")
	}
	if _, ok := m.UpdatePlugin(&Plugin{}); ok {
		t.Fatal("empty-ID update should fail")
	}
	if _, ok := m.UpdatePlugin(&Plugin{ID: "ghost"}); ok {
		t.Fatal("unknown plugin update should fail")
	}

	m.CreatePlugin(&Plugin{Name: "ui-ext", CreatedAt: time.Now().Add(-time.Hour)})
	all := m.ListPlugins()
	if len(all) != 2 {
		t.Fatalf("plugins = %d, want 2", len(all))
	}
	if all[0].Name != "ui-ext" { // 升序：最早创建在前
		t.Fatal("asc order broken")
	}

	if !m.DeletePlugin(p.ID) || m.DeletePlugin(p.ID) {
		t.Fatal("delete semantics broken")
	}
}

// ============================================================================
// SLO 管理（memory_slo.go）
// ============================================================================

func TestMemoryStore_SLO_CRUD_SLIStatus(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreateSLO("t1", nil); got != nil {
		t.Fatalf("CreateSLO(nil) = %+v, want nil", got)
	}

	slo := m.CreateSLO("t1", &SLO{Name: "api-avail", ServiceName: "api", Target: 99.9, Window: "30d",
		SLIs: []SLI{{Name: "availability", Metric: "up", Target: 99.9, Operator: ">="}}})
	if slo.ID == "" || !strings.HasPrefix(slo.ID, "slo-") {
		t.Fatalf("auto ID = %q, want slo- prefix", slo.ID)
	}
	if slo.CreatedAt.IsZero() || slo.UpdatedAt.IsZero() {
		t.Fatal("timestamps should be filled on create")
	}

	// SLIs 切片深拷贝隔离。
	slo.SLIs[0].Name = "MUTATED"
	g, ok := m.GetSLO("t1", slo.ID)
	if !ok || g.SLIs[0].Name != "availability" {
		t.Fatal("SLIs deep copy broken")
	}
	if _, ok := m.GetSLO("t2", slo.ID); ok {
		t.Fatal("cross-tenant get should fail")
	}
	if _, ok := m.GetSLO("t1", "nope"); ok {
		t.Fatal("unknown SLO get should fail")
	}

	// SLIStatus（更新前）：模拟值 met/99.5，TargetValue 取自 SLI.Target。
	st := m.SLIStatus("t1", slo.ID)
	if len(st) != 1 || st[0].SLIName != "availability" || st[0].Status != "met" ||
		st[0].CurrentValue != 99.5 || st[0].TargetValue != 99.9 {
		t.Fatalf("SLIStatus = %+v", st)
	}
	if st[0].LastEvaluated.IsZero() {
		t.Fatal("LastEvaluated should be filled")
	}
	if m.SLIStatus("t2", slo.ID) != nil {
		t.Fatal("cross-tenant SLIStatus should be nil")
	}
	if m.SLIStatus("t1", "nope") != nil {
		t.Fatal("unknown SLIStatus should be nil")
	}

	// UpdateSLO：整体替换语义——TenantID/CreatedAt 不可改；跨租户拒绝。
	u, ok := m.UpdateSLO("t1", &SLO{ID: slo.ID, Target: 99.95, TenantID: "evil",
		SLIs: []SLI{{Name: "availability", Metric: "up", Target: 99.95}}})
	if !ok || u.Target != 99.95 {
		t.Fatalf("update = (%+v,%v)", u, ok)
	}
	if u.TenantID != "t1" {
		t.Fatalf("TenantID overwritten to %q, want t1", u.TenantID)
	}
	if !u.CreatedAt.Equal(slo.CreatedAt) {
		t.Fatal("CreatedAt changed on update")
	}
	if _, ok := m.UpdateSLO("t2", &SLO{ID: slo.ID}); ok {
		t.Fatal("cross-tenant update should fail")
	}
	if _, ok := m.UpdateSLO("t1", &SLO{}); ok {
		t.Fatal("empty-ID update should fail")
	}

	// 更新后 SLIStatus 的 TargetValue 反映整体替换后的 SLI。
	st2 := m.SLIStatus("t1", slo.ID)
	if len(st2) != 1 || st2[0].TargetValue != 99.95 {
		t.Fatalf("SLIStatus after update = %+v, want TargetValue 99.95", st2)
	}

	// List：升序 + 租户过滤。
	m.CreateSLO("t1", &SLO{Name: "old-slo", CreatedAt: time.Now().Add(-time.Hour)})
	m.CreateSLO("t2", &SLO{Name: "foreign"})
	lst := m.ListSLOs("t1")
	if len(lst) != 2 {
		t.Fatalf("slos(t1) = %d, want 2", len(lst))
	}
	if lst[0].Name != "old-slo" { // 升序：最早在前
		t.Fatal("asc order broken")
	}
	for _, s := range lst { // 深拷贝副本
		s.Name = "MUTATED"
	}
	if g, _ := m.GetSLO("t1", slo.ID); g.Name == "MUTATED" {
		t.Fatal("ListSLOs deep copy broken")
	}

	// Delete：跨租户拒绝 → 成功 → 二次 false。
	if m.DeleteSLO("t2", slo.ID) || !m.DeleteSLO("t1", slo.ID) || m.DeleteSLO("t1", slo.ID) {
		t.Fatal("delete semantics broken")
	}
}

// ============================================================================
// 流量治理（memory_traffic.go）
// ============================================================================

func TestMemoryStore_TrafficPolicies_CRUD_EnableDisable(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreatePolicy("t1", nil); got != nil {
		t.Fatalf("CreatePolicy(nil) = %+v, want nil", got)
	}

	p := m.CreatePolicy("t1", &TrafficPolicy{Name: "canary-v2", ServiceName: "web", Type: "canary",
		CanaryWeights: map[string]int{"v1": 90, "v2": 10}})
	if p.ID == "" || !strings.HasPrefix(p.ID, "traffic-") {
		t.Fatalf("auto ID = %q, want traffic- prefix", p.ID)
	}
	if p.Status != "inactive" {
		t.Fatalf("default status = %q, want inactive", p.Status)
	}
	if p.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt should be filled on create")
	}

	// CanaryWeights map 深拷贝隔离。
	p.CanaryWeights["v2"] = 999
	g, ok := m.GetPolicy("t1", p.ID)
	if !ok || g.CanaryWeights["v2"] != 10 {
		t.Fatal("CanaryWeights deep copy broken")
	}
	if _, ok := m.GetPolicy("t2", p.ID); ok {
		t.Fatal("cross-tenant get should fail")
	}
	if _, ok := m.GetPolicy("t1", "nope"); ok {
		t.Fatal("unknown policy get should fail")
	}

	// Enable/Disable 正路径 + 错误路径。
	en, ok := m.EnablePolicy("t1", p.ID)
	if !ok || en.Status != "active" {
		t.Fatal("EnablePolicy failed")
	}
	dis, ok := m.DisablePolicy("t1", p.ID)
	if !ok || dis.Status != "inactive" {
		t.Fatal("DisablePolicy failed")
	}
	if _, ok := m.EnablePolicy("t2", p.ID); ok {
		t.Fatal("cross-tenant enable should fail")
	}
	if _, ok := m.DisablePolicy("t1", "nope"); ok {
		t.Fatal("unknown disable should fail")
	}

	m.CreatePolicy("t1", &TrafficPolicy{Name: "timeout-web", CreatedAt: time.Now().Add(-time.Hour)})
	lst := m.ListPolicies("t1")
	if len(lst) != 2 {
		t.Fatalf("policies(t1) = %d, want 2", len(lst))
	}
	if lst[0].Name != "canary-v2" { // 降序：最新在前
		t.Fatal("desc order broken")
	}
	if got := m.ListPolicies("t9"); len(got) != 0 {
		t.Fatalf("policies(t9) = %d, want 0", len(got))
	}

	// Update：CreatedAt/TenantID 保留；错误路径。
	u, ok := m.UpdatePolicy("t1", &TrafficPolicy{ID: p.ID, MirrorPercent: 5})
	if !ok || u.MirrorPercent != 5 {
		t.Fatalf("update = (%+v,%v)", u, ok)
	}
	if _, ok := m.UpdatePolicy("t2", &TrafficPolicy{ID: p.ID}); ok {
		t.Fatal("cross-tenant update should fail")
	}
	if _, ok := m.UpdatePolicy("t1", &TrafficPolicy{}); ok {
		t.Fatal("empty-ID update should fail")
	}

	if m.DeletePolicy("t2", p.ID) || !m.DeletePolicy("t1", p.ID) || m.DeletePolicy("t1", p.ID) {
		t.Fatal("delete semantics broken")
	}
}

// ============================================================================
// 自定义脚本（memory_script.go）——补 Get/Update/List/Delete 缺口
// ============================================================================

func TestMemoryStore_Script_CRUD_EnabledDefault(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreateScript("t1", nil); got != nil {
		t.Fatalf("CreateScript(nil) = %+v, want nil", got)
	}

	// 第四轮修复语义：Language 默认 shell；新建默认 Enabled=true（即使显式传 false）。
	s := m.CreateScript("t1", &Script{Name: "cleanup", Content: "echo hi", Enabled: false})
	if s.ID == "" || !strings.HasPrefix(s.ID, "script-") {
		t.Fatalf("auto ID = %q, want script- prefix", s.ID)
	}
	if s.Language != "shell" {
		t.Fatalf("Language default = %q, want shell", s.Language)
	}
	if !s.Enabled {
		t.Fatal("CreateScript should default Enabled=true（本轮修复语义）")
	}
	if s.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt should be filled on create")
	}

	// Get：正路径 + 跨租户 + 未知。
	g, ok := m.GetScript("t1", s.ID)
	if !ok || g.Content != "echo hi" {
		t.Fatalf("get = (%+v,%v)", g, ok)
	}
	if _, ok := m.GetScript("t2", s.ID); ok {
		t.Fatal("cross-tenant get should fail")
	}
	if _, ok := m.GetScript("t1", "nope"); ok {
		t.Fatal("unknown script get should fail")
	}

	// Update：禁用须走 UpdateScript；CreatedAt/TenantID 保留。
	u, ok := m.UpdateScript("t1", &Script{ID: s.ID, Name: "cleanup-v2", Enabled: false, TimeoutSec: 30})
	if !ok || u.Name != "cleanup-v2" || u.Enabled {
		t.Fatalf("update = (%+v,%v) enabled=%v", u, ok, u != nil && u.Enabled)
	}
	if !u.CreatedAt.Equal(s.CreatedAt) {
		t.Fatal("CreatedAt changed on update")
	}
	if _, ok := m.UpdateScript("t2", &Script{ID: s.ID}); ok {
		t.Fatal("cross-tenant update should fail")
	}
	if _, ok := m.UpdateScript("t1", &Script{}); ok {
		t.Fatal("empty-ID update should fail")
	}

	// List：降序 + 租户过滤。
	m.CreateScript("t1", &Script{Name: "older", CreatedAt: time.Now().Add(-time.Hour), Enabled: true})
	lst := m.ListScripts("t1")
	if len(lst) != 2 || lst[0].Name != "cleanup-v2" { // 降序：最新在前
		t.Fatalf("list len=%d first=%s", len(lst), lst[0].Name)
	}
	if got := m.ListScripts("t9"); len(got) != 0 {
		t.Fatalf("scripts(t9) = %d, want 0", len(got))
	}

	// 执行记录：FinishedAt 指针深拷贝；归属校验；零值 StartedAt 填充。
	base := time.Now().Add(-time.Hour)
	ft := base.Add(time.Minute)
	rec := m.RecordScriptExecution("t1", s.ID, "dev-1", "succeeded", "out", "", base, &ft)
	if rec.ID == "" || !strings.HasPrefix(rec.ID, "script-exec-") {
		t.Fatalf("execution ID = %q", rec.ID)
	}
	execs := m.ListScriptExecutions("t1", s.ID)
	if len(execs) != 1 || execs[0].Stdout != "out" {
		t.Fatalf("executions = %+v", execs)
	}
	if execs[0].FinishedAt == nil || !execs[0].FinishedAt.Equal(ft) {
		t.Fatal("FinishedAt lost on read")
	}
	// 读回副本的 FinishedAt 指针被改不应影响下一次读取（cloneScriptExecution 分支）。
	*execs[0].FinishedAt = ft.Add(time.Hour)
	execs2 := m.ListScriptExecutions("t1", s.ID)
	if execs2[0].FinishedAt == nil || execs2[0].FinishedAt.Equal(ft.Add(time.Hour)) {
		t.Fatal("FinishedAt deep copy broken")
	}
	if got := m.ListScriptExecutions("t1", "no-such-script"); len(got) != 0 {
		t.Fatal("executions for unknown script should be empty")
	}
	if got := m.ListScriptExecutions("t2", s.ID); len(got) != 0 {
		t.Fatal("cross-tenant executions should be empty")
	}
	z := m.RecordScriptExecution("t1", s.ID, "dev-1", "running", "", "", time.Time{}, nil)
	if z.StartedAt.IsZero() {
		t.Fatal("zero StartedAt should be filled with now")
	}

	// Delete：跨租户拒绝 → 成功 → 二次 false。
	if m.DeleteScript("t2", s.ID) || !m.DeleteScript("t1", s.ID) || m.DeleteScript("t1", s.ID) {
		t.Fatal("delete semantics broken")
	}
}

// ============================================================================
// 租户管理（memory_tenant.go）——补 Update/List/Delete 缺口
// ============================================================================

func TestMemoryStore_Tenant_UpdateListDelete(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreateTenant(nil); got != nil {
		t.Fatalf("CreateTenant(nil) = %+v, want nil", got)
	}

	old := time.Now().Add(-2 * time.Hour)
	a := m.CreateTenant(&Tenant{Name: "acme", DisplayName: "Acme Inc",
		Quota: TenantQuota{MaxDevices: 100}, CreatedAt: old})
	if a.ID == "" || !strings.HasPrefix(a.ID, "tenant-") {
		t.Fatalf("auto ID = %q, want tenant- prefix", a.ID)
	}
	if a.Status != TenantStatusActive {
		t.Fatalf("default status = %q, want active", a.Status)
	}
	b := m.CreateTenant(&Tenant{Name: "globex", CreatedAt: old.Add(-time.Hour)}) // 更早 → 升序第一

	// Update：DisplayName/Quota 可改，CreatedAt 保留；错误路径。
	u, ok := m.UpdateTenant(&Tenant{ID: a.ID, DisplayName: "Acme Corp",
		Status: TenantStatusSuspended, Quota: TenantQuota{MaxDevices: 200}})
	if !ok || u.DisplayName != "Acme Corp" || u.Status != TenantStatusSuspended || u.Quota.MaxDevices != 200 {
		t.Fatalf("update = (%+v,%v)", u, ok)
	}
	if !u.CreatedAt.Equal(old) {
		t.Fatal("CreatedAt changed on update")
	}
	if _, ok := m.UpdateTenant(&Tenant{}); ok {
		t.Fatal("empty-ID update should fail")
	}
	if _, ok := m.UpdateTenant(&Tenant{ID: "ghost"}); ok {
		t.Fatal("unknown tenant update should fail")
	}

	// List 全部升序（最早在前）+ Get。
	all := m.ListTenants()
	if len(all) != 2 {
		t.Fatalf("tenants = %d, want 2", len(all))
	}
	if all[0].ID != b.ID {
		t.Fatal("asc order broken (oldest first)")
	}
	if g, ok := m.GetTenant(a.ID); !ok || g.Status != TenantStatusSuspended {
		t.Fatalf("get after update = (%+v,%v)", g, ok)
	}
	if _, ok := m.GetTenant("ghost"); ok {
		t.Fatal("unknown tenant get should fail")
	}

	// Delete：成功后二次 false；数量回落。
	if !m.DeleteTenant(a.ID) || m.DeleteTenant(a.ID) {
		t.Fatal("delete semantics broken")
	}
	if got := m.ListTenants(); len(got) != 1 {
		t.Fatalf("after delete tenants = %d, want 1", len(got))
	}
}

// ============================================================================
// 工单管理（memory_ticket.go）——补 Update/List(filter)/Close 缺口
// ============================================================================

func TestMemoryStore_Ticket_UpdateFilterClose(t *testing.T) {
	m := NewMemoryStore()
	old := time.Now().Add(-2 * time.Hour)
	tk := m.CreateTicket("t1", &Ticket{Title: "disk full", Priority: "high", Category: "incident",
		CreatorID: "u1", AssigneeID: "u2", Tags: []string{"storage"}, CreatedAt: old})
	if tk.ID == "" || tk.Status != "open" {
		t.Fatalf("create defaults = %+v", tk)
	}

	// cloneTicket 的 Tags 深拷贝分支。
	tk.Tags[0] = "MUTATED"
	g, _ := m.GetTicket("t1", tk.ID)
	if g.Tags[0] != "storage" {
		t.Fatal("Tags deep copy broken")
	}

	// List：filter 各字段过滤 + 租户隔离 + 降序。
	m.CreateTicket("t1", &Ticket{Title: "cpu high", Priority: "urgent", Category: "incident",
		AssigneeID: "u2", CreatedAt: old.Add(-time.Hour)})
	m.CreateTicket("t2", &Ticket{Title: "foreign"})
	if got := m.ListTickets("t1", TicketFilter{}); len(got) != 2 {
		t.Fatalf("list(t1) = %d, want 2", len(got))
	}
	lst := m.ListTickets("t1", TicketFilter{})
	if lst[0].Title != "disk full" { // 降序：最新在前
		t.Fatalf("desc order broken: first=%s", lst[0].Title)
	}
	if got := m.ListTickets("t1", TicketFilter{Priority: "urgent"}); len(got) != 1 || got[0].Title != "cpu high" {
		t.Fatalf("priority filter = %+v", got)
	}
	if got := m.ListTickets("t1", TicketFilter{AssigneeID: "u2"}); len(got) != 2 {
		t.Fatalf("assignee filter = %d, want 2", len(got))
	}
	if got := m.ListTickets("t1", TicketFilter{Status: "resolved"}); len(got) != 0 {
		t.Fatalf("status filter should be empty, got %d", len(got))
	}
	if got := m.ListTickets("", TicketFilter{}); len(got) != 3 {
		t.Fatalf("all tenants = %d, want 3", len(got))
	}

	// Update：Status 可改、CreatedAt 保留；错误路径。
	u, ok := m.UpdateTicket("t1", &Ticket{ID: tk.ID, Status: "in_progress", AssigneeID: "u3"})
	if !ok || u.Status != "in_progress" || u.AssigneeID != "u3" {
		t.Fatalf("update = (%+v,%v)", u, ok)
	}
	if !u.CreatedAt.Equal(old) {
		t.Fatal("CreatedAt changed on update")
	}
	if _, ok := m.UpdateTicket("t2", &Ticket{ID: tk.ID}); ok {
		t.Fatal("cross-tenant update should fail")
	}
	if _, ok := m.UpdateTicket("t1", &Ticket{}); ok {
		t.Fatal("empty-ID update should fail")
	}

	// Close：closed + ResolvedAt 非nil；跨租户与未知拒绝。
	c, ok := m.CloseTicket("t1", tk.ID)
	if !ok || c.Status != "closed" || c.ResolvedAt == nil {
		t.Fatalf("close = (%+v,%v)", c, ok)
	}
	if _, ok := m.CloseTicket("t2", tk.ID); ok {
		t.Fatal("cross-tenant close should fail")
	}
	if _, ok := m.CloseTicket("t1", "nope"); ok {
		t.Fatal("close unknown ticket should fail")
	}
}

// ============================================================================
// 配置中心（memory_config.go）——补 DeleteConfig 与版本链路缺口
// ============================================================================

func TestMemoryStore_Config_DeleteWithHistory(t *testing.T) {
	m := NewMemoryStore()
	if m.SetConfig(nil) != nil {
		t.Fatal("SetConfig(nil) should return nil")
	}
	if _, ok := m.GetConfig("t1", "nope"); ok {
		t.Fatal("unknown config get should fail")
	}

	// 版本链：新建 v1 → 更新 v2，前版本进历史。
	v1 := m.SetConfig(&ConfigItem{TenantID: "t1", Key: "app/db/pool", Value: "10", Format: "text"})
	if v1.Version != 1 {
		t.Fatalf("first version = %d, want 1", v1.Version)
	}
	v2 := m.SetConfig(&ConfigItem{TenantID: "t1", Key: "app/db/pool", Value: "20", UpdatedBy: "u1"})
	if v2.Version != 2 || v2.Value != "20" {
		t.Fatalf("second version = %+v", v2)
	}
	hist := m.ConfigHistory("t1", "app/db/pool")
	if len(hist) != 1 || hist[0].Value != "10" {
		t.Fatalf("history = %+v, want [v1]", hist)
	}
	if hist2 := m.ConfigHistory("t1", "nope"); hist2 != nil {
		t.Fatalf("history of unknown key = %+v, want nil", hist2)
	}

	// PublishConfig：正路径 + 未知 key。
	pub, ok := m.PublishConfig("t1", "app/db/pool")
	if !ok || pub.Value != "20" {
		t.Fatalf("publish = (%+v,%v)", pub, ok)
	}
	if _, ok := m.PublishConfig("t1", "nope"); ok {
		t.Fatal("publish unknown config should fail")
	}

	// DeleteConfig：连同历史一并清除；幂等 false。
	if !m.DeleteConfig("t1", "app/db/pool") {
		t.Fatal("delete should succeed")
	}
	if m.DeleteConfig("t1", "app/db/pool") {
		t.Fatal("double delete should fail")
	}
	if _, ok := m.GetConfig("t1", "app/db/pool"); ok {
		t.Fatal("deleted config still readable")
	}
	if histAfter := m.ConfigHistory("t1", "app/db/pool"); histAfter != nil {
		t.Fatalf("history not cleared on delete: %+v", histAfter)
	}
}

// ============================================================================
// 密钥管理（memory_secret.go）——补 Rotate/Delete/Versions 缺口
// ============================================================================

func TestMemoryStore_Secret_RotateDeleteVersions(t *testing.T) {
	m := NewMemoryStore()
	if m.SetSecret(nil, "t1") != nil {
		t.Fatal("SetSecret(nil) should return nil")
	}
	if _, ok := m.GetSecret("t1", "nope"); ok {
		t.Fatal("unknown secret get should fail")
	}

	meta1 := m.SetSecret(&SecretItem{Key: "app/token", Value: "v1"}, "t1")
	if meta1.Version != 1 || meta1.KeyType != "passphrase" {
		t.Fatalf("meta1 = %+v, want version=1 passphrase", meta1)
	}

	// Rotate：沿用已有 KeyType 并产生新版本。
	meta2 := m.RotateSecret("t1", "app/token", "v2")
	if meta2.Version != 2 || meta2.KeyType != "passphrase" {
		t.Fatalf("rotated meta = %+v, want version=2 passphrase", meta2)
	}
	got, ok := m.GetSecret("t1", "app/token")
	if !ok || got.Value != "v2" || got.KeyType != "passphrase" {
		t.Fatalf("get after rotate = (%+v,%v)", got, ok)
	}

	// Rotate 不存在的密钥 → 等价 SetSecret（KeyType 默认 passphrase）。
	fresh := m.RotateSecret("t1", "app/new", "x")
	if fresh.Version != 1 || fresh.KeyType != "passphrase" {
		t.Fatalf("fresh rotate = %+v", fresh)
	}

	versions := m.SecretVersions("t1", "app/token")
	if len(versions) != 2 || versions[0].Version != 1 || versions[1].Version != 2 {
		t.Fatalf("versions = %+v, want [v1,v2] 升序", versions)
	}
	if vs := m.SecretVersions("t1", "nope"); vs != nil {
		t.Fatal("versions of unknown key should be nil")
	}

	// List 脱敏元信息按 key 升序。
	lst := m.ListSecrets("t1")
	if len(lst) != 2 || lst[0].Key != "app/new" {
		t.Fatalf("list secrets = %+v, want [app/new, app/token]", lst)
	}
	for _, mt := range lst {
		if mt.TenantID != "t1" {
			t.Fatal("tenant leak in ListSecrets")
		}
	}

	// Delete：清当前值 + 元信息 + 全部历史版本；幂等 false。
	if !m.DeleteSecret("t1", "app/token") {
		t.Fatal("delete secret failed")
	}
	if m.DeleteSecret("t1", "app/token") {
		t.Fatal("double delete should fail")
	}
	if _, ok := m.GetSecret("t1", "app/token"); ok {
		t.Fatal("deleted secret readable")
	}
	if vs := m.SecretVersions("t1", "app/token"); vs != nil {
		t.Fatal("versions not cleared on delete")
	}
}

// ============================================================================
// 服务发现（memory_discovery.go）——补 Deregister/查询/过期缺口
// ============================================================================

func TestMemoryStore_ServiceDiscovery_DeregisterAndQuery(t *testing.T) {
	m := NewMemoryStore()
	if m.RegisterService(nil) != nil {
		t.Fatal("RegisterService(nil) should return nil")
	}

	inst := m.RegisterService(&ServiceInstance{ServiceID: "svc-1", ServiceName: "orders",
		Address: "10.0.0.5", Port: 8080, Metadata: map[string]string{"az": "a"}, TenantID: "t1"})
	if inst.Status != "healthy" || inst.CreatedAt.IsZero() || inst.LastHeartbeat.IsZero() {
		t.Fatalf("defaults missing: %+v", inst)
	}

	// Metadata 深拷贝：修改副本不影响内部（第四轮修复的 clone-on-read 语义）。
	inst.Metadata["az"] = "MUTATED"
	insts := m.ServiceInstances("t1", "orders")
	if len(insts) != 1 || insts[0].Metadata["az"] != "a" {
		t.Fatalf("metadata deep copy broken: %+v", insts)
	}

	// 同名多实例按 ServiceID 升序稳定输出（覆盖 sortServiceInstances 分支）。
	m.RegisterService(&ServiceInstance{ServiceID: "svc-0", ServiceName: "orders",
		Address: "10.0.0.4", TenantID: "t1"})
	m.RegisterService(&ServiceInstance{ServiceID: "svc-9", ServiceName: "billing",
		Address: "10.0.0.6", TenantID: "t2"})
	ordered := m.ServiceInstances("", "orders")
	if len(ordered) != 2 || ordered[0].ServiceID != "svc-0" || ordered[1].ServiceID != "svc-1" {
		t.Fatalf("sort broken: %s,%s", ordered[0].ServiceID, ordered[1].ServiceID)
	}
	if got := m.AllServices("t1"); len(got) != 2 {
		t.Fatalf("AllServices(t1) = %d, want 2", len(got))
	}
	if got := m.AllServices(""); len(got) != 3 {
		t.Fatalf("AllServices(all) = %d, want 3", len(got))
	}

	// 心跳：未知实例 / 跨租户拒绝；旧副本不被原地更新。
	if m.HeartbeatService("t1", "nope", "") {
		t.Fatal("heartbeat unknown instance should fail")
	}
	if m.HeartbeatService("t2", "svc-1", "degraded") {
		t.Fatal("cross-tenant heartbeat should fail")
	}
	if !m.HeartbeatService("t1", "svc-1", "degraded") {
		t.Fatal("heartbeat should succeed")
	}
	if insts[0].Status == "degraded" {
		t.Fatal("stale copy reflected live status (clone-on-read broken)")
	}
	fresh := m.ServiceInstances("t1", "orders")
	if fresh[1].Status != "degraded" {
		t.Fatalf("status after heartbeat = %q, want degraded", fresh[1].Status)
	}

	// StaleServices：maxAge<=0 → nil；超龄实例被检出。
	if got := m.StaleServices("t1", 0); got != nil {
		t.Fatalf("StaleServices(maxAge=0) = %+v, want nil", got)
	}
	m.mu.Lock()
	m.services["svc-1"].LastHeartbeat = time.Now().Add(-2 * time.Hour)
	m.mu.Unlock()
	stale := m.StaleServices("t1", time.Hour)
	if len(stale) != 1 || stale[0].ServiceID != "svc-1" {
		t.Fatalf("stale = %+v, want [svc-1]", stale)
	}

	// Deregister：跨租户拒绝 → 成功 → 二次 false。
	if m.DeregisterService("t2", "svc-1") {
		t.Fatal("cross-tenant deregister should fail")
	}
	if !m.DeregisterService("t1", "svc-1") {
		t.Fatal("deregister should succeed")
	}
	if m.DeregisterService("t1", "svc-1") {
		t.Fatal("double deregister should fail")
	}
	if m.DeregisterService("t1", "nope") {
		t.Fatal("deregister unknown service should fail")
	}
}

// ============================================================================
// Webhook（memory_webhook.go）——补 Get/Update/List/Delete/Deliveries 缺口
// ============================================================================

func TestMemoryStore_Webhook_UpdateDeleteDeliveries(t *testing.T) {
	m := NewMemoryStore()
	if got := m.CreateWebhook("t1", nil); got != nil {
		t.Fatalf("CreateWebhook(nil) = %+v, want nil", got)
	}

	wh := m.CreateWebhook("t1", &Webhook{Name: "ops-hook", URL: "https://hooks.example.com/x",
		Events: []string{"alert.created"}, Headers: map[string]string{"X-Env": "prod"}})
	if wh.ID == "" || !strings.HasPrefix(wh.ID, "webhook-") {
		t.Fatalf("auto ID = %q, want webhook- prefix", wh.ID)
	}
	if wh.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt should be filled on create")
	}

	// Events/Headers 深拷贝（cloneWebhook 分支）。
	wh.Events[0] = "MUTATED"
	wh.Headers["X-Env"] = "MUTATED"
	g, _ := m.GetWebhook("t1", wh.ID)
	if g.Events[0] != "alert.created" || g.Headers["X-Env"] != "prod" {
		t.Fatal("Events/Headers deep copy broken")
	}
	if _, ok := m.GetWebhook("t2", wh.ID); ok {
		t.Fatal("cross-tenant get should fail")
	}
	if _, ok := m.GetWebhook("t1", "nope"); ok {
		t.Fatal("unknown webhook get should fail")
	}

	// List：降序 + 租户过滤。
	m.CreateWebhook("t1", &Webhook{Name: "older-hook", CreatedAt: time.Now().Add(-time.Hour)})
	m.CreateWebhook("t2", &Webhook{Name: "foreign-hook"})
	lst := m.ListWebhooks("t1")
	if len(lst) != 2 || lst[0].Name != "ops-hook" { // 降序：最新在前
		t.Fatalf("list len=%d first=%s", len(lst), lst[0].Name)
	}
	if got := m.ListWebhooks("t9"); len(got) != 0 {
		t.Fatalf("webhooks(t9) = %d, want 0", len(got))
	}

	// 投递记录：记录 → 归属校验 → 降序。
	d1 := m.RecordWebhookDelivery("t1", wh.ID, "alert.created", "{}", 200, "ok", "")
	if d1.ID == "" || !strings.HasPrefix(d1.ID, "wh-delivery-") || d1.DeliveredAt.IsZero() {
		t.Fatalf("delivery = %+v", d1)
	}
	time.Sleep(5 * time.Millisecond) // 保证 DeliveredAt 可分辨
	m.RecordWebhookDelivery("t1", wh.ID, "alert.created", "{}", 500, "", "boom")
	ds := m.ListWebhookDeliveries("t1", wh.ID)
	if len(ds) != 2 || ds[0].StatusCode != 500 { // 降序：最新在前
		t.Fatalf("deliveries = %+v", ds)
	}
	if got := m.ListWebhookDeliveries("t1", "no-such-hook"); len(got) != 0 {
		t.Fatal("deliveries for unknown hook should be empty")
	}
	if got := m.ListWebhookDeliveries("t2", wh.ID); len(got) != 0 {
		t.Fatal("cross-tenant deliveries should be empty")
	}

	// Update：TenantID/CreatedAt 保留；错误路径。
	u, ok := m.UpdateWebhook("t1", &Webhook{ID: wh.ID, URL: "https://hooks.example.com/y", Enabled: true})
	if !ok || u.URL != "https://hooks.example.com/y" || !u.Enabled {
		t.Fatalf("update = (%+v,%v)", u, ok)
	}
	if u.TenantID != "t1" {
		t.Fatalf("TenantID overwritten to %q, want t1", u.TenantID)
	}
	if _, ok := m.UpdateWebhook("t2", &Webhook{ID: wh.ID}); ok {
		t.Fatal("cross-tenant update should fail")
	}
	if _, ok := m.UpdateWebhook("t1", &Webhook{}); ok {
		t.Fatal("empty-ID update should fail")
	}

	// Delete：跨租户拒绝 → 成功 → 二次 false。
	if m.DeleteWebhook("t2", wh.ID) || !m.DeleteWebhook("t1", wh.ID) || m.DeleteWebhook("t1", wh.ID) {
		t.Fatal("delete semantics broken")
	}
}

// ============================================================================
// 私有辅助函数直测（同包白盒）
// ============================================================================

// TestMemoryCloneServiceInstances_Helper 覆盖批量 clone 辅助函数：
// nil 入参 / 空切片返回 nil；正常切片逐元素深拷贝（Metadata map 隔离）。
func TestMemoryCloneServiceInstances_Helper(t *testing.T) {
	if got := cloneServiceInstances(nil); got != nil {
		t.Fatalf("cloneServiceInstances(nil) = %+v, want nil", got)
	}
	if got := cloneServiceInstances([]*ServiceInstance{}); got != nil {
		t.Fatalf("cloneServiceInstances(empty) = %+v, want nil", got)
	}
	if got := cloneServiceInstance(nil); got != nil {
		t.Fatalf("cloneServiceInstance(nil) = %+v, want nil", got)
	}
	if got := cloneServiceInstance(&ServiceInstance{ServiceID: "s"}); got == nil || got.ServiceID != "s" {
		t.Fatalf("cloneServiceInstance(single) = %+v", got)
	}

	src := []*ServiceInstance{
		{ServiceID: "svc-b", Metadata: map[string]string{"k": "v"}},
		nil, // nil 元素应被拷贝为 nil 指针，不得 panic
		{ServiceID: "svc-a"},
	}
	cp := cloneServiceInstances(src)
	if len(cp) != 3 {
		t.Fatalf("len = %d, want 3", len(cp))
	}
	if cp[0] == src[0] {
		t.Fatal("shallow copy detected (pointer shared)")
	}
	cp[0].Metadata["k"] = "MUTATED"
	if src[0].Metadata["k"] != "v" {
		t.Fatal("Metadata deep copy broken")
	}
	if cp[1] != nil {
		t.Fatalf("nil element = %+v, want nil", cp[1])
	}
	if cp[2] == src[2] {
		t.Fatal("element pointer shared")
	}
}

// TestStubGuard_JoinAndWarnDomains 覆盖 stub_guard.go 的展示辅助：
// joinStubDomains 输出格式、WarnStubStoreDomains 不 panic、StubDomains 完整性。
// 现状：P1-P6 全部 15 个领域已实现 MySQL 持久化，StubDomains 收敛为空，
// joinStubDomains 返回空串、WarnStubStoreDomains 静默返回（不再告警）。
func TestStubGuard_JoinAndWarnDomains(t *testing.T) {
	got := joinStubDomains()
	want := ""
	if got != want {
		t.Fatalf("joinStubDomains() = %q, want %q", got, want)
	}
	if len(StubDomains) != 0 {
		t.Fatalf("StubDomains count = %d, want 0 (P1-P6 全部已持久化)", len(StubDomains))
	}
	// 构造函数接线告警：StubDomains 为空时静默返回，不应 panic 也不应告警。
	WarnStubStoreDomains("multi-schema-test")
	// StubNotImplemented 限频：同 key 连续两次调用不 panic，且第二次走窗口内静默分支。
	// 保留调用以覆盖限频逻辑（StubNotImplemented 本身与 StubDomains 解耦，仍可用）。
	StubNotImplemented("test-domain", "TestMethod")
	StubNotImplemented("test-domain", "TestMethod")
}

// ============================================================================
// MultiSchemaStore 委托层 smoke（multi_schema_p03/p1~p6.go）
// ============================================================================

// memoryStoreFactory 每租户 schema 返回独立 MemoryStore 的工厂。
// 用于以内存后端驱动 MultiSchemaStore 的委托路由层。
func memoryStoreFactory(schema string) (Store, error) {
	return NewMemoryStore(), nil
}

// newMultiSchemaWithMemory 以内存工厂构造 MultiSchemaStore（租户名须过 namer 校验：
// 仅 [a-zA-Z0-9_]，故测试用 "tenanta"/"tenantb" 这类标识）。
func newMultiSchemaWithMemory() *MultiSchemaStore {
	return newMultiSchemaWithFactory(DefaultSchemaNamer("opsmesh_tenant_"), memoryStoreFactory)
}

// TestMultiSchemaStore_DelegationP03ToP6 以一轮正路径 smoke 覆盖 P0.3-P6 各域
// 委托方法的完整函数体（storeFor 路由 + 底层转发），并验证跨租户隔离与
// 空租户聚合分支。断言保持最小化——委托层只负责转发，业务语义已由
// Memory 层测试覆盖；此处重点验证"路由可达且返回值透传"。
func TestMultiSchemaStore_DelegationP03ToP6(t *testing.T) {
	m := newMultiSchemaWithMemory()
	ten := "tenanta"

	// ---- P0.3 服务发现 ----
	inst := m.RegisterService(&ServiceInstance{ServiceID: "svc-1", ServiceName: "orders",
		Address: "10.0.0.5", TenantID: ten})
	if inst == nil || inst.Status != "healthy" {
		t.Fatalf("RegisterService delegate = %+v", inst)
	}
	m.RegisterService(&ServiceInstance{ServiceID: "svc-2", ServiceName: "orders",
		Address: "10.0.0.6", TenantID: ten})
	if got := m.ServiceInstances(ten, "orders"); len(got) != 2 || got[0].ServiceID != "svc-1" {
		t.Fatalf("ServiceInstances delegate = %d", len(got))
	}
	if got := m.AllServices(ten); len(got) != 2 {
		t.Fatalf("AllServices delegate = %d, want 2", len(got))
	}
	if !m.HeartbeatService(ten, "svc-1", "degraded") {
		t.Fatal("HeartbeatService delegate failed")
	}
	for _, s := range m.allStores() {
		ms := s.(*MemoryStore)
		ms.mu.Lock()
		for _, si := range ms.services {
			si.LastHeartbeat = time.Now().Add(-2 * time.Hour)
		}
		ms.mu.Unlock()
	}
	stale := m.StaleServices(ten, time.Hour)
	if len(stale) != 2 {
		t.Fatalf("StaleServices delegate = %d, want 2", len(stale))
	}
	if !m.DeregisterService(ten, "svc-2") || m.DeregisterService(ten, "svc-2") {
		t.Fatal("DeregisterService delegate broken")
	}

	// ---- P0.3 配置中心 ----
	v1 := m.SetConfig(&ConfigItem{TenantID: ten, Key: "app/x", Value: "1"})
	if v1 == nil || v1.Version != 1 {
		t.Fatalf("SetConfig delegate = %+v", v1)
	}
	m.SetConfig(&ConfigItem{TenantID: ten, Key: "app/x", Value: "2"})
	got, ok := m.GetConfig(ten, "app/x")
	if !ok || got.Value != "2" {
		t.Fatalf("GetConfig delegate = (%+v,%v)", got, ok)
	}
	if lst := m.ListConfigs(ten); len(lst) != 1 {
		t.Fatalf("ListConfigs delegate = %d", len(lst))
	}
	if hist := m.ConfigHistory(ten, "app/x"); len(hist) != 1 || hist[0].Value != "1" {
		t.Fatalf("ConfigHistory delegate = %+v", hist)
	}
	if pub, ok := m.PublishConfig(ten, "app/x"); !ok || pub.Value != "2" {
		t.Fatalf("PublishConfig delegate = (%+v,%v)", pub, ok)
	}
	if !m.DeleteConfig(ten, "app/x") || m.DeleteConfig(ten, "app/x") {
		t.Fatal("DeleteConfig delegate broken")
	}

	// ---- P0.3 密钥管理 ----
	meta1 := m.SetSecret(&SecretItem{Key: "k1", Value: "v1"}, ten)
	if meta1 == nil || meta1.Version != 1 {
		t.Fatalf("SetSecret delegate = %+v", meta1)
	}
	meta2 := m.RotateSecret(ten, "k1", "v2")
	if meta2 == nil || meta2.Version != 2 {
		t.Fatalf("RotateSecret delegate = %+v", meta2)
	}
	if item, ok := m.GetSecret(ten, "k1"); !ok || item.Value != "v2" {
		t.Fatalf("GetSecret delegate = (%+v,%v)", item, ok)
	}
	if lst := m.ListSecrets(ten); len(lst) != 1 {
		t.Fatalf("ListSecrets delegate = %d", len(lst))
	}
	if vs := m.SecretVersions(ten, "k1"); len(vs) != 2 {
		t.Fatalf("SecretVersions delegate = %d, want 2", len(vs))
	}
	if !m.DeleteSecret(ten, "k1") || m.DeleteSecret(ten, "k1") {
		t.Fatal("DeleteSecret delegate broken")
	}

	// ---- P1 工单 ----
	tk := m.CreateTicket(ten, &Ticket{Title: "t1-ticket"})
	if tk == nil || tk.ID == "" || tk.Status != "open" {
		t.Fatalf("CreateTicket delegate = %+v", tk)
	}
	if g, ok := m.GetTicket(ten, tk.ID); !ok || g.Title != "t1-ticket" {
		t.Fatalf("GetTicket delegate = (%+v,%v)", g, ok)
	}
	if u, ok := m.UpdateTicket(ten, &Ticket{ID: tk.ID, Status: "in_progress"}); !ok || u.Status != "in_progress" {
		t.Fatalf("UpdateTicket delegate = (%+v,%v)", u, ok)
	}
	if lst := m.ListTickets(ten, TicketFilter{}); len(lst) != 1 {
		t.Fatalf("ListTickets delegate = %d", len(lst))
	}
	if c, ok := m.CloseTicket(ten, tk.ID); !ok || c.Status != "closed" {
		t.Fatalf("CloseTicket delegate = (%+v,%v)", c, ok)
	}

	// ---- P1 SLO ----
	slo := m.CreateSLO(ten, &SLO{Name: "slo-1", SLIs: []SLI{{Name: "avail", Target: 99.9}}})
	if slo == nil || slo.ID == "" {
		t.Fatalf("CreateSLO delegate = %+v", slo)
	}
	if g, ok := m.GetSLO(ten, slo.ID); !ok || g.Name != "slo-1" {
		t.Fatalf("GetSLO delegate = (%+v,%v)", g, ok)
	}
	if u, ok := m.UpdateSLO(ten, &SLO{ID: slo.ID, Name: "slo-2"}); !ok || u.Name != "slo-2" {
		t.Fatalf("UpdateSLO delegate = (%+v,%v)", u, ok)
	}
	if lst := m.ListSLOs(ten); len(lst) != 1 {
		t.Fatalf("ListSLOs delegate = %d", len(lst))
	}
	if st := m.SLIStatus(ten, slo.ID); st == nil {
		t.Fatal("SLIStatus delegate returned nil")
	}
	if !m.DeleteSLO(ten, slo.ID) || m.DeleteSLO(ten, slo.ID) {
		t.Fatal("DeleteSLO delegate broken")
	}

	// ---- P2 流量治理 ----
	pol := m.CreatePolicy(ten, &TrafficPolicy{Name: "canary"})
	if pol == nil || pol.ID == "" || pol.Status != "inactive" {
		t.Fatalf("CreatePolicy delegate = %+v", pol)
	}
	if g, ok := m.GetPolicy(ten, pol.ID); !ok || g.Name != "canary" {
		t.Fatalf("GetPolicy delegate = (%+v,%v)", g, ok)
	}
	if u, ok := m.UpdatePolicy(ten, &TrafficPolicy{ID: pol.ID, MirrorPercent: 10}); !ok || u.MirrorPercent != 10 {
		t.Fatalf("UpdatePolicy delegate = (%+v,%v)", u, ok)
	}
	if lst := m.ListPolicies(ten); len(lst) != 1 {
		t.Fatalf("ListPolicies delegate = %d", len(lst))
	}
	if en, ok := m.EnablePolicy(ten, pol.ID); !ok || en.Status != "active" {
		t.Fatalf("EnablePolicy delegate = (%+v,%v)", en, ok)
	}
	if dis, ok := m.DisablePolicy(ten, pol.ID); !ok || dis.Status != "inactive" {
		t.Fatalf("DisablePolicy delegate = (%+v,%v)", dis, ok)
	}
	if !m.DeletePolicy(ten, pol.ID) || m.DeletePolicy(ten, pol.ID) {
		t.Fatal("DeletePolicy delegate broken")
	}

	// ---- P2 流水线 ----
	tpl := m.CreateTemplate(ten, &PipelineTemplate{Name: "build"})
	if tpl == nil || tpl.ID == "" {
		t.Fatalf("CreateTemplate delegate = %+v", tpl)
	}
	if g, ok := m.GetTemplate(ten, tpl.ID); !ok || g.Name != "build" {
		t.Fatalf("GetTemplate delegate = (%+v,%v)", g, ok)
	}
	if lst := m.ListTemplates(ten); len(lst) != 1 {
		t.Fatalf("ListTemplates delegate = %d", len(lst))
	}
	run := m.CreateRun(ten, &PipelineRun{TemplateID: tpl.ID})
	if run == nil || run.ID == "" || run.Status != "pending" {
		t.Fatalf("CreateRun delegate = %+v", run)
	}
	started := time.Now()
	if u, ok := m.UpdateRun(ten, &PipelineRun{ID: run.ID, Status: "running", StartedAt: &started}); !ok || u.Status != "running" {
		t.Fatalf("UpdateRun delegate = (%+v,%v)", u, ok)
	}
	if got := m.ListRuns(ten, tpl.ID); len(got) != 1 {
		t.Fatalf("ListRuns delegate = %d", len(got))
	}
	if _, ok := m.GetRun(ten, run.ID); !ok {
		t.Fatal("GetRun delegate miss")
	}
	if !m.DeleteTemplate(ten, tpl.ID) || m.DeleteTemplate(ten, tpl.ID) {
		t.Fatal("DeleteTemplate delegate broken")
	}

	// ---- P2 ArgoCD ----
	app := m.CreateApp(ten, &ArgoCDApp{Name: "gb"})
	if app == nil || app.ID == "" || app.Status != "unknown" {
		t.Fatalf("CreateApp delegate = %+v", app)
	}
	if g, ok := m.GetApp(ten, app.ID); !ok || g.Name != "gb" {
		t.Fatalf("GetApp delegate = (%+v,%v)", g, ok)
	}
	if u, ok := m.UpdateApp(ten, &ArgoCDApp{ID: app.ID, RepoURL: "https://x"}); !ok {
		t.Fatalf("UpdateApp delegate = (%+v,%v)", u, ok)
	}
	if lst := m.ListApps(ten); len(lst) != 1 {
		t.Fatalf("ListApps delegate = %d", len(lst))
	}
	if s, ok := m.SyncApp(ten, app.ID); !ok || s.Status != "synced" {
		t.Fatalf("SyncApp delegate = (%+v,%v)", s, ok)
	}
	if !m.DeleteApp(ten, app.ID) || m.DeleteApp(ten, app.ID) {
		t.Fatal("DeleteApp delegate broken")
	}

	// ---- P3 合规 ----
	rep := m.SaveReport(ten, &ComplianceReport{DeviceID: "dev-1", Score: 90})
	if rep == nil || rep.ID == "" {
		t.Fatalf("SaveReport delegate = %+v", rep)
	}
	if g, ok := m.GetReport(ten, rep.ID); !ok || g.Score != 90 {
		t.Fatalf("GetReport delegate = (%+v,%v)", g, ok)
	}
	if lst := m.ListReports(ten); len(lst) != 1 {
		t.Fatalf("ListReports delegate = %d", len(lst))
	}
	if !m.DeleteReport(ten, rep.ID) || m.DeleteReport(ten, rep.ID) {
		t.Fatal("DeleteReport delegate broken")
	}

	// ---- P3 备份 ----
	bk := m.CreateBackup(ten, &BackupRecord{Type: "full"})
	if bk == nil || bk.ID == "" || bk.Status != "creating" {
		t.Fatalf("CreateBackup delegate = %+v", bk)
	}
	if g, ok := m.GetBackup(ten, bk.ID); !ok || g.Type != "full" {
		t.Fatalf("GetBackup delegate = (%+v,%v)", g, ok)
	}
	if lst := m.ListBackups(ten); len(lst) != 1 {
		t.Fatalf("ListBackups delegate = %d", len(lst))
	}
	if !m.DeleteBackup(ten, bk.ID) || m.DeleteBackup(ten, bk.ID) {
		t.Fatal("DeleteBackup delegate broken")
	}

	// ---- P4 网络设备 ----
	nd := m.CreateNetworkDevice(ten, &NetworkDevice{Name: "sw-1"})
	if nd == nil || nd.ID == "" {
		t.Fatalf("CreateNetworkDevice delegate = %+v", nd)
	}
	if g, ok := m.GetNetworkDevice(ten, nd.ID); !ok || g.Name != "sw-1" {
		t.Fatalf("GetNetworkDevice delegate = (%+v,%v)", g, ok)
	}
	if u, ok := m.UpdateNetworkDevice(ten, &NetworkDevice{ID: nd.ID, Vendor: "huawei"}); !ok || u.Vendor != "huawei" {
		t.Fatalf("UpdateNetworkDevice delegate = (%+v,%v)", u, ok)
	}
	if c, ok := m.UpdateNetworkConfig(ten, nd.ID, "vlan 10"); !ok || c.Config != "vlan 10" {
		t.Fatalf("UpdateNetworkConfig delegate = (%+v,%v)", c, ok)
	}
	// metrics 按 deviceID 反查租户路由，须先登记 device→tenant 索引。
	m.deviceTenant[nd.ID] = ten
	m.StoreNetworkMetrics(nd.ID, &NetworkMetrics{CPUUsage: 5})
	if metric := m.GetNetworkMetrics(nd.ID); metric == nil || metric.CPUUsage != 5 {
		t.Fatalf("GetNetworkMetrics delegate = %+v", metric)
	}
	if lst := m.ListNetworkDevices(ten); len(lst) != 1 {
		t.Fatalf("ListNetworkDevices delegate = %d", len(lst))
	}
	if !m.DeleteNetworkDevice(ten, nd.ID) || m.DeleteNetworkDevice(ten, nd.ID) {
		t.Fatal("DeleteNetworkDevice delegate broken")
	}

	// ---- P4 自动化 ----
	rule := m.CreateAutomationRule(ten, &AutomationRule{Name: "rule-a"})
	if rule == nil || rule.ID == "" {
		t.Fatalf("CreateAutomationRule delegate = %+v", rule)
	}
	if g, ok := m.GetAutomationRule(ten, rule.ID); !ok || g.Name != "rule-a" {
		t.Fatalf("GetAutomationRule delegate = (%+v,%v)", g, ok)
	}
	if u, ok := m.UpdateAutomationRule(ten, &AutomationRule{ID: rule.ID, Description: "d"}); !ok {
		t.Fatalf("UpdateAutomationRule delegate = (%+v,%v)", u, ok)
	}
	if en, ok := m.EnableAutomationRule(ten, rule.ID); !ok || !en.Enabled {
		t.Fatalf("EnableAutomationRule delegate = (%+v,%v)", en, ok)
	}
	if dis, ok := m.DisableAutomationRule(ten, rule.ID); !ok || dis.Enabled {
		t.Fatalf("DisableAutomationRule delegate = (%+v,%v)", dis, ok)
	}
	if lst := m.ListAutomationRules(ten); len(lst) != 1 {
		t.Fatalf("ListAutomationRules delegate = %d", len(lst))
	}
	exec := m.CreateAutomationExecution(ten, &AutomationExecution{RuleID: rule.ID})
	if exec == nil || exec.ID == "" || exec.Status != "pending" {
		t.Fatalf("CreateAutomationExecution delegate = %+v", exec)
	}
	if g, ok := m.GetAutomationExecution(ten, exec.ID); !ok {
		t.Fatalf("GetAutomationExecution delegate = (%+v,%v)", g, ok)
	}
	if lst := m.ListAutomationExecutions(ten, 0); len(lst) != 1 {
		t.Fatalf("ListAutomationExecutions delegate = %d", len(lst))
	}
	if !m.DeleteAutomationRule(ten, rule.ID) || m.DeleteAutomationRule(ten, rule.ID) {
		t.Fatal("DeleteAutomationRule delegate broken")
	}

	// ---- P5 Webhook ----
	wh := m.CreateWebhook(ten, &Webhook{Name: "hook-1", URL: "https://hooks/x"})
	if wh == nil || wh.ID == "" {
		t.Fatalf("CreateWebhook delegate = %+v", wh)
	}
	if g, ok := m.GetWebhook(ten, wh.ID); !ok || g.Name != "hook-1" {
		t.Fatalf("GetWebhook delegate = (%+v,%v)", g, ok)
	}
	if u, ok := m.UpdateWebhook(ten, &Webhook{ID: wh.ID, Enabled: true}); !ok || !u.Enabled {
		t.Fatalf("UpdateWebhook delegate = (%+v,%v)", u, ok)
	}
	m.RecordWebhookDelivery(ten, wh.ID, "e", "{}", 200, "ok", "")
	if lst := m.ListWebhookDeliveries(ten, wh.ID); len(lst) != 1 {
		t.Fatalf("ListWebhookDeliveries delegate = %d", len(lst))
	}
	if lst := m.ListWebhooks(ten); len(lst) != 1 {
		t.Fatalf("ListWebhooks delegate = %d", len(lst))
	}
	if !m.DeleteWebhook(ten, wh.ID) || m.DeleteWebhook(ten, wh.ID) {
		t.Fatal("DeleteWebhook delegate broken")
	}

	// ---- P5 脚本 ----
	sc := m.CreateScript(ten, &Script{Name: "cleanup"})
	if sc == nil || sc.ID == "" || !sc.Enabled {
		t.Fatalf("CreateScript delegate = %+v（默认 Enabled=true）", sc)
	}
	if g, ok := m.GetScript(ten, sc.ID); !ok || g.Name != "cleanup" {
		t.Fatalf("GetScript delegate = (%+v,%v)", g, ok)
	}
	if u, ok := m.UpdateScript(ten, &Script{ID: sc.ID, TimeoutSec: 60}); !ok || u.TimeoutSec != 60 {
		t.Fatalf("UpdateScript delegate = (%+v,%v)", u, ok)
	}
	fin := time.Now()
	if rec := m.RecordScriptExecution(ten, sc.ID, "dev-1", "succeeded", "out", "", time.Time{}, &fin); rec == nil || rec.ID == "" {
		t.Fatalf("RecordScriptExecution delegate = %+v", rec)
	}
	if lst := m.ListScriptExecutions(ten, sc.ID); len(lst) != 1 {
		t.Fatalf("ListScriptExecutions delegate = %d", len(lst))
	}
	if lst := m.ListScripts(ten); len(lst) != 1 {
		t.Fatalf("ListScripts delegate = %d", len(lst))
	}
	if !m.DeleteScript(ten, sc.ID) || m.DeleteScript(ten, sc.ID) {
		t.Fatal("DeleteScript delegate broken")
	}

	// ---- P6 租户 / API Key / 插件 / 计费 ----
	tn := m.CreateTenant(&Tenant{ID: ten, Name: ten})
	if tn == nil || tn.Status != TenantStatusActive {
		t.Fatalf("CreateTenant delegate = %+v", tn)
	}
	if g, ok := m.GetTenant(ten); !ok || g.Name != ten {
		t.Fatalf("GetTenant delegate = (%+v,%v)", g, ok)
	}
	susp := TenantStatusSuspended
	if u, ok := m.UpdateTenant(&Tenant{ID: ten, Status: susp}); !ok || u.Status != TenantStatusSuspended {
		t.Fatalf("UpdateTenant delegate = (%+v,%v)", u, ok)
	}

	key := m.CreateAPIKey(ten, &APIKey{Name: "ci"})
	if key == nil || key.ID == "" {
		t.Fatalf("CreateAPIKey delegate = %+v", key)
	}
	if g, ok := m.GetAPIKey(ten, key.ID); !ok {
		t.Fatalf("GetAPIKey delegate = (%+v,%v)", g, ok)
	}
	if u, ok := m.UpdateAPIKey(ten, &APIKey{ID: key.ID, Enabled: true}); !ok || !u.Enabled {
		t.Fatalf("UpdateAPIKey delegate = (%+v,%v)", u, ok)
	}
	if lst := m.ListAPIKeys(ten); len(lst) != 1 {
		t.Fatalf("ListAPIKeys delegate = %d", len(lst))
	}
	if !m.DeleteAPIKey(ten, key.ID) || m.DeleteAPIKey(ten, key.ID) {
		t.Fatal("DeleteAPIKey delegate broken")
	}

	plg := m.CreatePlugin(&Plugin{Name: "gpu"})
	if plg == nil || plg.ID == "" {
		t.Fatalf("CreatePlugin delegate = %+v", plg)
	}
	if g, ok := m.GetPlugin(plg.ID); !ok || g.Name != "gpu" {
		t.Fatalf("GetPlugin delegate = (%+v,%v)", g, ok)
	}
	if u, ok := m.UpdatePlugin(&Plugin{ID: plg.ID, Installed: true}); !ok || !u.Installed {
		t.Fatalf("UpdatePlugin delegate = (%+v,%v)", u, ok)
	}

	plan := m.CreateBillingPlan(&SubscriptionPlan{Name: "pro"})
	if plan == nil || plan.ID == "" {
		t.Fatalf("CreateBillingPlan delegate = %+v", plan)
	}
	if g, ok := m.GetBillingPlan(plan.ID); !ok || g.Name != "pro" {
		t.Fatalf("GetBillingPlan delegate = (%+v,%v)", g, ok)
	}
	if u, ok := m.UpdateBillingPlan(&SubscriptionPlan{ID: plan.ID, Price: 100}); !ok || u.Price != 100 {
		t.Fatalf("UpdateBillingPlan delegate = (%+v,%v)", u, ok)
	}
	sub := m.CreateSubscription(&Subscription{TenantID: ten, PlanID: plan.ID})
	if sub == nil || sub.ID == "" || sub.Status != "active" {
		t.Fatalf("CreateSubscription delegate = %+v", sub)
	}
	if g, ok := m.GetSubscription(sub.ID); !ok || g.PlanID != plan.ID {
		t.Fatalf("GetSubscription delegate = (%+v,%v)", g, ok)
	}
	if u, ok := m.UpdateSubscription(&Subscription{ID: sub.ID, TenantID: ten, Status: "canceled"}); !ok || u.Status != "canceled" {
		t.Fatalf("UpdateSubscription delegate = (%+v,%v)", u, ok)
	}
	inv := m.CreateInvoice(&Invoice{TenantID: ten, Amount: 100})
	if inv == nil || inv.ID == "" || inv.Status != "pending" {
		t.Fatalf("CreateInvoice delegate = %+v", inv)
	}
	if g, ok := m.GetInvoice(inv.ID); !ok || g.Amount != 100 {
		t.Fatalf("GetInvoice delegate = (%+v,%v)", g, ok)
	}

	// ---- 空租户聚合分支：第二租户写入后 List*("") 遍历全部 store ----
	tenb := "tenantb"
	m.CreateAPIKey(tenb, &APIKey{Name: "other"}) // 触发第二个 schema 创建
	m.CreateInvoice(&Invoice{TenantID: tenb})
	if got := m.ListAPIKeys(""); len(got) < 1 {
		t.Fatalf("ListAPIKeys(\"\") aggregate = %d, want >=1", len(got))
	}
	if got := m.ListInvoices(""); len(got) < 2 {
		t.Fatalf("ListInvoices(\"\") aggregate = %d, want >=2", len(got))
	}
	if got := m.ListPlugins(); len(got) != 1 {
		t.Fatalf("ListPlugins aggregate = %d, want 1", len(got))
	}
	if got := m.ListBillingPlans(); len(got) != 1 {
		t.Fatalf("ListBillingPlans aggregate = %d, want 1", len(got))
	}
	if got := m.ListTenants(); len(got) < 1 {
		t.Fatalf("ListTenants aggregate = %d, want >=1", len(got))
	}

	// ---- P6 删除路径（放最后，避免影响前面的聚合断言）----
	if !m.DeleteSubscription(sub.ID) || m.DeleteSubscription(sub.ID) {
		t.Fatal("DeleteSubscription delegate broken")
	}
	if !m.DeletePlugin(plg.ID) || m.DeletePlugin(plg.ID) {
		t.Fatal("DeletePlugin delegate broken")
	}
	if !m.DeleteBillingPlan(plan.ID) || m.DeleteBillingPlan(plan.ID) {
		t.Fatal("DeleteBillingPlan delegate broken")
	}
	if !m.DeleteTenant(ten) || m.DeleteTenant(ten) {
		t.Fatal("DeleteTenant delegate broken")
	}
}
