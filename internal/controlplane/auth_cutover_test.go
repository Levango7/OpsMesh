// auth_cutover_test.go 切流路由（TD-60 方案 C）的行为守卫：
// 名册用户被送去 auth-svc、非名册用户逐字不变、总闸关闭时一切按本地。
//
// 判据来源：docs/td60-cutover-signal-design.md（设计）与 internal/controlplane/cutover（裁决）。
// 这里验证**接线**：五个端点（login/me/logout/refresh/change-password）+ register 开关。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Levango7/OpsMesh/internal/controlplane/credentials"
	"github.com/Levango7/OpsMesh/internal/store"
)

// rosterUser 在测试 store 里建一个可登录、已改密的用户并返回其用户名。
func rosterUser(t *testing.T, s *Server, name string) string {
	t.Helper()
	hash, err := credentials.HashPassword("Passw0rd!cutover")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if u := s.store.CreateUser(&store.User{
		Username: name, PasswordHash: hash, Status: "active", TenantID: "default",
		RoleIDs: []string{"role-admin"},
	}); u == nil {
		t.Fatalf("建用户 %s 失败", name)
	}
	return name
}

// cutoverTestServer 起一个假的 auth-svc 后端并返回（Server、后端命中记录、后端请求快照）。
func cutoverTestServer(t *testing.T, roster string) (*Server, *atomic.Int32, func() *http.Request) {
	t.Helper()
	var hits atomic.Int32
	var last atomic.Value // *http.Request 快照（深拷贝关键字段）
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		snap := r.Clone(r.Context())
		snap.Header = r.Header.Clone()
		last.Store(snap)
		w.Header().Set("Content-Type", "application/json")
		// 模拟 auth-svc 的登录响应：下发会话 Cookie，便于断言响应侧透传。
		http.SetCookie(w, &http.Cookie{Name: accessTokenCookieName, Value: "svc-issued-at", Path: "/"})
		_, _ = w.Write([]byte(`{"token":"svc-issued-at","user":{"username":"` + strings.TrimSpace(roster) + `"}}`))
	}))
	t.Cleanup(backend.Close)

	t.Setenv("AUTH_SVC_PROXY_ENABLED", "true")
	t.Setenv("AUTH_SVC_URL", backend.URL)
	t.Setenv("AUTH_CUTOVER_ROSTER", roster)

	s := newServiceProxyTestServer()
	rt, err := initCutoverRouter()
	if err != nil {
		t.Fatalf("initCutoverRouter: %v", err)
	}
	s.cutoverRouter = rt
	return s, &hits, func() *http.Request {
		v := last.Load()
		if v == nil {
			return nil
		}
		req, _ := v.(*http.Request)
		return req
	}
}

func loginPost(t *testing.T, s *Server, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	s.handleAuthLogin(w, req)
	return w
}

func TestCutoverLoginRoutesRosterUserToAuthSvc(t *testing.T) {
	s, hits, lastReq := cutoverTestServer(t, "alice")
	rosterUser(t, s, "alice")
	rosterUser(t, s, "bob")

	// 名册用户：整请求转发（后端收到登录请求与请求体）。
	w := loginPost(t, s, "alice", "Passw0rd!cutover")
	if hits.Load() != 1 {
		t.Fatalf("名册用户登录应转发到 auth-svc；hits=%d", hits.Load())
	}
	if w.Code != http.StatusOK {
		t.Fatalf("透传后端响应：status=%d body=%s", w.Code, w.Body.String())
	}
	req := lastReq()
	if req == nil || req.URL.Path != "/api/v1/auth/login" {
		t.Fatalf("后端收到的路径应为上游形态 /api/v1/auth/login；got=%v", req.URL.Path)
	}
	// 响应侧：auth-svc 的 Set-Cookie 必须原样透传给客户端（否则切过去后拿不到会话）。
	if !strings.Contains(w.Header().Get("Set-Cookie"), "svc-issued-at") {
		t.Errorf("后端 Set-Cookie 未透传: %q", w.Header().Get("Set-Cookie"))
	}

	// 非名册用户：逐字不变（本地处理，后端零命中）。
	before := hits.Load()
	w2 := loginPost(t, s, "bob", "Passw0rd!cutover")
	if hits.Load() != before {
		t.Fatalf("非名册用户不得转发；hits=%d→%d", before, hits.Load())
	}
	if w2.Code != http.StatusOK {
		t.Fatalf("非名册用户本地登录应成功：status=%d body=%s", w2.Code, w2.Body.String())
	}
}

func TestCutoverLoginKeepsLocalGuardsBeforeForwarding(t *testing.T) {
	s, hits, _ := cutoverTestServer(t, "alice")
	rosterUser(t, s, "alice")

	// 账号锁定：本地锁定态必须在转发**之前**拦住（本地防护口径对名册用户同样生效）。
	for i := 0; i < 20; i++ {
		s.loginGuard.RecordFail("alice")
	}
	if !s.loginGuard.Locked("alice") {
		t.Fatal("前置条件失败：alice 应已锁定")
	}
	w := loginPost(t, s, "alice", "Passw0rd!cutover")
	if hits.Load() != 0 {
		t.Fatalf("锁定用户的登录不得转发；hits=%d", hits.Load())
	}
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("锁定用户应 429；got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCutoverProxyDisabledIsNoOp(t *testing.T) {
	s, hits, _ := cutoverTestServer(t, "alice")
	rosterUser(t, s, "alice")
	// 总闸关闭：即使名册有内容，也一切按本地（与切流前逐字一致）。
	t.Setenv("AUTH_SVC_PROXY_ENABLED", "false")
	w := loginPost(t, s, "alice", "Passw0rd!cutover")
	if hits.Load() != 0 {
		t.Fatalf("总闸关闭时不得转发；hits=%d", hits.Load())
	}
	if w.Code != http.StatusOK {
		t.Fatalf("总闸关闭时名册用户仍走本地登录：status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCutoverMeAndLogoutRouteByToken(t *testing.T) {
	s, hits, lastReq := cutoverTestServer(t, "alice")
	rosterUser(t, s, "alice")
	u := s.store.GetUserByUsername("alice")
	token, err := s.issueUserToken(u)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.handleAuthMe(w, req)
	if hits.Load() != 1 {
		t.Fatalf("名册用户的 /me 应转发；hits=%d", hits.Load())
	}
	if got := lastReq(); got == nil || got.URL.Path != "/api/v1/auth/me" {
		t.Fatalf("后端路径应为 /api/v1/auth/me；got=%v", got.URL.Path)
	}
	if got := lastReq(); got == nil || !strings.Contains(got.Header.Get("Authorization"), "Bearer ") {
		t.Errorf("凭证应原样带给 auth-svc（自验 token 模型）: %v", got.Header.Get("Authorization"))
	}
	// 客户端自带的身份头必须被剥离（未经验证的头不跨进程传递）。
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("X-Tenant-ID", "spoofed")
	req2.Header.Set("X-User-Id", "spoofed-user")
	w2 := httptest.NewRecorder()
	s.handleAuthMe(w2, req2)
	if got := lastReq(); got == nil || got.Header.Get("X-Tenant-ID") != "" || got.Header.Get("X-User-Id") != "" {
		t.Errorf("客户端身份头应在转发前剥离: tenant=%q user=%q", got.Header.Get("X-Tenant-ID"), got.Header.Get("X-User-Id"))
	}
}

func TestCutoverRefreshByOwnershipAndRoster(t *testing.T) {
	s, hits, lastReq := cutoverTestServer(t, "alice")
	rosterUser(t, s, "alice")
	rosterUser(t, s, "bob")

	// ① 本地持有但用户**未迁移** ⇒ 本地处理（与切流前一致）。
	bobID := s.store.GetUserByUsername("bob").ID
	bobRT, err := s.createRefreshToken(bobID, "default", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: refreshTokenCookieName, Value: bobRT})
	w := httptest.NewRecorder()
	s.handleAuthRefresh(w, req)
	if hits.Load() != 0 {
		t.Fatalf("未迁移用户的 rt 应本地处理；hits=%d", hits.Load())
	}
	if w.Code != http.StatusOK {
		t.Fatalf("本地刷新应成功：status=%d body=%s", w.Code, w.Body.String())
	}

	// ② 本地持有但用户**已迁移** ⇒ 送 auth-svc（该用户重登一次，避免旧会话把它永久留在本地）。
	aliceID := s.store.GetUserByUsername("alice").ID
	aliceRT, err := s.createRefreshToken(aliceID, "default", "")
	if err != nil {
		t.Fatal(err)
	}
	before := hits.Load()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req2.AddCookie(&http.Cookie{Name: refreshTokenCookieName, Value: aliceRT})
	w2 := httptest.NewRecorder()
	s.handleAuthRefresh(w2, req2)
	if hits.Load() != before+1 {
		t.Fatalf("已迁移用户的本地 rt 应送对侧；hits=%d", hits.Load()-before)
	}
	if got := lastReq(); got == nil || got.URL.Path != "/api/v1/auth/refresh" {
		t.Fatalf("后端路径应为 /api/v1/auth/refresh；got=%v", got.URL.Path)
	}

	// ③ 本地根本不持有 ⇒ 属对侧（未知/对侧签发的 rt）。
	before = hits.Load()
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req3.AddCookie(&http.Cookie{Name: refreshTokenCookieName, Value: "unknown-rt-from-auth-svc"})
	w3 := httptest.NewRecorder()
	s.handleAuthRefresh(w3, req3)
	if hits.Load() != before+1 {
		t.Fatalf("本地不持有的 rt 应送对侧；hits=%d", hits.Load()-before)
	}
}

func TestCutoverChangePasswordTokenAndRegister(t *testing.T) {
	s, hits, lastReq := cutoverTestServer(t, "alice")
	rosterUser(t, s, "alice")

	// 首登改密：本地消费不到的令牌 ⇒ 属对侧 ⇒ 转发（设计端点表未列，本实现补齐）。
	body := `{"oldPassword":"a","newPassword":"b","changePasswordToken":"svc-issued-cpt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/change-password", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleAuthChangePassword(w, req)
	if hits.Load() != 1 {
		t.Fatalf("非本地改密令牌应转发；hits=%d", hits.Load())
	}
	if got := lastReq(); got == nil || got.URL.Path != "/api/v1/auth/change-password" {
		t.Fatalf("后端路径应为 /api/v1/auth/change-password；got=%v", got.URL.Path)
	}
	// 响应必须透传（修前 body 已被本地解析消费 ⇒ 转发发出"有长度无 body"的请求 ⇒ 502）。
	if w.Code != http.StatusOK {
		t.Fatalf("改密转发应透传后端响应：status=%d body=%s", w.Code, w.Body.String())
	}

	// 注册：本地"公开注册"闸先于路由（部署方关掉公开注册 ⇒ 全 403，不会绕道 auth-svc）。
	// 故此处先打开本地闸，再验证增量开关对路由的作用。
	s.cfg.PublicRegister = true
	before := hits.Load()
	reqR := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{"username":"newbie","password":"Passw0rd!x","email":"n@x.io"}`))
	wR := httptest.NewRecorder()
	s.handleAuthRegister(wR, reqR)
	if hits.Load() != before {
		t.Fatal("增量开关未开时注册应留在本地")
	}
	t.Setenv("AUTH_SVC_OWNS_NEW_ACCOUNTS", "true")
	reqR2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{"username":"newbie2","password":"Passw0rd!x","email":"n2@x.io"}`))
	wR2 := httptest.NewRecorder()
	s.handleAuthRegister(wR2, reqR2)
	if hits.Load() != before+1 {
		t.Fatal("增量开关打开后注册应转发")
	}
}

func TestCutoverLoginFailureCountParity(t *testing.T) {
	// 失败计数对齐：名册用户的凭证错在 auth-svc 侧发生，本地要按**转发结果**补记，
	// 否则「账号锁定」对名册用户不生效（设计 §10.2 的那条不对齐）。
	// 本用例走**真实登录路径**（而非直接调观测函数）——变异检验要求「去掉补记即判红」。
	var status atomic.Int32
	status.Store(http.StatusUnauthorized)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := int(status.Load())
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"error":"stub"}`))
	}))
	defer backend.Close()
	t.Setenv("AUTH_SVC_PROXY_ENABLED", "true")
	t.Setenv("AUTH_SVC_URL", backend.URL)
	t.Setenv("AUTH_CUTOVER_ROSTER", "alice,dave")
	s := newServiceProxyTestServer()
	rt, err := initCutoverRouter()
	if err != nil {
		t.Fatal(err)
	}
	s.cutoverRouter = rt
	rosterUser(t, s, "alice")
	rosterUser(t, s, "dave")

	// failsToLock 返回该账号再犯多少次失败才触发锁定（自校准，不依赖阈值常量）。
	failsToLock := func(name string) int {
		for i := 1; i <= 50; i++ {
			if s.loginGuard.RecordFail(name) {
				return i
			}
		}
		return 51
	}
	baseline := failsToLock("bob") // 干净账号基线（同时锁上 bob，不影响他人）
	if baseline > 49 {
		t.Fatalf("前置失败：50 次失败都未锁定（阈值异常）")
	}

	// ① 一次真实的"转发 401" ⇒ 本地计数应 +1（基线减一）。
	if w := loginPost(t, s, "alice", "wrong-password"); w.Code != http.StatusUnauthorized {
		t.Fatalf("对侧 401 应透传；got=%d body=%s", w.Code, w.Body.String())
	}
	if got := failsToLock("alice"); got != baseline-1 {
		t.Fatalf("转发 401 未在本地补记失败：alice 还需 %d 次才锁，期望 %d（基线 %d）", got, baseline-1, baseline)
	}

	// 注：failsToLock 是**破坏性测量**（把该账号的失败预算用光并锁上），
	// 故每个被测账号只测一次、且测量即最后一个动作（上面 alice 的测量已完成锁定）。
	if !s.loginGuard.Locked("alice") {
		t.Fatal("测量后 alice 应处于锁定态")
	}
	w := loginPost(t, s, "alice", "wrong-password")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("锁定的名册用户应 429（本地闸先于路由）；got=%d", w.Code)
	}

	// ③ 成功清计数：另起一个名册用户 dave——先欠一次失败，再让对侧回 200，
	// 其失败预算应回到完整 baseline（若没清，测量值会明显小于 baseline）。
	if w := loginPost(t, s, "dave", "wrong-password"); w.Code != http.StatusUnauthorized {
		t.Fatalf("dave 前置 401；got=%d", w.Code)
	}
	status.Store(http.StatusOK)
	if w := loginPost(t, s, "dave", "Passw0rd!cutover"); w.Code != http.StatusOK {
		t.Fatalf("对侧 200 应透传；got=%d body=%s", w.Code, w.Body.String())
	}
	if got := failsToLock("dave"); got < baseline {
		t.Fatalf("成功登录应清除失败计数：清后仅需 %d 次即再锁（基线 %d）", got, baseline)
	}
}

func TestCutoverOwnsNewAccountsBlocksLocalUserCreation(t *testing.T) {
	s, _, _ := cutoverTestServer(t, "alice")
	s.cfg.PublicRegister = true
	// 权限：走 admin 身份（本测试服务器 seed 了 admin）。
	auth := loginAsAdmin(t, s)

	doCreate := func(name string) *httptest.ResponseRecorder {
		body := strings.NewReader(`{"username":"` + name + `","password":"Passw0rd!x","email":"` + name + `@x.io"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users", body)
		req.Header.Set("Authorization", auth)
		req.Header.Set("X-Tenant-ID", "default")
		w := httptest.NewRecorder()
		s.handleCreateUser(w, req)
		return w
	}
	// 开关关闭（默认）：本地建号照常。
	if w := doCreate("stringer"); w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("开关关闭时本地建号应成功；got=%d body=%s", w.Code, w.Body.String())
	}
	// 开关打开（增量归 auth-svc）⇒ 本地建号明确拒绝并指向对侧（不得静默建在本地）。
	t.Setenv("AUTH_SVC_OWNS_NEW_ACCOUNTS", "true")
	w := doCreate("stringer2") // 换名：确保 409 只可能来自开关守卫，而不是"用户名已存在"
	if w.Code != http.StatusConflict {
		t.Fatalf("开关打开时本地建号应 409；got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "auth-svc") {
		t.Errorf("拒绝信息应指明去向 auth-svc：%s", w.Body.String())
	}
}
