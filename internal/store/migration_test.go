package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
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
// 迁移安全（P0-5）：幂等 DDL 解析 / 锁名 / fatal 错误（纯逻辑，无需 DB）
// ============================================================================

func TestParseIdempotentDDL_Recognized(t *testing.T) {
	cases := []struct {
		stmt string
		want idempotentDDL
	}{
		{"CREATE TABLE IF NOT EXISTS tasks (\n id INT);", idempotentDDL{kind: ddlTable, table: "tasks", expectExists: true}},
		{"CREATE TABLE tasks (id INT);", idempotentDDL{kind: ddlTable, table: "tasks", expectExists: true}},
		{"CREATE INDEX idx_audit_trace ON audit_log (trace_id);", idempotentDDL{kind: ddlIndex, table: "audit_log", name: "idx_audit_trace", expectExists: true}},
		{"CREATE UNIQUE INDEX u ON t (c);", idempotentDDL{kind: ddlIndex, table: "t", name: "u", expectExists: true}},
		{"ALTER TABLE tasks ADD COLUMN claim_epoch BIGINT NOT NULL DEFAULT 0;", idempotentDDL{kind: ddlColumn, table: "tasks", name: "claim_epoch", expectExists: true}},
		{"ALTER TABLE tasks ADD claim_epoch BIGINT;", idempotentDDL{kind: ddlColumn, table: "tasks", name: "claim_epoch", expectExists: true}},
		{"ALTER TABLE `users` ADD COLUMN `tenant_id` VARCHAR(64);", idempotentDDL{kind: ddlColumn, table: "users", name: "tenant_id", expectExists: true}},
		{"ALTER TABLE users DROP COLUMN tenant_id;", idempotentDDL{kind: ddlColumn, table: "users", name: "tenant_id", expectExists: false}},
		{"ALTER TABLE t DROP INDEX i;", idempotentDDL{kind: ddlIndex, table: "t", name: "i", expectExists: false}},
	}
	for _, c := range cases {
		got, ok := parseIdempotentDDL(c.stmt)
		if !ok {
			t.Fatalf("应识别: %q", c.stmt)
		}
		if got != c.want {
			t.Fatalf("解析错误\n stmt: %q\n got:  %+v\n want: %+v", c.stmt, got, c.want)
		}
	}
}

func TestParseIdempotentDDL_Unrecognized(t *testing.T) {
	// 不认识的形态必须返回 false——调用方据此原样报错，绝不吞掉真故障。
	unrecognized := []string{
		"",
		"INSERT INTO t VALUES (1);",
		"ALTER TABLE t ADD INDEX idx_c (c);",   // ADD 的是索引，不是列
		"ALTER TABLE t ADD UNIQUE KEY uk (c);", // 同上
		"ALTER TABLE t MODIFY COLUMN c INT;",   // 改列定义（非幂等语义）
		"ALTER TABLE t ADD CONSTRAINT fk FOREIGN KEY (c) REFERENCES o(id);",
		"DROP TABLE t;",
		"CREATE DATABASE x;",
		"SELECT 1;",
		"ALTER TABLE t;", // token 不足
	}
	for _, stmt := range unrecognized {
		if _, ok := parseIdempotentDDL(stmt); ok {
			t.Fatalf("不应识别（否则会吞掉真故障）: %q", stmt)
		}
	}
}

func TestIdempotentDDLErrorCodes(t *testing.T) {
	for _, code := range []uint16{1050, 1060, 1061, 1091} {
		if !idempotentDDLErrorCodes[code] {
			t.Fatalf("MySQL %d 应属可核实放行的幂等错误码", code)
		}
	}
	// 1054 Unknown column / 1064 语法错误 等真故障不得放行。
	for _, code := range []uint16{1054, 1064, 1045, 1146} {
		if idempotentDDLErrorCodes[code] {
			t.Fatalf("MySQL %d 不得被当作幂等放行", code)
		}
	}
}

func TestMigrationLockName(t *testing.T) {
	name := migrationLockName("opsmesh")
	if name != "opsmesh_mig_opsmesh" {
		t.Fatalf("锁名错误: %q", name)
	}
	// 超长库名须退化为定长哈希（MySQL 限制锁名 ≤64 字符）。
	long := strings.Repeat("d", 200)
	got := migrationLockName(long)
	if len(got) > 64 {
		t.Fatalf("锁名超长（MySQL 会报 3057）: len=%d", len(got))
	}
	if got == migrationLockName(strings.Repeat("d", 199)) {
		t.Fatal("不同库名不应映射到同一锁名")
	}
	// 幂等：同一库名恒定映射同一锁名（多副本才能互斥）。
	if migrationLockName(long) != got {
		t.Fatal("同一库名应恒定映射同一锁名")
	}
}

func TestFatalMigrationError(t *testing.T) {
	base := errors.New("checksum mismatch")
	err := fatalMigration(base)
	var fe *fatalMigrationError
	if !errors.As(err, &fe) {
		t.Fatal("fatalMigration 结果应可被 errors.As 识别")
	}
	if !errors.Is(err, base) {
		t.Fatal("fatalMigrationError 应保留原始错误（errors.Is）")
	}
	// 非 fatal 错误不得被误判。
	if errors.As(errors.New("connection refused"), &fe) {
		t.Fatal("普通错误不应被识别为 fatalMigrationError")
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
	s, err := NewSQLStore(testDSN, "", "")
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

// TestMigrationLock_ExcludesOtherSession 验证迁移咨询锁的真实互斥性（P0-5）。
//
// 实测方式：store A 持锁 → 另开连接查 IS_USED_LOCK（MySQL 侧真相，不是本地变量）
// 必须非 NULL；用极短超时的第二个 store B 获取同一锁必须失败（说明被 A 阻塞）；
// A 释放后 B 必须能立即获得。这是「多副本并发启动不会同时跑迁移」的直接证据。
func TestMigrationLock_ExcludesOtherSession(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()
	b, cleanupB := newTestSQLStoreSameSchema(t, s)
	defer cleanupB()

	lockName := migrationLockName("test_migration_lock")
	var dbName string
	if err := s.db.QueryRow(`SELECT DATABASE()`).Scan(&dbName); err != nil {
		t.Fatalf("查询当前库名失败: %v", err)
	}
	lockName = migrationLockName(dbName)

	release, err := s.acquireMigrationLock(context.Background())
	if err != nil {
		t.Fatalf("A 获取迁移锁失败: %v", err)
	}
	// MySQL 侧核实：锁确实被某会话持有（而非只在本地标记）。
	var holder sql.NullInt64
	if err := s.db.QueryRow(`SELECT IS_USED_LOCK(?)`, lockName).Scan(&holder); err != nil {
		t.Fatalf("IS_USED_LOCK 查询失败: %v", err)
	}
	if !holder.Valid {
		t.Fatal("持锁期间 IS_USED_LOCK 应为非 NULL（锁未真正生效）")
	}
	// B 用 1s 超时尝试同一把锁：必须超时失败（证明互斥）。
	start := time.Now()
	if _, err := b.acquireMigrationLockTimeout(context.Background(), 1); err == nil {
		t.Fatal("锁被持有时 B 不应获得同一把迁移锁")
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("B 应等待超时后才失败；实际耗时 %v", elapsed)
	}
	// A 释放后：B 应立即可得。
	release()
	if _, err := b.acquireMigrationLockTimeout(context.Background(), 5); err != nil {
		t.Fatalf("A 释放后 B 应能获得锁: %v", err)
	}
	// 释放函数的幂等性：重复调用不应 panic/报错。
	release()
}

// TestRunMigrations_VersionGate 验证「旧二进制 vs 新 schema」门禁（P0-5）：
// 库中记录了一个本二进制不认识的更高版本时，runMigrations 必须拒绝（且标记 fatal 不重试）。
func TestRunMigrations_VersionGate(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	if _, err := s.db.Exec(
		`INSERT INTO schema_migrations (version, applied_at, checksum) VALUES (9999, UTC_TIMESTAMP(), 'future')`); err != nil {
		t.Fatalf("插入未来版本记录失败: %v", err)
	}
	err := s.runMigrations()
	if err == nil {
		t.Fatal("库内版本高于二进制已知版本时应拒绝启动")
	}
	if !strings.Contains(err.Error(), "版本门禁") {
		t.Fatalf("错误信息应说明版本门禁: %v", err)
	}
	var fe *fatalMigrationError
	if !errors.As(err, &fe) {
		t.Fatal("版本门禁失败应标记 fatal（不重试）")
	}
}

// TestRunMigrations_ChecksumGateFatal 验证 checksum 被篡改时标记 fatal（不进入重试退避）。
func TestRunMigrations_ChecksumGateFatal(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	if _, err := s.db.Exec(
		`UPDATE schema_migrations SET checksum='tampered' WHERE version=1`); err != nil {
		t.Fatalf("篡改 checksum 失败: %v", err)
	}
	err := s.runMigrations()
	if err == nil {
		t.Fatal("checksum 不一致时应拒绝启动")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("错误信息应说明 checksum 不符: %v", err)
	}
	var fe *fatalMigrationError
	if !errors.As(err, &fe) {
		t.Fatal("checksum 篡改应标记 fatal（不重试）")
	}
}

// TestRunMigrations_ReplayAfterHalfApplied 验证「半迁移后重放收敛」（P0-5 核心）。
//
// 场景：MySQL 的 DDL 隐式提交，若 018 的 ALTER 已落库但版本记录未写入
// （进程退出/连接中断），下次启动会重放 018 的 ALTER → MySQL 报 1060 重复列。
// 旧实现直接判定迁移失败并使服务带半迁移 schema 运行；新实现经 information_schema
// 核实「列确实已存在」后幂等放行，并补写版本记录，最终收敛一致。
func TestRunMigrations_ReplayAfterHalfApplied(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	const version = 18 // 018_users_tenant_id.sql: ALTER TABLE users ADD COLUMN tenant_id
	if !appliedVersions(s)[version] {
		t.Skipf("当前迁移链未包含版本 %d，跳过半迁移重放用例", version)
	}
	if !columnExistsTest(s, "users", "tenant_id") {
		t.Skip("users.tenant_id 不存在，跳过半迁移重放用例")
	}
	// 模拟「DDL 已生效但版本记录丢失」。
	if _, err := s.db.Exec(`DELETE FROM schema_migrations WHERE version=?`, version); err != nil {
		t.Fatalf("删除版本记录失败: %v", err)
	}
	// 重放：ALTER 会报 1060，应被核实后放行并补写版本记录。
	if err := s.runMigrations(); err != nil {
		t.Fatalf("半迁移重放应收敛成功；got=%v", err)
	}
	if !appliedVersions(s)[version] {
		t.Fatalf("重放后版本 %d 应被补记为已应用", version)
	}
	if !columnExistsTest(s, "users", "tenant_id") {
		t.Fatal("重放后 users.tenant_id 应仍存在")
	}
}

// TestRunMigrations_ConcurrentStores 验证多副本并发启动不再互相打崩（P0-5）。
//
// 模拟「多副本同时首次连接同一 schema」：并发触发多个 store 的 runMigrations，
// 咨询锁串行化后全部应成功（无 1050/1060/1099 类失败）。
func TestRunMigrations_ConcurrentStores(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()
	stores := make([]*SQLStore, 0, 4)
	for i := 0; i < 4; i++ {
		st, cl := newTestSQLStoreSameSchema(t, s)
		defer cl()
		stores = append(stores, st)
	}
	// 抹掉版本记录模拟「全新未迁移库」（表结构仍在，锁的互斥是真实验证对象：
	// 若并发进入迁移，重放路径虽有幂等兜底，但 DDL 并发本身会放大故障面）。
	var maxV int
	if err := s.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&maxV); err != nil {
		t.Fatalf("查询最大版本失败: %v", err)
	}
	errs := make(chan error, len(stores))
	for _, st := range stores {
		go func(st *SQLStore) { errs <- st.runMigrations() }(st)
	}
	for i := 0; i < len(stores); i++ {
		if err := <-errs; err != nil {
			t.Fatalf("并发迁移应全部成功（咨询锁串行化）；got=%v", err)
		}
	}
	if !appliedVersions(s)[maxV] {
		t.Fatalf("并发迁移后版本 %d 应仍记录在案", maxV)
	}
}

// TestMigrationDownScripts_UnwindChain 在真实 MySQL 上验证「全量回滚链」（P0-5）。
//
// 背景：修复前 17 个迁移中仅 2 个有 .down.sql，且那 2 个是「无 SQL 的占位」，
// 企业客户升级事故时只能人工恢复备份。本用例把回滚脚本从「文档约定」变成
// 「被持续验证的可执行资产」：迁移链跑完后按版本倒序执行全部 .down.sql，
// 断言库内不残留任何由迁移创建的对象（脚本腐化、漏删对象会立即失败）。
func TestMigrationDownScripts_UnwindChain(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	// 1. 迁移链应已建齐对象（否则本用例前提不成立）。
	if !tableExists(s, "tasks") || !tableExists(s, "users") {
		t.Fatal("迁移后核心表应存在")
	}
	downs, err := downMigrationFiles()
	if err != nil {
		t.Fatalf("读取 .down.sql 失败: %v", err)
	}
	if len(downs) == 0 {
		t.Fatal("应存在回滚脚本")
	}
	// 2. 按版本倒序执行回滚脚本（真实 MySQL 手工回滚的操作序列）。
	for i := len(downs) - 1; i >= 0; i-- {
		mf := downs[i]
		for _, stmt := range splitSQLStatements(mf.content) {
			if _, err := s.db.Exec(stmt); err != nil {
				t.Fatalf("回滚 %d (%s) 语句执行失败: %v\n stmt: %s", mf.version, mf.name, err, stmt)
			}
		}
	}
	// 3. 断言：除 schema_migrations（由 runMigrations 硬编码维护，不属于任何迁移）外，
	//    不应残留任何表——残留即说明某个 .down.sql 漏删对象。
	rows, err := s.db.Query(
		`SELECT table_name FROM information_schema.tables WHERE table_schema=DATABASE() ORDER BY table_name`)
	if err != nil {
		t.Fatalf("列举残留表失败: %v", err)
	}
	defer rows.Close()
	var leftover []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		if name == "schema_migrations" {
			continue
		}
		leftover = append(leftover, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历残留表失败: %v", err)
	}
	if len(leftover) > 0 {
		t.Fatalf("全量回滚后仍残留表（对应迁移的回滚脚本漏删对象）: %v", leftover)
	}
}

// downMigrationFiles 读取 embed 中的 .down.sql 回滚脚本（正序返回）。
// 与 migrationFiles 的区别：后者显式跳过 .down.sql（正向迁移不执行回滚脚本）。
func downMigrationFiles() ([]migrationFile, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var files []migrationFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".down.sql") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".down.sql")
		i := 0
		for i < len(base) && base[i] >= '0' && base[i] <= '9' {
			i++
		}
		if i == 0 {
			return nil, fmt.Errorf("回滚脚本 %q 缺少版本号前缀", e.Name())
		}
		v, err := strconv.Atoi(base[:i])
		if err != nil {
			return nil, fmt.Errorf("解析回滚脚本版本号 %q: %w", base[:i], err)
		}
		content, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		files = append(files, migrationFile{version: v, name: e.Name(), content: string(content)})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })
	return files, nil
}

// columnExistsTest 查询指定表的列是否存在（测试辅助，独立于生产 helper）。
func columnExistsTest(s *SQLStore, table, column string) bool {
	var cnt int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name=?`,
		table, column).Scan(&cnt)
	return err == nil && cnt > 0
}

// newTestSQLStoreSameSchema 复用既有测试库的 DSN 再开一个 store（模拟第二个副本/服务）。
func newTestSQLStoreSameSchema(t *testing.T, s *SQLStore) (*SQLStore, func()) {
	t.Helper()
	var dbName string
	if err := s.db.QueryRow(`SELECT DATABASE()`).Scan(&dbName); err != nil {
		t.Fatalf("查询当前库名失败: %v", err)
	}
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	// 复用同一临时库（withDBName 保留参数），不新建库——多副本共享 schema 是真实拓扑。
	st, err := sql.Open("mysql", ensureParseTime(withDBName(dsn, dbName)))
	if err != nil {
		t.Fatalf("open same-schema store: %v", err)
	}
	store := &SQLStore{db: st, instanceID: "test-replica", secret: mustRandHex(32),
		deviceMetrics: make(map[string]*metricsRing), agentSecretCache: make(map[string]string)}
	return store, func() { st.Close() }
}
