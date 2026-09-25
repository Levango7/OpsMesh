package store

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"
)

//go:embed schema.sql
var deviceSchema string

// migrate 在启动时建表。
//
// 背景：本服务此前只有 schema.sql 而没有任何代码读取它（mysql.go 内无 CREATE TABLE，
// 也无 go:embed），main.go 注释宣称的「自动建表」并未发生。后果是 sql 后端下
// 首次查询即 "Table 'opsmesh_device.devices' doesn't exist"，设备清单/CMDB/发现
// 三项功能整体不可用。此处按 task-svc / alert-svc 的既有风格补上建表。
//
// 全部语句为 CREATE TABLE IF NOT EXISTS，多副本并发启动安全。
func (s *MySQLStore) migrate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, stmt := range splitStatements(deviceSchema) {
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
