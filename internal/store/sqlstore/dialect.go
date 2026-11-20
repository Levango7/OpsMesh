package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// Dialect 屏蔽 MySQL / PostgreSQL 两族 SQL 方言差异。本批（A3-1 方言地基）只接
// 本批有消费者的方法；无消费者的能力（LIMIT-OFFSET 分页、通用 Upsert、DDL 自增
// 翻译）不入口，列 A3-2/A3-3，避免死键。
//
// 设计约束：
//   - MySQL 实现封装现状 SQL 文本，行为逐字节等价（ConvertPlaceholders 恒等）；
//   - PG 实现按 PG 协议形态输出；NewSQLStore 按 DSN scheme 推断方言，签名不变。
type Dialect interface {
	// DriverName 返回 database/sql 驱动名（"mysql" | "pgx"）。
	DriverName() string
	// PrepareDSN 对方言专有 DSN 参数负责（MySQL 需 parseTime/clientFoundRows；
	// PG DSN 不认识这些参数，原样透传）。
	PrepareDSN(dsn string) string
	// ConvertPlaceholders 把 ? 占位符编号为 $1/$2/...（字面量感知：字符串/标识符
	// 内的 ? 不误转）。MySQL 方言恒等返回。
	ConvertPlaceholders(query string) string
	// InsertIgnore 生成幂等插入语句。MySQL: INSERT IGNORE INTO ...；
	// PG: INSERT ... ON CONFLICT DO NOTHING。
	// 语义差异如实登记：MySQL INSERT IGNORE 吞所有列级错误；PG ON CONFLICT DO NOTHING
	// 只跳过唯一键冲突，其余错误照报。本仓 seed 场景的意图即「存在即跳过」，二者等价。
	InsertIgnore(table string, cols []string) string
	// SchemaMigrationsDDL 建迁移登记表的 DDL。
	SchemaMigrationsDDL() string
	// SetLockWaitTimeoutStmt 设置迁移语句锁等待上限。
	SetLockWaitTimeoutStmt(sec int) string
	// AcquireMigrationLock / ReleaseMigrationLock 迁移串行化锁（会话级，
	// 连接断开自动释放）。MySQL 用 GET_LOCK/RELEASE_LOCK；PG 用
	// pg_try_advisory_lock(hashtext(name)::bigint) 客户端循环等待。
	AcquireMigrationLock(ctx context.Context, conn *sql.Conn, name string, timeoutSec int) error
	ReleaseMigrationLock(ctx context.Context, conn *sql.Conn, name string) error
	// CurrentDatabaseQuery 返回当前库名查询（用于构造按库隔离的锁名）。
	CurrentDatabaseQuery() string
	// MigrationFS 返回该方言的迁移文件 embed.FS（双轨：migrations/ 与 migrations/pg/）。
	MigrationFS() fs.FS
	// HasLegacyFixups 是否执行 applyLegacyColumnFixups 老库补列补丁。
	// PG 无迁移框架前的老库，整体 no-op。
	HasLegacyFixups() bool
	// IsIdempotentDDLError 判定错误是否「对象已存在/不存在」类（可二次核实后放行）。
	IsIdempotentDDLError(err error) bool
	// IsLockTimeoutError 判定是否迁移锁等待超时（MDL 1205 ⇔ PG 55P03）。
	IsLockTimeoutError(err error) bool
	// DDLTargetExistsQuery 返回核实 DDL 目标对象存在性的 (查询, 参数)。
	DDLTargetExistsQuery(t idempotentDDL) (query string, args []any)
	// IdentifierQuote 返回标识符引用字符（MySQL 反引号 / PG 双引号），
	// 供 DDL token 解析去引号用。
	IdentifierQuote() byte
}

// dialectForDSN 按 DSN scheme 推断方言。postgres://|postgresql://|pg:// ⇒ PG；
// 其余（MySQL 裸 DSN 与 mysql:// 前缀）一律 MySQL，默认分支保现状行为。
func dialectForDSN(dsn string) Dialect {
	for _, p := range []string{"postgres://", "postgresql://", "pg://"} {
		if strings.HasPrefix(dsn, p) {
			return postgresDialect{}
		}
	}
	return mysqlDialect{}
}

// ----------------------------------------------------------------------------
// MySQL 实现：封装现状 SQL 文本，行为逐字节等价。
// ----------------------------------------------------------------------------

type mysqlDialect struct{}

func (mysqlDialect) DriverName() string { return "mysql" }

func (mysqlDialect) PrepareDSN(dsn string) string { return ensureParseTime(dsn) }

func (mysqlDialect) ConvertPlaceholders(query string) string { return query }

func (mysqlDialect) InsertIgnore(table string, cols []string) string {
	return fmt.Sprintf("INSERT IGNORE INTO %s (%s) VALUES (%s)",
		table, strings.Join(cols, ", "), placeholdersMySQL(len(cols)))
}

func (mysqlDialect) SchemaMigrationsDDL() string {
	return `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INT PRIMARY KEY,
		applied_at DATETIME,
		checksum VARCHAR(64) NOT NULL DEFAULT ''
	)`
}

func (mysqlDialect) SetLockWaitTimeoutStmt(sec int) string {
	return fmt.Sprintf("SET SESSION lock_wait_timeout=%d", sec)
}

func (mysqlDialect) AcquireMigrationLock(ctx context.Context, conn *sql.Conn, name string, timeoutSec int) error {
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(?, ?)`, name, timeoutSec).Scan(&got); err != nil {
		return fmt.Errorf("acquire migration lock %s: %w", name, err)
	}
	// GET_LOCK 语义：1=获得，0=超时，NULL=出错。
	if !got.Valid || got.Int64 != 1 {
		return fmt.Errorf("acquire migration lock %s: 等待 %ds 未获得（另一实例正在迁移）", name, timeoutSec)
	}
	return nil
}

func (mysqlDialect) ReleaseMigrationLock(ctx context.Context, conn *sql.Conn, name string) error {
	_, err := conn.ExecContext(ctx, `SELECT RELEASE_LOCK(?)`, name)
	return err
}

func (mysqlDialect) CurrentDatabaseQuery() string { return `SELECT DATABASE()` }

func (mysqlDialect) MigrationFS() fs.FS { return mysqlMigrationsFS }

func (mysqlDialect) HasLegacyFixups() bool { return true }

func (mysqlDialect) IsIdempotentDDLError(err error) bool {
	var me *mysqlDriver.MySQLError
	if !errors.As(err, &me) {
		return false
	}
	return idempotentDDLErrorCodes[me.Number]
}

func (mysqlDialect) IsLockTimeoutError(err error) bool {
	var me *mysqlDriver.MySQLError
	return errors.As(err, &me) && me.Number == 1205
}

func (mysqlDialect) DDLTargetExistsQuery(t idempotentDDL) (string, []any) {
	switch t.kind {
	case ddlColumn:
		return `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name=?`, []any{t.table, t.name}
	case ddlIndex:
		return `SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name=? AND index_name=?`, []any{t.table, t.name}
	case ddlTable:
		return `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?`, []any{t.table}
	}
	return "", nil
}

func (mysqlDialect) IdentifierQuote() byte { return '`' }

// placeholdersMySQL 生成 n 个 ? 占位符（逗号分隔）。
func placeholdersMySQL(n int) string {
	ph := make([]string, n)
	for i := range ph {
		ph[i] = "?"
	}
	return strings.Join(ph, ", ")
}

// ----------------------------------------------------------------------------
// PostgreSQL 实现：pgx/v5 stdlib 驱动，占位符 $n，咨询锁 pg_try_advisory_lock。
// ----------------------------------------------------------------------------

type postgresDialect struct{}

func (postgresDialect) DriverName() string { return "pgx" }

func (postgresDialect) PrepareDSN(dsn string) string { return dsn }

func (postgresDialect) ConvertPlaceholders(query string) string {
	return convertPlaceholdersPG(query)
}

func (postgresDialect) InsertIgnore(table string, cols []string) string {
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING",
		table, strings.Join(cols, ", "), placeholdersPG(len(cols)))
}

func (postgresDialect) SchemaMigrationsDDL() string {
	return `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INT PRIMARY KEY,
		applied_at TIMESTAMP,
		checksum VARCHAR(64) NOT NULL DEFAULT ''
	)`
}

func (postgresDialect) SetLockWaitTimeoutStmt(sec int) string {
	return fmt.Sprintf("SET lock_timeout='%ds'", sec)
}

func (postgresDialect) AcquireMigrationLock(ctx context.Context, conn *sql.Conn, name string, timeoutSec int) error {
	// pg_try_advisory_lock 非阻塞 ⇒ 客户端循环等待，对齐 GET_LOCK(name, timeout) 的
	// 服务端阻塞语义。hashtext 返回 int4，pg_try_advisory_lock 只有 (bigint)/(int,int)
	// 重载，故 ::bigint 转换。锁名仍用 migrationLockName(dbName) 保按库隔离。
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for {
		var got bool
		if err := conn.QueryRowContext(ctx,
			`SELECT pg_try_advisory_lock(hashtext($1)::bigint)`, name).Scan(&got); err != nil {
			return fmt.Errorf("acquire migration lock %s: %w", name, err)
		}
		if got {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("acquire migration lock %s: 等待 %ds 未获得（另一实例正在迁移）", name, timeoutSec)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (postgresDialect) ReleaseMigrationLock(ctx context.Context, conn *sql.Conn, name string) error {
	_, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock(hashtext($1)::bigint)`, name)
	return err
}

func (postgresDialect) CurrentDatabaseQuery() string { return `SELECT current_database()` }

func (postgresDialect) MigrationFS() fs.FS { return pgMigrationsFS }

func (postgresDialect) HasLegacyFixups() bool { return false }

func (postgresDialect) IsIdempotentDDLError(err error) bool {
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		return false
	}
	switch pe.Code {
	case "42P07", "42701", "42704": // duplicate_table / duplicate_column / undefined_object
		return true
	}
	return false
}

func (postgresDialect) IsLockTimeoutError(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "55P03" // lock_not_available
}

func (postgresDialect) DDLTargetExistsQuery(t idempotentDDL) (string, []any) {
	switch t.kind {
	case ddlColumn:
		return `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name=$2`, []any{t.table, t.name}
	case ddlIndex:
		return `SELECT COUNT(*) FROM pg_indexes WHERE schemaname=current_schema() AND tablename=$1 AND indexname=$2`, []any{t.table, t.name}
	case ddlTable:
		return `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, []any{t.table}
	}
	return "", nil
}

func (postgresDialect) IdentifierQuote() byte { return '"' }

// placeholdersPG 生成 $1..$n 占位符（逗号分隔）。
func placeholdersPG(n int) string {
	ph := make([]string, n)
	for i := range ph {
		ph[i] = "$" + strconv.Itoa(i+1)
	}
	return strings.Join(ph, ", ")
}

// convertPlaceholdersPG 把 ? 占位符编号为 $1/$2/...，字面量感知：
// 单引号字符串（含 ” 转义与 \' 反斜杠转义）、双引号标识符、反引号标识符
// 内的 ? 一律不转。MySQL 文本里的反斜杠转义按 MySQL 规则跳过（PG 文本无
// 反斜杠转义需求，同状态机无害）。
func convertPlaceholdersPG(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 16)
	n := 0
	inSingle, inDouble, inBacktick := false, false, false
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case inSingle:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(query) {
				b.WriteByte(query[i+1])
				i++
				continue
			}
			if c == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' { // '' 转义
					b.WriteByte('\'')
					i++
					continue
				}
				inSingle = false
			}
		case inDouble:
			b.WriteByte(c)
			if c == '"' {
				inDouble = false
			}
		case inBacktick:
			b.WriteByte(c)
			if c == '`' {
				inBacktick = false
			}
		case c == '\'':
			inSingle = true
			b.WriteByte(c)
		case c == '"':
			inDouble = true
			b.WriteByte(c)
		case c == '`':
			inBacktick = true
			b.WriteByte(c)
		case c == '?':
			n++
			b.WriteString("$" + strconv.Itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
