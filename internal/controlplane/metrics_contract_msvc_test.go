// metrics_contract_msvc_test.go — 出厂监控资产 ↔ **微服务侧**序列的契约测试。
//
// 为什么不能只靠同包的 metrics_contract_test.go：那份测试按前缀取名（`opsmesh_` / `process_`），
// 而 pkg/metrics 渲染的序列**不带前缀**，业务指标更是共用一个家族名
// （business_metrics / business_metrics_total）靠 `name` 标签区分。于是：
//
//	business_metrics_total{name="task_reclm_failures"}   ← 少一个字母，PromQL 合法、不报错
//
// 在控制面那份测试里完全隐形（它连看都不看这一行），后果与该测试开头记录的同一类：
// 规则恒为 no data，客户以为有告警。本文件把这一族也纳入机器判定，并钉住三条同源的静默失效：
//   - 规则引用的业务序列所属服务**根本没被抓取**（数据从未进过 Prometheus）；
//   - 同一组规则在 compose 与 chart 两个装载点**漂移**（K8s 客户拿到的是缺规则的栈）；
//   - 规则里写死的阈值与代码常量脱钩（改代码不改规则 = 永不触发或天天误报）。
package controlplane

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// msvcRuleAssets 是出厂路径上会被真实加载的规则/面板文件（前两个是规则，第三个是面板）。
var msvcRuleAssets = []string{
	filepath.Join("..", "..", "deploy", "monitoring", "prometheus-alerts.yml"),
	filepath.Join("..", "..", "deploy", "helm", "opsmesh", "templates", "prometheusrule.yaml"),
	filepath.Join("..", "..", "deploy", "monitoring", "grafana", "dashboards", "opsmesh-overview.json"),
}

const businessAlertGroup = "opsmesh_microservice_business_alerts"

var (
	// businessSelectorRe 抓 `business_metrics{…}` / `business_metrics_total{…}` 选择器整体。
	// 只认这两个家族名：给 counter 配 rate() 却选了 gauge 家族，是最常见的一类自伤。
	businessSelectorRe = regexp.MustCompile(`business_metrics(?:_total)?\{[^}]*\}`)
	nameMatcherRe      = regexp.MustCompile(`name\s*(?:=|=~)\s*"([^"]*)"`)
	// constDeclRe 抓 `NAME = "字面量"`（const 单行声明、const 块内的缩进声明都算），
	// 用于解析以常量传入的指标名——alert-svc 的 externalNotifyFailureMetric、
	// autoscaler 的 decisionHistoryGauge 就是这么写的。
	constDeclRe = regexp.MustCompile(`(?m)^[ \t]*(?:const[ \t]+)?([A-Za-z_][A-Za-z0-9_]*)[ \t]*=[ \t]*"([^"]*)"`)
	// bizCallRe 抓业务指标调用的第一个实参：字面量，或「常量 + "后缀"」拼接。
	bizCallRe  = regexp.MustCompile(`(?:Add|Set)BusinessMetric\(\s*(?:"([^"]*)"|([A-Za-z][A-Za-z0-9_]*)(?:\s*\+\s*"([^"]*)")?)`)
	typeLineRe = regexp.MustCompile(`# TYPE ([a-z][a-z0-9_]+) (gauge|counter|histogram)`)
	nameReAlt  = regexp.MustCompile(`__name__=~"([^"]*)"`)

	alertLineRe = regexp.MustCompile(`^[ \t]*-[ \t]*alert:[ \t]*([A-Za-z0-9_]+)[ \t]*$`)
	exprLine2Re = regexp.MustCompile(`^[ \t]*expr:[ \t]*(.+?)[ \t]*$`)
	forLineRe   = regexp.MustCompile(`^[ \t]*for:[ \t]*(\S+)[ \t]*$`)
	sevLineRe   = regexp.MustCompile(`^[ \t]*severity:[ \t]*(\S+)[ \t]*$`)
	groupLineRe = regexp.MustCompile(`^[ \t]*-[ \t]*name:[ \t]*(\S+)[ \t]*$`)
	capConstRe  = regexp.MustCompile(`maxDecisionHistory[ \t]*=[ \t]*([0-9]+)`)
)

// repoRead 读仓库相对路径；失败即判红（静默跳过等于门禁自己瞎了还不报警）。
func repoRead(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", rel, err)
	}
	return string(raw)
}

// businessNamesFromSource 扫描 services/**，返回「业务指标名 → 产出它的服务」。
//
// 只认 Add/SetBusinessMetric 的实参，注释里出现的名字不算产出；_test.go 排除，
// 否则规则可以引用一个「只有测试才产出」的序列。
func businessNamesFromSource(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(filepath.Join("..", "..", "services"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src := repoRead(t, path)
		consts := map[string]string{}
		for _, m := range constDeclRe.FindAllStringSubmatch(src, -1) {
			consts[m[1]] = m[2]
		}
		rel := strings.TrimPrefix(filepath.ToSlash(path), filepath.ToSlash(filepath.Join("..", ".."))+"/")
		svc := strings.TrimPrefix(rel, "services/")
		if i := strings.Index(svc, "/"); i > 0 {
			svc = svc[:i]
		}
		for _, m := range bizCallRe.FindAllStringSubmatch(src, -1) {
			name := m[1]
			if name == "" {
				base, ok := consts[m[2]]
				if !ok {
					// 解析不出来的实参宁可判红：当"存在"会漏报，而漏报正是这类门禁最坏的失效。
					t.Errorf("%s: 业务指标名以无法解析的表达式传入（%s）——请改为 const 字面量，或在 constDeclRe 里补解析规则",
						rel, m[2])
					continue
				}
				name = base + m[3]
			}
			out[name] = svc
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描 services/ 失败: %v", err)
	}
	if len(out) < 15 {
		t.Fatalf("只从源码解析到 %d 个业务指标名（阈值 15）——解析失效会让本测试整体空转，宁可判红", len(out))
	}
	return out
}

// renderedFamilyNames 从两个渲染器源码的 `# TYPE <name> <type>` 字面量收集家族名。
// histogram 额外补出渲染期派生的 _bucket/_sum/_count（出厂规则里就引用了 _bucket）。
func renderedFamilyNames(t *testing.T) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, rel := range []string{
		filepath.Join("..", "..", "pkg", "metrics", "metrics.go"),
		filepath.Join("..", "..", "internal", "metrics", "metrics.go"),
	} {
		found := false
		for _, m := range typeLineRe.FindAllStringSubmatch(repoRead(t, rel), -1) {
			found = true
			names[m[1]] = true
			if m[2] == "histogram" {
				for _, s := range []string{"_bucket", "_sum", "_count"} {
					names[m[1]+s] = true
				}
			}
		}
		if !found {
			t.Errorf("%s 里没解析到任何 # TYPE 行——渲染格式变了，请同步本测试", rel)
		}
	}
	return names
}

// referencedBusinessNames 抽出规则/面板里被引用的业务指标 name 值（name=~"a|b" 的每个分支都算）。
// 含正则元字符的分支（如 name=~"task_.*"）不做名字对账：通配本来就不是某个具体序列的名字。
func referencedBusinessNames(t *testing.T, path string) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, expr := range exprLineRe.FindAllStringSubmatch(repoRead(t, path), -1) {
		// Grafana 面板是 JSON：表达式里的引号被转义成 \"，不还原就抽不出 name 匹配器。
		text := strings.ReplaceAll(expr[1], `\"`, `"`)
		for _, sel := range businessSelectorRe.FindAllString(text, -1) {
			for _, m := range nameMatcherRe.FindAllStringSubmatch(sel, -1) {
				for _, alt := range strings.Split(m[1], "|") {
					alt = strings.TrimSpace(alt)
					if alt == "" {
						continue
					}
					if strings.ContainsAny(alt, `.^$*+?()[]{}|\`) {
						t.Logf("%s: name 匹配器是通配（%s），跳过逐名对账", filepath.Base(path), alt)
						continue
					}
					seen[alt] = true
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// TestShippedRulesReferenceRealMicroserviceMetrics：出厂规则/面板引用的每个业务序列名，
// 都必须真的由某个服务的 Add/SetBusinessMetric 产出，且该服务在出厂抓取面里。
func TestShippedRulesReferenceRealMicroserviceMetrics(t *testing.T) {
	produced := businessNamesFromSource(t)
	prom := repoRead(t, filepath.Join("..", "..", "deploy", "monitoring", "prometheus.yml"))
	total := 0
	for _, f := range msvcRuleAssets {
		names := referencedBusinessNames(t, f)
		if len(names) == 0 {
			t.Errorf("%s 里没抽到任何 business_metrics 的 name 引用——文件被清空或格式变了，请同步本测试", filepath.Base(f))
			continue
		}
		total += len(names)
		for _, n := range names {
			svc, ok := produced[n]
			if !ok {
				t.Errorf("出厂规则引用了没有任何服务产出的业务序列：name=%q（来自 %s）\n"+
					"    后果与漏写 opsmesh_ 前缀同类：该表达式恒为 no data、PromQL 不报错，这条告警永远不会触发",
					n, filepath.Base(f))
				continue
			}
			// 引用得到、但所属服务没被抓取 = 同样恒 no data，只是错在部署面而不是规则面。
			if !strings.Contains(prom, `targets: ["`+svc+":") {
				t.Errorf("业务序列 name=%q 由 %s 产出，但 deploy/monitoring/prometheus.yml 没有 %s 的抓取任务\n"+
					"    后果：出厂 compose 栈里这个服务的指标从不进入 Prometheus，引用它的告警恒为 no data",
					n, svc, svc)
			}
		}
	}
	if total < 8 {
		t.Fatalf("三个出厂资产合计只引用了 %d 个业务序列名（阈值 8）——对账面过窄，判红", total)
	}
	t.Logf("已对账 %d 处业务序列引用；源码侧共 %d 个业务指标名", total, len(produced))
}

// TestShippedRulesGuardSeriesExist：`__name__=~"a|b|c"` 的每个分支都要真的被某个渲染器输出。
// 这种写法是「两套命名族并集」的唯一逃生口，也是最容易只写对一半的地方。
func TestShippedRulesGuardSeriesExist(t *testing.T) {
	families := renderedFamilyNames(t)
	produced := businessNamesFromSource(t)
	checked := 0
	for _, f := range msvcRuleAssets {
		for _, expr := range exprLineRe.FindAllStringSubmatch(repoRead(t, f), -1) {
			text := strings.ReplaceAll(expr[1], `\"`, `"`)
			for _, m := range nameReAlt.FindAllStringSubmatch(text, -1) {
				for _, alt := range strings.Split(m[1], "|") {
					alt = strings.TrimSpace(alt)
					if alt == "" {
						continue
					}
					checked++
					if !families[alt] {
						t.Errorf("规则里的 __name__=~ 分支 %q 没有任何渲染器输出（来自 %s）——已声明家族见 pkg/metrics 与 internal/metrics 的 # TYPE 行",
							alt, filepath.Base(f))
					}
				}
			}
		}
	}
	if checked < 4 {
		t.Fatalf("只校验到 %d 个 __name__ 分支（阈值 4）——解析失效会让本测试空转", checked)
	}
	// 顺手钉住一条口径事实：业务指标名不得与渲染器保留家族同名（否则 exposition 出现同名两套序列）。
	for n := range produced {
		if families[n] {
			t.Errorf("业务指标名 %q 与渲染器家族同名，exposition 会出现两套同名序列", n)
		}
	}
}

// ruleShape 是一条规则的可执行部分。注解文案允许两份不同，语义不允许。
type ruleShape struct {
	expr, forStep, severity string
}

// parseRuleGroup 按行抽 alert → (expr, for, severity)。
// 不能用 YAML 解析器：chart 那份是 Go 模板，含 {{ }} 指令，YAML 读不了。
func parseRuleGroup(t *testing.T, path, group string) map[string]ruleShape {
	t.Helper()
	lines := strings.Split(repoRead(t, path), "\n")
	inGroup := false
	if group == "" {
		inGroup = true
	}
	var (
		cur   string
		block []string
		out   = map[string]ruleShape{}
		order []string
		flush = func() {
			if cur == "" {
				return
			}
			shape := ruleShape{}
			for _, l := range block {
				if m := exprLine2Re.FindStringSubmatch(l); m != nil && shape.expr == "" {
					shape.expr = m[1]
				}
				if m := forLineRe.FindStringSubmatch(l); m != nil && shape.forStep == "" {
					shape.forStep = m[1]
				}
				if m := sevLineRe.FindStringSubmatch(l); m != nil && shape.severity == "" {
					shape.severity = m[1]
				}
			}
			out[cur] = shape
			order = append(order, cur)
		}
	)
	for _, l := range lines {
		if m := groupLineRe.FindStringSubmatch(l); m != nil {
			flush()
			cur, block = "", nil
			inGroup = (group == "" || m[1] == group)
			continue
		}
		if !inGroup {
			continue
		}
		if m := alertLineRe.FindStringSubmatch(l); m != nil {
			flush()
			cur, block = m[1], nil
			continue
		}
		if cur != "" {
			block = append(block, l)
		}
	}
	flush()
	if len(out) == 0 {
		t.Errorf("%s 没解析到任何规则（格式变了会让镜像对账空转）", filepath.Base(path))
	}
	return out
}

// TestBusinessRuleMirrorBetweenComposeAndChart：微服务业务组必须在两条交付路径上等量存在。
// compose 客户与 K8s 客户的装载点不同（prometheus.yml 的 rule_files / PrometheusRule CR），
// 只改一边就会有一侧「看起来配了告警、实际那组规则不存在」——本项目反复登记过的漂移形态。
func TestBusinessRuleMirrorBetweenComposeAndChart(t *testing.T) {
	compose := parseRuleGroup(t, msvcRuleAssets[0], businessAlertGroup)
	chart := parseRuleGroup(t, msvcRuleAssets[1], businessAlertGroup)
	if len(compose) < 5 {
		t.Fatalf("compose 侧分组只解析到 %d 条规则（阈值 5）——对账面过窄", len(compose))
	}
	var missing []string
	for _, name := range sortedRuleNames(compose) {
		c := compose[name]
		h, ok := chart[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if c.expr != h.expr || c.forStep != h.forStep || c.severity != h.severity {
			t.Errorf("规则 %s 在两份出厂文件里语义不同：\n    compose: expr=%s for=%s sev=%s\n    chart  : expr=%s for=%s sev=%s",
				name, c.expr, c.forStep, c.severity, h.expr, h.forStep, h.severity)
		}
	}
	if len(missing) > 0 {
		t.Errorf("chart 的 %s 缺 compose 里已有的规则：%v（K8s 客户少 %d 条告警）",
			businessAlertGroup, missing, len(missing))
	}
	for name := range chart {
		if _, ok := compose[name]; !ok {
			t.Errorf("chart 里有 %s 而 compose 的 %s 没有：出厂栈缺这条规则", name, businessAlertGroup)
		}
	}
	if len(compose) != len(chart) {
		t.Errorf("两份出厂文件的规则条数不同：compose %d / chart %d", len(compose), len(chart))
	}
	t.Logf("两份出厂文件在 %s 上逐条一致（%d 条）", businessAlertGroup, len(compose))
}

// TestAutoscalerCapLiteralMatchesCode：出厂规则里写死的数字必须等于代码里的 maxDecisionHistory。
// 阈值与常量脱钩是双向失效：写高了永不触发，写低了天天误报。
func TestAutoscalerCapLiteralMatchesCode(t *testing.T) {
	src := repoRead(t, filepath.Join("..", "..", "services", "autoscaler-svc", "internal", "evaluator", "evaluator.go"))
	m := capConstRe.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("没在 evaluator.go 里找到 maxDecisionHistory 常量——缓冲实现变了，请同步本测试与出厂规则")
	}
	capLit := m[1]
	for _, f := range msvcRuleAssets[:2] {
		found := false
		for _, expr := range exprLineRe.FindAllStringSubmatch(repoRead(t, f), -1) {
			if !strings.Contains(expr[1], `name="autoscaler_decision_history_entries"`) {
				continue
			}
			found = true
			if !strings.HasSuffix(strings.TrimRight(strings.TrimSpace(expr[1]), " \t"), capLit) {
				t.Errorf("%s 的 autoscaler 决策历史阈值不等于代码常量 maxDecisionHistory=%s：%s",
					filepath.Base(f), capLit, strings.TrimSpace(expr[1]))
			}
		}
		if !found {
			t.Errorf("%s 里没有 autoscaler_decision_history_entries 规则", filepath.Base(f))
		}
	}
}

func sortedRuleNames(m map[string]ruleShape) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// docMsvcRowRe 抓 §4.1.1 表格行的第 1 列与第 3 列（指标 / 产出服务）。
var docMsvcRowRe = regexp.MustCompile(`(?m)^\|\s*(.+?)\s*\|\s*[^|]*\|\s*([^|]*?)\s*\|`)
var docAllNamesRe = regexp.MustCompile(`name="([a-z_][a-z0-9_]*)"`)
var identHeadRe = regexp.MustCompile(`^[a-z][a-z0-9_]*`)

// TestDocumentedMicroserviceMetricsAreScraped：operations.md §4.1.1 承诺给客户的每个微服务序列，
// 必须真的由**它声称的那个服务**产出，且该服务在出厂抓取面里。
//
// 为什么单独一条：§4.1 那张表由 TestDocumentedMetricsAreExported 钉住，但那份测试只看控制面的
// exposition——微服务家族从来不由控制面渲染。文档里写错服务归属、或写一个只有测试才产出的名字，
// 在 §4.1.1 里同样会静默通过。而客户是按文档接监控的。
func TestDocumentedMicroserviceMetricsAreScraped(t *testing.T) {
	raw := repoRead(t, filepath.Join("..", "..", "docs", "operations.md"))
	section, ok := markdownSection(raw, "#### 4.1.1 ")
	if !ok {
		t.Fatalf("operations.md 里没有 `#### 4.1.1 ` 小节——文档结构变了，请同步本测试")
	}
	prom := repoRead(t, filepath.Join("..", "..", "deploy", "monitoring", "prometheus.yml"))
	produced := businessNamesFromSource(t)
	families := renderedFamilyNames(t)

	rows := 0
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue // 只认第一列是反引号指标名的数据行（表头与分隔行跳过）
		}
		m := docMsvcRowRe.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("§4.1.1 表格行解析失败：%s", line)
			continue
		}
		metricCell, svcCell := m[1], m[2]
		rows++
		names := docAllNamesRe.FindAllStringSubmatch(metricCell, -1)
		if len(names) == 0 {
			// 没有 name 标签 ⇒ 应当是渲染器直接输出的家族名（service_info / http_* / *_series…）。
			for _, seg := range strings.Split(metricCell, "/") {
				seg = strings.Trim(strings.TrimSpace(seg), "`")
				if seg == "" {
					continue
				}
				head := identHeadRe.FindString(seg)
				if head == "" || !families[head] {
					t.Errorf("§4.1.1 写了家族名 %q（解析为 %q），但两个渲染器都没声明它", seg, head)
				}
			}
			continue
		}
		for _, n := range names {
			name := n[1]
			src, ok := produced[name]
			if !ok {
				t.Errorf("§4.1.1 承诺了业务序列 name=%q，但没有任何服务产出它——按文档接告警会拿到永久空序列", name)
				continue
			}
			if svcCell != "" && !strings.Contains(svcCell, "全部") && !strings.Contains(svcCell, src) {
				t.Errorf("§4.1.1 把 name=%q 归给 %q，实际由 %q 产出——服务归属写错会把排查与抓取排期指错方向", name, svcCell, src)
			}
			if !strings.Contains(prom, `targets: ["`+src+":") {
				t.Errorf("§4.1.1 承诺了 name=%q（%s 产出），但 prometheus.yml 没有 %s 的抓取任务", name, src, src)
			}
		}
	}
	if rows < 12 {
		t.Fatalf("§4.1.1 只解析到 %d 行指标（阈值 12）——表格格式变了会让本测试空转", rows)
	}
	t.Logf("§4.1.1 的 %d 行均已对账（源码侧共 %d 个业务指标名）", rows, len(produced))
}
