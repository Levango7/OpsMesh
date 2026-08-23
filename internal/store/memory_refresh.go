// memory_refresh.go 实现 MemoryStore 的 RefreshTokenStore 子接口（刷新令牌）。
//
// 刷新令牌内存实现：
//   - refreshTokens 字段在 MemoryStore struct 中定义（map[string]*RefreshToken）；
//   - NewMemoryStore 中初始化为空 map；
//   - 本文件实现 Save/Get/Delete 方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点（与 memory_k8s.go 风格一致）：
//   - key 为 TokenHash（明文 token 的 SHA-256 摘要，明文不落库）；
//   - GetRefreshToken 返回深拷贝避免外部修改破坏内部状态；
//   - SaveRefreshToken 按 TokenHash 幂等（map 覆盖即 upsert）；
//   - memory 存储无持久化失败，SaveRefreshToken 恒返回 nil（除入参校验错误）。
package store

import (
	"errors"
	"time"
)

// errRefreshTokenHashRequired 入参校验错误：TokenHash 为空时拒绝（主键不可空）。
// memory 与 sql 实现共享，避免两处定义漂移。
var errRefreshTokenHashRequired = errors.New("refresh token: tokenHash required")

// SaveRefreshToken 保存/更新一个 refresh token（按 TokenHash 幂等 upsert）。
//
// 行为：
//   - rt 为 nil 时直接返回（空操作）；
//   - TokenHash 为空时拒绝（主键不可空），返回错误；
//   - TenantID 为空时归一为 default（与其他领域一致）；
//   - CreatedAt 为零值时填当前时间（新建场景）。
func (m *MemoryStore) SaveRefreshToken(rt *RefreshToken) error {
	if rt == nil {
		return nil
	}
	if rt.TokenHash == "" {
		return errRefreshTokenHashRequired
	}
	if rt.TenantID == "" {
		rt.TenantID = "default"
	}
	if rt.CreatedAt.IsZero() {
		rt.CreatedAt = time.Now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// map 覆盖即 upsert（按 TokenHash 幂等）。
	m.refreshTokens[rt.TokenHash] = rt
	return nil
}

// GetRefreshToken 按 TokenHash 返回单个 refresh token（深拷贝；不存在返回 nil）。
func (m *MemoryStore) GetRefreshToken(tokenHash string) *RefreshToken {
	if tokenHash == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	rt, ok := m.refreshTokens[tokenHash]
	if !ok {
		return nil
	}
	cp := *rt
	return &cp
}

// DeleteRefreshToken 按 TokenHash 删除 refresh token，返回是否删除成功（不存在返回 false）。
func (m *MemoryStore) DeleteRefreshToken(tokenHash string) bool {
	if tokenHash == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.refreshTokens[tokenHash]; !ok {
		return false
	}
	delete(m.refreshTokens, tokenHash)
	return true
}

// ConsumeRefreshToken 原子消费 refresh token：读取并立即删除（互斥锁保护，单次原子完成）。
// 多副本并发下同一 token 只能被消费一次，防重放。
// 不存在返回 (nil, false)。返回深拷贝避免外部修改破坏内部状态。
func (m *MemoryStore) ConsumeRefreshToken(tokenHash string) (*RefreshToken, bool) {
	if tokenHash == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rt, ok := m.refreshTokens[tokenHash]
	if !ok {
		return nil, false
	}
	// 原子读取+删除：在持锁期间完成 Get+Delete，并发安全。
	delete(m.refreshTokens, tokenHash)
	cp := *rt
	return &cp, true
}

// CleanupRefreshTokens 清理过期 refresh token。
// 仅删除 ExpiresAt < now 的条目，未过期的不动。
func (m *MemoryStore) CleanupRefreshTokens() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	n := 0
	for hash, rt := range m.refreshTokens {
		if rt.ExpiresAt.Before(now) {
			delete(m.refreshTokens, hash)
			n++
		}
	}
	return n
}
