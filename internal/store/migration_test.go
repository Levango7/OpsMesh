package store

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// 本文件测试版本化迁移框架（runMigrations）。
//
// 测试分两层：
//  1. 纯逻辑层（无需 MySQL，始终运行）：splitSQLStatements 拆分、migrationFiles
//     从 embed.FS 读取与版本号解析排序；
//  2. 集成层（需真实 MySQL，默认跳过）：runMigrations 在全新库上建表、幂等性、
//     schema_migrations 表记录正确版本。运行方式与 TestSQLStore_TenantIsolation 一致：
//     OPSMESH_TEST_MYSQL_DSN="user:pass@tcp(127.0.0.1:3306)/opsmesh?parseTime=true" \
//     go test ./internal/store/ -run TestRunMigrations -v

// ============================================================================
// splitSQLStatements：多语句 SQL 拆分（纯逻辑，无需 DB）
// ============================================================================

func TestSplitSQLStatements_SingleStatement(t *testing.T) {
	in := "CREATE TABLE IF NOT EXISTS foo (id INT PRIMARY KEY);"
	got := splitSQLStatements(in)
	if len(got) != 1 {
		t.Fatalf("应拆出 1 条语句；got=%d", len(got))
	}
	if !strings.Contains(got[0], "CREATE TABLE") {
		t.Fatalf("语句内容错误: %q", got[0])
	}
}

func TestSplitSQLStatements_MultipleStatements(t *testing.T) {
	in := `-- file header
CREATE TABLE IF NOT EXISTS a (id INT PRIMARY KEY);

CREATE TABLE IF NOT EXISTS b (id INT PRIMARY KEY);
`
	got := splitSQLStatements(in)
	if len(got) != 2 {
		t.Fatalf("应拆出 2 条语句；got=%d (%v)", len(got), got)
	}
	if !strings.Contains(got[0], "CREATE TABLE IF NOT EXISTS a") {
		t.Fatalf("第 1 条语句错误: %q", got[0])
	}
	if !strings.Contains(got[1], "CREATE TABLE IF NOT EXISTS b") {
		t.Fatalf("第 2 条语句错误: %q", got[1])
	}
}

func TestSplitSQLStatements_SkipsCommentsAndBlanks(t *testing.T) {
	in := `-- 这是注释
-- 另一行注释

CREATE TABLE IF NOT EXISTS c (id INT PRIMARY KEY);
`
	got := splitSQLStatements(in)
	if len(got) != 1 {
		t.Fatalf("注释与空行应被跳过，仅 1 条语句；got=%d (%v)", len(got), got)
	}
}

func TestSplitSQLStatements_EmptyInput(t *testing.T) {
	got := splitSQLStatements("")
	if len(got) != 0 {
		t.Fatalf("空输入应返回 0 条语句；got=%d", len(got))
	}
}

// ============================================================================
// migrationFiles：从 embed.FS 读取迁移文件、版本号解析、排序（纯逻辑，无需 DB）
// ============================================================================

func TestMigrationFiles_ContainsInitial(t *testing.T) {
	files, err := migrationFiles()
	if err != nil {
		t.Fatalf("migrationFiles: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("应至少包含 001_initial.sql")
	}
	first := files[0]
	if first.version != 1 {
		t.Fatalf("首个迁移版本应为 1；got=%d", first.version)
	}
	if first.name != "001_initial.sql" {
		t.Fatalf("首个迁移文件名应为 001_initial.sql；got=%q", first.name)
	}
	if !strings.Contains(first.content, "CREATE TABLE IF NOT EXISTS agents") {
		t.Fatal("001_initial.sql 应包含 agents 建表语句")
	}
}

func TestMigrationFiles_SortedByVersion(t *testing.T) {
	files, err := migrationFiles()
	if err != nil {
		t.Fatalf("migrationFiles: %v", err)
	}
	for i := 1; i < len(files); i++ {
		if files[i-1].version >= files[i].version {
			t.Fatalf("迁移文件应按版本升序；files[%d].version=%d >= files[%d].version=%d",
				i-1, files[i-1].version, i, files[i].version)
		}
	}
}

func TestMigrationFiles_InitialContainsAllTables(t *testing.T) {
	files, err := migrationFiles()
	if err != nil {
		t.Fatalf("migrationFiles: %v", err)
	}
	// 001_initial.sql 应包含历史上 initSchema 的全部 CREATE TABLE（schema_migrations 除外，
	// 该表由 runMigrations 在执行迁移前硬编码创建）。
	required := []string{
		"agents", "devices", "tasks", "task_results",
		"audit_log", "leader_lease", "install_tokens",
		"users", "roles", "permissions",
		"alerts", "ci_types", "ci_items", "ci_relations", "ci_attr_templates",
		"k8s_clusters", "alert_rules", "os_templates", "middleware_templates",
		"refresh_tokens",
	}
	content := files[0].content
	for _, tbl := range required {
		needle := "CREATE TABLE IF NOT EXISTS " + tbl
		if !strings.Contains(content, needle) {
			t.Fatalf("001_initial.sql 缺少 %q 建表语句", needle)
		}
	}
	// schema_migrations 表不应在 001_initial.sql 中（由 runMigrations 先建）。
	if strings.Contains(content, "CREATE TABLE IF NOT EXISTS schema_migrations") {
		t.Fatal("001_initial.sql 不应包含 schema_migrations 建表语句（由 runMigrations 硬编码先建）")
	}
}

// ============================================================================
// 集成测试：runMigrations（需真实 MySQL，默认跳过）
// ============================================================================

// newTestSQLStore 创建一个指向唯一临时数据库的 SQLStore，并返回 cleanup。
// 临时库在 cleanup 中 DROP，避免测试间污染。DSN 格式：user:pass@tcp(host:port)/dbname?params。
func newTestSQLStore(t *testing.T) (*SQLStore, func()) {
	t.Helper()
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping migration integration test")
	}
	adminDSN := stripDBName(dsn)
	dbName := fmt.Sprintf("test_migration_%d", time.Now().UnixNano())

	// 创建临时库。
	adminDB, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE " + dbName); err != nil {
		adminDB.Close()
		t.Fatalf("create temp db %s: %v", dbName, err)
	}
	adminDB.Close()

	// 连接临时库并运行迁移（NewSQLStore 内部已调 runMigrations）。
	testDSN := withDBName(dsn, dbName)
	s, err := NewSQLStore(testDSN, "")
	if err != nil {
		dropTestDB(adminDSN, dbName)
		t.Fatalf("NewSQLStore: %v", err)
	}

	cleanup := func() {
		s.db.Close()
		dropTestDB(adminDSN, dbName)
	}
	return s, cleanup
}

// stripDBName 从 DSN 中去掉 dbname，保留 ?params，用于连 mysql 不指定库。
// user:pass@tcp(host:port)/dbname?params → user:pass@tcp(host:port)/?params
// 注意：go-sql-driver 要求 dbname 分隔符 "/" 必须存在（空库名也要保留），
// 否则报 "missing the slash separating the database name"。
func stripDBName(dsn string) string {
	idx := strings.LastIndex(dsn, "/")
	if idx == -1 {
		return dsn
	}
	head := dsn[:idx]
	tail := dsn[idx+1:] // dbname?params
	qIdx := strings.Index(tail, "?")
	if qIdx == -1 {
		return head + "/"
	}
	return head + "/" + tail[qIdx:]
}

// withDBName 将 DSN 中的 dbname 替换为指定名称。
func withDBName(dsn, dbName string) string {
	idx := strings.LastIndex(dsn, "/")
	if idx == -1 {
		return dsn
	}
	head := dsn[:idx]
	tail := dsn[idx+1:]
	qIdx := strings.Index(tail, "?")
	if qIdx == -1 {
		return head + "/" + dbName
	}
	return head + "/" + dbName + tail[qIdx:]
}

func dropTestDB(adminDSN, dbName string) {
	db, err := sql.Open("mysql", adminDSN)
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.Exec("DROP DATABASE IF EXISTS " + dbName)
}

// tableExists 查询当前库中指定表是否存在。
func tableExists(s *SQLStore, table string) bool {
	var cnt int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?`,
		table).Scan(&cnt)
	return err == nil && cnt > 0
}

// appliedVersions 读取 schema_migrations 表中已记录的版本号集合。
func appliedVersions(s *SQLStore) map[int]bool {
	rows, err := s.db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	m := make(map[int]bool)
	for rows.Next() {
		var v int
		if rows.Scan(&v) == nil {
			m[v] = true
		}
	}
	return m
}

// TestRunMigrationsFreshDB 在全新临时库上运行迁移，验证核心表已创建。
func TestRunMigrationsFreshDB(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	// NewSQLStore 已在构造时调用 runMigrations，此处验证建表结果。
	expected := []string{
		"agents", "devices", "tasks", "task_results",
		"schema_migrations", "audit_log", "leader_lease", "install_tokens",
		"users", "roles", "permissions",
		"alerts", "ci_types", "ci_items", "ci_relations", "ci_attr_templates",
		"k8s_clusters", "alert_rules", "os_templates", "middleware_templates",
		"refresh_tokens",
	}
	for _, tbl := range expected {
		if !tableExists(s, tbl) {
			t.Fatalf("迁移后表 %q 应存在", tbl)
		}
	}
}

// TestRunMigrationsIdempotent 连续运行两次迁移，第二次应无操作不报错。
func TestRunMigrationsIdempotent(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	// 第一次 runMigrations 已由 NewSQLStore 完成；此处再调一次，应成功且不报错。
	if err := s.runMigrations(); err != nil {
		t.Fatalf("第二次 runMigrations 应幂等无错；got=%v", err)
	}
	// 第三次再调一次，确保多次幂等。
	if err := s.runMigrations(); err != nil {
		t.Fatalf("第三次 runMigrations 应幂等无错；got=%v", err)
	}
	// 验证核心表仍存在。
	if !tableExists(s, "agents") || !tableExists(s, "tasks") {
		t.Fatal("幂等运行后核心表应仍存在")
	}
}

// TestSchemaMigrationsTable 验证 schema_migrations 表记录了正确版本。
func TestSchemaMigrationsTable(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	versions := appliedVersions(s)
	if versions == nil {
		t.Fatal("无法读取 schema_migrations 表")
	}
	// 001_initial.sql 对应版本 1，必须已记录。
	if !versions[1] {
		t.Fatalf("schema_migrations 应记录 version=1；got=%v", versions)
	}
	// 验证 schema_migrations 表结构（version / applied_at 两列）。
	var version int
	var appliedAt time.Time
	err := s.db.QueryRow(
		`SELECT version, applied_at FROM schema_migrations WHERE version=1`).Scan(&version, &appliedAt)
	if err != nil {
		t.Fatalf("查询 version=1 记录失败: %v", err)
	}
	if version != 1 {
		t.Fatalf("version 应为 1；got=%d", version)
	}
	if appliedAt.IsZero() {
		t.Fatal("applied_at 不应为零值")
	}
}

// TestRunMigrations_InitSchemaAlias 验证 deprecated 的 initSchema 仍能正常转发到 runMigrations。
// 确保向后兼容：外部若仍调 initSchema 不应破坏。
func TestRunMigrations_InitSchemaAlias(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	// initSchema 现在是 runMigrations 的别名，应幂等无错。
	if err := s.initSchema(); err != nil {
		t.Fatalf("initSchema（deprecated 别名）应幂等无错；got=%v", err)
	}
	if !tableExists(s, "agents") {
		t.Fatal("initSchema 转发后表应仍存在")
	}
}
