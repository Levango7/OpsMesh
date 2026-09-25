package factory

import (
	"errors"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"all empty", []string{"", "", ""}, ""},
		{"first wins", []string{"a", "b", "c"}, "a"},
		{"second wins", []string{"", "b", "c"}, "b"},
		{"third wins", []string{"", "", "c"}, "c"},
		{"single", []string{"only"}, "only"},
		{"nil input", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FirstNonEmpty(tt.in...)
			if got != tt.want {
				t.Fatalf("FirstNonEmpty(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSelectStore_Memory(t *testing.T) {
	cfg := &config.Config{
		Store:     "memory",
		Demo:      true,
		RedisAddr: "",
	}
	st, err := SelectStore(cfg, nil)
	if err != nil {
		t.Fatalf("SelectStore(memory) error: %v", err)
	}
	if _, ok := st.(*store.MemoryStore); !ok {
		t.Fatalf("expected *store.MemoryStore, got %T", st)
	}
}

func TestSelectStore_EmptyDSNFallsBack(t *testing.T) {
	cfg := &config.Config{
		Store:       "mysql",
		MySQLDSN:    "",
		MultiSchema: false,
		Demo:        true,
	}
	st, err := SelectStore(cfg, nil)
	if err != nil {
		t.Fatalf("SelectStore(mysql, empty DSN) error: %v", err)
	}
	if _, ok := st.(*store.MemoryStore); !ok {
		t.Fatalf("expected fallback *store.MemoryStore, got %T", st)
	}
}

func TestSelectSessionStore_InProcess(t *testing.T) {
	cfg := &config.Config{SessionStore: ""}
	ss, err := SelectSessionStore(cfg)
	if err != nil {
		t.Fatalf("SelectSessionStore(empty) error: %v", err)
	}
	if _, ok := ss.(*store.InProcessSessionStore); !ok {
		t.Fatalf("expected *store.InProcessSessionStore, got %T", ss)
	}
}

func TestSelectSessionStore_InvalidFormat(t *testing.T) {
	cfg := &config.Config{SessionStore: "not-a-url"}
	_, err := SelectSessionStore(cfg)
	if err == nil {
		t.Fatal("expected error for invalid session store format")
	}
}

// TestSelectSessionStore_AcceptsUserinfoPassword 验证 redis://:password@host:port 可用。
func TestSelectSessionStore_AcceptsUserinfoPassword(t *testing.T) {
	cfg := &config.Config{SessionStore: "redis://:s3cret@127.0.0.1:1"}
	ss, err := SelectSessionStore(cfg)
	if err != nil {
		t.Fatalf("带内嵌口令的 session-store 应可用: %v", err)
	}
	if _, ok := ss.(*store.RedisSessionStore); !ok {
		t.Fatalf("expected *store.RedisSessionStore, got %T", ss)
	}
}

// TestSelectSessionStore_RejectsMalformedUserinfo 验证畸形 URL 被拒绝。
func TestSelectSessionStore_RejectsMalformedUserinfo(t *testing.T) {
	for _, raw := range []string{
		"redis://:onlypass@",     // 有口令无 host
		"redis://",               // 无 host
		"mysql://localhost:3306", // 非 redis scheme
	} {
		cfg := &config.Config{SessionStore: raw}
		if _, err := SelectSessionStore(cfg); err == nil {
			t.Fatalf("session-store=%q 应被拒绝", raw)
		}
	}
}

// TestParseSessionStore_PasswordPrecedence 验证内嵌口令优先于 --redis-password。
func TestParseSessionStore_PasswordPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		fallback string
		wantAddr string
		wantPass string
	}{
		{"内嵌口令优先", "redis://:urlpass@r:6379", "flagpass", "r:6379", "urlpass"},
		{"无内嵌口令回落 flag", "redis://r:6379", "flagpass", "r:6379", "flagpass"},
		{"仅用户名无口令回落 flag", "redis://default@r:6379", "flagpass", "r:6379", "flagpass"},
		{"两者皆空", "redis://r:6379", "", "r:6379", ""},
		{"百分号编码口令被解码", "redis://:p%40ss%3A1@r:6379", "", "r:6379", "p@ss:1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, pass, err := parseSessionStore(tt.raw, tt.fallback)
			if err != nil {
				t.Fatalf("parseSessionStore(%q) error: %v", tt.raw, err)
			}
			if addr != tt.wantAddr || pass != tt.wantPass {
				t.Fatalf("parseSessionStore(%q) = (%q, %q), want (%q, %q)", tt.raw, addr, pass, tt.wantAddr, tt.wantPass)
			}
		})
	}
}

func TestStoreDispatcher_TaskStatesEmpty(t *testing.T) {
	st := store.NewMemoryStore()
	d := &StoreDispatcher{Store: st}

	emptyStates := d.TaskStates(nil, "default")
	if len(emptyStates) != 0 {
		t.Fatalf("TaskStates(nil) = %v, want empty", emptyStates)
	}
}

func TestStoreDispatcher_CreateTask(t *testing.T) {
	st := store.NewMemoryStore()
	d := &StoreDispatcher{Store: st}

	task := &proto.Task{TaskID: "task-disp-1", TenantID: "default", Status: "running"}
	created := d.CreateTask(task)
	if created == nil {
		t.Fatal("CreateTask returned nil")
	}
	if created.TaskID != "task-disp-1" {
		t.Fatalf("CreateTask returned taskID = %q, want task-disp-1", created.TaskID)
	}
}

// =============================================================================
// subStoreOrFallback：M3/M5 子存储的「生产 fail-fast / 非生产退内存」策略（P1-8）
// =============================================================================

func TestSubStoreOrFallback_OK(t *testing.T) {
	used := false
	got, err := subStoreOrFallback(true, "测试子存储",
		func() (string, error) { return "sql", nil },
		func() string { used = true; return "memory" })
	if err != nil {
		t.Fatalf("构造成功时不应返回错误: %v", err)
	}
	if got != "sql" || used {
		t.Fatalf("期望走 SQL 实现且不使用回退，got=%q used=%v", got, used)
	}
}

func TestSubStoreOrFallback_ProductionFailFast(t *testing.T) {
	fallbackUsed := false
	got, err := subStoreOrFallback(true, "M3 部署后端(MySQL)",
		func() (string, error) { return "", errors.New("table create denied") },
		func() string { fallbackUsed = true; return "memory" })
	if err == nil {
		t.Fatal("生产模式下子存储构造失败必须返回错误（不得静默退内存）")
	}
	if !strings.Contains(err.Error(), "fail-fast") || !strings.Contains(err.Error(), "table create denied") {
		t.Fatalf("错误信息需同时说明 fail-fast 语义与根因，got: %v", err)
	}
	if fallbackUsed {
		t.Fatal("生产模式不得调用内存回退实现")
	}
	if got != "" {
		t.Fatalf("失败时应返回零值，got=%q", got)
	}
}

func TestSubStoreOrFallback_NonProductionFallsBack(t *testing.T) {
	got, err := subStoreOrFallback(false, "M3 部署后端(MySQL)",
		func() (string, error) { return "", errors.New("table create denied") },
		func() string { return "memory" })
	if err != nil {
		t.Fatalf("非生产模式应回退而非报错: %v", err)
	}
	if got != "memory" {
		t.Fatalf("非生产模式应回退到 memory，got=%q", got)
	}
}
