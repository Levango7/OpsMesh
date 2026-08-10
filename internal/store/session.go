// session.go 定义 SessionStore 接口及其进程内实现 InProcessSessionStore。
//
// 背景（B-6 多副本会话状态不共享）：
//   - 原 auth.go 中 tokenBlacklist（JWT 吊销黑名单）、changePasswordTokens（改密令牌）、
//     loginGuard 的限流/失败计数均为进程内 map，多副本 HA 部署下登出后 access token
//     在其他副本仍有效、改密令牌跨副本不可用、登录限流各副本独立计数。
//   - 现抽象为 SessionStore 接口，两种实现：
//     · InProcessSessionStore：封装原有进程内 map 逻辑（单副本/demo 默认）；
//     · RedisSessionStore：经 Redis 共享状态（多副本 HA，见 redis_session.go）。
//   - controlplane 通过 SessionStore 接口操作，不直接访问 map，平滑切换后端。
//
// 接口方法语义：
//   - IsBlacklisted/Blacklist：JWT access token 吊销黑名单（登出后立即失效）；
//   - IncrRateLimit/ResetRateLimit：滑动窗口计数器（登录限流 + 失败计数）；
//   - CreateChangePasswordToken/ConsumeChangePasswordToken：一次性改密令牌；
//   - Purge*：清理过期条目（InProcess 需主动清理，Redis 靠 TTL 自动过期）。
package store

import (
	"sync"
	"time"
)

// SessionStore 会话状态存储接口（B-6 多副本共享）。
//
// 抽象 auth.go 中原本进程内的三类状态：
//  1. JWT access token 吊销黑名单（登出后 jti 加入黑名单，校验时检查）；
//  2. 滑动窗口计数器（登录 IP 限流 + 用户名失败计数）；
//  3. 一次性改密令牌（首登强制改密场景）。
//
// 两种实现：
//   - InProcessSessionStore：进程内 map + sync.Mutex（单副本/demo 默认，零依赖）；
//   - RedisSessionStore：Redis 后端（多副本 HA 共享，见 redis_session.go）。
//
// 所有方法须并发安全。
type SessionStore interface {
	// ============================================================================
	// JWT access token 吊销黑名单
	// ============================================================================

	// IsBlacklisted 检查 jti 是否在黑名单中且未过期。
	// jti 为空时返回 false（无 jti 的旧 token 不校验吊销，向后兼容）。
	IsBlacklisted(jti string) bool

	// Blacklist 将 jti 加入黑名单，ttl 后自动过期（token 自然过期后黑名单条目无意义）。
	// jti 为空时 no-op。
	Blacklist(jti string, ttl time.Duration)

	// PurgeBlacklist 清理所有已过期的黑名单条目（防无界增长）。
	// Redis 实现靠 TTL 自动过期，此方法为 no-op；InProcess 实现需主动清理。
	PurgeBlacklist()

	// ============================================================================
	// 滑动窗口计数器（登录限流 + 失败计数）
	// ============================================================================

	// IncrRateLimit 对 key 在 window 滑动窗口内的计数 +1，返回当前窗口内的计数。
	// 窗口过期后自动重置为 1（新窗口的第一次计数）。
	// 用于：
	//   - 登录 IP 限流（key="ratelimit:ip:<ip>"，window=限流窗口，阈值=burst）；
	//   - 用户名失败计数（key="fail:user:<username>"，window=失败窗口，阈值=maxFails）。
	IncrRateLimit(key string, window time.Duration) int

	// ResetRateLimit 重置 key 的计数（登录成功后清除失败计数用）。
	// 不存在时 no-op。
	ResetRateLimit(key string)

	// ============================================================================
	// 一次性改密令牌（首登强制改密场景）
	// ============================================================================

	// CreateChangePasswordToken 存储 token → userID 映射，ttl 后自动过期。
	// token 为空时返回错误（主键不可空）。
	CreateChangePasswordToken(token, userID string, ttl time.Duration) error

	// ConsumeChangePasswordToken 原子消费 token，返回关联的 userID。
	// 一次性：消费后立即删除，防重放。
	// 不存在/已消费/已过期返回 ("", false)。
	ConsumeChangePasswordToken(token string) (userID string, ok bool)

	// PurgeChangePasswordTokens 清理过期改密令牌（防无界增长）。
	// Redis 实现靠 TTL 自动过期，此方法为 no-op；InProcess 实现需主动清理。
	PurgeChangePasswordTokens()

	// ============================================================================
	// 生命周期
	// ============================================================================

	// Close 释放底层资源（Redis 连接等）。
	// InProcess 实现为 no-op；Redis 实现关闭客户端连接。
	// 调用方（Server 销毁时）应调用此方法避免资源泄漏。
	Close() error
}

// ============================================================================
// InProcessSessionStore 进程内实现（单副本/demo 默认，零依赖）。
//
// 封装原有 auth.go 中的进程内 map 逻辑：
//   - blacklist: map[string]time.Time（jti -> 过期时间）；
//   - rateLimits: map[string]*rateLimitRec（key -> 计数记录）；
//   - changePasswordTokens: map[string]*changePasswordRec（token -> 会话记录）。
//
// 全部经 sync.Mutex 保护，并发安全。
// ============================================================================

// InProcessSessionStore 进程内 SessionStore 实现。
type InProcessSessionStore struct {
	mu sync.Mutex

	blacklist  map[string]time.Time          // jti -> 过期时间
	rateLimits map[string]*rateLimitRec      // key -> 计数记录
	cptokens   map[string]*changePasswordRec // token -> 会话记录
}

// rateLimitRec 滑动窗口计数记录。
type rateLimitRec struct {
	count  int
	window time.Time // 窗口起始时间（now - window 内的计数）
}

// changePasswordRec 改密令牌会话记录。
type changePasswordRec struct {
	userID    string
	expiresAt time.Time
}

// NewInProcessSessionStore 构造空的进程内 SessionStore。
func NewInProcessSessionStore() *InProcessSessionStore {
	return &InProcessSessionStore{
		blacklist:  make(map[string]time.Time),
		rateLimits: make(map[string]*rateLimitRec),
		cptokens:   make(map[string]*changePasswordRec),
	}
}

// IsBlacklisted 检查 jti 是否在黑名单中且未过期（过期条目顺带清理）。
func (s *InProcessSessionStore) IsBlacklisted(jti string) bool {
	if jti == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.blacklist[jti]
	if !ok {
		return false
	}
	// token 已自然过期，黑名单条目可清理。
	if time.Now().After(exp) {
		delete(s.blacklist, jti)
		return false
	}
	return true
}

// Blacklist 将 jti 加入黑名单，ttl 后自动过期。
func (s *InProcessSessionStore) Blacklist(jti string, ttl time.Duration) {
	if jti == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blacklist[jti] = time.Now().Add(ttl)
}

// PurgeBlacklist 清理所有已过期的黑名单条目。
func (s *InProcessSessionStore) PurgeBlacklist() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for jti, exp := range s.blacklist {
		if now.After(exp) {
			delete(s.blacklist, jti)
		}
	}
}

// IncrRateLimit 对 key 在 window 滑动窗口内的计数 +1，返回当前窗口内的计数。
// 窗口过期后自动重置为 1（新窗口的第一次计数）。
func (s *InProcessSessionStore) IncrRateLimit(key string, window time.Duration) int {
	if key == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	rec, ok := s.rateLimits[key]
	if !ok || now.Sub(rec.window) > window {
		// 不存在或窗口已过期：开启新窗口，计数为 1。
		s.rateLimits[key] = &rateLimitRec{count: 1, window: now}
		return 1
	}
	rec.count++
	return rec.count
}

// ResetRateLimit 重置 key 的计数。
func (s *InProcessSessionStore) ResetRateLimit(key string) {
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rateLimits, key)
}

// CreateChangePasswordToken 存储 token → userID 映射，ttl 后自动过期。
func (s *InProcessSessionStore) CreateChangePasswordToken(token, userID string, ttl time.Duration) error {
	if token == "" {
		return errChangePasswordTokenRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cptokens[token] = &changePasswordRec{
		userID:    userID,
		expiresAt: time.Now().Add(ttl),
	}
	return nil
}

// ConsumeChangePasswordToken 原子消费 token，返回关联的 userID。
// 一次性：消费后立即删除，防重放。
func (s *InProcessSessionStore) ConsumeChangePasswordToken(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.cptokens[token]
	if !ok {
		return "", false
	}
	delete(s.cptokens, token) // 一次性：消费即删除
	if time.Now().After(rec.expiresAt) {
		return "", false
	}
	return rec.userID, true
}

// PurgeChangePasswordTokens 清理过期改密令牌。
// 由 loginGuard.sweep 周期调用，防 cptokens map 在长运行中无界增长。
func (s *InProcessSessionStore) PurgeChangePasswordTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for token, rec := range s.cptokens {
		if now.After(rec.expiresAt) {
			delete(s.cptokens, token)
		}
	}
}

// Close 释放底层资源（进程内无资源，no-op）。
func (s *InProcessSessionStore) Close() error {
	return nil
}

// errChangePasswordTokenRequired 入参校验错误：token 为空时拒绝（主键不可空）。
var errChangePasswordTokenRequired = errString("change password token: token required")

// errString 自定义错误类型（避免与 memory_refresh.go 的 errRefreshTokenHashRequired 重复定义 errors.New）。
type errString string

func (e errString) Error() string { return string(e) }

// 编译期断言：确保 InProcessSessionStore 实现 SessionStore 接口。
var _ SessionStore = (*InProcessSessionStore)(nil)
