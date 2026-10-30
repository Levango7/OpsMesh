package cost

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------- BudgetManager ----------

func TestBudgetManagerDefaultAndCopy(t *testing.T) {
	bm := NewBudgetManager()
	b := bm.GetBudget("unknown")
	if b.DailyLimit != 100000 || b.MonthlyLimit != 1000000 || b.AlertThreshold != 0.8 {
		t.Fatalf("未知租户默认预算 = %+v", b)
	}
	// 返回值必须是副本：调用方改字段不得污染管理器的状态。
	b.DailyLimit = 1
	if got := bm.GetBudget("unknown").DailyLimit; got != 100000 {
		t.Fatalf("GetBudget 返回了内部指针，改副本后默认值变成 %d", got)
	}

	bm.SetBudget("t1", 100, 1000, 0.5)
	got := bm.GetBudget("t1")
	if got.DailyLimit != 100 || got.MonthlyLimit != 1000 || got.AlertThreshold != 0.5 {
		t.Fatalf("SetBudget 后 GetBudget = %+v", got)
	}
}

func TestBudgetManagerRecordUsageAndAlerts(t *testing.T) {
	bm := NewBudgetManager()
	bm.SetBudget("t1", 100, 1000, 0.8)

	// 80% 阈值：累计 79 不告警，到 80 告警（日维度）。
	if ok, alert := bm.RecordUsage("t1", 79); !ok || alert != nil {
		t.Fatalf("79/100 不应告警: ok=%v alert=%+v", ok, alert)
	}
	ok, alert := bm.RecordUsage("t1", 1)
	if ok || alert == nil || alert.Type != "daily" || alert.Used != 80 || alert.Limit != 100 {
		t.Fatalf("80/100 应告警 daily: ok=%v alert=%+v", ok, alert)
	}
	// 阈值告警 ≠ 超预算：80/100 仍在额度内。
	if bm.IsOverBudget("t1") {
		t.Fatal("80/100 只应告警，IsOverBudget 应为 false")
	}
	if _, alert := bm.RecordUsage("t1", 20); alert == nil || alert.Type != "daily" {
		t.Fatalf("100/100 应继续告警: %+v", alert)
	}
	if !bm.IsOverBudget("t1") {
		t.Fatal("日用量达上限，IsOverBudget 应为 true")
	}

	alerts := bm.GetAlerts("t1")
	if len(alerts) != 2 || alerts[0].TenantID != "t1" {
		t.Fatalf("GetAlerts(t1) = %+v", alerts)
	}
	if n := len(bm.GetAlerts("t2")); n != 0 {
		t.Fatalf("GetAlerts(t2) = %d 条, want 0", n)
	}
	if n := len(bm.GetAlerts("")); n != 2 {
		t.Fatalf("GetAlerts(全部) = %d 条, want 2", n)
	}

	// 未知租户：RecordUsage 自建默认预算，不告警。
	if ok, alert := bm.RecordUsage("t-new", 10); !ok || alert != nil {
		t.Fatalf("未知租户首次记录 = (%v,%+v), want (true,nil)", ok, alert)
	}
	if got := bm.GetBudget("t-new").UsedToday; got != 10 {
		t.Fatalf("未知租户首次记录后 UsedToday = %d, want 10", got)
	}
}

func TestBudgetManagerMonthlyAlert(t *testing.T) {
	bm := NewBudgetManager()
	// 日额度设得极大，确保只可能触发月维度。
	bm.SetBudget("t1", 1000000, 100, 0.8)
	ok, alert := bm.RecordUsage("t1", 80)
	if ok || alert == nil || alert.Type != "monthly" || alert.Used != 80 || alert.Limit != 100 {
		t.Fatalf("80/100 月用量应告警 monthly: ok=%v alert=%+v", ok, alert)
	}
}

func TestBudgetManagerDailyReset(t *testing.T) {
	bm := NewBudgetManager()
	bm.SetBudget("t1", 1000, 10000, 0.8)
	bm.mu.Lock()
	bm.budgets["t1"].UsedToday = 900
	bm.budgets["t1"].UsedMonth = 900
	bm.budgets["t1"].LastReset = time.Now().Add(-25 * time.Hour)
	bm.mu.Unlock()

	if _, alert := bm.RecordUsage("t1", 50); alert != nil {
		t.Fatalf("跨日重置后 50/1000 不应告警: %+v", alert)
	}
	if got := bm.GetBudget("t1").UsedToday; got != 50 {
		t.Fatalf("跨日重置后 UsedToday = %d, want 50（旧值 900 必须清零）", got)
	}
	if got := bm.GetBudget("t1").UsedMonth; got != 950 {
		t.Fatalf("月度累计不应随每日重置清零: %d", got)
	}
}

// UsedMonth 只在自然月切换时清零；此前实现只重置日计数，月度累计只增不减，
// 一旦用满月度额度就永久处于超预算状态。
func TestBudgetManagerMonthlyReset(t *testing.T) {
	bm := NewBudgetManager()
	bm.SetBudget("t1", 100000, 10000, 0.8)
	bm.mu.Lock()
	bm.budgets["t1"].UsedMonth = 900
	// 用「上月 1 日」而非 AddDate(0,-1,0)：月末（如 10-31）回退一个月会被规范化到本月。
	now := time.Now()
	bm.budgets["t1"].monthReset = time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, now.Location())
	bm.mu.Unlock()

	if _, alert := bm.RecordUsage("t1", 50); alert != nil {
		t.Fatalf("跨月后 50/10000 不应告警: %+v", alert)
	}
	if got := bm.GetBudget("t1").UsedMonth; got != 50 {
		t.Fatalf("跨月后 UsedMonth = %d, want 50（上月 900 必须清零）", got)
	}
}

func TestBudgetManagerIsOverBudgetUnknownTenant(t *testing.T) {
	if NewBudgetManager().IsOverBudget("nobody") {
		t.Fatal("未知租户不应被判超预算")
	}
}

// 窗口归一必须发生在读侧：只在写入时归一的话，跨窗口后 UI 仍显示上个窗口的用量。
func TestBudgetManagerReadPathNormalizesWindow(t *testing.T) {
	bm := NewBudgetManager()
	bm.SetBudget("t1", 1000, 10000, 0.8)
	bm.mu.Lock()
	yesterday := time.Now().AddDate(0, 0, -1)
	loc := bm.budgets["t1"].location()
	bm.budgets["t1"].UsedToday = 900
	bm.budgets["t1"].LastReset = dayStart(yesterday, loc)
	bm.mu.Unlock()

	got := bm.GetBudget("t1") // 只读，不写
	if got.UsedToday != 0 {
		t.Fatalf("跨日后只读 GetBudget 的 UsedToday = %d, want 0（读侧未归一）", got.UsedToday)
	}
	if !got.LastReset.Equal(dayStart(time.Now(), loc)) {
		t.Fatalf("LastReset 未对齐到当前自然日零点: %v", got.LastReset)
	}
}

// 同一实例、同一 UTC 时刻、同一存储的窗口起点：UTC 租户已进入新的一天，
// Asia/Shanghai 租户仍在前一天——窗口必须按租户时区切，不能按服务器时区。
func TestBudgetManagerWindowFollowsTenantTimezone(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC)    // 上海已是 01-01 08:30
	stored := time.Date(2025, 12, 31, 16, 0, 0, 0, time.UTC) // = 上海 01-01 零点

	cases := []struct {
		tz         string
		wantUsed   int
		wantReason string
	}{
		{"UTC", 10, "UTC 已进入 01-01 新日，旧日用量应清零后再累计"},
		{"Asia/Shanghai", 910, "上海仍在 01-01 窗口内（起点即 12-31T16:00Z），用量必须保留"},
	}
	for _, tc := range cases {
		bm := NewBudgetManager()
		bm.now = func() time.Time { return fixed }
		bm.mu.Lock()
		bm.budgets["t1"] = &TokenBudget{
			TenantID: "t1", DailyLimit: 100000, MonthlyLimit: 1000000, AlertThreshold: 0.8,
			Timezone: tc.tz, UsedToday: 900, UsedMonth: 900, LastReset: stored, monthReset: stored,
		}
		bm.mu.Unlock()

		if _, alert := bm.RecordUsage("t1", 10); alert != nil {
			t.Fatalf("%s: 10/100000 不应告警: %+v", tc.tz, alert)
		}
		if got := bm.GetBudget("t1").UsedToday; got != tc.wantUsed {
			t.Fatalf("%s: UsedToday = %d, want %d（%s）", tc.tz, got, tc.wantUsed, tc.wantReason)
		}
	}
}

// 提额/改阈值不得顺带抹掉已发生的用量，否则「改一次预算」就等价于清零一次额度。
func TestSetBudgetPreservesUsage(t *testing.T) {
	bm := NewBudgetManager()
	bm.SetBudget("t1", 100, 1000, 0.8)
	if _, alert := bm.RecordUsage("t1", 50); alert != nil {
		t.Fatalf("50/100 未达 80%% 阈值不应告警: %+v", alert)
	}

	bm.SetBudget("t1", 200, 2000, 0.9)
	got := bm.GetBudget("t1")
	if got.DailyLimit != 200 || got.MonthlyLimit != 2000 || got.AlertThreshold != 0.9 {
		t.Fatalf("额度未更新: %+v", got)
	}
	if got.UsedToday != 50 || got.UsedMonth != 50 {
		t.Fatalf("改额度把用量清零了: usedToday=%d usedMonth=%d", got.UsedToday, got.UsedMonth)
	}
}

func TestSetTimezone(t *testing.T) {
	bm := NewBudgetManager()
	if err := bm.SetTimezone("t1", "Asia/Shanghai"); err != nil {
		t.Fatalf("SetTimezone: %v", err)
	}

	// 非法时区名必须既报错、也不改动已存的窗口时区。
	if err := bm.SetTimezone("t1", "Not/AZone"); err == nil {
		t.Fatal("非法时区名应报错")
	}
	if got := bm.GetBudget("t1").Timezone; got != "Asia/Shanghai" {
		t.Fatalf("非法时区把已存时区改掉了: %q", got)
	}

	if err := bm.SetTimezone("t1", ""); err != nil {
		t.Fatalf("空时区应归一为 UTC: %v", err)
	}
	if got := bm.GetBudget("t1").Timezone; got != "UTC" {
		t.Fatalf("Timezone = %q, want UTC", got)
	}
}

// ---------- ModelRouter ----------

func TestModelRouterRoute(t *testing.T) {
	mr := NewModelRouter()
	cases := []struct {
		task string
		tier ModelTier
		mt   int
	}{
		{"classify", TierSmall, 256},
		{"reason", TierLarge, 4096},
		{"chat", TierMedium, 2048},
	}
	for _, tc := range cases {
		got := mr.Route(tc.task)
		if got.Tier != tc.tier || got.MaxTokens != tc.mt || got.TaskType != tc.task {
			t.Fatalf("Route(%s) = %+v, want tier=%s maxTokens=%d", tc.task, got, tc.tier, tc.mt)
		}
	}
	// 未登记任务回退到 medium 默认规则。
	if got := mr.Route("unknown-task"); got.Tier != TierMedium || got.TaskType != "unknown-task" {
		t.Fatalf("Route(未登记) = %+v, want medium 默认", got)
	}
}

func TestModelRouterRulesAndModels(t *testing.T) {
	mr := NewModelRouter()
	rules := mr.GetRules()
	if len(rules) == 0 {
		t.Fatal("默认路由规则为空")
	}
	// GetRules 必须是副本。
	rules[0].Model = "hacked"
	if mr.GetRules()[0].Model == "hacked" {
		t.Fatal("GetRules 返回了内部切片")
	}

	mr.SetRules([]RoutingRule{{TaskType: "audit", Tier: TierLarge, Provider: "p", Model: "m", MaxTokens: 9}})
	if got := mr.Route("audit"); got.Model != "m" || got.MaxTokens != 9 {
		t.Fatalf("SetRules 未生效: %+v", got)
	}

	mr.RegisterModel(ModelSpec{Name: "small-a", Tier: TierSmall})
	mr.RegisterModel(ModelSpec{Name: "large-a", Tier: TierLarge})
	mr.RegisterModel(ModelSpec{Name: "small-b", Tier: TierSmall})
	small := mr.GetModelsByTier(TierSmall)
	if len(small) != 2 || small[0].Name != "small-a" || small[1].Name != "small-b" {
		t.Fatalf("GetModelsByTier(small) = %+v", small)
	}
	if got := mr.GetModelsByTier(TierMedium); len(got) != 0 {
		t.Fatalf("GetModelsByTier(medium) = %+v, want 空", got)
	}
}

// ---------- CostTracker ----------

func TestCostTrackerRecordAndReport(t *testing.T) {
	ct := NewCostTracker()
	ct.RegisterPricing(ModelSpec{Name: "m1", CostPer1KIn: 1.0, CostPer1KOut: 2.0})
	ct.RegisterPricing(ModelSpec{Name: "m2", CostPer1KIn: 0.5, CostPer1KOut: 0.5})

	ct.Record("t1", "openai", "m1", 1000, 500, "chat")  // 1.0 + 1.0 = 2.0
	ct.Record("t1", "openai", "m2", 2000, 0, "code")    // 1.0
	ct.Record("t2", "azure", "unknown", 4000, 4000, "") // 未登记定价 ⇒ 0

	full := ct.Report("", time.Time{})
	if full.RecordCount != 3 {
		t.Fatalf("RecordCount = %d, want 3", full.RecordCount)
	}
	if diff := full.TotalCost - 3.0; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("TotalCost = %v, want 3.0", full.TotalCost)
	}
	if full.TotalInputTokens != 7000 || full.TotalOutputTokens != 4500 {
		t.Fatalf("Token 汇总 = in:%d out:%d", full.TotalInputTokens, full.TotalOutputTokens)
	}
	if full.ByProvider["openai"] != 3.0 || full.ByModel["m1"] != 2.0 || full.ByTenant["t1"] != 3.0 || full.ByTenant["t2"] != 0 {
		t.Fatalf("聚合结果 = %+v", full)
	}
	if _, ok := full.ByTaskType[""]; ok {
		t.Fatal("空 TaskType 不应进入 ByTaskType")
	}

	t1 := ct.Report("t1", time.Time{})
	if t1.RecordCount != 2 || len(t1.ByTenant) != 1 {
		t.Fatalf("租户过滤后 = count:%d tenants:%v", t1.RecordCount, t1.ByTenant)
	}
	if future := ct.Report("", time.Now().Add(time.Hour)); future.RecordCount != 0 {
		t.Fatalf("since=未来 应无记录, got %d", future.RecordCount)
	}
	if past := ct.Report("", time.Now().Add(-time.Hour)); past.RecordCount != 3 {
		t.Fatalf("since=过去 应含全部记录, got %d", past.RecordCount)
	}
}

// ---------- ReportGenerator ----------

func TestReportGeneratorMarkdown(t *testing.T) {
	rg := NewReportGenerator()
	md := rg.GenerateMarkdown("标题", []ReportSection{
		{Title: "章节一", Content: "正文", Table: [][]string{{"列A", "列B"}, {"1", "2"}}, Footer: "脚注"},
		{Title: "章节二"},
	})
	for _, want := range []string{"# 标题\n", "## 章节一\n", "正文\n", "| 列A | 列B |\n", "|---|---|\n", "| 1 | 2 |\n", "脚注\n", "## 章节二\n"} {
		if !strings.Contains(md, want) {
			t.Fatalf("Markdown 缺少 %q：%s", want, md)
		}
	}
}

func TestReportGeneratorCostReport(t *testing.T) {
	rg := NewReportGenerator()
	md := rg.GenerateCostReport(&CostReport{
		TotalCost:         1.2345,
		TotalInputTokens:  10,
		TotalOutputTokens: 20,
		RecordCount:       2,
		ByProvider:        map[string]float64{"openai": 1.2345},
	}, "成本")
	for _, want := range []string{"# 成本\n", "总成本: $1.2345", "总输入 Token: 10", "## 按提供商\n", "| openai | 1.2345 |"} {
		if !strings.Contains(md, want) {
			t.Fatalf("成本报表缺少 %q：%s", want, md)
		}
	}
	// 无数据的分组不应生成空表。
	if strings.Contains(md, "## 按模型") {
		t.Fatalf("空的按模型分组不应出现：%s", md)
	}
}

// ---------- CostHandler ----------

func newTestHandler() *CostHandler {
	bm := NewBudgetManager()
	mr := NewModelRouter()
	ct := NewCostTracker()
	ct.RegisterPricing(ModelSpec{Name: "m1", CostPer1KIn: 1.0, CostPer1KOut: 1.0})
	return NewCostHandler(bm, mr, ct)
}

func do(t *testing.T, fn http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	fn(w, r)
	return w
}

func TestCostHandlerBudget(t *testing.T) {
	h := newTestHandler()

	w := do(t, h.HandleBudget, http.MethodGet, "/api/v1/cost/budget?tenantId=t1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET budget = %d", w.Code)
	}
	var b TokenBudget
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("解码 budget: %v", err)
	}
	if b.TenantID != "t1" || b.DailyLimit != 100000 {
		t.Fatalf("GET budget = %+v", b)
	}
	if b.Timezone != "UTC" {
		t.Fatalf("默认窗口时区 = %q, want UTC（不能按服务器本地时区默认切）", b.Timezone)
	}

	if w := do(t, h.HandleBudget, http.MethodPost, "/api/v1/cost/budget", "{bad"); w.Code != http.StatusBadRequest {
		t.Fatalf("POST 非法 JSON = %d, want 400", w.Code)
	}
	if w := do(t, h.HandleBudget, http.MethodPost, "/api/v1/cost/budget", `{"tenantId":"t1","daily":10,"monthly":100,"threshold":0.5}`); w.Code != http.StatusOK {
		t.Fatalf("POST 合法预算 = %d", w.Code)
	}
	if got := h.budget.GetBudget("t1"); got.DailyLimit != 10 || got.AlertThreshold != 0.5 {
		t.Fatalf("POST 后预算未生效: %+v", got)
	}
	// 非法时区：400，且不能出现「额度已改、时区没改」的半应用。
	if w := do(t, h.HandleBudget, http.MethodPost, "/api/v1/cost/budget", `{"tenantId":"t2","daily":10,"monthly":20,"threshold":0.5,"timezone":"Not/AZone"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("非法时区 = %d, want 400", w.Code)
	}
	if got := h.budget.GetBudget("t2"); got.DailyLimit != 100000 {
		t.Fatalf("非法时区请求已把额度写进去: daily=%d", got.DailyLimit)
	}
	if w := do(t, h.HandleBudget, http.MethodPost, "/api/v1/cost/budget", `{"tenantId":"t2","daily":10,"monthly":20,"threshold":0.5,"timezone":"Asia/Shanghai"}`); w.Code != http.StatusOK {
		t.Fatalf("POST 带合法时区 = %d", w.Code)
	}
	if got := h.budget.GetBudget("t2"); got.Timezone != "Asia/Shanghai" || got.DailyLimit != 10 {
		t.Fatalf("时区/额度未同时生效: %+v", got)
	}

	if w := do(t, h.HandleBudget, http.MethodDelete, "/api/v1/cost/budget", ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE budget = %d, want 405", w.Code)
	}
}

func TestCostHandlerAlerts(t *testing.T) {
	h := newTestHandler()
	h.budget.SetBudget("t1", 10, 100, 0.5)
	h.budget.RecordUsage("t1", 10)

	w := do(t, h.HandleBudgetAlerts, http.MethodGet, "/api/v1/cost/alerts?tenantId=t1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET alerts = %d", w.Code)
	}
	var resp struct {
		Alerts []BudgetAlert `json:"alerts"`
		Count  int           `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解码 alerts: %v", err)
	}
	if resp.Count != 1 || len(resp.Alerts) != 1 {
		t.Fatalf("alerts 响应 = %+v", resp)
	}
	if w := do(t, h.HandleBudgetAlerts, http.MethodPost, "/api/v1/cost/alerts", ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST alerts = %d, want 405", w.Code)
	}
}

func TestCostHandlerReport(t *testing.T) {
	h := newTestHandler()
	h.tracker.Record("t1", "openai", "m1", 1000, 0, "chat")
	h.tracker.Record("t2", "azure", "m1", 1000, 0, "code")

	// since 非法必须 400：静默忽略会让客户端把全量当窗口内成本。
	if w := do(t, h.HandleCostReport, http.MethodGet, "/api/v1/cost/report?since=yesterday", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("非法 since = %d, want 400", w.Code)
	}

	w := do(t, h.HandleCostReport, http.MethodGet, "/api/v1/cost/report?tenantId=t1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET report = %d", w.Code)
	}
	var rep CostReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("解码 report: %v", err)
	}
	if rep.RecordCount != 1 || rep.TotalCost != 1.0 {
		t.Fatalf("按租户报表 = %+v", rep)
	}

	w = do(t, h.HandleCostReport, http.MethodGet, "/api/v1/cost/report?format=markdown", "")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("markdown 报表 = %d / %q", w.Code, w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Body.String(), "# AI Cost Report") {
		t.Fatalf("markdown 报表缺少标题: %s", w.Body.String())
	}
	if w := do(t, h.HandleCostReport, http.MethodPost, "/api/v1/cost/report", ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST report = %d, want 405", w.Code)
	}
}

func TestCostHandlerRoutes(t *testing.T) {
	h := newTestHandler()

	w := do(t, h.HandleModelRoute, http.MethodGet, "/api/v1/cost/routes", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "classify") {
		t.Fatalf("GET routes = %d %s", w.Code, w.Body.String())
	}
	if w := do(t, h.HandleModelRoute, http.MethodPost, "/api/v1/cost/routes", "{bad"); w.Code != http.StatusBadRequest {
		t.Fatalf("POST routes 非法 JSON = %d", w.Code)
	}
	if w := do(t, h.HandleModelRoute, http.MethodPost, "/api/v1/cost/routes", `{"rules":[{"taskType":"audit","tier":"large","model":"m"}]}`); w.Code != http.StatusOK {
		t.Fatalf("POST routes = %d", w.Code)
	}
	if got := h.router.Route("audit"); got.Model != "m" {
		t.Fatalf("路由规则未生效: %+v", got)
	}
	if w := do(t, h.HandleModelRoute, http.MethodPut, "/api/v1/cost/routes", ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT routes = %d, want 405", w.Code)
	}

	// 上一段 POST 已整体替换规则，单任务路由用全新实例验证默认规则表。
	h2 := newTestHandler()
	if w := do(t, h2.HandleRoute, http.MethodGet, "/api/v1/cost/route", ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET route = %d, want 405", w.Code)
	}
	if w := do(t, h2.HandleRoute, http.MethodPost, "/api/v1/cost/route", "{bad"); w.Code != http.StatusBadRequest {
		t.Fatalf("POST route 非法 JSON = %d", w.Code)
	}
	w = do(t, h2.HandleRoute, http.MethodPost, "/api/v1/cost/route", `{"taskType":"reason"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"large"`) {
		t.Fatalf("POST route reason = %d %s", w.Code, w.Body.String())
	}
}

func TestCostHandlerGenerateReport(t *testing.T) {
	h := newTestHandler()
	if w := do(t, h.HandleGenerateReport, http.MethodGet, "/api/v1/cost/report/generate", ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET generate = %d, want 405", w.Code)
	}
	if w := do(t, h.HandleGenerateReport, http.MethodPost, "/api/v1/cost/report/generate", "{bad"); w.Code != http.StatusBadRequest {
		t.Fatalf("POST generate 非法 JSON = %d", w.Code)
	}

	// 未给标题时用默认标题；默认格式输出 markdown。
	w := do(t, h.HandleGenerateReport, http.MethodPost, "/api/v1/cost/report/generate", `{"sections":[{"title":"S","content":"C"}]}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "# OpsMesh Report") {
		t.Fatalf("默认标题/格式 = %d %s", w.Code, w.Body.String())
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("默认格式 Content-Type = %q", w.Header().Get("Content-Type"))
	}

	// 非 markdown 格式回退为 JSON 包装。
	w = do(t, h.HandleGenerateReport, http.MethodPost, "/api/v1/cost/report/generate", `{"title":"T","sections":[{"title":"S"}],"format":"json"}`)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("json 格式 = %d / %q", w.Code, w.Header().Get("Content-Type"))
	}
	var resp struct {
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || !strings.Contains(resp.Markdown, "# T") {
		t.Fatalf("json 包装 = %+v (err=%v)", resp, err)
	}
}

// 注册表必须把每个端点都接上（方法+路径）；错方法由 ServeMux 直接给 405。
func TestRegisterCostRoutes(t *testing.T) {
	mux := http.NewServeMux()
	RegisterCostRoutes(mux, newTestHandler())

	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/cost/budget"},
		{http.MethodPost, "/api/v1/cost/budget"},
		{http.MethodGet, "/api/v1/cost/alerts"},
		{http.MethodGet, "/api/v1/cost/report"},
		{http.MethodGet, "/api/v1/cost/routes"},
		{http.MethodPost, "/api/v1/cost/routes"},
		{http.MethodPost, "/api/v1/cost/route"},
		{http.MethodPost, "/api/v1/cost/report/generate"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed {
			t.Fatalf("%s %s 未注册（%d）", tc.method, tc.path, w.Code)
		}
	}
}
