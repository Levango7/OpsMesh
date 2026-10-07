package store

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestSchemaCoversSQLColumns 防回归：嵌入 schema.sql 的列集合必须覆盖本包全部 SQL
// 语句引用的列，且 migration.go 的补列清单（ensureColumns）与 schema.sql 一致。
//
// 缺陷原型（2026-10-07 实测）：AllTasks() 的 SELECT 与 fire 闭包的回写链路引用
// tasks.last_fired_at，但 schema.sql 的 tasks 表没有该列（A-1 阶段限制的历史残留，
// 注释却声称「task-svc schema.sql 也已声明」）——在「服务自建表」的部署路径
// （opsmesh_task 库由 init-databases.sql 只建库不建表，表由 migrateTasks 建）下，
// AllTasks 每次都因 Unknown column 失败且**静默 return nil** ⇒ fire/reclaim/影子
// 三条读侧整轮空转、无任何日志。UpdateTask 的 UPDATE 不含该列还会让 fire 的
// 同分钟去重永不生效（内存改完写不回去）。
//
// 这类缺陷的共性是「DDL 与 SQL 语句的列清单漂移」——本测试从源码里取两份清单
// 直接比对，不依赖数据库，能在 CI 里跑（与 incident-svc 的同名守卫同一族）。
func TestSchemaCoversSQLColumns(t *testing.T) {
	schema, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatalf("读取 schema.sql 失败: %v", err)
	}
	code := ""
	for _, f := range []string{"mysql.go", "token.go", "store.go"} {
		if b, ferr := os.ReadFile(f); ferr == nil {
			code += string(b) + "\n"
		}
	}
	if strings.TrimSpace(code) == "" {
		t.Fatal("读取不到任何 store 包源码（文件被改名？请同步更新本测试）")
	}

	ddl := parseAllColumns(string(schema))
	if len(ddl) == 0 {
		t.Fatal("schema.sql 解析不出任何 CREATE TABLE（格式已变，请同步更新本测试）")
	}

	checked := 0
	// INSERT INTO <t> (...) —— 列清单显式
	for _, m := range regexp.MustCompile(`(?is)INSERT\s+INTO\s+(\w+)\s*\(([^)]*)\)`).FindAllStringSubmatch(code, -1) {
		cols, ok := ddl[strings.ToLower(m[1])]
		if !ok {
			t.Errorf("INSERT 引用表 %s 但 schema.sql 没有建它", m[1])
			continue
		}
		checked += assertColumnsInDDL(t, "INSERT INTO "+m[1], m[2], cols)
	}
	// UPDATE <t> SET a=?, b=? —— 赋值列
	for _, m := range regexp.MustCompile(`(?is)UPDATE\s+(\w+)\s+SET\s+([^"]*?)WHERE`).FindAllStringSubmatch(code, -1) {
		cols, ok := ddl[strings.ToLower(m[1])]
		if !ok {
			t.Errorf("UPDATE 引用表 %s 但 schema.sql 没有建它", m[1])
			continue
		}
		checked += assertColumnsInDDL(t, "UPDATE "+m[1], m[2], cols)
	}
	// SELECT <列清单> FROM <t> —— SELECT * 与 COUNT(*) 不匹配也不需匹配
	for _, m := range regexp.MustCompile(`(?is)SELECT\s+([^*()]*?)\s+FROM\s+(\w+)`).FindAllStringSubmatch(code, -1) {
		cols, ok := ddl[strings.ToLower(m[2])]
		if !ok {
			continue // 其他库的表（本包测试不判）
		}
		checked += assertColumnsInDDL(t, "SELECT..FROM "+m[2], m[1], cols)
	}
	// 覆盖下限：列引用比对不足这个数说明提取逻辑坏了（测试在空转）。
	if checked < 80 {
		t.Fatalf("列清单比对只覆盖 %d 列 < 下限 80——测试失效（检查提取逻辑）", checked)
	}
}

// TestEnsureColumnsMatchSchema 防回归：补列清单里每一列必须也存在于 schema.sql 的
// 建表定义里。否则「新建库」与「旧库补列」会得到两种不同的表——漂移被制度化了。
// 同时守住预检路径本身：ensureColumns 必须真的用 information_schema 预检
// （MySQL 8 无 ADD COLUMN IF NOT EXISTS，无预检的 ALTER 会让重复启动直接崩）。
func TestEnsureColumnsMatchSchema(t *testing.T) {
	schema, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatalf("读取 schema.sql 失败: %v", err)
	}
	ddl := parseAllColumns(string(schema))
	if len(taskEnsureColumns) == 0 {
		t.Fatal("taskEnsureColumns 为空——若补列机制已删除，请连本测试一起删并说明理由")
	}
	for _, col := range taskEnsureColumns {
		// ALTER 语句必须是往 tasks 表加这一列（词边界，防止改名漏判）。
		re := regexp.MustCompile(`(?i)ALTER\s+TABLE\s+tasks\s+ADD\s+COLUMN\s+` + col.name + `\b`)
		if !re.MatchString(col.ddl) {
			t.Errorf("taskEnsureColumns[%s] 的 DDL 不是往 tasks 表加该列: %s", col.name, col.ddl)
		}
		if !ddl["tasks"][col.name] {
			t.Errorf("taskEnsureColumns 要补列 %s，但 schema.sql 的 tasks 建表里没有它——新建库与旧库将得到两种表", col.name)
		}
	}
	mig, err := os.ReadFile("migration.go")
	if err != nil {
		t.Fatalf("读取 migration.go 失败: %v", err)
	}
	if !strings.Contains(string(mig), "information_schema.columns") {
		t.Error("ensureColumns 缺少 information_schema 预检——重复启动会在 ALTER 上崩")
	}
}

// parseAllColumns 按括号深度截取 CREATE TABLE 表体，逐行取列名。
// INDEX/KEY/PRIMARY/CONSTRAINT 行跳过（与 incident-svc 的同族守卫一致）。
func parseAllColumns(src string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	re := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?\x60?(\w+)\x60?\s*\(`)
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		start := strings.Index(src, m[0]) + len(m[0])
		depth := 1
		body := ""
		for i := start; i < len(src) && depth > 0; i++ {
			switch src[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					continue
				}
			}
			if depth > 0 {
				body += string(src[i])
			}
		}
		cols := map[string]bool{}
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
			up := strings.ToUpper(line)
			if line == "" ||
				strings.HasPrefix(up, "INDEX") || strings.HasPrefix(up, "KEY") ||
				strings.HasPrefix(up, "UNIQUE") || strings.HasPrefix(up, "PRIMARY") ||
				strings.HasPrefix(up, "CONSTRAINT") || strings.HasPrefix(up, "FULLTEXT") {
				continue
			}
			f := strings.Fields(line)
			if len(f) > 0 {
				cols[strings.ToLower(strings.Trim(f[0], "`"))] = true
			}
		}
		out[strings.ToLower(m[1])] = cols
	}
	return out
}

func assertColumnsInDDL(t *testing.T, stmt, list string, cols map[string]bool) int {
	t.Helper()
	n := 0
	for _, c := range strings.Split(list, ",") {
		c = strings.ToLower(strings.TrimSpace(c))
		if i := strings.Index(c, " "); i > 0 {
			c = c[:i] // "status = ?" / "agent_id=?" 形态
		}
		c = strings.TrimSuffix(c, "=")
		c = strings.Trim(c, "`")
		if c == "" || c == "*" {
			continue
		}
		if !regexp.MustCompile(`^\w+$`).MatchString(c) {
			continue // 表达式/函数，不猜
		}
		n++
		if !cols[c] {
			t.Errorf("%s 引用了建表语句里不存在的列 %q——服务自建表部署下该语句必然失败（Unknown column）", stmt, c)
		}
	}
	return n
}
