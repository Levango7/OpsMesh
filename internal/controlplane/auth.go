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
	"fmt"
	"log"
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
//
// P1-G4 原子消费：原实现为 Get→Delete→校验三步，多副本并发下同一 rt 可被双消费
// （副本 A Get 后、Delete 前，副本 B 也 Get 到同一 rt）。现改为调用
// store.ConsumeRefreshToken 原子读取+删除，保证同一 rt 仅被消费一次。
//
// C-4 DeviceFP deadline：超过 cfg.DeviceFPDeadline 之后签发的 refresh token 必须绑定
// DeviceFP（非空）。deadline 前保持向后兼容（DeviceFP 为空时跳过设备绑定校验）；
// deadline 后 DeviceFP 为空则拒绝（强制设备绑定，防 token 跨设备重放）。
// deadline 零值=不强制（完全向后兼容）。
func (s *Server) consumeRefreshToken(id, deviceFP string) (*store.RefreshToken, bool) {
	hash := hashRefreshToken(id)
	// 原子消费：读取+删除在单次互斥操作内完成，防并发双消费（P1-G4）。
	rt, ok := s.store.ConsumeRefreshToken(hash)
	if !ok || rt == nil {
		return nil, false
	}
	if time.Now().After(rt.ExpiresAt) {
		return nil, false
	}
	// C-4 DeviceFP deadline 强制非空：超过 deadline 且存储的 DeviceFP 为空时拒绝。
	// 用于渐进式强制设备绑定：deadline 前允许旧客户端不传 DeviceFP（向后兼容），
	// deadline 后强制要求新签发的 refresh token 必须绑定 DeviceFP。
	// deadline 零值=不强制（完全向后兼容，原有行为）。
	if !s.deviceFPDeadline.IsZero() && rt.CreatedAt.After(s.deviceFPDeadline) && rt.DeviceFP == "" {
		return nil, false
	}
	// 设备绑定校验（task 112）：存储的 DeviceFP 非空且请求携带了 DeviceFP 时，两者必须匹配。
	// 存储 DeviceFP 为空（旧客户端签发时未绑定）或请求未携带 DeviceFP 时不校验（向后兼容）。
	// 注：DeviceFP 为空时跳过校验，兼容旧客户端签发的 token（未绑定设备指纹）。
	// 新签发的 refresh token 均绑定 DeviceFP，旧 token 旋转后自动获得绑定。
	// C-4：deadline 后签发的 token 已在上方强制 DeviceFP 非空，此处校验必然执行。
	if rt.DeviceFP != "" && deviceFP != "" && rt.DeviceFP != deviceFP {
		return nil, false
	}
	return rt, true
}

// revokeRefreshToken 吊销指定刷新令牌（登出时调用）。
func (s *Server) revokeRefreshToken(id string) {
	s.store.DeleteRefreshToken(hashRefreshToken(id))
}

// revokeAccessTokenFromRequest 从请求中提取 access token，解析 jti 并加入吊销黑名单（P1-G4）。
// 登出时调用，使 access token 在过期前立即失效（而非等 15min 自然过期）。
// token 缺失/无效时静默跳过（不阻断登出流程）。
// B-6：黑名单经 SessionStore 共享，多副本下登出全局生效（Redis 后端时）。
func (s *Server) revokeAccessTokenFromRequest(r *http.Request) {
	if s.sessionStore == nil {
		return
	}
	// 提取 token：优先 Authorization: Bearer，回退 HttpOnly Cookie（与 userFromToken 一致）。
	tokenStr, err := extractBearer(r)
	if err != nil {
		if ck, ckErr := r.Cookie(accessTokenCookieName); ckErr == nil && strings.TrimSpace(ck.Value) != "" {
			tokenStr = ck.Value
		} else {
			return
		}
	}
	claims, err := authctx.ParseHSJWT(tokenStr, s.jwtSecret)
	if err != nil {
		return // token 无效/过期，无需吊销（已自然失效）
	}
	// 计算剩余 TTL：token 过期时间 - 当前时间。已过期的 token 无需吊销。
	ttl := time.Until(claims.ExpiresAt)
	if ttl <= 0 {
		return
	}
	s.sessionStore.Blacklist(claims.JTI, ttl)
}

// purgeExpiredRefreshTokens 清理过期刷新令牌（task 112：store 持久化后改为 no-op）。
//
// 原进程内 map 实现需周期扫描清理防内存无限增长；改用 store 持久化后：
//   - MemoryStore：consumeRefreshToken 校验过期时已 DeleteRefreshToken 顺带清理；
//   - SQLStore：可由 DB 定时任务或后续扩展 store 层批量清理接口处理；
//   - 本函数保留 no-op 签名以兼容 server.go startRefreshSweep 调用，避免破坏现有启动流程。
//
// P1-G4：顺带清理 JWT 吊销黑名单与改密令牌的过期条目（token 自然过期后条目无意义）。
// B-6：经 SessionStore 接口清理，InProcess 主动清理 map，Redis 靠 TTL 自动过期（no-op）。
func (s *Server) purgeExpiredRefreshTokens() {
	// no-op：store 持久化后过期清理由 consumeRefreshToken 顺带完成（校验过期即删除）。
	// P1-G4/B-6：清理 token blacklist 与改密令牌过期条目，防无界增长。
	if s.sessionStore != nil {
		s.sessionStore.PurgeBlacklist()
		s.sessionStore.PurgeChangePasswordTokens()
	}
}

// ============================================================================
// 改密令牌存储（安全债 85 + 任务 96）：mustChangePassword=true 用户登录时不签发
// access token，仅签发一次性短时效 changePasswordToken（5min），仅可用于
// /api/v1/auth/change-password。改密成功后才签发正式 at+rt。
//
// B-6 多副本共享：原进程内 map 改为经 SessionStore 接口存储，多副本下任一副本签发
// 的改密令牌在其他副本也可消费（Redis 后端时）。
// ============================================================================

const changePasswordTokenExpiry = 5 * time.Minute

// createChangePasswordToken 生成并存储一个一次性改密令牌（crypto/rand，32 字节十六进制）。
// 有效期 5 分钟，仅用于 /api/v1/auth/change-password。
// B-6：经 SessionStore 持久化，多副本共享。
func (s *Server) createChangePasswordToken(userID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	if err := s.sessionStore.CreateChangePasswordToken(id, userID, changePasswordTokenExpiry); err != nil {
		return "", err
	}
	return id, nil
}

// consumeChangePasswordToken 校验并消费改密令牌（一次性：校验通过即删除，防重放）。
// 返回关联的 userID；无效/过期/已消费返回 ("", false)。
// B-6：经 SessionStore 原子消费，多副本下同一令牌仅被消费一次。
func (s *Server) consumeChangePasswordToken(id string) (string, bool) {
	return s.sessionStore.ConsumeChangePasswordToken(id)
}

// randHexID 生成随机十六进制 ID（16 字节，crypto/rand 密码学安全）。
// 用于用户/角色 ID 分配（调用方未填 ID 时）。
//
// 安全要求：熵源失败时 panic 而非回退到可预测值。回退到 "prefix+timestamp+fallback"
// 会使 ID 可预测，攻击者可枚举/伪造 ID 绕过唯一性假设；密码学安全场景下熵源不可用
// 属于不可恢复的运行时故障，应快速失败暴露问题而非静默降级。
func randHexID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
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

// rotateDefaultAdminPassword 在非 demo 模式下，若默认 admin 仍使用弱口令 "admin123"，
// 则生成随机密码替换并打印到日志（一次性提示管理员复制）。返回是否执行了替换。
//
// P1-G4 安全加固：默认 admin 弱口令 "admin123" 靠 mustChangePassword 兜底，但若管理员
// 忽略改密提示，弱口令将持续可登。改为首次启动时生成随机口令（16 字节 hex），即使管理员
// 不改密，攻击者也无法用已知弱口令登录。随机密码仅打印一次到日志，须妥善保管。
//
// 幂等性：仅当 admin 当前密码仍是 "admin123"（bcrypt 比对命中）时才重置，避免覆盖管理员
// 已修改的密码。MemoryStore 每次启动都是新实例（admin 始终是 admin123），每次都重置；
// SQLStore 持久化，重启后 admin 已是随机口令，bcrypt 比对不命中，不重复重置。
//
// 保留 MustChangePassword=true：ChangePassword 会清除该标记，随后用 UpdateUser 恢复，
// 确保首登仍强制改密（与安全债 85 一致）。
func rotateDefaultAdminPassword(st store.Store) bool {
	u := st.GetUserByUsername("admin")
	if u == nil {
		return false
	}
	// 仅当仍是默认弱口令 admin123 时才重置（避免覆盖管理员已改的密码）。
	if !verifyPassword(u.PasswordHash, "admin123") {
		return false
	}
	// 生成随机密码：16 字节 hex（32 字符，crypto/rand 密码学安全）。
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Printf("[controlplane] 生成随机 admin 密码失败: %v", err)
		return false
	}
	password := hex.EncodeToString(b)
	hash, err := hashPassword(password)
	if err != nil {
		log.Printf("[controlplane] 哈希随机 admin 密码失败: %v", err)
		return false
	}
	// ChangePassword 写入新哈希但会清除 MustChangePassword 标记。
	if !st.ChangePassword(u.ID, hash) {
		log.Printf("[controlplane] 更新 admin 随机密码失败")
		return false
	}
	// 恢复 MustChangePassword=true（首登仍强制改密），同时保留原有 email/roles/status。
	cp := *u
	cp.MustChangePassword = true
	st.UpdateUser(&cp)
	// 一次性打印随机密码到日志，提示管理员复制（后续重启不重复打印，因密码已非 admin123）。
	log.Printf("[controlplane] ============================================================")
	log.Printf("[controlplane] 安全提示：默认 admin 密码已替换为随机口令（首登仍须改密）。")
	log.Printf("[controlplane]   一次性随机密码（请立即复制并登录后修改）: %s", password)
	log.Printf("[controlplane] ============================================================")
	return true
}

// ============================================================================
// loginGuard 登录/注册防爆破 + 限流（P1-4）。
//   - 限流：按客户端 IP 令牌桶，约束单位时间登录/注册尝试次数，防撞库与 DoS。
//   - 防爆破：按用户名累计失败次数，超阈值临时锁定账号，挫败密码爆破。
//
// B-6 多副本共享：
//   - IP 令牌桶限流保留进程内（多副本各自限流，副本数 N 时实际阈值 N*burst，可接受；
//     令牌桶算法依赖 tokens/last 时序状态，难以用 Redis 原子操作精确实现）。
//   - 失败计数 + 账号锁定经 SessionStore 共享（多副本下任一副本触发锁定后其他副本也拒绝；
//     撞库攻击者无法在不同副本上各消耗 loginMaxFails 次配额）。
// ============================================================================

const (
	loginRateBurst  = 5                // 令牌桶容量（瞬时允许的最大尝试数）；收紧自 10，强化撞库防护
	loginRateRefill = 1.0 / 6.0        // 令牌补充速率（每秒），约每 6s 1 个，≈10/min；收紧自 1/3（≈20/min）
	loginMaxFails   = 5                // 单账号允许的连续失败次数
	loginFailWindow = 15 * time.Minute // 失败计数滑动窗口
	loginLockDur    = 15 * time.Minute // 账号锁定时长
)

type loginGuard struct {
	mu    sync.Mutex
	ips   map[string]*rateRec // 客户端 IP -> 限流令牌桶（进程内，多副本各自限流）
	done  chan struct{}       // stopSweep 关闭此 chan 通知 sweep goroutine 退出
	store store.SessionStore  // B-6：失败计数 + 账号锁定经 SessionStore 共享
}

type rateRec struct {
	tokens float64
	last   time.Time
}

func newLoginGuard(ss store.SessionStore) *loginGuard {
	if ss == nil {
		// 兜底：测试或未初始化场景用进程内 store，避免 nil panic。
		ss = store.NewInProcessSessionStore()
	}
	return &loginGuard{
		ips:   make(map[string]*rateRec),
		done:  make(chan struct{}),
		store: ss,
	}
}

// allow 按 IP 令牌桶判断本次尝试是否被限流（true=放行）。
// IP 令牌桶保留进程内（多副本各自限流，可接受）。
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

// failKey 拼接失败计数的 SessionStore key。
func loginFailKey(username string) string {
	return "fail:user:" + username
}

// lockKey 拼接账号锁定的 SessionStore key。
func loginLockKey(username string) string {
	return "lock:user:" + username
}

// recordFail 记录一次账号失败尝试；返回是否触发锁定。
// B-6：失败计数经 SessionStore 共享，多副本下累计失败次数全局一致。
func (g *loginGuard) recordFail(username string) bool {
	count := g.store.IncrRateLimit(loginFailKey(username), loginFailWindow)
	if count >= loginMaxFails {
		// 触发锁定：将锁定标记加入 SessionStore 黑名单，ttl=锁定时长。
		// 多副本下任一副本触发锁定后，其他副本的 locked 检查也会命中。
		g.store.Blacklist(loginLockKey(username), loginLockDur)
		return true
	}
	return false
}

// locked 判断账号当前是否处于锁定状态。
// B-6：锁定状态经 SessionStore 共享，多副本下任一副本触发锁定后其他副本也拒绝。
func (g *loginGuard) locked(username string) bool {
	return g.store.IsBlacklisted(loginLockKey(username))
}

// resetFail 登录成功后清除该账号失败计数（解锁）。
// B-6：经 SessionStore 共享，多副本下任一副本登录成功后其他副本也清除计数。
func (g *loginGuard) resetFail(username string) {
	g.store.ResetRateLimit(loginFailKey(username))
}

// startSweep 启动后台回收 goroutine，定期清理过期限流令牌桶与已解锁且超窗的失败计数，
// 防止 ips/fails map 在长运行中无界增长（原实现只增不删，进程内内存泄漏）。
// 仅在 NewServer（生产路径）调用；测试直接构造 Server 不触发，避免测试悬挂 goroutine。
//
// 退出机制：goroutine 通过 select 监听 g.done 与 ticker.C，stopSweep 关闭 g.done 即可让其
// 优雅退出。当前 Server 未暴露 Close/Shutdown 方法，调用方（NewServer 的拥有者）在销毁
// Server 前应显式调用 s.loginGuard.stopSweep() 以避免 goroutine 泄漏；后续若新增 Server.Close
// 应在其中调用 stopSweep。
func (g *loginGuard) startSweep(interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-g.done:
				return
			case <-ticker.C:
				g.sweep()
			}
		}
	}()
}

// stopSweep 通知 startSweep 启动的后台 goroutine 退出（关闭 done chan）。
// 幂等：用 recover 容忍多次调用（重复 close 已关闭 chan 会 panic）。
func (g *loginGuard) stopSweep() {
	defer func() { _ = recover() }()
	close(g.done)
}

// sweep 清理过期条目：
//   - ips：令牌已回满（无待补充）且超过 1 小时无新活动 → 回收；
//
// B-6：失败计数 + 账号锁定 + 改密令牌已迁入 SessionStore，过期清理由 SessionStore 负责
// （InProcess 在 PurgeBlacklist/PurgeChangePasswordTokens 中清理，Redis 靠 TTL 自动过期）。
// 此处仅清理进程内 ips 令牌桶。
func (g *loginGuard) sweep() {
	g.mu.Lock()
	now := time.Now()
	for ip, rec := range g.ips {
		if rec.tokens >= loginRateBurst && now.Sub(rec.last) > time.Hour {
			delete(g.ips, ip)
		}
	}
	g.mu.Unlock()
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

// ============================================================================
// JWT access token 吊销黑名单（P1-G4 + B-6）。
//
// 登出时 access token 仍在 15min 有效期内持续可用（无状态 JWT 无法主动失效）。
// 此黑名单记录已登出的 jti（JWT ID），userFromToken 校验时检查 jti 是否在黑名单。
//
// B-6 多副本共享：黑名单经 SessionStore 持久化，多副本下登出全局生效。
//   - InProcessSessionStore：进程内 map（单副本/demo 默认）；
//   - RedisSessionStore：Redis 后端（多副本 HA 共享，登出后所有副本立即拒绝该 token）。
// ============================================================================

// issueUserToken 为用户签发 JWT token。
// claims 包含：用户 ID/用户名/角色 ID/权限/租户/过期时间。
// P1-G4：SignJWT 自动生成 jti（JWT ID），用于登出吊销。
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
	// P1-G4 JWT 吊销：登出时 jti 加入黑名单，校验时检查。
	// B-6：黑名单经 SessionStore 共享，多副本下登出全局生效（Redis 后端时）。
	if s.sessionStore != nil && s.sessionStore.IsBlacklisted(claims.JTI) {
		return nil, errors.New("token has been revoked")
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

// getRolePermCache 返回角色名→权限集合映射（取自 store.RolePermissions()）。
//
// 原实现为包初始化时计算一次的全局 var rolePermCache，管理员经 CreateRole/UpdateRole/
// DeleteRole 修改角色权限后缓存不更新，导致权限陈旧（已分配新权限的用户被旧缓存拒绝，
// 或已收回权限的用户仍被旧缓存放行）。改为每次调用动态查询 store.RolePermissions()，
// 保证权限划分始终与 seedRBAC/DB 当前定义一致，杜绝定义漂移。
//
// 性能：store.RolePermissions() 仅遍历预置 rbacPermSpecs 派生映射（纯内存计算，无 IO），
// 每次调用开销可忽略，且 authorizeByRoles 仅在网关注入/联邦转发路径（非热路径）调用。
func getRolePermCache() map[string][]string {
	return store.RolePermissions()
}

// requireProd 统一产品级 RBAC 鉴权闸：在 requireTenantContext（租户隔离）之后调用，
// 校验当前身份是否拥有 required 权限。兼容两种身份来源：
//   - 联邦入站（X-Federation-Forwarded=1）：必须经 verifyFederationRequest 验签 HMAC 通过，
//     信任来自可信控制面 peer 的请求（用户级 RBAC 已在来源控制面执行）；验签失败 → 403。
//   - Authorization: Bearer（或 opsmesh_at Cookie）：走 JWT 路径（requirePermission）。
//   - 网关注入 X-User-Roles（角色名）：展开为权限集合后校验（authorizeByRoles）。
//   - demo 模式且无任何身份头：放行，保持本地一键体验的宽松语义（与 requireTenantContext 一致）。
//   - 其余：401。
//
// 返回 (user, ok)；ok=false 时已写入响应，调用方应直接 return。
func (s *Server) requireProd(w http.ResponseWriter, r *http.Request, required string) (*store.User, bool) {
	// 1. 联邦入站：必须验签 HMAC 通过才信任 peer（用户 RBAC 已在来源侧执行）。
	//    原实现仅判断头存在即放行，未验签，任意客户端伪造 X-Federation-Forwarded=1 即可绕过 RBAC。
	if r.Header.Get("X-Federation-Forwarded") == "1" {
		if err := s.verifyFederationRequest(r); err != nil {
			log.Printf("controlplane: requireProd 联邦验签失败: %v", err)
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "federation signature verification failed"})
			return nil, false
		}
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
	// 动态查询角色权限映射，保证管理员修改角色权限后立即生效（无陈旧缓存）。
	rolePerms := getRolePermCache()
	for _, rn := range roleNames {
		for _, p := range rolePerms[rn] {
			if p == required {
				return nil, true
			}
		}
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "permission denied: " + required})
	return nil, false
}
