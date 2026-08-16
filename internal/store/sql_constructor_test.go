// sql_constructor_test.go 测试 SQLStore 的构造器方法和纯函数（无需数据库连接）。
//
// 覆盖范围：
//   - SQLStore.WithBus / WithDemo / WithSecret / DB / publish
//   - sha256Hex 纯函数
//   - splitSQLStatements 边界情况
//
// 直接构造 SQLStore 结构体（不通过 NewSQLStore），避免触发 MySQL 连接和迁移。
package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"opsmesh/internal/events"
)

// newSQLStoreForTest 直接构造 SQLStore（不连数据库），用于测试构造器方法。
func newSQLStoreForTest() *SQLStore {
	return &SQLStore{
		secret:           "test-secret",
		deviceMetrics:    make(map[string]*metricsRing),
		agentSecretCache: make(map[string]string),
	}
}

// TestSQLStore_WithBus 验证 WithBus 注入事件总线。
func TestSQLStore_WithBus(t *testing.T) {
	s := newSQLStoreForTest()
	bus := &recordingBus{}
	ret := s.WithBus(bus)
	if ret != s {
		t.Fatal("WithBus 应返回 s 自身")
	}
	if s.bus == nil {
		t.Fatal("WithBus 未注入 bus")
	}
	// publish 应通过 bus 发布事件
	s.publish(events.Event{Action: "test", Target: "t1"})
	if len(bus.events) != 1 || bus.events[0].Action != "test" {
		t.Fatalf("publish 未通过 bus 发布: %+v", bus.events)
	}
	// nil bus 时 publish 不应 panic
	s2 := newSQLStoreForTest()
	s2.publish(events.Event{Action: "noop"}) // 不应 panic
}

// TestSQLStore_WithDemo 验证 WithDemo 设置演示模式。
func TestSQLStore_WithDemo(t *testing.T) {
	s := newSQLStoreForTest()
	ret := s.WithDemo(true)
	if ret == nil {
		t.Fatal("WithDemo 应返回非 nil")
	}
	if !s.demo {
		t.Fatal("WithDemo(true) 未设置 demo 标志")
	}
	s.WithDemo(false)
	if s.demo {
		t.Fatal("WithDemo(false) 应清除 demo 标志")
	}
}

// TestSQLStore_WithSecret 验证 WithSecret 注入密钥。
func TestSQLStore_WithSecret(t *testing.T) {
	s := newSQLStoreForTest()
	origSecret := s.secret
	// 空密钥不覆盖
	s.WithSecret("")
	if s.secret != origSecret {
		t.Fatal("WithSecret(\"\") 不应覆盖密钥")
	}
	// 非空密钥覆盖
	s.WithSecret("new-secret")
	if s.secret != "new-secret" {
		t.Fatalf("WithSecret 未覆盖: %q", s.secret)
	}
	// 返回 s 自身
	if s.WithSecret("x") != s {
		t.Fatal("WithSecret 应返回 s 自身")
	}
}

// TestSQLStore_DB 验证 DB() 返回底层 *sql.DB。
func TestSQLStore_DB(t *testing.T) {
	s := newSQLStoreForTest()
	// 初始 db 为 nil
	if s.DB() != nil {
		t.Fatal("初始 DB() 应为 nil")
	}
	// 通过 sql.Open 创建不连接的 db（只验证 DSN，不真正连接）
	db, err := sql.Open("mysql", "root:@tcp(127.0.0.1:3306)/test?parseTime=true")
	if err != nil {
		t.Fatalf("sql.Open err = %v", err)
	}
	defer db.Close()
	s.db = db
	if s.DB() != db {
		t.Fatal("DB() 未返回设置的 db")
	}
}

// TestSQLStore_Publish_NilBus 验证 nil bus 时 publish 不 panic。
func TestSQLStore_Publish_NilBus(t *testing.T) {
	s := newSQLStoreForTest()
	s.bus = nil
	s.publish(events.Event{Action: "test"}) // 不应 panic
}

// TestSha256Hex 验证 sha256Hex 纯函数。
func TestSha256Hex(t *testing.T) {
	got := sha256Hex("hello")
	if len(got) != 64 {
		t.Fatalf("sha256Hex 长度 = %d, want 64", len(got))
	}
	// 相同输入相同输出
	if sha256Hex("hello") != got {
		t.Fatal("sha256Hex 不幂等")
	}
	// 不同输入不同输出
	if sha256Hex("world") == got {
		t.Fatal("sha256Hex 不同输入应不同输出")
	}
	// 空串
	if sha256Hex("") == "" {
		t.Fatal("sha256Hex(\"\") 不应为空")
	}
}

// TestSplitSQLStatements_EdgeCases 验证 splitSQLStatements 边界情况。
func TestSplitSQLStatements_EdgeCases(t *testing.T) {
	// 空串
	if got := splitSQLStatements(""); len(got) != 0 {
		t.Fatalf("splitSQLStatements(\"\") = %d, want 0", len(got))
	}
	// 仅注释
	if got := splitSQLStatements("-- comment\n-- another"); len(got) != 0 {
		t.Fatalf("splitSQLStatements(注释) = %d, want 0", len(got))
	}
	// 仅空行
	if got := splitSQLStatements("\n\n\n"); len(got) != 0 {
		t.Fatalf("splitSQLStatements(空行) = %d, want 0", len(got))
	}
	// 无分号结尾（缓冲区残留）
	got := splitSQLStatements("SELECT 1")
	if len(got) != 1 || !strings.Contains(got[0], "SELECT 1") {
		t.Fatalf("无分号结尾 = %+v", got)
	}
	// 多语句
	got = splitSQLStatements("SELECT 1;\nSELECT 2;\nSELECT 3;")
	if len(got) != 3 {
		t.Fatalf("多语句 = %d, want 3", len(got))
	}
	// 混合注释 + 语句
	got = splitSQLStatements("-- comment\nSELECT 1;\n-- another\nSELECT 2;")
	if len(got) != 2 {
		t.Fatalf("混合注释 = %d, want 2", len(got))
	}
}

// TestSQLStore_Publish_BusError 验证 publish 在 bus 返回错误时不 panic（仅日志）。
func TestSQLStore_Publish_BusError(t *testing.T) {
	s := newSQLStoreForTest()
	s.bus = &errorBus{}
	s.publish(events.Event{Action: "test"}) // 不应 panic
}

// errorBus 是测试用事件总线，Publish 总是返回错误。
type errorBus struct{}

func (b *errorBus) Publish(_ context.Context, _ events.Event) error {
	return errPublishFailed
}

var errPublishFailed = errString("publish failed")
