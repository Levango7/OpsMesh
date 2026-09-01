// Package migrate 的单元测试（白盒测试，覆盖文件扫描、版本解析、
// 前向迁移、回滚与 _migrations 表记录）。
//
// 测试策略：不引入新依赖、不依赖真实 MySQL —— 使用仓库已有直接依赖
// go-sqlmock（internal/cmdb、internal/logstore 同款做法）注入 fake *sql.DB，
// 另辅以临时目录扫描 .sql 文件验证纯文件发现/版本排序逻辑。
package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// newMockMigrator 构造 sqlmock 数据库 + 指向 schemaDir 的 Migrator。
func newMockMigrator(t *testing.T, schemaDir string) (*Migrator, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewMigrator(db, schemaDir), mock
}

// writeSQL 在临时目录写入一个迁移文件并返回其完整路径。
func writeSQL(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入迁移文件 %s: %v", name, err)
	}
	return path
}

// TestParseMigrationFilename：版本号前缀命名的解析（正常 + 各种非法形态）。
func TestParseMigrationFilename(t *testing.T) {
	cases := []struct {
		name    string
		version int
		desc    string
		wantErr bool
	}{
		{"001_initial.sql", 1, "initial", false},
		{"010_add_users_table.sql", 10, "add_users_table", false},
		{"nouunderscore.sql", 0, "", true},                   // 完全无下划线
		{"999_version_only.sql", 999, "version_only", false}, // 版本后跟描述，数字开头的版本段合法
		{"abc_not_number.sql", 0, "", true},                  // 版本非数字
		{"_missing_version.sql", 0, "", true},                // 版本段为空
		{"1.sql", 0, "", true},                               // 无下划线分隔描述
		{"2_x.sql", 2, "x", false},                           // 非零填充版本也合法
	}
	for _, c := range cases {
		v, desc, err := parseMigrationFilename(c.name)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: 期望报错，got version=%d desc=%q", c.name, v, desc)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: 不期望报错: %v", c.name, err)
			continue
		}
		if v != c.version || desc != c.desc {
			t.Errorf("%s: got (%d, %q), want (%d, %q)", c.name, v, desc, c.version, c.desc)
		}
	}
}

// TestLoadMigrationFiles：文件发现 + 版本排序（跳过 down/子目录/非法命名/非 sql）。
func TestLoadMigrationFiles(t *testing.T) {
	dir := t.TempDir()
	writeSQL(t, dir, "003_add_index.sql", "-- 3")
	writeSQL(t, dir, "001_initial.sql", "-- 1")
	writeSQL(t, dir, "002_users.sql", "-- 2")
	writeSQL(t, dir, "001_initial.down.sql", "-- 1 down") // 回滚文件不应进入前向列表
	writeSQL(t, dir, "not_a_migration.sql", "-- bad")     // 非法命名被跳过
	writeSQL(t, dir, "readme.txt", "-- ignored")          // 非 .sql 被跳过
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeSQL(t, filepath.Join(dir, "subdir"), "000_nested.sql", "-- nested") // 子目录被跳过

	m := &Migrator{schemaDir: dir, tableName: "_migrations"}
	files, err := m.loadMigrationFiles()
	if err != nil {
		t.Fatalf("loadMigrationFiles: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("应发现 3 个前向迁移文件，got %d: %+v", len(files), files)
	}
	// 断言按版本升序排列
	wantVersions := []int{1, 2, 3}
	for i, f := range files {
		if f.Version != wantVersions[i] {
			t.Fatalf("files[%d].Version = %d, want %d（应升序）", i, f.Version, wantVersions[i])
		}
	}
	if files[0].Name != "initial" || files[0].Path != filepath.Join(dir, "001_initial.sql") {
		t.Fatalf("files[0] = %+v, want name=initial path=001_initial.sql", files[0])
	}
}

// TestLoadDownMigrationFiles：down 文件按版本聚合为 map。
func TestLoadDownMigrationFiles(t *testing.T) {
	dir := t.TempDir()
	writeSQL(t, dir, "001_initial.down.sql", "-- down 1")
	writeSQL(t, dir, "002_users.down.sql", "-- down 2")
	writeSQL(t, dir, "003_add_index.sql", "-- forward only") // 非 down 文件被跳过
	writeSQL(t, dir, "bad_.down.sql", "-- bad version")      // 版本解析失败被跳过

	m := &Migrator{schemaDir: dir, tableName: "_migrations"}
	downs, err := m.loadDownMigrationFiles()
	if err != nil {
		t.Fatalf("loadDownMigrationFiles: %v", err)
	}
	if len(downs) != 2 {
		t.Fatalf("应发现 2 个 down 文件，got %d: %v", len(downs), downs)
	}
	if _, ok := downs[1]; !ok {
		t.Fatal("downs[1] 应存在（001_initial.down.sql）")
	}
	if _, ok := downs[2]; !ok {
		t.Fatal("downs[2] 应存在（002_users.down.sql）")
	}
	if downs[1].name != "001_initial" || downs[1].path != filepath.Join(dir, "001_initial.down.sql") {
		t.Fatalf("downs[1] = %+v", downs[1])
	}
}

// TestLoadMigrationFiles_MissingDir：schema 目录不存在时报错。
func TestLoadMigrationFiles_MissingDir(t *testing.T) {
	m := &Migrator{schemaDir: filepath.Join(t.TempDir(), "no-such-dir"), tableName: "_migrations"}
	if _, err := m.loadMigrationFiles(); err == nil {
		t.Fatal("目录不存在时应返回错误")
	}
	if _, err := m.loadDownMigrationFiles(); err == nil {
		t.Fatal("目录不存在时 down 扫描也应返回错误")
	}
}

// TestRunMigrations_AppliesPendingInOrder：未应用的迁移按版本升序逐个执行并记录。
func TestRunMigrations_AppliesPendingInOrder(t *testing.T) {
	dir := t.TempDir()
	writeSQL(t, dir, "001_initial.sql", "CREATE TABLE a(id INT);")
	writeSQL(t, dir, "002_users.sql", "CREATE TABLE b(id INT);")
	writeSQL(t, dir, "003_add_index.sql", "CREATE INDEX idx ON a(id);")

	m, mock := newMockMigrator(t, dir)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS _migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\), 0\\) FROM _migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(0))
	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE a").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO _migrations").WithArgs(1, "initial", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE b").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO _migrations").WithArgs(2, "users", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("CREATE INDEX idx").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO _migrations").WithArgs(3, "add_index", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(3, 1))
	mock.ExpectCommit()

	if err := m.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL 期望未全部满足: %v", err)
	}
}

// TestRunMigrations_SkipsApplied：已应用（版本 <= 当前）的迁移被跳过。
func TestRunMigrations_SkipsApplied(t *testing.T) {
	dir := t.TempDir()
	writeSQL(t, dir, "001_initial.sql", "CREATE TABLE a(id INT);")
	writeSQL(t, dir, "002_users.sql", "CREATE TABLE b(id INT);")

	m, mock := newMockMigrator(t, dir)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS _migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	// 当前版本已是 2：两个迁移都应跳过，不再有 Exec/Begin
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\), 0\\) FROM _migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(2))

	if err := m.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("不应产生额外的迁移执行: %v", err)
	}
}

// TestRunMigrations_IdempotentRerun：空目录（或全部已应用）时幂等成功。
func TestRunMigrations_IdempotentRerun(t *testing.T) {
	dir := t.TempDir() // 空目录：无迁移文件
	m, mock := newMockMigrator(t, dir)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS _migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\), 0\\) FROM _migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(0))
	if err := m.RunMigrations(); err != nil {
		t.Fatalf("空目录应幂等成功: %v", err)
	}
}

// TestRunMigrations_FailsAndStops：某迁移执行失败时停止并包装错误。
func TestRunMigrations_FailsAndStops(t *testing.T) {
	dir := t.TempDir()
	writeSQL(t, dir, "001_initial.sql", "CREATE TABLE a(id INT);")
	writeSQL(t, dir, "002_users.sql", "CREATE TABLE b(id INT);")

	m, mock := newMockMigrator(t, dir)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS _migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\), 0\\) FROM _migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(0))
	mock.ExpectBegin()
	execErr := fmt.Errorf("syntax error near 'TABLE'")
	mock.ExpectExec("CREATE TABLE a").WillReturnError(execErr)
	mock.ExpectRollback()

	err := m.RunMigrations()
	if err == nil {
		t.Fatal("迁移失败时应返回错误")
	}
	if want := "failed to apply migration 1 (initial)"; !contains(err.Error(), want) {
		t.Fatalf("错误应包含 %q, got %v", want, err)
	}
}

// TestRunMigrations_MissingDB：db 为 nil 时返回语义化错误。
func TestRunMigrations_MissingDB(t *testing.T) {
	m := NewMigrator(nil, t.TempDir())
	err := m.RunMigrations()
	if err == nil || !contains(err.Error(), "database connection is nil") {
		t.Fatalf("nil db 应报连接为空错误, got %v", err)
	}
}

// TestGetVersion：返回 _migrations 表中的最高版本；空表返回 0。
func TestGetVersion(t *testing.T) {
	m, mock := newMockMigrator(t, ".")
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\), 0\\) FROM _migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(7))
	v, err := m.GetVersion()
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if v != 7 {
		t.Fatalf("GetVersion = %d, want 7", v)
	}
}

// TestGetVersion_Empty：空表（COALESCE 兜底 0）返回 0。
func TestGetVersion_Empty(t *testing.T) {
	m, mock := newMockMigrator(t, ".")
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\), 0\\) FROM _migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(0))
	v, err := m.GetVersion()
	if err != nil || v != 0 {
		t.Fatalf("空表应返回 (0, nil), got (%d, %v)", v, err)
	}
}

// TestGetVersion_NilDB：db 为 nil 时报错。
func TestGetVersion_NilDB(t *testing.T) {
	m := NewMigrator(nil, ".")
	if _, err := m.GetVersion(); err == nil {
		t.Fatal("nil db 时 GetVersion 应报错")
	}
}

// TestGetMigrationHistory：按版本升序返回已应用记录。
func TestGetMigrationHistory(t *testing.T) {
	m, mock := newMockMigrator(t, ".")
	rows := sqlmock.NewRows([]string{"version", "name", "applied_at"}).
		AddRow(1, "initial", time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)).
		AddRow(2, "users", time.Date(2026, 1, 2, 11, 30, 0, 0, time.UTC))
	mock.ExpectQuery("SELECT version, name, applied_at FROM _migrations ORDER BY version ASC").
		WillReturnRows(rows)

	records, err := m.GetMigrationHistory()
	if err != nil {
		t.Fatalf("GetMigrationHistory: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("应返回 2 条记录, got %d", len(records))
	}
	if records[0].Version != 1 || records[0].Name != "initial" {
		t.Fatalf("records[0] = %+v, want version=1 name=initial", records[0])
	}
	if records[1].Version != 2 || records[1].Name != "users" {
		t.Fatalf("records[1] = %+v, want version=2 name=users", records[1])
	}
	if records[1].AppliedAt.IsZero() {
		t.Fatal("applied_at 应被扫描为非零时间")
	}
}

// TestGetMigrationHistory_Empty：无记录时返回空 slice 且无错误。
func TestGetMigrationHistory_Empty(t *testing.T) {
	m, mock := newMockMigrator(t, ".")
	mock.ExpectQuery("SELECT version, name, applied_at FROM _migrations ORDER BY version ASC").
		WillReturnRows(sqlmock.NewRows([]string{"version", "name", "applied_at"}))
	records, err := m.GetMigrationHistory()
	if err != nil {
		t.Fatalf("GetMigrationHistory: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("无记录时应返回空, got %d", len(records))
	}
}

// TestGetMigrationHistory_NilDB：db 为 nil 时报错。
func TestGetMigrationHistory_NilDB(t *testing.T) {
	m := NewMigrator(nil, ".")
	if _, err := m.GetMigrationHistory(); err == nil {
		t.Fatal("nil db 时 GetMigrationHistory 应报错")
	}
}

// TestRollback_RevertsDownToTarget：从当前版本按版本降序回滚到目标版本（含）。
func TestRollback_RevertsDownToTarget(t *testing.T) {
	dir := t.TempDir()
	writeSQL(t, dir, "001_initial.down.sql", "DROP TABLE a;")
	writeSQL(t, dir, "002_users.down.sql", "DROP TABLE b;")
	writeSQL(t, dir, "003_index.down.sql", "DROP INDEX idx;")

	m, mock := newMockMigrator(t, dir)
	// 当前版本 3，目标 1：依次回滚 v3、v2（降序，含目标之前的版本）
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\), 0\\) FROM _migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(3))
	mock.ExpectBegin()
	mock.ExpectExec("DROP INDEX idx").WillReturnResult(sqlmock.NewResult(0, 0))
	// 修复后语义：回滚走 DELETE 分支（负版本），从 _migrations 删除该版本记录
	// ——真实 MySQL 中 INSERT 同版本必主键冲突，原实现回滚第一步就会失败。
	mock.ExpectExec("DELETE FROM _migrations").WithArgs(3).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("DROP TABLE b").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM _migrations").WithArgs(2).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := m.Rollback(1); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL 期望未全部满足: %v", err)
	}
}

// TestRollback_NoOpWhenTargetAtOrAboveCurrent：目标版本 >= 当前版本时是 no-op。
func TestRollback_NoOpWhenTargetAtOrAboveCurrent(t *testing.T) {
	dir := t.TempDir()
	m, mock := newMockMigrator(t, dir)
	// 两次 Rollback 各自查询一次当前版本；目标 >= 当前时不应有任何 down 执行
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\), 0\\) FROM _migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(2))
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\), 0\\) FROM _migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(2))
	if err := m.Rollback(2); err != nil {
		t.Fatalf("Rollback(2) 应 no-op: %v", err)
	}
	if err := m.Rollback(5); err != nil {
		t.Fatalf("Rollback(5) 应 no-op: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("不应产生额外 SQL: %v", err)
	}
}

// TestRollback_MissingDownFile：缺少回滚文件时报错并指明版本号。
func TestRollback_MissingDownFile(t *testing.T) {
	dir := t.TempDir()
	writeSQL(t, dir, "001_initial.down.sql", "DROP TABLE a;")
	// v2 没有 down 文件

	m, mock := newMockMigrator(t, dir)
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\), 0\\) FROM _migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(2))

	err := m.Rollback(0)
	if err == nil || !contains(err.Error(), "no rollback file for version 2") {
		t.Fatalf("应报缺少 v2 回滚文件, got %v", err)
	}
}

// TestRollback_NilDB：db 为 nil 时报错。
func TestRollback_NilDB(t *testing.T) {
	m := NewMigrator(nil, ".")
	if err := m.Rollback(0); err == nil {
		t.Fatal("nil db 时 Rollback 应报错")
	}
}

// TestApplyMigration_DeletesNegativeVersion：Version <= 0 的迁移走 DELETE 分支
// （回滚文件以负版本记录时的删除语义）。
func TestApplyMigration_DeletesNegativeVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeSQL(t, dir, "001_initial.down.sql", "DROP TABLE a;")

	m, mock := newMockMigrator(t, dir)
	mock.ExpectBegin()
	mock.ExpectExec("DROP TABLE a").WillReturnResult(sqlmock.NewResult(0, 0))
	// Version=-1：applyMigration 应执行 DELETE FROM _migrations WHERE version = 1
	mock.ExpectExec("DELETE FROM _migrations").WithArgs(1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := m.applyMigration(migration{Version: -1, Name: "001_initial", Path: path})
	if err != nil {
		t.Fatalf("applyMigration: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL 期望未全部满足: %v", err)
	}
}

// TestApplyMigration_MissingFile：迁移文件丢失时报错。
func TestApplyMigration_MissingFile(t *testing.T) {
	m, _ := newMockMigrator(t, t.TempDir())
	err := m.applyMigration(migration{Version: 1, Name: "gone", Path: filepath.Join(t.TempDir(), "nope.sql")})
	if err == nil || !contains(err.Error(), "failed to read migration file") {
		t.Fatalf("应报读取文件失败, got %v", err)
	}
}

// TestApplyMigration_TxBeginError：开启事务失败时报错。
func TestApplyMigration_TxBeginError(t *testing.T) {
	dir := t.TempDir()
	path := writeSQL(t, dir, "001_initial.sql", "SELECT 1;")

	m, mock := newMockMigrator(t, dir)
	mock.ExpectBegin().WillReturnError(fmt.Errorf("connection refused"))
	err := m.applyMigration(migration{Version: 1, Name: "initial", Path: path})
	if err == nil || !contains(err.Error(), "failed to begin transaction") {
		t.Fatalf("应报事务开启失败, got %v", err)
	}
}

// TestEnsureMigrationsTable：建表语句发到正确的表名。
func TestEnsureMigrationsTable(t *testing.T) {
	m, mock := newMockMigrator(t, ".")
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS _migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := m.ensureMigrationsTable(); err != nil {
		t.Fatalf("ensureMigrationsTable: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL 期望未全部满足: %v", err)
	}
}

// TestNewMigratorDefaults：NewMigrator 填充默认表名与目录。
func TestNewMigratorDefaults(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	m := NewMigrator(db, "schemas")
	if m.tableName != "_migrations" {
		t.Fatalf("tableName = %q, want _migrations", m.tableName)
	}
	if m.schemaDir != "schemas" {
		t.Fatalf("schemaDir = %q, want schemas", m.schemaDir)
	}
}

// contains 是 fmt.Errorf 语义的字符串包含辅助断言。
func contains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && indexOf(s, substr) >= 0)
}

// indexOf 返回 substr 在 s 中的首个下标，未找到返回 -1。
func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
