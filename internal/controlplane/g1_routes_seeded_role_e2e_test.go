// g1_routes_seeded_role_e2e_test.go 补的是单元/接线用例覆盖不到的那一层。
//
// 接线用例证明「handler 会核对权限点」「不注入回调就匿名可达」，权限目录守护证明
// 「被引用的权限点在目录里」。但 JWT 路径的真实判定来自 store 里的 role 行
// （requirePermission → userPermissions → store.GetRole(rid).Permissions，由 seedRBAC
// 从 RolePermissions() 派生落盘）。这三段中间任何一环掉链子，端点就对 admin 恒 403——
// 2026-09-29 那批 15 项缺口就是这个形状（当时是靠 sim 手工取证才发现的）。
// 本用例用真实 NewServer + 真 seedRBAC + 真 HS256 验签把四族端点与 flux 各打一遍。
package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	aigateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/cost"
	"github.com/Levango7/OpsMesh/internal/knowledge"
	"github.com/Levango7/OpsMesh/internal/store"
)

const g1Tenant = "default"

func newG1E2EServer(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()
	cfg := &config.Config{
		Mode:                "controlplane",
		Store:               "memory",
		EventBus:            "noop",
		RequireAuth:         true,
		TrustGatewayHeaders: false,
		JWTSecret:           "g1-e2e-jwt-secret-0123456789abcdef",
		AdminPassword:       "G1-E2e-Strong-Passw0rd-2026!",
		GRPCPort:            0,
		HTTPPort:            0,
		MetricsPort:         0,
		TaskMaxRetries:      3,
	}
	s, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// 自建用户绑定预置角色：预置 admin/operator/viewer 的 MustChangePassword 属凭据加固语义，
	// 与本案要证的「角色权限展开」无关，混在一起会让改密规则的任何调整都变成这里的假红。
	for _, spec := range []struct{ id, roleID string }{
		{"g1-e2e-admin", "role-admin"},
		{"g1-e2e-operator", "role-operator"},
		{"g1-e2e-viewer", "role-viewer"},
	} {
		if u := s.store.CreateUser(&store.User{ID: spec.id, Username: spec.id, PasswordHash: "unused-token-only", Status: "active", RoleIDs: []string{spec.roleID}, TenantID: g1Tenant}); u == nil {
			t.Fatalf("建测试用户 %s 失败", spec.id)
		}
	}

	mux := http.NewServeMux()
	knowledge.RegisterKnowledgeRoutes(mux, s.knowledgeHandler)
	cost.RegisterCostRoutes(mux, s.costHandler)
	aigateway.RegisterAIGatewayRoutes(mux, s.aiGatewayHandler)
	s.aiopsHandler.RegisterRoutes(mux)
	// flux 的路由在生产里由 Start() 注册；这里直接挂同一个 handler，
	// 目的是证 flux:read/flux:write 真的能从目录走到 role 行再走到这道闸。
	mux.HandleFunc("GET /api/v1/flux/apps", s.handleListFluxApps)
	return s, mux
}

// g1Token 用服务端同一把 jwtSecret 签发，身份落在 store 的真实用户记录上。
func g1Token(t *testing.T, s *Server, userID string) string {
	t.Helper()
	u := s.store.GetUser(userID)
	if u == nil {
		t.Fatalf("用户 %s 不存在", userID)
	}
	if u.MustChangePassword {
		t.Fatalf("测试用户 %s 带强制改密标记，本用例的身份前置不成立", userID)
	}
	tok, err := authctx.SignJWT(authctx.JWTClaims{
		UserID: u.ID, Username: u.Username, Roles: u.RoleIDs,
		TenantID: g1Tenant, ExpiresAt: time.Now().Add(time.Hour),
	}, s.jwtSecret)
	if err != nil {
		t.Fatalf("签 token: %v", err)
	}
	return tok
}

func g1Call(mux *http.ServeMux, token, method, path, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Tenant-ID", g1Tenant)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

// 三个角色各自的可达面：read 族对 admin/operator/viewer 都通，write 族只通 admin。
func TestG1RoutesReachableBySeededRoles(t *testing.T) {
	s, mux := newG1E2EServer(t)
	admin := g1Token(t, s, "g1-e2e-admin")
	operator := g1Token(t, s, "g1-e2e-operator")
	viewer := g1Token(t, s, "g1-e2e-viewer")

	// 「不得被拒」= 既不能 401（身份链断）也不能 403（权限点没落进角色）。
	// 其余状态码是业务侧结果（如 aiops 引擎未启用回 503），本用例不管。
	mustPass := func(tok, who, method, path, body string) {
		t.Helper()
		rec := g1Call(mux, tok, method, path, body)
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
			t.Errorf("%s %s %s = %d（权限点没从目录走到 role 行）body=%s", who, method, path, rec.Code, rec.Body.String())
		}
	}
	mustDeny := func(tok, who, method, path, body string) {
		t.Helper()
		rec := g1Call(mux, tok, method, path, body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s %s = %d, want 403（该角色不该持写权限）body=%s", who, method, path, rec.Code, rec.Body.String())
		}
	}

	// admin：四族 + flux 全通
	mustPass(admin, "admin", http.MethodGet, "/api/v1/flux/apps", "")
	mustPass(admin, "admin", http.MethodPost, "/api/v1/knowledge/ingest", `{"documents":[{"id":"e2e-1","content":"c","embedding":[1]}]}`)
	mustPass(admin, "admin", http.MethodPost, "/api/v1/knowledge/sync", `{}`)
	mustPass(admin, "admin", http.MethodGet, "/api/v1/cost/budget", "")
	mustPass(admin, "admin", http.MethodPost, "/api/v1/cost/budget", `{"daily":100,"monthly":1000,"threshold":0.8,"timezone":"Asia/Shanghai"}`)
	mustPass(admin, "admin", http.MethodPost, "/api/v1/ai/rag/ingest", `{"documents":[{"id":"e2e-1","content":"c","embedding":[1]}]}`)
	mustPass(admin, "admin", http.MethodGet, "/api/v1/ai/usage", "")
	mustPass(admin, "admin", http.MethodPost, "/api/v1/ai/prompts", `{"id":"e2e-p1","content":"c"}`)
	mustPass(admin, "admin", http.MethodGet, "/api/v1/aiops/status", "")

	// operator：四族 read 通（operatorReadOnlyGroups），write 一律 403
	for _, p := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/flux/apps"},
		{http.MethodGet, "/api/v1/knowledge/docs"},
		{http.MethodGet, "/api/v1/cost/budget"},
		{http.MethodGet, "/api/v1/ai/prompts"},
		{http.MethodGet, "/api/v1/ai/health"},
	} {
		mustPass(operator, "operator", p.method, p.path, "")
	}
	mustDeny(operator, "operator", http.MethodPost, "/api/v1/knowledge/ingest", `{"documents":[{"id":"e2e-2","content":"c","embedding":[1]}]}`)
	mustDeny(operator, "operator", http.MethodPost, "/api/v1/cost/budget", `{"daily":1,"monthly":1,"threshold":0.5}`)
	mustDeny(operator, "operator", http.MethodPost, "/api/v1/ai/rag/ingest", `{"documents":[{"id":"e2e-2","content":"c","embedding":[1]}]}`)
	mustDeny(operator, "operator", http.MethodDelete, "/api/v1/knowledge/docs/e2e-1", "")

	// viewer：read 通、write 拒（与 operator 同一写面判据，read 面另含 cost:read）
	mustPass(viewer, "viewer", http.MethodGet, "/api/v1/knowledge/docs", "")
	mustPass(viewer, "viewer", http.MethodGet, "/api/v1/cost/report", "")
	mustPass(viewer, "viewer", http.MethodGet, "/api/v1/ai/prompts", "")
	mustDeny(viewer, "viewer", http.MethodPost, "/api/v1/ai/prompts", `{"id":"e2e-p2","content":"c"}`)
	mustDeny(viewer, "viewer", http.MethodPost, "/api/v1/knowledge/sync", `{}`)

	// 反空断言：上面 mustPass 若因为「整片都放行」而全绿，这里会红——
	// 不带身份必须仍然 401，写面必须仍然拒特定角色。
	for _, p := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/flux/apps"},
		{http.MethodPost, "/api/v1/knowledge/ingest"},
		{http.MethodGet, "/api/v1/cost/budget"},
		{http.MethodPost, "/api/v1/ai/rag/ingest"},
	} {
		rec := g1Call(mux, "", p.method, p.path, `{"documents":[{"id":"x","content":"c","embedding":[1]}]}`)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("匿名 %s %s = %d, want 401 body=%s", p.method, p.path, rec.Code, rec.Body.String())
		}
	}

	// cost 的租户口径同样要真的生效：viewer 用 ?tenantId= 指名别家，读到的必须是自己的。
	rec := g1Call(mux, viewer, http.MethodGet, "/api/v1/cost/budget?tenantId=victim", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer 读预算应 200，got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"tenantId":"`+g1Tenant+`"`) {
		t.Fatalf("预算必须落在已验签租户上，body=%s", rec.Body.String())
	}
}

// flux 专项回归：目录补齐前 /api/v1/flux/apps 对含 admin 在内的全部角色恒 403，
// 前端 /flux 页（requirePerm 'flux:read'）因此是死路由。这条把「admin 打得开」钉住。
func TestFluxAppsReachableByAdmin(t *testing.T) {
	s, mux := newG1E2EServer(t)
	rec := g1Call(mux, g1Token(t, s, "g1-e2e-admin"), http.MethodGet, "/api/v1/flux/apps", "")
	if rec.Code == http.StatusForbidden && strings.Contains(rec.Body.String(), "flux:read") {
		t.Fatalf("flux:read 又不在角色权限里了（权限目录缺项的经典形状）：%s", rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("admin 列 flux 应用应 200，got %d body=%s", rec.Code, rec.Body.String())
	}
}
