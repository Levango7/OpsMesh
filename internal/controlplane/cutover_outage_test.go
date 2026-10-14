package controlplane

// cutover_outage_test.go — 对侧（auth-svc）**宕机**时的 fail-closed 语义守卫
// （2026-10-14 §6.3 闭环；此前只有演练台的过程性证据，且那次证据因基线失效不可用）。
//
// 背景：演练报告 §6.3 留了一条未闭环——名册用户"持有效对侧会话 + 对侧被击杀"时
// refresh 的确切状态码没取到实证；代码走查曾担心 `cutoverRouteRefresh` 在转发失败时
// 会**回落本地消费**（⇒ 401 + 清 Cookie = "对侧故障期刷新丢会话"）。本测试把该判断
// 做成确定性判据，结论是**代码本来就是对的**，走查的那个担忧不成立。
//
// 关键语义（读完 forwardAuthToSvc 才能下结论）：
//   - forwardAuthToSvc 只有在**配置缺失**（无规则/未配 URL）时返回 false ⇒ 回落本地
//     是正确的（那种情况下根本没有对侧可转）；
//   - 一旦走到 proxy.ServeHTTP，即使对侧**连接被拒**，ErrorHandler 已写 502 且函数
//     返回 true ⇒ 切流各口的调用方直接带着 502 返回，**不会**回落本地。
// 因此对侧宕机时：login/refresh/me 全部 502（fail-closed），会话 Cookie 不清、
// loginGuard 不累计失败（基础设施故障 ≠ 凭证错误，与 §3.6 同纪律）。
//
// 为什么值得钉死：这是"切流真开之后第一个会遇到的故障形态"。若有人日后把
// forwardAuthToSvc 的返回值语义改坏（例如把"已写错误响应"也变成 false），回落
// 路径会把 502 变成 401 + 清 Cookie——本组测试即刻判红。

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// deadPeerURL 起一次监听再立即关闭，返回一个**此刻一定无人监听**的 base URL。
// 比硬编码端口稳妥：硬编码端口在本机可能真有服务。
func deadPeerURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return "http://" + addr
}

// outageTestServer 造一个"切流开着、名册有用户、但对侧宕机"的控制面。
func outageTestServer(t *testing.T, roster string) *Server {
	t.Helper()
	t.Setenv("AUTH_SVC_PROXY_ENABLED", "true")
	t.Setenv("AUTH_SVC_URL", deadPeerURL(t))
	t.Setenv("AUTH_CUTOVER_ROSTER", roster)

	s := newServiceProxyTestServer()
	rt, err := initCutoverRouter()
	if err != nil {
		t.Fatalf("initCutoverRouter: %v", err)
	}
	s.cutoverRouter = rt
	return s
}

// loginWith 构造一次登录请求并直接打 handleAuthLogin。
func loginWith(t *testing.T, s *Server, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`))
	w := httptest.NewRecorder()
	s.handleAuthLogin(w, req)
	return w
}

// TestCutoverPeerDown_LoginFailsClosed502 §6.3 判据一：对侧宕机时名册用户的登录
// 必须 **502 fail-closed**——不得 401（不冒充凭据错误）、不得回落本地（本地口令
// 早已在对侧改掉）。
func TestCutoverPeerDown_LoginFailsClosed502(t *testing.T) {
	s := outageTestServer(t, "alice")
	rosterUser(t, s, "alice")

	w := loginWith(t, s, "alice", "Passw0rd!cutover")
	if w.Code != http.StatusBadGateway {
		t.Fatalf("对侧宕机时名册用户登录应 502，实得 %d：%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "invalid username or password") {
		t.Errorf("502 不得伪装成凭据错误：%s", w.Body.String())
	}
	// 响应体必须**恰为**对侧故障的 502 JSON，不得再叠加本地处理的结果。
	// （rosterUser 建的本地用户口令与登录口令相同，若转发后回落本地，本地登录会
	// 成功并把 token 响应追加进来——状态码被 recorder 的首写定调仍是 502，只有
	// 响应体/CD 能识别"同一请求被处理了两次"。）
	if got := strings.TrimSpace(w.Body.String()); got != `{"error":"auth backend unavailable"}` {
		t.Fatalf("响应体应恰为 502 JSON（不得叠加本地登录结果），实得 %s", got)
	}
}

// TestCutoverPeerDown_LoginDoesNotAccrueLockout §3.6 同纪律在切流路径上的体现：
// 对侧宕机时反复登录**不得**把账号锁掉（基础设施故障 ≠ 凭证错误）。
// 判据：连打 3 次 502（注意别打满 5 次——那是 loginEndpointRateLimit 的 IP 限流
// 阈值，与账号锁是两回事，本仓测试里已有专测）后账号仍未锁；换个非名册用户登录
// 仍然 200，证明本地面未受牵连。
func TestCutoverPeerDown_LoginDoesNotAccrueLockout(t *testing.T) {
	s := outageTestServer(t, "alice")
	rosterUser(t, s, "alice")
	rosterUser(t, s, "bob")

	for i := 0; i < 3; i++ {
		if w := loginWith(t, s, "alice", "Passw0rd!cutover"); w.Code != http.StatusBadGateway {
			t.Fatalf("第 %d 次登录应 502，实得 %d", i+1, w.Code)
		}
	}
	if s.loginGuard.Locked("alice") {
		t.Error("对侧宕机的 502 不得累计登录失败把名册用户锁住（§3.6 纪律）")
	}
	// 本地面未受牵连：非名册用户照常登录成功（证明宕机只影响切流路径）。
	if w := loginWith(t, s, "bob", "Passw0rd!cutover"); w.Code != http.StatusOK {
		t.Fatalf("非名册用户本地登录应 200，实得 %d：%s", w.Code, w.Body.String())
	}
}

// TestCutoverPeerDown_RefreshKeepsSession §6.3 判据二（原未闭环项）：对侧宕机时
// 名册会话的 refresh 必须 502 **且不清 Cookie**——活跃会话不得被一次对侧故障
// 摧毁（与 §3.7 "refresh 故障期不清会话"同纪律）。
func TestCutoverPeerDown_RefreshKeepsSession(t *testing.T) {
	s := outageTestServer(t, "alice")
	rosterUser(t, s, "alice") // 名册含 alice ⇒ 本地持有的 rt 也要送对侧（必须先建用户）
	aliceID := s.store.GetUserByUsername("alice").ID
	aliceRT, err := s.createRefreshToken(aliceID, "default", "")
	if err != nil {
		t.Fatalf("createRefreshToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: refreshTokenCookieName, Value: aliceRT})
	w := httptest.NewRecorder()
	s.handleAuthRefresh(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("对侧宕机时 refresh 应 502，实得 %d：%s", w.Code, w.Body.String())
	}
	for _, sc := range w.Result().Cookies() {
		if (sc.Name == accessTokenCookieName || sc.Name == refreshTokenCookieName) && sc.MaxAge < 0 {
			t.Errorf("故障期不得清空 %s Cookie（会话被对侧故障摧毁）：%s=%q",
				sc.Name, sc.Name, sc.Value)
		}
	}
	// 回落本地会重签会话 Cookie（本地 rt 有效 ⇒ 刷新成功）——有任意会话 Cookie
	// 即证明"同一请求被处理了两次"。
	if cks := w.Result().Cookies(); len(cks) > 0 {
		names := make([]string, 0, len(cks))
		for _, sc := range cks {
			names = append(names, sc.Name)
		}
		t.Errorf("故障期 refresh 不应下发任何 Cookie（回落本地会重签），实得 %v", names)
	}
}

// TestCutoverPeerDown_MeFailsClosed me 口同样 fail-closed（502，不回落本地）：
// 回落会拿本地旧用户数据冒充对侧身份。
func TestCutoverPeerDown_MeFailsClosed(t *testing.T) {
	s := outageTestServer(t, "alice")
	rosterUser(t, s, "alice")
	u := s.store.GetUserByUsername("alice")
	token, err := s.issueUserToken(u)
	if err != nil {
		t.Fatalf("issueUserToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.handleAuthMe(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("对侧宕机时 /me 应 502，实得 %d：%s", w.Code, w.Body.String())
	}
}
