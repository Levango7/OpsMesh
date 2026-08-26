// gateway_test.go 测试 Phase 5 API 网关引擎（gateway.go）。
//
// 覆盖范围：
//   - NewRateLimiter：构造 + nil 安全
//   - RateLimiter.Allow：不限流（ratePerSec<=0）、限流（ratePerSec>0）、令牌补充
//   - MatchRoute：前缀匹配、方法匹配、Enabled 过滤、空规则、多规则顺序
package extension

import (
	"testing"
	"time"
)

// =============================================================================
// NewRateLimiter + RateLimiter.Allow
// ============================================================================

// TestNewRateLimiter_Unlimited 验证 ratePerSec<=0 时 Allow 恒返回 true。
func TestNewRateLimiter_Unlimited(t *testing.T) {
	rl := NewRateLimiter(0)
	for i := 0; i < 100; i++ {
		if !rl.Allow() {
			t.Fatalf("Allow()=false, want true (unlimited); i=%d", i)
		}
	}
	// nil 也应不限流
	var nilRL *RateLimiter
	if !nilRL.Allow() {
		t.Fatal("nil.Allow()=false, want true")
	}
}

// TestNewRateLimiter_Limited 验证 ratePerSec>0 时 Allow 在桶容量内返回 true。
func TestNewRateLimiter_Limited(t *testing.T) {
	rl := NewRateLimiter(10) // 桶容量 10
	// 前 10 次应全部允许（桶初始满）
	for i := 0; i < 10; i++ {
		if !rl.Allow() {
			t.Fatalf("Allow()=false, want true (bucket has tokens); i=%d", i)
		}
	}
	// 第 11 次应拒绝（桶空）
	if rl.Allow() {
		t.Fatal("Allow()=true, want false (bucket empty)")
	}
}

// TestNewRateLimiter_Refill 验证令牌补充：等待后 Allow 应再次返回 true。
func TestNewRateLimiter_Refill(t *testing.T) {
	rl := NewRateLimiter(100) // 桶容量 100，ratePerSec=100
	// 耗尽桶
	for i := 0; i < 100; i++ {
		rl.Allow()
	}
	if rl.Allow() {
		t.Fatal("Allow()=true after drain, want false")
	}
	// 等待 50ms，应补充约 5 个令牌（100/s * 0.05s = 5）
	time.Sleep(60 * time.Millisecond)
	// 至少应允许 1 次
	if !rl.Allow() {
		t.Fatal("Allow()=false after refill, want true")
	}
}

// =============================================================================
// MatchRoute
// ============================================================================

// TestMatchRoute_PrefixMatch 验证路径前缀匹配。
func TestMatchRoute_PrefixMatch(t *testing.T) {
	rules := []*RouteRule{
		{ID: "r1", PathPrefix: "/api/v1/", Enabled: true},
		{ID: "r2", PathPrefix: "/api/v2/", Enabled: true},
	}
	got := MatchRoute(rules, "/api/v1/foo", "GET")
	if got == nil || got.ID != "r1" {
		t.Fatalf("MatchRoute=%+v, want r1", got)
	}
}

// TestMatchRoute_MethodMatch 验证方法匹配。
func TestMatchRoute_MethodMatch(t *testing.T) {
	rules := []*RouteRule{
		{ID: "r1", PathPrefix: "/api/", Methods: []string{"GET", "POST"}, Enabled: true},
	}
	// GET 命中
	if got := MatchRoute(rules, "/api/foo", "GET"); got == nil || got.ID != "r1" {
		t.Fatalf("MatchRoute GET=%+v, want r1", got)
	}
	// PUT 不命中（不在 Methods 列表）
	if got := MatchRoute(rules, "/api/foo", "PUT"); got != nil {
		t.Fatalf("MatchRoute PUT=%+v, want nil", got)
	}
}

// TestMatchRoute_EmptyMethods 验证空 Methods 视为全部允许。
func TestMatchRoute_EmptyMethods(t *testing.T) {
	rules := []*RouteRule{
		{ID: "r1", PathPrefix: "/api/", Enabled: true},
	}
	for _, m := range []string{"GET", "POST", "PUT", "DELETE", "PATCH"} {
		if got := MatchRoute(rules, "/api/foo", m); got == nil || got.ID != "r1" {
			t.Fatalf("MatchRoute %s=%+v, want r1", m, got)
		}
	}
}

// TestMatchRoute_Disabled 验证 Enabled=false 的规则被跳过。
func TestMatchRoute_Disabled(t *testing.T) {
	rules := []*RouteRule{
		{ID: "r1", PathPrefix: "/api/", Enabled: false},
		{ID: "r2", PathPrefix: "/api/", Enabled: true},
	}
	got := MatchRoute(rules, "/api/foo", "GET")
	if got == nil || got.ID != "r2" {
		t.Fatalf("MatchRoute=%+v, want r2 (skip disabled r1)", got)
	}
}

// TestMatchRoute_EmptyPrefix 验证空前缀匹配任意路径。
func TestMatchRoute_EmptyPrefix(t *testing.T) {
	rules := []*RouteRule{
		{ID: "r1", PathPrefix: "", Enabled: true},
	}
	for _, p := range []string{"/api/foo", "/anything", "/"} {
		if got := MatchRoute(rules, p, "GET"); got == nil || got.ID != "r1" {
			t.Fatalf("MatchRoute %s=%+v, want r1", p, got)
		}
	}
}

// TestMatchRoute_NoMatch 验证无命中返回 nil。
func TestMatchRoute_NoMatch(t *testing.T) {
	rules := []*RouteRule{
		{ID: "r1", PathPrefix: "/api/v1/", Enabled: true},
	}
	if got := MatchRoute(rules, "/other/foo", "GET"); got != nil {
		t.Fatalf("MatchRoute=%+v, want nil", got)
	}
}

// TestMatchRoute_EmptyRules 验证空规则列表返回 nil。
func TestMatchRoute_EmptyRules(t *testing.T) {
	if got := MatchRoute(nil, "/api/foo", "GET"); got != nil {
		t.Fatalf("MatchRoute(nil)=%+v, want nil", got)
	}
	if got := MatchRoute([]*RouteRule{}, "/api/foo", "GET"); got != nil {
		t.Fatalf("MatchRoute([])=%+v, want nil", got)
	}
}

// TestMatchRoute_NilRule 验证 nil 规则被跳过。
func TestMatchRoute_NilRule(t *testing.T) {
	rules := []*RouteRule{
		nil,
		{ID: "r1", PathPrefix: "/api/", Enabled: true},
	}
	got := MatchRoute(rules, "/api/foo", "GET")
	if got == nil || got.ID != "r1" {
		t.Fatalf("MatchRoute=%+v, want r1 (skip nil)", got)
	}
}

// TestMatchRoute_FirstMatch 验证多规则按顺序返回首条命中。
func TestMatchRoute_FirstMatch(t *testing.T) {
	rules := []*RouteRule{
		{ID: "r1", PathPrefix: "/api/", Enabled: true},
		{ID: "r2", PathPrefix: "/api/v1/", Enabled: true},
	}
	// /api/v1/foo 同时匹配 r1 和 r2，应返回 r1（首条）
	got := MatchRoute(rules, "/api/v1/foo", "GET")
	if got == nil || got.ID != "r1" {
		t.Fatalf("MatchRoute=%+v, want r1 (first match)", got)
	}
}
