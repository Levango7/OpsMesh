package logstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// TestSQLNullRoundTripRealMySQL 是真库往返：证明"写入侧产 NULL"与"读取侧容得下 NULL"这两件事
// 在同一个真实驱动上成立。sqlmock 那份用例（TestSQLQueryReadsNullColumns）只能证明
// 转换层不报错，证明不了写读两端对得上——那正是这个缺陷藏了这么久的原因。
//
// 约定：无 OPSMESH_TEST_MYSQL_DSN 时 skip（与 internal/store 的集成用例同一约定）。
// **但 skip 不等于跑过**：本包的真库路径要靠 ci.yml 里一个专门的步骤执行
// （integration job 原本只测 ./internal/store/...，若不同步加一步，这个用例会在 CI 里永不执行）。
func TestSQLNullRoundTripRealMySQL(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN 未设置，跳过日志 SQL 后端的真库往返")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping（DSN 不可用）: %v", err)
	}
	ctx := context.Background()
	s := &SQLLogStore{db: db}
	if err := s.initSchema(ctx); err != nil {
		t.Fatalf("initSchema: %v", err)
	}

	// 独立租户 + 用完即删：CI 的 opsmesh 库里会留下 log_entries 表，但行不留给别人看见。
	tenant := fmt.Sprintf("t-null-%d", time.Now().UnixNano())
	defer func() {
		if _, err := db.Exec(`DELETE FROM log_entries WHERE tenant_id=?`, tenant); err != nil {
			t.Logf("清理失败（不影响判定）: %v", err)
		}
	}()

	// 只有 tenant/ts/level/source/message —— device/agent/task 留空，
	// Append 内部的 nullStr() 应把它们落成 NULL。
	if err := s.Append(ctx, &Entry{
		TenantID: tenant, Level: "warn", Source: "system",
		Message: "null round trip", Timestamp: time.Now().UTC().Truncate(time.Second),
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// 先单独确认库里真的是 NULL（而不是空串）——否则整条测试会退化成"读写都走空串"的假验证。
	var isNull bool
	if err := db.QueryRowContext(ctx,
		`SELECT task_id IS NULL FROM log_entries WHERE tenant_id=?`, tenant).Scan(&isNull); err != nil {
		t.Fatalf("查 NULL 性: %v", err)
	}
	if !isNull {
		t.Fatalf("前提不成立：task_id 没落成 NULL，写入侧语义已变（本用例失去意义，需重看 nullStr）")
	}

	out, err := s.Query(ctx, Query{TenantID: tenant, Limit: 10})
	if err != nil {
		t.Fatalf("含 NULL 行的检索必须成功，实际整条查询失败: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 hit, got %d", len(out))
	}
	got := out[0]
	if got.DeviceID != "" || got.AgentID != "" || got.TaskID != "" {
		t.Errorf("NULL 应读成空串，实际 device=%q agent=%q task=%q", got.DeviceID, got.AgentID, got.TaskID)
	}
	if got.Level != "warn" || got.Source != "system" || got.Message != "null round trip" {
		t.Errorf("非 NULL 列必须原样往返，实际 level=%q source=%q message=%q", got.Level, got.Source, got.Message)
	}
	if got.TenantID != tenant {
		t.Errorf("租户串了：want %q got %q", tenant, got.TenantID)
	}
}
