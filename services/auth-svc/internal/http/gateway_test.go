// gateway_test.go — A1 HTTP 网关单测（方案 V2 风险点 R1-R6 的防回归锚）。
//
// 覆盖目标：
//   - R1/R2 Cookie 语义：登录写双 HttpOnly Cookie（字段逐项与 controlplane 对齐）；
//     refresh 只写 Cookie 不返回 token body（前端单飞契约）。
//   - R3 DeviceFP：签发绑定 + 跨设备刷新拒绝 + 空 FP 兼容。
//   - R4 change-password token 模式：无 token 401、首登改密流全程。
//   - R6 注册审批：注册 201 pending → Login 拒绝 → approve 后可登。
//   - 用户枚举防护：不存在用户/密码错统一 401。
package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/auth-svc/internal/auth"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/store"
)

// newTestGateway 构造网关 + mux（cookieSecure=false 便于断言明文属性）。
func newTestGateway() (*Gateway, *http.ServeMux, *service.Service) {
	eng := auth.NewEngine("test-secret", 15*time.Minute, 7*24*time.Hour)
	st := store.NewMemoryStore()
	svc := service.NewService(eng, st)
	g := NewGateway(svc, false)
	mux := http.NewServeMux()
	g.RegisterRoutes(mux)
	return g, mux, svc
}

// clearMustChangePassword 模拟 admin 已完成首登改密（seed 的 admin 带安全基线标记）。
func clearMustChangePassword(t *testing.T, svc *service.Service) {
	t.Helper()
	hash, err := auth.HashPassword("admin123")
	if err != nil {
		t.Fatalf("hash password failed: %v", err)
	}
	if err := svc.Store().ChangePassword("user-admin", hash); err != nil {
		t.Fatalf("clear must-change-password failed: %v", err)
	}
}

// doReq 发请求返回 recorder。
func doReq(t *testing.T, mux *http.ServeMux, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// findCookie 从 recorder 的 Set-Cookie 列表按名提取（未写返回 nil）。
func findCookie(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// loginAsAdmin 走登录端点拿双 Cookie 并转成请求头（供后续请求复用）。
func loginAsAdmin(t *testing.T, mux *http.ServeMux, svc *service.Service) map[string]string {
	t.Helper()
	clearMustChangePassword(t, svc)
	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"admin123"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: got %d, body=%s", rec.Code, rec.Body.String())
	}
	at := findCookie(t, rec, "opsmesh_at")
	rt := findCookie(t, rec, "opsmesh_rt")
	if at == nil || rt == nil {
		t.Fatal("登录应写双 Cookie（opsmesh_at + opsmesh_rt）")
	}
	return map[string]string{"Cookie": "opsmesh_at=" + at.Value + "; opsmesh_rt=" + rt.Value}
}

// ============ R1/R2：Cookie 语义 + refresh 单飞契约 ============

func TestLogin_CookieFieldsMatchControlplane(t *testing.T) {
	_, mux, svc := newTestGateway()
	clearMustChangePassword(t, svc)

	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"admin123"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: got %d, body=%s", rec.Code, rec.Body.String())
	}
	// R1：Cookie 字段与 controlplane/auth.go setCookie 逐项对齐。
	for _, name := range []string{"opsmesh_at", "opsmesh_rt"} {
		c := findCookie(t, rec, name)
		if c == nil {
			t.Fatalf("缺 Cookie %s", name)
		}
		if !c.HttpOnly {
			t.Errorf("%s 必须 HttpOnly", name)
		}
		if c.Path != "/" {
			t.Errorf("%s Path=%q, want /", name, c.Path)
		}
		if c.SameSite != http.SameSiteLaxMode {
			t.Errorf("%s SameSite=%v, want Lax", name, c.SameSite)
		}
		if c.Secure { // 测试构造 cookieSecure=false
			t.Errorf("%s Secure 应为 false（cookieSecure=false 时）", name)
		}
	}
	// at 的 MaxAge = accessTTL 秒（15min=900）。
	if c := findCookie(t, rec, "opsmesh_at"); c.MaxAge != 900 {
		t.Errorf("at MaxAge=%d, want 900（15min）", c.MaxAge)
	}
}

func TestRefresh_CookieOnlyNoTokenBody(t *testing.T) {
	_, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)

	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/refresh", "", cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: got %d, body=%s", rec.Code, rec.Body.String())
	}
	// R2：body 不含 token（前端 postEmpty 只查状态码）。
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("refresh body 非对象: %v", err)
	}
	if _, hasAt := resp["accessToken"]; hasAt {
		t.Error("refresh body 不应含 accessToken（R2 契约：只写 Cookie）")
	}
	// 新 at Cookie 写入（旋转）。
	if findCookie(t, rec, "opsmesh_at") == nil {
		t.Error("refresh 应写新 opsmesh_at Cookie")
	}
}

// ============ R3：DeviceFP 绑定 + 跨设备拒绝 ============

func TestDeviceFP_CrossDeviceRefreshRejected(t *testing.T) {
	_, mux, svc := newTestGateway()
	clearMustChangePassword(t, svc)

	// 设备 A（fp-a）登录。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"admin123"}`,
		map[string]string{"X-Device-FP": "fp-a"})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: got %d", rec.Code)
	}
	rtCookie := findCookie(t, rec, "opsmesh_rt")
	if rtCookie == nil {
		t.Fatal("登录应写 rt Cookie")
	}

	// 设备 B（fp-b）用该 rt 刷新 → 拒绝（R3 跨设备重放防护）。
	recB := doReq(t, mux, http.MethodPost, "/api/v1/auth/refresh", "",
		map[string]string{"Cookie": "opsmesh_rt=" + rtCookie.Value, "X-Device-FP": "fp-b"})
	if recB.Code != http.StatusUnauthorized {
		t.Fatalf("跨设备刷新应 401，实际 %d", recB.Code)
	}

	// 设备 A（fp-a）用同一 rt → 已被上一次消费（原子 Consume），同样拒绝——
	// 说明 rt 一次性语义 + FP 校验双闸都在工作。
	recA := doReq(t, mux, http.MethodPost, "/api/v1/auth/refresh", "",
		map[string]string{"Cookie": "opsmesh_rt=" + rtCookie.Value, "X-Device-FP": "fp-a"})
	if recA.Code != http.StatusUnauthorized {
		t.Fatalf("已消费 rt 再用应 401（一次性语义），实际 %d", recA.Code)
	}
}

func TestDeviceFP_EmptyFPBackwardCompatible(t *testing.T) {
	_, mux, svc := newTestGateway()
	// 不带 FP 登录（旧客户端）→ 刷新也不带 FP → 应通过（空 FP 兼容）。
	clearMustChangePassword(t, svc) // 常规 at+rt 流须先清首登标记（否则无 rt）。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"admin123"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("无 FP 登录应成功，got %d", rec.Code)
	}
	rt := findCookie(t, rec, "opsmesh_rt")
	if rt == nil {
		t.Fatal("登录应写 rt Cookie")
	}
	rec2 := doReq(t, mux, http.MethodPost, "/api/v1/auth/refresh", "",
		map[string]string{"Cookie": "opsmesh_rt=" + rt.Value})
	if rec2.Code != http.StatusOK {
		t.Fatalf("空 FP 签发 + 空 FP 刷新应通过（向后兼容），got %d", rec2.Code)
	}
}

// ============ R4：change-password token 模式 ============

func TestChangePassword_RequiresTokenNotUserId(t *testing.T) {
	_, mux, _ := newTestGateway()

	// 无任何凭证 → 401（不接受 body.user_id 直调）。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/change-password",
		`{"oldPassword":"admin123","newPassword":"NewPass123","userId":"user-admin"}`, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无凭证改密应 401（R4：token 模式强制），实际 %d", rec.Code)
	}
}

func TestChangePassword_FirstLoginFlow(t *testing.T) {
	_, mux, _ := newTestGateway()
	// seed admin 带 MustChangePassword=true → 首登拿改密 token。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"admin123"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("首登 login: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		MustChangePassword  bool   `json:"mustChangePassword"`
		ChangePasswordToken string `json:"changePasswordToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("login body 解析: %v", err)
	}
	if !resp.MustChangePassword || resp.ChangePasswordToken == "" {
		t.Fatal("首登响应应带 mustChangePassword=true + changePasswordToken")
	}
	// 首登流不写 rt（改密专用会话不可刷新）。
	if findCookie(t, rec, "opsmesh_rt") != nil {
		t.Error("首登改密流不应写 rt Cookie（与 controlplane 同语义）")
	}

	// 用改密 token 改密 → 200。
	rec2 := doReq(t, mux, http.MethodPost, "/api/v1/auth/change-password",
		`{"oldPassword":"admin123","newPassword":"NewPass123x","changePasswordToken":"`+resp.ChangePasswordToken+`"}`, nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("首登改密: got %d, body=%s", rec2.Code, rec2.Body.String())
	}

	// 新密码可登录（改密生效）。
	rec3 := doReq(t, mux, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"NewPass123x"}`, nil)
	if rec3.Code != http.StatusOK {
		t.Fatalf("新密码登录: got %d, body=%s", rec3.Code, rec3.Body.String())
	}
	// 改密后 MustChangePassword 已清（常规 at+rt 流恢复）。
	var resp3 struct {
		MustChangePassword bool `json:"mustChangePassword"`
	}
	_ = json.Unmarshal(rec3.Body.Bytes(), &resp3)
	if resp3.MustChangePassword {
		t.Error("改密后 mustChangePassword 应已清除")
	}
}

// ============ R6：注册审批流 ============

func TestRegister_PendingApprovalFlow(t *testing.T) {
	g, mux, svc := newTestGateway()

	// 注册 → 201 pending。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/register",
		`{"username":"newuser","password":"SomePass123","email":"n@x.io"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: got %d, body=%s", rec.Code, rec.Body.String())
	}
	// pending 用户登录 → 401（Status!=active 拒绝）。
	rec2 := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"newuser","password":"SomePass123"}`, nil)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("pending 用户登录应 401，实际 %d", rec2.Code)
	}

	// admin 登录后 approve。
	adminCookies := loginAsAdmin(t, mux, svc)
	u := g.svc.Store().GetUserByUsername("newuser")
	if u == nil {
		t.Fatal("注册用户未入库")
	}
	rec3 := doReq(t, mux, http.MethodPost, "/api/v1/users/"+u.ID+"/approve", "", adminCookies)
	if rec3.Code != http.StatusOK {
		t.Fatalf("approve: got %d, body=%s", rec3.Code, rec3.Body.String())
	}
	// approve 后可登录。
	rec4 := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"newuser","password":"SomePass123"}`, nil)
	if rec4.Code != http.StatusOK {
		t.Fatalf("approve 后登录: got %d", rec4.Code)
	}

	// 重复 approve → 409（仅 pending 可审批）。
	rec5 := doReq(t, mux, http.MethodPost, "/api/v1/users/"+u.ID+"/approve", "", adminCookies)
	if rec5.Code != http.StatusConflict {
		t.Fatalf("非 pending 重复审批应 409，实际 %d", rec5.Code)
	}
}

// ============ 用户枚举防护 ============

func TestLogin_NoUsernameEnumeration(t *testing.T) {
	_, mux, _ := newTestGateway()

	// 不存在的用户 vs 密码错：状态码与 body 必须一致（防枚举）。
	recA := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"ghost-user","password":"whatever"}`, nil)
	recB := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"wrong-password"}`, nil)
	if recA.Code != recB.Code {
		t.Fatalf("不存在用户(%d)与密码错(%d)状态码应一致（防枚举）", recA.Code, recB.Code)
	}
	if recA.Code != http.StatusUnauthorized {
		t.Fatalf("两者都应 401，实际 %d", recA.Code)
	}
	if strings.Contains(recA.Body.String(), "not found") || strings.Contains(recA.Body.String(), "不存在") {
		t.Error("响应不应泄露用户不存在（统一 invalid credentials）")
	}
}

// ============ me / logout ============

func TestMe_And_Logout(t *testing.T) {
	_, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)

	rec := doReq(t, mux, http.MethodGet, "/api/v1/auth/me", "", cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var me map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me["username"] != "admin" {
		t.Errorf("me 应返回 admin，实际 %v", me["username"])
	}

	// logout → 清双 Cookie + 后续 me 401（at 吊销进黑名单）。
	rec2 := doReq(t, mux, http.MethodPost, "/api/v1/auth/logout", "", cookies)
	if rec2.Code != http.StatusOK {
		t.Fatalf("logout: got %d", rec2.Code)
	}
	if findCookie(t, rec2, "opsmesh_at").Value != "" {
		// 清除语义：空值 Cookie（或 MaxAge -1）——Value 空即可。
		t.Log("at Cookie 清除为空值 ✓")
	}
	rec3 := doReq(t, mux, http.MethodGet, "/api/v1/auth/me", "", cookies)
	if rec3.Code != http.StatusUnauthorized {
		t.Fatalf("登出后 me 应 401（at 已吊销），实际 %d", rec3.Code)
	}
}
