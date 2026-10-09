// limiter.go 按 IP 的令牌桶限流器（TD-87 批 2 第二批：自父包 server_security.go 迁入）。
//
// 内存有界（P1-5）：IP 由请求方自选（IPv6 地址空间近乎无限），无上限的 buckets 本身就是
// 内存耗尽面——达到 maxBuckets 后先清理空闲桶；仍满则放行但不建桶（攻击方早已持有桶并处于
// 限流之下，放行新 IP 不削弱对在途攻击的抑制，同时内存不再增长）。
//
// 父包保留 `Limiter` 类型别名 + `newRateLimiter` 薄包装（server_security.go），
// 9 处构造点与 `rateLimitMiddleware` 零改动；触及内部态（buckets/tokenBucket/maxBuckets）的
// 3 个用例随类型迁入 limiter_test.go。
package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/internal/logx"
)

const maxRateLimitBuckets = 50000

// Limiter 按 IP 令牌桶限流器。
// 每个 IP 维护一个独立的令牌桶，按 ratePerSec 速率补充令牌，桶容量=ratePerSec（允许 1s 突发）。
// 超过桶容量时拒绝请求（返回 429）。sweepInterval 周期清理空闲 IP 条目防内存泄漏。
type Limiter struct {
	mu            sync.Mutex
	buckets       map[string]*tokenBucket
	ratePerSec    int
	sweepInterval time.Duration
	// maxBuckets 跟踪的 IP 桶数上限（<=0 时取 maxRateLimitBuckets）；untracked 为达上限后
	// 未跟踪（直接放行且不建桶）的请求数，lastUntrackedLog 为上次告警时刻（限噪用）。
	maxBuckets       int
	untracked        uint64
	lastUntrackedLog time.Time
}

// tokenBucket 令牌桶。lastRefill 为上次补充时刻，tokens 为当前令牌数（浮点支持分数补充）。
type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
}

// New 构造限流器。ratePerSec 为每秒允许的请求数；sweepInterval 为清理周期。
func New(ratePerSec int, sweepInterval time.Duration) *Limiter {
	rl := &Limiter{
		buckets:       make(map[string]*tokenBucket),
		ratePerSec:    ratePerSec,
		sweepInterval: sweepInterval,
		maxBuckets:    maxRateLimitBuckets,
	}
	go rl.sweepLoop()
	return rl
}

// Allow 检查 IP 是否允许放行。true=放行并消耗一个令牌；false=拒绝（429）。
//
// 内存有界（P1-5）：IP 由请求方自选（IPv6 地址空间近乎无限），无上限的 buckets
// 本身即内存耗尽面。达到上限后先清理空闲桶；若仍满，则放行但不建桶——正在刷流量的
// 攻击方早已持有桶并处于限流之下，放行新 IP 不会削弱对在途攻击的抑制，同时内存不再增长。
func (rl *Limiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b, ok := rl.buckets[ip]
	if !ok {
		limit := rl.maxBuckets
		if limit <= 0 {
			limit = maxRateLimitBuckets
		}
		if len(rl.buckets) >= limit {
			rl.evictIdleLocked(now)
			if len(rl.buckets) >= limit {
				rl.untracked++
				if rl.lastUntrackedLog.IsZero() || now.Sub(rl.lastUntrackedLog) > 30*time.Second {
					rl.lastUntrackedLog = now
					logx.Warn(context.Background(), "限流器 IP 桶已达上限：本请求仅放行不跟踪（不再新建桶）",
						"tracked", len(rl.buckets), "limit", limit, "untracked_total", rl.untracked)
				}
				return true
			}
		}
		// 首次访问：满桶（容量=ratePerSec），允许 1s 突发。
		b = &tokenBucket{tokens: float64(rl.ratePerSec), lastRefill: now}
		rl.buckets[ip] = b
	}
	// 按经过时间补充令牌。
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * float64(rl.ratePerSec)
	if b.tokens > float64(rl.ratePerSec) {
		b.tokens = float64(rl.ratePerSec) // 上限=桶容量
	}
	b.lastRefill = now
	if b.tokens >= 1 {
		b.tokens -= 1
		return true
	}
	return false
}

// evictIdleLocked 清理超过 sweepInterval 未访问的桶（调用方须持锁）。
func (rl *Limiter) evictIdleLocked(now time.Time) {
	for ip, b := range rl.buckets {
		if now.Sub(b.lastRefill) > rl.sweepInterval {
			delete(rl.buckets, ip)
		}
	}
}

// sweepLoop 周期清理超过 sweepInterval 未访问的 IP 条目，防内存泄漏。
func (rl *Limiter) sweepLoop() {
	ticker := time.NewTicker(rl.sweepInterval)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		rl.evictIdleLocked(time.Now())
		rl.mu.Unlock()
	}
}
