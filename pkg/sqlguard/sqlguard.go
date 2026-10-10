// Package sqlguard 是「SQL 读路径」的静态对账共享实现（TD-90）：
// 对 Go 源码里每个查询调用，比对 **SELECT 列表的列数** 与 **消费侧 Scan 的目标数**。
//
// # 为什么要有这个包
//
// database/sql 在列数不等时报 `expected N destination arguments in Scan, not M`，
// 而调用方普遍只 storefail.Record 后返回 nil/continue ⇒ 该读路径**静默返回空**；
// 内存后端正确、单测全绿，只在真库/生产暴露。历史上该形态已发生两次
// （sqlstore.GetK8sCluster SELECT 7 / Scan 8；sqlstore.GetTasks SELECT 8 / Scan 9）。
// 判据与具体 store 无关，故下沉到 pkg/（各服务模块唯一合法的共享层，见 pkg/secretcrypto 先例）：
// 机制只有一份，各包只留薄接入断言——判红文案、覆盖率下限、豁免账留在各自测试里（那是政策，不是机制）。
//
// # 判据链（只认能静态证实的，不猜）
//
//  1. 找查询调用（Query/QueryRow 及 Context 变体）的 SQL 实参与赋值目标变量；
//  2. SQL 表达式求值：包级/函数级字面量与常量引用、跨行 `+` 拼接（同名多值即弃判）；
//  3. SELECT 列表按顶层逗号计数（括号内不算 ⇒ COALESCE(a,b) 记 1 列；含 `*`/`?` 即弃判）；
//  4. 消费侧三种形态：`rows.Scan(...)`、链式 `...QueryRow(...).Scan(...)`、
//     `scanX(row)` helper（目标数取自 helper 函数体内唯一的 Scan）。
//
// 不可判的宽站点（消费侧 ≥3 个目标）由调用方决定是否登记豁免——机制只报告，不替调用方定政策。
package sqlguard

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// GoFiles 返回 dir 下的非测试 Go 源文件（排序稳定，便于复现）。
func GoFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

// scanSite 是一次 .Scan( 调用。
type scanSite struct {
	args   []string
	scanAt int // src 中 ".Scan(" 的起始偏移
	line   int
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

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

// Result 一次全包对账的结果。
type Result struct {
	Judged          int               // SELECT 可判且与所有消费侧一致
	Mismatches      []string          // 列数 ≠ 消费侧目标数（真缺陷形态）
	Unresolved      int               // SQL 表达式解析不了（运行时拼接）
	Uncountable     int               // 列数数不出来（SELECT */含 ?）
	NoConsumer      int               // SQL 可判但同函数内找不到消费侧
	NoConsumerSites []string          // 上述站点明细（"本机制看不见"，非缺陷）
	WideUnjudged    map[string]string // 消费侧 ≥3 而 SQL 不可判（键 file#fn#var）——需扩展解析或登记豁免
	Forms           map[string]int    // 消费形态计数（direct/chained/helper）
}

// Analyze 遍历 dir 下的非测试 Go 源文件，对每个查询调用做「SELECT 列数 ↔ 消费侧 Scan 目标数」对账。
// 不可判的宽站点见 Result.WideUnjudged；「同函数内找不到消费侧」见 Result.NoConsumerSites
// （它是"本机制看不见"，不是缺陷——rows 传给非 scan* helper 属常见合法形态，判红留给调用方定）。
func Analyze(dir string) (Result, error) {
	st := Result{Forms: map[string]int{}, WideUnjudged: map[string]string{}}
	markWide := func(f, fn, varName, why string, consumers []scanConsumer) {
		for _, c := range consumers {
			if c.arity >= 3 {
				key := filepathBase(f) + "#" + fn + "#" + varName
				st.WideUnjudged[key] = fmt.Sprintf("%s（%s；消费侧 %s(%s)=%d，%s）",
					key, why, c.via, c.kind, c.arity, why)
				return
			}
		}
	}
	files, err := GoFiles(dir)
	if err != nil {
		return Result{}, err
	}
	idx, err := buildIndex(files)
	if err != nil {
		return Result{}, err
	}
	consts, err := buildConstIndex(files)
	if err != nil {
		return Result{}, err
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return Result{}, fmt.Errorf("读取 %s 失败: %v", f, err)
		}
		src := string(b)
		res := newSQLResolver(src, consts)
		for _, qs := range queryCallSites(src, idx) {
			if qs.sqlExpr == "" {
				st.Unresolved++
				continue
			}
			consumers := scanConsumers(src, qs, idx)
			sqlVal, ok := res.resolveExprValue(qs.sqlExpr, qs.fn)
			if !ok {
				st.Unresolved++
				markWide(f, qs.fn, qs.varName, "SQL 运行时拼接（fmt.Sprintf/未知标识符），静态不可判", consumers)
				continue
			}
			n, ok := selectColumnCount(sqlVal)
			if !ok {
				st.Uncountable++
				markWide(f, qs.fn, qs.varName, "SELECT 含 * / ? 等静态数不出的项", consumers)
				continue
			}
			if len(consumers) == 0 {
				st.NoConsumer++
				markWide(f, qs.fn, qs.varName, "找不到消费侧 Scan（rows 被传去别处）", nil)
				st.NoConsumerSites = append(st.NoConsumerSites, fmt.Sprintf(
					"%s:%d fn=%s var=%s——SELECT %d 列但同函数内找不到消费侧 Scan（rows 传给了非 scan* helper？）  sql=%.70s",
					f, qs.line, qs.fn, qs.varName, n, oneLine(sqlVal)))
				continue
			}
			for _, c := range consumers {
				st.Forms[c.kind]++
				if c.arity == 0 {
					// 目标数不可解析（如展开了未知切片的 Scan）：列为宽站点待豁免，不算比对。
					st.Uncountable++
					markWide(f, qs.fn, qs.varName, "Scan 目标数不可解析（展开了未知切片的 ...）", consumers)
					continue
				}
				// judged 统计「**参与了比对**」的站点（含不一致者）：下限防的是解析面塌缩，
				// 不是掩盖真缺陷——若只在一致时才计数，一处真缺陷会先撞下限，
				// 报出「判定数只有 125」而把「哪一行列数不等」埋掉（变异检验踩过）。
				st.Judged++
				if c.arity != n {
					st.Mismatches = append(st.Mismatches, fmt.Sprintf(
						"%s:%d fn=%s var=%s——SELECT %d 列 vs %s(%s) 扫 %d 个目标  sql=%.70s",
						f, qs.line, qs.fn, qs.varName, n, c.via, c.kind, c.arity, oneLine(sqlVal)))
				}
			}
		}
	}
	return st, nil
}

func filepathBase(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// arityJudgedFloor 参与比对的站点数棘轮下限（只许上调）。
// 127 = 2026-10-10 落地时的实测值（全包 136 个查询调用；不可解析 6、列数不可判 3，

type sqlResolver struct {
	pkg map[string]string
	fn  map[string]map[string]string
}

// newSQLResolver 构造单文件解析器；pkg 为**包级**常量表（跨文件构建，见 buildConstIndex）——
// 常量常定义在别的文件（如 sql_alerts.go 的 alertRuleColumns 用在 sql_m2.go），
// 只按本文件收集会让整类站点不可判（实测 1 处宽站点因此失明）。
func newSQLResolver(src string, pkg map[string]string) *sqlResolver {
	r := &sqlResolver{pkg: map[string]string{}, fn: map[string]map[string]string{}}
	for k, v := range pkg {
		r.pkg[k] = v
	}
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

func queryCallSites(src string, idx *pkgIndex) []querySite {
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
				scope, _ := funcBody(src, enclosingFuncName(src, m[0]))
				if n, ok := countScanArgs(sin, idx, scope); ok {
					chained = n
				} else {
					chained = 0 // 展开形态不可解析：按"目标数未知"处理
				}
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
			// FROM 必须**词边界**匹配：否则 `from_replicas` 里的 "from" 会被当成子句边界，
			// 列表被截断、列数少算（实测把 autoscaler 的 10 列读成 5 列并误报不一致）。
			if depth == 0 && j+4 <= len(rest) && strings.EqualFold(rest[j:j+4], "FROM") {
				var before, after byte = ' ', ' '
				if j > 0 {
					before = rest[j-1]
				}
				if j+4 < len(rest) {
					after = rest[j+4]
				}
				if !isIdentByte(before) && !isIdentByte(after) {
					fromAt = j
					j = len(rest)
				}
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
		// 注：这里**不再**按"像标识符"拒判。能走到这一步的表达式已由 resolveExprValue 完全求值，
		// 残留标识符只可能出现在不可解析分支（早已进 unresolved）；而真实列名并不都长 snake_case
		// （如 device 表的 `lastHeartbeat`）——按驼峰拒判会把整类站点误判成"数不出列数"（实测 6 处）。
		n++
	}
	if n == 0 {
		return 0, false
	}
	return n, true
}

// isIdentByte 判断 b 是否属于标识符字符（用于词边界）。
func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

type scanConsumer struct {
	via   string
	kind  string // "direct" | "chained" | "helper"
	arity int
}

// pkgIndex 包级索引（跨文件解析，避免"helper 定义在别的文件里"整类失明）：
//   - helpers：函数/方法名 → Scan 目标数（helper 形态 `scanX(row)`）；
//   - spreads：方法名 → `return []any{...}` 的顶层元素数（`rows.Scan(x.dest()...)` 展开形态）。
//
// 同名多值一律不收录（弃判优于猜错）。
type pkgIndex struct {
	helpers map[string]int
	spreads map[string]int
}

// buildConstIndex 跨文件收集包级字符串常量（`const x = "..."` / `const ( x = ... )`），
// 同名多值一律丢弃（弃判优于猜错）。
func buildConstIndex(files []string) (map[string]string, error) {
	seen := map[string]map[string]bool{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败: %v", f, err)
		}
		one := &sqlResolver{pkg: map[string]string{}}
		one.collectPkgLevel(string(b))
		for name, val := range one.pkg {
			if seen[name] == nil {
				seen[name] = map[string]bool{}
			}
			seen[name][val] = true
		}
	}
	out := map[string]string{}
	for name, vals := range seen {
		if len(vals) == 1 {
			for v := range vals {
				out[name] = v
			}
		}
	}
	return out, nil
}

func buildIndex(files []string) (*pkgIndex, error) {
	idx := &pkgIndex{helpers: map[string]int{}, spreads: map[string]int{}}
	helperSeen := map[string]map[int]bool{}
	spreadSeen := map[string]map[int]bool{}
	add := func(m map[string]map[int]bool, name string, n int) {
		if m[name] == nil {
			m[name] = map[int]bool{}
		}
		m[name][n] = true
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败: %v", f, err)
		}
		src := string(b)
		for _, fb := range functionBodies(src) {
			for _, site := range scanSites(fb.body) {
				add(helperSeen, fb.name, len(site.args))
			}
			if n, ok := spreadSliceLen(fb.body); ok {
				add(spreadSeen, fb.name, n) // 同名唯一时按裸名可用
				if fb.recvType != "" {
					spreadSeen[fb.recvType+"."+fb.name] = map[int]bool{n: true} // 类型限定名（跨类型重名可靠）
				}
			}
			if n, ok := scanFuncArity(fb.sig, fb.body); ok {
				add(helperSeen, fb.name, n)
			}
		}
	}
	for name, set := range helperSeen {
		if len(set) == 1 {
			for n := range set {
				idx.helpers[name] = n
			}
		}
	}
	for name, set := range spreadSeen {
		if len(set) == 1 {
			for n := range set {
				idx.spreads[name] = n
			}
		}
	}
	return idx, nil
}

// spreadSliceLen 若函数体是「单一 return []any{...} / []interface{}{...}」形态，
// 返回该切片字面量的顶层元素数。alert-svc 的 `nullAlert.dest()` 即此形态——
// 不解析它，`rows.Scan(na.dest()...)` 会被数成 1 个目标而误报不一致（实测 4 处）。
func spreadSliceLen(body string) (int, bool) {
	m := regexp.MustCompile(`return\s+\[\](?:any|interface\s*\{\s*\})\s*\{`).FindStringIndex(body)
	if m == nil {
		return 0, false
	}
	blk, ok := braceBody(body, m[1]-1)
	if !ok {
		return 0, false
	}
	n := countTopLevelBraces(strings.TrimSuffix(strings.TrimPrefix(blk, "{"), "}"))
	if n == 0 {
		return 0, false
	}
	return n, true
}

// scanConsumers 找该查询的消费点（同函数内的 rows.Scan / helper(row) / 链式 Scan）。
// arity==0 表示「目标数不可解析」（如展开了未知切片）——由调用方按宽站点处理。
func scanConsumers(src string, qs querySite, idx *pkgIndex) []scanConsumer {
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
				n, _ := countScanArgs(inner, idx, body) // 不可解析记 0
				out = append(out, scanConsumer{via: qs.varName, kind: "direct", arity: n})
			}
		}
		helperCall := regexp.MustCompile(`\b(\w+)\(\s*` + regexp.QuoteMeta(qs.varName) + `\s*[,)]`)
		for _, m := range helperCall.FindAllStringSubmatch(body, -1) {
			if m[1] == qs.varName {
				continue
			}
			if n, ok := idx.helpers[m[1]]; ok {
				out = append(out, scanConsumer{via: m[1], kind: "helper", arity: n})
			}
		}
		// 第四种形态：把 Scan **方法值**传给 helper（`scanRunbook(rows.Scan)`）——
		// 目标数来自该 helper 内对它那一次的调用实参数（idx.helpers 已收录该形态）。
		scanFunc := regexp.MustCompile(`\b(\w+)\(\s*` + regexp.QuoteMeta(qs.varName) + `\.Scan\s*[,)]`)
		for _, m := range scanFunc.FindAllStringSubmatch(body, -1) {
			if n, ok := idx.helpers[m[1]]; ok {
				out = append(out, scanConsumer{via: m[1] + "(" + qs.varName + ".Scan)", kind: "scanfunc", arity: n})
			}
		}
	}
	return out
}

// countScanArgs 数一次 Scan 的目标数：普通形态按顶层逗号；展开形态（`x.dest()...`）
// 查 pkgIndex.spreads。ok=false 或 n==0 表示不可解析。
func countScanArgs(argText string, idx *pkgIndex, scope string) (int, bool) {
	if t := strings.TrimSpace(argText); strings.HasSuffix(t, "...") {
		m := regexp.MustCompile(`^(\w+)\.(\w+)\(\)\s*\.\.\.$`).FindStringSubmatch(t)
		if m == nil {
			return 0, false
		}
		if typeName := inferLocalType(scope, m[1]); typeName != "" {
			if n, ok := idx.spreads[typeName+"."+m[2]]; ok {
				return n, true
			}
		}
		n, ok := idx.spreads[m[2]]
		return n, ok
	}
	n := countTopLevelNonEmpty(argText)
	if n == 0 {
		return 0, false
	}
	return n, true
}

// inferLocalType 在作用域文本里推局部变量的类型名：`var na nullAlert`、`x := &T{}`、`x := T{}`、
// `x := new(T)`。推不出返回空串——调用方回退到裸方法名索引（同名唯一才可用）。
func inferLocalType(scope, varName string) string {
	if scope == "" || varName == "" {
		return ""
	}
	pats := []*regexp.Regexp{
		regexp.MustCompile(`\bvar\s+` + regexp.QuoteMeta(varName) + `\s+\*?([A-Za-z_]\w*)`),
		regexp.MustCompile(`\b` + regexp.QuoteMeta(varName) + `\s*:?=\s*&?([A-Za-z_]\w*)\s*\{`),
		regexp.MustCompile(`\b` + regexp.QuoteMeta(varName) + `\s*:?=\s*(?:new|make)\s*\(\s*([A-Za-z_]\w*)`),
	}
	for _, re := range pats {
		if m := re.FindStringSubmatch(scope); m != nil {
			return m[1]
		}
	}
	return ""
}

// countTopLevelBraces 数顶层逗号分隔的元素个数（{}、() 与字符串内的逗号不计）。
func countTopLevelBraces(s string) int {
	n := 0
	depth, inStr, start := 0, false, 0
	flush := func(end int) {
		if strings.TrimSpace(s[start:end]) != "" {
			n++
		}
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'' || c == '"' || c == '`':
			inStr = !inStr
		case inStr:
		case c == '(' || c == '{' || c == '[':
			depth++
		case c == ')' || c == '}' || c == ']':
			depth--
		case c == ',' && depth == 0:
			flush(i)
			start = i + 1
		}
	}
	flush(len(s))
	return n
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
		sig := src[loc[0] : loc[1]+brace]
		out = append(out, funcBodyInfo{
			name:     funcNameFromSignature(sig),
			recvType: receiverTypeFromSignature(sig),
			sig:      sig,
			body:     body,
		})
	}
	return out
}

// receiverTypeFromSignature 从签名文本取方法接收者类型名（`func (n *nullAlert) dest()` → "nullAlert"）。
func receiverTypeFromSignature(sig string) string {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sig), "func"))
	if !strings.HasPrefix(s, "(") {
		return ""
	}
	end := strings.Index(s, ")")
	if end < 0 {
		return ""
	}
	recv := s[1:end]
	// 取最后一个标识符（跳过 `*`、参数名与方括号）。
	fields := regexp.MustCompile(`[A-Za-z_]\w*|\*|\[\]`).FindAllString(recv, -1)
	for i := len(fields) - 1; i >= 0; i-- {
		if f := fields[i]; f != "*" && f != "[]" {
			return f
		}
	}
	return ""
}

// scanFuncArity 识别「helper 接收 scan 方法值」形态（`scanRunbook(rows.Scan)`）：
// 签名里有 `func(dest ...any) error` 形态的参数，且函数体里只调它一次 ⇒ 该调用的实参数即目标数。
// 罕见但真实（runbook-svc），不认这种形态该包的读侧会整片"找不到消费侧"。
func scanFuncArity(sig, body string) (int, bool) {
	m := regexp.MustCompile(`(\w+)\s+func\s*\(\s*(?:dest\s+)?\.\.\.\s*(?:any|interface\{\})\s*\)\s*error`).FindStringSubmatch(sig)
	if m == nil {
		return 0, false
	}
	param := m[1]
	call := regexp.MustCompile(`\b` + regexp.QuoteMeta(param) + `\(`)
	found := map[int]bool{}
	for _, loc := range call.FindAllStringIndex(body, -1) {
		if inner, _, ok := parenSpan(body, loc[1]-1); ok {
			if n := countTopLevelNonEmpty(inner); n > 0 {
				found[n] = true
			}
		}
	}
	if len(found) != 1 {
		return 0, false
	}
	for n := range found {
		return n, true
	}
	return 0, false
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
	name     string
	recvType string // 方法接收者类型名（函数为空）
	sig      string // 签名原文（`func` 到函数体 `{`）
	body     string
}
