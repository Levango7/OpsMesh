// Package http 提供 auth-svc 的 HTTP 网关（REST API，方案 B 用户中心后端）。
//
// 设计原则（A1，2026-09-09 方案 V2 风险审核后）：
//   - **默认关闭**：AUTH_SVC_HTTP_ENABLED=false 时网关不注册任何路由——杜绝与
//     controlplane 并存期的双轨 Cookie 互写冲突（R1）；controlplane 仍是唯一登录入口。
//   - **controlplane 零触碰**：方案 B 约束——controlplane 的 121 处 requireProd/
//     ParseHSJWT 本地验签一个字不动；本网关是平行用户中心（数据边界：在 auth-svc
//     注册的用户只在 auth-svc 生效，R8 文档声明）。
//   - Cookie 语义与 controlplane/auth.go setCookie 逐字段对齐（R1/R2）：
//     opsmesh_at / opsmesh_rt，Path=/、HttpOnly、SameSite=Lax、Secure 条件启用。
//   - refresh 只写 Cookie 不返回 token body（R2：前端 request.js refreshing 单飞
//     契约——postEmpty 只查 status，body 不消费）。
//   - change-password 只走 token 模式（R4）：从 Cookie/Authorization 读 token
//     解析 userID，绝不接受 body.user_id 直调（防 HTTP 化后越权改密）。
//   - DeviceFP（R3）：login/refresh 从 X-Device-FP 头读取，签发绑定+刷新校验
//     （空 FP 兼容旧客户端）。
//   - 注册审批（R6）：Register 仅 HTTP 端点（不动 gRPC proto），默认 Status=pending
//     须 admin approve（controlplane --allow-public-register=false 同安全基线）。
package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	authv1 "github.com/Levango7/OpsMesh/services/auth-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/auth"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/cache"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/store"
)

// Cookie 名与 TTL 与 controlplane/auth.go:56-59 逐字一致（跨入口互换的前提）。
const (
	accessTokenCookieName  = "opsmesh_at"
	refreshTokenCookieName = "opsmesh_rt"
)

// Gateway 持有 HTTP handler 依赖。
type Gateway struct {
	svc          *service.Service
	cookieSecure bool
	guard        *loginGuard         // A2 防爆破（login/register 入口）
	deviceFP     *deviceFPManager    // 设备指纹管理（TD-60）
	sessions     *cache.SessionStore // Redis Session 存储（可为 nil=JWT 无状态模式）
}

// NewGateway 构造 Gateway。cookieSecure 由 main 注入（AUTH_SVC_HTTP_COOKIE_SECURE
// 显式配置，或 TLS 推断——与 controlplane cookieSecure 同语义）。
//
// 向后兼容：不带 Redis/DeviceFP 参数的构造走纯内存模式（测试用）。
func NewGateway(svc *service.Service, cookieSecure bool) *Gateway {
	return &Gateway{
		svc:          svc,
		cookieSecure: cookieSecure,
		guard:        newLoginGuard(),
		deviceFP:     newDeviceFPManager(nil, false), // 默认关闭设备指纹校验（测试兼容）
	}
}

// GatewayConfig 封装 Gateway 构造的可选安全配置（TD-60）。
type GatewayConfig struct {
	Cache           *cache.Cache        // Redis 缓存（nil=纯内存）
	DeviceFPEnabled bool                // 设备指纹校验开关
	Sessions        *cache.SessionStore // Redis Session 存储（nil=JWT 无状态）
}

// NewGatewayWithConfig 构造带安全配置的 Gateway（TD-60 增强）。
//
// cache 非 nil 时：loginGuard 走 Redis 后端（多副本共享失败计数），
// deviceFPManager 走 Redis 存储已知设备集合。
// sessions 非 nil 时：启用 Redis Session 存储；否则降级为 JWT 无状态模式。
func NewGatewayWithConfig(svc *service.Service, cookieSecure bool, gc *GatewayConfig) *Gateway {
	g := &Gateway{
		svc:          svc,
		cookieSecure: cookieSecure,
		sessions:     gc.Sessions,
	}
	if gc.Cache != nil {
		g.guard = newLoginGuardWithCache(gc.Cache)
		g.deviceFP = newDeviceFPManager(gc.Cache, gc.DeviceFPEnabled)
	} else {
		g.guard = newLoginGuard()
		g.deviceFP = newDeviceFPManager(nil, gc.DeviceFPEnabled)
	}
	return g
}

// clientIP 提取客户端 IP（httptest 场景 RemoteAddr 可靠；代理场景留待部署层
// X-Forwarded-For 处理——与 controlplane 同限制，MVP 直连语义）。
func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

// setCookie 与 controlplane setCookie 逐字段对齐（R1：同名 Cookie 语义必须一致）。
func (g *Gateway) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   g.cookieSecure,
	})
}

// clearCookies 清空双 Cookie（logout；与 controlplane 清除语义一致：空值 + MaxAge -1）。
func (g *Gateway) clearCookies(w http.ResponseWriter) {
	g.setCookie(w, accessTokenCookieName, "", -1)
	g.setCookie(w, refreshTokenCookieName, "", -1)
}

// deviceFP 从 X-Device-FP 头读取设备指纹（与 controlplane deviceFingerprint 同源）。
func deviceFP(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Device-FP"))
}

// RegisterRoutes 注册全部 HTTP 路由。仅当 HTTP 网关启用（R1 开关）时由 main 调用。
func (g *Gateway) RegisterRoutes(mux *http.ServeMux) {
	// 认证流（Cookie 语义端点——与前端 web/enterprise/src/api/auth.js 契约对齐）。
	mux.HandleFunc("/api/v1/auth/login", g.handleLogin)
	mux.HandleFunc("/api/v1/auth/logout", g.handleLogout)
	mux.HandleFunc("/api/v1/auth/refresh", g.handleRefresh)
	mux.HandleFunc("/api/v1/auth/register", g.handleRegister)
	mux.HandleFunc("/api/v1/auth/change-password", g.handleChangePassword)
	mux.HandleFunc("/api/v1/auth/me", g.handleMe)

	// 用户/角色/权限管理（与 controlplane /users /roles /permissions 路径一致）。
	mux.HandleFunc("/api/v1/users", g.managementHandler("user", g.handleUsers))
	mux.HandleFunc("/api/v1/users/", g.managementHandler("user", g.handleUserDetail))
	mux.HandleFunc("/api/v1/roles", g.managementHandler("role", g.handleRoles))
	mux.HandleFunc("/api/v1/roles/", g.managementHandler("role", g.handleRoleDetail))
	mux.HandleFunc("/api/v1/permissions", g.managementHandler("permission", g.handlePermissions))
}

// managementHandler 统一保护管理路由；认证端点不经过此中间件。
// 角色/权限查询与 controlplane 一致，允许有效登录用户访问。
// auth-svc 已定义 user:approve 权限，审批由 user:approve 门控（对齐 controlplane）。
func (g *Gateway) managementHandler(resource string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		permission := resource + ":write"
		switch r.Method {
		case http.MethodGet:
			permission = resource + ":read"
			if resource == "role" || resource == "permission" {
				permission = ""
			}
		case http.MethodDelete:
			permission = resource + ":delete"
		}
		if _, ok := g.requirePermission(w, r, permission); !ok {
			return
		}
		next(w, r)
	}
}

// ============ 认证流 ============

// handleLogin POST /api/v1/auth/login {username,password} → 双 HttpOnly Cookie。
//
// 首登 mustChangePassword：与 controlplane 同语义——不签常规 at+rt，签 5min 改密
// 专用 token 进 opsmesh_at Cookie，响应体带 MustChangePassword=true + ChangePasswordToken。
//
// TD-60 增强：
//   - 设备指纹采集（X-Device-FP 头优先，否则 UA+IP+TLS 合成）
//   - 未知设备触发二次验证（响应 needMFA=true，前端引导 MFA/邮箱确认）
func (g *Gateway) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	// A2 防爆破两道闸（与 controlplane loginGuard 同参数）：
	// IP 令牌桶（429）+ 账号锁定（423 Locked 语义，此处用 429 统一——与
	// controlplane 一致：账号锁定同样以"拒绝"呈现，不区分码，防探测锁定状态）。
	if !g.guard.allowIP(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many attempts from this IP")
		return
	}
	if g.guard.accountLocked(body.Username) {
		writeError(w, http.StatusTooManyRequests, "account temporarily locked")
		return
	}
	// TD-60 设备指纹采集：X-Device-FP 头优先，否则 UA+IP+TLS 合成。
	fp := collectDeviceFP(r)
	resp, err := g.svc.LoginWithFP(r.Context(), &authv1.LoginRequest{
		Username: body.Username,
		Password: body.Password,
	}, fp)
	if err != nil {
		// 失败计入账号锁定计数（成功才复位——与 controlplane 同语义）。
		g.guard.recordFail(body.Username)
		// 统一 401（不区分"用户不存在/密码错/非 active"——防用户名枚举，controlplane 同语义）。
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	g.guard.recordSuccess(body.Username)

	// TD-60 设备指纹校验：未知设备触发二次验证。
	// 登录成功后检查设备是否已知；未知设备注册并标记 needMFA。
	// needMFA=true 时仍下发 token（用户已通过密码验证），前端据 needMFA 引导 MFA 确认。
	_, needMFA := g.deviceFP.checkAndRegister(resp.User.Id, fp)

	// at 写 Cookie；rt 仅常规登录写（首登改密流不写 rt——与 controlplane 一致）。
	g.setCookie(w, accessTokenCookieName, resp.AccessToken, int(resp.ExpiresIn))
	if resp.RefreshToken != "" {
		g.setCookie(w, refreshTokenCookieName, resp.RefreshToken, 7*24*3600)
	}
	// TD-60 创建 Redis Session（可用时；降级 no-op）。
	if g.sessions != nil && g.sessions.Enabled() && resp.AccessToken != "" {
		sid := sessionIDFromToken(resp.AccessToken)
		_ = g.sessions.Create(&cache.Session{
			SessionID: sid,
			UserID:    resp.User.Id,
			TenantID:  "default",
			DeviceFP:  fp,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":                toPublicUser(resp.User),
		"mustChangePassword":  resp.MustChangePassword,
		"changePasswordToken": resp.ChangePasswordToken,
		"needMFA":             needMFA,
		"deviceFP":            fp != "", // 标记是否绑定了设备指纹
	})
}

// handleLogout POST /api/v1/auth/logout → 清双 Cookie + 吊销（jti 黑名单 + rt 删除 + Session 撤销）。
func (g *Gateway) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	at := readCookie(r, accessTokenCookieName)
	rt := readCookie(r, refreshTokenCookieName)
	// TD-60 Session 撤销（Redis 可用时即时失效；不可用时 JWT 黑名单兜底）。
	// sessionID = SHA-256(access token)，与 handleLogin 创建时一致。
	if g.sessions != nil && g.sessions.Enabled() && at != "" {
		_ = g.sessions.Revoke(sessionIDFromToken(at))
	}
	// 复用 service.Logout（jti 黑名单 + rt 删除）。
	_, _ = g.svc.Logout(r.Context(), &authv1.LogoutRequest{
		AccessToken:  at,
		RefreshToken: rt,
	})
	g.clearCookies(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleRefresh POST /api/v1/auth/refresh → 静默旋转（R2：只写 Cookie，无 token body）。
//
// 前端 request.js 的 refreshing 单飞依赖本端点语义与 controlplane 一致：
// 200 + Set-Cookie×2，body 不被消费。
func (g *Gateway) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rt := readCookie(r, refreshTokenCookieName)
	if rt == "" {
		writeError(w, http.StatusUnauthorized, "no refresh token")
		return
	}
	resp, err := g.svc.RefreshTokenWithFP(r.Context(), rt, collectDeviceFP(r))
	if err != nil {
		// 刷新失败清 Cookie（与 controlplane 同语义：会话终局，防残 Cookie 误导）。
		g.clearCookies(w)
		writeError(w, http.StatusUnauthorized, "refresh failed")
		return
	}
	g.setCookie(w, accessTokenCookieName, resp.AccessToken, int(resp.ExpiresIn))
	g.setCookie(w, refreshTokenCookieName, resp.RefreshToken, 7*24*3600)
	// R2：body 只回 status（前端 postEmpty 只查状态码）。
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleRegister POST /api/v1/auth/register {username,password,email} → 201 pending。
//
// 安全基线（R6）：注册默认 Status=pending 须 admin 审批（controlplane
// --allow-public-register=false 同语义；立即签发 token 的免审批模式不进 HTTP 层）。
func (g *Gateway) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	// A2 防爆破：注册入口同样过 IP 令牌桶（与 controlplane loginGuard 挂注册端点同语义）。
	if !g.guard.allowIP(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many attempts from this IP")
		return
	}
	// A2 强口令校验（与 controlplane validateStrongPassword 同规则集）。
	if msg := validateStrongPassword(body.Password); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if _, err := g.svc.CreateUser(r.Context(), &authv1.CreateUserRequest{
		Username: body.Username,
		Password: body.Password,
		Email:    body.Email,
	}); err != nil {
		if err == service.ErrUserExists {
			writeError(w, http.StatusConflict, "username already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 注册用户强制 pending（CreateUser 默认 active——注册流覆盖为 pending）。
	if u := g.svc.Store().GetUserByUsername(body.Username); u != nil {
		u.Status = "pending"
		_ = g.svc.Store().UpdateUser(u)
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"message": "registration submitted, pending admin approval",
	})
}

// handleChangePassword POST /api/v1/auth/change-password（R4：只走 token 模式）。
//
// 双流与 controlplane 对齐：
//   - 首登流：body 带 changePasswordToken（登录响应下发）→ 校验 token 取 userID →
//     验旧密 → 改密 → 清 MustChangePassword；
//   - 常规流：Cookie/Authorization 的 at → ValidateToken 取 userID → 验旧密 → 改密。
//
// 绝不接受 body.user_id 直调（gRPC 内部语义，HTTP 化即越权漏洞）。
func (g *Gateway) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		OldPassword         string `json:"oldPassword"`
		NewPassword         string `json:"newPassword"`
		ChangePasswordToken string `json:"changePasswordToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.OldPassword == "" || body.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "oldPassword and newPassword are required")
		return
	}
	// A2 强口令校验（新密码必须满足强度规则——与 controlplane 改密路径同语义）。
	if msg := validateStrongPassword(body.NewPassword); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	// userID 解析：首登 token 优先，回退常规 at（Cookie → Authorization Bearer）。
	var userID string
	token := body.ChangePasswordToken
	if token == "" {
		token = bearerOrCookie(r)
	}
	if token == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	claims, err := g.svc.ValidateToken(r.Context(), &authv1.ValidateTokenRequest{Token: token})
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}
	userID = claims.UserId

	if _, err := g.svc.ChangePassword(r.Context(), &authv1.ChangePasswordRequest{
		UserId:      userID,
		OldPassword: body.OldPassword,
		NewPassword: body.NewPassword,
	}); err != nil {
		if err == service.ErrPasswordMismatch {
			writeError(w, http.StatusUnauthorized, "old password does not match")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	g.clearCookies(w) // 改密后会话终局，前端引导重新登录（controlplane 同语义）。
	writeJSON(w, http.StatusOK, map[string]string{"status": "password changed"})
}

// handleMe GET /api/v1/auth/me → 当前用户（at Cookie → ValidateToken）。
func (g *Gateway) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	token := bearerOrCookie(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	resp, err := g.svc.ValidateToken(r.Context(), &authv1.ValidateTokenRequest{Token: token})
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}
	u := g.svc.Store().GetUser(resp.UserId)
	if u == nil {
		writeError(w, http.StatusUnauthorized, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       u.ID,
		"username": u.Username,
		"email":    u.Email,
		"roles":    u.RoleIDs,
	})
}

// ============ 用户/角色/权限管理 ============

// handleUsers GET 列表 / POST 创建（approve 用 POST {id}/approve）。
func (g *Gateway) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		resp, err := g.svc.ListUsers(r.Context(), nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": resp.Users})
	case http.MethodPost:
		// 管理员创建用户（区别于注册端点：管理端创建默认 active）。
		var body struct {
			Username string `json:"username"`
			Email    string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" {
			writeError(w, http.StatusBadRequest, "username is required")
			return
		}
		if _, err := g.svc.CreateUser(r.Context(), &authv1.CreateUserRequest{
			Username: body.Username,
			Email:    body.Email,
		}); err != nil {
			if err == service.ErrUserExists {
				writeError(w, http.StatusConflict, "username already exists")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleUserDetail GET {id} / DELETE {id} / POST {id}/approve|reject（注册审批，R6）。
func (g *Gateway) handleUserDetail(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/users/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	// 审批子路径：POST {id}/approve|reject。
	if strings.HasSuffix(rest, "/approve") || strings.HasSuffix(rest, "/reject") {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		// 鉴权：需 user:approve 权限（对齐 controlplane auth_users.go:163,197）。
		if _, ok := g.requirePermission(w, r, "user:approve"); !ok {
			return
		}
		id := strings.TrimSuffix(strings.TrimSuffix(rest, "/approve"), "/reject")
		action := "approve"
		if strings.HasSuffix(rest, "/reject") {
			action = "reject"
		}
		u := g.svc.Store().GetUser(id)
		if u == nil {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		if u.Status != "pending" {
			writeError(w, http.StatusConflict, "only pending users can be approved/rejected")
			return
		}
		if action == "approve" {
			u.Status = "active"
		} else {
			u.Status = "rejected"
		}
		if err := g.svc.Store().UpdateUser(u); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": u.Status})
		return
	}
	switch r.Method {
	case http.MethodPut:
		g.handleUpdateUser(w, r, rest)
	case http.MethodGet:
		u := g.svc.Store().GetUser(rest)
		if u == nil {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeJSON(w, http.StatusOK, u)
	case http.MethodDelete:
		if _, err := g.svc.DeleteUser(r.Context(), &authv1.DeleteUserRequest{Id: rest}); err != nil {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleUpdateUser 处理 PUT /api/v1/users/{id}：更新用户 email/roleIDs/status。
// 鉴权：需 user:write 权限；status 变更（active/rejected）额外要求 user:approve
// （对齐 controlplane auth_users.go:253，防低权限绕过审批流）。
// 请求体：{email?, roleIds?, status?}；仅更新提供的非空字段（与 controlplane
// handleUpdateUser 同语义）。
func (g *Gateway) handleUpdateUser(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := g.requirePermission(w, r, "user:write"); !ok {
		return
	}
	var body struct {
		Email   string   `json:"email"`
		RoleIDs []string `json:"roleIds"`
		Status  string   `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// 状态变更需更高权限：仅 user:write 不能激活/禁用账号，须 user:approve
	// （与 controlplane 审批模型一致，防低权限绕过审批流）。
	if body.Status != "" {
		if _, ok := g.requirePermission(w, r, "user:approve"); !ok {
			return
		}
	}
	if _, err := g.svc.UpdateUserFields(r.Context(), id, body.Email, body.Status, body.RoleIDs); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	// 响应与 GET 分支一致：返回 store.User。
	writeJSON(w, http.StatusOK, g.svc.Store().GetUser(id))
}

// handleRoles GET 列表 / POST 创建。
func (g *Gateway) handleRoles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		resp, err := g.svc.ListRoles(r.Context(), nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"roles": resp.Roles})
	case http.MethodPost:
		var body struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Permissions []string `json:"permissions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if _, err := g.svc.CreateRole(r.Context(), &authv1.CreateRoleRequest{
			Name:        body.Name,
			Description: body.Description,
			Permissions: body.Permissions,
		}); err != nil {
			if err == service.ErrRoleExists {
				writeError(w, http.StatusConflict, "role already exists")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleRoleDetail GET {id} / DELETE {id}。
func (g *Gateway) handleRoleDetail(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/roles/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		g.handleUpdateRole(w, r, rest)
	case http.MethodGet:
		resp, err := g.svc.GetRole(r.Context(), &authv1.GetRoleRequest{Id: rest})
		if err != nil {
			writeError(w, http.StatusNotFound, "role not found")
			return
		}
		writeJSON(w, http.StatusOK, resp)
	case http.MethodDelete:
		if _, err := g.svc.DeleteRole(r.Context(), &authv1.DeleteRoleRequest{Id: rest}); err != nil {
			writeError(w, http.StatusNotFound, "role not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleUpdateRole 处理 PUT /api/v1/roles/{id}：更新角色 description/permissions。
// 鉴权：需 role:write 权限（admin 角色拥有，与 controlplane requirePermission 同语义）。
// 请求体：{description?, permissions?}；仅更新提供的非空字段（与 controlplane
// handleUpdateRole 同语义）。
func (g *Gateway) handleUpdateRole(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := g.requirePermission(w, r, "role:write"); !ok {
		return
	}
	var body struct {
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	resp, err := g.svc.UpdateRoleFields(r.Context(), id, body.Description, body.Permissions)
	if err != nil {
		writeError(w, http.StatusNotFound, "role not found")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handlePermissions GET /api/v1/permissions。
func (g *Gateway) handlePermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	resp, err := g.svc.ListPermissions(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": resp.Permissions})
}

// ============ 工具 ============

// readCookie 读指定 Cookie（空串=不存在）。
func readCookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

// bearerOrCookie 优先 Authorization Bearer，回退 at Cookie。
func bearerOrCookie(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return readCookie(r, accessTokenCookieName)
}

// requirePermission 校验请求携带有效 token 且调用方拥有指定权限。
// 返回 ValidateTokenResponse 与 true 表示通过，否则已写入错误响应并返回 false。
// 与 controlplane requirePermission 同语义（权限经 token claims 展开）。
// admin 角色拥有全部权限（seedAdminRole），故 admin 自然通过所有 user:*/role:* 检查。
//
// permission 为空串语义（仅管理路由 GET role/permission 列表使用，对齐 controlplane
// auth_roles.go:40 / auth_perms.go:24 的 userFromToken 语义）：只要求有效登录，
// 不做权限点检查。当前仅 Gateway 内部传入该特殊值；对外仍要求非空权限名。
func (g *Gateway) requirePermission(w http.ResponseWriter, r *http.Request, permission string) (*authv1.ValidateTokenResponse, bool) {
	token := bearerOrCookie(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	resp, err := g.svc.ValidateToken(r.Context(), &authv1.ValidateTokenRequest{Token: token})
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return nil, false
	}
	if resp == nil || !resp.Valid {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return nil, false
	}
	u := g.svc.Store().GetUser(resp.UserId)
	if u == nil || u.Status != "active" {
		writeError(w, http.StatusUnauthorized, "user is not active")
		return nil, false
	}
	// 首登 token 仅供改密，不能据其全量权限访问管理端点。
	if u.MustChangePassword {
		writeError(w, http.StatusForbidden, "password change required")
		return nil, false
	}
	if permission == "" {
		return resp, true
	}
	for _, p := range resp.Permissions {
		if p == permission {
			return resp, true
		}
	}
	writeError(w, http.StatusForbidden, "insufficient permissions")
	return nil, false
}

// toPublicUser proto User → 网关公开字段（不含敏感）。
func toPublicUser(u *authv1.User) map[string]any {
	if u == nil {
		return nil
	}
	return map[string]any{
		"id":       u.Id,
		"username": u.Username,
		"email":    u.Email,
		"status":   u.Status,
		"roleIds":  u.RoleIds,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// 编译期依赖断言（store 包被 Gateway 经 service.Store() 间接消费）。
var _ = store.NewMemoryStore
var _ = auth.HashRefreshToken

// sessionIDFromToken 由 access token 计算 Session ID（SHA-256 摘要）。
// 用于 Redis SessionStore 的 session 标识：Login 时 Create，Logout 时 Revoke。
// 用 hash 而非明文 token 作 key，避免 token 明文暴露在 Redis key 中。
func sessionIDFromToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
