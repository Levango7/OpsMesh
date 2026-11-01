package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/cost"
	"github.com/Levango7/OpsMesh/internal/knowledge"
)

// G1 收口批（2026-11-02）的接线回归：knowledge 8 条、cost 8 条、aiops 6 条必须由
// NewServer 注入鉴权回调，匿名请求一律 401。
//
// 三族各自的漏法不同：
//   - knowledge/cost：包内压根没有鉴权闸（与 ai/agent、ai/gateway 同族）；
//   - aiops：包里早就写好了 SetAuthorize，但全仓没有任何调用点（grep 'SetAuthorize'
//     只命中定义处），authz==nil 时 authorize() 原样放行 ⇒ 六条端点完全匿名，
//     其中 POST /aiops/execute 会把修复方案直接下发成 agent 任务。
//
// 权限点（aiops:*/knowledge:*/cost:*）同批进目录，且由
// internal/store/rbac_perm_source_guard_test.go 的源码扫描守护；接线与目录必须同批，
// 只做其一要么仍然匿名（不接线），要么对全部角色恒 403（不补目录）。
func TestG1RoutesAreAuthorized(t *testing.T) {
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
	if s.knowledgeHandler == nil || s.costHandler == nil || s.aiopsHandler == nil {
		t.Fatal("knowledge/cost/aiops handler 未构造")
	}
	if s.knowledgeHandler.Authorize == nil {
		t.Fatal("knowledge 未注入鉴权回调——8 条路由将匿名可达")
	}
	if s.costHandler.Authorize == nil {
		t.Fatal("cost 未注入鉴权回调——8 条路由将匿名可达")
	}

	mux := http.NewServeMux()
	knowledge.RegisterKnowledgeRoutes(mux, s.knowledgeHandler)
	cost.RegisterCostRoutes(mux, s.costHandler)
	s.aiopsHandler.RegisterRoutes(mux)

	cases := []struct{ method, path, body string }{
		// knowledge 8 条
		{http.MethodPost, "/api/v1/knowledge/sync", `{}`},
		{http.MethodPost, "/api/v1/knowledge/search", `{"query":"cpu"}`},
		{http.MethodPost, "/api/v1/knowledge/ingest", `{"documents":[{"id":"d1","content":"投毒"}]}`},
		{http.MethodGet, "/api/v1/knowledge/docs", ""},
		{http.MethodDelete, "/api/v1/knowledge/docs/d1", ""},
		{http.MethodGet, "/api/v1/knowledge/stats", ""},
		{http.MethodGet, "/api/v1/knowledge/sync-configs", ""},
		{http.MethodPut, "/api/v1/knowledge/sync-configs/cmdb", `{"enabled":false}`},
		// cost 8 条
		{http.MethodGet, "/api/v1/cost/budget?tenantId=victim", ""},
		{http.MethodPost, "/api/v1/cost/budget", `{"tenantId":"victim","daily":1,"monthly":1,"threshold":0.01}`},
		{http.MethodGet, "/api/v1/cost/alerts?tenantId=victim", ""},
		{http.MethodGet, "/api/v1/cost/report?tenantId=victim", ""},
		{http.MethodGet, "/api/v1/cost/routes", ""},
		{http.MethodPost, "/api/v1/cost/routes", `{"rules":[{"taskType":"chat","model":"劫持"}]}`},
		{http.MethodPost, "/api/v1/cost/route", `{"taskType":"chat"}`},
		{http.MethodPost, "/api/v1/cost/report/generate", `{"title":"t"}`},
		// aiops 6 条
		{http.MethodPost, "/api/v1/aiops/analyze", `{"alertId":"a1"}`},
		{http.MethodPost, "/api/v1/aiops/remediate", `{"alertId":"a1"}`},
		{http.MethodPost, "/api/v1/aiops/remediate-nl", `{"text":"重启 nginx"}`},
		{http.MethodPost, "/api/v1/aiops/execute", `{"planId":"p1"}`},
		{http.MethodGet, "/api/v1/aiops/status", ""},
		{http.MethodGet, "/api/v1/aiops/executions", ""},
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
