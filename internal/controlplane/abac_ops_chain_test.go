// abac_ops_chain_test.go — ABAC 路由的真实中间件链回归（2026-11-17）。
//
// 背景：默认拒绝网（server_middleware.go:183 defaultDenyMiddleware）只做身份**校验**，
// 不把 authctx 注入请求 context（requireTenantContext 只返回 actx）。若 handleABAC
// 从 r.Context() 取租户，TenantID 恒为空：
//   - 设备租户守卫 dev.TenantID != "" 把一切带 deviceID 的合法请求判 403；
//   - policy-create 落库的镜像 TenantID 为空，被 abac_engine 当成全局策略（跨租户可见）。
//
// 本组用例走生产同款链路（defaultDenyMiddleware → mux → handleABAC）+ 真 JWT +
// 真 seedRBAC，钉住：匿名 401、同租户设备 200、跨租户设备 403、policy-create 需写
// 权限、策略镜像按租户隔离。
package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

const (
	abacTenantA = "abac-tenant-a"
	abacTenantB = "abac-tenant-b"
)

// newABACChainServer 构造生产语义控制面（requireAuth=true / demo=false），
// 返回同一份 defaultDenyMiddleware 包装，调用方不得绕过它直调 handler。
func newABACChainServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	cfg := &config.Config{
		Mode:                "controlplane",
		Store:               "memory",
		EventBus:            "noop",
		RequireAuth:         true,
		TrustGatewayHeaders: false,
		JWTSecret:           "abac-chain-jwt-secret-0123456789abcdef",
		AdminPassword:       "Abac-Chain-Passw0rd-2026!",
		GRPCPort:            0,
		HTTPPort:            0,
		MetricsPort:         0,
		TaskMaxRetries:      3,
	}
	s, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// 自建用户绑定预置角色（预置账号的 MustChangePassword 与本案无关）。
	for _, spec := range []struct{ id, roleID, tenant string }{
		{"abac-admin-a", "role-admin", abacTenantA},
		{"abac-operator-a", "role-operator", abacTenantA},
		{"abac-viewer-a", "role-viewer", abacTenantA},
		{"abac-admin-b", "role-admin", abacTenantB},
	} {
		if u := s.store.CreateUser(&store.User{
			ID: spec.id, Username: spec.id, PasswordHash: "unused-token-only",
			Status: "active", RoleIDs: []string{spec.roleID}, TenantID: spec.tenant,
		}); u == nil {
			t.Fatalf("建测试用户 %s 失败", spec.id)
		}
	}
	s.store.UpsertDevice(&proto.DeviceInfo{
		DeviceID: "abac-dev-a", TenantID: abacTenantA, Hostname: "host-a",
		OS: "linux", Arch: "amd64", State: "online", Managed: true,
	})
	s.store.UpsertDevice(&proto.DeviceInfo{
		DeviceID: "abac-dev-b", TenantID: abacTenantB, Hostname: "host-b",
		OS: "linux", Arch: "amd64", State: "online", Managed: true,
	})
	mux := http.NewServeMux()
	s.registerABACRoutes(mux)
	// 生产同款鉴权网（server_lifecycle.go:518）。
	return s, s.defaultDenyMiddleware(mux)
}

// abacChainToken 用服务端同一把 jwtSecret 签发，身份落在 store 的真实用户记录上。
func abacChainToken(t *testing.T, s *Server, userID string) string {
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
		TenantID: u.TenantID, ExpiresAt: time.Now().Add(time.Hour),
	}, s.jwtSecret)
	if err != nil {
		t.Fatalf("签 token: %v", err)
	}
	return tok
}

// abacChainCall 经真实中间件链发一次 POST /api/v1/abac。
func abacChainCall(h http.Handler, token, tenantID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/abac", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func abacEvalBody(deviceID string) string {
	return `{"deviceID":"` + deviceID + `","action":"evaluate","subject":"alice","resource":"vm:42","actionAttr":"read"}`
}

// 匿名请求必须被默认拒绝网 401（不是 handler 内某条分支的副作用）。
func TestABACChain_AnonymousUnauthorized(t *testing.T) {
	_, h := newABACChainServer(t)
	rec := abacChainCall(h, "", "", abacEvalBody("abac-dev-a"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("匿名 POST /api/v1/abac = %d, want 401 body=%s", rec.Code, rec.Body.String())
	}
}

// 租户 A 访问本租户设备：真链路上必须 200（修复前 actx.TenantID 为空 → 恒 403）。
func TestABACChain_EvaluateOwnDeviceAllowed(t *testing.T) {
	s, h := newABACChainServer(t)
	tok := abacChainToken(t, s, "abac-admin-a")
	rec := abacChainCall(h, tok, abacTenantA, abacEvalBody("abac-dev-a"))
	if rec.Code != http.StatusOK {
		t.Fatalf("同租户设备评估应 200，得到 %d body=%s", rec.Code, rec.Body.String())
	}
	resp := abacDecodeBody(t, rec)
	if resp["evaluatedBy"] != "server+device-context" {
		t.Fatalf("evaluatedBy=%v, want server+device-context", resp["evaluatedBy"])
	}
	if resp["decision"] != "deny" {
		t.Fatalf("无策略时应 default-deny，得到 decision=%v reason=%v", resp["decision"], resp["reason"])
	}
}

// 租户 A 访问租户 B 的设备：403 且不泄露决策/设备上下文。
func TestABACChain_EvaluateCrossTenantDeviceForbidden(t *testing.T) {
	s, h := newABACChainServer(t)
	tok := abacChainToken(t, s, "abac-admin-a")
	rec := abacChainCall(h, tok, abacTenantA, abacEvalBody("abac-dev-b"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("跨租户设备评估应 403，得到 %d body=%s", rec.Code, rec.Body.String())
	}
	resp := abacDecodeBody(t, rec)
	if _, exists := resp["decision"]; exists {
		t.Fatalf("403 不得包含决策字段: %s", rec.Body.String())
	}
	if _, exists := resp["deviceContext"]; exists {
		t.Fatalf("403 不得泄露设备上下文: %s", rec.Body.String())
	}
}

const abacPolicyCreateBody = `{"deviceID":"abac-dev-a","action":"policy-create","subject":"alice","resource":"vm:42","actionAttr":"read"}`

// abacPolicyBodyOn 同三元组（alice/vm:42/read）在指定设备/动作上的请求体。
func abacPolicyBodyOn(deviceID, action string) string {
	return `{"deviceID":"` + deviceID + `","action":"` + action + `","subject":"alice","resource":"vm:42","actionAttr":"read"}`
}

// policy-create 是写操作：viewer（仅 abac:read）必须 403，admin（全量）必须放行。
func TestABACChain_PolicyCreateRequiresWritePermission(t *testing.T) {
	s, h := newABACChainServer(t)

	viewer := abacChainToken(t, s, "abac-viewer-a")
	rec := abacChainCall(h, viewer, abacTenantA, abacPolicyCreateBody)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer policy-create 应 403，得到 %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "abac:write") {
		t.Fatalf("拒绝原因应指向 abac:write 权限点，得到 %s", rec.Body.String())
	}
	if n := len(s.listABACPolicies(abacTenantA)); n != 0 {
		t.Fatalf("被拒的 policy-create 不得落镜像，得到 %d 条", n)
	}

	admin := abacChainToken(t, s, "abac-admin-a")
	rec = abacChainCall(h, admin, abacTenantA, abacPolicyCreateBody)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("admin policy-create 应 202，得到 %d body=%s", rec.Code, rec.Body.String())
	}
	if n := len(s.listABACPolicies(abacTenantA)); n != 1 {
		t.Fatalf("admin policy-create 应落 1 条镜像，得到 %d 条", n)
	}
}

// 策略镜像必须落在 actx.TenantID（真租户）上：A 创建的策略不能给 B 的评估放行。
func TestABACChain_PolicyMirrorTenantScoped(t *testing.T) {
	s, h := newABACChainServer(t)
	adminA := abacChainToken(t, s, "abac-admin-a")
	if rec := abacChainCall(h, adminA, abacTenantA, abacPolicyCreateBody); rec.Code != http.StatusAccepted {
		t.Fatalf("admin A policy-create 应 202，得到 %d body=%s", rec.Code, rec.Body.String())
	}

	rec := abacChainCall(h, adminA, abacTenantA, abacEvalBody(""))
	if rec.Code != http.StatusOK {
		t.Fatalf("A 评估应 200，得到 %d body=%s", rec.Code, rec.Body.String())
	}
	if got := abacDecodeBody(t, rec)["decision"]; got != "allow" {
		t.Fatalf("A 应命中自己的策略，decision=%v body=%s", got, rec.Body.String())
	}

	adminB := abacChainToken(t, s, "abac-admin-b")
	rec = abacChainCall(h, adminB, abacTenantB, abacEvalBody(""))
	if rec.Code != http.StatusOK {
		t.Fatalf("B 评估应 200（默认拒绝不是 403），得到 %d body=%s", rec.Code, rec.Body.String())
	}
	if got := abacDecodeBody(t, rec)["decision"]; got != "deny" {
		t.Fatalf("A 的策略不得给 B 放行，decision=%v body=%s", got, rec.Body.String())
	}
}

// 读/写面拆分：evaluate/list 是咨询面（abac:read）——operator 与 viewer 都应可用；
// policy-create 改变决策面（abac:write）——两者都必须 403，admin 仍 202。
func TestABACChain_ReadEvaluateWriteSplit(t *testing.T) {
	s, h := newABACChainServer(t)

	for _, tc := range []struct{ user, role string }{
		{"abac-operator-a", "operator"},
		{"abac-viewer-a", "viewer"},
	} {
		tok := abacChainToken(t, s, tc.user)

		rec := abacChainCall(h, tok, abacTenantA, abacEvalBody(""))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s evaluate 应 200（abac:read），得到 %d body=%s", tc.role, rec.Code, rec.Body.String())
		}
		if got := abacDecodeBody(t, rec)["decision"]; got != "deny" {
			t.Fatalf("%s 无策略时 evaluate 应 default-deny，得到 decision=%v body=%s", tc.role, got, rec.Body.String())
		}

		rec = abacChainCall(h, tok, abacTenantA, abacPolicyCreateBody)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s policy-create 应 403（abac:write 仅 admin），得到 %d body=%s", tc.role, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "abac:write") {
			t.Fatalf("%s 的拒绝原因应指向 abac:write，得到 %s", tc.role, rec.Body.String())
		}
	}
	if n := len(s.listABACPolicies(abacTenantA)); n != 0 {
		t.Fatalf("被拒的 policy-create 不得落镜像，得到 %d 条", n)
	}

	admin := abacChainToken(t, s, "abac-admin-a")
	rec := abacChainCall(h, admin, abacTenantA, abacPolicyCreateBody)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("admin policy-create 应 202，得到 %d body=%s", rec.Code, rec.Body.String())
	}
	if n := len(s.listABACPolicies(abacTenantA)); n != 1 {
		t.Fatalf("admin policy-create 应落 1 条镜像，得到 %d 条", n)
	}
}

// 镜像键的租户分量：两个租户创建**完全相同**的三元组时互不覆盖；
// 删除 A 的策略不得移除 B 的镜像（B 的评估仍按自己的策略放行）。
func TestABACChain_PolicyMirrorKeyTenantScoped(t *testing.T) {
	s, h := newABACChainServer(t)
	adminA := abacChainToken(t, s, "abac-admin-a")
	adminB := abacChainToken(t, s, "abac-admin-b")

	if rec := abacChainCall(h, adminA, abacTenantA, abacPolicyCreateBody); rec.Code != http.StatusAccepted {
		t.Fatalf("admin A policy-create 应 202，得到 %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := abacChainCall(h, adminB, abacTenantB, abacPolicyBodyOn("abac-dev-b", "policy-create")); rec.Code != http.StatusAccepted {
		t.Fatalf("admin B 同三元组 policy-create 应 202，得到 %d body=%s", rec.Code, rec.Body.String())
	}
	if na, nb := len(s.listABACPolicies(abacTenantA)), len(s.listABACPolicies(abacTenantB)); na != 1 || nb != 1 {
		t.Fatalf("同三元组应按租户各存一条镜像，得到 A=%d B=%d", na, nb)
	}

	decisionOf := func(tok, tenant string) interface{} {
		rec := abacChainCall(h, tok, tenant, abacEvalBody(""))
		if rec.Code != http.StatusOK {
			t.Fatalf("evaluate 应 200，得到 %d body=%s", rec.Code, rec.Body.String())
		}
		return abacDecodeBody(t, rec)["decision"]
	}
	if got := decisionOf(adminA, abacTenantA); got != "allow" {
		t.Fatalf("A 应命中自己的策略，decision=%v", got)
	}
	if got := decisionOf(adminB, abacTenantB); got != "allow" {
		t.Fatalf("B 应命中自己的策略，decision=%v", got)
	}

	// A 删除同一三元组：只应移除 A 的镜像。
	if rec := abacChainCall(h, adminA, abacTenantA, abacPolicyBodyOn("abac-dev-a", "policy-delete")); rec.Code != http.StatusAccepted {
		t.Fatalf("admin A policy-delete 应 202，得到 %d body=%s", rec.Code, rec.Body.String())
	}
	if na, nb := len(s.listABACPolicies(abacTenantA)), len(s.listABACPolicies(abacTenantB)); na != 0 || nb != 1 {
		t.Fatalf("删除 A 后镜像数应为 A=0 B=1，得到 A=%d B=%d", na, nb)
	}
	if got := decisionOf(adminA, abacTenantA); got != "deny" {
		t.Fatalf("A 删除自己的策略后应回到 default-deny，decision=%v", got)
	}
	if got := decisionOf(adminB, abacTenantB); got != "allow" {
		t.Fatalf("删除 A 的策略不得影响 B 的镜像，decision=%v", got)
	}
}
