// auth_guard.go 登录/注册防爆破 + 限流（loginGuard）。
//   - 限流：按客户端 IP 令牌桶，约束单位时间登录/注册尝试次数，防撞库与 DoS。
//   - 防爆破：按用户名累计失败次数，超阈值临时锁定账号，挫败密码爆破。
//
// 多副本共享：
//   - IP 令牌桶限流保留进程内（多副本各自限流，副本数 N 时实际阈值 N*burst，可接受；
//     令牌桶算法依赖 tokens/last 时序状态，难以用 Redis 原子操作精确实现）。
//   - 失败计数 + 账号锁定经 SessionStore 共享（多副本下任一副本触发锁定后其他副本也拒绝；
//     撞库攻击者无法在不同副本上各消耗 loginMaxFails 次配额）。
package controlplane

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/store"
)

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
// 失败计数经 SessionStore 共享，多副本下累计失败次数全局一致。
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
// 锁定状态经 SessionStore 共享，多副本下任一副本触发锁定后其他副本也拒绝。
func (g *loginGuard) locked(username string) bool {
	return g.store.IsBlacklisted(loginLockKey(username))
}

// resetFail 登录成功后清除该账号失败计数（解锁）。
// 经 SessionStore 共享，多副本下任一副本登录成功后其他副本也清除计数。
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
	defer func() { recover() }() //nolint:errcheck // recover 返回值非 error，吞 panic 即为语义
	close(g.done)
}

// sweep 清理过期条目：
//   - ips：令牌已回满（无待补充）且超过 1 小时无新活动 → 回收；
//
// 失败计数 + 账号锁定 + 改密令牌已迁入 SessionStore，过期清理由 SessionStore 负责
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

// deviceFingerprint 从请求头 X-Device-FP 提取设备指纹（设备绑定）。
// 前端可传 User-Agent 摘要或随机 UUID（同设备稳定即可）。空串表示不校验设备
// （向后兼容：旧客户端不传头时 DeviceFP 为空，签发时存空，验证时不校验）。
func deviceFingerprint(r *http.Request) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Header.Get("X-Device-FP"))
}
