// nullable_scan_guard_test.go — 全包可空列 ↔ Scan 目标类型门禁（TD-74 收口）。
//
// 背景：TD-74 记录「控制面 internal/store 仍是抽样核对」，且同一形态已在多个库反复发生。
// 本文件把抽样换成**穷举**：任何环境都跑，逐个 .Scan( 站点判定
// 「schema 里可空的列，是否落到了 sql.Null* 目标上」。
//
// 为什么值得单列一个门禁而不是逐处加测试：
// 逐处测试只能守住已经想到的那几处，而本缺陷的形态是
// 「新增一列 / 换一条 SELECT / 加一个读侧方法」时静默复发——
// 漏写症状不是报错，是**整行读不出来**（设备/任务/审计/用户凭空消失），
// 且只在真库 + 特定数据形态下才暴露。
//
// 判定口径（刻意收窄，避免误报把门禁自己变成噪音）：
//   - 列清单来自 migrations/*.sql（单一来源，与 CI integration job 同源）；
//   - 只有「列数 == Scan 目标数」的站点参与判定——列数不等说明 SELECT 里混了
//     表达式/聚合，本门禁不猜，交给列数对账与真库回归；
//   - 该列在 WHERE 里被 IS NOT NULL / IS NULL 显式约束时跳过（NULL 已不可能出现）；
//   - 该列在 SELECT 里被 COALESCE/IFNULL 兜底时跳过。
package sqlstore

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// nullableSchemaColumns 解析migrations/*.sql，得到 table → {该列是否可空}。
var nullableSchemaColumns = buildNullableSchemaColumns()

func buildNullableSchemaColumns() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	files, _ := filepath.Glob(filepath.Join("migrations", "*.sql"))
	for _, f := range files {
		if strings.HasSuffix(f, ".down.sql") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		src := string(b)
		for _, m := range regexp.MustCompile(`(?s)CREATE TABLE(?: IF NOT EXISTS)?\s+(\w+)\s*\((.*?)\n\)`).FindAllStringSubmatch(src, -1) {
			tbl, body := m[1], m[2]
			if _, ok := out[tbl]; !ok {
				out[tbl] = map[string]bool{}
			}
			for _, col := range definedColumns(body) {
				// 任一迁移把它声明成 NOT NULL 即视为非空（最后一次声明说了算）。
				out[tbl][col] = !colDeclaredNotNull(body, col)
			}
		}
	}
	return out
}

// definedColumns 取 CREATE TABLE 体里的列名（跳过约束/索引行）。
func definedColumns(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		if regexp.MustCompile(`^(PRIMARY KEY|UNIQUE|INDEX|KEY|CONSTRAINT|FOREIGN)\b`).MatchString(line) {
			continue
		}
		if f := strings.Fields(line); len(f) >= 2 {
			out = append(out, strings.Trim(f[0], "`"))
		}
	}
	return out
}

func colDeclaredNotNull(body, col string) bool {
	for _, line := range strings.Split(body, "\n") {
		l := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.Trim(l, "` "), col) && !strings.HasPrefix(l, col+" ") {
			continue
		}
		u := strings.ToUpper(l)
		if strings.Contains(u, "NOT NULL") || strings.Contains(u, "AUTO_INCREMENT") || strings.Contains(u, "PRIMARY KEY") {
			return true
		}
	}
	return false
}

// storeGoFiles 返回包内非测试 Go 源文件。
func storeGoFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("列举包内文件失败: %v", err)
	}
	var out []string
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// scanSite 是一次 .Scan( 调用。
type scanSite struct {
	args   []string
	scanAt int // src 中 ".Scan(" 的起始偏移
	line   int
}

// scanSites 找出 src 内全部 .Scan(...) 调用（按括号配平，支持跨行）。
func scanSites(src string) []scanSite {
	var out []scanSite
	for _, m := range regexp.MustCompile(`\.Scan\(`).FindAllStringIndex(src, -1) {
		openIdx := m[0] + len(".Scan")
		depth, j := 1, openIdx+1
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
		inner := src[openIdx+1 : j-1]
		var args []string
		for _, a := range splitTopLevel(inner) {
			if strings.TrimSpace(a) != "" {
				args = append(args, a)
			}
		}
		out = append(out, scanSite{
			args:   args,
			scanAt: m[0],
			line:   strings.Count(src[:m[0]], "\n") + 1,
		})
	}
	return out
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

// selectColumnsBefore 取 scanAt 之前最近一条能解析出表名的 SELECT，返回列名与整条 SELECT。
func selectColumnsBefore(src string, scanAt int) ([]string, string, string) {
	back := src[:scanAt]
	loc := regexp.MustCompile(`(?is)SELECT\s+(.+?)\s+FROM\s+`+"`?"+`(\w+)`+"`?").FindAllStringSubmatchIndex(back, -1)
	if len(loc) == 0 {
		return nil, "", ""
	}
	last := loc[len(loc)-1]
	selBody := back[last[2]:last[3]]
	tbl := back[last[4]:last[5]]
	// 正则只吃到表名为止，WHERE 子句在其后——不补齐的话 guardedByWhere 永远看不到
	// `WHERE col IS NOT NULL`，会把「已显式排除 NULL」的站点误判成缺陷。
	// 本仓库的查询都是反引号/双引号原样字符串，故按下一个引号收敛；引号太远则截断兜底。
	end := last[1]
	if off := strings.IndexAny(back[end:], "`\""); off >= 0 && off < 800 {
		end += off + 1
	} else if end+400 < len(back) {
		end += 400
	}
	sel := back[last[0]:end]
	var cols []string
	for _, c := range splitTopLevel(selBody) {
		c = strings.TrimSpace(c)
		c = strings.TrimPrefix(c, "DISTINCT ")
		c = strings.Trim(c, "`")
		if c == "" || strings.Contains(c, "(") || c == "?" || c == "*" {
			return nil, "", "" // 混入表达式/聚合：整站放弃判定
		}
		cols = append(cols, c)
	}
	if len(cols) < 2 {
		return nil, "", ""
	}
	return cols, tbl, sel
}

// guardedByWhere：该列在 WHERE 里被 IS NOT NULL / IS NULL 约束 ⇒ NULL 到不了扫描。
func guardedByWhere(sel, col string) bool {
	w := sel
	i := strings.Index(strings.ToUpper(w), " WHERE ")
	if i < 0 {
		return false
	}
	w = w[i:]
	if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(col) + `\b\s+(IS\s+NOT\s+NULL|IS\s+NULL)`).MatchString(w) {
		return true
	}
	// `WHERE col<>''` 之类：NULL 与 '' 比较结果为 NULL，该行被排除。
	return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(col) + `\b\s*<>\s*''`).MatchString(w)
}

// coalescedInSelect：该列在 SELECT 里被 COALESCE/IFNULL 兜底。
func coalescedInSelect(sel, col string) bool {
	re := regexp.MustCompile(`(?i)(COALESCE|IFNULL)\s*\(\s*` + "`?" + regexp.QuoteMeta(col) + "`?" + `\s*,`)
	return re.MatchString(sel)
}

// nullVarsIn 收集文件内声明为 sql.Null* 的变量名（含分组声明与 := 形式）。
func nullVarsIn(src string) map[string]bool {
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*var\s+([^\n=]+?)\s+sql\.Null\w+`).FindAllStringSubmatch(src, -1) {
		for _, n := range strings.Split(m[1], ",") {
			if n = strings.TrimSpace(n); n != "" {
				out[n] = true
			}
		}
	}
	for _, m := range regexp.MustCompile(`(\w+)\s*:=\s*sql\.Null\w+\s*\{\}`).FindAllStringSubmatch(src, -1) {
		out[m[1]] = true
	}
	return out
}

// baseIdent 取 &x / &x.Field / &arr[i] 里的基标识符。
// 含 "." 时返回 ""——字段目标的判定交给 TestHelperScansCoverNullableColumns。
func baseIdent(arg string) string {
	a := strings.TrimSpace(arg)
	if strings.Contains(a, ".") {
		return ""
	}
	a = strings.TrimPrefix(strings.TrimPrefix(a, "&"), "*")
	a = strings.TrimSpace(regexp.MustCompile(`\[.*?\]`).ReplaceAllString(a, ""))
	if i := strings.Index(a, "."); i >= 0 {
		a = a[:i]
	}
	return a
}

// enclosingFuncName 返回 offset 所在函数的 `func Name` 部分（取不到返回 "?"）。
// enclosingFuncName 取出包住该站点的函数名，供 helperScanTables 绑定用。
//
// 修过一处会让整类站点静默失明的 bug：原实现取「第一个左括号之前的文本」当函数名，
// 于是方法形态 `func (s *SQLStore) Alerts(...)` 得到的是**空串**（第一个 ( 就是接收者的括号）。
// 后果不是判错而是不判——写在方法里的内联扫描全都登记不进 helperScanTables，
// 而 helper 判定只认「表名 == ?」，空串永远绑不上任何表，这些站点就此长期失明。
// 现在：接收者括号配对跳过后，再取名字。
func enclosingFuncName(src string, at int) string {
	lines := strings.Split(src[:at], "\n")
	for i := len(lines) - 1; i >= 0 && i > len(lines)-80; i-- {
		l := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(l, "func ") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(l, "func "))
		if strings.HasPrefix(rest, "(") { // 方法：先跳过接收者
			depth := 0
			for k := 0; k < len(rest); k++ {
				switch rest[k] {
				case '(':
					depth++
				case ')':
					depth--
					if depth == 0 {
						name := strings.TrimSpace(rest[k+1:])
						if j := strings.IndexAny(name, "( "); j >= 0 {
							name = name[:j]
						}
						return name
					}
				}
			}
			return ""
		}
		if j := strings.IndexAny(rest, "( "); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	return "?"
}

// camelToSnake 把 Go 字段名映射回列名：SnmpCommunity → snmp_community、AgentID → agent_id。
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

func sprintf(f string, line int, tbl, col, arg string) string {
	return f + ":" + strconv.Itoa(line) + "  " + tbl + "." + col + " → " + strings.TrimSpace(arg)
}

// TestNullableColumnsScanIntoNullTypes 覆盖「能从就近 SELECT 归属到表」的站点。
func TestNullableColumnsScanIntoNullTypes(t *testing.T) {
	if len(nullableSchemaColumns) < 20 {
		t.Fatalf("只解析出 %d 张表，migrations 解析已失效（本测试会空跑）", len(nullableSchemaColumns))
	}
	var findings []string
	checkedSites := 0
	for _, f := range storeGoFiles(t) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		src := string(b)
		nullVars := nullVarsIn(src)
		for _, site := range scanSites(src) {
			cols, tbl, sel := selectColumnsBefore(src, site.scanAt)
			if tbl == "" || len(cols) == 0 || len(cols) != len(site.args) {
				continue // 列数不等或归属不到：本门禁不猜
			}
			nullable := nullableSchemaColumns[tbl]
			if len(nullable) == 0 {
				continue
			}
			checkedSites++
			for i, col := range cols {
				if !nullable[col] {
					continue
				}
				if guardedByWhere(sel, col) || coalescedInSelect(sel, col) {
					continue // NULL 不可能到达扫描
				}
				base := baseIdent(site.args[i])
				if base != "" && nullVars[base] {
					continue
				}
				findings = append(findings, sprintf(f, site.line, tbl, col, site.args[i]))
			}
		}
	}
	if checkedSites == 0 {
		t.Fatalf("没有任何站点参与判定，解析/匹配逻辑已失效（本测试会空跑）")
	}
	if len(findings) > 0 {
		t.Errorf("以下 Scan 站点把可空列扫进了标量目标——任一列为 NULL 时驱动报 "+
			"converting NULL to ... is unsupported，整行读不出来（记录/设备/任务凭空消失）：\n  %s",
			strings.Join(findings, "\n  "))
	}
}

// helperScanTables 把 helper 形态读侧绑定到它读的表。
//
// 为什么需要这张表：形如 `func scanXxx(row rowScanner, …) *Xxx` 的公共读侧，
// SELECT 写在调用方，故「就近取上一条 SELECT」的归属方式在这里必然取不到表——
// 只靠归属的判定对它们完全失明。显式绑定表之后，同一套判定就能覆盖到。
var helperScanTables = map[string]string{
	"scanAlertRule":          "alert_rules",
	"scanAPIKey":             "api_keys",
	"scanArgoCDApp":          "argocd_apps",
	"scanAutomationRule":     "automation_rules",
	"scanBackupRecord":       "backup_records",
	"scanBillingPlan":        "billing_plans",
	"scanComplianceReport":   "compliance_reports",
	"scanConfigItem":         "configs",
	"scanInvoice":            "invoices",
	"scanK8sCluster":         "k8s_clusters",
	"scanMiddlewareTemplate": "middleware_templates",
	"scanNetworkDevice":      "network_devices",
	"scanNotifyChannel":      "notify_channels",
	"scanNotifyTemplate":     "notify_templates",
	"scanOSTemplate":         "os_templates",
	"scanPipelineRun":        "pipeline_runs",
	"scanPipelineTemplate":   "pipeline_templates",
	"scanPlugin":             "plugins",
	"scanRefreshToken":       "refresh_tokens",
	"scanRole":               "roles",
	"scanSLO":                "slos",
	"scanScript":             "scripts",
	"scanScriptExecution":    "script_executions",
	"scanSecretMeta":         "secrets",
	"scanServiceInstance":    "services",
	"scanSilence":            "alert_silences",
	"scanSubscription":       "subscriptions",
	"scanTenant":             "tenants",
	"scanTicket":             "tickets",
	"scanTrafficPolicy":      "traffic_policies",
	"scanUser":               "users",
	"scanWebhook":            "webhooks",
	"scanWebhookDelivery":    "webhook_deliveries",

	// 以下 5 条是接手时补的：它们的 SELECT 或写在调用方、或用变量拼出来，
	// 就近归属要么抓不到表、要么抓到**上一条**语句（列数必然不等），
	// 于是「①不判、③不要求登记」两头落空。实测漏掉的正是
	// Alerts（12 列，表里 13 列可空）与审计链读取（10 列）这类大扫描。
	"Alerts":                  "alerts",
	"Alert":                   "alerts",
	"scanAuditRow":            "audit_log",
	"VerifyAuditChain":        "audit_log",
	"scanAutomationExecution": "automation_executions",
	"scanNetworkMetrics":      "network_metrics",
}

// scanSiteExemptions 是「参数 ≥3 列但不参与判定」的**显式账目**，每条必须写理由。
//
// 为什么要有这张表而不是接受"覆盖率 57.5%"：没被登记的大扫描站点，与"确实不需要判定"的
// 聚合查询，在门禁输出一模一样（都是静默跳过）。前者正是 TD-74 复发的入口。
// 强制写明理由 + 断言账目不过期（站点没了就必须删条目），才让"跳过"这件事可审计。
var scanSiteExemptions = map[string]string{
	// 三个 AVG() 聚合：返回的是计算值，不是表行；NULL 由 SQL 层聚合语义处理。
	"sql_network.go#QueryNetworkMetrics": "SELECT AVG(cpu_usage), AVG(memory_usage), AVG(temperature) —— 聚合表达式，无表行可扫描",
}

// judgedSitesFloor 是判定覆盖的**棘轮下限**：只允许在覆盖真的变好时上调。
// 为什么用棘轮而不是百分比：百分比会随代码量涨跌而失去意义，而下限一旦被调低，
// 必须有人为"为什么变差了"写下理由——这正是防"门禁悄悄失去牙齿"的那道刹车。
//
// 56 = 接手时的实测值（全包 87 个 Scan 站点；其中参数 >=3 的宽扫描已全部
// 「参与判定」或「进豁免账」，剩下的 31 处是 args<=2 的标量/聚合读取，如 COUNT(*)、
// SELECT version, checksum）。上调它之前先确认新站点是真的进了判定而不是被顺手豁免。
const judgedSitesFloor = 56

// camelToSnakeOf 供豁免账目匹配使用（文件名#函数名 形态的键）。
func siteKey(file, fn string) string {
	return filepath.Base(file) + "#" + fn
}

// TestHelperScansCoverNullableColumns 对 helper 形态读侧断言：
// schema 可空的列不得被直接扫进结构体字段（&x.Field），必须经局部 sql.Null* 中转。
func TestHelperScansCoverNullableColumns(t *testing.T) {
	if len(helperScanTables) < 30 {
		t.Fatalf("只绑定了 %d 个 helper，登记表已失效", len(helperScanTables))
	}
	var findings []string
	judged := 0
	for _, f := range storeGoFiles(t) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		src := string(b)
		nullVars := nullVarsIn(src)
		for _, site := range scanSites(src) {
			tbl, bound := helperScanTables[enclosingFuncName(src, site.scanAt)]
			if !bound {
				continue
			}
			nullable := nullableSchemaColumns[tbl]
			if len(nullable) == 0 {
				continue
			}
			judged++
			for _, a := range site.args {
				arg := strings.TrimSpace(a)
				if !strings.HasPrefix(arg, "&") {
					continue
				}
				inner := strings.TrimPrefix(arg, "&")
				// 取**最后**一段做列名：`&r.Event.TenantID` 这类嵌套结构体字段，
				// 用第一个点会拿到 "Event.TenantID"，camelToSnake 之后对不上任何列——
				// 于是嵌套字段的站点全部伪装成"已判定且通过"（接手时实测踩到）。
				if i := strings.LastIndex(inner, "."); i >= 0 {
					col := camelToSnake(inner[i+1:])
					if nullable[col] {
						findings = append(findings, sprintf(f, site.line, tbl, col, arg))
					}
					continue
				}
				base := strings.TrimPrefix(inner, "*")
				col := camelToSnake(base)
				if nullable[col] && !nullVars[base] {
					findings = append(findings, sprintf(f, site.line, tbl, col, arg))
				}
			}
		}
	}
	if judged == 0 {
		t.Fatalf("没有 helper 站点参与判定，匹配逻辑已失效（本测试会空跑）")
	}
	if len(findings) > 0 {
		t.Errorf("以下 helper 读侧把可空列直接扫进标量/结构体字段（NULL 会让整行读不出来）：\n  %s",
			strings.Join(findings, "\n  "))
	}
}

// TestHelperScanTablesAreComplete 保证新增 helper 读侧必须绑定表，否则上面对它失明。
func TestHelperScanTablesAreComplete(t *testing.T) {
	var unbound []string
	total := 0
	for _, f := range storeGoFiles(t) {
		b, _ := os.ReadFile(f)
		src := string(b)
		for _, site := range scanSites(src) {
			cols, tbl, _ := selectColumnsBefore(src, site.scanAt)
			// 只有「列数 == 目标数」时才认为就近归属可信：抓到上一条语句的列清单说明归属错位，
			// 此时该站点其实两头不管（①因列数不等跳过、③因"已归属到表"不要求登记）。
			// 实测这样溜过去的是 4 处大扫描，含 13 列可空的 alerts。
			if (tbl != "" && len(cols) == len(site.args)) || len(site.args) < 3 {
				continue
			}
			structField := false
			for _, a := range site.args {
				if strings.Contains(strings.TrimSpace(a), ".") {
					structField = true
				}
			}
			if !structField {
				continue
			}
			total++
			fn := enclosingFuncName(src, site.scanAt)
			if _, ok := helperScanTables[fn]; !ok {
				unbound = append(unbound, f+":"+strconv.Itoa(site.line)+" "+fn)
			}
		}
	}
	if total == 0 {
		t.Fatalf("没找到 helper 形态读侧，匹配逻辑已失效（本测试会空跑）")
	}
	if len(unbound) == 0 {
		return // 全部已绑定：这是期望状态
	}
	sort.Strings(unbound)
	t.Errorf("新增 helper 读侧但未在 helperScanTables 绑定表（门禁对它失明）：\n  %s",
		strings.Join(unbound, "\n  "))
}

// TestEveryWideScanSiteIsJudgedOrExempted 断言：参数 >=3 的扫描站点要么参与判定，
// 要么在 scanSiteExemptions 里带着理由被显式豁免；并守 judgedSitesFloor 棘轮。
//
// 为什么必须有这条：前面两个测试"通过"只说明**看到的地方**没问题，不说明看见了多少。
// TD-74 的原罪正是抽样——把抽样的沉默换成可审计的账目，才算收口。
func TestEveryWideScanSiteIsJudgedOrExempted(t *testing.T) {
	judged := map[string]bool{}
	seenKeys := map[string]bool{}
	var unaccounted []string
	for _, f := range storeGoFiles(t) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		src := string(b)
		for _, site := range scanSites(src) {
			fn := enclosingFuncName(src, site.scanAt)
			key := f + ":" + strconv.Itoa(site.line)
			cols, tbl, _ := selectColumnsBefore(src, site.scanAt)
			if tbl != "" && len(cols) == len(site.args) && len(nullableSchemaColumns[tbl]) > 0 {
				judged[key] = true // ①：就近 SELECT 可信
			}
			if htbl, ok := helperScanTables[fn]; ok && len(nullableSchemaColumns[htbl]) > 0 {
				judged[key] = true // ②：登记表绑定表名
			}
			seenKeys[siteKey(f, fn)] = true
			if len(site.args) < 3 || judged[key] {
				continue
			}
			if _, ok := scanSiteExemptions[siteKey(f, fn)]; ok {
				continue
			}
			unaccounted = append(unaccounted, key+" func="+fn+
				" args="+strconv.Itoa(len(site.args))+" 就近归属表="+tbl+"（归属错位或无表）")
		}
	}
	if len(unaccounted) > 0 {
		sort.Strings(unaccounted)
		t.Errorf("以下站点既不参与判定、也不在豁免账上，等于对门禁隐形——正是 TD-74 的复发入口：\n  %s\n"+
			"  处置：能判定就在 helperScanTables 绑定表名；确属聚合/表达式就在 scanSiteExemptions 写明理由",
			strings.Join(unaccounted, "\n  "))
	}
	// 过期豁免必须删掉：留着会让"曾经合理"变成永久免检。
	for k := range scanSiteExemptions {
		if !seenKeys[k] {
			t.Errorf("豁免账里的 %q 已找不到对应站点（过期，请删掉这条）", k)
		}
	}
	if n := len(judged); n < judgedSitesFloor {
		t.Errorf("参与判定的站点数从下限 %d 掉到 %d：要么新增了未登记的读侧站点，要么解析逻辑失效；"+
			"确需下调必须在这里写明理由", judgedSitesFloor, n)
	}
}
