// guard.go — A2 登录防爆破 guard（与 controlplane auth.go:392-414 参数逐字一致）。
//
// 两道闸（controlplane loginGuard 同语义）：
//  1. IP 令牌桶：burst=5、refill≈1/6s（10/min）——防撞库+DoS；进程内实现
//     （多副本各自限流，副本数 N 时实际阈值 N*burst——与 controlplane 同已知限制）。
//  2. 账号锁定：单账号连续失败 5 次/15min 窗口 → 锁 15min——进程内计数
//     （A2 单副本 MVP；controlplane 经 SessionStore 共享计数，auth-svc 多副本
//     立项时接 Redis 对齐——方案 V2 R7 声明）。
//
// 挂载点：HTTP 网关的 login/register 入口（gRPC 侧已有 ratelimit 拦截器兜底）。
package http

import (
	"sync"
	"time"
)

// guard 参数与 controlplane/auth.go:402-407 常量逐字一致（双轨对照前提）。
const (
	guardRateBurst  = 5                // 令牌桶容量（瞬时最大尝试数）
	guardRateRefill = 1.0 / 6.0        // 补充速率（每秒），约每 6s 1 个 ≈10/min
	guardMaxFails   = 5                // 单账号连续失败阈值
	guardFailWindow = 15 * time.Minute // 失败计数滑动窗口
	guardLockDur    = 15 * time.Minute // 账号锁定时长
)

// loginGuard 防爆破状态（进程内）。
type loginGuard struct {
	mu    sync.Mutex
	ips   map[string]*guardRateRec // IP → 令牌桶
	fails map[string]*guardFailRec // username → 失败记录
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

func newLoginGuard() *loginGuard {
	return &loginGuard{
		ips:   make(map[string]*guardRateRec),
		fails: make(map[string]*guardFailRec),
	}
}

// allowIP IP 令牌桶判断（true=放行）。与 controlplane loginGuard.allow 同算法。
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

// accountLocked 账号是否处于锁定（含过期清理）。
func (g *loginGuard) accountLocked(username string) bool {
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

// recordFail 记录一次登录失败；达阈值锁定账号（窗口滑动与 controlplane 语义一致）。
func (g *loginGuard) recordFail(username string) {
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
func (g *loginGuard) recordSuccess(username string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.fails, username)
}
