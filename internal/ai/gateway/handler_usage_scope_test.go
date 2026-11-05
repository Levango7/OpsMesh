// handler_usage_scope_test.go — TD-97 ③④ /ai/usage 口径的行为契约测试。
//
// 背景：该端点此前返回网关级累计（不分租户），任何持 ai:read 的人都能看到
// 平台总量（tech-debt.md TD-97 ③）。本文件钉住修好后的语义：
//   - 默认 = 本租户口径（无论 scope 参数）；
//   - scope=all 仅在「default 租户 + platform:readall」双门齐备时给全量；
//   - 非 default 租户即便持 platform:readall 也只拿到自己的（不确认全量口径存在）；
//   - default 租户但无 platform:readall 时静默降回本租户口径（不报错）。
package aigateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/store"
)

// tenantAuthorize 按表决定放行/拒绝，并记录被问到的权限点序列。
func tenantAuthorize(tenant string, granted map[string]bool, seen *[]string) func(http.ResponseWriter, *http.Request, string) (authctx.Context, bool) {
	return func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
		*seen = append(*seen, perm)
		if granted[perm] {
			return authctx.Context{TenantID: tenant, UserID: "u1"}, true
		}
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "permission denied: " + perm})
		return authctx.Context{}, false
	}
}

func doGetUsage(h *AIGatewayHandler, scope string) *httptest.ResponseRecorder {
	url := "/api/v1/ai/usage"
	if scope != "" {
		url += "?scope=" + scope
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	h.HandleTokenUsage(rec, req)
	return rec
}

func TestUsage_DefaultScopeIsOwnTenant(t *testing.T) {
	g := NewGateway()
	sp := &stubProvider{}
	g.RegisterProvider(ProviderConfig{Name: "p1", APIKey: "k", Models: []string{"m1"}}, sp)
	// 两个租户各打一次，产生可区分用量
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		ctx := tenantCtx(tenant)
		_, _ = g.Complete(ctx, CompletionRequest{Model: "m1", Messages: []Message{{Role: "user", Content: tenant}}})
	}

	var seen []string
	h := NewAIGatewayHandler(g, NewInMemoryVectorStore(), 5)
	h.Authorize = tenantAuthorize("tenant-a", map[string]bool{"ai:read": true}, &seen)

	rec := doGetUsage(h, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("默认查询应 200：%d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// tenant-a 与 tenant-b 用量相同（各 150），单看数字无法区分归属，
	// 故用 scope 回显 + 后续 case 的对比来钉口径。
	if !contains(body, `"scope":"own"`) {
		t.Fatalf("默认口径应回显 own：%s", body)
	}
	// 未问 platform:readall（默认形态不做第二次鉴权）
	for _, p := range seen {
		if p == "platform:readall" {
			t.Fatal("默认口径不应触发 platform:readall 鉴权")
		}
	}
}

func TestUsage_ScopeAllGrantedOnlyForDefaultTenantWithPermission(t *testing.T) {
	g := NewGateway()
	sp := &stubProvider{}
	g.RegisterProvider(ProviderConfig{Name: "p1", APIKey: "k", Models: []string{"m1"}}, sp)
	ctxA := tenantCtx("tenant-a")
	ctxDef := tenantCtx(store.DefaultTenantID)
	_, _ = g.Complete(ctxA, CompletionRequest{Model: "m1", Messages: []Message{{Role: "user", Content: "a"}}})
	_, _ = g.Complete(ctxDef, CompletionRequest{Model: "m1", Messages: []Message{{Role: "user", Content: "d"}}})

	// ① default 租户 + platform:readall ⇒ 全量（两条记录之和）
	var seen []string
	h := NewAIGatewayHandler(g, NewInMemoryVectorStore(), 5)
	h.Authorize = tenantAuthorize(store.DefaultTenantID, map[string]bool{"ai:read": true, "platform:readall": true}, &seen)
	rec := doGetUsage(h, "all")
	if rec.Code != http.StatusOK {
		t.Fatalf("平台口径应 200：%d", rec.Code)
	}
	body := rec.Body.String()
	if !contains(body, `"scope":"all"`) {
		t.Fatalf("平台口径应回显 all：%s", body)
	}
	if !contains(body, `"totalTokens":300`) { // 150+150
		t.Fatalf("平台口径应是两租户之和 300：%s", body)
	}

	// ② 业务租户持 platform:readall ⇒ 仍只给自己（不确认全量存在）
	seen = nil
	h2 := NewAIGatewayHandler(g, NewInMemoryVectorStore(), 5)
	h2.Authorize = tenantAuthorize("tenant-a", map[string]bool{"ai:read": true, "platform:readall": true}, &seen)
	rec2 := doGetUsage(h2, "all")
	body2 := rec2.Body.String()
	if !contains(body2, `"scope":"own"`) || !contains(body2, `"totalTokens":150`) {
		t.Fatalf("业务租户持 platform:readall 也只应拿到自己的 150：%s", body2)
	}

	// ③ default 租户但无 platform:readall ⇒ 明确 403（平台运维本人就该知道缺权限；
	//    设计修正：不再追求「静默降级」——authorize 失败时回调已把 403 写进同一
	//    ResponseWriter，继续写 200 只会得到半截响应）
	h3 := NewAIGatewayHandler(g, NewInMemoryVectorStore(), 5)
	h3.Authorize = tenantAuthorize(store.DefaultTenantID, map[string]bool{"ai:read": true}, &seen)
	rec3 := doGetUsage(h3, "all")
	if rec3.Code != http.StatusForbidden {
		t.Fatalf("平台租户索要全量但无权限应 403：%d %s", rec3.Code, rec3.Body.String())
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
