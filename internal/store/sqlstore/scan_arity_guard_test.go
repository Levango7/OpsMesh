// scan_arity_guard_test.go — SELECT 列清单 ↔ 消费侧 Scan 目标数 的**对账**门禁（TD-90 落地）。
//
// 先导背景：nullable_scan_guard_test.go 的判定口径刻意收窄为「列数 == 目标数才判」，
// 并在注释里把列数不等的站点留给了「列数对账」（即本文件）。而 2026-10-10 的真实缺陷
// `sqlstore.GetK8sCluster` 恰恰是列数不等：SELECT 7 列 / scanK8sCluster 的 Scan 8 个目标
// ⇒ row.Scan 必然报错、函数只见 nil（按 ID 查/改/删与租户归属校验全部恒失败）；
// 内存后端正确 + 该读取路径此前没有真库用例 ⇒ 长期潜伏。本门禁把这类「列数与目标数错位」
// 从只能靠真库撞见，变成静态可判。
//
// 判据链（只认能静态证实的，不猜）：
//  1. 找查询调用（Query/QueryRow/QueryContext/QueryRowContext）与它的 SQL 参数；
//  2. SQL 参数做字面量展开：函数内 `q := "SELECT " + cols + " FROM t"` 与包级/包级块内的
//     `cols = "a, b, " + "c"` 跨行拼接都还原成完整字符串；
//  3. SELECT 列表按顶层逗号计数（括号内逗号不计 ⇒ COALESCE(a,b) 记 1 列；`*`/`?`/残留标识符 ⇒ 不可判）；
//  4. 消费侧三种形态：
//     - 直接 `rows.Scan(...)`（同函数内、接收者即该变量）；
//     - 链式 `...QueryRowContext(...).Scan(...)`（无中间变量）；
//     - helper `scanX(row)` / `scanX(rows)`（目标数取自该 helper 函数体内的 Scan）。
//  5. 不可判的宽站点（SELECT 列数 ≥3）进豁免账（键= 文件#函数，避免行号漂移），
//     判红门槛之外还有「判定覆盖下限」，防「绿着，但没在看」。
package sqlstore

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ---------- 对账（报告模式与判红门禁共用同一份采集，避免两份逻辑漂移） ----------

type arityStats struct {
	judged       int               // SELECT 可判且与所有消费侧一致
	mismatches   []string          // 列数 ≠ 消费侧目标数（真缺陷形态）
	unresolved   int               // SQL 表达式解析不了（运行时拼接）
	uncountable  int               // 列数数不出来（SELECT */含 ?）
	noConsumer   int               // SQL 可判但找不到消费侧
	wideUnjudged map[string]string // 消费侧 ≥3 而 SQL 不可判（键 file#fn#var）——需扩展解析或登记豁免
	forms        map[string]int    // 消费形态计数（direct/chained/helper）
}

// collectArity 遍历全包非测试文件，对每个查询调用做「SELECT 列数 ↔ 消费侧 Scan 目标数」对账。
func collectArity(t *testing.T) arityStats {
	t.Helper()
	st := arityStats{forms: map[string]int{}, wideUnjudged: map[string]string{}}
	markWide := func(f, fn, varName, why string, consumers []scanConsumer) {
		for _, c := range consumers {
			if c.arity >= 3 {
				key := filepathBase(f) + "#" + fn + "#" + varName
				st.wideUnjudged[key] = fmt.Sprintf("%s（%s；消费侧 %s(%s)=%d，%s）",
					key, why, c.via, c.kind, c.arity, why)
				return
			}
		}
	}
	for _, f := range storeGoFiles(t) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		src := string(b)
		res := newSQLResolver(src)
		for _, qs := range queryCallSites(src) {
			if qs.sqlExpr == "" {
				st.unresolved++
				continue
			}
			consumers := scanConsumers(src, qs)
			sqlVal, ok := res.resolveExprValue(qs.sqlExpr, qs.fn)
			if !ok {
				st.unresolved++
				markWide(f, qs.fn, qs.varName, "SQL 运行时拼接（fmt.Sprintf/未知标识符），静态不可判", consumers)
				continue
			}
			n, ok := selectColumnCount(sqlVal)
			if !ok {
				st.uncountable++
				markWide(f, qs.fn, qs.varName, "SELECT 含 * / ? 等静态数不出的项", consumers)
				continue
			}
			if len(consumers) == 0 {
				st.noConsumer++
				markWide(f, qs.fn, qs.varName, "找不到消费侧 Scan（rows 被传去别处）", nil)
				st.mismatches = append(st.mismatches, fmt.Sprintf(
					"%s:%d fn=%s var=%s——SELECT %d 列但找不到消费侧 Scan（rows 传去别处？此处静默失明）  sql=%.70s",
					f, qs.line, qs.fn, qs.varName, n, oneLine(sqlVal)))
				continue
			}
			for _, c := range consumers {
				st.forms[c.kind]++
				// judged 统计「**参与了比对**」的站点（含不一致者）：下限防的是解析面塌缩，
				// 不是掩盖真缺陷——若只在一致时才计数，一处真缺陷会先撞下限，
				// 报出「判定数只有 125」而把「哪一行列数不等」埋掉（变异检验踩过）。
				st.judged++
				if c.arity != n {
					st.mismatches = append(st.mismatches, fmt.Sprintf(
						"%s:%d fn=%s var=%s——SELECT %d 列 vs %s(%s) 扫 %d 个目标  sql=%.70s",
						f, qs.line, qs.fn, qs.varName, n, c.via, c.kind, c.arity, oneLine(sqlVal)))
				}
			}
		}
	}
	return st
}

func filepathBase(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// arityJudgedFloor 参与比对的站点数棘轮下限（只许上调）。
// 127 = 2026-10-10 落地时的实测值（全包 136 个查询调用；不可解析 6、列数不可判 3，
// 其余全部参与比对）。覆盖变差必须有人写下理由——防「门禁悄悄失去牙齿」。
const arityJudgedFloor = 127

// arityWideExemptions 是「消费侧 ≥3 而 SQL 不可判」的显式账目（键 file#fn#var）。
// 当前为空：三种消费形态（direct/chained/helper）与包级/函数级字面量展开已覆盖全部宽站点。
// 空表也是合法状态；条目必须带理由，且站点消失/被解析成功后必须删除（过期即判红）。
var arityWideExemptions = map[string]string{}

// TestSelectColumnCountMatchesScanTargets 是 TD-90 的主体门禁：
// 每个能静态解析出 SQL 的查询，其 SELECT 列数必须与消费侧 Scan 的目标数一致。
//
// 缺陷形态（已发生两次，均只在真库/生产暴露）：`rows.Scan` 列数不等时报
// `expected N destination arguments in Scan, not M`，调用方通常只记 storefail 然后
// 返回 nil/continue ⇒ 该读路径**静默返回空列表**（内存后端正确故单测全绿）。
// 历史实例：`GetK8sCluster`（SELECT 7 / Scan 8）、`GetTasks`（SELECT 8 / Scan 9）。
func TestSelectColumnCountMatchesScanTargets(t *testing.T) {
	st := collectArity(t)
	if st.judged < arityJudgedFloor {
		t.Errorf("参与比对的站点只有 %d 个（< 下限 %d）——解析或匹配逻辑失效，本门禁在空跑；"+
			"先跑 OPSMESH_ARITY_REPORT=1 看明细", st.judged, arityJudgedFloor)
	}
	if len(st.mismatches) > 0 {
		sort.Strings(st.mismatches)
		t.Errorf("以下站点的 SELECT 列数 ≠ 消费侧 Scan 目标数——database/sql 会直接报 "+
			"expected N destination arguments in Scan, not M，调用方多半只记 storefail 就返回空，"+
			"该读路径在真库上静默失效（GetK8sCluster、GetTasks 两次真实缺陷即此形态）：\n  %s",
			strings.Join(st.mismatches, "\n  "))
	}
	var unregistered []string
	for key, desc := range st.wideUnjudged {
		if _, ok := arityWideExemptions[key]; !ok {
			unregistered = append(unregistered, desc)
		}
	}
	if len(unregistered) > 0 {
		sort.Strings(unregistered)
		t.Errorf("以下宽扫描站点（消费侧 ≥3 个目标）静态判不出 SELECT 列数，且未登记豁免：\n  %s\n"+
			"先尝试扩展解析（字面量/常量拼接的最常见形态已覆盖）；确属运行时拼接的，带理由登记为豁免",
			strings.Join(unregistered, "\n  "))
	}
	for key := range arityWideExemptions {
		if _, ok := st.wideUnjudged[key]; !ok {
			t.Errorf("arityWideExemptions 的条目 %q 已不对应任何未判定站点（解析已覆盖或站点已删）——"+
				"请清理，否则「曾经合理」会变成永久免检", key)
		}
	}
}

// TestSelectArityReport 诊断模式（不判红）：设 OPSMESH_ARITY_REPORT=1 打印全包对账明细，
// 供扩展解析能力、调整下限、排查失明站点时看真实数据。
func TestSelectArityReport(t *testing.T) {
	if os.Getenv("OPSMESH_ARITY_REPORT") == "" {
		t.Skip("报告模式：设 OPSMESH_ARITY_REPORT=1 运行")
	}
	st := collectArity(t)
	t.Logf("判定一致 %d（下限 %d）；SQL 不可解析 %d；列数不可判 %d；无消费侧 %d；宽站点未判定 %d",
		st.judged, arityJudgedFloor, st.unresolved, st.uncountable, st.noConsumer, len(st.wideUnjudged))
	for _, k := range sortedKeys(st.forms) {
		t.Logf("  消费形态：%-8s %d", k, st.forms[k])
	}
	for _, l := range st.mismatches {
		t.Logf("  MISMATCH %s", l)
	}
	for key, desc := range st.wideUnjudged {
		t.Logf("  WIDE-UNJUDGED %s → %s", key, desc)
	}
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---------- 字面量展开 ----------

// sqlResolver 把「名字 → 字符串字面量」按作用域分两层：
// 包级（const/var 声明，含 const ( ... ) 块）与函数级（函数体内的 := / = 赋值）。
// 同名多值一律丢弃——宁可弃判，不可猜错。
type sqlResolver struct {
	pkg map[string]string
	fn  map[string]map[string]string
}

func newSQLResolver(src string) *sqlResolver {
	r := &sqlResolver{pkg: map[string]string{}, fn: map[string]map[string]string{}}
	r.collectPkgLevel(src)
	for _, fb := range functionBodies(src) {
		r.collectFnLevel(fb)
	}
	return r
}

// resolveExprValue 把「字面量 + 标识符」拼接的 SQL 表达式求值为**最终字符串**。
// 与 resolve 的区别：本函数把字面量拆掉引号后再拼接，结果里不再残留反引号——
// 残留会让 splitTopLevel 把整个列表误判成「一个未闭合字符串」（早期版本 SELECT 列表
// 全被数成 1 列的假阳性即此因）。任一环不可解析（未知标识符/函数调用）即 ok=false。
func (r *sqlResolver) resolveExprValue(expr, fn string) (string, bool) {
	return r.resolveParts(expr, fn, 0)
}

func (r *sqlResolver) resolveParts(expr, fn string, depth int) (string, bool) {
	if depth > 4 {
		return "", false
	}
	var b strings.Builder
	for i := 0; i < len(expr); {
		switch c := expr[i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '+':
			i++
		case c == '`':
			end := strings.IndexByte(expr[i+1:], '`')
			if end < 0 {
				return "", false
			}
			b.WriteString(expr[i+1 : i+1+end])
			i += end + 2
		case c == '"':
			j := i + 1
			for j < len(expr) && expr[j] != '"' {
				if expr[j] == '\\' && j+1 < len(expr) {
					j++
				}
				b.WriteByte(expr[j])
				j++
			}
			if j >= len(expr) {
				return "", false
			}
			i = j + 1
		case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			j := i + 1
			for j < len(expr) && (expr[j] == '_' || (expr[j] >= 'a' && expr[j] <= 'z') ||
				(expr[j] >= 'A' && expr[j] <= 'Z') || (expr[j] >= '0' && expr[j] <= '9')) {
				j++
			}
			id := expr[i:j]
			val, ok := r.lookup(id, fn)
			if !ok {
				return "", false
			}
			// 查表命中的是**已求值的字面量内容**（收集阶段已完成拼接与去引号），原样写入；
			// 若再当表达式解析，会因值里的逗号/问号等 SQL 字符而失败
			// （早期版本即此错：常量全都"解析不动"，14 处站点被迫进不可判桶）。
			b.WriteString(val)
			i = j
		default:
			return "", false
		}
	}
	return b.String(), true
}

func (r *sqlResolver) lookup(id, fn string) (string, bool) {
	if local := r.fn[fn]; local != nil {
		if v, ok := local[id]; ok {
			return v, true
		}
	}
	v, ok := r.pkg[id]
	return v, ok
}

var literalHeadRe = regexp.MustCompile(`([A-Za-z_]\w*)\s*:?=\s*`)

// collectPkgLevel 解析包级声明：`const x = ...` / `var x = ...` 以及 `const ( x = ... )` 块内条目。
func (r *sqlResolver) collectPkgLevel(src string) {
	seen := map[string]map[string]bool{}
	add := func(name, val string) {
		if seen[name] == nil {
			seen[name] = map[string]bool{}
		}
		seen[name][val] = true
	}
	// 块形态：^const ( / ^var ( 到 ^) 之间，条目按行找 name = <chain>。
	blockRe := regexp.MustCompile(`(?ms)^(?:const|var)\s*\(\s*\n(.*?)\n\)`)
	for _, m := range blockRe.FindAllStringSubmatch(src, -1) {
		for _, line := range strings.Split(m[1], "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "//") {
				continue
			}
			lm := literalHeadRe.FindStringSubmatchIndex(line)
			if lm == nil {
				continue
			}
			val, _, ok := parseLiteralChain(line, lm[1])
			if ok {
				add(line[lm[2]:lm[3]], val)
			}
		}
	}
	// 单条形态：行首 const x = ... / var x = ...（可跨行拼接）。
	for _, m := range regexp.MustCompile(`(?m)^(?:const|var)\s+([A-Za-z_]\w*)\s*=\s*`).FindAllStringSubmatchIndex(src, -1) {
		val, _, ok := parseLiteralChain(src, m[1])
		if ok {
			add(src[m[2]:m[3]], val)
		}
	}
	for name, vals := range seen {
		if len(vals) == 1 {
			for v := range vals {
				r.pkg[name] = v
			}
		}
	}
}

// collectFnLevel 解析函数体内的赋值（:= 与 =），按函数归一。
func (r *sqlResolver) collectFnLevel(fb funcBodyInfo) {
	fnName := fb.name
	body := fb.body
	if fnName == "" || fnName == "?" {
		return
	}
	seen := map[string]map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^[ \t]+([A-Za-z_]\w*)\s*:?=\s*`).FindAllStringSubmatchIndex(body, -1) {
		val, _, ok := parseLiteralChainResolved(body, m[1], func(id string) (string, bool) {
			v, ok := r.pkg[id]
			return v, ok
		})
		if !ok {
			continue
		}
		name := body[m[2]:m[3]]
		if seen[name] == nil {
			seen[name] = map[string]bool{}
		}
		seen[name][val] = true
	}
	local := map[string]string{}
	for name, vals := range seen {
		if len(vals) == 1 {
			for v := range vals {
				local[name] = v
			}
		}
	}
	if len(local) > 0 {
		if r.fn[fnName] == nil {
			r.fn[fnName] = local
		} else {
			for k, v := range local {
				r.fn[fnName][k] = v
			}
		}
	}
}

// parseLiteralChain 从 from 起解析「字符串字面量 (+ 字符串字面量)*」的拼接链。
// 支持跨行；遇到非字面量（函数调用/未知标识符）即失败（宁弃判）。
func parseLiteralChain(src string, from int) (string, int, bool) {
	return parseLiteralChainResolved(src, from, nil)
}

// parseLiteralChainResolved 同 parseLiteralChain，但可用 resolve 展开标识符引用——
// 函数内的 `q := "SELECT " + cols + " FROM t"` 就靠它才收得进函数级常量表
// （收不到就整类站点不可判，实测 7 处宽站点因此失明）。
func parseLiteralChainResolved(src string, from int, resolve func(string) (string, bool)) (string, int, bool) {
	var b strings.Builder
	i := from
	for {
		for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
			i++
		}
		if i >= len(src) {
			return "", 0, false
		}
		switch src[i] {
		case '`':
			end := strings.IndexByte(src[i+1:], '`')
			if end < 0 {
				return "", 0, false
			}
			b.WriteString(src[i+1 : i+1+end])
			i += end + 2
		case '"':
			j := i + 1
			var sb strings.Builder
			for j < len(src) && src[j] != '"' {
				if src[j] == '\\' && j+1 < len(src) {
					j++
				}
				sb.WriteByte(src[j])
				j++
			}
			if j >= len(src) {
				return "", 0, false
			}
			b.WriteString(sb.String())
			i = j + 1
		default:
			if resolve == nil {
				return "", 0, false
			}
			j := i
			for j < len(src) && (src[j] == '_' || (src[j] >= 'a' && src[j] <= 'z') ||
				(src[j] >= 'A' && src[j] <= 'Z') || (src[j] >= '0' && src[j] <= '9')) {
				j++
			}
			if j == i {
				return "", 0, false
			}
			val, ok := resolve(src[i:j])
			if !ok {
				return "", 0, false
			}
			b.WriteString(val)
			i = j
		}
		for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\r') {
			i++
		}
		if i < len(src) && src[i] == '\n' {
			// 跨行仅在下一行是延续（以 + 开头）时成立；否则链结束。
			k := i
			for k < len(src) && (src[k] == '\n' || src[k] == ' ' || src[k] == '\t' || src[k] == '\r') {
				k++
			}
			if k < len(src) && src[k] == '+' {
				i = k
			} else {
				return b.String(), i, true
			}
		}
		if i < len(src) && src[i] == '+' {
			i++
			continue
		}
		return b.String(), i, true
	}
}

// ---------- 查询与消费侧 ----------

type querySite struct {
	varName string
	sqlExpr string
	line    int
	at      int
	fn      string
	chained int // 链式 .Scan(...) 的目标数；无链式 = -1
}

func queryCallSites(src string) []querySite {
	var out []querySite
	re := regexp.MustCompile(`\.(Query|QueryRow)(Context)?\(`)
	for _, m := range re.FindAllStringIndex(src, -1) {
		open := strings.Index(src[m[0]:m[1]], "(") + m[0]
		inner, closeIdx, ok := parenSpan(src, open)
		if !ok {
			continue
		}
		args := splitTopLevel(inner)
		// 取 SQL 实参：优先含 SELECT 或反引号的实参（Go 形态的 SQL 字面量），
		// 最后才回退到裸标识符——否则会把第一个实参 `ctx` 当成 SQL（早期版本即此错）。
		sqlExpr := ""
		for _, want := range []func(string) bool{
			func(a string) bool { return strings.Contains(strings.ToUpper(a), "SELECT") },
			func(a string) bool { return strings.Contains(a, "`") },
			func(a string) bool { return isIdentLike(a) && !isCommonNonSQLIdent(a) },
		} {
			for _, a := range args {
				if want(a) {
					sqlExpr = strings.TrimSpace(a)
					break
				}
			}
			if sqlExpr != "" {
				break
			}
		}
		chained := -1
		if m := regexp.MustCompile(`^\s*\.\s*Scan\(`).FindStringIndex(src[closeIdx+1:]); m != nil {
			so := closeIdx + 1 + m[1] - 1
			if sin, _, ok := parenSpan(src, so); ok {
				chained = countTopLevelNonEmpty(sin)
			}
		}
		out = append(out, querySite{
			varName: assignTargetBefore(src, m[0]),
			sqlExpr: sqlExpr,
			line:    strings.Count(src[:m[0]], "\n") + 1,
			at:      m[0],
			fn:      enclosingFuncName(src, m[0]),
			chained: chained,
		})
	}
	return out
}

func isIdentLike(s string) bool {
	s = strings.TrimSpace(s)
	return regexp.MustCompile(`^[A-Za-z_]\w*$`).MatchString(s)
}

// isCommonNonSQLIdent 排掉「一眼不是 SQL」的常见实参名（上下文/租户/ID 等），
// 免得取参回退到裸标识符时把它们当成 SQL。
func isCommonNonSQLIdent(s string) bool {
	switch strings.TrimSpace(s) {
	case "ctx", "context", "db", "id", "tenantID", "tenantId", "tenant", "userID", "user", "k", "key", "name":
		return true
	}
	return false
}

// parenSpan 返回 offset（指向 '('）配对括号内的内容与右括号下标。
func parenSpan(src string, open int) (string, int, bool) {
	if open >= len(src) || src[open] != '(' {
		return "", 0, false
	}
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[open+1 : i], i, true
			}
		}
	}
	return "", 0, false
}

// assignTargetBefore 从调用位置往回找本语句的赋值目标变量（`x := ...` / `x, err := ...`）。
func assignTargetBefore(src string, at int) string {
	start := at - 400
	if start < 0 {
		start = 0
	}
	seg := src[start:at]
	if i := strings.LastIndexAny(seg, ";\n}"); i >= 0 {
		seg = seg[i+1:]
	}
	seg = strings.TrimSpace(seg)
	if seg == "" {
		return ""
	}
	m := regexp.MustCompile(`^(\w+)\s*(?:,\s*\w+)?\s*(?::=|=)`).FindStringSubmatch(seg)
	if m == nil {
		return ""
	}
	return m[1]
}

// selectColumnCount 数 SELECT 列表的顶层项数。不可判时 ok=false。
func selectColumnCount(sql string) (int, bool) {
	i := strings.Index(strings.ToUpper(sql), "SELECT")
	if i < 0 {
		return 0, false
	}
	rest := sql[i+len("SELECT"):]
	depth := 0
	fromAt := -1
	for j := 0; j < len(rest); j++ {
		switch rest[j] {
		case '(':
			depth++
		case ')':
			depth--
		default:
			if depth == 0 && j+4 <= len(rest) && strings.EqualFold(rest[j:j+4], "FROM") {
				fromAt = j
				j = len(rest)
			}
		}
	}
	if fromAt < 0 {
		return 0, false
	}
	n := 0
	for _, it := range splitTopLevel(rest[:fromAt]) {
		it = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(it), "DISTINCT "))
		if it == "" {
			continue
		}
		if it == "*" || strings.HasSuffix(it, ".*") || strings.Contains(it, "?") {
			return 0, false
		}
		// 展开残留的裸标识符（非常量、非列名的驼峰/短名）⇒ 不可判；含括号的表达式（COALESCE(...)）正常计数。
		if !strings.Contains(it, "(") && !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(it) {
			if isIdentLike(it) {
				return 0, false
			}
		}
		n++
	}
	if n == 0 {
		return 0, false
	}
	return n, true
}

type scanConsumer struct {
	via   string
	kind  string // "direct" | "chained" | "helper"
	arity int
}

// scanConsumers 找该查询的消费点（同函数内的 rows.Scan / helper(row) / 链式 Scan）。
func scanConsumers(src string, qs querySite) []scanConsumer {
	var out []scanConsumer
	if qs.chained >= 0 {
		out = append(out, scanConsumer{via: "chained", kind: "chained", arity: qs.chained})
	}
	body, ok := funcBody(src, qs.fn)
	if !ok {
		return out
	}
	if qs.varName != "" {
		direct := regexp.MustCompile(regexp.QuoteMeta(qs.varName) + `\.Scan\(`)
		for _, m := range direct.FindAllStringIndex(body, -1) {
			open := m[1] - 1
			if inner, _, ok := parenSpan(body, open); ok {
				out = append(out, scanConsumer{via: qs.varName, kind: "direct", arity: countTopLevelNonEmpty(inner)})
			}
		}
		helperCall := regexp.MustCompile(`\b(\w+)\(\s*` + regexp.QuoteMeta(qs.varName) + `\s*[,)]`)
		for _, m := range helperCall.FindAllStringSubmatch(body, -1) {
			if m[1] == qs.varName {
				continue
			}
			if n, ok := functionScanArity(src, m[1]); ok {
				out = append(out, scanConsumer{via: m[1], kind: "helper", arity: n})
			}
		}
	}
	return out
}

func countTopLevelNonEmpty(inner string) int {
	n := 0
	for _, a := range splitTopLevel(inner) {
		if strings.TrimSpace(a) != "" {
			n++
		}
	}
	return n
}

// functionScanArity 取某函数体内 Scan 调用的目标数；多处不同或找不到 → 不可判。
func functionScanArity(src, fn string) (int, bool) {
	body, ok := funcBody(src, fn)
	if !ok {
		return 0, false
	}
	var found []int
	for _, site := range scanSites(body) {
		found = append(found, len(site.args))
	}
	if len(found) == 0 {
		return 0, false
	}
	first := found[0]
	for _, n := range found[1:] {
		if n != first {
			return 0, false
		}
	}
	return first, true
}

// funcBody 取出名为 fn 的函数体。
func funcBody(src, fn string) (string, bool) {
	if fn == "" || fn == "?" {
		return "", false
	}
	re := regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s*)?` + regexp.QuoteMeta(fn) + `\s*\(`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return "", false
	}
	open := strings.Index(src[loc[0]:loc[1]], "(") + loc[0]
	_, _, ok := parenSpan(src, open)
	if !ok {
		return "", false
	}
	return braceBody(src, loc[1])
}

// braceBody 从 from 起找第一个 '{' 并返回配对到 '}' 的整段（含花括号）。
func braceBody(src string, from int) (string, bool) {
	brace := strings.Index(src[from:], "{")
	if brace < 0 {
		return "", false
	}
	brace += from
	depth := 0
	for i := brace; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[brace : i+1], true
			}
		}
	}
	return "", false
}

// functionBodies 返回全部顶层函数的 (名字, 函数体)（用于函数级常量收集）。
func functionBodies(src string) []funcBodyInfo {
	var out []funcBodyInfo
	re := regexp.MustCompile(`(?m)^func\s`)
	for _, loc := range re.FindAllStringIndex(src, -1) {
		body, ok := braceBody(src, loc[1])
		if !ok {
			continue
		}
		// 名字从**签名原文**（`func` 到函数体 `{`）解析。不要用 enclosingFuncName(src, loc[1])：
		// 那里 src[:at] 的末行只有 "func"（无尾随空格），前缀匹配失败会退到**上一个**函数的签名行，
		// 方法名被错记成前一个函数——函数级常量随即全部查不到（静默失明：实测 16 处宽站点因此不可判）。
		brace := strings.Index(src[loc[1]:], "{")
		if brace < 0 {
			continue
		}
		name := funcNameFromSignature(src[loc[0] : loc[1]+brace])
		out = append(out, funcBodyInfo{name: name, body: body})
	}
	return out
}

// funcNameFromSignature 从签名文本取函数名（跳过方法接收者）。
func funcNameFromSignature(sig string) string {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sig), "func"))
	if strings.HasPrefix(s, "(") {
		depth := 0
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					s = strings.TrimSpace(s[i+1:])
					i = len(s)
				}
			}
		}
	}
	if j := strings.IndexAny(s, "( "); j >= 0 {
		return s[:j]
	}
	return s
}

type funcBodyInfo struct {
	name string
	body string
}
