package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/logx"
)

//go:embed schema.sql
var authSchema string

// migrate 在启动时建表。
//
// 背景：本服务此前只有 schema.sql 而没有任何代码读取它。NewMySQLStore 在 Ping 之后
// 直接进 seedDefaults() 做 INSERT，而表尚不存在——AUTH_SVC_STORE_TYPE=sql 时启动
// 即失败（"Table 'opsmesh.users' doesn't exist"）。默认 storeType=memory 掩盖了
// 该缺陷，一旦切换到 sql 后端（多副本共享用户库的推荐形态）就不可用。
//
// 全部语句为 CREATE TABLE IF NOT EXISTS，多副本并发启动安全。
func (s *MySQLStore) migrate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, stmt := range splitStatements(authSchema) {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
	}
	// 既有库补列（tenant_id 由 D4 / issue #27 裁决② 引入）：CREATE TABLE IF NOT EXISTS
	// 对**已存在**的表是 no-op，存量库不补列就会在登录签发 JWT 时拿到空租户
	// （claims.tenant_id 空串回退 "default"，所有用户落进 default 租户）。
	// MySQL 8 无 ADD COLUMN IF NOT EXISTS，故先查 information_schema 再决定加不加（幂等）。
	if err := ensureColumn(ctx, s.db, "users", "tenant_id",
		`ALTER TABLE users ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '行级租户归属（D4 / issue #27 裁决②）'`); err != nil {
		return err
	}
	return nil
}

// ensureColumn 在列不存在时执行给定 DDL 补列；已存在则什么都不做（MySQL 8 无 ADD COLUMN IF NOT EXISTS）。
// 表名/列名只进查询条件，DDL 由调用方以字面量提供（不接受外部输入拼接）。
func ensureColumn(ctx context.Context, db *sql.DB, table, column, ddl string) error {
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`, table, column).Scan(&n); err != nil {
		return fmt.Errorf("检查 %s.%s: %w", table, column, err)
	}
	if n > 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("补列 %s.%s: %w", table, column, err)
	}
	logx.Info(ctx, "[store] 表已补列（既有库迁移）", "service", "auth-svc", "table", table, "column", column)
	return nil
}

// splitStatements 按 ";" 切分 DDL 脚本，并剔除注释行与空行。
// 每个返回项均为可直接 Exec 的单条语句。
func splitStatements(script string) []string {
	var stmts []string
	for _, chunk := range strings.Split(script, ";") {
		var b strings.Builder
		for _, line := range strings.Split(chunk, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "--") {
				continue
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		if stmt := strings.TrimSpace(b.String()); stmt != "" {
			stmts = append(stmts, stmt)
		}
	}
	return stmts
}
