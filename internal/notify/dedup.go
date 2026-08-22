// Package notify 通知去重。
//
// 本文件实现通知去重：基于消息指纹（hash）的 TTL 缓存，相同消息在 TTL 内只发送一次。
// 重复消息会刷新过期时间（滑动窗口语义），避免短时间内告警风暴。
package notify

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// ============================================================================
// 通知去重器
// ============================================================================

// Deduplicator 通知去重器：基于消息指纹的 TTL 缓存。
//
// 字段语义：
//   - ttl：去重 TTL；相同消息在 TTL 内视为重复（IsDuplicate 返回 true）。
//   - seen：指纹 → 过期时间映射；过期条目由 Cleanup 清理。
//
// 并发安全：sync.RWMutex 保护 seen map。
// 滑动窗口语义：重复消息会刷新过期时间（IsDuplicate 内更新），避免持续告警的场景下
// 重复告警永远过不了去重窗口（每来一次就续期，可能导致永不放行）。
// 若需固定窗口语义，可在 IsDuplicate 返回 true 时不更新过期时间（见 IsDuplicateStrict）。
type Deduplicator struct {
	ttl  time.Duration
	mu   sync.RWMutex
	seen map[string]time.Time // key=hash(msg), value=expiry
}

// NewDeduplicator 构造去重器。ttl <= 0 时使用默认 5 分钟。
func NewDeduplicator(ttl time.Duration) *Deduplicator {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Deduplicator{
		ttl:  ttl,
		seen: make(map[string]time.Time),
	}
}

// messageHash 计算消息指纹（SHA-256 摘要的 hex 编码）。
// 指纹基于 Title/Body/Format/Severity/Source 五个字段（Timestamp 不参与，
// 避免同一告警因时间戳不同而绕过去重）。
func messageHash(msg *Message) string {
	if msg == nil {
		return ""
	}
	h := sha256.New()
	h.Write([]byte(msg.Title))
	h.Write([]byte{0}) // 分隔符，避免字段拼接歧义
	h.Write([]byte(msg.Body))
	h.Write([]byte{0})
	h.Write([]byte(msg.Format))
	h.Write([]byte{0})
	h.Write([]byte(msg.Severity))
	h.Write([]byte{0})
	h.Write([]byte(msg.Source))
	return hex.EncodeToString(h.Sum(nil))
}

// IsDuplicate 检查消息是否为重复（滑动窗口语义）。
//
// 行为：
//   - msg 为 nil → false（不视为重复，放行）
//   - 指纹不存在或已过期 → false（放行），并记录指纹 + 过期时间
//   - 指纹存在且未过期 → true（重复），并刷新过期时间（滑动窗口）
//
// 滑动窗口语义：每次重复都会续期 TTL，适合"持续告警应持续抑制"场景。
// 若需固定窗口（重复不续期），使用 IsDuplicateStrict。
func (d *Deduplicator) IsDuplicate(msg *Message) bool {
	if msg == nil {
		return false
	}
	key := messageHash(msg)
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	if expiry, exists := d.seen[key]; exists && now.Before(expiry) {
		// 重复且未过期：刷新过期时间（滑动窗口）。
		d.seen[key] = now.Add(d.ttl)
		return true
	}
	// 不存在或已过期：记录并放行。
	d.seen[key] = now.Add(d.ttl)
	return false
}

// IsDuplicateStrict 检查消息是否为重复（固定窗口语义）。
//
// 与 IsDuplicate 区别：重复时不刷新过期时间，首次记录的过期时间始终生效。
// 适合"窗口过后必须放行一次"场景（避免持续告警被永久抑制）。
func (d *Deduplicator) IsDuplicateStrict(msg *Message) bool {
	if msg == nil {
		return false
	}
	key := messageHash(msg)
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	if expiry, exists := d.seen[key]; exists && now.Before(expiry) {
		// 重复且未过期：不刷新过期时间（固定窗口）。
		return true
	}
	// 不存在或已过期：记录并放行。
	d.seen[key] = now.Add(d.ttl)
	return false
}

// Cleanup 清理过期条目。周期调用防止内存泄漏（如每分钟一次）。
// 返回清理的条目数（调试/监控用）。
func (d *Deduplicator) Cleanup() int {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	count := 0
	for k, expiry := range d.seen {
		if !now.Before(expiry) { // now >= expiry，已过期
			delete(d.seen, k)
			count++
		}
	}
	return count
}

// Size 返回当前缓存条目数（含已过期但未清理的；调试/监控用）。
func (d *Deduplicator) Size() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.seen)
}

// Reset 清空所有缓存条目（用于测试或强制重置）。
func (d *Deduplicator) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen = make(map[string]time.Time)
}
