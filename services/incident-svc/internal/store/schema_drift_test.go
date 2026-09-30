package store

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestSchemaCoversSQLColumns 防回归：DDL 的列集合必须覆盖本包所有 SQL 引用的列，
// 且既有库必须有补列路径。
//
// 缺陷原型（2026-10-01 复核）：occurred_at（MTTD 起点）只加进了
// deploy/docker/scripts/init-mysql.sql，而本包 initSchema 的 CREATE TABLE IF NOT EXISTS
// 不含该列 ⇒ 在「服务自建表」的部署路径（K8s / 自备 MySQL）下，CreateIncident 的 INSERT
// 每次都因 Unknown column 失败；既有库上 IF NOT EXISTS 又是 no-op，永远不会补列。
// 这类缺陷的共性是「DDL 与 SQL 语句的列清单漂移」——本测试从源码里取两份清单直接比对，
// 不依赖数据库，能在 CI 里跑。
func TestSchemaCoversSQLColumns(t *testing.T) {
	src, err := os.ReadFile("mysql.go")
	if err != nil {
		t.Fatalf("读取 mysql.go 失败: %v", err)
	}
	code := string(src)

	ddl := regexp.MustCompile(`(?s)CREATE TABLE IF NOT EXISTS incidents \((.*?)\n\t\t\)`).FindStringSubmatch(code)
	if ddl == nil {
		t.Fatal("未能从 mysql.go 提取 incidents 的 CREATE TABLE 语句（正则失配，请同步更新本测试）")
	}
	cols := map[string]bool{}
	for _, line := range strings.Split(ddl[1], "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
		if line == "" || strings.HasPrefix(strings.ToUpper(line), "INDEX") {
			continue
		}
		cols[strings.ToLower(strings.Fields(line)[0])] = true
	}
	if len(cols) == 0 {
		t.Fatal("CREATE TABLE 列解析结果为空（正则或 DDL 格式已变，请同步更新本测试）")
	}

	// 所有 INSERT INTO incidents (...) 的列清单
	inserts := regexp.MustCompile(`INSERT INTO incidents \(([^)]*)\)`).FindAllStringSubmatch(code, -1)
	if len(inserts) == 0 {
		t.Fatal("未找到任何 INSERT INTO incidents 语句（正则失配，请同步更新本测试）")
	}
	// 所有 SELECT <列清单> FROM incidents 的列清单（SELECT * 不匹配也不需匹配）
	selects := regexp.MustCompile(`(?s)SELECT ([^*]*?) FROM incidents`).FindAllStringSubmatch(code, -1)

	checked := 0
	for _, m := range inserts {
		checked += assertColumnsInDDL(t, "INSERT INTO incidents", m[1], cols)
	}
	for _, m := range selects {
		checked += assertColumnsInDDL(t, "SELECT ... FROM incidents", m[1], cols)
	}
	if checked == 0 {
		t.Fatal("列清单比对覆盖 0 列——测试失效（请检查提取逻辑）")
	}

	// 既有库补列路径：MySQL 8 无 ADD COLUMN IF NOT EXISTS，必须有 information_schema 预检。
	if !strings.Contains(code, "information_schema.columns") {
		t.Error("initSchema 缺少 information_schema 预检——既有库不会获得后加的列")
	}
	// 列名后必须紧跟空白/逗号/分行（）——否则 occurred_at_removed 这类改名也会被判过。
	if !regexp.MustCompile(`ALTER TABLE incidents ADD COLUMN occurred_at\b`).MatchString(code) {
		t.Error("initSchema 缺少 incidents.occurred_at 的补列语句——既有库仍会因 Unknown column 失败")
	}
}

func assertColumnsInDDL(t *testing.T, stmt, list string, cols map[string]bool) int {
	t.Helper()
	n := 0
	for _, c := range strings.Split(list, ",") {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		n++
		if !cols[c] {
			t.Errorf("%s 引用了 CREATE TABLE 里不存在的列 %q——服务自建表部署下该语句必然失败", stmt, c)
		}
	}
	return n
}
