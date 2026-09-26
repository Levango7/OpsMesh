// metrics_cache_test.go — /metrics 计数缓存（metrics_cache.go）的单测。
//
// 覆盖两类主张：
//  1. **合并计算**：TTL 内多次抓取只算一次（这才是要解决的问题——每波抓取 5 次全表读）。
//  2. **失效方向保守**：ttl<=0（含测试里直接 &Server{} 构造、不经 New() 的路径）等于完全
//     关闭缓存，行为与引入缓存之前逐字相同；过期后必须重算，不能返回陈旧值。
package controlplane

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/metrics"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

func TestAppCountsCache_ZeroTTLAlwaysComputes(t *testing.T) {
	var calls atomic.Int32
	c := &appCountsCache{} // 零值：ttl=0 → 不缓存
	compute := func() appCounts {
		calls.Add(1)
		return appCounts{devices: int(calls.Load())}
	}
	for i := 0; i < 5; i++ {
		if got := c.resolve(compute); got.devices != i+1 {
			t.Fatalf("第 %d 次应重新计算，得到 %+v", i+1, got)
		}
	}
	if calls.Load() != 5 {
		t.Fatalf("ttl=0 时 compute 调用数=%d, want 5（缓存未生效才算正确）", calls.Load())
	}
}

func TestAppCountsCache_CoalescesWithinTTL(t *testing.T) {
	var calls atomic.Int32
	c := &appCountsCache{ttl: time.Hour}
	compute := func() appCounts {
		calls.Add(1)
		return appCounts{devices: 7, tasks: 3, agents: 2}
	}
	first := c.resolve(compute)
	for i := 0; i < 9; i++ {
		if got := c.resolve(compute); got != first {
			t.Fatalf("TTL 内应复用缓存：got %+v want %+v", got, first)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("10 次 resolve 只应计算 1 次，实际 %d 次", calls.Load())
	}
}

func TestAppCountsCache_ExpiresAfterTTL(t *testing.T) {
	var calls atomic.Int32
	c := &appCountsCache{ttl: 20 * time.Millisecond}
	compute := func() appCounts {
		return appCounts{devices: int(calls.Add(1))}
	}
	if got := c.resolve(compute); got.devices != 1 {
		t.Fatalf("首次=%+v", got)
	}
	if got := c.resolve(compute); got.devices != 1 {
		t.Fatalf("TTL 内应仍是 1，得到 %+v", got)
	}
	time.Sleep(40 * time.Millisecond)
	if got := c.resolve(compute); got.devices != 2 {
		t.Fatalf("TTL 过期后应重算为 2，得到 %+v", got)
	}
}

func TestAppCountsCache_InvalidateAndNilReceiver(t *testing.T) {
	c := &appCountsCache{ttl: time.Hour}
	n := 0
	compute := func() appCounts {
		n++
		return appCounts{devices: n}
	}
	c.resolve(compute)
	c.invalidate()
	if got := c.resolve(compute); got.devices != 2 {
		t.Fatalf("invalidate 后应重算，得到 %+v", got)
	}
	// nil 接收者：ttl 无法判断，必须走实算而不是 panic。
	var nilCache *appCountsCache
	if got := nilCache.resolve(func() appCounts { return appCounts{devices: 42} }); got.devices != 42 {
		t.Fatalf("nil 接收者应实算，得到 %+v", got)
	}
	nilCache.invalidate() // 不得 panic
}

// TestAppCountsCache_ConcurrentResolve 并发抓取下不得重复计算、不得竞争
// （CI 的 -race 批次覆盖本用例）。
func TestAppCountsCache_ConcurrentResolve(t *testing.T) {
	c := &appCountsCache{ttl: time.Hour}
	var calls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := c.resolve(func() appCounts {
				calls.Add(1)
				time.Sleep(time.Millisecond)
				return appCounts{devices: 1}
			})
			if got.devices != 1 {
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
func TestMetricsEndpoint_WithCacheEnabled(t *testing.T) {
	st := store.NewMemoryStore()
	st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	s := &Server{
		store:         st,
		cfg:           &config.Config{MetricsAllowCIDR: "127.0.0.0/8"},
		metricsCounts: appCountsCache{ttl: time.Hour},
		metrics:       metrics.New(),
	}
	scrape := func() string {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.RemoteAddr = "127.0.0.1:5555"
		s.handlePrometheusMetrics(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		return appGaugeLines(w.Body.String())
	}
	a := scrape()
	// 抓取后再写一条数据：TTL 内应看到旧值（证明命中缓存），invalidate 后看到新值。
	//
	// 用"工单"而不是"任务"作新增数据：任务计数在 exposition 里是 counter
	// （opsmesh_tasks_total{status}，只在任务真的完成/失败后才产出），
	// 单纯 CreateTask 不会改变任何受本缓存管辖的读数；工单数则是每次抓取都算的快照值。
	st.CreateTicket("t1", &store.Ticket{Title: "cache-probe", Status: "open"})
	b := scrape()
	if a != b {
		t.Fatalf("TTL 内两次抓取的应用级计数应一致（命中缓存）：\n%s\n---\n%s", a, b)
	}
	s.metricsCounts.invalidate()
	c := scrape()
	if c == a {
		t.Fatalf("invalidate 后应反映新工单数，输出仍与首次相同：\n%s", c)
	}
	if !strings.Contains(c, "opsmesh_tickets_open 1") {
		t.Fatalf("invalidate 后应看到 opsmesh_tickets_open 1，实际：\n%s", c)
	}
}

// appGaugeLines 从 exposition 里挑出受 TTL 缓存管辖的应用级计数行。
func appGaugeLines(body string) string {
	wanted := []string{
		"opsmesh_agents_total", "opsmesh_devices_total", "opsmesh_device_status",
		"opsmesh_alerts_active", "opsmesh_tickets_open",
	}
	var out []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, w := range wanted {
			if strings.HasPrefix(trimmed, w) {
				out = append(out, trimmed)
				break
			}
		}
	}
	slices.Sort(out)
	return strings.Join(out, "\n")
}
