// auth_tokens.go 刷新令牌 + 改密令牌 + JWT 令牌管理。
//
// 刷新令牌存储（从进程内全局 map 改为 store 持久化）：
// 原实现（MVP）使用进程内 map 存 refresh token，多副本 HA 部署下登录态随机失效
// （登录请求落到副本 A，刷新请求落到副本 B 时 rt 不存在 → 401）。现改为经
// store.RefreshTokenStore 接口持久化（MemoryStore / SQLStore 均已实现），
// 多副本共享同一 MySQL 时跨副本续期一致。
//
// 安全设计（与 install_tokens 同范式，明文不落库）：
//   - 库存/内存只存 token 的 SHA-256 摘要（TokenHash），不存明文；
//   - DeviceFP（设备指纹）绑定签发设备，防 token 跨设备重放；
//   - 旋转：consume 校验通过即 DeleteRefreshToken，旧 rt 立即作废防重放。
package controlplane

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/store"
)

// hashRefreshToken 计算 refresh token 明文的 SHA-256 摘要（hex 编码）。
// 库存/内存只存摘要，不存明文——DB 只读账号/备份泄露不等于活体 refresh token 泄露。
func hashRefreshToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// createRefreshToken 生成并持久化一个刷新令牌（crypto/rand，32 字节十六进制）。
// 返回明文 token（仅下发给客户端 Cookie），库内只存其 SHA-256 摘要。
// deviceFP 绑定签发设备（空串=不校验设备，向后兼容旧客户端）。
// tenantID 为该用户的所属租户（空值归一为 default），随 refresh token 持久化，
// 供多副本下登录态审计与租户归属对账使用。
func (s *Server) createRefreshToken(userID, tenantID, deviceFP string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	// 持久化：存摘要 + UserID + TenantID + DeviceFP + 过期时间，不存明文。
	if err := s.store.SaveRefreshToken(&store.RefreshToken{
		TokenHash: hashRefreshToken(id),
		UserID:    userID,
		TenantID:  tenantOrDefault(tenantID),
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
// 原子消费：原实现为 Get→Delete→校验三步，多副本并发下同一 rt 可被双消费
// （副本 A Get 后、Delete 前，副本 B 也 Get 到同一 rt）。现改为调用
// store.ConsumeRefreshToken 原子读取+删除，保证同一 rt 仅被消费一次。
//
// DeviceFP deadline：超过 cfg.DeviceFPDeadline 之后签发的 refresh token 必须绑定
// DeviceFP（非空）。deadline 前保持向后兼容（DeviceFP 为空时跳过设备绑定校验）；
// deadline 后 DeviceFP 为空则拒绝（强制设备绑定，防 token 跨设备重放）。
// deadline 零值=不强制（完全向后兼容）。
func (s *Server) consumeRefreshToken(id, deviceFP string) (*store.RefreshToken, bool) {
	hash := hashRefreshToken(id)
	// 原子消费：读取+删除在单次互斥操作内完成，防并发双消费。
	rt, ok := s.store.ConsumeRefreshToken(hash)
	if !ok || rt == nil {
		return nil, false
	}
	if time.Now().After(rt.ExpiresAt) {
		return nil, false
	}
	// DeviceFP deadline 强制非空：超过 deadline 且存储的 DeviceFP 为空时拒绝。
	// 用于渐进式强制设备绑定：deadline 前允许旧客户端不传 DeviceFP（向后兼容），
	// deadline 后强制要求新签发的 refresh token 必须绑定 DeviceFP。
	// deadline 零值=不强制（完全向后兼容，原有行为）。
	if !s.deviceFPDeadline.IsZero() && rt.CreatedAt.After(s.deviceFPDeadline) && rt.DeviceFP == "" {
		return nil, false
	}
	// 设备绑定校验：存储的 DeviceFP 非空且请求携带了 DeviceFP 时，两者必须匹配。
	// 存储 DeviceFP 为空（旧客户端签发时未绑定）或请求未携带 DeviceFP 时不校验（向后兼容）。
	// 注：DeviceFP 为空时跳过校验，兼容旧客户端签发的 token（未绑定设备指纹）。
	// 新签发的 refresh token 均绑定 DeviceFP，旧 token 旋转后自动获得绑定。
	// ：deadline 后签发的 token 已在上方强制 DeviceFP 非空，此处校验必然执行。
	if rt.DeviceFP != "" && deviceFP != "" && rt.DeviceFP != deviceFP {
		return nil, false
	}
	return rt, true
}

// revokeRefreshToken 吊销指定刷新令牌（登出时调用）。
func (s *Server) revokeRefreshToken(id string) {
	s.store.DeleteRefreshToken(hashRefreshToken(id))
}

// revokeAccessTokenFromRequest 从请求中提取 access token，解析 jti 并加入吊销黑名单。
// 登出时调用，使 access token 在过期前立即失效（而非等 15min 自然过期）。
// token 缺失/无效时静默跳过（不阻断登出流程）。
// 黑名单经 SessionStore 共享，多副本下登出全局生效（Redis 后端时）。
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

// purgeExpiredRefreshTokens 清理过期刷新令牌（store 持久化后改为 no-op）。
//
// 原进程内 map 实现需周期扫描清理防内存无限增长；改用 store 持久化后：
//   - MemoryStore：consumeRefreshToken 校验过期时已 DeleteRefreshToken 顺带清理；
//   - SQLStore：可由 DB 定时任务或后续扩展 store 层批量清理接口处理；
//   - 本函数保留 no-op 签名以兼容 server.go startRefreshSweep 调用，避免破坏现有启动流程。
//
// 顺带清理 JWT 吊销黑名单与改密令牌的过期条目（token 自然过期后条目无意义）。
// 经 SessionStore 接口清理，InProcess 主动清理 map，Redis 靠 TTL 自动过期（no-op）。
func (s *Server) purgeExpiredRefreshTokens() {
	// no-op：store 持久化后过期清理由 consumeRefreshToken 顺带完成（校验过期即删除）。
	// /：清理 token blacklist 与改密令牌过期条目，防无界增长。
	if s.sessionStore != nil {
		s.sessionStore.PurgeBlacklist()
		s.sessionStore.PurgeChangePasswordTokens()
	}
}

// ============================================================================
// 改密令牌存储（安全债）：mustChangePassword=true 用户登录时不签发
// access token，仅签发一次性短时效 changePasswordToken（5min），仅可用于
// /api/v1/auth/change-password。改密成功后才签发正式 at+rt。
//
// 多副本共享：原进程内 map 改为经 SessionStore 接口存储，多副本下任一副本签发
// 的改密令牌在其他副本也可消费（Redis 后端时）。
// ============================================================================

// createChangePasswordToken 生成并存储一个一次性改密令牌（crypto/rand，32 字节十六进制）。
// 有效期 5 分钟，仅用于 /api/v1/auth/change-password。
// 经 SessionStore 持久化，多副本共享。
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
// 经 SessionStore 原子消费，多副本下同一令牌仅被消费一次。
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
		// panic 合理：crypto/rand 失败意味着系统随机数生成器不可用（如 /dev/urandom 不可读），
		// 这是不可恢复的系统级错误，无法降级处理。正常环境下不会触发。
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return prefix + "-" + hex.EncodeToString(b)
}

// ============================================================================
// JWT access token 吊销黑名单（+）。
//
// 登出时 access token 仍在 15min 有效期内持续可用（无状态 JWT 无法主动失效）。
// 此黑名单记录已登出的 jti（JWT ID），userFromToken 校验时检查 jti 是否在黑名单。
//
// 多副本共享：黑名单经 SessionStore 持久化，多副本下登出全局生效。
//   - InProcessSessionStore：进程内 map（单副本/demo 默认）；
//   - RedisSessionStore：Redis 后端（多副本 HA 共享，登出后所有副本立即拒绝该 token）。
// ============================================================================

// issueUserToken 为用户签发 JWT token。
// claims 包含：用户 ID/用户名/角色 ID/权限/租户/过期时间。
// ：SignJWT 自动生成 jti（JWT ID），用于登出吊销。
//
// 租户（P0-6）：取用户实体的 TenantID（空值归一 default），不再硬编码 "default"。
// 该 claim 是下游按租户作用域的唯一身份来源（requireTenantContext 交叉校验
// X-Tenant-ID 头，device/task/script/alert 各域按此过滤），故必须反映用户真实归属。
func (s *Server) issueUserToken(u *store.User) (string, error) {
	claims := authctx.JWTClaims{
		UserID:      u.ID,
		Username:    u.Username,
		Roles:       u.RoleIDs,
		Permissions: s.userPermissions(u),
		TenantID:    tenantOrDefault(u.TenantID),
		ExpiresAt:   time.Now().Add(accessTokenExpiry),
	}
	return authctx.SignJWT(claims, s.jwtSecret)
}

// ============================================================================
// 鉴权中间件：userFromToken / extractBearer / hasUserIdentityToken
// ============================================================================

// userFromToken 从请求的 Authorization: Bearer <token> 提取并验签 JWT，返回对应用户。
// 用于 /api/v1/auth/me 与用户管理 API 的鉴权。
// token 缺失/无效/用户不存在 → 返回 error。
func (s *Server) userFromToken(r *http.Request) (*store.User, error) {
	tokenStr, err := extractBearer(r)
	if err != nil {
		// ：Bearer 头缺失时回退 HttpOnly Cookie（前端不再持久化 token 到
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
	// JWT 吊销：登出时 jti 加入黑名单，校验时检查。
	// 黑名单经 SessionStore 共享，多副本下登出全局生效（Redis 后端时）。
	if s.sessionStore != nil && s.sessionStore.IsBlacklisted(claims.JTI) {
		return nil, errors.New("token has been revoked")
	}
	u := s.store.GetUser(claims.UserID)
	if u == nil {
		return nil, errors.New("user not found")
	}
	// 吊销：非 active 用户（disabled/rejected/pending/空）既有的有效签名 token 立即失效，
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
		return "", errors.New("authorization header is not Bearer format")
	}
	token := strings.TrimSpace(v[len(prefix):])
	if token == "" {
		return "", errors.New("empty bearer token")
	}
	return token, nil
}

// hasUserIdentityToken 判断请求是否携带用户身份 token（Bearer JWT 或 HttpOnly Cookie）。
// API Key（Bearer om_* 或 X-API-Key）不属用户 token，返回 false（交由 requireTenantContext 走 API Key 路径）。
func (s *Server) hasUserIdentityToken(r *http.Request) bool {
	if _, isKey := extractAPIKey(r); isKey {
		return false
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return true
	}
	if ck, err := r.Cookie(accessTokenCookieName); err == nil && strings.TrimSpace(ck.Value) != "" {
		return true
	}
	return false
}
