// upsert_integration_test.go 全调用点真库 PREPARE 验证。
//
// A3-3 把 45 处通用 upsert 迁移到 Dialect.Upsert 后（Task 6-8），
// 剩余三个语义特例保留专用构建器（upsert_special.go，Task 9）。
// 本测试对两者输出的 SQL 在 MySQL 与 PostgreSQL 真库上执行
// PREPARE（PrepareContext）：服务端解析成功即证明方言化输出
// 可被双库接受（表/列/冲突目标/占位符数量均合法）。
//
// 门控：未设置 OPSMESH_TEST_MYSQL_DSN / OPSMESH_TEST_PG_DSN 时
// 对应子测试 skip（沿用 newTestSQLStore/newPostgresTestStore 契约）；
// 设置 DSN 时不得 skip。
package sqlstore

import (
	"context"
	"testing"
)

// TestUpsertPrepareAllCallsites 覆盖 45 通用 + 3 特例，共 48 条
// UPSERT 语句，在两个真实数据库上逐条 PREPARE。
func TestUpsertPrepareAllCallsites(t *testing.T) {
	calls := collectUpsertCalls(t, sqlstoreProductionFiles(t))
	if len(calls) != 45 {
		t.Fatalf("通用 UPSERT 调用数=%d, want 45", len(calls))
	}

	t.Run("mysql", func(t *testing.T) {
		s, cleanup := newTestSQLStore(t)
		defer cleanup()
		prepareAll(t, s, mysqlDialect{}, calls)
		prepareSpecials(t, s, mysqlDialect{})
	})

	t.Run("postgres", func(t *testing.T) {
		s, _, cleanup := newPostgresTestStore(t)
		defer cleanup()
		prepareAll(t, s, postgresDialect{}, calls)
		prepareSpecials(t, s, postgresDialect{})
	})
}

// prepareAll 对每个收集到的调用点按方言生成 UPSERT 并 PREPARE。
func prepareAll(t *testing.T, s *SQLStore, d Dialect, calls []upsertCallSpec) {
	t.Helper()
	for _, c := range calls {
		query := d.Upsert(c.Table, c.Cols, c.Values, c.Conflict, c.UpdateCols)
		stmt, err := s.db.PrepareContext(context.Background(), query)
		if err != nil {
			t.Fatalf("%s:%s PREPARE %s: %v", c.File, c.Pos, c.Table, err)
		}
		stmt.Close()
	}
}

// prepareSpecials 对三个语义特例构建器输出的语句执行相同的
// PrepareContext 验证（与通用调用点同一断言强度）。
func prepareSpecials(t *testing.T, s *SQLStore, d Dialect) {
	t.Helper()
	specials := []struct {
		name  string
		query string
	}{
		{"leader_lease", upsertLeaderLease(d)},
		{"abac_policies", upsertABACPolicy(d)},
		{"lifecycle_jobs", upsertLifecycleJob(d)},
	}
	for _, sp := range specials {
		stmt, err := s.db.PrepareContext(context.Background(), sp.query)
		if err != nil {
			t.Fatalf("%s PREPARE 失败: %v\nSQL: %s", sp.name, err, sp.query)
		}
		stmt.Close()
	}
}
