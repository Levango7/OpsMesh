// cache_test.go metricscache 包的缓存语义用例（TD-87 批 1：自 controlplane/metrics_cache_test.go 拆出；
// 端到端接线用例 TestMetricsEndpoint_WithCacheEnabled 留在父包）。
package metricscache

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAppCountsCache_ZeroTTLAlwaysComputes(t *testing.T) {
	var calls atomic.Int32
	c := &AppCountsCache{} // 零值：ttl=0 → 不缓存
	compute := func() AppCounts {
		calls.Add(1)
		return AppCounts{Devices: int(calls.Load())}
	}
	for i := 0; i < 5; i++ {
		if got := c.Resolve(compute); got.Devices != i+1 {
			t.Fatalf("第 %d 次应重新计算，得到 %+v", i+1, got)
		}
	}
	if calls.Load() != 5 {
		t.Fatalf("ttl=0 时 compute 调用数=%d, want 5（缓存未生效才算正确）", calls.Load())
	}
}

func TestAppCountsCache_CoalescesWithinTTL(t *testing.T) {
	var calls atomic.Int32
	c := &AppCountsCache{TTL: time.Hour}
	compute := func() AppCounts {
		calls.Add(1)
		return AppCounts{Devices: 7, Tasks: 3, Agents: 2}
	}
	first := c.Resolve(compute)
	for i := 0; i < 9; i++ {
		if got := c.Resolve(compute); got != first {
			t.Fatalf("TTL 内应复用缓存：got %+v want %+v", got, first)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("10 次 resolve 只应计算 1 次，实际 %d 次", calls.Load())
	}
}

func TestAppCountsCache_ExpiresAfterTTL(t *testing.T) {
	var calls atomic.Int32
	c := &AppCountsCache{TTL: 20 * time.Millisecond}
	compute := func() AppCounts {
		return AppCounts{Devices: int(calls.Add(1))}
	}
	if got := c.Resolve(compute); got.Devices != 1 {
		t.Fatalf("首次=%+v", got)
	}
	if got := c.Resolve(compute); got.Devices != 1 {
		t.Fatalf("TTL 内应仍是 1，得到 %+v", got)
	}
	time.Sleep(40 * time.Millisecond)
	if got := c.Resolve(compute); got.Devices != 2 {
		t.Fatalf("TTL 过期后应重算为 2，得到 %+v", got)
	}
}

func TestAppCountsCache_InvalidateAndNilReceiver(t *testing.T) {
	c := &AppCountsCache{TTL: time.Hour}
	n := 0
	compute := func() AppCounts {
		n++
		return AppCounts{Devices: n}
	}
	c.Resolve(compute)
	c.Invalidate()
	if got := c.Resolve(compute); got.Devices != 2 {
		t.Fatalf("invalidate 后应重算，得到 %+v", got)
	}
	// nil 接收者：ttl 无法判断，必须走实算而不是 panic。
	var nilCache *AppCountsCache
	if got := nilCache.Resolve(func() AppCounts { return AppCounts{Devices: 42} }); got.Devices != 42 {
		t.Fatalf("nil 接收者应实算，得到 %+v", got)
	}
	nilCache.Invalidate() // 不得 panic
}

// TestAppCountsCache_ConcurrentResolve 并发抓取下不得重复计算、不得竞争
// （CI 的 -race 批次覆盖本用例）。
func TestAppCountsCache_ConcurrentResolve(t *testing.T) {
	c := &AppCountsCache{TTL: time.Hour}
	var calls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := c.Resolve(func() AppCounts {
				calls.Add(1)
				time.Sleep(time.Millisecond)
				return AppCounts{Devices: 1}
			})
			if got.Devices != 1 {
				t.Errorf("并发下计数被改写：%+v", got)
			}
		}()
	}
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("并发 16 路只应计算 1 次，实际 %d 次", n)
	}
}

// TestMetricsEndpoint_WithCacheEnabled 端到端验证接线：开缓存后连续两次抓取的
// **应用级计数**一致，且计数仍来自真实 store（不是被缓存写死的假值）。
//
// 2026-09-26 起 8080 与 9091 共用一份注册表渲染，响应体里多了 go_*/process_* 这类
// **每次抓取本就会变**的运行期读数；再拿"整份 body 逐字相等"当缓存命中的判据，
// 会从"验证缓存"退化成"验证 goroutine 数没变"（偶发失败 + 判据错位）。
// 因此这里只比对受本缓存管辖的应用级行。
