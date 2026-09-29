package logstore

import (
	"context"
	"errors"
	"strings"
	"testing"
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
