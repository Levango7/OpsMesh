// auth.go 实现用户中心的核心鉴权 helper 与中间件。
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
//
// 文件拆分（按 handler 域）：
//   - auth.go：核心 helper（Cookie/刷新令牌/改密令牌/密码哈希/loginGuard）+ 鉴权中间件（userFromToken/requirePermission/requireProd）；
//   - auth_login.go：注册/登录/登出/刷新/当前用户/改密 handler；
//   - auth_users.go：用户管理 handler（CRUD + 审批/拒绝）；
//   - auth_roles.go：角色管理 handler（CRUD）；
//   - auth_perms.go：权限查询 handler。
package controlplane

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/authctx"
	"opsmesh/internal/store"

	"golang.org/x/crypto/bcrypt"
)

// 双 HttpOnly Cookie 令牌方案（同源最简单且安全）：
//   - at（access token）：短期 JWT（15min），仅标识身份，XSS 窃取后利用窗口极小；
//   - rt（refresh token）：长期不透明随机串（7d），服务端可吊销/旋转，用于静默续期。
//
// 两者均为 HttpOnly + SameSite=Lax（防 XSS 读取 / 防 CSRF 跨站携带），同源由浏览器自动携带。
const (
	accessTokenCookieName  = "opsmesh_at"
	refreshTokenCookieName = "opsmesh_rt"
	accessTokenExpiry      = 15 * time.Minute
	refreshTokenExpiry     = 7 * 24 * time.Hour
)

// setCookie 统一的 HttpOnly Cookie 写入（Path=/、SameSite=Lax；HTTPS 部署才置 Secure）。
// task 112 Cookie Secure：优先用 cfg.CookieSecure（HTTPS 反代终止 TLS 时显式开启），
// 回退到 TLSCert 非空（控制面直连 HTTPS 时自动启用），二者任一为 true 即置 Secure。
func (s *Server) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure(),
	})
}

// cookieSecure 决定 Cookie 的 Secure 属性（task 112）。
// 优先级：cfg.CookieSecure 显式 true → true；否则回退到 TLSCert 非空（HTTPS 直连自动启用）。
// 这样既支持 HTTPS 反代终止 TLS（控制面收 HTTP 但对外 HTTPS，须显式 --cookie-secure=true），
// 又保持原有 TLSCert 兜底语义（控制面自身持证直连 HTTPS 时自动 Secure）。
func (s *Server) cookieSecure() bool {
	if s.cfg == nil {
		return false
	}
	return s.cfg.CookieSecure || s.cfg.TLSCert != ""
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
// 刷新令牌存储（task 112：从进程内全局 map 改为 store 持久化）。
//
// 原实现（MVP）使用进程内 map 存 refresh token，多副本 HA 部署下登录态随机失效
// （登录请求落到副本 A，刷新请求落到副本 B 时 rt 不存在 → 401）。现改为经
// store.RefreshTokenStore 接口持久化（MemoryStore / SQLStore 均已实现，task 111），
// 多副本共享同一 MySQL 时跨副本续期一致。
//
// 安全设计（与 install_tokens 同范式，P1-F7 明文不落库）：
//   - 库存/内存只存 token 的 SHA-256 摘要（TokenHash），不存明文；
//   - DeviceFP（设备指纹）绑定签发设备，防 token 跨设备重放；
//   - 旋转：consume 校验通过即 DeleteRefreshToken，旧 rt 立即作废防重放。
// ============================================================================

// hashRefreshToken 计算 refresh token 明文的 SHA-256 摘要（hex 编码）。
// 库存/内存只存摘要，不存明文——DB 只读账号/备份泄露不等于活体 refresh token 泄露（P1-F7）。
func hashRefreshToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// deviceFingerprint 从请求头 X-Device-FP 提取设备指纹（task 112 设备绑定）。
// 前端可传 User-Agent 摘要或随机 UUID（同设备稳定即可）。空串表示不校验设备
// （向后兼容：旧客户端不传头时 DeviceFP 为空，签发时存空，验证时不校验）。
func deviceFingerprint(r *http.Request) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Header.Get("X-Device-FP"))
}

// createRefreshToken 生成并持久化一个刷新令牌（crypto/rand，32 字节十六进制）。
// 返回明文 token（仅下发给客户端 Cookie），库内只存其 SHA-256 摘要。
// deviceFP 绑定签发设备（空串=不校验设备，向后兼容旧客户端）。
func (s *Server) createRefreshToken(userID, deviceFP string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	// 持久化：存摘要 + UserID + DeviceFP + 过期时间，不存明文。
	if err := s.store.SaveRefreshToken(&store.RefreshToken{
		TokenHash: hashRefreshToken(id),
		UserID:    userID,
		TenantID:  "default", // 用户中心为平台级，统一 default 租户
		DeviceFP:  deviceFP,
		ExpiresAt: time.Now().Add(refreshTokenExpiry),
	}); err != nil {
		return "", err
	}
	return id, nil
}

// consumeRefreshToken 校验并消费刷新令牌（一次性：校验通过即删除，实现旋转）。
// deviceFP 为请求携带的设备指纹，与存储的 DeviceFP 比对防跨设备重放。
// 无效/过期/已消费/设备指纹不匹配返回 (nil, false)。
func (s *Server) consumeRefreshToken(id, deviceFP string) (*store.RefreshToken, bool) {
	hash := hashRefreshToken(id)
	rt := s.store.GetRefreshToken(hash)
	if rt == nil {
		return nil, false
	}
	// 旋转：旧 rt 立即作废，防重放（无论后续校验是否通过均删除）。
	s.store.DeleteRefreshToken(hash)
	if time.Now().After(rt.ExpiresAt) {
		return nil, false
	}
	// 设备绑定校验（task 112）：存储的 DeviceFP 非空且请求携带了 DeviceFP 时，两者必须匹配。
	// 存储 DeviceFP 为空（旧客户端签发时未绑定）或请求未携带 DeviceFP 时不校验（向后兼容）。
	if rt.DeviceFP != "" && deviceFP != "" && rt.DeviceFP != deviceFP {
		return nil, false
	}
	return rt, true
}

// revokeRefreshToken 吊销指定刷新令牌（登出时调用）。
func (s *Server) revokeRefreshToken(id string) {
	s.store.DeleteRefreshToken(hashRefreshToken(id))
}

// purgeExpiredRefreshTokens 清理过期刷新令牌（task 112：store 持久化后改为 no-op）。
//
// 原进程内 map 实现需周期扫描清理防内存无限增长；改用 store 持久化后：
//   - MemoryStore：consumeRefreshToken 校验过期时已 DeleteRefreshToken 顺带清理；
//   - SQLStore：可由 DB 定时任务或后续扩展 store 层批量清理接口处理；
//   - 本函数保留 no-op 签名以兼容 server.go startRefreshSweep 调用，避免破坏现有启动流程。
func (s *Server) purgeExpiredRefreshTokens() {
	// no-op：store 持久化后过期清理由 consumeRefreshToken 顺带完成（校验过期即删除）。
}

// ============================================================================
// 改密令牌存储（安全债 85 + 任务 96）：mustChangePassword=true 用户登录时不签发
// access token，仅签发一次性短时效 changePasswordToken（5min），仅可用于
// /api/v1/auth/change-password。改密成功后才签发正式 at+rt。
// ============================================================================

const changePasswordTokenExpiry = 5 * time.Minute

// changePasswordSession 改密令牌会话记录。
type changePasswordSession struct {
	UserID    string
	ExpiresAt time.Time
}

var changePasswordTokens = struct {
	sync.Mutex
	m map[string]*changePasswordSession
}{m: make(map[string]*changePasswordSession)}

// createChangePasswordToken 生成并存储一个一次性改密令牌（crypto/rand，32 字节十六进制）。
// 有效期 5 分钟，仅用于 /api/v1/auth/change-password。
func createChangePasswordToken(userID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	changePasswordTokens.Lock()
	changePasswordTokens.m[id] = &changePasswordSession{UserID: userID, ExpiresAt: time.Now().Add(changePasswordTokenExpiry)}
	changePasswordTokens.Unlock()
	return id, nil
}

// consumeChangePasswordToken 校验并消费改密令牌（一次性：校验通过即删除，防重放）。
// 无效/过期/已消费返回 (nil, false)。
func consumeChangePasswordToken(id string) (*changePasswordSession, bool) {
	changePasswordTokens.Lock()
	defer changePasswordTokens.Unlock()
	cs, ok := changePasswordTokens.m[id]
	if !ok {
		return nil, false
	}
	delete(changePasswordTokens.m, id) // 一次性：消费即删除，防重放
	if time.Now().After(cs.ExpiresAt) {
		return nil, false
	}
	return cs, true
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

// hashPassword 用 bcrypt 哈希密码。
// 使用 cost=12（生产推荐基线，DefaultCost=10 偏低）。
// 注意：现有用户密码哈希可能用 cost=10 生成，bcrypt.CompareHashAndPassword
// 会自动适配不同 cost，因此无需迁移旧哈希；新哈希与改密后哈希均使用 cost=12。
const bcryptCost = 12

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
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
// ChangePasswordToken（任务 96）：MustChangePassword=true 时签发的一次性短时效 token（5min），
// 仅用于 /api/v1/auth/change-password；此时不下发 access token（Token 字段为空、不下发 at Cookie），
// 改密成功后才签发正式 at + rt。
type authResponse struct {
	Token               string      `json:"token"`
	User                *store.User `json:"user"`
	MustChangePassword  bool        `json:"mustChangePassword"`
	ChangePasswordToken string      `json:"changePasswordToken,omitempty"`
}

// ============================================================================
// 强口令校验（安全债 85）：跨域 helper，供 auth_login.go 与 auth_users.go 共用。
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

// ============================================================================
// 鉴权中间件：userFromToken / extractBearer / requirePermission
// ============================================================================

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
//
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
