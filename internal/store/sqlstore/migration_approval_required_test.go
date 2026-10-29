package sqlstore

// 守的是「升级上来的库 ≡ 新装的库」这条不变式里被演练抓出的那一处：
// tasks.approval_required 的列定义。
//
// 起因（2026-10-27 存量库升级演练 docs/upgrade-rehearsal-2026-10-27.md §4）：
// 新装库由 028 建出 `TINYINT NOT NULL DEFAULT 0`，而老库升级路径是先由
// initSchemaExtra 用 `BOOLEAN DEFAULT 0` 补列（= tinyint(1) NULL），之后 028 的
// ADD COLUMN 撞 1060 被幂等放行——两条路径从此长期不等价，且 028 自身修不了
// （ADD COLUMN 见列存在即放行，改不了既有列的定义）。030 负责收敛存量库。
//
// 执行方：CI integration job 的 `go test ./internal/store/...`（OPSMESH_TEST_MYSQL_DSN）。

import (
	"context"
	"testing"
)

// approvalRequiredDef 把列定义压成一个可比较的字符串：类型|可空|默认值。
func approvalRequiredDef(t *testing.T, s *SQLStore) string {
	t.Helper()
	var columnType, nullable string
	var def *string
	if err := s.db.QueryRow(
		`SELECT column_type, is_nullable, column_default FROM information_schema.columns
		  WHERE table_schema=DATABASE() AND table_name='tasks' AND column_name='approval_required'`).
		Scan(&columnType, &nullable, &def); err != nil {
		t.Fatalf("读 approval_required 列定义: %v", err)
	}
	got := columnType + "|" + nullable + "|"
	if def != nil {
		got += *def
	}
	return got
}

// TestMigration030NormalizesApprovalRequired_MySQL 复刻客户升级那一刻的形状：
// 列是历史遗留的可空 tinyint(1)、库里已有 NULL 行、030 尚未应用。跑完 runMigrations 后
// 必须与**新装库**逐一相同（类型/可空/默认值），且历史 NULL 被回填成 0。
func TestMigration030NormalizesApprovalRequired_MySQL(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()
	ctx := context.Background()

	// 基线取自同一份代码的新装库（不是写死字符串）：写死会把「两边一起漂」放过去。
	canonical := approvalRequiredDef(t, s)
	if canonical != "tinyint|NO|0" {
		t.Fatalf("新装库的 approval_required 不是规范形状（028/030 自身漂了）：%q", canonical)
	}

	// 注入漂移：老库的列定义 + 一条 NULL 行；抹掉 030 的版本记录让它重新应用。
	if _, err := s.db.ExecContext(ctx,
		"ALTER TABLE tasks MODIFY COLUMN approval_required TINYINT(1) NULL DEFAULT 0"); err != nil {
		t.Fatalf("注入老库列定义: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, status, approval_required) VALUES ('td82-null-probe','pending',NULL)`); err != nil {
		t.Fatalf("注入 NULL 行: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		"DELETE FROM schema_migrations WHERE version=?", 30); err != nil {
		t.Fatalf("抹掉 030 版本记录: %v", err)
	}
	if got := approvalRequiredDef(t, s); got == canonical {
		t.Fatalf("漂移注入没生效（列定义仍是 %q），本用例没有射程", got)
	}

	// 升级那一刻：同一个入口 runMigrations。
	if err := s.runMigrations(); err != nil {
		t.Fatalf("升级路径 runMigrations 失败: %v", err)
	}

	if got := approvalRequiredDef(t, s); got != canonical {
		t.Fatalf("升级后的列定义仍与新装不等价：升级=%q 新装=%q（030 未把存量库收敛回规范形状）", got, canonical)
	}
	var backfilled int
	if err := s.db.QueryRowContext(ctx,
		`SELECT approval_required FROM tasks WHERE task_id='td82-null-probe'`).Scan(&backfilled); err != nil {
		t.Fatalf("读回填后的行: %v", err)
	}
	if backfilled != 0 {
		t.Fatalf("历史 NULL 行应回填为 0（审批语义只有两态，NULL 无业务含义），实得 %d", backfilled)
	}
	var recorded int
	if err := s.db.QueryRowContext(ctx,
		"SELECT count(*) FROM schema_migrations WHERE version=?", 30).Scan(&recorded); err != nil {
		t.Fatalf("查 030 版本记录: %v", err)
	}
	if recorded != 1 {
		t.Fatalf("030 应在应用后写回版本记录，实得 %d 条", recorded)
	}
}
