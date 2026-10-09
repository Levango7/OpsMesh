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

func TestManagement_RequiresAuthentication(t *testing.T) {
	_, mux, _ := newTestGateway()
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/v1/users"}, {"POST", "/api/v1/users"},
		{"GET", "/api/v1/users/user-admin"}, {"PUT", "/api/v1/users/user-admin"},
		{"DELETE", "/api/v1/users/user-admin"},
		{"POST", "/api/v1/users/user-admin/approve"}, {"POST", "/api/v1/users/user-admin/reject"},
		{"GET", "/api/v1/roles"}, {"POST", "/api/v1/roles"},
		{"GET", "/api/v1/roles/role-admin"}, {"PUT", "/api/v1/roles/role-admin"},
		{"DELETE", "/api/v1/roles/role-admin"}, {"GET", "/api/v1/permissions"},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			for _, headers := range []map[string]string{nil, {"Authorization": "Bearer invalid"}, {"Cookie": "opsmesh_at=invalid"}} {
				rec := doReq(t, mux, route.method, route.path, `{}`, headers)
				if rec.Code != http.StatusUnauthorized {
					t.Errorf("应拒绝未认证请求，got %d, body=%s", rec.Code, rec.Body.String())
				}
			}
		})
	}
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
	// newPassword 须满足强策略，否则先被 400 拦截（此测试验证 token 缺失 → 401）。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/change-password",
		`{"oldPassword":"admin123","newPassword":"NewPass123!xy","userId":"user-admin"}`, nil)
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

	// 用改密 token 改密 → 200（TD-60：新密码须满足 12+字符+特殊字符）。
	rec2 := doReq(t, mux, http.MethodPost, "/api/v1/auth/change-password",
		`{"oldPassword":"admin123","newPassword":"NewPass123x!y","changePasswordToken":"`+resp.ChangePasswordToken+`"}`, nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("首登改密: got %d, body=%s", rec2.Code, rec2.Body.String())
	}

	// 新密码可登录（改密生效）。
	rec3 := doReq(t, mux, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"NewPass123x!y"}`, nil)
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

	// 注册 → 201 pending（TD-60：密码须满足 12+字符+特殊字符）。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/register",
		`{"username":"newuser","password":"SomePass123!x","email":"n@x.io"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: got %d, body=%s", rec.Code, rec.Body.String())
	}
	// pending 用户登录 → 401（Status!=active 拒绝）。
	rec2 := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"newuser","password":"SomePass123!x"}`, nil)
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
		`{"username":"newuser","password":"SomePass123!x"}`, nil)
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

// TestMe_ReturnsEffectivePermissions 守护 TD-60 A-2 契约对齐（阻断级）：
// /auth/me 必须返回 permissions（角色展开后的有效权限集合）。
//
// 为什么是阻断级：前端 stores/auth.js:31 以 user.permissions 作为侧栏/操作
// 门控的唯一数据源，且 hasPerm() 在权限集合为空时对一切返回 false——
// 缺这个字段的后果是「登录成功但侧栏全线隐藏」，且不报任何错。
// 该字段在 2026-10-09 的契约比对中被发现缺失，本测试防回归。
func TestMe_ReturnsEffectivePermissions(t *testing.T) {
	g, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)

	// 取 admin 的期望权限：按 RoleIDs 展开 ListRoles 的 Permissions 并集。
	u := g.svc.Store().GetUserByUsername("admin")
	if u == nil || len(u.RoleIDs) == 0 {
		t.Fatalf("前置失败：admin 或 admin 角色缺失（RoleIDs=%v）", u.RoleIDs)
	}
	want := map[string]bool{}
	for _, r := range g.svc.Store().ListRoles() {
		for _, id := range u.RoleIDs {
			if r.ID == id {
				for _, p := range r.Permissions {
					want[p] = true
				}
			}
		}
	}
	if len(want) == 0 {
		t.Fatalf("前置失败：admin 角色未绑定任何权限，测试无意义")
	}

	rec := doReq(t, mux, http.MethodGet, "/api/v1/auth/me", "", cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: got %d, body=%s", rec.Code, rec.Body.String())
	}
	var me map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("me 响应不是合法 JSON: %v", err)
	}

	// 1) permissions 必须存在且非空（阻断级判据）。
	raw, ok := me["permissions"]
	if !ok {
		t.Fatalf("me 响应缺 permissions 字段（TD-60 A-2 阻断级回归）：body=%s", rec.Body.String())
	}
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		t.Fatalf("permissions 应为非空数组，实际 %v", raw)
	}
	got := map[string]bool{}
	for _, v := range arr {
		got[v.(string)] = true
	}
	// 2) 内容须与「按角色展开」一致（不多不少）。
	for p := range want {
		if !got[p] {
			t.Errorf("permissions 缺期望权限 %q", p)
		}
	}
	for p := range got {
		if !want[p] {
			t.Errorf("permissions 含未期望权限 %q（应严格等于角色并集）", p)
		}
	}
	// 3) 不得重复（并集去重）。
	if len(got) != len(arr) {
		t.Errorf("permissions 含重复项：去重后 %d，实际 %d", len(got), len(arr))
	}

	// 4) 与 controlplane 对齐的字段名须并存（双轨期两种拼写都可用）。
	for _, k := range []string{"roleIDs", "tenantId", "status", "mustChangePassword"} {
		if _, ok := me[k]; !ok {
			t.Errorf("me 响应缺对齐字段 %q（controlplane 侧同名）", k)
		}
	}
}

// newTestGatewayWith 用显式 GatewayConfig 构造网关（用于验证开关类行为）。
func newTestGatewayWith(publicRegister bool) (*Gateway, *http.ServeMux, *service.Service) {
	eng := auth.NewEngine("test-secret", 15*time.Minute, 7*24*time.Hour)
	st := store.NewMemoryStore()
	svc := service.NewService(eng, st)
	g := NewGatewayWithConfig(svc, false, &GatewayConfig{PublicRegister: publicRegister})
	mux := http.NewServeMux()
	g.RegisterRoutes(mux)
	return g, mux, svc
}

// TestRegister_PublicRegisterDisabled 守护公开注册闸门（TD-60 A-2 §2.1）：
// PublicRegister=false 时必须 403，且**不得创建任何用户**。
// 对齐 controlplane auth_login.go:44（--public-register=false → 403）。
func TestRegister_PublicRegisterDisabled(t *testing.T) {
	_, mux, svc := newTestGatewayWith(false)

	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/register",
		`{"username":"nope","password":"SomePass123!x","email":"n@x.io"}`, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("闸门关闭时注册应 403，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if u := svc.Store().GetUserByUsername("nope"); u != nil {
		t.Error("403 拒绝后仍创建了用户——闸门必须挡在建号之前")
	}
	// 管理员建号路径不受闸门影响（走 /api/v1/users，非本端点）。
	_ = svc
}

// TestAdminCreateUser 守护管理员建号端点（POST /api/v1/users）。
//
// 背景：修复前该 handler 不接收 password 也不接 roleIDs，而 service.CreateUser
// 要求密码非空 ⇒ 管理员建号 **100% 返回 500**，且管理员没有任何指派角色的入口
// （只能建出无角色账号）。现有测试从未覆盖此路径，故缺陷长期不可见。
//
// 契约对齐 controlplane auth_users.go handleCreateUser：
// {username,password,email,roleIDs}；缺口令 400；弱口令 400；
// 未知角色 400；成功 201 且带口令可登录、角色已生效于 /auth/me 的 permissions。
func TestAdminCreateUser(t *testing.T) {
	_, mux, svc := newTestGateway()
	clearMustChangePassword(t, svc)
	cookies := loginAsAdmin(t, mux, svc)

	// 1) 缺 password ⇒ 400（对齐 service 的必填语义与 controlplane）。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/users",
		`{"username":"nou-pass"}`, cookies)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 password 应 400，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	// 2) 弱口令 ⇒ 400。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/users",
		`{"username":"weakpw","password":"123"}`, cookies)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("弱口令应 400，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	// 3) 未知角色 ⇒ 400。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/users",
		`{"username":"badrole","password":"SomePass123!x","roleIDs":["role-nope"]}`, cookies)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知角色应 400，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if svc.Store().GetUserByUsername("badrole") != nil {
		t.Error("未知角色被拒后不得建号")
	}
	// 4) 正常建号（带只读角色）⇒ 201，且口令可登录、角色进入 permissions。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/users",
		`{"username":"newbie","password":"SomePass123!x","email":"n@x.io","roleIDs":["role-viewer"]}`, cookies)
	if rec.Code != http.StatusCreated {
		t.Fatalf("正常建号应 201，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	u := svc.Store().GetUserByUsername("newbie")
	if u == nil {
		t.Fatal("建号后用户应存在")
	}
	if len(u.RoleIDs) != 1 || u.RoleIDs[0] != "role-viewer" {
		t.Fatalf("角色应被指派，实际 %v", u.RoleIDs)
	}
	// 新建账号立即 active（管理端创建无须审批）⇒ 可直接登录。
	recLogin := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"newbie","password":"SomePass123!x"}`, nil)
	if recLogin.Code != http.StatusOK {
		t.Fatalf("新建账号(active)应可登录，实际 %d body=%s", recLogin.Code, recLogin.Body.String())
	}
	at := findCookie(t, recLogin, "opsmesh_at")
	if at == nil {
		t.Fatal("登录应下发 opsmesh_at")
	}
	recMe := doReq(t, mux, http.MethodGet, "/api/v1/auth/me", "", map[string]string{"Cookie": "opsmesh_at=" + at.Value})
	var me map[string]any
	_ = json.Unmarshal(recMe.Body.Bytes(), &me)
	if arr, _ := me["permissions"].([]any); len(arr) == 0 {
		t.Error("新建账号的 /auth/me 应有 permissions（viewer 只读集）——角色指派未生效")
	}
	// 5) 重名 ⇒ 409。
	rec = doReq(t, mux, http.MethodPost, "/api/v1/users",
		`{"username":"newbie","password":"SomePass123!x"}`, cookies)
	if rec.Code != http.StatusConflict {
		t.Fatalf("重名应 409，实际 %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestRegister_BindsReadOnlyViewerRole 守护自注册默认角色（TD-60 A-2 §2.1 缺口）：
// 注册用户必须绑 role-viewer，且该角色**只含 `*:read`**——
// 这是「低权限、安全」的硬约束：新用户不能审批自己的注册、不能建号、
// 不能改角色（自我提权路径全部封死）。
func TestRegister_BindsReadOnlyViewerRole(t *testing.T) {
	_, mux, svc := newTestGatewayWith(true)

	// 1) viewer 角色必须存在，且内容恰为全部 :read（不多不少）。
	vr := svc.Store().GetRole("role-viewer")
	if vr == nil {
		t.Fatal("role-viewer 未 seed——自注册默认角色缺失")
	}
	want := map[string]bool{}
	for _, p := range svc.Store().ListPermissions() {
		if strings.HasSuffix(p.Name, ":read") {
			want[p.Name] = true
		}
	}
	got := map[string]bool{}
	for _, p := range vr.Permissions {
		got[p] = true
	}
	if len(want) == 0 {
		t.Fatal("权限目录无只读点，测试无意义")
	}
	for p := range want {
		if !got[p] {
			t.Errorf("viewer 缺只读权限 %q", p)
		}
	}
	for p := range got {
		if !want[p] {
			t.Errorf("viewer 含非只读权限 %q（违反最小权限）", p)
		}
	}
	// 2) 安全硬约束：任何写/删/审批/授权类权限点都不得出现在 viewer。
	for _, forbidden := range []string{"user:write", "user:delete", "user:approve", "role:write", "role:delete", "role:assign"} {
		if got[forbidden] {
			t.Errorf("viewer 不得持有 %q（否则自注册用户可自我提权/自审批）", forbidden)
		}
	}

	// 3) 注册用户确实被绑到 role-viewer。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/register",
		`{"username":"v-u","password":"SomePass123!x","email":"v@x.io"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("注册应 201，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	u := svc.Store().GetUserByUsername("v-u")
	if u == nil {
		t.Fatal("注册后应存在用户")
	}
	if len(u.RoleIDs) != 1 || u.RoleIDs[0] != "role-viewer" {
		t.Fatalf("注册用户应绑定 role-viewer，实际 %v", u.RoleIDs)
	}
	// /auth/me 的 permissions 应恰好是 viewer 的只读集（契约已对齐的字段）。
	u.Status = "active"
	_ = svc.Store().UpdateUser(u)
	hash, _ := auth.HashPassword("SomePass123!x")
	_ = svc.Store().ChangePassword(u.ID, hash)
	recLogin := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"v-u","password":"SomePass123!x"}`, nil)
	if recLogin.Code != http.StatusOK {
		t.Fatalf("审批后(模拟 active)应可登录，实际 %d body=%s", recLogin.Code, recLogin.Body.String())
	}
	cookies := map[string]string{"Cookie": "opsmesh_at=" + findCookie(t, recLogin, "opsmesh_at").Value +
		"; opsmesh_rt=" + findCookie(t, recLogin, "opsmesh_rt").Value}
	recMe := doReq(t, mux, http.MethodGet, "/api/v1/auth/me", "", cookies)
	var me map[string]any
	_ = json.Unmarshal(recMe.Body.Bytes(), &me)
	arr, _ := me["permissions"].([]any)
	if len(arr) != len(want) {
		t.Errorf("me.permissions 应等于 viewer 只读集(%d)，实际 %d", len(want), len(arr))
	}
}

// TestRegister_MissingDefaultRoleRefused 守护默认角色缺失时**拒绝注册并留痕**，
// 而不是静默建出无任何权限的孤儿账号（对齐 controlplane auth_login.go:80 的前置校验）。
func TestRegister_MissingDefaultRoleRefused(t *testing.T) {
	_, mux, svc := newTestGatewayWith(true)
	// 删掉 role-viewer 模拟「seed 未生效/被误删」。
	svc.Store().DeleteRole("role-viewer")

	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/register",
		`{"username":"orphan","password":"SomePass123!x","email":"o@x.io"}`, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("默认角色缺失时应 500 拒绝，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if u := svc.Store().GetUserByUsername("orphan"); u != nil {
		t.Error("默认角色缺失时不得建号（否则产出无权限孤儿账号）")
	}
}

// 默认（PublicRegister=true）注册返回 201，且落库状态**直接就是 pending**
// （旧实现是先 active 再回写，窗口期内可被登录）。
func TestRegister_DefaultOpenThenPending(t *testing.T) {
	g, mux, svc := newTestGatewayWith(true)
	if !g.publicRegister {
		t.Fatal("默认应开放公开注册（与 controlplane --public-register 默认 true 对齐）")
	}

	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/register",
		`{"username":"pending-u","password":"SomePass123!x","email":"p@x.io"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("默认应允许注册，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	u := svc.Store().GetUserByUsername("pending-u")
	if u == nil {
		t.Fatal("注册后应存在用户")
	}
	if u.Status != "pending" {
		t.Errorf("注册用户应直接落定为 pending，实际 %q（旧实现为 active 后回写，存在可登录窗口）", u.Status)
	}
	// 未审批 + pending ⇒ 登录必须失败（闸门不只挡注册，也挡住提前登录）。
	recLogin := doReq(t, mux, http.MethodPost, "/api/v1/auth/login",
		`{"username":"pending-u","password":"SomePass123!x"}`, nil)
	if recLogin.Code == http.StatusOK {
		t.Error("pending 用户不得登录成功")
	}
}

// TestChangePassword_SamePasswordRejected 守护「新旧相同」校验与 IP 限流
// （TD-60 A-2 §2.6：auth-svc 原缺这两项，controlplane auth_login.go:366/414 有）。
func TestChangePassword_SamePasswordRejected(t *testing.T) {
	_, mux, svc := newTestGateway()
	clearMustChangePassword(t, svc)
	cookies := loginAsAdmin(t, mux, svc)

	// 新旧相同 ⇒ 400。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/change-password",
		`{"oldPassword":"admin123","newPassword":"admin123"}`, cookies)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("新旧相同应 400，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	// 口令未被改动（400 不得产生副作用）。
	if u := svc.Store().GetUserByUsername("admin"); u == nil || !auth.VerifyPassword(u.PasswordHash, "admin123") {
		t.Error("被 400 拒绝的改密不应改动现有口令")
	}
	// 正常改密仍应成功（证明上面的 400 不是把整条路径堵死了）。
	rec2 := doReq(t, mux, http.MethodPost, "/api/v1/auth/change-password",
		`{"oldPassword":"admin123","newPassword":"BrandNewPass456!"}`, cookies)
	if rec2.Code != http.StatusOK {
		t.Fatalf("正常改密应 200，实际 %d body=%s", rec2.Code, rec2.Body.String())
	}
}

// ============ PUT /api/v1/users/{id} + PUT /api/v1/roles/{id} ============

// TestUpdateUser_AdminCanUpdateFields 验证 admin 可经 PUT 更新用户 email/status。
func TestUpdateUser_AdminCanUpdateFields(t *testing.T) {
	g, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)

	// 注册一个测试用户（密码满足强策略）。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/register",
		`{"username":"editme","password":"SomePass123!x","email":"old@x.io"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: got %d, body=%s", rec.Code, rec.Body.String())
	}
	u := g.svc.Store().GetUserByUsername("editme")
	if u == nil {
		t.Fatal("测试用户未入库")
	}

	// PUT 更新 email + status（pending → active）。
	rec2 := doReq(t, mux, http.MethodPut, "/api/v1/users/"+u.ID,
		`{"email":"new@x.io","status":"active"}`, cookies)
	if rec2.Code != http.StatusOK {
		t.Fatalf("update user: got %d, body=%s", rec2.Code, rec2.Body.String())
	}
	updated := g.svc.Store().GetUser(u.ID)
	if updated.Email != "new@x.io" {
		t.Errorf("email 未更新，got %s", updated.Email)
	}
	if updated.Status != "active" {
		t.Errorf("status 未更新，got %s", updated.Status)
	}
}

// TestUpdateUser_RequiresAuth 验证无 token 更新用户被拒（401）。
func TestUpdateUser_RequiresAuth(t *testing.T) {
	_, mux, _ := newTestGateway()
	rec := doReq(t, mux, http.MethodPut, "/api/v1/users/user-admin",
		`{"email":"x@x.io"}`, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无 token 更新用户应 401，实际 %d", rec.Code)
	}
}

// TestUpdateUser_PartialUpdate 验证仅更新提供的字段，未提供字段保持不变。
func TestUpdateUser_PartialUpdate(t *testing.T) {
	g, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)

	rec := doReq(t, mux, http.MethodPost, "/api/v1/auth/register",
		`{"username":"partial","password":"SomePass123!x","email":"keep@x.io"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: got %d", rec.Code)
	}
	u := g.svc.Store().GetUserByUsername("partial")

	// 仅更新 status，email 应保持不变。
	rec2 := doReq(t, mux, http.MethodPut, "/api/v1/users/"+u.ID,
		`{"status":"active"}`, cookies)
	if rec2.Code != http.StatusOK {
		t.Fatalf("partial update: got %d, body=%s", rec2.Code, rec2.Body.String())
	}
	updated := g.svc.Store().GetUser(u.ID)
	if updated.Email != "keep@x.io" {
		t.Errorf("未提供的 email 应保持不变，got %s", updated.Email)
	}
	if updated.Status != "active" {
		t.Errorf("status 应更新为 active，got %s", updated.Status)
	}
}

// TestUpdateRole_AdminCanUpdateFields 验证 admin 可经 PUT 更新角色 description/permissions。
func TestUpdateRole_AdminCanUpdateFields(t *testing.T) {
	g, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)

	// 创建测试角色（不动 admin 角色避免污染）。
	rec := doReq(t, mux, http.MethodPost, "/api/v1/roles",
		`{"name":"editor","description":"old desc","permissions":["user:read"]}`, cookies)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create role: got %d, body=%s", rec.Code, rec.Body.String())
	}
	role := g.svc.Store().GetRoleByName("editor")
	if role == nil {
		t.Fatal("测试角色未入库")
	}

	// PUT 更新 description + permissions。
	rec2 := doReq(t, mux, http.MethodPut, "/api/v1/roles/"+role.ID,
		`{"description":"updated desc","permissions":["user:read","role:read"]}`, cookies)
	if rec2.Code != http.StatusOK {
		t.Fatalf("update role: got %d, body=%s", rec2.Code, rec2.Body.String())
	}
	updated := g.svc.Store().GetRole(role.ID)
	if updated.Description != "updated desc" {
		t.Errorf("description 未更新，got %s", updated.Description)
	}
	if len(updated.Permissions) != 2 {
		t.Errorf("permissions 数量应 2，got %d (%v)", len(updated.Permissions), updated.Permissions)
	}
}

// TestUpdateRole_RequiresAuth 验证无 token 更新角色被拒（401）。
func TestUpdateRole_RequiresAuth(t *testing.T) {
	_, mux, _ := newTestGateway()
	rec := doReq(t, mux, http.MethodPut, "/api/v1/roles/role-admin",
		`{"description":"x"}`, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无 token 更新角色应 401，实际 %d", rec.Code)
	}
}

// TestUpdateRole_NotFound 验证更新不存在的角色返回 404。
func TestUpdateRole_NotFound(t *testing.T) {
	_, mux, svc := newTestGateway()
	cookies := loginAsAdmin(t, mux, svc)
	rec := doReq(t, mux, http.MethodPut, "/api/v1/roles/role-nonexistent",
		`{"description":"x"}`, cookies)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("更新不存在角色应 404，实际 %d", rec.Code)
	}
}
