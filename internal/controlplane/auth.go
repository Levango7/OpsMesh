// auth.go 实现用户中心 HTTP handler：注册/登录/查询当前用户 + 用户/角色/权限 CRUD。
//
// 与 server.go 中已有的"网关注入身份"模式（authctx.FromHTTPHeader）互补：
//   - 网关注入模式：内核不自鉴权，身份由前置网关（APISIX/IAM）注入 X-Tenant-ID 等头；
//   - 用户中心模式：内核自行管理用户/角色/权限，登录/注册后签发 JWT，后续请求携带 token 鉴权。
//
// 两种模式并存：网关注入用于 agent↔控制面 gRPC 通道（已有），用户中心用于 B/S 仪表盘登录（新增）。
//
// 鉴权流程：
//  1. POST /api/v1/auth/register | login → 校验凭据 → 签发 JWT（HS256）→ 返回 {token, user}；
//  2. 后续请求携带 Authorization: Bearer <token>；
//  3. 用户管理 API（/api/v1/users、/api/v1/roles）从 token 提取用户 → 校验 user:read/write 权限；
//  4. /api/v1/auth/me 从 token 提取用户信息返回。
//
// 安全要点：
//   - 密码 bcrypt 哈希（绝不存明文）；最短 6 字符；
//   - JWT 密钥来自 config.JWTSecret（空=随机生成，重启后旧 token 失效）；
//   - 用户管理 API 需认证 + 权限校验（user:read/user:write/user:delete）；
//   - 错误响应统一 {"error": "message"} 格式。
package controlplane

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/authctx"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"

	"golang.org/x/crypto/bcrypt"
)

// 双 HttpOnly Cookie 令牌方案（同源最简单且安全）：
//   - at（access token）：短期 JWT（15min），仅标识身份，XSS 窃取后利用窗口极小；
//   - rt（refresh token）：长期不透明随机串（7d），服务端可吊销/旋转，用于静默续期。
// 两者均为 HttpOnly + SameSite=Lax（防 XSS 读取 / 防 CSRF 跨站携带），同源由浏览器自动携带。
const (
	accessTokenCookieName  = "opsmesh_at"
	refreshTokenCookieName = "opsmesh_rt"
	accessTokenExpiry      = 15 * time.Minute
	refreshTokenExpiry     = 7 * 24 * time.Hour
)

// setCookie 统一的 HttpOnly Cookie 写入（Path=/、SameSite=Lax；HTTPS 部署才置 Secure）。
func (s *Server) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.TLSCert != "", // HTTPS 部署才置 Secure（明文 http 内网下不设，否则会话丢失）
	})
}

// setAccessCookie 下发短期访问令牌（at）HttpOnly Cookie。
func (s *Server) setAccessCookie(w http.ResponseWriter, token string) {
	s.setCookie(w, accessTokenCookieName, token, int(accessTokenExpiry.Seconds()))
}

// setRefreshCookie 下发刷新令牌（rt）HttpOnly Cookie（长期，服务端可吊销/旋转）。
func (s *Server) setRefreshCookie(w http.ResponseWriter, rt string) {
	s.setCookie(w, refreshTokenCookieName, rt, int(refreshTokenExpiry.Seconds()))
}

// setAuthCookies 同时下发 at + rt（登录/刷新成功时调用）。
func (s *Server) setAuthCookies(w http.ResponseWriter, at, rt string) {
	s.setAccessCookie(w, at)
	s.setRefreshCookie(w, rt)
}

// clearAuthCookies 清除 at + rt（登出时调用）。
func (s *Server) clearAuthCookies(w http.ResponseWriter) {
	s.setCookie(w, accessTokenCookieName, "", -1)
	s.setCookie(w, refreshTokenCookieName, "", -1)
}

// ============================================================================
// 刷新令牌存储（服务端状态，支持吊销与旋转）—— MVP 采用进程内存储。
// 多副本/重启场景应替换为 DB/Redis（接口稳定，后续平滑替换）。
// ============================================================================

// refreshSession 刷新令牌会话记录。
type refreshSession struct {
	UserID    string
	ExpiresAt time.Time
}

var refreshTokens = struct {
	sync.Mutex
	m map[string]*refreshSession
}{m: make(map[string]*refreshSession)}

// createRefreshToken 生成并存储一个刷新令牌 ID（crypto/rand，32 字节十六进制）。
func createRefreshToken(userID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	refreshTokens.Lock()
	refreshTokens.m[id] = &refreshSession{UserID: userID, ExpiresAt: time.Now().Add(refreshTokenExpiry)}
	refreshTokens.Unlock()
	return id, nil
}

// consumeRefreshToken 校验并消费刷新令牌（一次性：校验通过即删除，实现旋转）。
// 无效/过期/已消费返回 (nil, false)。
func consumeRefreshToken(id string) (*refreshSession, bool) {
	refreshTokens.Lock()
	defer refreshTokens.Unlock()
	rs, ok := refreshTokens.m[id]
	if !ok {
		return nil, false
	}
	delete(refreshTokens.m, id) // 旋转：旧 rt 立即作废，防重放
	if time.Now().After(rs.ExpiresAt) {
		return nil, false
	}
	return rs, true
}

// revokeRefreshToken 吊销指定刷新令牌（登出时调用）。
func revokeRefreshToken(id string) {
	refreshTokens.Lock()
	delete(refreshTokens.m, id)
	refreshTokens.Unlock()
}

// revokeUserRefreshTokens 吊销某用户全部刷新令牌（禁用/删除账号时收回全部会话）。
func revokeUserRefreshTokens(userID string) {
	refreshTokens.Lock()
	for k, v := range refreshTokens.m {
		if v.UserID == userID {
			delete(refreshTokens.m, k)
		}
	}
	refreshTokens.Unlock()
}

// purgeExpiredRefreshTokens 清理过期刷新令牌，防内存无限增长（由 startRefreshSweep 周期调用）。
func purgeExpiredRefreshTokens() {
	now := time.Now()
	refreshTokens.Lock()
	for k, v := range refreshTokens.m {
		if now.After(v.ExpiresAt) {
			delete(refreshTokens.m, k)
		}
	}
	refreshTokens.Unlock()
}

// randHexID 生成随机十六进制 ID（16 字节，crypto/rand 密码学安全）。
// 用于用户/角色 ID 分配（调用方未填 ID 时）。
func randHexID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 熵源失败回退时间戳（降级但可容忍，唯一性由 store 索引兜底）。
		return prefix + "-" + hex.EncodeToString([]byte(time.Now().Format("20060102150405"))) + "fallback"
	}
	return prefix + "-" + hex.EncodeToString(b)
}

// hashPassword 用 bcrypt 哈希密码（DefaultCost=10）。
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// verifyPassword 校验 bcrypt 哈希与明文密码是否匹配。
func verifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ============================================================================
// loginGuard 登录/注册防爆破 + 限流（P1-4，进程内实现）。
//   - 限流：按客户端 IP 令牌桶，约束单位时间登录/注册尝试次数，防撞库与 DoS。
//   - 防爆破：按用户名累计失败次数，超阈值临时锁定账号，挫败密码爆破。
//
// 设计为进程内（单副本足够）；多副本 HA 部署下各副本独立计数，建议后续以 Redis
// 共享计数（接口此处保持稳定即可平滑替换）。
// ============================================================================

const (
	loginRateBurst  = 10               // 令牌桶容量（瞬时允许的最大尝试数）
	loginRateRefill = 1.0 / 3.0        // 令牌补充速率（每秒），约每 3s 1 个，≈20/min
	loginMaxFails   = 5                // 单账号允许的连续失败次数
	loginFailWindow = 15 * time.Minute // 失败计数滑动窗口
	loginLockDur    = 15 * time.Minute // 账号锁定时长
)

type loginGuard struct {
	mu    sync.Mutex
	ips   map[string]*rateRec // 客户端 IP -> 限流令牌桶
	fails map[string]*failRec // 用户名 -> 失败计数记录
}

type rateRec struct {
	tokens float64
	last   time.Time
}

type failRec struct {
	count       int
	firstAt     time.Time
	lockedUntil time.Time
}

func newLoginGuard() *loginGuard {
	return &loginGuard{
		ips:   make(map[string]*rateRec),
		fails: make(map[string]*failRec),
	}
}

// allow 按 IP 令牌桶判断本次尝试是否被限流（true=放行）。
func (g *loginGuard) allow(ip string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	rec, ok := g.ips[ip]
	if !ok {
		rec = &rateRec{tokens: loginRateBurst, last: now}
		g.ips[ip] = rec
	}
	elapsed := now.Sub(rec.last).Seconds()
	rec.tokens += elapsed * loginRateRefill
	if rec.tokens > loginRateBurst {
		rec.tokens = loginRateBurst
	}
	rec.last = now
	if rec.tokens < 1 {
		return false // 令牌不足，限流
	}
	rec.tokens--
	return true
}

// recordFail 记录一次账号失败尝试；返回是否触发锁定。
func (g *loginGuard) recordFail(username string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	rec, ok := g.fails[username]
	if !ok || now.Sub(rec.firstAt) > loginFailWindow {
		rec = &failRec{count: 0, firstAt: now}
		g.fails[username] = rec
	}
	rec.count++
	if rec.count >= loginMaxFails {
		rec.lockedUntil = now.Add(loginLockDur)
		return true
	}
	return false
}

// locked 判断账号当前是否处于锁定状态。
func (g *loginGuard) locked(username string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	rec, ok := g.fails[username]
	if !ok {
		return false
	}
	return time.Now().Before(rec.lockedUntil)
}

// resetFail 登录成功后清除该账号失败计数（解锁）。
func (g *loginGuard) resetFail(username string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.fails, username)
}

// startSweep 启动后台回收 goroutine，定期清理过期限流令牌桶与已解锁且超窗的失败计数，
// 防止 ips/fails map 在长运行中无界增长（原实现只增不删，进程内内存泄漏）。
// 仅在 NewServer（生产路径）调用；测试直接构造 Server 不触发，避免测试悬挂 goroutine。
func (g *loginGuard) startSweep(interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			g.sweep()
		}
	}()
}

// sweep 清理过期条目：
//   - ips：令牌已回满（无待补充）且超过 1 小时无新活动 → 回收；
//   - fails：当前未锁定且失败窗口已过的失败计数 → 回收。
func (g *loginGuard) sweep() {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	for ip, rec := range g.ips {
		if rec.tokens >= loginRateBurst && now.Sub(rec.last) > time.Hour {
			delete(g.ips, ip)
		}
	}
	for user, rec := range g.fails {
		if now.After(rec.lockedUntil) && now.Sub(rec.firstAt) > loginFailWindow {
			delete(g.fails, user)
		}
	}
}

// clientIP 提取客户端真实 IP。
// trustProxy=false（默认，安全）：仅用 RemoteAddr，防止客户端伪造 X-Forwarded-For 绕过登录限流/审计；
// trustProxy=true（确有可信反代/LB 前置并注入真实 IP 时）：信任 XFF 首段。
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if idx := strings.Index(xff, ","); idx > 0 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// userPermissions 展开用户经角色获得的全部权限字符串（去重）。
// 用户 → RoleIDs → 各 Role.Permissions → 合并去重。
func (s *Server) userPermissions(u *store.User) []string {
	if u == nil || len(u.RoleIDs) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, rid := range u.RoleIDs {
		r := s.store.GetRole(rid)
		if r == nil {
			continue
		}
		for _, p := range r.Permissions {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// userRoleNames 返回用户绑定的角色名列表（用于 JWT claims，便于前端展示）。
func (s *Server) userRoleNames(u *store.User) []string {
	if u == nil || len(u.RoleIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(u.RoleIDs))
	for _, rid := range u.RoleIDs {
		if r := s.store.GetRole(rid); r != nil {
			out = append(out, r.Name)
		}
	}
	return out
}

// issueUserToken 为用户签发 JWT token。
// claims 包含：用户 ID/用户名/角色 ID/权限/租户/过期时间。
func (s *Server) issueUserToken(u *store.User) (string, error) {
	claims := authctx.JWTClaims{
		UserID:      u.ID,
		Username:    u.Username,
		Roles:       u.RoleIDs,
		Permissions: s.userPermissions(u),
		TenantID:    "default", // 用户中心为平台级，统一 "default" 租户
		ExpiresAt:   time.Now().Add(accessTokenExpiry),
	}
	return authctx.SignJWT(claims, s.jwtSecret)
}

// authResponse 登录/注册成功响应体。
// MustChangePassword（安全债 85）：当用户首登须改密时为 true，前端据此弹出改密对话框。
type authResponse struct {
	Token              string      `json:"token"`
	User               *store.User `json:"user"`
	MustChangePassword bool        `json:"mustChangePassword"`
}

// ============================================================================
// 认证 handler：register / login / me
// ============================================================================

// handleAuthRegister 处理 POST /api/v1/auth/register：用户注册。
// 请求体：{username, password, email?}；密码最短 6 字符，bcrypt 哈希后存库。
//
// 注册安全（P1-7）：
//   - --public-register=false 时返回 403 拒绝公开注册（仅管理员可经 POST /api/v1/users 创建）；
//   - --allow-public-register=true 时新用户 Status="active" 并立即签发 token（仅演示/内网受信环境）；
//   - 否则（默认 --allow-public-register=false）新用户 Status="pending"，不签发 token，
//     返回 201 {"message": "registration submitted, pending admin approval"}，须管理员审批后激活。
//
// 用户名重复返回 409。
func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// P1-7 注册安全：--public-register=false 时关闭公开注册接口。
	if !s.cfg.PublicRegister {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "public registration is disabled"})
		return
	}
	// P1-4 限流：按客户端 IP 令牌桶约束注册频率，防滥用/枚举。
	if !s.loginGuard.allow(clientIP(r, s.cfg.TrustProxy)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests, slow down"})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Username == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	if msg := validateStrongPassword(body.Password); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	// 用户名重复校验。
	if existing := s.store.GetUserByUsername(body.Username); existing != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "password hash failed: " + err.Error()})
		return
	}
	// P1-7 注册安全：只有显式 --allow-public-register=true 时才免审批（Status=active + 立即签发 token）。
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
	if s.store.CreateUser(u) == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: u.ID, Action: "user_register", Target: u.ID, Detail: "username=" + u.Username + " status=" + initialStatus,
	})
	// 只有 --allow-public-register=true 时才立即签发 token；否则返回 pending 提示。
	if s.cfg.AllowPublicRegister {
		token, err := s.issueUserToken(u)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token sign failed: " + err.Error()})
			return
		}
		rt, err := createRefreshToken(u.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "refresh token gen failed: " + err.Error()})
			return
		}
		s.setAuthCookies(w, token, rt) // task 94：at+rt 双 HttpOnly Cookie 下发
		writeJSON(w, http.StatusCreated, authResponse{Token: token, User: u})
		return
	}
	// 默认：不签发 token，返回待审批提示。
	writeJSON(w, http.StatusCreated, map[string]string{
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
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// P1-4 限流：按客户端 IP 令牌桶约束登录频率，防撞库与 DoS。
	if !s.loginGuard.allow(clientIP(r, s.cfg.TrustProxy)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests, slow down"})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Username == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	// P1-4 防爆破：账号处于锁定态时直接拒绝，避免继续尝试。
	if s.loginGuard.locked(body.Username) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "account temporarily locked due to too many failed attempts, try later"})
		return
	}
	u := s.store.GetUserByUsername(body.Username)
	if u == nil {
		// 用户名不存在也计入限流计数窗口（不暴露账号是否存在，同样走锁定逻辑防枚举）。
		s.loginGuard.recordFail(body.Username)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}
	if !verifyPassword(u.PasswordHash, body.Password) {
		s.loginGuard.recordFail(body.Username)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}
	// P1-7 注册安全：密码已校验通过后再检查 Status，根据状态返回差异化提示。
	// 顺序保证：未持正确密码的攻击者无法探测账号状态（防枚举）。
	if u.Status != "active" {
		switch u.Status {
		case "pending":
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "account pending admin approval"})
		case "disabled":
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "account disabled"})
		case "rejected":
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "account registration rejected"})
		default:
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "account not active"})
		}
		return
	}
	// 登录成功：清除失败计数（解锁）。
	s.loginGuard.resetFail(body.Username)
	token, err := s.issueUserToken(u)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token sign failed: " + err.Error()})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: u.ID, Action: "user_login", Target: u.ID, Detail: "username=" + u.Username,
	})
	// 双 Cookie：at（短寿命，JS 不可读）+ rt（长寿命，服务端可吊销/旋转）。
	rt, err := createRefreshToken(u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "refresh token gen failed: " + err.Error()})
		return
	}
	s.setAuthCookies(w, token, rt)
	// 安全债 85：返回 mustChangePassword 标记，前端据此弹出改密对话框。
	writeJSON(w, http.StatusOK, authResponse{Token: token, User: u, MustChangePassword: u.MustChangePassword})
}

// handleAuthLogout 处理 POST /api/v1/auth/logout：登出并清除会话 Cookie（task 94）。
// JWT 为无状态令牌，服务端不做黑名单（MVP）；清除 HttpOnly Cookie 即终止浏览器会话，
// 前端同时清空内存 token。token 自然过期由 jwtTokenExpiry（24h）约束。
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if u, err := s.userFromToken(r); err == nil {
		s.store.Audit(&proto.AuditEvent{
			TenantID: "default", UserID: u.ID, Action: "user_logout", Target: u.ID, Detail: "username=" + u.Username,
		})
	}
	// 吊销请求携带的 rt，并清除 at+rt Cookie（服务端状态失效 + 浏览器会话终止）。
	if ck, ckErr := r.Cookie(refreshTokenCookieName); ckErr == nil && strings.TrimSpace(ck.Value) != "" {
		revokeRefreshToken(ck.Value)
	}
	s.clearAuthCookies(w)
	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

// handleAuthRefresh 处理 POST /api/v1/auth/refresh：用 rt Cookie 静默换取新 at+rt（旋转）。
// 同源 HttpOnly rt 由浏览器自动携带；成功重置 at（短寿命）+ 新 rt，旧 rt 立即失效（防重放）。
// 缺失/无效/过期 rt → 401 并清除 Cookie（前端据此跳转登录）。
func (s *Server) handleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ck, err := r.Cookie(refreshTokenCookieName)
	if err != nil || strings.TrimSpace(ck.Value) == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing refresh token"})
		return
	}
	sess, ok := consumeRefreshToken(ck.Value)
	if !ok {
		s.clearAuthCookies(w)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired refresh token"})
		return
	}
	u := s.store.GetUser(sess.UserID)
	if u == nil || u.Status != "active" {
		s.clearAuthCookies(w)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user not active"})
		return
	}
	at, err := s.issueUserToken(u)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token sign failed: " + err.Error()})
		return
	}
	rt, err := createRefreshToken(u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "refresh token gen failed: " + err.Error()})
		return
	}
	s.setAuthCookies(w, at, rt)
	writeJSON(w, http.StatusOK, map[string]interface{}{"user": u})
}

// handleAuthMe 处理 GET /api/v1/auth/me：返回当前登录用户信息。
// 从 Authorization: Bearer <token> 提取用户 ID，查库返回最新用户信息。
// 无 token / token 无效 / 用户不存在 → 401。
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	u, err := s.userFromToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
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
	writeJSON(w, http.StatusOK, u)
}

// ============================================================================
// 改密 handler（安全债 85）：POST /api/v1/auth/change-password
// ============================================================================

// changePasswordMinLen 改密新密码最短长度（强口令基线：8 字符）。
const changePasswordMinLen = 8

// validateStrongPassword 强口令校验（安全债 85）：至少 8 字符，包含大小写字母与数字。
// 返回不满足时的可读提示（满足返回空串）。
func validateStrongPassword(pw string) string {
	if len(pw) < changePasswordMinLen {
		return "password too short (min 8 chars)"
	}
	var hasUpper, hasLower, hasDigit bool
	for _, c := range pw {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		}
	}
	if !hasUpper {
		return "password must contain at least one uppercase letter"
	}
	if !hasLower {
		return "password must contain at least one lowercase letter"
	}
	if !hasDigit {
		return "password must contain at least one digit"
	}
	return ""
}

// handleAuthChangePassword 处理 POST /api/v1/auth/change-password：用户改密（安全债 85）。
// 请求体：{oldPassword, newPassword}；鉴权：须携带当前用户有效 token。
// 流程：从 token 提取用户 → 校验旧密码 → 校验新密码强度 → bcrypt 哈希 → 落库 → 清除 mustChangePassword。
// 新密码强度：≥8 字符且含大小写字母与数字。新旧相同拒绝（防无效改密）。
func (s *Server) handleAuthChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	u, err := s.userFromToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	var body struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.OldPassword == "" || body.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "oldPassword and newPassword are required"})
		return
	}
	// 旧密码校验：与当前 PasswordHash 比对，失败返回 401（防越权改密）。
	if !verifyPassword(u.PasswordHash, body.OldPassword) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "old password incorrect"})
		return
	}
	// 新旧相同拒绝（防无效改密绕过强制改密）。
	if body.OldPassword == body.NewPassword {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new password must differ from old password"})
		return
	}
	// 新密码强度校验。
	if msg := validateStrongPassword(body.NewPassword); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	// bcrypt 哈希新密码。
	newHash, err := hashPassword(body.NewPassword)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "password hash failed: " + err.Error()})
		return
	}
	// 落库：写入新哈希并清除 must_change_password 标记。
	if !s.store.ChangePassword(u.ID, newHash) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: u.ID, Action: "user_change_password", Target: u.ID, Detail: "username=" + u.Username,
	})
	writeJSON(w, http.StatusOK, map[string]string{"message": "password changed"})
}

// userFromToken 从请求的 Authorization: Bearer <token> 提取并验签 JWT，返回对应用户。
// 用于 /api/v1/auth/me 与用户管理 API 的鉴权。
// token 缺失/无效/用户不存在 → 返回 error。
func (s *Server) userFromToken(r *http.Request) (*store.User, error) {
	tokenStr, err := extractBearer(r)
	if err != nil {
		// task 94：Bearer 头缺失时回退 HttpOnly Cookie（前端不再持久化 token 到
		// localStorage，刷新后靠 Cookie 保持会话；两路均走同一 ParseHSJWT 校验）。
		if ck, ckErr := r.Cookie(accessTokenCookieName); ckErr == nil && strings.TrimSpace(ck.Value) != "" {
			tokenStr = ck.Value
		} else {
			return nil, err
		}
	}
	claims, err := authctx.ParseHSJWT(tokenStr, s.jwtSecret)
	if err != nil {
		return nil, err
	}
	u := s.store.GetUser(claims.UserID)
	if u == nil {
		return nil, errors.New("user not found")
	}
	// P1 吊销：非 active 用户（disabled/rejected/pending/空）既有的有效签名 token 立即失效，
	// 使管理员禁用/删除账号后无需等待 24h 过期即收回访问。
	if u.Status != "active" {
		return nil, errors.New("user account is not active")
	}
	return u, nil
}

// extractBearer 从 Authorization 头提取 Bearer token。
func extractBearer(r *http.Request) (string, error) {
	v := r.Header.Get("Authorization")
	if v == "" {
		return "", errors.New("missing Authorization header")
	}
	const prefix = "Bearer "
	if len(v) < len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return "", errors.New("Authorization header is not Bearer format")
	}
	token := strings.TrimSpace(v[len(prefix):])
	if token == "" {
		return "", errors.New("empty bearer token")
	}
	return token, nil
}

// requirePermission 鉴权中间件：从 token 提取用户，校验是否拥有指定权限。
// 返回 (user, ok)；ok=false 时已写入 401/403 响应，调用方应直接 return。
// 权限校验逻辑：展开用户经角色获得的权限，检查是否含 required 权限。
func (s *Server) requirePermission(w http.ResponseWriter, r *http.Request, required string) (*store.User, bool) {
	u, err := s.userFromToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return nil, false
	}
	// P1 强制改密：标记 MustChangePassword 的用户只能访问 /api/v1/auth/change-password
	// （该端点走 userFromToken，不经此处），其余受保护 API 一律拒绝，避免弱口令长期在线。
	if u.MustChangePassword {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "password change required (MUST_CHANGE_PASSWORD)"})
		return nil, false
	}
	perms := s.userPermissions(u)
	for _, p := range perms {
		if p == required {
			return u, true
		}
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "permission denied: " + required})
	return nil, false
}

// ============================================================================
// 统一产品级 RBAC 闸（B3：权限控制无疏漏）—— 兼容两种身份来源。
// ============================================================================

// rolePermCache 缓存角色名→权限集合映射（取自 store.RolePermissions()，包初始化时计算一次）。
var rolePermCache = store.RolePermissions()

// requireProd 统一产品级 RBAC 鉴权闸：在 requireTenantContext（租户隔离）之后调用，
// 校验当前身份是否拥有 required 权限。兼容两种身份来源：
//   - 联邦入站（X-Federation-Forwarded=1）：调用方已用 verifyFederationRequest 验签 HMAC，
//     信任来自可信控制面 peer 的请求（用户级 RBAC 已在来源控制面执行）；直接放行。
//   - Authorization: Bearer（或 opsmesh_at Cookie）：走 JWT 路径（requirePermission）。
//   - 网关注入 X-User-Roles（角色名）：展开为权限集合后校验（authorizeByRoles）。
//   - demo 模式且无任何身份头：放行，保持本地一键体验的宽松语义（与 requireTenantContext 一致）。
//   - 其余：401。
// 返回 (user, ok)；ok=false 时已写入响应，调用方应直接 return。
func (s *Server) requireProd(w http.ResponseWriter, r *http.Request, required string) (*store.User, bool) {
	// 1. 联邦入站：verifyFederationRequest 已验签 HMAC，信任 peer（用户 RBAC 已在来源侧执行）。
	if r.Header.Get("X-Federation-Forwarded") == "1" {
		return nil, true
	}
	// 2. JWT Bearer / Cookie 路径：用户中心登录后携带的 token。
	auth := r.Header.Get("Authorization")
	hasBearer := strings.HasPrefix(auth, "Bearer ")
	hasCookie := false
	if ck, err := r.Cookie(accessTokenCookieName); err == nil && strings.TrimSpace(ck.Value) != "" {
		hasCookie = true
	}
	if hasBearer || hasCookie {
		return s.requirePermission(w, r, required)
	}
	// 3. 网关注入路径：X-User-Roles 携带角色名。
	if strings.TrimSpace(r.Header.Get("X-User-Roles")) != "" {
		return s.authorizeByRoles(w, r, required)
	}
	// 4. demo 模式宽松放行（无身份，自动填充 default/demo）。
	if s.cfg != nil && s.cfg.Demo {
		return nil, true
	}
	// 5. 无可用身份 → 拒绝。
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity (no bearer token or gateway role header)"})
	return nil, false
}

// authorizeByRoles 将网关注入/联邦转发的角色名展开为权限集合并校验 required。
func (s *Server) authorizeByRoles(w http.ResponseWriter, r *http.Request, required string) (*store.User, bool) {
	roleNames := authctx.FromHTTPHeader(r.Header).Roles
	if len(roleNames) == 0 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "no roles in identity context"})
		return nil, false
	}
	for _, rn := range roleNames {
		for _, p := range rolePermCache[rn] {
			if p == required {
				return nil, true
			}
		}
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "permission denied: " + required})
	return nil, false
}

// ============================================================================
// 用户管理 handler：/api/v1/users
// ============================================================================

// handleUsers 统一处理 /api/v1/users：
//   - GET：列出全部用户（需 user:read 权限）
//   - POST：创建用户（需 user:write 权限）
func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListUsers(w, r)
	case http.MethodPost:
		s.handleCreateUser(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListUsers 处理 GET /api/v1/users：列出全部用户（需 user:read 权限）。
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "user:read"); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"users": s.store.ListUsers()})
}

// handleCreateUser 处理 POST /api/v1/users：创建用户（需 user:write 权限）。
// 请求体：{username, password, email?, role_ids?}；密码最短 6 字符，bcrypt 哈希后存库。
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "user:write")
	if !ok {
		return
	}
	var body struct {
		Username string   `json:"username"`
		Password string   `json:"password"`
		Email    string   `json:"email"`
		RoleIDs  []string `json:"role_ids"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Username == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	if msg := validateStrongPassword(body.Password); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	// P3 角色引用校验：role_ids 若存在须全部指向真实角色，避免写入无效角色引用。
	for _, rid := range body.RoleIDs {
		if rid != "" && s.store.GetRole(rid) == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown role id: " + rid})
			return
		}
	}
	if s.store.GetUserByUsername(body.Username) != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "password hash failed: " + err.Error()})
		return
	}
	u := &store.User{
		ID:           randHexID("user"),
		Username:     body.Username,
		Email:        body.Email,
		PasswordHash: hash,
		Status:       "active",
		RoleIDs:      body.RoleIDs,
	}
	if s.store.CreateUser(u) == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "user_create", Target: u.ID, Detail: "username=" + u.Username,
	})
	writeJSON(w, http.StatusCreated, u)
}

// handleUserRouting 分派 /api/v1/users/{id} 子路径：
//   - PUT /api/v1/users/{id}：更新用户（需 user:write 权限）
//   - DELETE /api/v1/users/{id}：删除用户（需 user:delete 权限）
//   - POST /api/v1/users/{id}/approve：审批用户（需 user:approve 权限，P1-7 注册安全）
//   - POST /api/v1/users/{id}/reject：拒绝用户（需 user:approve 权限，P1-7 注册安全）
func (s *Server) handleUserRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/users/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user id required"})
		return
	}
	// 解析 {id} 或 {id}/approve 或 {id}/reject。
	id := rest
	subAction := ""
	if idx := strings.Index(rest, "/"); idx > 0 {
		id = rest[:idx]
		subAction = rest[idx+1:]
	}
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user id required"})
		return
	}
	// 子路径分发（approve/reject）。
	if subAction != "" {
		switch subAction {
		case "approve":
			s.handleApproveUser(w, r, id)
			return
		case "reject":
			s.handleRejectUser(w, r, id)
			return
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + subAction})
			return
		}
	}
	// 主路径分发（PUT/DELETE）。
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateUser(w, r, id)
	case http.MethodDelete:
		s.handleDeleteUser(w, r, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleApproveUser 处理 POST /api/v1/users/{id}/approve：管理员审批用户注册（P1-7 注册安全）。
// 将用户 Status 从 "pending" 改为 "active"；仅 pending 状态可审批，其他状态返回 409。
// 鉴权：需 user:approve 权限（admin 角色自动拥有）。
func (s *Server) handleApproveUser(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "user:approve")
	if !ok {
		return
	}
	existing := s.store.GetUser(id)
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if existing.Status != "pending" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "user is not pending (current status: " + existing.Status + ")"})
		return
	}
	existing.Status = "active"
	if !s.store.UpdateUser(existing) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "user_approve", Target: id, Detail: "approved user " + existing.Username,
	})
	writeJSON(w, http.StatusOK, s.store.GetUser(id))
}

// handleRejectUser 处理 POST /api/v1/users/{id}/reject：管理员拒绝用户注册（P1-7 注册安全）。
// 将用户 Status 改为 "rejected"；仅 pending 状态可拒绝，其他状态返回 409。
// 鉴权：需 user:approve 权限（admin 角色自动拥有）。
// 请求体可选：{reason?: "拒绝原因"}，记录到审计日志。
func (s *Server) handleRejectUser(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "user:approve")
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	// 请求体可选；解析失败时记录日志（兼容空 body 调用）。
	if err := decodeJSONBody(w, r, &body); err != nil && err != io.EOF {
		log.Printf("controlplane: handleApproveUser 解析请求体失败: %v", err)
	}
	existing := s.store.GetUser(id)
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if existing.Status != "pending" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "user is not pending (current status: " + existing.Status + ")"})
		return
	}
	existing.Status = "rejected"
	if !s.store.UpdateUser(existing) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	detail := "rejected user " + existing.Username
	if body.Reason != "" {
		detail += " reason: " + body.Reason
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "user_reject", Target: id, Detail: detail,
	})
	writeJSON(w, http.StatusOK, s.store.GetUser(id))
}

// handleUpdateUser 处理 PUT /api/v1/users/{id}：更新用户 email/roles/status（需 user:write 权限）。
// 请求体：{email?, role_ids?, status?}；仅更新非空字段。
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "user:write")
	if !ok {
		return
	}
	var body struct {
		Email   string   `json:"email"`
		RoleIDs []string `json:"role_ids"`
		Status  string   `json:"status"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	// P2 状态变更需更高权限：仅 user:write 不能激活/禁用账号，须 user:approve（与 P1-7 审批模型一致），
	// 防止低权限用户自行把 Status 置 active/rejected 绕过审批流。
	if body.Status != "" {
		if _, ok := s.requirePermission(w, r, "user:approve"); !ok {
			return
		}
	}
	existing := s.store.GetUser(id)
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if body.Email != "" {
		existing.Email = body.Email
	}
	if body.RoleIDs != nil {
		existing.RoleIDs = body.RoleIDs
	}
	if body.Status != "" {
		existing.Status = body.Status
	}
	if !s.store.UpdateUser(existing) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "user_update", Target: id, Detail: "updated via HTTP",
	})
	writeJSON(w, http.StatusOK, s.store.GetUser(id))
}

// handleDeleteUser 处理 DELETE /api/v1/users/{id}：删除用户（需 user:delete 权限）。
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "user:delete")
	if !ok {
		return
	}
	if s.store.GetUser(id) == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if !s.store.DeleteUser(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "user_delete", Target: id, Detail: "deleted via HTTP",
	})
	w.WriteHeader(http.StatusNoContent)
}

// ============================================================================
// 角色管理 handler：/api/v1/roles
// ============================================================================

// handleRoles 统一处理 /api/v1/roles：
//   - GET：列出全部角色（需 role:read 权限，但为简化前端展示，登录用户均可查看）
//   - POST：创建角色（需 role:write 权限）
func (s *Server) handleRoles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListRoles(w, r)
	case http.MethodPost:
		s.handleCreateRole(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListRoles 处理 GET /api/v1/roles：列出全部角色。
// 鉴权：仅需有效 token（登录用户均可查看角色列表，便于前端角色选择下拉框）。
func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	if _, err := s.userFromToken(r); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"roles": s.store.ListRoles()})
}

// handleCreateRole 处理 POST /api/v1/roles：创建角色（需 role:write 权限）。
// 请求体：{name, description, permissions[]}。
func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "role:write")
	if !ok {
		return
	}
	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role name is required"})
		return
	}
	role := &store.Role{
		ID:          randHexID("role"),
		Name:        body.Name,
		Description: body.Description,
		Permissions: body.Permissions,
	}
	if s.store.CreateRole(role) == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "role name already exists"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "role_create", Target: role.ID, Detail: "name=" + role.Name,
	})
	writeJSON(w, http.StatusCreated, role)
}

// handleRoleRouting 分派 /api/v1/roles/{id} 子路径：
//   - PUT /api/v1/roles/{id}：更新角色（需 role:write 权限）
//   - DELETE /api/v1/roles/{id}：删除角色（需 role:delete 权限）
func (s *Server) handleRoleRouting(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/roles/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role id required"})
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateRole(w, r, id)
	case http.MethodDelete:
		s.handleDeleteRole(w, r, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleUpdateRole 处理 PUT /api/v1/roles/{id}：更新角色 description/permissions（需 role:write 权限）。
func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "role:write")
	if !ok {
		return
	}
	var body struct {
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	existing := s.store.GetRole(id)
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "role not found"})
		return
	}
	if body.Description != "" {
		existing.Description = body.Description
	}
	if body.Permissions != nil {
		existing.Permissions = body.Permissions
	}
	if !s.store.UpdateRole(existing) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "role not found"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "role_update", Target: id, Detail: "updated via HTTP",
	})
	writeJSON(w, http.StatusOK, s.store.GetRole(id))
}

// handleDeleteRole 处理 DELETE /api/v1/roles/{id}：删除角色（需 role:delete 权限）。
func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "role:delete")
	if !ok {
		return
	}
	if s.store.GetRole(id) == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "role not found"})
		return
	}
	if !s.store.DeleteRole(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "role not found"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "role_delete", Target: id, Detail: "deleted via HTTP",
	})
	w.WriteHeader(http.StatusNoContent)
}

// ============================================================================
// 权限查询 handler：/api/v1/permissions
// ============================================================================

// handlePermissions 处理 GET /api/v1/permissions：返回全部预定义权限。
// 鉴权：仅需有效 token（登录用户均可查看权限列表，便于前端权限选择）。
func (s *Server) handlePermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, err := s.userFromToken(r); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"permissions": s.store.ListPermissions()})
}
