// mysql_scan_test.go — 本服务读侧必须扛得住可空列（与 alert-svc 同源门禁，TD-74 同族）。
//
// 为什么要有这层：可空列被直接扫进标量时，驱动报
// `converting NULL to ... is unsupported`，**整行读不出来**——
// 不是报错，是配置项/密钥/定时任务在列表与直查里同时消失，
// 且只在真库 + 特定数据形态下才暴露。
//
// 判定口径（刻意收窄，避免误报把门禁自己变成噪音）：
//   - 可空性来自 schema.sql；没有该文件时解析 Go 内联 DDL（部分服务把建表写在代码里）。
//     **可空且无 DEFAULT** 才算真风险——有 DEFAULT 时省略该列的 INSERT 会填默认值，
//     读侧遇不到 NULL；
//   - sql.Null* 与 []byte 目标都视为已保护。[]byte 尤其不是漏网：
//     database/sql 把 NULL 转成 nil 字节切片，不报错；
//   - 只判「列数 == Scan 目标数」的站点，混入表达式/聚合的 SELECT 不猜；
//   - 嵌套字段（&r.Event.TenantID）按**最后一段**推列名。用第一个点切分会让
//     `Event.TenantID` 映射不到任何列，整类嵌套字段「算作已判定却一条判不到」——
//     门禁最危险的形态：绿着，但没在看。
//
// 覆盖账目：TestEveryWideScanSiteIsJudgedOrExempted 要求每个参数≥3 的站点
// 要么参与判定、要么带理由进 scanSiteExemptions；豁免条目过期即判红
// （站点消失必须删条目，否则「曾经合理」会变成永久免检）。
package orchestration

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// judgedSitesFloor 是「参与判定的宽站点数」的下限，只许上调。
// 覆盖回退时门禁必须变红，而不是安静地少看几个站点。
const judgedSitesFloor = 1

// scanSiteExemptions 登记「参数≥3 但不参与判定」的站点及理由。
// 键是 "文件名:行号"。没有条目可留空——空表本身也是合法状态。
var scanSiteExemptions = map[string]string{
	"sql.go:169": "SELECT 列清单由 wfCols 常量拼接（sql.go:27），门禁静态解析不了；11 列与 Scan 目标一一对应，可空列 dag/cron/last_run_at/last_run_status/created_at/updated_at 均已走 sql.Null* 中转",
}

// riskyNullableColumns 解析建表来源，得到 table → {可空且无 DEFAULT 的列}。
var riskyNullableColumns = buildRiskyNullableColumns()

func buildRiskyNullableColumns() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	if b, err := os.ReadFile("schema.sql"); err == nil {
		parseCreateTables(string(b), out)
	}
	// schema.sql 不存在时退回 Go 内联 DDL（部分服务把建表写在代码里）。
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		if b, err := os.ReadFile(f); err == nil {
			parseCreateTables(string(b), out)
		}
	}
	return out
}

// parseCreateTables 按行扫 CREATE TABLE ... ( 到以 ")" 开头的行为止。
// 刻意不用正则匹配到 `\n\)`：`) ENGINE=InnoDB` 这种表尾选项会让正则把后面
// 几张表的建表语句一起吞进当前表体（曾导致主键列被误报成可空）。
func parseCreateTables(src string, out map[string]map[string]bool) {
	lines := strings.Split(src, "\n")
	re := regexp.MustCompile(`(?i)CREATE TABLE(?: IF NOT EXISTS)?\s+(\w+)\s*\(`)
	for i := 0; i < len(lines); i++ {
		m := re.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		cols := out[m[1]]
		if cols == nil {
			cols = map[string]bool{}
			out[m[1]] = cols
		}
		for i++; i < len(lines); i++ {
			l := strings.TrimSpace(lines[i])
			if strings.HasPrefix(l, ")") {
				break
			}
			l = strings.TrimSuffix(l, ",")
			if l == "" || strings.HasPrefix(l, "--") {
				continue
			}
			if regexp.MustCompile(`^(PRIMARY KEY|UNIQUE|INDEX|KEY|CONSTRAINT|FOREIGN)\b`).MatchString(l) {
				continue
			}
			f := strings.Fields(l)
			if len(f) < 2 {
				continue
			}
			u := strings.ToUpper(l)
			nullable := !strings.Contains(u, "NOT NULL") &&
				!strings.Contains(u, "AUTO_INCREMENT") &&
				!strings.Contains(u, "PRIMARY KEY")
			if nullable && !strings.Contains(u, "DEFAULT") {
				cols[strings.Trim(f[0], "`")] = true
			}
		}
	}
}

// TestNullableColumnsScanIntoNullTypes 是本门禁主体：
// schema 里「可空且无 DEFAULT」的列，不得被直接扫进标量/结构体字段。
func TestNullableColumnsScanIntoNullTypes(t *testing.T) {
	if len(riskyNullableColumns) < 1 {
		t.Fatalf("一张表都没解析出来，建表来源读取或解析已失效")
	}
	var findings []string
	judged := 0
	for _, s := range scanSites(t) {
		if !s.judged {
			continue
		}
		judged++
		risky := riskyNullableColumns[s.tbl]
		nullVars := s.nullVars
		for i, col := range s.cols {
			if !risky[col] {
				continue
			}
			arg := strings.TrimSpace(s.args[i])
			if !strings.HasPrefix(arg, "&") {
				continue
			}
			inner := strings.TrimPrefix(arg, "&")
			// []byte 目标对 NULL 安全（driver 置 nil，不报错）——
			// 但类型是声明出来的，Scan 的参数文本里看不到，故按变量名判定。
			base := strings.TrimPrefix(strings.SplitN(inner, ".", 2)[0], "*")
			if strings.HasPrefix(inner, "[]") || s.byteVars[base] {
				continue
			}
			// 嵌套字段取最后一段：&r.Event.TenantID → TenantID → tenant_id。
			// 用第一个点切分会让整类嵌套字段「算作已判定却一条判不到」。
			if idx := strings.LastIndex(inner, "."); idx >= 0 {
				findings = append(findings, lineOf(s, riskyNullableColumns[s.tbl], camelToSnake(inner[idx+1:]), arg))
				continue
			}
			if !nullVars[base] {
				findings = append(findings, lineOf(s, riskyNullableColumns[s.tbl], camelToSnake(base), arg))
			}
		}
	}
	if judged < judgedSitesFloor {
		t.Fatalf("只有 %d 个站点参与判定，低于下限 %d：解析或匹配逻辑已失效（本测试会空跑）",
			judged, judgedSitesFloor)
	}
	if len(findings) > 0 {
		sort.Strings(findings)
		t.Errorf("以下 Scan 把可空且无 DEFAULT 的列扫进了标量目标——遇 NULL 时驱动报 "+
			"converting NULL to ... is unsupported，整行读不出来（记录在列表与直查里同时消失）：\n  %s",
			strings.Join(findings, "\n  "))
	}
}

// TestEveryWideScanSiteIsJudgedOrExempted 把「没被判定的站点」逼到台面上。
// 没有这条，解析失效、列名映射失效、归属失效都会表现为「门禁绿着，但没在看」。
func TestEveryWideScanSiteIsJudgedOrExempted(t *testing.T) {
	var unjudged []string
	judged := 0
	for _, s := range scanSites(t) {
		// args<=2 的标量/聚合读取（SELECT COUNT(*) … Scan(&cnt) 之类）不在本门禁口径内，
		// 既不要求判定也不要求登记。
		if len(s.args) < 3 {
			continue
		}
		if s.judged {
			judged++
			continue
		}
		key := s.file + ":" + strconv.Itoa(s.line)
		if _, ok := scanSiteExemptions[key]; !ok {
			unjudged = append(unjudged, key+"  ("+exemptReason(s)+")")
		}
	}
	if judged < judgedSitesFloor {
		t.Fatalf("只有 %d 个宽站点参与判定，低于下限 %d", judged, judgedSitesFloor)
	}
	if len(unjudged) > 0 {
		sort.Strings(unjudged)
		t.Errorf("以下宽 Scan 站点既未参与判定、也未在 scanSiteExemptions 登记理由：\n  %s\n"+
			"请先确认它真的安全（可空列都落在 sql.Null*/[]byte 目标上，或 SELECT 里有表达式），"+
			"再带理由登记；否则它处于无人看管状态。",
			strings.Join(unjudged, "\n  "))
	}
	var stale []string
	for key := range scanSiteExemptions {
		if !hasSiteAt(key) {
			stale = append(stale, key)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("scanSiteExemptions 里的条目已不存在对应站点（函数被删/改名/行号漂移），"+
			"请清理——否则「曾经合理」会变成永久免检：\n  %s", strings.Join(stale, "\n  "))
	}
}

// exemptReason 给出「为什么没判定」的可读线索，便于判断该不该登记豁免。
func exemptReason(s site) string {
	switch {
	case len(s.args) < 3:
		return fmtInt(len(s.args)) + " 个目标，属标量/聚合读取"
	case s.tbl == "":
		return "归属不到表（SELECT 里有表达式/聚合，或取不到紧邻的 SELECT）"
	case len(s.cols) != len(s.args):
		return "列数 " + fmtInt(len(s.cols)) + " != 目标数 " + fmtInt(len(s.args))
	case len(riskyNullableColumns[s.tbl]) == 0:
		return "表 " + s.tbl + " 没有可空且无 DEFAULT 的列"
	default:
		return "未判定"
	}
}

func fmtInt(n int) string { return strconv.Itoa(n) }

func hasSiteAt(key string) bool {
	i := strings.LastIndex(key, ":")
	if i < 0 {
		return false
	}
	f, ln := key[:i], key[i+1:]
	n, err := strconv.Atoi(ln)
	if err != nil {
		return false
	}
	for _, s := range currentSites {
		if s.file == f && s.line == n {
			return true
		}
	}
	return false
}

type site struct {
	file     string
	line     int
	tbl      string
	cols     []string
	args     []string
	nullVars map[string]bool
	byteVars map[string]bool
	judged   bool
}

// currentSites 供 hasSiteAt 复查，避免同一份文件被重复解析。
var currentSites []site

func lineOf(s site, risky map[string]bool, col, arg string) string {
	return s.file + ":" + strconv.Itoa(s.line) + "  " + s.tbl + "." + col + " → " + arg
}

// scanSites 找出各源文件里的 .Scan(...) 并归属到表。
func scanSites(t *testing.T) []site {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("列举包内文件失败: %v", err)
	}
	var out []site
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		src := string(b)
		nullVars := collectVars(src, `(?m)^\s*var\s+([^\n=]+?)\s+sql\.Null\w+`)
		byteVars := collectVars(src, `(?m)^\s*var\s+([^\n=]+?)\s+\[\]byte`)
		for _, m := range regexp.MustCompile(`(\w+)\s+(?::=|=)\s*\[\]byte`).FindAllStringSubmatch(src, -1) {
			byteVars[m[1]] = true
		}
		for _, m := range regexp.MustCompile(`\.Scan\(`).FindAllStringIndex(src, -1) {
			i, depth, j := m[0]+len(".Scan"), 1, m[0]+len(".Scan")+1
			for ; j < len(src) && depth > 0; j++ {
				switch src[j] {
				case '(':
					depth++
				case ')':
					depth--
				}
			}
			if depth != 0 {
				continue
			}
			var args []string
			for _, a := range splitTopLevel(src[i+1 : j-1]) {
				if strings.TrimSpace(a) != "" {
					args = append(args, a)
				}
			}
			cols, tbl := selectBefore(src[:m[0]])
			s := site{
				file: f, line: strings.Count(src[:m[0]], "\n") + 1,
				tbl: tbl, cols: cols, args: args,
				nullVars: nullVars, byteVars: byteVars,
			}
			// 只有「归属到表 + 列数与目标数吻合 + 该表确有可空无 DEFAULT 列」
			// 才算真的参与判定。列数不等时抓到的是上一条语句的列清单，
			// 拿它当归属等于给自己发免检证。
			s.judged = tbl != "" && len(cols) == len(args) && len(riskyNullableColumns[tbl]) > 0
			out = append(out, s)
		}
	}
	currentSites = out
	return out
}

// collectVars 按正则收集分组变量声明里的所有变量名。
func collectVars(src, pattern string) map[string]bool {
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(pattern).FindAllStringSubmatch(src, -1) {
		for _, n := range strings.Split(m[1], ",") {
			if n = strings.TrimSpace(n); n != "" {
				out[n] = true
			}
		}
	}
	return out
}

// selectBefore 取最近一条能解析出表名的 SELECT 的列清单。
func selectBefore(back string) ([]string, string) {
	clean := stripLineComments(back)
	locs := regexp.MustCompile(`(?is)SELECT\s+(.+?)\s+FROM\s+`+"`?"+`(\w+)`+"`?").
		FindAllStringSubmatchIndex(clean, -1)
	ident := regexp.MustCompile("^`?[A-Za-z_][A-Za-z0-9_]*`?$")
	for i := len(locs) - 1; i >= 0; i-- {
		last := locs[i]
		var cols []string
		ok := true
		for _, c := range splitTopLevel(clean[last[2]:last[3]]) {
			c = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c), "DISTINCT "))
			c = strings.Trim(c, "`")
			if c == "" || strings.Contains(c, "(") || c == "?" || c == "*" || !ident.MatchString(c) {
				ok = false
				break
			}
			cols = append(cols, c)
		}
		if ok && len(cols) >= 2 {
			return cols, clean[last[4]:last[5]]
		}
	}
	return nil, ""
}

// stripLineComments 把 // 之后的行注释替换成等长空格，保持所有偏移不变。
//
// 必须剥：本服务的注释里就写着「与 scanTasks 的 24 列清单严格对齐」这类句子，
// 其中可能同时出现 SELECT 与 FROM <表名>。配合 (?s) 让 `.` 跨行、`.+?` 取最短，
// 正则会latch 到注释里那一句，把一句中文当成列清单 ⇒ 解析不出列 ⇒
// 归属静默失败（站点被判成「不参与判定」，从而不受任何检查）。
// 这是本门禁最危险的形态：绿着，但没在看。
//
// 只处理行注释：本仓库的 SQL 写在普通字符串或反引号里；若将来出现含 // 的
// 字面量导致误剥，具体站点会在 TestEveryWideScanSiteIsJudgedOrExempted
// 里以「归属不到表」显式暴露出来，不会静默。
func stripLineComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i] + strings.Repeat(" ", len(line)-i)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// splitTopLevel 按顶层逗号切分（括号/字符串内的逗号不切）。
func splitTopLevel(s string) []string {
	var out []string
	depth, inStr, start := 0, false, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'' || c == '"' || c == '`':
			inStr = !inStr
		case inStr:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// camelToSnake 把 Go 字段名映射回列名：LastFiredAt → last_fired_at。
func camelToSnake(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			prevLower := i > 0 && runes[i-1] >= 'a' && runes[i-1] <= 'z'
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
			prevUpper := i > 0 && runes[i-1] >= 'A' && runes[i-1] <= 'Z'
			if prevLower || (prevUpper && nextLower) {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
