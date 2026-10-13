package controlplane

// cutover_change_password_body_test.go — 主动改密转发的 **body 还原**守卫（2026-10-13 端到端复演实测）。
//
// 症状：已登录的迁移用户（名册用户）主动改密 → 用户拿 502；cp.log 一行定案：
//   `cutover 转发 auth-svc 失败: net/http: HTTP/1.x transport connection broken:
//    http: ContentLength=111 with Body length 0`
// 根因：handler 已按 JSON 解析消费 body，而"已登录主动改密"分支的转发
//（cutoverForwardByToken）**漏了转发前还原**——登录口与首登改密分支当年都修过同一形状，
// 这里漏了。判据＝转发必须真的到达对侧（hits==1）且不 502：body 丢失时 ReverseProxy
// 在发出前就报错，对侧一次都收不到。
//
// 变异（2026-10-13 实测）：把该分支的 rawBody 换回 nil／去掉 restore ⇒ 本守卫判红（hits=0）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/authctx"
)

func TestCutoverChangePassword_ForwardRestoresBody(t *testing.T) {
	const name = "cutover-cpw"
	s, hits, _ := cutoverTestServer(t, name)
	s.jwtSecret = []byte("test-jwt-secret-cutover-cpw-32bytes!!")
	rosterUser(t, s, name)

	tok, err := authctx.SignJWT(authctx.JWTClaims{
		UserID: "local-ignored", Username: name, Roles: []string{"role-admin"},
		TenantID: "default", ExpiresAt: time.Now().Add(time.Hour),
	}, s.jwtSecret)
	if err != nil {
		t.Fatalf("签 token: %v", err)
	}

	// 不带 changePasswordToken ⇒ 走"已登录主动改密"分支（本守卫的目标分支）。
	body := `{"oldPassword":"Passw0rd!cutover","newPassword":"Passw0rd!cutover2"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/change-password", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleAuthChangePassword(rec, req)

	if hits.Load() == 0 {
		t.Fatalf("名册用户的主动改密没有到达对侧（body 未还原时 ReverseProxy 因 ContentLength≠实际 body 在发出前就报错，对侧一次都收不到）——code=%d body=%s",
			rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusBadGateway {
		t.Fatalf("转发 502（ContentLength with Body length 0 形状）：%s", rec.Body.String())
	}
}
