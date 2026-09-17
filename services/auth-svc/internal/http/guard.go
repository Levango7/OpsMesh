// guard.go — A2 登录防爆破 guard（与 controlplane auth.go:392-414 参数逐字一致）。
//
// 两道闸（controlplane loginGuard 同语义）：
//  1. IP 令牌桶：burst=5、refill≈1/6s（10/min）——防撞库+DoS；进程内实现
//     （多副本各自限流，副本数 N 时实际阈值 N*burst——与 controlplane 同已知限制）。
//  2. 账号锁定：单账号连续失败 5 次/15min 窗口 → 锁 15min。
//     Redis 可用时：失败计数 + 锁定标记存 Redis（多副本共享，与 controlplane 经
//     SessionStore 共享计数同语义）；Redis 不可用时降级为进程内计数（单副本限制）。
//
// 挂载点：HTTP 网关的 login/register 入口（gRPC 侧已有 ratelimit 拦截器兜底）。
package http

import (
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/auth-svc/internal/cache"
)

// guard 参数与 controlplane/auth.go:402-407 常量逐字一致（双轨对照前提）。
const (
	guardRateBurst  = 5                // 令牌桶容量（瞬时最大尝试数）
	guardRateRefill = 1.0 / 6.0        // 补充速率（每秒），约每 6s 1 个 ≈10/min
	guardMaxFails   = 5                // 单账号连续失败阈值
	guardFailWindow = 15 * time.Minute // 失败计数滑动窗口
	guardLockDur    = 15 * time.Minute // 账号锁定时长
)

// loginGuard 防爆破状态。
//
// IP 令牌桶始终进程内（多副本各自限流，与 controlplane 同语义）。
// 账号锁定：cache 非 nil 且可用时走 Redis（多副本共享）；否则走内存 fails map。
type loginGuard struct {
	mu    sync.Mutex
	ips   map[string]*guardRateRec // IP → 令牌桶（进程内）
	fails map[string]*guardFailRec // username → 失败记录（内存降级）
	cache *cache.Cache             // Redis 后端（可为 nil 或 disabled → 内存降级）
}

// guardRateRec IP 令牌桶状态。
type guardRateRec struct {
	tokens float64
	last   time.Time
}

// guardFailRec 账号失败记录（窗口内计数 + 锁定截止时间）。
type guardFailRec struct {
	fails    int
	windowAt time.Time // 当前失败窗口起点
	lockedTo time.Time // 锁定截止（零值=未锁）
}

// newLoginGuard 构造纯内存 guard（向后兼容：测试与无 Redis 场景）。
func newLoginGuard() *loginGuard {
	return &loginGuard{
		ips:   make(map[string]*guardRateRec),
		fails: make(map[string]*guardFailRec),
	}
}

// newLoginGuardWithCache 构造带 Redis 后端的 guard。
//
// cache 非 nil 且 Enabled 时：账号锁定计数存 Redis（多副本共享）。
// cache 为 nil 或 disabled 时：降级为内存计数（与 newLoginGuard 等价）。
func newLoginGuardWithCache(c *cache.Cache) *loginGuard {
	g := newLoginGuard()
	g.cache = c
	return g
}

// allowIP IP 令牌桶判断（true=放行）。与 controlplane loginGuard.allow 同算法。
// IP 令牌桶始终进程内（多副本各自限流，与 controlplane 同已知限制）。
func (g *loginGuard) allowIP(ip string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	rec, ok := g.ips[ip]
	if !ok {
		rec = &guardRateRec{tokens: guardRateBurst, last: now}
		g.ips[ip] = rec
	}
	rec.tokens += now.Sub(rec.last).Seconds() * guardRateRefill
	if rec.tokens > guardRateBurst {
		rec.tokens = guardRateBurst
	}
	rec.last = now
	if rec.tokens < 1 {
		return false
	}
	rec.tokens--
	return true
}

// accountLocked 账号是否处于锁定。
//
// Redis 可用时：查 Redis lock key（存在=锁定）。
// Redis 不可用时：查内存 fails map。
func (g *loginGuard) accountLocked(username string) bool {
	// Redis 后端。
	if g.cache != nil && g.cache.Enabled() {
		return g.cache.Exists("guard:lock:" + username)
	}
	// 内存降级。
	g.mu.Lock()
	defer g.mu.Unlock()
	rec, ok := g.fails[username]
	if !ok {
		return false
	}
	if rec.lockedTo.IsZero() || time.Now().After(rec.lockedTo) {
		return false // 未锁或锁已过期
	}
	return true
}

// recordFail 记录一次登录失败；达阈值锁定账号。
//
// Redis 可用时：INCR 失败计数（TTL=guardFailWindow），达阈值时 Set lock key（TTL=guardLockDur）。
// Redis 不可用时：内存计数（窗口滑动与 controlplane 语义一致）。
func (g *loginGuard) recordFail(username string) {
	// Redis 后端。
	if g.cache != nil && g.cache.Enabled() {
		failKey := "guard:fails:" + username
		n, ok := g.cache.IncrWithExpire(failKey, guardFailWindow)
		if !ok {
			// Redis 操作失败，降级内存。
			g.recordFailMemory(username)
			return
		}
		if n >= int64(guardMaxFails) {
			// 达阈值：设置锁定标记（TTL=guardLockDur）。
			g.cache.SetWithTTL("guard:lock:"+username, 1, guardLockDur)
		}
		return
	}
	// 内存降级。
	g.recordFailMemory(username)
}

// recordFailMemory 内存降级的失败计数（与原实现逐字一致）。
func (g *loginGuard) recordFailMemory(username string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	rec, ok := g.fails[username]
	if !ok || now.Sub(rec.windowAt) > guardFailWindow {
		// 新窗口（首次失败或旧窗口过期）：重置计数。
		rec = &guardFailRec{fails: 0, windowAt: now}
		g.fails[username] = rec
	}
	rec.fails++
	if rec.fails >= guardMaxFails {
		rec.lockedTo = now.Add(guardLockDur)
	}
}

// recordSuccess 登录成功清除失败计数（与 controlplane 同语义：成功即复位）。
//
// Redis 可用时：Delete 失败计数 + 锁定标记。
// Redis 不可用时：Delete 内存 fails map 条目。
func (g *loginGuard) recordSuccess(username string) {
	// Redis 后端。
	if g.cache != nil && g.cache.Enabled() {
		g.cache.Delete("guard:fails:" + username)
		g.cache.Delete("guard:lock:" + username)
		return
	}
	// 内存降级。
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.fails, username)
}
