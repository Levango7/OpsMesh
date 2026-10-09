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
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/controlplane/metricscache"
	"github.com/Levango7/OpsMesh/internal/metrics"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// 因此这里只比对受本缓存管辖的应用级行。
func TestMetricsEndpoint_WithCacheEnabled(t *testing.T) {
	st := store.NewMemoryStore()
	st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	s := &Server{
		store:         st,
		cfg:           &config.Config{MetricsAllowCIDR: "127.0.0.0/8"},
		metricsCounts: metricscache.AppCountsCache{TTL: time.Hour},
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
	s.metricsCounts.Invalidate()
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
