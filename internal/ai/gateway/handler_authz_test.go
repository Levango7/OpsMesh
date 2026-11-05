// handler_authz_test.go 钉住 AI 网关 11 条注册的权限点映射与「拒绝即不动数据」。
//
// 缺陷背景（G1 同族，与 ai/agent、k8slifecycle 并列）：RegisterAIGatewayRoutes
// 挂上 mux 后没有任何鉴权闸，匿名即可
//   - POST /ai/chat|/ai/embed 烧本租户模型额度（公网免费模型代理）；
//   - POST /ai/rag/ingest 往共享向量库投文档（下一次别人检索就命中）；
//   - POST/PUT/DELETE /ai/prompts 增删提示词模板（提示词是 Agent 侧的事实源）。
//
// 用例走 mux 而不是直调 handler：这样「注册时挂错方法/错 handler」也会被同一张表抓到。
package aigateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// denyAuthorize 记录请求携带的权限点后一律写 403 并返回 false（= 已登录但权限不足）。
func denyAuthorize(seen *string) func(http.ResponseWriter, *http.Request, string) (authctx.Context, bool) {
	return func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
		*seen = perm
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "permission denied: " + perm})
		return authctx.Context{}, false
	}
}

// countingAuthorize 记录回调被调了几次（用于「405 不该消费鉴权」的断言）。
func countingAuthorize(calls *int) func(http.ResponseWriter, *http.Request, string) (authctx.Context, bool) {
	return func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
		*calls++
		return authctx.Context{TenantID: "t1", UserID: "u1"}, true
	}
}

func TestAIGatewayRoutePermissionMapping(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   string
	}{
		{"chat", http.MethodPost, "/api/v1/ai/chat", `{"messages":[{"role":"user","content":"hi"}]}`, "ai:read"},
		{"embed", http.MethodPost, "/api/v1/ai/embed", `{"text":"hi"}`, "ai:read"},
		{"rag search", http.MethodPost, "/api/v1/ai/rag/search", `{"query":"hi"}`, "ai:read"},
		{"rag ingest", http.MethodPost, "/api/v1/ai/rag/ingest", `{"documents":[{"id":"d1","content":"c","embedding":[1]}]}`, "ai:write"},
		{"usage", http.MethodGet, "/api/v1/ai/usage", "", "ai:read"},
		{"providers health", http.MethodGet, "/api/v1/ai/health", "", "ai:read"},
		{"prompts 列表", http.MethodGet, "/api/v1/ai/prompts", "", "ai:read"},
		{"prompts 新建", http.MethodPost, "/api/v1/ai/prompts", `{"id":"p1","content":"c"}`, "ai:write"},
		{"prompts 单读", http.MethodGet, "/api/v1/ai/prompts/p1", "", "ai:read"},
		{"prompts 改", http.MethodPut, "/api/v1/ai/prompts/p1", `{"content":"c2"}`, "ai:write"},
		{"prompts 删", http.MethodDelete, "/api/v1/ai/prompts/p1", "", "ai:write"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs := NewInMemoryVectorStore()
			h := NewAIGatewayHandler(NewGateway(), vs, 5)
			var seen string
			h.Authorize = denyAuthorize(&seen)

			mux := http.NewServeMux()
			RegisterAIGatewayRoutes(mux, h)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.body != "" {
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

// 拒绝必须发生在任何写动作之前：只回 403 却已经把文档落库/模板改掉，等于闸装了但没接上。
func TestAIGatewayDenyDoesNotMutate(t *testing.T) {
	vs := NewInMemoryVectorStore()
	h := NewAIGatewayHandler(NewGateway(), vs, 5)
	if _, err := h.promptStore.Create(context.Background(), &PromptTemplate{ID: "p1", Content: "原始内容"}); err != nil {
		t.Fatalf("seed prompt: %v", err)
	}
	mux := http.NewServeMux()
	RegisterAIGatewayRoutes(mux, h)

	serve := func(method, path, body string) int {
		var seen string
		h.Authorize = denyAuthorize(&seen)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := serve(http.MethodPost, "/api/v1/ai/rag/ingest", `{"documents":[{"id":"d1","content":"c","embedding":[1]}]}`); code != http.StatusForbidden {
		t.Fatalf("ingest 拒绝应 403，got %d", code)
	}
	if n, err := vs.Count(context.Background()); err != nil || n != 0 {
		t.Fatalf("403 之后向量库应仍为空，got count=%d err=%v", n, err)
	}

	if code := serve(http.MethodPost, "/api/v1/ai/prompts", `{"id":"p2","content":"c"}`); code != http.StatusForbidden {
		t.Fatalf("prompts POST 拒绝应 403，got %d", code)
	}
	if got := h.promptStore.List(context.Background(), ""); len(got) != 1 || got[0].ID != "p1" {
		t.Fatalf("403 之后不应新增模板，got %+v", got)
	}

	if code := serve(http.MethodPut, "/api/v1/ai/prompts/p1", `{"content":"篡改"}`); code != http.StatusForbidden {
		t.Fatalf("prompts PUT 拒绝应 403，got %d", code)
	}
	if tmpl, ok := h.promptStore.Get(context.Background(), "p1"); !ok || tmpl.Content != "原始内容" {
		t.Fatalf("403 之后模板内容不应被改写，got %+v ok=%v", tmpl, ok)
	}

	if code := serve(http.MethodDelete, "/api/v1/ai/prompts/p1", ""); code != http.StatusForbidden {
		t.Fatalf("prompts DELETE 拒绝应 403，got %d", code)
	}
	if _, ok := h.promptStore.Get(context.Background(), "p1"); !ok {
		t.Fatal("403 之后模板不应被删除")
	}
}

// 未服务的动词直接 405，且不消费鉴权回调（与各 handler「方法检查前置」的口径一致）。
func TestAIGatewayUnsupportedVerbSkipsAuthorize(t *testing.T) {
	h := NewAIGatewayHandler(NewGateway(), NewInMemoryVectorStore(), 5)
	calls := 0
	h.Authorize = countingAuthorize(&calls)
	mux := http.NewServeMux()
	RegisterAIGatewayRoutes(mux, h)

	// 对照拍：确实有动词会命中回调，否则下面的 calls==0 是空断言。
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/ai/prompts", nil))
	if calls != 1 {
		t.Fatalf("对照失败：GET /ai/prompts 应调用鉴权 1 次，got %d", calls)
	}
	calls = 0

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/ai/chat"},
		{http.MethodGet, "/api/v1/ai/embed"},
		{http.MethodGet, "/api/v1/ai/rag/ingest"},
		{http.MethodDelete, "/api/v1/ai/rag/search"},
		{http.MethodPost, "/api/v1/ai/usage"},
		{http.MethodPost, "/api/v1/ai/health"},
		{http.MethodPatch, "/api/v1/ai/prompts"},
		{http.MethodPatch, "/api/v1/ai/prompts/p1"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		// Go 1.22 ServeMux 的方法不匹配自带 405；prompts/{id} 的子树模式会命中 handler 的动词分支。
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s = %d, want 405 body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	if calls != 0 {
		t.Fatalf("405 路径不应调用鉴权回调，got %d 次", calls)
	}
}

// 未注入回调（独立部署/单测）保持透传，不改变既有行为。
func TestAIGatewayAuthorizeNilPassesThrough(t *testing.T) {
	h := NewAIGatewayHandler(NewGateway(), NewInMemoryVectorStore(), 5)
	rec := httptest.NewRecorder()
	h.HandlePromptTemplates(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ai/prompts", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("未启用鉴权时提示词列表应放行，got %d body=%s", rec.Code, rec.Body.String())
	}
}
