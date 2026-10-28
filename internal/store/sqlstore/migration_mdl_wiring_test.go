package sqlstore

// 本文件守的是**接线**，不是机制。
//
// 起因（2026-10-28 变异实测）：把 sql.go 里 runMigrations 的
//     SET SESSION lock_wait_timeout = migrationLockWaitSec()
// 整段删掉，`go test ./internal/store/...`（含 sqlstore 507s 的 DSN 真库用例）**0 FAIL**。
// 既有的 TestMigrationMDLFailFast_MySQL 自己在 migConn 上手工执行了同一条 SET（其注释写明
// 「复刻 runMigrations 的接线」），于是它证明的是「MySQL 在设了上限之后会报 1205」，
// 而不是「我们的迁移连接真的设了这个上限」。上限一旦丢，生产 migrate 退回
// @@lock_wait_timeout=31536000（1 年）：升级窗口里旧容器的长事务持表时，ALTER 排队、
// 排在它后面的目标表写入一起被无限期挂住（docs/upgrade-rehearsal-2026-10-27.md §3 实测
// 10 条 tasks 写入 23.0s，基线 0.4s）。
//
// 执行方：CI integration job 的 `go test ./internal/store/...`
// （.github/workflows/ci.yml 里给了 OPSMESH_TEST_MYSQL_DSN），本机用同一条 DSN 复现。

import (
	"context"
	"strings"
	"testing"
	"time"
)

// tasks 上的一条真实 ALTER：025 ADD COLUMN priority / idempotency_key。
// 选它的理由：它改的是**业务写入的热表**，正是演练报告里被挂住 23s 的那张表。
const mdlProbeMigrationVersion = 25

// TestMigrationLockWaitWiredIntoRunMigrations_MySQL 断言三件事，缺一不可：
//  1. 走生产入口 runMigrations（不是手工搭连接）时，MDL 等待确实有上限；
//  2. 超时是**快速失败**（远小于 migrate 总预算 120s），而不是把 ALTER 挂到预算耗尽；
//  3. 报错点名 MDL 等待并给出可执行指引（操作者据此换维护窗口，而不是只看到一句 exec stmt）。
func TestMigrationLockWaitWiredIntoRunMigrations_MySQL(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()
	ctx := context.Background()

	// 让 runMigrations 必须重新执行 025：抹掉版本记录 + 撤掉它加的第一个列。
	// 只撤 priority 就够——ALTER 要先拿到 tasks 的 MDL 才会报 1060（重复列），
	// 所以「幂等放行」这条捷径绕不过锁等待。
	if _, err := s.db.ExecContext(ctx,
		"DELETE FROM schema_migrations WHERE version=?", mdlProbeMigrationVersion); err != nil {
		t.Fatalf("抹掉迁移 %d 的版本记录: %v", mdlProbeMigrationVersion, err)
	}
	if _, err := s.db.ExecContext(ctx,
		"ALTER TABLE tasks DROP COLUMN priority"); err != nil {
		t.Fatalf("回退 025 的 priority 列: %v", err)
	}

	// 会话 A：持 tasks 的 MDL（事务内 SELECT 不提交）= 升级窗口里旧容器正在跑的长事务。
	holder, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatalf("holder conn: %v", err)
	}
	// 解锁用独立 context：t.Fatalf 走到这里时原 ctx 可能已结束，不能把解锁一起丢掉。
	defer func() {
		_, _ = holder.ExecContext(context.Background(), "ROLLBACK")
		_ = holder.Close()
	}()
	if _, err := holder.ExecContext(ctx, "SET SESSION lock_wait_timeout=31536000"); err != nil {
		t.Fatalf("holder 设长等待: %v", err)
	}
	if _, err := holder.ExecContext(ctx, "BEGIN"); err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	if _, err := holder.ExecContext(ctx, "SELECT COUNT(*) FROM tasks"); err != nil {
		t.Fatalf("holder 持 tasks MDL: %v", err)
	}

	// 上限经 env 调到 4s：既让用例跑得快，也是**生产读的同一个变量**——
	// 用例不替生产路径代设任何连接变量，否则就退化成文件开头说的那种假覆盖。
	t.Setenv("OPSMESH_MIGRATION_LOCK_WAIT_SEC", "4")

	// 看门狗 40s：远大于 4s 上限，又小于 migrate 总预算（60 锁 + 60 工作 = 120s）。
	// 上限缺失时 ALTER 会一直挂到预算耗尽，所以这条线能稳定区分两种形状。
	const watchdog = 40 * time.Second
	type outcome struct {
		err     error
		elapsed time.Duration
	}
	done := make(chan outcome, 1)
	start := time.Now()
	go func() { done <- outcome{err: s.runMigrations(), elapsed: time.Since(start)} }()

	var res outcome
	select {
	case res = <-done:
	case <-time.After(watchdog):
		t.Fatalf("runMigrations 在 %v 内没有返回：迁移连接的 MDL 等待**没有上限**"+
			"（runMigrations 里缺 SET SESSION lock_wait_timeout）⇒ 长事务持表时目标表写入被无限期挂住",
			watchdog)
	}
	if res.err == nil {
		t.Fatalf("长事务持 tasks 的 MDL 时 runMigrations 竟然成功：ALTER 从未真正等锁，本用例没有射程")
	}
	if !strings.Contains(res.err.Error(), "MDL 等待超时") {
		t.Fatalf("runMigrations 的报错没点名 MDL 等待（操作者拿不到「何时重试、调哪个变量」的指引）：%v", res.err)
	}
	if res.elapsed > 20*time.Second {
		t.Fatalf("应在上限（4s）附近快速失败，实得 %v：上限形同虚设，会拖到 migrate 总预算耗尽", res.elapsed)
	}
	t.Logf("接线实证：runMigrations 在 %v 后以 MDL 等待超时快速失败：%s",
		res.elapsed.Round(time.Millisecond), strings.SplitN(res.err.Error(), "\n", 2)[0])

	// 释放长事务后同一条迁移必须正常完成——上限不能伤正常路径。
	if _, err := holder.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		t.Fatalf("holder rollback: %v", err)
	}
	if err := s.runMigrations(); err != nil {
		t.Fatalf("长事务释放后 runMigrations 仍失败: %v", err)
	}
	var reAdded, recorded int
	if err := s.db.QueryRowContext(ctx,
		`select count(*) from information_schema.columns
		  where table_schema=database() and table_name='tasks' and column_name='priority'`).Scan(&reAdded); err != nil {
		t.Fatalf("查 priority 列: %v", err)
	}
	if err := s.db.QueryRowContext(ctx,
		"select count(*) from schema_migrations where version=?", mdlProbeMigrationVersion).Scan(&recorded); err != nil {
		t.Fatalf("查版本记录: %v", err)
	}
	if reAdded != 1 || recorded != 1 {
		t.Fatalf("重放后应重新拿到列与版本记录；实得 列=%d 记录=%d", reAdded, recorded)
	}
}
