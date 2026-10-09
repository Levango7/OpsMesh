// server_security.go — SSRF 防护 + IP 限流（令牌桶）
package controlplane

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/controlplane/ratelimit"
	"github.com/Levango7/OpsMesh/internal/egress"
)

// rateLimiter 限流器别名 + 构造薄包装（TD-87 批 2 第二批：实现已迁 internal/controlplane/ratelimit）。
type rateLimiter = ratelimit.Limiter

func newRateLimiter(ratePerSec int, sweepInterval time.Duration) *rateLimiter {
	return ratelimit.New(ratePerSec, sweepInterval)
}

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
// isPrivateIP 私网/环回/链路本地/未指定网段判定（SSRF 防线）。
// TD-87 批 2 第二批：与 internal/egress 的实现**逐行等价**（都含 0.0.0.0/8 增强与 IPv6 ULA），
// 原先两份副本各自演进的风险就此消除——本函数改为薄委托，判定清单只有 egress 一处。
func isPrivateIP(ip net.IP) bool { return egress.IsPrivateIP(ip) }

// ============================================================================
// API 限流（控制面熔断）
// ============================================================================

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
		if !s.rateLimiter.Allow(ip) {
			w.Header().Set("Retry-After", "1")
			paginate.JSONError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// ============================================================================
// 分布式可观测性：审计日志关联 trace_id
// ============================================================================

// audit 是审计日志写入 helper：从 ctx 提取 OTel trace_id 注入 AuditEvent.TraceID，
// 然后转发到 store.Audit。分布式可观测性：使审计日志与链路追踪/日志/SSE 事件关联。
//
// 用法（替代直接 s.store.Audit）：
//
//	s.audit(r.Context(), &proto.AuditEvent{TenantID: ..., Action: ..., ...})
//
// ctx 无有效 span 时 TraceID 为空串（向后兼容，不破坏无 OTel 场景）。
