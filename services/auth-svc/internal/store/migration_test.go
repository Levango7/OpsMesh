package store

import (
	"strings"
	"testing"
)

// TestSplitStatements_AuthSchema 验证嵌入的 schema.sql 能被完整拆成可执行语句。
// 建表此前从未执行过（NewMySQLStore 直接进 seedDefaults 做 INSERT），故这里既校验
// 数量也校验表名与顺序，防止将来改动 schema.sql 后迁移静默漏建表。
func TestSplitStatements_AuthSchema(t *testing.T) {
	stmts := splitStatements(authSchema)
	if len(stmts) != 7 {
		t.Fatalf("语句数 = %d, want 7（users/roles/permissions/user_roles/role_permissions/refresh_tokens/jti_blacklist）", len(stmts))
	}
	want := []string{"users", "roles", "permissions", "user_roles", "role_permissions", "refresh_tokens", "jti_blacklist"}
	for i, table := range want {
		if !strings.HasPrefix(stmts[i], "CREATE TABLE IF NOT EXISTS "+table+" (") {
			t.Errorf("语句[%d] 应以建 %s 表开头，实际: %.60s", i, table, stmts[i])
		}
	}
	for i, stmt := range stmts {
		if strings.Contains(stmt, "--") {
			t.Errorf("语句[%d] 仍含 SQL 注释，Exec 会报语法错误: %.80s", i, stmt)
		}
		if strings.Contains(stmt, ";") {
			t.Errorf("语句[%d] 仍含分号（未被切分干净）: %.80s", i, stmt)
		}
	}
}

// TestSplitStatements_EdgeCases 覆盖空脚本、纯注释、缺尾分号等边界。
func TestSplitStatements_EdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   int
	}{
		{"空脚本", "", 0},
		{"仅空白", "  \n\t\n", 0},
		{"仅注释", "-- just a comment\n-- another\n", 0},
		{"末尾无分号", "CREATE TABLE t (a INT)", 1},
		{"行内注释被剔除", "-- head\nCREATE TABLE t (a INT); -- tail\n", 1},
		{"连续分号不产生空语句", "CREATE TABLE a (x INT);;CREATE TABLE b (y INT);", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitStatements(tc.script)
			if len(got) != tc.want {
				t.Fatalf("语句数 = %d, want %d; got=%q", len(got), tc.want, got)
			}
		})
	}
}
