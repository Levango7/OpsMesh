// limiter_test.go 限流器的内部态用例（TD-87 批 2 第二批：自父包 server_security_extra_test.go /
// coverage_extra_test.go 随类型迁入——这些用例直接构造 buckets/tokenBucket/maxBuckets，
// 跨包无法访问，按惯例与类型同住）。
package ratelimit

import (
	"fmt"
	"testing"
	"time"
)

func TestRateLimiter_BucketCapBounded(t *testing.T) {
	// 手工构造（不启动 sweepLoop，避免与 -race 下的字段写入竞争）。
	rl := &Limiter{buckets: map[string]*tokenBucket{}, ratePerSec: 1, sweepInterval: time.Hour, maxBuckets: 3}
	for i := 1; i <= 3; i++ {
		if !rl.Allow(fmt.Sprintf("10.0.0.%d", i)) {
			t.Fatalf("第 %d 个 IP 首次请求应放行", i)
		}
	}
	if len(rl.buckets) != 3 {
		t.Fatalf("桶数 = %d, want 3", len(rl.buckets))
	}
	// 第 4 个 IP：桶已满 → 放行但不建桶（内存不再增长）。
	if !rl.Allow("10.0.0.99") {
		t.Fatal("桶满时新 IP 应放行（不误杀），只是不跟踪")
	}
	if len(rl.buckets) != 3 {
		t.Fatalf("桶数应保持 3（有界），实际 %d", len(rl.buckets))
	}
	if rl.untracked != 1 {
		t.Fatalf("untracked = %d, want 1", rl.untracked)
	}
	// 已跟踪 IP 的超速请求仍被拒绝（限流器对在途攻击依然有效）。
	//
	// 判据必须与经过时间无关：ratePerSec=1 时，若循环本身耗时 ≥1s（全量测试并跑、
	// CI 负载高时实测发生过），桶会恰好补回一个令牌、断言随机翻红。故这里把该桶
	// 显式置为「零令牌 + 刚刚补充过」——等价于「同一秒内连发两次」的确定性形态。
	rl.buckets["10.0.0.1"].tokens = 0
	rl.buckets["10.0.0.1"].lastRefill = time.Now()
	if rl.Allow("10.0.0.1") {
		t.Fatal("已跟踪 IP 零令牌时应被拒绝（429）")
	}
}

// TestRateLimiter_BucketCapEvictsIdleFirst 验证桶满时优先清理空闲桶，
// 使活跃期后的新 IP 重新纳入跟踪（而非永久不跟踪）。
func TestRateLimiter_BucketCapEvictsIdleFirst(t *testing.T) {
	rl := &Limiter{buckets: map[string]*tokenBucket{}, ratePerSec: 1, sweepInterval: time.Millisecond, maxBuckets: 2}
	rl.Allow("10.0.0.1")
	rl.Allow("10.0.0.2")
	time.Sleep(5 * time.Millisecond) // 两个桶均超过 sweepInterval → 视为空闲
	if !rl.Allow("10.0.0.3") {
		t.Fatal("空闲桶清理后应放行新 IP")
	}
	if _, ok := rl.buckets["10.0.0.3"]; !ok {
		t.Fatal("空闲桶应先被清理，新 IP 应被跟踪")
	}
	if rl.untracked != 0 {
		t.Fatalf("清理出空间后不应出现未跟踪请求，untracked = %d", rl.untracked)
	}
	if len(rl.buckets) != 1 {
		t.Fatalf("桶数 = %d, want 1（两个空闲桶被清理）", len(rl.buckets))
	}
}

func TestRateLimiterSweepLoop(t *testing.T) {
	rl := &Limiter{
		buckets:       make(map[string]*tokenBucket),
		sweepInterval: 50 * time.Millisecond,
	}
	rl.buckets["1.2.3.4"] = &tokenBucket{tokens: 5, lastRefill: time.Now().Add(-1 * time.Hour)}
	done := make(chan struct{})
	go func() {
		rl.sweepLoop()
		close(done)
	}()
	// 等待 sweepInterval 过后 sweep 清理过期 bucket
	time.Sleep(150 * time.Millisecond)
	rl.mu.Lock()
	_, stillExists := rl.buckets["1.2.3.4"]
	rl.mu.Unlock()
	if stillExists {
		t.Fatal("sweepLoop did not clean up expired bucket")
	}
}
