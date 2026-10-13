package controlplane

// user_from_token_fallback_test.go — TD-60 切流身份回落守卫（2026-10-13 模拟用户团队演练批次 0 第 2 波）。
//
// 缺陷形态（三个模拟用户人设 + 协调者独立复现）：名册用户在对侧（auth-svc）登录后，
// **业务面五连全 401 `user not found`**——"能登录、能改密、能看 /me，活儿全干不了"；
// 该现象精确命中 runbook 的回退判据"任何 user not found ⇒ 停批回退"⇒ 开切即事故。
// 根因：userFromToken 严格按令牌 sub 查**本侧**库，而对侧签发的 sub 本侧不存在。
//
// 四条守卫：
//  ① 对侧形态令牌（sub 未知 / username 命中本侧 / roles 空）⇒ 业务面必须可用（回落生效）；
//  ② 双未知（sub 与 username 都查不到）⇒ 仍 401（回落不得把"查不到"变"放行"）；
//  ③ username 指向非 active 用户 ⇒ 不得放行（status 闸不放宽）；
//  ④ 本侧正常令牌 ⇒ 身份取自自己的 sub（回落分支不生效、不回归）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/store"
)

func cutoverFallbackServer(t *testing.T) (*Server, []byte) {
	t.Helper()
	secret := []byte("test-jwt-secret-for-cutover-fallback-32b!")
	s := newExtraTestServer()
	s.jwtSecret = secret
	if s.store.CreateUser(&store.User{
		ID: "local-u1", Username: "cutover-u1", Email: "u1@example.com",
		TenantID: "default", PasswordHash: "x", Status: "active",
		RoleIDs: []string{"role-admin"},
	}) == nil {
		t.Fatal("种子用户失败")
	}
	return s, secret
}

// farSideShapeToken 签一枚"对侧形态"令牌：sub 是本侧不存在的 id、roles 为空（对侧库里该用户无角色）。
func farSideShapeToken(t *testing.T, secret []byte, sub, username string) string {
	t.Helper()
	tok, err := authctx.SignJWT(authctx.JWTClaims{
		UserID: sub, Username: username, Roles: []string{},
		TenantID: "default", ExpiresAt: time.Now().Add(time.Hour),
	}, secret)
	if err != nil {
		t.Fatalf("签 token: %v", err)
	}
	return tok
}

func callDevices(t *testing.T, s *Server, token string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleDevices(rec, req)
	return rec.Code, rec.Body.String()
}

// TestUserFromToken_FarSideSessionResolvesLocally ①：切流后对侧会话在业务面必须可用。
func TestUserFromToken_FarSideSessionResolvesLocally(t *testing.T) {
	s, secret := cutoverFallbackServer(t)
	tok := farSideShapeToken(t, secret, "user-66b3a378-729a-42ac-ad80-0c220eb7f6bc", "cutover-u1")

	code, body := callDevices(t, s, tok)
	if code == http.StatusUnauthorized {
		t.Fatalf("对侧形态令牌被 401（切流后业务面全挂的原形状）：%s", body)
	}
	if code != http.StatusOK {
		t.Fatalf("回落命中后业务面应 200，实得 %d：%s", code, body)
	}
	// 身份必须是**本侧**记录（权限面同时恢复的关键：本侧权限取自本侧用户）。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Tenant-ID", "default")
	u, err := s.userFromToken(req)
	if err != nil {
		t.Fatalf("回落应解析出本侧身份：%v", err)
	}
	if u.ID != "local-u1" {
		t.Fatalf("回落身份应为本侧 local-u1（权限按本侧记录判），实得 %s", u.ID)
	}
}

// TestUserFromToken_DoubleUnknownStillUnauthorized ②：双未知必须仍 401。
func TestUserFromToken_DoubleUnknownStillUnauthorized(t *testing.T) {
	s, secret := cutoverFallbackServer(t)
	tok := farSideShapeToken(t, secret, "user-unknown-sub", "no-such-user")
	code, body := callDevices(t, s, tok)
	if code != http.StatusUnauthorized {
		t.Fatalf("sub 与 username 都查不到应 401，实得 %d：%s", code, body)
	}
	if !strings.Contains(body, "user not found") {
		t.Fatalf("401 文案应含 %q，实得：%s", "user not found", body)
	}
}

// TestUserFromToken_DisabledLocalUserNotRescued ③：username 指向非 active 用户不得放行。
func TestUserFromToken_DisabledLocalUserNotRescued(t *testing.T) {
	s, secret := cutoverFallbackServer(t)
	if s.store.CreateUser(&store.User{
		ID: "local-u2", Username: "cutover-disabled", Email: "u2@example.com",
		TenantID: "default", PasswordHash: "x", Status: "disabled",
		RoleIDs: []string{"role-admin"},
	}) == nil {
		t.Fatal("种子 disabled 用户失败")
	}
	tok := farSideShapeToken(t, secret, "user-unknown-sub-2", "cutover-disabled")
	code, body := callDevices(t, s, tok)
	if code == http.StatusOK {
		t.Fatalf("username 指向 disabled 用户竟被放行（status 闸被绕过）：%s", body)
	}
}

// TestUserFromToken_LocalTokenUnchanged ④：本侧正常令牌不回归（身份取自自身 sub）。
func TestUserFromToken_LocalTokenUnchanged(t *testing.T) {
	s, secret := cutoverFallbackServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer "+farSideShapeToken(t, secret, "local-u1", "cutover-u1"))
	req.Header.Set("X-Tenant-ID", "default")
	u, err := s.userFromToken(req)
	if err != nil {
		t.Fatalf("本侧令牌应解析成功：%v", err)
	}
	if u.ID != "local-u1" {
		t.Fatalf("身份应为 local-u1，实得 %s", u.ID)
	}
	// 序列化一下响应体形状，顺便保证上面 ① 的断言面对的是真实 handler JSON。
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req2.Header.Set("Authorization", "Bearer "+farSideShapeToken(t, secret, "local-u1", "cutover-u1"))
	req2.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleDevices(rec, req2)
	if rec.Code != http.StatusOK {
		t.Fatalf("本侧令牌业务面应 200，实得 %d：%s", rec.Code, rec.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("业务面响应应为 JSON：%v", err)
	}
}
