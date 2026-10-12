package controlplane

// me_identity_backfill_test.go — A2 回归守卫（2026-10-12 模拟用户演练）。
//
// 症状（两个 agent 独立复现）：`GET /api/v1/me` 在**按文档带上 `X-Tenant-ID`** 时返回空壳
//   {"mode":"gateway-injected","roles":null,"tenantID":"default","userID":""}
// 而不带该头的同一请求反而正确回填 userID —— 同一进程两个 me 端点"各说各话"，
// 新人据此判定"我没登录成功/身份注入没接上"，审计员据此误解自己的权限。
//
// 根因：requireTenantContext 的"租户头非空"分支校验完租户一致性就 return，从不从令牌回填身份
//（"头空"分支才回填）。本守卫钉住"两条分支给出同一份身份"。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/store"
)

func meFixture(t *testing.T) (*Server, string) {
	t.Helper()
	secret := []byte("test-jwt-secret-for-a2-guard-32bytes!!")
	s := newExtraTestServer()
	s.jwtSecret = secret
	// userFromToken 会查库（token 有效但用户不存在应 401），故种子一个真实用户。
	if s.store.CreateUser(&store.User{
		ID: "u-42", Username: "alice", Email: "alice@example.com",
		TenantID: "t9", PasswordHash: "x", Status: "active",
		RoleIDs: []string{"role-operator"},
	}) == nil {
		t.Fatal("种子用户失败")
	}
	tok, err := authctx.SignJWT(authctx.JWTClaims{
		UserID: "u-42", Username: "alice", Roles: []string{"role-operator"},
		TenantID: "t9", ExpiresAt: time.Now().Add(time.Hour),
	}, secret)
	if err != nil {
		t.Fatalf("签 token: %v", err)
	}
	return s, tok
}

func callMe(t *testing.T, s *Server, tenantHeader, token string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if tenantHeader != "" {
		req.Header.Set("X-Tenant-ID", tenantHeader)
	}
	rec := httptest.NewRecorder()
	s.handleMe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleMe 应 200，实得 %d：%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析: %v", err)
	}
	return out
}

// TestHandleMe_SameIdentityWithAndWithoutTenantHeader 两条分支必须给出同一份身份（A2 核心判据）。
func TestHandleMe_SameIdentityWithAndWithoutTenantHeader(t *testing.T) {
	s, tok := meFixture(t)

	withHdr := callMe(t, s, "t9", tok)
	noHdr := callMe(t, s, "", tok)

	if withHdr["userID"] != "u-42" {
		t.Errorf("带租户头时 userID=%v，want u-42 —— 空 userID 会让用户判定\"没登录成功\"（A2 原形状）", withHdr["userID"])
	}
	if withHdr["userID"] != noHdr["userID"] {
		t.Errorf("带/不带租户头的 userID 不一致：%v vs %v（同一进程两个 me 端点各说各话）", withHdr["userID"], noHdr["userID"])
	}
	rolesWith, _ := withHdr["roles"].([]any)
	if len(rolesWith) == 0 {
		t.Errorf("带租户头时 roles=%v，应为令牌里的角色（roles:null 会让用户误以为毫无权限）", withHdr["roles"])
	}
	rolesNo, _ := noHdr["roles"].([]any)
	if len(rolesWith) != len(rolesNo) {
		t.Errorf("带/不带租户头的 roles 形状不一致：%v vs %v", withHdr["roles"], noHdr["roles"])
	}
	if withHdr["tenantID"] != "t9" {
		t.Errorf("tenantID=%v，want t9", withHdr["tenantID"])
	}
}

// TestHandleMe_HeaderTenantMismatchStillRejected 修复不得放宽安全判据：头里写的租户与令牌不一致必须 403。
func TestHandleMe_HeaderTenantMismatchStillRejected(t *testing.T) {
	s, tok := meFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Tenant-ID", "t-other") // 与令牌的 t9 不符
	rec := httptest.NewRecorder()
	s.handleMe(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("租户头与令牌不符应 403，实得 %d：%s", rec.Code, rec.Body.String())
	}
}
