package sqlstore

import (
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"testing"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// dialect_test.go — Dialect 接口的纯逻辑面（无 DB）。
// 运行时面（AcquireMigrationLock 咨询锁循环、runMigrations 建表）由
// sql_postgres_test.go 在真库上覆盖；本文件钉死双轨的文本契约与判定逻辑。

func TestDialectForDSN(t *testing.T) {
	cases := []struct {
		dsn  string
		want string
	}{
		{"postgres://u:p@127.0.0.1:5432/opsmesh", "pgx"},
		{"postgresql://u:p@127.0.0.1:5432/opsmesh", "pgx"},
		{"pg://u:p@127.0.0.1:5432/opsmesh", "pgx"},
		{"user:pass@tcp(127.0.0.1:3306)/opsmesh?parseTime=true", "mysql"},
		{"mysql://user:pass@tcp(127.0.0.1:3306)/opsmesh", "mysql"},
		{"", "mysql"},
	}
	for _, c := range cases {
		if got := dialectForDSN(c.dsn).DriverName(); got != c.want {
			t.Errorf("dialectForDSN(%q).DriverName()=%q, want %q", c.dsn, got, c.want)
		}
	}
}

func TestConvertPlaceholdersPG(t *testing.T) {
	cases := []struct{ in, want string }{
		{"SELECT * FROM t WHERE a = ? AND b = ?", "SELECT * FROM t WHERE a = $1 AND b = $2"},
		{"SELECT * FROM t WHERE name = 'what?'", "SELECT * FROM t WHERE name = 'what?'"},
		{"INSERT INTO t VALUES ('it''s?')", "INSERT INTO t VALUES ('it''s?')"},
		{"INSERT INTO t VALUES ('a\\?b')", "INSERT INTO t VALUES ('a\\?b')"},
		{`SELECT "col?" FROM t`, `SELECT "col?" FROM t`},
		{"SELECT `col?` FROM t", "SELECT `col?` FROM t"},
		{"", ""},
		{"SELECT ? FROM t WHERE x = 'a?' AND y = \"b?\" AND z = `c?`", "SELECT $1 FROM t WHERE x = 'a?' AND y = \"b?\" AND z = `c?`"},
	}
	d := postgresDialect{}
	for _, c := range cases {
		if got := d.ConvertPlaceholders(c.in); got != c.want {
			t.Errorf("ConvertPlaceholdersPG(%q)=%q, want %q", c.in, got, c.want)
		}
	}
	m := mysqlDialect{}
	for _, c := range cases {
		if got := m.ConvertPlaceholders(c.in); got != c.in {
			t.Errorf("mysql ConvertPlaceholders(%q)=%q, want 恒等", c.in, got)
		}
	}
}

func TestInsertIgnore(t *testing.T) {
	wantM := "INSERT IGNORE INTO t (a, b) VALUES (?, ?)"
	m := mysqlDialect{}
	if got := m.InsertIgnore("t", []string{"a", "b"}); got != wantM {
		t.Errorf("mysql InsertIgnore=%q, want %q", got, wantM)
	}
	wantP := "INSERT INTO t (a, b) VALUES ($1, $2) ON CONFLICT DO NOTHING"
	p := postgresDialect{}
	if got := p.InsertIgnore("t", []string{"a", "b"}); got != wantP {
		t.Errorf("pg InsertIgnore=%q, want %q", got, wantP)
	}
}

func TestSchemaMigrationsDDL(t *testing.T) {
	for _, d := range []Dialect{mysqlDialect{}, postgresDialect{}} {
		ddl := d.SchemaMigrationsDDL()
		if !strings.Contains(ddl, "CREATE TABLE IF NOT EXISTS schema_migrations") {
			t.Errorf("%s SchemaMigrationsDDL 缺建表语句: %q", d.DriverName(), ddl)
		}
	}
}

func TestSetLockWaitTimeoutStmt(t *testing.T) {
	m := mysqlDialect{}
	if got := m.SetLockWaitTimeoutStmt(5); got != "SET SESSION lock_wait_timeout=5" {
		t.Errorf("mysql SetLockWaitTimeoutStmt=%q", got)
	}
	p := postgresDialect{}
	if got := p.SetLockWaitTimeoutStmt(5); got != "SET lock_timeout='5s'" {
		t.Errorf("pg SetLockWaitTimeoutStmt=%q", got)
	}
}

func TestIsIdempotentDDLError(t *testing.T) {
	m := mysqlDialect{}
	for _, c := range []uint16{1050, 1060, 1061, 1091} {
		if !m.IsIdempotentDDLError(&mysqlDriver.MySQLError{Number: c}) {
			t.Errorf("mysql %d 应判幂等", c)
		}
	}
	if m.IsIdempotentDDLError(&mysqlDriver.MySQLError{Number: 1064}) {
		t.Error("mysql 1064 不应判幂等")
	}
	if m.IsIdempotentDDLError(errors.New("x")) {
		t.Error("非 MySQLError 不应判幂等")
	}
	if !m.IsIdempotentDDLError(fmt.Errorf("wrap: %w", &mysqlDriver.MySQLError{Number: 1050})) {
		t.Error("wrapped MySQLError 应穿透判定")
	}
	p := postgresDialect{}
	for _, c := range []string{"42P07", "42701", "42704"} {
		if !p.IsIdempotentDDLError(&pgconn.PgError{Code: c}) {
			t.Errorf("pg %s 应判幂等", c)
		}
	}
	if p.IsIdempotentDDLError(&pgconn.PgError{Code: "42601"}) {
		t.Error("pg 42601 不应判幂等")
	}
	if p.IsIdempotentDDLError(errors.New("x")) {
		t.Error("非 PgError 不应判幂等")
	}
	if !p.IsIdempotentDDLError(fmt.Errorf("wrap: %w", &pgconn.PgError{Code: "42P07"})) {
		t.Error("wrapped PgError 应穿透判定")
	}
}

func TestIsLockTimeoutError(t *testing.T) {
	m := mysqlDialect{}
	if !m.IsLockTimeoutError(&mysqlDriver.MySQLError{Number: 1205}) {
		t.Error("mysql 1205 应判锁超时")
	}
	if m.IsLockTimeoutError(&mysqlDriver.MySQLError{Number: 1064}) {
		t.Error("mysql 1064 不应判锁超时")
	}
	p := postgresDialect{}
	if !p.IsLockTimeoutError(&pgconn.PgError{Code: "55P03"}) {
		t.Error("pg 55P03 应判锁超时")
	}
	if p.IsLockTimeoutError(&pgconn.PgError{Code: "42P07"}) {
		t.Error("pg 42P07 不应判锁超时")
	}
}

func TestIdentifierQuote(t *testing.T) {
	m := mysqlDialect{}
	if m.IdentifierQuote() != '`' {
		t.Errorf("mysql IdentifierQuote=%q", m.IdentifierQuote())
	}
	p := postgresDialect{}
	if p.IdentifierQuote() != '"' {
		t.Errorf("pg IdentifierQuote=%q", p.IdentifierQuote())
	}
}

func TestDDLTargetExistsQuery(t *testing.T) {
	col := idempotentDDL{kind: ddlColumn, table: "t", name: "c", expectExists: true}
	idx := idempotentDDL{kind: ddlIndex, table: "t", name: "i", expectExists: false}
	tbl := idempotentDDL{kind: ddlTable, table: "t", name: "", expectExists: true}

	m := mysqlDialect{}
	mq, ma := m.DDLTargetExistsQuery(col)
	if !strings.Contains(mq, "information_schema.columns") || len(ma) != 2 {
		t.Errorf("mysql column 查询=%q 参数=%v", mq, ma)
	}
	p := postgresDialect{}
	pq, pa := p.DDLTargetExistsQuery(col)
	if !strings.Contains(pq, "information_schema.columns") || !strings.Contains(pq, "$1") || len(pa) != 2 {
		t.Errorf("pg column 查询=%q 参数=%v", pq, pa)
	}
	if fmt.Sprint(pa) != "[t c]" {
		t.Errorf("pg column 参数顺序=%v, want [t c]", pa)
	}

	mi, _ := m.DDLTargetExistsQuery(idx)
	pi, _ := p.DDLTargetExistsQuery(idx)
	if !strings.Contains(mi, "information_schema.statistics") {
		t.Errorf("mysql index 查询=%q", mi)
	}
	if !strings.Contains(pi, "pg_indexes") {
		t.Errorf("pg index 查询=%q", pi)
	}

	mt, _ := m.DDLTargetExistsQuery(tbl)
	pt, _ := p.DDLTargetExistsQuery(tbl)
	if !strings.Contains(mt, "information_schema.tables") {
		t.Errorf("mysql table 查询=%q", mt)
	}
	if !strings.Contains(pt, "information_schema.tables") {
		t.Errorf("pg table 查询=%q", pt)
	}
}

func TestHasLegacyFixups(t *testing.T) {
	m := mysqlDialect{}
	if !m.HasLegacyFixups() {
		t.Error("mysql 应执行 legacy fixups")
	}
	p := postgresDialect{}
	if p.HasLegacyFixups() {
		t.Error("pg 应 no-op")
	}
}

func TestPrepareDSN(t *testing.T) {
	m := mysqlDialect{}
	out := m.PrepareDSN("user:pass@tcp(127.0.0.1:3306)/opsmesh")
	if !strings.Contains(out, "parseTime=true") {
		t.Errorf("mysql PrepareDSN=%q, 应补 parseTime=true", out)
	}
	pgIn := "postgres://u:p@h/opsmesh?sslmode=disable"
	p := postgresDialect{}
	if p.PrepareDSN(pgIn) != pgIn {
		t.Errorf("pg PrepareDSN=%q, 应恒等", p.PrepareDSN(pgIn))
	}
}

func TestCurrentDatabaseQuery(t *testing.T) {
	m := mysqlDialect{}
	if m.CurrentDatabaseQuery() != "SELECT DATABASE()" {
		t.Errorf("mysql CurrentDatabaseQuery=%q", m.CurrentDatabaseQuery())
	}
	p := postgresDialect{}
	if p.CurrentDatabaseQuery() != "SELECT current_database()" {
		t.Errorf("pg CurrentDatabaseQuery=%q", p.CurrentDatabaseQuery())
	}
}

func TestMigrationFS(t *testing.T) {
	mNames := a31ReadDirNames(t, mysqlDialect{}.MigrationFS())
	found001 := false
	hasDown := false
	for _, n := range mNames {
		if n == "001_initial.sql" {
			found001 = true
		}
		if strings.HasSuffix(n, ".down.sql") {
			hasDown = true
		}
	}
	if !found001 {
		t.Errorf("mysql 迁移 FS 应有 001_initial.sql, got %v", mNames)
	}
	if !hasDown {
		t.Error("mysql 迁移 FS 应含 down.sql 文件")
	}
	pNames := a31ReadDirNames(t, postgresDialect{}.MigrationFS())
	if want := []string{"001_initial.sql"}; !reflect.DeepEqual(pNames, want) {
		t.Errorf("pg 迁移 FS 应只含 001_initial.sql, got %v", pNames)
	}
}

func a31ReadDirNames(t *testing.T, fsys fs.FS) []string {
	t.Helper()
	ents, err := fs.ReadDir(fsys, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}
