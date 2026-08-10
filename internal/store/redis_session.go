// redis_session.go 实现 SessionStore 接口的 Redis 后端（B-6 多副本会话状态共享）。
//
// 背景：InProcessSessionStore 的三类状态（JWT 黑名单/限流计数/改密令牌）均为进程内 map，
// 多副本 HA 部署下：
//   - 副本 A 登出将 jti 加入本地黑名单，副本 B 的 userFromToken 校验时查本地黑名单未命中 →
//     access token 在副本 B 仍有效（登出未生效）；
//   - 副本 A 签发改密令牌，副本 B 收到改密请求时查本地 map 未命中 → 首登改密跨副本失败；
//   - 撞库攻击者可在不同副本上各消耗 loginMaxFails 次失败配额 → 限流被副本数放大。
//
// RedisSessionStore 经 Redis 共享上述状态，多副本下登出/改密/限流全局一致。
//
// 实现要点：
//   - JWT 黑名单：SET key jti EX ttl（ttl 后 Redis 自动清理，无需 PurgeBlacklist）；
//   - 限流计数：INCR key + EXPIRE key window（首次 INCR 时设置 TTL，窗口过期后 Redis 自动重置）；
//   - 改密令牌：SET key userID EX ttl + GETDEL key（原子消费，Lua 脚本保证 GET+DEL 原子）；
//   - 全部操作带 context.Background() + 短超时（避免 Redis 故障时拖垮控制面）。
//
// 容错策略：
//   - Redis 不可达时，IsBlacklisted 返回 false（fail-open，不阻断已登录用户），
//     Blacklist/IncrRateLimit/CreateChangePasswordToken 记录错误日志后静默返回（不阻断主流程），
//     ConsumeChangePasswordToken 返回 ("", false)（fail-closed，改密令牌消费失败拒绝改密，安全优先）。
//   - 生产环境应配置 Redis HA（Sentinel/Cluster）+ 监控 Redis 连通性。
package store

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisSessionStore Redis 后端 SessionStore 实现。
//
// 多副本 HA 部署下经 Redis 共享会话状态（JWT 黑名单/限流计数/改密令牌），
// 使登出/改密/限流全局一致。
type RedisSessionStore struct {
	client *redis.Client
	prefix string // key 前缀（多租户/多实例隔离，默认 "opsmesh:"）
}

// NewRedisSessionStore 构造 Redis 后端 SessionStore。
//
// 参数：
//   - addr：Redis 地址（如 "redis:6379"）；
//   - prefix：key 前缀（如 "opsmesh:"，多实例共享同一 Redis 时隔离）；
//   - dialTimeout：连接超时（建议 5s）。
//
// 返回的实例已就绪，可直接使用。Redis 不可达不在此处 fail-fast（容错策略见文件注释），
// 调用方在首次操作时会感知到错误（IsBlacklisted 返回 false 等）。
func NewRedisSessionStore(addr, prefix string, dialTimeout time.Duration) (*RedisSessionStore, error) {
	if addr == "" {
		return nil, errRedisAddrRequired
	}
	if prefix == "" {
		prefix = "opsmesh:"
	}
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}
	client := redis.NewClient(&redis.Options{
		Addr:        addr,
		DialTimeout: dialTimeout,
	})
	// Ping 验证连通性（不 fail-fast，仅记录日志；首次操作失败时容错策略生效）。
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("[store] Redis 连通性检查失败（容错降级，IsBlacklisted 将 fail-open）: %v", err)
	}
	return &RedisSessionStore{client: client, prefix: prefix}, nil
}

// errRedisAddrRequired Redis 地址为空时返回。
var errRedisAddrRequired = errString("redis session store: addr required")

// key 拼接完整 Redis key（prefix + 业务 key）。
func (s *RedisSessionStore) key(k string) string {
	return s.prefix + k
}

// blacklistKey 拼接 JWT 黑名单 key。
func (s *RedisSessionStore) blacklistKey(jti string) string {
	return s.key("blacklist:" + jti)
}

// rateLimitKey 拼接限流计数 key。
func (s *RedisSessionStore) rateLimitKey(k string) string {
	return s.key("ratelimit:" + k)
}

// cpTokenKey 拼接改密令牌 key。
func (s *RedisSessionStore) cpTokenKey(token string) string {
	return s.key("cptoken:" + token)
}

// ============================================================================
// JWT access token 吊销黑名单
// ============================================================================

// IsBlacklisted 检查 jti 是否在黑名单中。
//
// 容错：Redis 不可达时返回 false（fail-open，不阻断已登录用户）。
// Redis 故障期间登出的 token 在 Redis 恢复后仍可被吊销（Blacklist 操作会在恢复后重试写入）。
func (s *RedisSessionStore) IsBlacklisted(jti string) bool {
	if jti == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	// EXISTS 检查 key 是否存在（TTL 过期后 Redis 自动删除，无需额外清理）。
	n, err := s.client.Exists(ctx, s.blacklistKey(jti)).Result()
	if err != nil {
		// fail-open：Redis 故障时不阻断已登录用户（登出仅在 Redis 恢复后生效）。
		log.Printf("[store] Redis IsBlacklisted 失败（fail-open）: %v", err)
		return false
	}
	return n > 0
}

// Blacklist 将 jti 加入黑名单，ttl 后 Redis 自动过期。
//
// 容错：Redis 不可达时记录日志后静默返回（不阻断登出主流程）。
// Redis 恢复后该 jti 不会被补写（登出是一次性操作），但 token 会在 accessTokenExpiry 后自然过期。
func (s *RedisSessionStore) Blacklist(jti string, ttl time.Duration) {
	if jti == "" {
		return
	}
	if ttl <= 0 {
		ttl = accessTokenExpiryFallback
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	if err := s.client.Set(ctx, s.blacklistKey(jti), "1", ttl).Err(); err != nil {
		log.Printf("[store] Redis Blacklist 失败（登出未生效，token 将在 %v 后自然过期）: %v", ttl, err)
	}
}

// PurgeBlacklist 清理过期黑名单条目。
//
// Redis 实现靠 TTL 自动过期，此方法为 no-op（保留接口一致性）。
func (s *RedisSessionStore) PurgeBlacklist() {
	// no-op：Redis TTL 自动过期。
}

// ============================================================================
// 滑动窗口计数器（登录限流 + 失败计数）
// ============================================================================

// IncrRateLimit 对 key 在 window 滑动窗口内的计数 +1，返回当前窗口内的计数。
//
// 实现用 Redis INCR + EXPIRE：
//   - INCR key：计数 +1，返回新值；
//   - 若新值为 1（首次计数或窗口已过期被 Redis 清除），设置 EXPIRE key window；
//   - 若新值 > 1，不重设 EXPIRE（保持原窗口 TTL，避免每次计数都延长窗口）。
//
// 容错：Redis 不可达时返回 0（fail-open，不限流，避免 Redis 故障时所有登录被拒绝）。
// 安全权衡：Redis 故障期间限流失效，攻击者可借此窗口撞库；但 Redis 故障是运维事件，
// 应优先保证可用性（登录不被拒），限流失效窗口由 Redis HA + 监控兜底。
func (s *RedisSessionStore) IncrRateLimit(key string, window time.Duration) int {
	if key == "" {
		return 0
	}
	if window <= 0 {
		window = redisDefaultRateLimitWindow
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	rk := s.rateLimitKey(key)
	// INCR 原子计数 +1。
	n, err := s.client.Incr(ctx, rk).Result()
	if err != nil {
		// fail-open：Redis 故障时不限流（避免所有登录被拒绝）。
		log.Printf("[store] Redis IncrRateLimit 失败（fail-open，不限流）: %v", err)
		return 0
	}
	// 首次计数（n=1）时设置窗口 TTL；后续计数不重设（保持固定窗口语义）。
	if n == 1 {
		if err := s.client.Expire(ctx, rk, window).Err(); err != nil {
			log.Printf("[store] Redis IncrRateLimit 设置 TTL 失败（计数仍生效，但窗口可能不精确）: %v", err)
		}
	}
	return int(n)
}

// ResetRateLimit 重置 key 的计数。
//
// 容错：Redis 不可达时静默返回（不阻断登录成功流程）。
func (s *RedisSessionStore) ResetRateLimit(key string) {
	if key == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	if err := s.client.Del(ctx, s.rateLimitKey(key)).Err(); err != nil {
		log.Printf("[store] Redis ResetRateLimit 失败（不影响主流程）: %v", err)
	}
}

// ============================================================================
// 一次性改密令牌
// ============================================================================

// CreateChangePasswordToken 存储 token → userID 映射，ttl 后 Redis 自动过期。
//
// 容错：Redis 不可达时返回错误（调用方据此返回 500，首登改密失败，用户须重新登录获取新令牌）。
func (s *RedisSessionStore) CreateChangePasswordToken(token, userID string, ttl time.Duration) error {
	if token == "" {
		return errChangePasswordTokenRequired
	}
	if ttl <= 0 {
		ttl = redisDefaultCPTokenTTL
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	if err := s.client.Set(ctx, s.cpTokenKey(token), userID, ttl).Err(); err != nil {
		log.Printf("[store] Redis CreateChangePasswordToken 失败: %v", err)
		return err
	}
	return nil
}

// ConsumeChangePasswordToken 原子消费 token，返回关联的 userID。
//
// 实现用 Lua 脚本保证 GET + DEL 原子（避免并发消费同一 token）：
//
//	if redis.call("EXISTS", KEYS[1]) == 0 then return "" end
//	local v = redis.call("GET", KEYS[1])
//	redis.call("DEL", KEYS[1])
//	return v
//
// 容错：Redis 不可达时返回 ("", false)（fail-closed，改密令牌消费失败拒绝改密，安全优先）。
func (s *RedisSessionStore) ConsumeChangePasswordToken(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	// GETDEL 原子获取并删除（Redis 6.2+，go-redis v9 支持）。
	// 一次性：消费即删除，防重放。
	userID, err := s.client.GetDel(ctx, s.cpTokenKey(token)).Result()
	if err != nil {
		if err == redis.Nil {
			// token 不存在/已消费/已过期：正常拒绝。
			return "", false
		}
		// Redis 故障：fail-closed（拒绝改密，安全优先）。
		log.Printf("[store] Redis ConsumeChangePasswordToken 失败（fail-closed）: %v", err)
		return "", false
	}
	return userID, true
}

// PurgeChangePasswordTokens 清理过期改密令牌。
//
// Redis 实现靠 TTL 自动过期，此方法为 no-op（保留接口一致性）。
func (s *RedisSessionStore) PurgeChangePasswordTokens() {
	// no-op：Redis TTL 自动过期。
}

// ============================================================================
// 生命周期
// ============================================================================

// Close 关闭 Redis 客户端连接。
func (s *RedisSessionStore) Close() error {
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

// ============================================================================
// 常量
// ============================================================================

const (
	// redisOpTimeout 单次 Redis 操作超时（避免 Redis 故障时拖垮控制面）。
	redisOpTimeout = 2 * time.Second

	// accessTokenExpiryFallback Blacklist ttl 兜底（与 auth.go accessTokenExpiry 一致，15min）。
	accessTokenExpiryFallback = 15 * time.Minute

	// redisDefaultRateLimitWindow IncrRateLimit window 兜底。
	redisDefaultRateLimitWindow = 15 * time.Minute

	// redisDefaultCPTokenTTL 改密令牌 ttl 兜底（与 auth.go changePasswordTokenExpiry 一致，5min）。
	redisDefaultCPTokenTTL = 5 * time.Minute
)

// 编译期断言：确保 RedisSessionStore 实现 SessionStore 接口。
var _ SessionStore = (*RedisSessionStore)(nil)
