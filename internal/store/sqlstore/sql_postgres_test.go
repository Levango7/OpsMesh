package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// sql_postgres_test.go — PG 真库集成层（OPSMESH_TEST_PG_DSN，未设则跳过）。
// 执行方：本机 a31-pg15 容器与 CI（待接线）对齐 OPSMESH_TEST_MYSQL_DSN 形态。
//
// 验收口径（CLAIM A3-1 方言地基）：schema_migrations + 001 建表绿 + seedRBAC 落库
// + InsertIgnore 的 ON CONFLICT 形态真库生效。全查询面对齐列 A3-2/A3-3，不在本
// 文件射程。

// pgAdminDSN 把 URL 形态 DSN 的库名换成 /postgres（CREATE/DROP DATABASE 须连别的库）。
func pgAdminDSN(t *testing.T, dsn string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse pg dsn: %v", err)
	}
	if u.Scheme == "" || !strings.HasPrefix(u.Scheme, "postgres") && u.Scheme != "pg" {
		t.Fatalf("OPSMESH_TEST_PG_DSN 须为 URL 形态（postgres://user:pass@host:port/db），got %q", dsn)
	}
	u.Path = "/postgres"
	return u.String()
}

func pgTestDSN(t *testing.T, dsn, dbName string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse pg dsn: %v", err)
	}
	u.Path = "/" + dbName
	return u.String()
}

// newPostgresTestStore 建唯一临时库并在其上 NewSQLStore（内部跑 runMigrations+seedRBAC）。
func newPostgresTestStore(t *testing.T) (*SQLStore, string, func()) {
	t.Helper()
	dsn := os.Getenv("OPSMESH_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_PG_DSN not set; skipping postgres integration test")
	}
	adminDSN := pgAdminDSN(t, dsn)
	dbName := fmt.Sprintf("test_pg_%d", time.Now().UnixNano())

	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	if _, err := adminDB.Exec(`CREATE DATABASE "` + dbName + `"`); err != nil {
		adminDB.Close()
		t.Fatalf("create temp db %s: %v", dbName, err)
	}
	adminDB.Close()

	s, err := NewSQLStore(pgTestDSN(t, dsn, dbName), "", "")
	if err != nil {
		dropPGTestDB(t, adminDSN, dbName)
		t.Fatalf("NewSQLStore(pg): %v", err)
	}
	return s, dbName, func() {
		_ = s.db.Close()
		dropPGTestDB(t, adminDSN, dbName)
	}
}

func dropPGTestDB(t *testing.T, adminDSN, dbName string) {
	t.Helper()
	db, err := sql.Open("pgx", adminDSN)
	if err != nil {
		return
	}
	defer db.Close()
	if _, err := db.Exec(`DROP DATABASE IF EXISTS "` + dbName + `"`); err != nil {
		t.Logf("drop temp db %s: %v", dbName, err)
	}
}

// TestPostgresMigrationAndSeed 断言首装链路：dialect DDL 建 schema_migrations、
// 001 应用并登记、seedRBAC 三表落库、CurrentDatabaseQuery 可解析。
func TestPostgresMigrationAndSeed(t *testing.T) {
	s, dbName, cleanup := newPostgresTestStore(t)
	defer cleanup()
	ctx := context.Background()

	var cnt int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version=$1", 1).Scan(&cnt); err != nil {
		t.Fatalf("schema_migrations 不可读: %v", err)
	}
	if cnt != 1 {
		t.Errorf("schema_migrations 应登记 version=1 一行, got %d", cnt)
	}

	var gotDB string
	if err := s.db.QueryRowContext(ctx, s.dialect.CurrentDatabaseQuery()).Scan(&gotDB); err != nil {
		t.Fatalf("current_database(): %v", err)
	}
	if gotDB != dbName {
		t.Errorf("CurrentDatabaseQuery=%q, want %q", gotDB, dbName)
	}

	for _, tt := range []struct {
		table string
		min   int
	}{
		{"permissions", 1},
		{"roles", 3},
		{"users", 3},
	} {
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tt.table).Scan(&cnt); err != nil {
			t.Fatalf("%s 不可读: %v", tt.table, err)
		}
		if cnt < tt.min {
			t.Errorf("%s 行数=%d, want >=%d（seedRBAC 未落）", tt.table, cnt, tt.min)
		}
	}

	// 001 建表抽查：boolean 翻译列可读回 true/false。
	var mustChange bool
	if err := s.db.QueryRowContext(ctx,
		"SELECT must_change_password FROM users WHERE username='admin'").Scan(&mustChange); err != nil {
		t.Fatalf("users.must_change_password(boolean) 不可读: %v", err)
	}
	if !mustChange {
		t.Error("admin 预置口令须 must_change_password=true（boolean 列翻译契约）")
	}
}

// TestPostgresInsertIgnoreIdempotent 验证 ON CONFLICT DO NOTHING 真库形态：
// 同主键二次插入不报错且行数不增。
func TestPostgresInsertIgnoreIdempotent(t *testing.T) {
	s, _, cleanup := newPostgresTestStore(t)
	defer cleanup()
	ctx := context.Background()

	stmt := s.dialect.ConvertPlaceholders(s.dialect.InsertIgnore("leader_lease", []string{"id"}))
	if !strings.Contains(stmt, "ON CONFLICT DO NOTHING") {
		t.Fatalf("InsertIgnore 形态=%q, 缺 ON CONFLICT DO NOTHING", stmt)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.db.ExecContext(ctx, stmt, 1); err != nil {
			t.Fatalf("第 %d 次 InsertIgnore: %v", i+1, err)
		}
	}
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM leader_lease WHERE id=1").Scan(&n); err != nil {
		t.Fatalf("count leader_lease: %v", err)
	}
	if n != 1 {
		t.Errorf("leader_lease id=1 行数=%d, want 1（ON CONFLICT DO NOTHING 未生效）", n)
	}
}
