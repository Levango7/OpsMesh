// Package ratelimit provides global, per-IP, and per-user rate limiting
// for OpsMesh services. It supports both HTTP middleware and gRPC interceptor
// patterns, with automatic cleanup of stale entries.
//
// Configuration via environment variables:
//   - GLOBAL_RPS - Global requests per second (default: 1000)
//   - IP_RPS     - Per-IP requests per second (default: 30)
//   - USER_RPS   - Per-user requests per second (default: 60)
package ratelimit

import (
	"context"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// ==================== Global Limiter ====================

// NewGlobalLimiter creates a global token bucket rate limiter.
// rps defines the sustained requests per second, burst defines
// the maximum burst size.
func NewGlobalLimiter(rps int, burst int) *rate.Limiter {
	return rate.NewLimiter(rate.Limit(rps), burst)
}

// ==================== Per-IP Limiter ====================

// IPLimiter provides per-IP rate limiting with automatic cleanup.
type IPLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rateLimiterEntry
	rps      rate.Limit
	burst    int
	ttl      time.Duration
}

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewIPLimiter creates a per-IP rate limiter with automatic cleanup.
// rps defines sustained requests per second per IP, burst defines
// the maximum burst size per IP.
func NewIPLimiter(rps int, burst int) *IPLimiter {
	il := &IPLimiter{
		limiters: make(map[string]*rateLimiterEntry),
		rps:      rate.Limit(rps),
		burst:    burst,
		ttl:      5 * time.Minute,
	}
	go il.cleanupLoop()
	return il
}

// Allow checks whether a request from the given IP is allowed.
func (il *IPLimiter) Allow(ip string) bool {
	il.mu.Lock()
	defer il.mu.Unlock()

	entry, ok := il.limiters[ip]
	if !ok {
		entry = &rateLimiterEntry{
			limiter: rate.NewLimiter(il.rps, il.burst),
		}
		il.limiters[ip] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter.Allow()
}

// cleanupLoop periodically removes stale IP entries every 5 minutes.
func (il *IPLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		il.cleanup()
	}
}

func (il *IPLimiter) cleanup() {
	il.mu.Lock()
	defer il.mu.Unlock()

	now := time.Now()
	for ip, entry := range il.limiters {
		if now.Sub(entry.lastSeen) > il.ttl {
			delete(il.limiters, ip)
		}
	}
}

// ==================== Per-User Limiter ====================

// UserLimiter provides per-user rate limiting with automatic cleanup.
type UserLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rateLimiterEntry
	rps      rate.Limit
	burst    int
	ttl      time.Duration
}

// NewUserLimiter creates a per-user rate limiter with automatic cleanup.
// rps defines sustained requests per second per user, burst defines
// the maximum burst size per user.
func NewUserLimiter(rps int, burst int) *UserLimiter {
	ul := &UserLimiter{
		limiters: make(map[string]*rateLimiterEntry),
		rps:      rate.Limit(rps),
		burst:    burst,
		ttl:      5 * time.Minute,
	}
	go ul.cleanupLoop()
	return ul
}

// Allow checks whether a request from the given user is allowed.
func (ul *UserLimiter) Allow(userID string) bool {
	ul.mu.Lock()
	defer ul.mu.Unlock()

	entry, ok := ul.limiters[userID]
	if !ok {
		entry = &rateLimiterEntry{
			limiter: rate.NewLimiter(ul.rps, ul.burst),
		}
		ul.limiters[userID] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter.Allow()
}

// cleanupLoop periodically removes stale user entries every 5 minutes.
func (ul *UserLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		ul.cleanup()
	}
}

func (ul *UserLimiter) cleanup() {
	ul.mu.Lock()
	defer ul.mu.Unlock()

	now := time.Now()
	for userID, entry := range ul.limiters {
		if now.Sub(entry.lastSeen) > ul.ttl {
			delete(ul.limiters, userID)
		}
	}
}

// ==================== HTTP Middleware ====================

// Middleware returns HTTP middleware that applies global, per-IP, and per-user
// rate limiting. It reads configuration from environment variables:
//   - GLOBAL_RPS (default: 1000)
//   - IP_RPS (default: 30)
//   - USER_RPS (default: 60)
func Middleware() func(http.Handler) http.Handler {
	globalRPS := getEnvInt("GLOBAL_RPS", 1000)
	ipRPS := getEnvInt("IP_RPS", 30)
	userRPS := getEnvInt("USER_RPS", 60)

	globalLimiter := NewGlobalLimiter(globalRPS, globalRPS*2)
	ipLimiter := NewIPLimiter(ipRPS, ipRPS*2)
	userLimiter := NewUserLimiter(userRPS, userRPS*2)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Global rate limit
			if !globalLimiter.Allow() {
				w.Header().Set("Retry-After", "1")
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(globalRPS))
				http.Error(w, "全局速率限制，请稍后重试", http.StatusTooManyRequests)
				return
			}

			// Per-IP rate limit
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if !ipLimiter.Allow(ip) {
				w.Header().Set("Retry-After", "1")
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(ipRPS))
				http.Error(w, "IP 速率限制，请稍后重试", http.StatusTooManyRequests)
				return
			}

			// Per-user rate limit
			userID := r.Header.Get("X-User-ID")
			if userID == "" {
				userID = "anonymous"
			}
			if !userLimiter.Allow(userID) {
				w.Header().Set("Retry-After", "1")
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(userRPS))
				http.Error(w, "用户速率限制，请稍后重试", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ==================== gRPC Interceptor ====================

// GRPCInterceptor returns a gRPC unary server interceptor that applies
// global, per-IP, and per-user rate limiting.
func GRPCInterceptor() grpc.UnaryServerInterceptor {
	globalRPS := getEnvInt("GLOBAL_RPS", 1000)
	ipRPS := getEnvInt("IP_RPS", 30)
	userRPS := getEnvInt("USER_RPS", 60)

	globalLimiter := NewGlobalLimiter(globalRPS, globalRPS*2)
	ipLimiter := NewIPLimiter(ipRPS, ipRPS*2)
	userLimiter := NewUserLimiter(userRPS, userRPS*2)

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Global rate limit
		if !globalLimiter.Allow() {
			return nil, status.Errorf(codes.ResourceExhausted, "全局速率限制，请稍后重试")
		}

		// Per-IP rate limit (from peer info)
		if p, ok := peer.FromContext(ctx); ok {
			if !ipLimiter.Allow(p.Addr.String()) {
				return nil, status.Errorf(codes.ResourceExhausted, "IP 速率限制，请稍后重试")
			}
		}

		// Per-user rate limit (from metadata)
		userID := userFromMetadata(ctx)
		if !userLimiter.Allow(userID) {
			return nil, status.Errorf(codes.ResourceExhausted, "用户速率限制，请稍后重试")
		}

		return handler(ctx, req)
	}
}

// ==================== Helpers ====================

func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return n
}

// userFromMetadata extracts user ID from gRPC incoming metadata.
func userFromMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "anonymous"
	}
	vals := md.Get("x-user-id")
	if len(vals) > 0 && vals[0] != "" {
		return vals[0]
	}
	return "anonymous"
}
