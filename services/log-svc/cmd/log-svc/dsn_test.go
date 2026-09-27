package main

import "testing"

// ensureParseTime 幂等性回归守护（2026-09-25 线上缺陷）：生产清单（compose/Helm）给出的 DSN
// 已自带 "?parseTime=true"，一旦该函数不再幂等，就会拼出非法 DSN，sql.Open 失败后
// 服务静默退回内存存储——表现为"部署成功但重启即丢数据"。
// 函数挂接真实调用点（main.go sql backend），不再守护已删除的死代码。
func TestEnsureParseTimeIdempotent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"u:p@tcp(db:3306)/opsmesh", "u:p@tcp(db:3306)/opsmesh?parseTime=true"},
		{"u:p@tcp(db:3306)/opsmesh?parseTime=true", "u:p@tcp(db:3306)/opsmesh?parseTime=true"},
		{"u:p@tcp(db:3306)/opsmesh?charset=utf8mb4", "u:p@tcp(db:3306)/opsmesh?charset=utf8mb4&parseTime=true"},
	}
	for _, c := range cases {
		if got := ensureParseTime(c.in); got != c.want {
			t.Errorf("ensureParseTime(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}
