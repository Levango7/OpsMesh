// multi_schema_delegation_test.go MultiSchemaStore 委托层 smoke（自 memory_crud_extra_test.go 尾段迁出，TD-61）。
//
// TD-61 末批后本文件已随 multi_schema*.go 下沉 multischema 包（原「留在父包」的说明作废）；
// StubDomains 展示辅助用例（TestStubGuard_JoinAndWarnDomains）留在父包 stub_guard_test.go。
package multischema

import (
	"testing"
	"time"
)

// ============================================================================
// MultiSchemaStore 委托层 smoke（multi_schema_p03/p1~p6.go）
// ============================================================================

// memoryStoreFactory 每租户 schema 返回独立 MemoryStore 的工厂。
// 用于以内存后端驱动 MultiSchemaStore 的委托路由层。
func memoryStoreFactory(schema string) (Store, error) {
	return newMemoryStore(), nil
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
	// 委托路由可达性：新鲜实例在 1h 窗口内不过期（返回 0）。
	// 过期语义由 memory 包内测试用私有状态构造覆盖（拆包后父包无法访问 memory 子包私有状态，
	// 且 StaleServices 对 maxAge<=0 直接返回 nil，无法用负窗口替代）。
	if got := m.StaleServices(ten, time.Hour); len(got) != 0 {
		t.Fatalf("StaleServices delegate = %d, want 0（新鲜实例）", len(got))
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
	meta1 := m.SetSecret(&SecretItem{Key: "k1", Value: "${vault:test/k1}"}, ten)
	if meta1 == nil || meta1.Version != 1 {
		t.Fatalf("SetSecret delegate = %+v", meta1)
	}
	meta2 := m.RotateSecret(ten, "k1", "${vault:test/k1/v2}")
	if meta2 == nil || meta2.Version != 2 {
		t.Fatalf("RotateSecret delegate = %+v", meta2)
	}
	if item, ok := m.GetSecret(ten, "k1"); !ok || item.Value != "${vault:test/k1/v2}" {
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
