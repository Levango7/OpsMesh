// gateway.go 实现 API 网关引擎（Phase 5 扩展能力）。
//
// 提供路由规则匹配 + 令牌桶限流 + 网关统计聚合能力，供控制面 gateway handler 复用。
//
// 设计要点：
//   - RouteRule 描述一条路由规则（路径前缀 + 后端 + 方法 + 限流）；
//   - RateLimiter 为令牌桶实现，按 ratePerSec 补充令牌，Allow() 抢占一个令牌；
//   - MatchRoute 按 PathPrefix 前缀匹配 + Methods 包含校验，返回首条命中规则；
//   - GatewayStats 聚合网关请求数/错误数/平均延迟/活跃路由数，由控制面统计端点查询。
package extension

import (
	"strings"
	"sync"
	"time"
)

// RouteRule API 网关路由规则。
//
// 按 (TenantID, ID) 唯一标识；PathPrefix 为路径前缀匹配（如 /api/v1/）；
// TargetBackend 为后端服务地址（如 http://backend:8080）；Methods 为允许的 HTTP 方法列表
// （空表示全部允许）；RateLimitPerSec 为每秒限流（0 表示不限流）；Enabled 控制规则是否生效。
type RouteRule struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenantID"`
	Name            string    `json:"name"`
	PathPrefix      string    `json:"pathPrefix"`
	TargetBackend   string    `json:"targetBackend"`
	Methods         []string  `json:"methods"`
	RateLimitPerSec int       `json:"rateLimitPerSec"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// GatewayStats API 网关统计聚合（由控制面统计端点查询返回）。
type GatewayStats struct {
	TotalRequests int64   `json:"totalRequests"`
	TotalErrors   int64   `json:"totalErrors"`
	AvgLatencyMs  float64 `json:"avgLatencyMs"`
	ActiveRoutes  int     `json:"activeRoutes"`
}

// RateLimiter 令牌桶限流器。
//
// 按 ratePerSec 速率补充令牌，桶容量等于 ratePerSec（1 秒突发量）；
// Allow() 抢占一个令牌，命中返回 true，桶空返回 false。
// 并发安全（mu 保护 tokens + lastRefill）。
type RateLimiter struct {
	ratePerSec int
	tokens     float64
	lastRefill time.Time
	mu         sync.Mutex
}

// NewRateLimiter 构造一个令牌桶限流器。
// ratePerSec <= 0 时视为不限流（Allow 恒返回 true）。
func NewRateLimiter(ratePerSec int) *RateLimiter {
	return &RateLimiter{
		ratePerSec: ratePerSec,
		tokens:     float64(ratePerSec),
		lastRefill: time.Now(),
	}
}

// Allow 抢占一个令牌。
// ratePerSec<=0 时恒返回 true（不限流）；桶空时返回 false。
// 内部按时间差补充令牌（最多到桶容量）。
func (rl *RateLimiter) Allow() bool {
	if rl == nil || rl.ratePerSec <= 0 {
		return true
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	rl.tokens += elapsed * float64(rl.ratePerSec)
	if rl.tokens > float64(rl.ratePerSec) {
		rl.tokens = float64(rl.ratePerSec)
	}
	rl.lastRefill = now
	if rl.tokens < 1 {
		return false
	}
	rl.tokens -= 1
	return true
}

// MatchRoute 按 (path, method) 匹配首条命中规则。
//
// 匹配条件：
//   - rule.Enabled 为 true；
//   - path 以 rule.PathPrefix 为前缀（空前缀视为匹配任意路径）；
//   - rule.Methods 为空或包含 method（大小写敏感，方法名大写）。
//
// 返回首条命中规则（按 rules 顺序）；无命中返回 nil。
func MatchRoute(rules []*RouteRule, path, method string) *RouteRule {
	for _, r := range rules {
		if r == nil || !r.Enabled {
			continue
		}
		// 路径前缀匹配：空前缀视为匹配任意。
		if r.PathPrefix != "" && !strings.HasPrefix(path, r.PathPrefix) {
			continue
		}
		// 方法匹配：空 Methods 视为全部允许。
		if len(r.Methods) > 0 {
			hit := false
			for _, m := range r.Methods {
				if m == method {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		return r
	}
	return nil
}
