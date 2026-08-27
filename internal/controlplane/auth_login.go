// auth_login.go 实现认证 handler：注册/登录/登出/刷新/当前用户/改密。
//
// 从 auth.go 拆分而来（纯代码移动，未修改任何逻辑）。依赖 auth.go 中的核心 helper：
//   - loginGuard（防爆破/限流）、hashPassword/verifyPassword（bcrypt）；
//   - issueUserToken（JWT 签发）、createRefreshToken/consumeRefreshToken/revokeRefreshToken（rt 旋转）；
//   - createChangePasswordToken/consumeChangePasswordToken（首登强制改密令牌）；
//   - setAuthCookies/clearAuthCookies（双 HttpOnly Cookie）、deviceFingerprint（设备绑定）；
//   - userFromToken（Bearer/Cookie 鉴权）、validateStrongPassword（强口令校验）；
//   - randHexID（ID 分配）、authResponse（响应体）。
package controlplane

import (
	"opsmesh/internal/controlplane/paginate"
	"log"
	"net/http"
	"strings"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// ============================================================================
// 认证 handler：register / login / me
// ============================================================================

// handleAuthRegister 处理 POST /api/v1/auth/register：用户注册。
// 请求体：{username, password, email?}；密码最短 6 字符，bcrypt 哈希后存库。
//
// 注册安全：
//   - --public-register=false 时返回 403 拒绝公开注册（仅管理员可经 POST /api/v1/users 创建）；
//   - --allow-public-register=true 时新用户 Status="active" 并立即签发 token（仅演示/内网受信环境）；
//   - 否则（默认 --allow-public-register=false）新用户 Status="pending"，不签发 token，
//     返回 201 {"message": "registration submitted, pending admin approval"}，须管理员审批后激活。
//
// 用户名重复返回 409。
func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 注册安全：--public-register=false 时关闭公开注册接口。
	if !s.cfg.PublicRegister {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "public registration is disabled"})
		return
	}
	// 限流：按客户端 IP 令牌桶约束注册频率，防滥用/枚举。
	if !s.loginGuard.allow(clientIP(r, s.cfg.TrustProxy)) {
		paginate.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests, slow down"})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		log.Printf("controlplane: handleAuthRegister 解析请求体失败: %v", err)
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Username == "" || body.Password == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	if msg := validateStrongPassword(body.Password); msg != "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	// 用户名重复校验。
	if existing := s.store.GetUserByUsername(body.Username); existing != nil {
		paginate.WriteJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	// 默认角色存在性校验：注册用户硬编码绑定 role-viewer，须确保该角色已 seed 入库。
	// 若角色缺失（seed 未执行/被误删），继续创建会把无效角色引用写入用户记录，导致后续
	// 权限展开失败（userPermissions 跳过 nil 角色），用户实际无任何权限且难以排查。
	// 此处前置校验快速失败暴露配置问题，返回 500 表明是服务端数据缺陷而非客户端错误。
	if s.store.GetRole("role-viewer") == nil {
		log.Printf("controlplane: handleAuthRegister 默认角色 role-viewer 不存在，注册中止")
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "default role not found"})
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		log.Printf("controlplane: handleAuthRegister 哈希密码失败: %v", err)
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	// 注册安全：只有显式 --allow-public-register=true 时才免审批（Status=active + 立即签发 token）。
	// 否则所有注册（包括 demo 模式）都走 pending 审批流程（默认安全基线）。
	initialStatus := "pending"
	if s.cfg.AllowPublicRegister {
		initialStatus = "active"
	}
	u := &store.User{
		ID:           randHexID("user"),
		Username:     body.Username,
		Email:        body.Email,
		PasswordHash: hash,
		Status:       initialStatus,
		RoleIDs:      []string{"role-viewer"}, // 注册用户默认 viewer 角色
	}
	// 注：store.CreateUser 内部也会校验用户名唯一性（兜底），此处提前检查以提供更友好的错误消息。
	if s.store.CreateUser(u) == nil {
		paginate.WriteJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: u.ID, Action: "user_register", Target: u.ID, Detail: sanitizeAuditDetail("username=" + u.Username + " status=" + initialStatus),
	})
	// 只有 --allow-public-register=true 时才立即签发 token；否则返回 pending 提示。
	if s.cfg.AllowPublicRegister {
		token, err := s.issueUserToken(u)
		if err != nil {
			log.Printf("controlplane: handleAuthRegister 签发 token 失败: %v", err)
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		rt, err := s.createRefreshToken(u.ID, deviceFingerprint(r))
		if err != nil {
			log.Printf("controlplane: handleAuthRegister 生成刷新令牌失败: %v", err)
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		s.setAuthCookies(w, token, rt) // ：at+rt 双 HttpOnly Cookie 下发
		paginate.WriteJSON(w, http.StatusCreated, authResponse{Token: token, User: u})
		return
	}
	// 默认：不签发 token，返回待审批提示。
	paginate.WriteJSON(w, http.StatusCreated, map[string]string{
		"message": "registration submitted, pending admin approval",
		"userId":  u.ID,
		"status":  u.Status,
	})
}

// handleAuthLogin 处理 POST /api/v1/auth/login：用户登录。
// 请求体：{username, password}；校验 bcrypt 哈希后签发 JWT。
// 成功返回 200 {token, user}；用户名不存在/密码错误返回 401。
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 限流：按客户端 IP 令牌桶约束登录频率，防撞库与 DoS。
	if !s.loginGuard.allow(clientIP(r, s.cfg.TrustProxy)) {
		paginate.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests, slow down"})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		log.Printf("controlplane: handleAuthLogin 解析请求体失败: %v", err)
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Username == "" || body.Password == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	// 防爆破：账号处于锁定态时直接拒绝，避免继续尝试。
	if s.loginGuard.locked(body.Username) {
		paginate.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "account temporarily locked due to too many failed attempts, try later"})
		return
	}
	u := s.store.GetUserByUsername(body.Username)
	if u == nil {
		// 用户名不存在也计入限流计数窗口（不暴露账号是否存在，同样走锁定逻辑防枚举）。
		s.loginGuard.recordFail(body.Username)
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}
	if !verifyPassword(u.PasswordHash, body.Password) {
		s.loginGuard.recordFail(body.Username)
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}
	// 注册安全：密码已校验通过后再检查 Status，根据状态返回差异化提示。
	// 顺序保证：未持正确密码的攻击者无法探测账号状态（防枚举）。
	if u.Status != "active" {
		switch u.Status {
		case "pending":
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "account pending admin approval"})
		case "disabled":
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "account disabled"})
		case "rejected":
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "account registration rejected"})
		default:
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "account not active"})
		}
		return
	}
	// 登录成功：清除失败计数（解锁）。
	s.loginGuard.resetFail(body.Username)
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: u.ID, Action: "user_login", Target: u.ID, Detail: sanitizeAuditDetail("username=" + u.Username),
	})
	// ：mustChangePassword=true 时不签发 access token（at），仅签发一次性短时效
	// changePasswordToken（5min），仅可用于 /api/v1/auth/change-password。改密成功后才签发
	// 正式 at+rt。避免弱口令用户持有效 at 长期访问受保护 API。
	if u.MustChangePassword {
		cpt, err := s.createChangePasswordToken(u.ID)
		if err != nil {
			log.Printf("controlplane: handleAuthLogin 生成改密令牌失败: %v", err)
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, authResponse{
			User:                u,
			MustChangePassword:  true,
			ChangePasswordToken: cpt,
		})
		return
	}
	token, err := s.issueUserToken(u)
	if err != nil {
		log.Printf("controlplane: handleAuthLogin 签发 token 失败: %v", err)
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	// 双 Cookie：at（短寿命，JS 不可读）+ rt（长寿命，服务端可吊销/旋转）。
	rt, err := s.createRefreshToken(u.ID, deviceFingerprint(r))
	if err != nil {
		log.Printf("controlplane: handleAuthLogin 生成刷新令牌失败: %v", err)
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	s.setAuthCookies(w, token, rt)
	paginate.WriteJSON(w, http.StatusOK, authResponse{Token: token, User: u})
}

// handleAuthLogout 处理 POST /api/v1/auth/logout：登出并清除会话 Cookie。
// 登出时将 access token 的 jti 加入吊销黑名单，使 token 立即失效
// （而非等 15min 自然过期）。同时吊销 refresh token 并清除 HttpOnly Cookie。
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if u, err := s.userFromToken(r); err == nil {
		// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
		s.audit(r.Context(), &proto.AuditEvent{
			TenantID: "default", UserID: u.ID, Action: "user_logout", Target: u.ID, Detail: sanitizeAuditDetail("username=" + u.Username),
		})
	}
	// 吊销 access token：jti 加入黑名单，使登出后 token 立即失效。
	s.revokeAccessTokenFromRequest(r)
	// 吊销请求携带的 rt，并清除 at+rt Cookie（服务端状态失效 + 浏览器会话终止）。
	if ck, ckErr := r.Cookie(refreshTokenCookieName); ckErr == nil && strings.TrimSpace(ck.Value) != "" {
		s.revokeRefreshToken(ck.Value)
	}
	s.clearAuthCookies(w)
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

// handleAuthRefresh 处理 POST /api/v1/auth/refresh：用 rt Cookie 静默换取新 at+rt（旋转）。
// 同源 HttpOnly rt 由浏览器自动携带；成功重置 at（短寿命）+ 新 rt，旧 rt 立即失效（防重放）。
// 缺失/无效/过期 rt → 401 并清除 Cookie（前端据此跳转登录）。
//
// 设备绑定：验证时校验请求的 X-Device-FP 与存储的 DeviceFP 一致，
// 不匹配拒绝刷新（防 token 跨设备重放）；签发新 rt 时绑定当前设备指纹。
func (s *Server) handleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ck, err := r.Cookie(refreshTokenCookieName)
	if err != nil || strings.TrimSpace(ck.Value) == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing refresh token"})
		return
	}
	// 设备绑定：consumeRefreshToken 校验请求的 DeviceFP 与存储一致。
	sess, ok := s.consumeRefreshToken(ck.Value, deviceFingerprint(r))
	if !ok {
		s.clearAuthCookies(w)
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired refresh token"})
		return
	}
	u := s.store.GetUser(sess.UserID)
	if u == nil || u.Status != "active" {
		s.clearAuthCookies(w)
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "user not active"})
		return
	}
	at, err := s.issueUserToken(u)
	if err != nil {
		log.Printf("controlplane: handleAuthRefresh 签发 token 失败: %v", err)
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	// 签发新 rt 时绑定当前设备指纹（与 consume 时的 DeviceFP 一致，实现设备绑定旋转）。
	rt, err := s.createRefreshToken(u.ID, deviceFingerprint(r))
	if err != nil {
		log.Printf("controlplane: handleAuthRefresh 生成刷新令牌失败: %v", err)
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	s.setAuthCookies(w, at, rt)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"user": u})
}

// handleAuthMe 处理 GET /api/v1/auth/me：返回当前登录用户信息。
// 从 Authorization: Bearer <token> 提取用户 ID，查库返回最新用户信息。
// 无 token / token 无效 / 用户不存在 → 401。
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	u, err := s.userFromToken(r)
	if err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	// 展开角色 → 有效权限集合，供前端侧栏按权限过滤功能入口（与 requireProd 闸同源）。
	// 取并集去重；role_ids 解析失败或角色不存在时跳过该角色，不阻断主流程。
	eff := make([]string, 0, 16)
	seen := make(map[string]bool, 16)
	for _, rid := range u.RoleIDs {
		role := s.store.GetRole(rid)
		if role == nil {
			continue
		}
		for _, p := range role.Permissions {
			if !seen[p] {
				seen[p] = true
				eff = append(eff, p)
			}
		}
	}
	u.EffectivePermissions = eff
	paginate.WriteJSON(w, http.StatusOK, u)
}

// ============================================================================
// 改密 handler（安全债）：POST /api/v1/auth/change-password
// ============================================================================

// handleAuthChangePassword 处理 POST /api/v1/auth/change-password：用户改密（安全债）。
//
// 支持两种鉴权场景：
//  1. 首登强制改密（mustChangePassword=true）：请求体携带 changePasswordToken（登录时签发的一次性
//     短时效 token），消费后获取用户身份。改密成功后签发正式 at+rt 并下发 Cookie，前端据此进入正常会话。
//  2. 已登录用户主动改密：携带 Authorization: Bearer <token> 或 at Cookie（userFromToken 鉴权）。
//     改密成功后不重新签发 token（现有 at 仍有效）。
//
// 请求体：{oldPassword, newPassword, changePasswordToken?}。
// 流程：鉴权 → 校验旧密码 → 校验新密码强度 → bcrypt 哈希 → 落库 → 清除 mustChangePassword
//
//	→（首登场景）签发正式 at+rt。
//
// 新密码强度：≥8 字符且含大小写字母与数字。新旧相同拒绝（防无效改密）。
func (s *Server) handleAuthChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 限流：按客户端 IP 令牌桶约束改密频率，防暴力破解旧密码。
	// 复用 loginGuard 的 IP 令牌桶（与登录/注册同维度），避免单独维护限流器。
	if !s.loginGuard.allow(clientIP(r, s.cfg.TrustProxy)) {
		paginate.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests, slow down"})
		return
	}
	var body struct {
		OldPassword         string `json:"oldPassword"`
		NewPassword         string `json:"newPassword"`
		ChangePasswordToken string `json:"changePasswordToken"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		log.Printf("controlplane: handleAuthChangePassword 解析请求体失败: %v", err)
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.OldPassword == "" || body.NewPassword == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "oldPassword and newPassword are required"})
		return
	}
	// 鉴权：优先使用 changePasswordToken（首登强制改密场景），否则走 userFromToken（已登录主动改密）。
	var u *store.User
	firstLoginChange := false
	if body.ChangePasswordToken != "" {
		userID, ok := s.consumeChangePasswordToken(body.ChangePasswordToken)
		if !ok {
			paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired change password token"})
			return
		}
		user := s.store.GetUser(userID)
		if user == nil {
			paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired change password token"})
			return
		}
		u = user
		firstLoginChange = true
	} else {
		user, err := s.userFromToken(r)
		if err != nil {
			paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		u = user
	}
	// 旧密码校验：与当前 PasswordHash 比对，失败返回 401（防越权改密）。
	if !verifyPassword(u.PasswordHash, body.OldPassword) {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "old password incorrect"})
		return
	}
	// 新旧相同拒绝（防无效改密绕过强制改密）。
	if body.OldPassword == body.NewPassword {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "new password must differ from old password"})
		return
	}
	// 新密码强度校验。
	if msg := validateStrongPassword(body.NewPassword); msg != "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	// bcrypt 哈希新密码。
	newHash, err := hashPassword(body.NewPassword)
	if err != nil {
		log.Printf("controlplane: handleAuthChangePassword 哈希密码失败: %v", err)
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	// 落库：写入新哈希并清除 must_change_password 标记。
	if !s.store.ChangePassword(u.ID, newHash) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: u.ID, Action: "user_change_password", Target: u.ID, Detail: sanitizeAuditDetail("username=" + u.Username),
	})
	// 首登强制改密场景：改密成功后签发正式 at+rt，前端据此进入正常会话。
	if firstLoginChange {
		token, err := s.issueUserToken(u)
		if err != nil {
			log.Printf("controlplane: handleAuthChangePassword 签发 token 失败: %v", err)
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		rt, err := s.createRefreshToken(u.ID, deviceFingerprint(r))
		if err != nil {
			log.Printf("controlplane: handleAuthChangePassword 生成刷新令牌失败: %v", err)
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		s.setAuthCookies(w, token, rt)
		paginate.WriteJSON(w, http.StatusOK, authResponse{Token: token, User: u})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"message": "password changed"})
}
