package store

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"
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
