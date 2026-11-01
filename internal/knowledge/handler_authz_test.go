// handler_authz_test.go 钉住知识库 8 条注册的权限点映射，以及「拒绝必须发生在写动作之前」。
//
// 缺陷背景（G1 同族，2026-11-02）：RegisterKnowledgeRoutes 挂上 mux 后没有任何鉴权闸，
// 匿名即可 sync（从外部源拉资料覆盖文档库）、ingest（直接写文档库）、
// DELETE docs/{id}（删文档）、PUT sync-configs/{source}（关掉某源的同步），
// 也能匿名读走文档正文（search/docs/stats/sync-configs）。
package knowledge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// denyAuthz 记录请求携带的权限点后一律写 403 返回 false（= 已登录但权限不足）。
func denyAuthz(seen *string) func(http.ResponseWriter, *http.Request, string) (authctx.Context, bool) {
	return func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
		*seen = perm
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "permission denied: " + perm})
		return authctx.Context{}, false
	}
}

func TestKnowledgeRoutePermissionMapping(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   string
	}{
		{"sync", http.MethodPost, "/api/v1/knowledge/sync", `{}`, "knowledge:write"},
		{"search", http.MethodPost, "/api/v1/knowledge/search", `{"query":"cpu"}`, "knowledge:read"},
		{"ingest", http.MethodPost, "/api/v1/knowledge/ingest", `{"documents":[{"id":"d1","content":"c"}]}`, "knowledge:write"},
		{"docs 列表", http.MethodGet, "/api/v1/knowledge/docs", "", "knowledge:read"},
		{"docs 删除", http.MethodDelete, "/api/v1/knowledge/docs/d1", "", "knowledge:write"},
		{"stats", http.MethodGet, "/api/v1/knowledge/stats", "", "knowledge:read"},
		{"sync-configs 列表", http.MethodGet, "/api/v1/knowledge/sync-configs", "", "knowledge:read"},
		{"sync-configs 改", http.MethodPut, "/api/v1/knowledge/sync-configs/cmdb", `{"enabled":false}`, "knowledge:write"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newTestHandler()
			var seen string
			h.Authorize = denyAuthz(&seen)
			mux := http.NewServeMux()
			RegisterKnowledgeRoutes(mux, h)

			var req *http.Request
			if tc.body == "" {
				req = httptest.NewRequest(tc.method, tc.path, nil)
			} else {
				req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("拒绝时应 403，got %d body=%s", rec.Code, rec.Body.String())
			}
			if seen != tc.want {
				t.Fatalf("%s %s 权限点 = %q, want %q", tc.method, tc.path, seen, tc.want)
			}
		})
	}
}

// 403 之后文档库和同步配置必须一点没动：闸装了但接在写动作之后等于没装。
func TestKnowledgeDenyDoesNotMutate(t *testing.T) {
	h, store := newTestHandler()
	mux := http.NewServeMux()
	RegisterKnowledgeRoutes(mux, h)

	serve := func(method, path, body string) int {
		var seen string
		h.Authorize = denyAuthz(&seen)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec.Code
	}

	base, err := store.Count(context.Background())
	if err != nil || base == 0 {
		t.Fatalf("前置条件：newTestHandler 应已播种文档，got count=%d err=%v", base, err)
	}

	if code := serve(http.MethodPost, "/api/v1/knowledge/ingest", `{"documents":[{"id":"evil","content":"投毒","embedding":[1]}]}`); code != http.StatusForbidden {
		t.Fatalf("ingest 拒绝应 403，got %d", code)
	}
	if n, err := store.Count(context.Background()); err != nil || n != base {
		t.Fatalf("403 之后文档库条数不应变化，got count=%d want %d err=%v", n, base, err)
	}

	// 预置一篇文档，验证删除与改配置都不会在 403 之后生效。
	if err := store.Upsert(context.Background(), []KnowledgeDoc{{ID: "keep", Content: "原文", Embedding: []float64{1}}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	withKeep := base + 1
	if code := serve(http.MethodDelete, "/api/v1/knowledge/docs/keep", ""); code != http.StatusForbidden {
		t.Fatalf("delete 拒绝应 403，got %d", code)
	}
	if n, err := store.Count(context.Background()); err != nil || n != withKeep {
		t.Fatalf("403 之后文档不应被删除，got count=%d want %d err=%v", n, withKeep, err)
	}

	before := h.syncEng.GetConfigs()
	if len(before) == 0 {
		t.Fatal("对照失败：同步引擎没有预置配置，本用例的前置条件已变")
	}
	if code := serve(http.MethodPut, "/api/v1/knowledge/sync-configs/cmdb", `{"enabled":false,"interval":"10m"}`); code != http.StatusForbidden {
		t.Fatalf("sync-configs PUT 拒绝应 403，got %d", code)
	}
	for _, c := range h.syncEng.GetConfigs() {
		if !c.Enabled {
			t.Fatalf("403 之后不应有任何同步源被关掉：%+v", c)
		}
	}
}

// 未服务的动词由 mux 直接 405，不消费鉴权回调（对照拍证明回调确实会被服务的动词调用）。
func TestKnowledgeUnsupportedVerbSkipsAuthorize(t *testing.T) {
	h, _ := newTestHandler()
	calls := 0
	h.Authorize = func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
		calls++
		return authctx.Context{TenantID: "t1"}, true
	}
	mux := http.NewServeMux()
	RegisterKnowledgeRoutes(mux, h)

	// 对照拍：GET /docs 会走回调一次，否则下面的 calls==0 是空断言。
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/docs", nil))
	if calls != 1 {
		t.Fatalf("对照失败：GET /knowledge/docs 应调用鉴权 1 次，got %d", calls)
	}
	calls = 0

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/knowledge/sync"},
		{http.MethodGet, "/api/v1/knowledge/search"},
		{http.MethodGet, "/api/v1/knowledge/ingest"},
		{http.MethodPost, "/api/v1/knowledge/docs"},
		{http.MethodPut, "/api/v1/knowledge/stats"},
		{http.MethodDelete, "/api/v1/knowledge/sync-configs/cmdb"},
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
