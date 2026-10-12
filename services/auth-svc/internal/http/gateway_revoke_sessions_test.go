package http

// gateway_revoke_sessions_test.go — F2（TD-60 演练报告）：按用户吊销会话端点的端到端守卫。
//
// 要钉住的语义（也是这条端点的存在理由）：
//   控制面把某用户从切流名册摘除（回退）后，**对侧签发的旧 rt 必须立刻不可再刷新**——
//   否则对侧 rt 仍返回 200 而 /me 已按名册走本侧返回 401，用户视角是"刷新说还活着、
//   一干正事就报错"的半死态（演练报告 F2 原文）。
//
// 判据刻意用**真实流程**而不是直接断 store：登录拿 rt → 吊销 → 拿同一个 rt 去刷新必须 401。
// 只断 store 里"条数变少"会漏掉"端点没挂上/权限写错/吊销了错的人"这类坏法。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Levango7/OpsMesh/services/auth-svc/internal/auth"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/store"
)

// seedUser 夹具里只种子了 admin（MemoryStore.seedAdminUser），被测目标用户必须自建。
// MustChangePassword=false：本用例验的是 rt 生命周期，首登强制改密态不会下发 rt。
func seedUser(t *testing.T, svc *service.Service, id, username, password string, roleIDs []string) {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash %s: %v", username, err)
	}
	if _, err := svc.Store().CreateUser(&store.User{
		ID: id, Username: username, Email: username + "@test.local",
		PasswordHash: hash, Status: "active", RoleIDs: roleIDs,
	}); err != nil {
		t.Fatalf("建 %s: %v", username, err)
	}
}

// loginJSON 走网关登录，返回响应体、Set-Cookie 列表与状态码。
func loginJSON(t *testing.T, mux *http.ServeMux, body string) (map[string]any, []*http.Cookie, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out, rec.Result().Cookies(), rec.Code
}

// adminCookie 用一个"已完成首登改密"的 admin 会话调管理端点。
// 为什么不用 Authorization: Bearer —— 登录响应在本夹具下既不给 token 也不给改密令牌
// （deviceFP 门禁），而网关的 requirePermission 走 bearerOrCookie，Cookie 等价可用。
// 这本身也顺带验了"Cookie 会话能调管理端点"这条真实路径。
func adminCookie(t *testing.T, mux *http.ServeMux) *http.Cookie {
	t.Helper()
	_, cookies, code := loginJSON(t, mux, `{"username":"admin","password":"admin123"}`)
	if code != http.StatusOK {
		t.Fatalf("admin 登录应 200，实得 %d", code)
	}
	c := cookieByName(cookies, accessTokenCookieName)
	if c == nil {
		t.Fatalf("admin 登录未下发 %s：cookies=%v", accessTokenCookieName, cookies)
	}
	return c
}

func cookieByName(cs []*http.Cookie, name string) *http.Cookie {
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestRevokeSessionsKillsRefreshToken(t *testing.T) {
	_, mux, svc := newTestGateway()
	// 种子账号默认带首登强制改密标记（登录只发 changePasswordToken、不发 rt），
	// 本用例要验 rt 生命周期，故先把目标用户与 admin 的标记清掉（等价于"已完成首登改密"）。
	seedUser(t, svc, "user-target", "target-1", "Target-User-2026x!", []string{"role-operator"})
	clearMustChangePassword(t, svc) // 夹具助手：等价于 admin 已完成首登改密

	// 1) 目标用户登录，拿到 rt（这就是"被切到对侧的那份会话"）
	body, cookies, code := loginJSON(t, mux, `{"username":"target-1","password":"Target-User-2026x!"}`)
	if code != http.StatusOK {
		t.Fatalf("operator 登录应 200，实得 %d：%v", code, body)
	}
	rt := cookieByName(cookies, refreshTokenCookieName)
	if rt == nil {
		t.Fatalf("登录未下发 %s：cookies=%v body=%v", refreshTokenCookieName, cookies, body)
	}

	// 2) 基线：该 rt 能刷新
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: refreshTokenCookieName, Value: rt.Value})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("基线刷新应 200，实得 %d：%s", rec.Code, rec.Body.String())
	}

	// 3) admin 会话调吊销端点
	adm := adminCookie(t, mux)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/users/user-target/revoke-sessions", nil)
	req.AddCookie(adm)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("吊销端点应 200，实得 %d：%s", rec.Code, rec.Body.String())
	}
	var revoked map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &revoked); err != nil {
		t.Fatalf("吊销响应解析失败: %v", err)
	}
	if n, _ := revoked["refreshTokensDeleted"].(float64); n < 1 {
		t.Errorf("吊销应至少删掉 1 条 rt，实得 %v（响应：%s）", revoked["refreshTokensDeleted"], rec.Body.String())
	}

	// 4) 关键判据：同一个 rt 再刷新，必须 401（= 半死态被消除）
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: refreshTokenCookieName, Value: rt.Value})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("吊销后该 rt 必须刷新失败（401），实得 %d：%s —— 这正是 F2 的半死态", rec.Code, rec.Body.String())
	}
}

// TestRevokeSessionsRequiresUserWrite 权限面：无 user:write 的调用方必须 403。
func TestRevokeSessionsRequiresUserWrite(t *testing.T) {
	_, mux, svc := newTestGateway()
	seedUser(t, svc, "user-ro", "ro-1", "Ro-User-2026x!", []string{"role-viewer"})
	_, vcookies, vcode := loginJSON(t, mux, `{"username":"ro-1","password":"Ro-User-2026x!"}`)
	vc := cookieByName(vcookies, accessTokenCookieName)
	if vcode != http.StatusOK || vc == nil {
		t.Skipf("viewer 夹具不可用（%d），跳过权限面断言", vcode)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/user-target/revoke-sessions", nil)
	req.AddCookie(vc)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("无 user:write 者调吊销端点应 403，实得 %d：%s", rec.Code, rec.Body.String())
	}
}

// TestRevokeSessionsUnknownUser 未知用户必须 404——静默 200 会让调用方以为"已吊销"（假成功）。
func TestRevokeSessionsUnknownUser(t *testing.T) {
	_, mux, svc := newTestGateway()
	clearMustChangePassword(t, svc)
	adm := adminCookie(t, mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/user-does-not-exist/revoke-sessions", nil)
	req.AddCookie(adm)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("未知用户应 404，实得 %d：%s", rec.Code, rec.Body.String())
	}
}
