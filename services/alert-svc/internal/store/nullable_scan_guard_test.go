// nullable_scan_guard_test.go — alert-svc 读侧可空列守卫（TD-74 同族，为本服务的展开形态特化）。
//
// 为什么本服务需要单独一层：读侧扫描点统一走 helper（nullAlert/nullAlertRule）的
// dest() 展开——`rows.Scan(na.dest()...)` 的参数只有 1 个，「列清单 ↔ Scan 目标」
// 的就近归属对展开形态天生失明。旧门禁（mysql_scan_test.go）只验证 helper 自身
// 安全（结构体字段都是 sql.Null*），**不验证每个扫描点都走 helper**：将来有人
// 绕过 helper 给 silences（schema 第三张表，今天没有读侧）写一个裸扫
// ListSilences，旧门禁不会红。
//
// 本守卫把账目补齐——每个 .Scan( 站点必须落到三格之一：
//  1. **注册过的 helper 展开**：变量类型在 helperScanTables 里绑定了表，
//     字段安全性由 TestHelperScanTablesAreComplete 逐列对账；
//  2. **列数与目标数吻合、可空列全落在 sql.Null*/[]byte 目标**（模板判定，
//     含剥行注释防中文注释里的 SELECT 干扰归属、嵌套字段按最后一段推列名）；
//  3. **带理由登记 scanSiteExemptions**——豁免条目过期即判红。
//
// 覆盖账目 + judgedSitesFloor 棘轮：覆盖回退时守卫必须变红，而不是安静地少看几个站点。
package store

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// helperScanTables 登记 helper 类型 → 绑定表。注册的含义：该类型 dest() 的每个
// 目标对绑定表的可空列都经 sql.Null* 中转，由 TestHelperScanTablesAreComplete
// 按结构体字段逐列对账。新增 helper 读侧必须注册，否则其站点在记账测试里判红。
var helperScanTables = map[string]string{
	"nullAlert":     "alerts",
	"nullAlertRule": "alert_rules",
}

// judgedSitesFloor 是「参与判定的宽站点数」的下限，只许上调。
// 当前 4 = 4 个 helper 展开站点（alerts 两个 + alert_rules 两个）。
const judgedSitesFloor = 4

// scanSiteExemptions 登记不参与判定、也不是注册 helper 展开的站点及理由。
// 键是 "文件名:行号"。
var scanSiteExemptions = map[string]string{}

// riskyNullableColumns 解析建表来源，得到 table → {可空且无 DEFAULT 的列}。
var riskyNullableColumns = buildRiskyNullableColumns()

func buildRiskyNullableColumns() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	if b, err := os.ReadFile("schema.sql"); err == nil {
		parseCreateTables(string(b), out)
	}
	// schema.sql 不存在或漏表时退回 Go 内联 DDL（本服务 initSchema 也建表）。
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

type site struct {
	file     string
	line     int
	tbl      string
	cols     []string
	args     []string
	spread   string // 非空 = dest() 展开形态，值为 helper 变量名
	nullVars map[string]bool
	byteVars map[string]bool
	judged   bool
}

// currentSites 供 hasSiteAt 复查，避免同一份文件被重复解析。
var currentSites []site

func lineOf(s site, col, arg string) string {
	return s.file + ":" + strconv.Itoa(s.line) + "  " + s.tbl + "." + col + " → " + arg
}

func siteKey(s site) string {
	return s.file + ":" + strconv.Itoa(s.line)
}

// scanSites 找出各源文件里的 .Scan(...) 并归属：展开站点按注册的 helper 类型
// 绑表，普通站点按就近可解析的 SELECT 列清单归属。
func scanSites(t *testing.T) []site {
	t.Helper()
	types := make([]string, 0, len(helperScanTables))
	for k := range helperScanTables {
		types = append(types, k)
	}
	sort.Strings(types)
	typeAlt := strings.Join(types, "|")
	varDeclRe := regexp.MustCompile(`(?m)^\s*var\s+(\w+)\s+(` + typeAlt + `)\s*$`)
	shortDeclRe := regexp.MustCompile(`(?m)^\s*(\w+)\s*:=\s*(` + typeAlt + `)\{\s*$`)
	spreadRe := regexp.MustCompile(`^(\w+)\.dest\(\)\.\.\.$`)

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
		helperVars := map[string]string{}
		for _, m := range varDeclRe.FindAllStringSubmatch(src, -1) {
			helperVars[m[1]] = m[2]
		}
		for _, m := range shortDeclRe.FindAllStringSubmatch(src, -1) {
			helperVars[m[1]] = m[2]
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
			s := site{
				file: f, line: strings.Count(src[:m[0]], "\n") + 1,
				args: args, nullVars: nullVars, byteVars: byteVars,
			}
			// 展开形态：`X.dest()...`。注册过的类型按绑定表覆盖；
			// 未注册的类型留给记账测试判红（新 helper 必须注册）。
			if len(args) == 1 {
				if sm := spreadRe.FindStringSubmatch(strings.TrimSpace(args[0])); sm != nil {
					s.spread = sm[1]
					if typ, ok := helperVars[sm[1]]; ok {
						s.tbl = helperScanTables[typ]
						s.judged = true
					}
					out = append(out, s)
					continue
				}
			}
			cols, tbl := selectBefore(src[:m[0]])
			s.tbl = tbl
			s.cols = cols
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

// TestEveryWideScanSiteIsJudgedOrExempted 把「没被判定的站点」逼到台面上。
// 没有这条，解析失效、helper 注册失效、列名映射失效都会表现为「门禁绿着，但没在看」。
func TestEveryWideScanSiteIsJudgedOrExempted(t *testing.T) {
	var unjudged []string
	judged := 0
	for _, s := range scanSites(t) {
		if s.spread != "" {
			if s.judged {
				judged++
			} else {
				unjudged = append(unjudged, siteKey(s)+
					"  (dest() 展开但变量 "+s.spread+" 的类型未注册进 helperScanTables——"+
					"注册后由 TestHelperScanTablesAreComplete 对账字段)")
			}
			continue
		}
		// args<=2 的标量/聚合读取（SELECT COUNT(*) … Scan(&cnt) 之类）不在本门禁口径内。
		if len(s.args) < 3 {
			continue
		}
		if s.judged {
			judged++
			continue
		}
		if _, ok := scanSiteExemptions[siteKey(s)]; !ok {
			unjudged = append(unjudged, siteKey(s)+"  ("+exemptReason(s)+")")
		}
	}
	if judged < judgedSitesFloor {
		t.Fatalf("只有 %d 个站点参与判定，低于下限 %d：解析、归属或 helper 注册已失效（本测试会空跑）",
			judged, judgedSitesFloor)
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

// TestNullableColumnsScanIntoNullTypes 判普通（非展开）站点：schema 里
// 「可空且无 DEFAULT」的列，不得被直接扫进标量/结构体字段。
// 展开站点的安全性由 TestHelperScanTablesAreComplete 按绑定表逐列对账，此处不重复。
func TestNullableColumnsScanIntoNullTypes(t *testing.T) {
	if len(riskyNullableColumns) < 1 {
		t.Fatalf("一张表都没解析出来，建表来源读取或解析已失效")
	}
	var findings []string
	for _, s := range scanSites(t) {
		if s.spread != "" || !s.judged {
			continue
		}
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
				findings = append(findings, lineOf(s, camelToSnake(inner[idx+1:]), arg))
				continue
			}
			if !nullVars[base] {
				findings = append(findings, lineOf(s, camelToSnake(base), arg))
			}
		}
	}
	if len(findings) > 0 {
		sort.Strings(findings)
		t.Errorf("以下 Scan 把可空且无 DEFAULT 的列扫进了标量目标——遇 NULL 时驱动报 "+
			"converting NULL to ... is unsupported，整行读不出来（记录在列表与直查里同时消失）：\n  %s",
			strings.Join(findings, "\n  "))
	}
}

// TestHelperScanTablesAreComplete 对账注册 helper 的字段安全性：
// 绑定表 schema 里没写 NOT NULL 的列，helper 结构体对应字段必须是 sql.Null*。
// 判据取源码字面量而不是结构体字段类型——用结构体当「读侧是否安全」的判据
// 会把缺陷本身当成事实。新增 helper（如将来的 nullSilence）注册进
// helperScanTables 即自动纳入本对账。
func TestHelperScanTablesAreComplete(t *testing.T) {
	schema := readInto(t, "schema.sql")
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("列举包内文件失败: %v", err)
	}
	var srcs []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatalf("读取 %s 失败: %v", f, rerr)
		}
		srcs = append(srcs, string(b))
	}
	for typ, table := range helperScanTables {
		if !regexp.MustCompile(`CREATE TABLE IF NOT EXISTS ` + table + ` \(`).MatchString(schema) {
			t.Fatalf("schema.sql 里找不到表 %s 的建表语句（helper %s 的绑定已失效，本测试会空跑）", table, typ)
		}
		var block string
		found := false
		for _, src := range srcs {
			if b := extractBetween(src, "type "+typ+" struct {", "\n}"); b != "" {
				block, found = b, true
				break
			}
		}
		if !found {
			t.Errorf("helper %s 已注册但找不到其结构体声明", typ)
			continue
		}
		for _, col := range nullableColumns(schema, table) {
			f := goFieldName(col)
			if !regexp.MustCompile(`\b` + f + `\s+sql\.Null`).MatchString(block) {
				t.Errorf("列 %s 在 schema 里可空，但 %s.%s 不是 sql.Null* 目标（NULL 会让整行读不出来）", col, typ, f)
			}
		}
	}
}

// exemptReason 给出「为什么没判定」的可读线索，便于判断该不该登记豁免。
func exemptReason(s site) string {
	switch {
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
// 必须剥：注释里可能同时出现 SELECT 与 FROM <表名>。配合 (?s) 让 `.` 跨行、
// `.+?` 取最短，正则会 latch 到注释里那一句，把一句中文当成列清单 ⇒
// 解析不出列 ⇒ 归属静默失败（站点被判成「不参与判定」，从而不受任何检查）。
// 这是本门禁最危险的形态：绿着，但没在看。
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
