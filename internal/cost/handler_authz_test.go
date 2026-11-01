// handler_authz_test.go 钉住成本控制 8 条注册的权限点映射、拒绝不动数据，
// 以及 tenantId 以**已验签上下文**为准（不能再由客户端自报）。
//
// 缺陷背景（2026-11-02，G1 同族的第二层）：RegisterCostRoutes 的 8 条注册此前既无
// 鉴权闸，又直接把 ?tenantId= / body.tenantId 当作查询与写入的作用域——任何人
// 改一个参数就能读别家的预算、告警、报表，POST 还能替别家改额度（越权写）。
package cost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

func newAuthzHandler() (*CostHandler, *BudgetManager, *ModelRouter, *CostTracker) {
	bm := NewBudgetManager()
	mr := NewModelRouter()
	ct := NewCostTracker()
	return NewCostHandler(bm, mr, ct), bm, mr, ct
}

// denyCost 记录请求携带的权限点后一律写 403 返回 false。
func denyCost(seen *string) func(http.ResponseWriter, *http.Request, string) (authctx.Context, bool) {
	return func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
		*seen = perm
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "permission denied: " + perm})
		return authctx.Context{}, false
	}
}

// allowAs 造一个「鉴权通过、身份固定为该租户」的回调。
func allowAs(tenantID string) func(http.ResponseWriter, *http.Request, string) (authctx.Context, bool) {
	return func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
		return authctx.Context{TenantID: tenantID, UserID: "u1"}, true
	}
}

func TestCostRoutePermissionMapping(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   string
	}{
		{"budget 读", http.MethodGet, "/api/v1/cost/budget?tenantId=t1", "", "cost:read"},
		{"budget 写", http.MethodPost, "/api/v1/cost/budget", `{"tenantId":"t1","daily":100,"monthly":1000,"threshold":0.8}`, "cost:write"},
		{"alerts", http.MethodGet, "/api/v1/cost/alerts?tenantId=t1", "", "cost:read"},
		{"report", http.MethodGet, "/api/v1/cost/report?tenantId=t1", "", "cost:read"},
		{"routes 读", http.MethodGet, "/api/v1/cost/routes", "", "cost:read"},
		{"routes 写", http.MethodPost, "/api/v1/cost/routes", `{"rules":[{"taskType":"chat","model":"m1"}]}`, "cost:write"},
		{"route 查询", http.MethodPost, "/api/v1/cost/route", `{"taskType":"chat"}`, "cost:read"},
		{"报表生成", http.MethodPost, "/api/v1/cost/report/generate", `{"title":"t"}`, "cost:read"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _, _, _ := newAuthzHandler()
			var seen string
			h.Authorize = denyCost(&seen)
			mux := http.NewServeMux()
			RegisterCostRoutes(mux, h)

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("拒绝时应 403，got %d body=%s", rec.Code, rec.Body.String())
			}
			if seen != tc.want {
				t.Fatalf("%s %s 权限点 = %q, want %q", tc.method, tc.path, seen, tc.want)
			}
		})
	}
}

// 拒绝之后预算与路由规则必须原样不动。
func TestCostDenyDoesNotMutate(t *testing.T) {
	h, bm, mr, _ := newAuthzHandler()
	bm.SetBudget("t1", 1000, 20000, 0.8)
	mr.SetRules([]RoutingRule{{TaskType: "chat", Model: "keep-me", Tier: TierSmall}})
	mux := http.NewServeMux()
	RegisterCostRoutes(mux, h)

	serve := func(method, path, body string) int {
		var seen string
		h.Authorize = denyCost(&seen)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec.Code
	}

	if code := serve(http.MethodPost, "/api/v1/cost/budget", `{"tenantId":"t1","daily":1,"monthly":1,"threshold":0.01,"timezone":"UTC"}`); code != http.StatusForbidden {
		t.Fatalf("budget 写拒绝应 403，got %d", code)
	}
	if b := bm.GetBudget("t1"); b.DailyLimit != 1000 || b.MonthlyLimit != 20000 || b.AlertThreshold != 0.8 {
		t.Fatalf("403 之后预算不应被改写，got %+v", b)
	}

	if code := serve(http.MethodPost, "/api/v1/cost/routes", `{"rules":[{"taskType":"chat","model":"劫持","tier":"large"}]}`); code != http.StatusForbidden {
		t.Fatalf("routes 写拒绝应 403，got %d", code)
	}
	rules := mr.GetRules()
	if len(rules) != 1 || rules[0].Model != "keep-me" {
		t.Fatalf("403 之后路由规则不应被替换，got %+v", rules)
	}
}

// 自报 tenantId 不得决定作用域：读别家的预算/报表/告警、替别家改额度都是越权。
func TestCostTenantComesFromVerifiedContext(t *testing.T) {
	h, bm, _, ct := newAuthzHandler()
	bm.SetBudget("victim", 7, 70, 0.5)
	// 让 victim 真的产出一条越限告警，否则「不得回给 attacker」是空断言。
	if _, alerts := bm.RecordUsage("victim", 10); len(alerts) == 0 {
		t.Fatal("对照失败：RecordUsage 越限应产生告警，否则本用例什么都没测")
	}
	ct.Record("victim", "openai", "gpt-4o", 1000, 1000, "chat")
	h.Authorize = allowAs("attacker")
	mux := http.NewServeMux()
	RegisterCostRoutes(mux, h)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	rec := get("/api/v1/cost/budget?tenantId=victim")
	if rec.Code != http.StatusOK {
		t.Fatalf("budget 读应 200，got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"tenantId":"attacker"`) {
		t.Fatalf("读到的应是已验签租户自己的预算，body=%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"dailyLimit":7`) {
		t.Fatalf("不得把别家（victim）的额度回给 attacker，body=%s", rec.Body.String())
	}

	rec = get("/api/v1/cost/report?tenantId=victim")
	if rec.Code != http.StatusOK {
		t.Fatalf("report 读应 200，got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"recordCount":1`) {
		t.Fatalf("报表不得含别家的消耗记录，body=%s", rec.Body.String())
	}

	rec = get("/api/v1/cost/alerts?tenantId=victim")
	if rec.Code != http.StatusOK {
		t.Fatalf("alerts 读应 200，got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"tenantId":"victim"`) {
		t.Fatalf("告警列表不得含别家的告警，body=%s", rec.Body.String())
	}

	// 写：请求体自报 victim，也只能改到自己（attacker）头上。
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/cost/budget",
		strings.NewReader(`{"tenantId":"victim","daily":999999,"monthly":999999,"threshold":0.9}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("budget 写（已授权）应 200，got %d body=%s", rec.Code, rec.Body.String())
	}
	if v := bm.GetBudget("victim"); v.DailyLimit != 7 {
		t.Fatalf("victim 的额度不得被 attacker 改写，got daily=%d", v.DailyLimit)
	}
	if a := bm.GetBudget("attacker"); a.DailyLimit != 999999 {
		t.Fatalf("已验签租户自己的额度应生效，got daily=%d", a.DailyLimit)
	}
}

// 未启用鉴权（独立部署）时沿用客户端自报值，行为与改造前一致——
// 同时证明上面的断言不是「永远返回空预算」造成的假绿。
func TestCostOpenModeKeepsSelfReportedTenant(t *testing.T) {
	h, bm, _, _ := newAuthzHandler()
	bm.SetBudget("victim", 7, 70, 0.5)
	mux := http.NewServeMux()
	RegisterCostRoutes(mux, h) // h.Authorize == nil

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cost/budget?tenantId=victim", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"dailyLimit":7`) {
		t.Fatalf("未启用鉴权时应沿用自报 tenantId，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// 未服务的动词由 mux 直接 405，不消费鉴权回调（对照拍证明回调会被服务的动词调用）。
func TestCostUnsupportedVerbSkipsAuthorize(t *testing.T) {
	h, _, _, _ := newAuthzHandler()
	calls := 0
	h.Authorize = func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
		calls++
		return authctx.Context{TenantID: "t1"}, true
	}
	mux := http.NewServeMux()
	RegisterCostRoutes(mux, h)

	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/cost/routes", nil))
	if calls != 1 {
		t.Fatalf("对照失败：GET /cost/routes 应调用鉴权 1 次，got %d", calls)
	}
	calls = 0

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/cost/route"},
		{http.MethodGet, "/api/v1/cost/report/generate"},
		{http.MethodDelete, "/api/v1/cost/budget"},
		{http.MethodPut, "/api/v1/cost/routes"},
		{http.MethodPost, "/api/v1/cost/alerts"},
		{http.MethodPost, "/api/v1/cost/report"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s = %d, want 405 body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	if calls != 0 {
		t.Fatalf("405 路径不应调用鉴权回调，got %d 次", calls)
	}
}

// 时间窗口不因鉴权改造而漂移：预算写入后仍按已验签租户的时区口径记账（回归护栏）。
func TestCostBudgetWriteStillHonoursTimezoneAfterAuthz(t *testing.T) {
	h, bm, _, _ := newAuthzHandler()
	h.Authorize = allowAs("t1")
	mux := http.NewServeMux()
	RegisterCostRoutes(mux, h)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/cost/budget",
		strings.NewReader(`{"tenantId":"ignored","daily":100,"monthly":1000,"threshold":0.8,"timezone":"Asia/Shanghai"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("写预算应 200，got %d body=%s", rec.Code, rec.Body.String())
	}
	b := bm.GetBudget("t1")
	if b.Timezone != "Asia/Shanghai" {
		t.Fatalf("时区应落在已验签租户上，got %+v", b)
	}
}
