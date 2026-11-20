package sqlstore

import (
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// pgParityCreateTableRe 匹配 CREATE TABLE [IF NOT EXISTS] name / `name` / "name"。
// MySQL 轨用裸标识符，PG 轨用双引号标识符，故引号可选。
var pgParityCreateTableRe = regexp.MustCompile("(?i)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?[`\"]?([A-Za-z0-9_]+)[`\"]?")

// pgParityTablesFromFS 收集一个 embed FS 目录下所有 .sql 声明的表名。
// 注释行（以 -- 开头）先剥离，避免把注释里的 CREATE TABLE 示例文本当成真声明。
func pgParityTablesFromFS(t *testing.T, fsys fs.FS, dir string) map[string]bool {
	t.Helper()
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		t.Fatalf("读 embed FS %s 失败: %v", dir, err)
	}
	out := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		content, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("读 %s/%s 失败: %v", dir, e.Name(), err)
		}
		for _, line := range strings.Split(string(content), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "--") {
				continue
			}
			for _, m := range pgParityCreateTableRe.FindAllStringSubmatch(line, -1) {
				out[m[1]] = true
			}
		}
	}
	return out
}

// TestPGMigrationTableParityWithMySQL 钉死 PG 轨首文件与 MySQL 轨迁移的表集合一致。
//
// 为什么要有这道门：PG 轨语义是「001_initial.sql 一次建齐全库」，而 MySQL 轨的迁移
// 会随业务批次持续新增表（032-036 又添了 5 张）。两侧一旦分叉，PG 轨建出的库就缺表，
// 而缺表在 MySQL 轨上不会暴露——只有真库对照才看得见。本门把该差集变成 CI 红。
//
// 已知且刻意豁免的项（不属表集合差集）：
//   - schema_migrations：两侧均由 Go 侧 dialect DDL / initSchema 建，不入迁移文件；
//   - ci_items.ci_attrs_text：MySQL 侧生成列，PG 无等价物（生成列语义分歧列 A3-2）。
func TestPGMigrationTableParityWithMySQL(t *testing.T) {
	mysqlTables := pgParityTablesFromFS(t, mysqlMigrationsFS, ".")
	pgTables := pgParityTablesFromFS(t, pgMigrationsFS, ".")

	if len(mysqlTables) == 0 {
		t.Fatalf("MySQL 轨迁移未解析出任何 CREATE TABLE（解析器失效？），本门不可信")
	}
	if len(pgTables) == 0 {
		t.Fatalf("PG 轨 001 未解析出任何 CREATE TABLE（解析器失效？），本门不可信")
	}

	var onlyMySQL, onlyPG []string
	for name := range mysqlTables {
		if !pgTables[name] {
			onlyMySQL = append(onlyMySQL, name)
		}
	}
	for name := range pgTables {
		if !mysqlTables[name] {
			onlyPG = append(onlyPG, name)
		}
	}
	sort.Strings(onlyMySQL)
	sort.Strings(onlyPG)

	if len(onlyMySQL) > 0 {
		t.Errorf("MySQL 轨有而 PG 轨 001 缺的表（PG 建库将缺表）: %v", onlyMySQL)
	}
	if len(onlyPG) > 0 {
		t.Errorf("PG 轨 001 有而 MySQL 轨缺的表（PG 轨多出，来源不明）: %v", onlyPG)
	}
	t.Logf("两轨表集合一致：MySQL=%d PG=%d", len(mysqlTables), len(pgTables))
}
