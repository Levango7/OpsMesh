package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aigateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
	"github.com/Levango7/OpsMesh/internal/config"
)

// 回归：AI 网关的 11 条注册必须由 NewServer 注入鉴权回调。
// 修复前 aigateway.RegisterAIGatewayRoutes 挂上 mux 后没有任何闸：匿名即可
// ① POST /ai/chat 与 /ai/embed 烧本租户的模型额度（等于公网免费模型代理），
// ② POST /ai/rag/ingest 往共享向量库投文档，③ POST/PUT/DELETE /ai/prompts 改提示词事实源。
// 与 ai/agent、k8slifecycle 同族（docs/semantic-rulings-2026-10-30.md 记为 G1 全域匿名缺口）。
func TestAIGatewayRoutesAreAuthorized(t *testing.T) {
	cfg := &config.Config{
		Mode:                "controlplane",
		Store:               "memory",
		EventBus:            "noop",
		RequireAuth:         true,
		TrustGatewayHeaders: false,
		GRPCPort:            0,
		HTTPPort:            0,
		MetricsPort:         0,
		TaskMaxRetries:      3,
	}
	s, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if s.aiGatewayHandler == nil {
		t.Fatal("aiGatewayHandler 未构造")
	}
	if s.aiGatewayHandler.Authorize == nil {
		t.Fatal("AI 网关未注入鉴权回调——11 条路由将匿名可达")
	}

	mux := http.NewServeMux()
	aigateway.RegisterAIGatewayRoutes(mux, s.aiGatewayHandler)

	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/ai/chat", `{"messages":[{"role":"user","content":"你好"}]}`},
		{http.MethodPost, "/api/v1/ai/embed", `{"text":"你好"}`},
		{http.MethodPost, "/api/v1/ai/rag/ingest", `{"documents":[{"id":"d1","content":"投毒","embedding":[1]}]}`},
		{http.MethodPost, "/api/v1/ai/rag/search", `{"query":"你好"}`},
		{http.MethodGet, "/api/v1/ai/usage", ""},
		{http.MethodGet, "/api/v1/ai/health", ""},
		{http.MethodGet, "/api/v1/ai/prompts", ""},
		{http.MethodPost, "/api/v1/ai/prompts", `{"id":"p1","content":"c"}`},
		{http.MethodGet, "/api/v1/ai/prompts/p1", ""},
		{http.MethodPut, "/api/v1/ai/prompts/p1", `{"content":"c2"}`},
		{http.MethodDelete, "/api/v1/ai/prompts/p1", ""},
	}
	for _, tc := range cases {
		var req *http.Request
		if tc.body == "" {
			req = httptest.NewRequest(tc.method, tc.path, nil)
		} else {
			req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s 匿名请求 = %d, want 401（body=%s）", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}
