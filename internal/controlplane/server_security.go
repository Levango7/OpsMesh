// server_security.go — SSRF 防护 + IP 限流（令牌桶）
package controlplane

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

func validateURLSSRF(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	// 协议白名单：仅允许 http/https。
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("scheme %q not allowed (only http/https)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("empty host in URL")
	}
	// 解析主机名中的 IP 地址（如果是域名则解析 DNS）。
	ips, err := net.LookupIP(host)
	if err != nil {
		// DNS 解析失败：可能是 IP 字面量，尝试直接解析。
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("cannot resolve host %q: %w", host, err)
		}
		ips = []net.IP{ip}
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("host %q resolves to private/loopback/link-local address %s", host, ip)
		}
	}
	return nil
}

// isPrivateIP 判断 IP 是否为私网/环回/链路本地/元数据地址。
func isPrivateIP(ip net.IP) bool {
	// IPv4 私网/环回/链路本地。
	if ip4 := ip.To4(); ip4 != nil {
		// 127.x.x.x（环回）
		if ip4[0] == 127 {
			return true
		}
		// 10.x.x.x（A 类私网）
		if ip4[0] == 10 {
			return true
		}
		// 172.16-31.x.x（B 类私网）
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// 192.168.x.x（C 类私网）
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		// 169.254.x.x（链路本地 + 云元数据 169.254.169.254）
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		// 0.0.0.0/8（本网/未指定，task 248 SSRF 防护：原仅拒 0.0.0.0 单地址，
		// 增强为拒整个 0.0.0.0/8 网段，防 0.x.x.x 绕过 SSRF 校验访问本机网络栈）
		if ip4[0] == 0 {
			return true
		}
		return false
	}
	// IPv6：拒绝 loopback (::1) 和 link-local (fe80::/10)。
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// IPv6 ULA (fc00::/7) 私网地址。
	if len(ip) == 16 {
		if (ip[0] & 0xfe) == 0xfc {
			return true
		}
	}
	return false
}

// ============================================================================
// M1-2 API 限流（控制面熔断）
// ============================================================================

// rateLimiter 按 IP 令牌桶限流器。
// 每个 IP 维护一个独立的令牌桶，按 ratePerSec 速率补充令牌，桶容量=ratePerSec（允许 1s 突发）。
// 超过桶容量时拒绝请求（返回 429）。sweepInterval 周期清理空闲 IP 条目防内存泄漏。
type rateLimiter struct {
	mu            sync.Mutex
	buckets       map[string]*tokenBucket
	ratePerSec    int
	sweepInterval time.Duration
}

// tokenBucket 令牌桶。lastRefill 为上次补充时刻，tokens 为当前令牌数（浮点支持分数补充）。
type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
}

// newRateLimiter 构造限流器。ratePerSec 为每秒允许的请求数；sweepInterval 为清理周期。
func newRateLimiter(ratePerSec int, sweepInterval time.Duration) *rateLimiter {
	rl := &rateLimiter{
		buckets:       make(map[string]*tokenBucket),
		ratePerSec:    ratePerSec,
		sweepInterval: sweepInterval,
	}
	go rl.sweepLoop()
	return rl
}

// allow 检查 IP 是否允许放行。true=放行并消耗一个令牌；false=拒绝（429）。
func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b, ok := rl.buckets[ip]
	if !ok {
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

// sweepLoop 周期清理超过 sweepInterval 未访问的 IP 条目，防内存泄漏。
func (rl *rateLimiter) sweepLoop() {
	ticker := time.NewTicker(rl.sweepInterval)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, b := range rl.buckets {
			if now.Sub(b.lastRefill) > rl.sweepInterval {
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// rateLimitMiddleware API 限流中间件。按客户端 IP 令牌桶限流，超阈值返回 429。
// rateLimiter=nil 时透传（禁用限流，向后兼容）。
// 健康检查端点（/healthz, /readyz）不限流，避免 K8s 探针被限流误杀。
func (s *Server) rateLimitMiddleware(h http.Handler) http.Handler {
	if s.rateLimiter == nil {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 健康检查端点不限流，避免 K8s liveness/readiness 探针被限流误杀 Pod。
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			h.ServeHTTP(w, r)
			return
		}
		ip := clientIP(r, s.cfg.TrustProxy)
		if !s.rateLimiter.allow(ip) {
			w.Header().Set("Retry-After", "1")
			jsonError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// ============================================================================
// M1-4 分布式可观测性：审计日志关联 trace_id
// ============================================================================

// audit 是审计日志写入 helper：从 ctx 提取 OTel trace_id 注入 AuditEvent.TraceID，
// 然后转发到 store.Audit。M1-4 分布式可观测性：使审计日志与链路追踪/日志/SSE 事件关联。
//
// 用法（替代直接 s.store.Audit）：
//
//	s.audit(r.Context(), &proto.AuditEvent{TenantID: ..., Action: ..., ...})
//
// ctx 无有效 span 时 TraceID 为空串（向后兼容，不破坏无 OTel 场景）。
