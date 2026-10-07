package store

import (
	"os"
	"strings"
	"testing"
)

// TestRunbookColsCoverDDL 防回归：runbookCols 常量是本包全部 runbooks 语句的列清单
// 来源（INSERT/SELECT 都用它拼，scanRunbook 的 Scan 目标序也对齐它），它必须 ⊆ 建表 DDL。
//
// 为什么要有这层：本包的 INSERT 形如 `INSERT INTO runbooks (`+runbookCols+`)`，
// 跨服务的静态门禁（validate-deploy-assets.sh 第 21 节）提取不到常量拼接的列清单，
// 该站点由本测试负责。守卫方式与 incident-svc / task-svc 的 schema_drift_test 同族。
func TestRunbookColsCoverDDL(t *testing.T) {
	src, err := os.ReadFile("mysql.go")
	if err != nil {
		t.Fatalf("读取 mysql.go 失败: %v", err)
	}
	code := string(src)

	// 1) runbookCols 必须仍以常量形式存在（若改成动态拼接，本测试的锚就漂了，判红提醒同步）
	constIdx := strings.Index(code, "const runbookCols = ")
	if constIdx < 0 {
		t.Fatal("找不到 const runbookCols —— 列清单来源已变，请同步更新本测试")
	}
	lineEnd := strings.Index(code[constIdx:], "\n")
	colsLine := code[constIdx : constIdx+lineEnd]
	colsStart := strings.Index(colsLine, "`")
	colsEnd := strings.LastIndex(colsLine, "`")
	if colsStart < 0 || colsEnd <= colsStart {
		t.Fatalf("runbookCols 常量语法已变，取不到列清单: %s", colsLine)
	}
	cols := map[string]bool{}
	for _, c := range strings.Split(colsLine[colsStart+1:colsEnd], ",") {
		c = strings.ToLower(strings.TrimSpace(c))
		if c != "" {
			cols[c] = true
		}
	}
	if len(cols) < 5 {
		t.Fatalf("runbookCols 解析出 %d 列 < 5（提取逻辑失效，判红）", len(cols))
	}

	// 2) 每个 runbookCols 成员必须出现在 CREATE TABLE runbooks 的表体里
	start := strings.Index(code, "CREATE TABLE IF NOT EXISTS runbooks (")
	if start < 0 {
		t.Fatal("找不到 runbooks 的 CREATE TABLE")
	}
	// 从表名的 '(' 开始计深：i 先推进到 '('，depth 变 1 后才开始收集表体。
	// （旧写法在 depth==0 && i>start 时就 break，扫到 "runbooks (" 之前就退出了——body 恒空。）
	i := strings.Index(code[start:], "(")
	if i < 0 {
		t.Fatal("runbooks 的 CREATE TABLE 里找不到左括号")
	}
	i += start
	depth := 0
	body := ""
	for ; i < len(code); i++ {
		switch code[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 {
			break
		}
		if depth > 0 {
			body += string(code[i])
		}
	}
	ddlCols := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		up := strings.ToUpper(line)
		if line == "" || strings.HasPrefix(up, "INDEX") || strings.HasPrefix(up, "PRIMARY") || strings.HasPrefix(up, "KEY") || strings.HasPrefix(up, "UNIQUE") {
			continue
		}
		f := strings.Fields(line)
		if len(f) > 0 {
			ddlCols[strings.ToLower(strings.Trim(f[0], ","))] = true
		}
	}
	for c := range cols {
		if !ddlCols[c] {
			t.Errorf("runbookCols 引用的列 %q 不在 runbooks 的 CREATE TABLE 里——服务自建表/共用建表两条路径都会因 Unknown column 失败", c)
		}
	}
}
