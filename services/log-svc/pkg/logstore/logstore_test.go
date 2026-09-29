package logstore

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/pkg/metrics"
)

// readOnlyBackends 列出"只读"后端（日志须由采集器直推，OpsMesh 侧不写入）。
func readOnlyBackends() []struct {
	name string
	ls   LogStore
} {
	return []struct {
		name string
		ls   LogStore
	}{
		{"elasticsearch", NewESStore("http://es:9200", "opsmesh-logs")},
		{"loki", NewLokiStore("http://loki:3100")},
	}
}

// TestAppendReturnsErrAppendUnsupported 验证只读后端的 Append 显式回哨兵错误。
// 旧实现 return nil ⇒ service.AppendLog 仍回 entryToProto(entry)（ID=0 的伪成功），
// 客户端以为写进去了，实际一条都没落。
func TestAppendReturnsErrAppendUnsupported(t *testing.T) {
	entries := []struct {
		name  string
		entry *Entry
	}{
		{"正常条目", &Entry{TenantID: "t1", Message: "boom"}},
		{"nil 条目", nil},
	}
	for _, be := range readOnlyBackends() {
		for _, c := range entries {
			t.Run(be.name+"/"+c.name, func(t *testing.T) {
				err := be.ls.Append(context.Background(), c.entry)
				if err == nil {
					t.Fatal("Append 必须回错误（return nil 即为静默丢失）")
				}
				if !errors.Is(err, ErrAppendUnsupported) {
					t.Fatalf("want ErrAppendUnsupported, got %v", err)
				}
			})
		}
	}
}

// TestSupportsAppend 验证写入能力矩阵：只读后端为 false，memory/sql 为 true，
// 未声明能力的实现按 true 处理（不给新后端凭空加限制）。
func TestSupportsAppend(t *testing.T) {
	cases := []struct {
		name string
		ls   LogStore
		want bool
	}{
		{"memory", NewMemory(0), true},
		{"memory-with-index", NewMemoryWithIndex(0), true},
		{"sql", &SQLLogStore{}, true},
		{"elasticsearch", NewESStore("http://es:9200", "opsmesh-logs"), false},
		{"loki", NewLokiStore("http://loki:3100"), false},
		{"unknown-impl-defaults-to-supported", opaqueStore{inner: NewMemory(0)}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SupportsAppend(c.ls); got != c.want {
				t.Fatalf("SupportsAppend(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

// TestAppendUnsupportedProbe 验证只读后端自报只读——SupportsAppend 的判定全靠该探针，
// 探针漏实现时调用方会继续逐条 Append 并吞掉错误（回到静默丢失）。
func TestAppendUnsupportedProbe(t *testing.T) {
	for _, be := range readOnlyBackends() {
		t.Run(be.name, func(t *testing.T) {
			a, ok := be.ls.(appendUnsupported)
			if !ok {
				t.Fatalf("%T 须实现 AppendUnsupported，否则能力探测失效", be.ls)
			}
			if !a.AppendUnsupported() {
				t.Fatalf("%T.AppendUnsupported() = false, want true", be.ls)
			}
		})
	}
}

// TestErrAppendUnsupportedIsActionable 验证哨兵文案自带出路（采集器 + 可写后端）：
// 它会经 codes.Unimplemented 成为客户端看到的唯一提示。
func TestErrAppendUnsupportedIsActionable(t *testing.T) {
	msg := ErrAppendUnsupported.Error()
	for _, want := range []string{"filebeat", "promtail", "memory", "sql"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误文案应含 %q（给调用方可执行指引），got %q", want, msg)
		}
	}
}

// opaqueStore 是不声明写入能力的后端样例（覆盖 SupportsAppend 的默认放行分支）。
type opaqueStore struct{ inner LogStore }

func (o opaqueStore) Append(ctx context.Context, e *Entry) error { return o.inner.Append(ctx, e) }
func (o opaqueStore) Query(ctx context.Context, q Query) ([]Entry, error) {
	return o.inner.Query(ctx, q)
}
func (o opaqueStore) Close() error { return o.inner.Close() }

// TestMemoryRingDroppedCounted —— 容量淘汰必须成为可告警的事实。
// 淘汰前这里什么都不记：「这条日志被挤掉了」和「这条从没写过」在检索面上完全同形。
func TestMemoryRingDroppedCounted(t *testing.T) {
	metrics.Init("log-svc-test")
	m := NewMemory(3)
	ctx := context.Background()

	// 未超容量：一次都不该计。
	for i := 0; i < 3; i++ {
		if err := m.Append(ctx, &Entry{TenantID: "t1", Message: "x"}); err != nil {
			t.Fatalf("Append 失败: %v", err)
		}
	}
	if got := m.Dropped(); got != 0 {
		t.Fatalf("未超容量却计了淘汰 %d 条", got)
	}

	// 超容量 4 条 ⇒ 淘汰 4 条（每次只挤掉最旧一条）。
	for i := 0; i < 4; i++ {
		if err := m.Append(ctx, &Entry{TenantID: "t1", Message: "y"}); err != nil {
			t.Fatalf("Append 失败: %v", err)
		}
	}
	if got := m.Dropped(); got != 4 {
		t.Fatalf("淘汰计数 = %d, 期望 4", got)
	}

	// 只保留最新 cap 条：被淘汰的必须是旧的那批，否则"查不到最近日志"会变成常态。
	got, err := m.Query(ctx, Query{TenantID: "t1", Limit: 10})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("缓冲保留 %d 条, 期望 3", len(got))
	}

	// 同一个数字要能从 /metrics 读到（counter 家族；写成 gauge 就成了恒值、increase() 无意义）。
	rec := httptest.NewRecorder()
	metrics.GetHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `business_metrics_total{name="log_memory_dropped"} 4`) {
		t.Fatalf("淘汰计数未进 counter 家族\n---%s", body)
	}
	if strings.Contains(body, `business_metrics{name="log_memory_dropped"}`) {
		t.Fatalf("淘汰计数被写进 gauge 家族（应为 counter）\n---%s", body)
	}
}
