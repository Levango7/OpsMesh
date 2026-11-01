package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/ai/agent"
	"github.com/Levango7/OpsMesh/internal/config"
)

// 回归：AI Agent 六条路由必须由 NewServer 注入鉴权回调。
// 此前 agent.RegisterAgentRoutes 挂在 mux 上后没有任何鉴权闸——匿名即可
// POST /api/v1/ai/agent/execute 驱动 Agent（其工具集含 execute_command 远程 shell），
// 并按请求体自报的 userId/tenantId 冒充任意身份（docs/semantic-rulings-2026-10-30.md 同族缺口）。
func TestAIAgentRoutesAreAuthorized(t *testing.T) {
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
	if s.agentHandler == nil {
		t.Fatal("agentHandler 未构造")
	}
	if s.agentHandler.Authorize == nil {
		t.Fatal("AI Agent 未注入鉴权回调——六条路由将匿名可达")
	}

	mux := http.NewServeMux()
	agent.RegisterAgentRoutes(mux, s.agentHandler)

	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/ai/agent/execute", `{"input":"重启 web-01 的 nginx","tenantId":"victim","userId":"attacker"}`},
		{http.MethodPost, "/api/v1/ai/agent/classify", `{"input":"查一下 cpu"}`},
		{http.MethodPost, "/api/v1/ai/agent/decompose", `{"input":"排查并修复"}`},
		{http.MethodGet, "/api/v1/ai/agent/memory?sessionId=s1", ""},
		{http.MethodDelete, "/api/v1/ai/agent/memory?sessionId=s1", ""},
		{http.MethodGet, "/api/v1/ai/agent/tools", ""},
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
