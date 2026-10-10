// auth_cutover.go 切流路由（TD-60 方案 C）的 Server 侧接线：
// **裁决在 `internal/controlplane/cutover`（纯逻辑、可单测），转发在这一层**——
// 按设计 §4，裁决点放在 auth handler 内（它本就持有语义：在读 username、在签 token），
// 而不是通用代理层（那需要缓存/重放 body，且会把路由策略泄漏到所有域）。
//
// 转发复用 svcproxy 的 auth 域规则数据（目标地址 / 路径改写 / 自环检查），
// 与聚合代理同一份事实源；凭证（Cookie / Authorization）**原样带过去**——
// auth 域是自验 token 模型，凭证必须由 auth-svc 自己验。客户端自带的身份头
// （X-Tenant-ID / X-User-Id / X-User-Roles）一律剥离：未经验证的头不跨进程传递。
//
// 防护口径：登录/改密的本地防护（IP 限流 + 账号锁定检查）**在路由之前**已经执行，
// 故名册用户的登录也受本地限流约束；但**失败计数在 auth-svc 侧产生**，本地
// Loginguard 看不到 ⇒ 「账号锁定」对名册用户不生效（等价性问题见提案 §9.4 与
// 设计文档 §8.3，属 auth-svc 侧口径）。这一条写在文档里，不假装已对齐。
package controlplane

import (
	"context"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/controlplane/cutover"
	"github.com/Levango7/OpsMesh/internal/controlplane/svcproxy"
	"github.com/Levango7/OpsMesh/internal/logx"
)

// localAuthPrefix / authSvcPublicPrefix：本地面与代理面在 auth 域的同构关系
// （svcproxy 的 auth 规则：`/api/v1/auth-svc/*` 改写为上游 `/api/v1/auth/*`）。
const (
	localAuthPrefix    = "/api/v1/auth"
	authSvcPublicPrefx = "/api/v1/auth-svc"
)

// initCutoverRouter 装载名册并构造裁决器（NewServer 调用；非法配置 fail-fast）。
func initCutoverRouter() (*cutover.Router, error) {
	roster, err := cutover.LoadRoster(envOrEmpty("AUTH_CUTOVER_ROSTER"), envOrEmpty("AUTH_CUTOVER_ROSTER_FILE"))
	if err != nil {
		return nil, err
	}
	return cutover.NewRouter(roster), nil
}

func envOrEmpty(k string) string {
	return strings.TrimSpace(os.Getenv(k))
}

// selfCheckCutoverRoster 启动/重载自检：名册条目数 + 逐条核对**本侧是否存在该账号**。
// 本侧不存在 ⇒ 告警而不是阻断：名字可能是拼写错误（该查），也可能是迁移后本侧已清理（正常）。
// 这一步是设计 §5「写入前必须校验」在本侧能做到的那一半——对侧存在性只有 auth-svc 能答。
func (s *Server) selfCheckCutoverRoster() {
	rt := s.cutoverRouter
	if rt == nil || rt.Roster() == nil {
		return
	}
	entries := rt.Roster().Entries()
	logx.Info(context.Background(), "切流名册已装载",
		"source", rt.Roster().Source(), "count", len(entries), "proxy_enabled", rt.Enabled(),
		"owns_new_accounts", rt.OwnsNewAccounts())
	for _, name := range entries {
		if s.store.GetUserByUsername(name) == nil {
			logx.Warn(context.Background(), "切流名册条目在本侧不存在（拼写错误？或迁移后已清理）",
				"username", name, "hint", "名册每条应对应一次已完成的账号迁移")
		}
	}
}

// cutoverRosterLoop 名册热重载：周期重读来源（文件可热更；env 内联不可）。
// 回滚粒度 = 名册条目：摘除即单用户回退、清空即回纯本地，**无需重启控制面**。
func (s *Server) cutoverRosterLoop(ctx context.Context) {
	rt := s.cutoverRouter
	if rt == nil || rt.Roster() == nil {
		return
	}
	before := rt.Roster().Size()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := rt.Roster().Reload()
			if err != nil {
				logx.Warn(ctx, "切流名册重载失败（保留上一份可用名册）", "err", err, "keep", n)
				continue
			}
			if n != before {
				logx.Info(ctx, "切流名册已变更", "before", before, "after", n, "source", rt.Roster().Source())
				before = n
				s.selfCheckCutoverRoster()
			}
		}
	}
}

// tokenUsername 从请求凭证解出用户名（**不查库**）：Bearer 优先、Cookie 回退，
// 与 userFromToken/tenantFromBearer 同一提取口径。失败返回空串。
func (s *Server) tokenUsername(r *http.Request) string {
	tokenStr, err := extractBearer(r)
	if err != nil {
		if ck, ckErr := r.Cookie(accessTokenCookieName); ckErr == nil && strings.TrimSpace(ck.Value) != "" {
			tokenStr = ck.Value
		} else {
			return ""
		}
	}
	claims, err := authctx.ParseHSJWT(tokenStr, s.jwtSecret)
	if err != nil {
		return ""
	}
	return claims.Username
}

// cutoverForwardLogin 登录口的切流裁决（用户名在请求体里，认证前即可读）。
func (s *Server) cutoverForwardLogin(w http.ResponseWriter, r *http.Request, username string, rawBody []byte) bool {
	rt := s.cutoverRouter
	if rt == nil || !rt.RouteLogin(username) {
		return false
	}
	restoreRequestBody(r, rawBody) // 本地解析已消费 body：转发前还原（否则 502）
	// 失败计数对齐：名册用户的凭证校验发生在对侧，本地 loginguard 看不到其失败
	// ⇒ 账号锁定原本对名册用户不生效。这里按**转发后的状态码**在本地补记
	// （401=凭证错 ⇒ 记失败；2xx ⇒ 清计数；403/409 等账号状态类拒绝与本地语义一致地不记）。
	rec := &statusRecorder{ResponseWriter: w}
	if !s.forwardAuthToSvc(rec, r) {
		return false // 规则不活跃/未配置 ⇒ 回落本地（行为与切流前一致）
	}
	s.observeForwardedLogin(username, rec.status)
	logx.Info(r.Context(), "切流路由：转 auth-svc", "endpoint", "login", "user", username, "status", rec.status)
	return true
}

// observeForwardedLogin 把对侧的登录结果折算成本地失败计数（见 cutoverForwardLogin 注释）。
// 残余不对齐（写明）：绕过本地面、直接打 auth-svc 的失败仍不可见——那部分只有对侧口径能覆盖。
func (s *Server) observeForwardedLogin(username string, status int) {
	if s.loginGuard == nil || username == "" {
		return
	}
	if status == 0 {
		status = http.StatusOK // statusRecorder 只在 WriteHeader 时记录；隐式 200 也按成功清计数
	}
	switch {
	case status == http.StatusUnauthorized:
		s.loginGuard.RecordFail(username)
	case status >= 200 && status < 300:
		s.loginGuard.ResetFail(username)
	}
}

// 状态码观测复用 server_middleware.go 的 statusRecorder（HTTP 指标中间件同款，透传 Flush）。

// cutoverForwardByToken me/logout/主动改密口：按 JWT 的 username 裁决（认证前可读，不查库）。
func (s *Server) cutoverForwardByToken(w http.ResponseWriter, r *http.Request, endpoint string) bool {
	rt := s.cutoverRouter
	if rt == nil {
		return false
	}
	username := s.tokenUsername(r)
	if !rt.RouteUser(username) {
		return false
	}
	if !s.forwardAuthToSvc(w, r) {
		return false
	}
	logx.Info(r.Context(), "切流路由：转 auth-svc", "endpoint", endpoint, "user", username)
	return true
}

// cutoverRouteRefresh 刷新口的切流裁决（设计 §4）：按 **rt 归属侧 + 名册** 双判。
// 顺序要紧：必须先「非破坏性窥视」本地是否持有该 rt，**不能先 consume**
// （consume 会旋转/删除本地会话，若判定要转发就晚了）。
func (s *Server) cutoverRouteRefresh(w http.ResponseWriter, r *http.Request, rtValue string) bool {
	rt := s.cutoverRouter
	if rt == nil {
		return false
	}
	row := s.store.GetRefreshToken(hashRefreshToken(rtValue))
	localOwns := row != nil
	inRoster := false
	if localOwns {
		if u := s.store.GetUser(row.UserID); u != nil {
			inRoster = rt.Roster().Match(u.Username)
		}
	}
	if !rt.RouteRefresh(localOwns, inRoster) {
		return false
	}
	if !s.forwardAuthToSvc(w, r) {
		return false
	}
	logx.Info(r.Context(), "切流路由：转 auth-svc", "endpoint", "refresh",
		"local_owns", localOwns, "user_in_roster", inRoster)
	return true
}

// cutoverRouteChangePasswordToken 首登改密令牌（不透明串，只存在于签发侧会话存储）：
// 本地消费失败即不属本地 ⇒ 转 auth-svc。设计文档 §2 的端点表未列 change-password，
// 但首登改密是该流程的必经一步（登录被路由后令牌也由 auth-svc 签发）——此处补齐，
// 已在协调板告知并行线。
func (s *Server) cutoverRouteChangePasswordToken(w http.ResponseWriter, r *http.Request, rawBody []byte) bool {
	rt := s.cutoverRouter
	if rt == nil || !rt.RouteTokenOwner(false) {
		return false
	}
	restoreRequestBody(r, rawBody) // 同上：整请求转发
	if !s.forwardAuthToSvc(w, r) {
		return false
	}
	logx.Info(r.Context(), "切流路由：转 auth-svc", "endpoint", "change-password", "reason", "改密令牌不属本地")
	return true
}

// cutoverForwardRegister 注册口：不做请求级路由，按全局增量开关裁决（设计 §4）。
func (s *Server) cutoverForwardRegister(w http.ResponseWriter, r *http.Request, rawBody []byte) bool {
	rt := s.cutoverRouter
	if rt == nil || !rt.RouteRegister() {
		return false
	}
	restoreRequestBody(r, rawBody) // 同上：整请求转发
	if !s.forwardAuthToSvc(w, r) {
		return false
	}
	logx.Info(r.Context(), "切流路由：转 auth-svc", "endpoint", "register", "reason", "AUTH_SVC_OWNS_NEW_ACCOUNTS=true")
	return true
}

// forwardAuthToSvc 把请求整体转发到 auth-svc 的 auth 域。
// 返回 false 表示**未转发**（规则不活跃/查不到）——调用方回落到本地处理；
// 返回 true 表示已处理（含写出 4xx/5xx 错误响应）。
func (s *Server) forwardAuthToSvc(w http.ResponseWriter, r *http.Request) bool {
	rule := svcproxy.Lookup(authSvcPublicPrefx)
	if rule == nil || !rule.IsActive() {
		return false
	}
	target := rule.UpstreamBase()
	if target == nil {
		writeProxyErrorJSON(w, http.StatusServiceUnavailable, "auth backend address invalid: "+rule.EnvKey)
		return true
	}
	// 自环拦截与聚合代理同源：后端指向控制面自身时停在这里，避免请求改写后又回到本进程
	// 命中同名 handler，返回"另一个子系统的数据"且无错误信号。
	if port, self := svcproxy.SelfLoopTarget(target, s.httpPort, s.grpcPort, s.metricsPort); self {
		writeProxyErrorJSON(w, http.StatusServiceUnavailable,
			"auth backend points at the control plane itself ("+target.Hostname()+":"+strconv.Itoa(port)+"); set "+rule.EnvKey)
		return true
	}
	// 本地面 → 代理面：/api/v1/auth/me → /api/v1/auth-svc/me（随后由规则改写为上游 /api/v1/auth/me）。
	if strings.HasPrefix(r.URL.Path, localAuthPrefix) {
		r.URL.Path = authSvcPublicPrefx + strings.TrimPrefix(r.URL.Path, localAuthPrefix)
		r.URL.RawPath = ""
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.URL.Path = rule.RewriteProxyPath(req.URL.Path)
		req.URL.RawPath = ""
		// 未经验证的客户端身份头一律剥离（与聚合代理同一治理）；凭证原样保留给 auth-svc 自验。
		req.Header.Del("X-Tenant-ID")
		req.Header.Del("X-User-Id")
		req.Header.Del("X-User-Roles")
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		log.Printf("[controlplane] cutover 转发 auth-svc 失败: %v", err)
		writeProxyErrorJSON(rw, http.StatusBadGateway, "auth backend unavailable")
	}
	proxy.ServeHTTP(w, r)
	return true
}
