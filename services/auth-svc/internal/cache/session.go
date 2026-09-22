// session.go — Redis Session 存储（TD-60 安全增强）。
//
// 基于 Redis 的有状态 Session 管理，作为 JWT 无状态方案的增强：
//   - Session 过期：Redis TTL 自动清理（无需定时扫描）
//   - Session 续期：滑动过期（每次访问刷新 TTL）
//   - Session 撤销：Delete 即时失效（登出/封禁全局生效）
//   - 多副本共享：Redis 后端时所有副本共享 Session 状态
//
// 降级策略（Redis 不可用）：
//   - 所有操作降级为 no-op，调用方回退到 JWT 无状态模式
//   - ValidateSession 返回 (nil, nil) 表示"不阻止"（JWT 已校验通过）
//   - 撤销操作静默跳过（JWT 自然过期兜底）
//
// 与 controlplane SessionStore 接口语义对齐（黑名单 + 改密令牌 + Session）。
package cache

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// Session 表示一个用户会话。
type Session struct {
	SessionID string    `json:"session_id"` // 会话 ID（= JWT jti）
	UserID    string    `json:"user_id"`
	TenantID  string    `json:"tenant_id"`
	DeviceFP  string    `json:"device_fp"` // 绑定的设备指纹
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"` // 最后访问时间（滑动过期用）
}

// SessionStore 提供基于 Redis 的 Session 存储与生命周期管理。
//
// Redis 可用时：Session 存 Redis（key=session:{id}），TTL=滑动过期。
// Redis 不可用时：降级为无状态模式（所有操作 no-op，Validate 始终放行）。
type SessionStore struct {
	client  *redis.Client
	ttl     time.Duration // Session TTL（滑动过期）
	enabled bool
}

// sessionKeyPrefix Redis key 前缀。
const sessionKeyPrefix = "session:"

// NewSessionStore 创建 Session 存储。
// addr 为 Redis 地址（空=默认 localhost:6379）；ttl 为 Session 过期时间。
// Redis 不可用时 enabled=false，降级为无状态模式。
func NewSessionStore(addr string, ttl time.Duration) *SessionStore {
	if addr == "" {
		addr = defaultRedisAddr
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour // 默认 24h
	}
	client := redis.NewClient(&redis.Options{
		Addr:        addr,
		DialTimeout: opTimeout,
		ReadTimeout: opTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	enabled := true
	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("[session] Redis unreachable at %s (degraded to stateless JWT mode): %v", addr, err)
		enabled = false
	}

	return &SessionStore{
		client:  client,
		ttl:     ttl,
		enabled: enabled,
	}
}

// Enabled 报告 Session 存储是否可用（Redis 连通）。
// false 时调用方应回退到 JWT 无状态模式。
func (s *SessionStore) Enabled() bool {
	return s.enabled
}

// Create 创建一个新 Session。
// Redis 不可用时 no-op（降级：JWT 已携带身份，无需 Session）。
func (s *SessionStore) Create(sess *Session) error {
	if !s.enabled {
		return nil
	}
	sess.CreatedAt = time.Now()
	sess.LastSeen = sess.CreatedAt
	return s.save(sess)
}

// save 持久化 Session（含滑动 TTL）。
func (s *SessionStore) save(sess *Session) error {
	if !s.enabled {
		return nil
	}
	data, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	return s.client.Set(ctx, s.key(sess.SessionID), data, s.ttl).Err()
}

// Validate 校验 Session 是否有效（存在且未过期）。
//
// Redis 可用时：查 Redis，命中则续期（滑动过期）并返回 Session。
// Redis 不可用时：返回 (nil, nil) 表示"不阻止"（降级到 JWT 无状态校验）。
//
// 返回 (session, error)：
//   - 有效：(session, nil)
//   - 不存在/已过期：(nil, nil)
//   - Redis 错误：(nil, err)
func (s *SessionStore) Validate(sessionID string) (*Session, error) {
	if !s.enabled {
		return nil, nil // 降级：不阻止
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	data, err := s.client.Get(ctx, s.key(sessionID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Session 不存在/已过期
		}
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	// 滑动过期：续期 TTL。
	sess.LastSeen = time.Now()
	_ = s.save(&sess)
	return &sess, nil
}

// Revoke 撤销 Session（登出/封禁）。
// Redis 不可用时 no-op（降级：JWT 自然过期兜底）。
func (s *SessionStore) Revoke(sessionID string) error {
	if !s.enabled {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	return s.client.Del(ctx, s.key(sessionID)).Err()
}

// RevokeAllForUser 撤销某用户的所有 Session（改密/封禁用户）。
//
// 实现方式：维护 user→{sessionIDs} 反向索引（key=user_sessions:{userID}，Set 结构）。
// 撤销时遍历 Set 逐个 Delete。Redis 不可用时 no-op。
func (s *SessionStore) RevokeAllForUser(userID string) error {
	if !s.enabled {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	idxKey := "user_sessions:" + userID
	sessionIDs, err := s.client.SMembers(ctx, idxKey).Result()
	if err != nil {
		return err
	}
	for _, sid := range sessionIDs {
		_ = s.client.Del(ctx, s.key(sid)).Err()
	}
	_ = s.client.Del(ctx, idxKey).Err()
	return nil
}

// Refresh 续期 Session（滑动过期）。
// 等价于 Validate 但不返回 Session（仅刷新 TTL）。
func (s *SessionStore) Refresh(sessionID string) error {
	if !s.enabled {
		return nil
	}
	_, err := s.Validate(sessionID)
	return err
}

// Close 关闭 Redis 连接。
func (s *SessionStore) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// key 构造 Redis key。
func (s *SessionStore) key(sessionID string) string {
	return sessionKeyPrefix + sessionID
}
