// metrics_contract_test.go — 出厂监控资产 ↔ 实际抓取面之间的契约测试。
//
// 起因（2026-09-26 实测）：prometheus-alerts.yml 与 Helm PrometheusRule 里有一批告警，
// 引用的序列在 **Prometheus 真正抓取的 9091 面上不存在**：
//
//	http_requests_total                          漏了 opsmesh_ 前缀（真名 opsmesh_http_requests_total）
//	http_request_duration_seconds_bucket         同上
//	opsmesh_tasks_failed_total                   真名是 opsmesh_tasks_total{status="failed"}
//	opsmesh_device_status                        当时全仓根本不产出这个指标
//	process_cpu_seconds_total                    注册表只输出 start_time/rss/vms/pid，没有 CPU
//
// 这类缺陷**不会报错**：PromQL 语法合法，而 Prometheus 对"无数据"的处置就是不评估。
// 于是交付物看起来"配了告警"，实际那几条永远不可能触发——客户出事时才发现没有告警。
// 本测试把该缺口变成机器判定：从出厂规则里抽出被引用的本项目序列名，
// 逐个要求在控制面真实渲染的 exposition 里可见。
package controlplane

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/metrics"
	"github.com/Levango7/OpsMesh/internal/store"
)

// exprLineRe 只取规则/面板里的**表达式行**。
// 同时支持两种宿主格式：YAML（`expr: …`）与 Grafana 面板 JSON（`{ "expr": "…"` 单行对象）。
// 不能扫全文：prometheus-alerts.yml 的 group 名（opsmesh_service_alerts / opsmesh_business_alerts …）
// 也以 opsmesh_ 开头，扫全文会把"组名"当指标名误判——第一版就误报了 5 条。
var exprLineRe = regexp.MustCompile(`(?m)^[[:space:]]*\{?[[:space:]]*["']?(?:expr|expression)["']?[[:space:]]*:[[:space:]]*(.*)$`)

// identRe 抽出表达式文本里形如标识符的片段。
var identRe = regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_]*`)

// externalSeries 是不由控制面渲染、因此不能要求它出现在 exposition 里的名字。
// 每条都写明归属：否则这张表会变成"对不上就塞进来"的垃圾桶。
var externalSeries = map[string]string{
	"node_filesystem_avail_bytes": "需 node_exporter；本栈不含，相关规则已移到 prometheus-alerts.host.example.yml",
	"node_filesystem_size_bytes":  "需 node_exporter；同上",
}

// contractTestServer 构造与生产同构的 Server：注册表必须显式建好，
// 否则渲染出的是空注册表，本测试会失去意义（HTTP/审计链/仪表值等序列都不会出现）。
func contractTestServer() *Server {
	return &Server{
		store: store.NewMemoryStore(),
		cfg:   &config.Config{TaskMaxRetries: 3, MetricsAllowCIDR: "127.0.0.0/8"},

		jwtSecret:    []byte("test-jwt-secret-for-metrics-contract-32b!"),
		sessionStore: store.NewInProcessSessionStore(),
		metrics:      metrics.New(),
	}
}

// expositionNames 渲染一次 /metrics，返回其中的指标名集合。
//
// 渲染前先制造一点"真的发生过"的事件：任务计数与 HTTP 计数都是 counter，
// 没有事件时按 Prometheus 惯例就是不产出序列——不预先产生就会把合法名字判成缺失。
func expositionNames(t *testing.T) map[string]bool {
	t.Helper()
	s := contractTestServer()
	s.metrics.RecordHTTP("GET", "/api/v1/devices", "200", 0.01)
	s.metrics.IncTask("done")
	s.metrics.IncTask("failed")
	s.metrics.SetAuditChainStatus(true, 100, true)
	s.metrics.IncAgentSignature("v2", "ok")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:56789"
	w := httptest.NewRecorder()
	s.handlePrometheusMetrics(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /metrics 期望 200，实得 %d；body=%s", w.Code, w.Body.String())
	}
	names := map[string]bool{}
	for _, line := range strings.Split(w.Body.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := line
		if i := strings.IndexAny(name, "{ "); i >= 0 {
			name = name[:i]
		}
		names[name] = true
	}
	if len(names) < 10 {
		t.Fatalf("/metrics 只渲染出 %d 个指标，契约测试失去意义（渲染路径被改空？）", len(names))
	}
	return names
}

// referencedMetrics 从规则/面板文件的表达式里抽出被引用的本项目序列名。
func referencedMetrics(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", filepath.Base(path), err)
	}
	seen := map[string]bool{}
	for _, expr := range exprLineRe.FindAllStringSubmatch(string(raw), -1) {
		for _, m := range identRe.FindAllString(expr[1], -1) {
			if strings.HasPrefix(m, "opsmesh_") || strings.HasPrefix(m, "process_") {
				seen[m] = true
			}
		}
	}
	if len(seen) == 0 {
		// 解析不到任何引用 = 门禁自己瞎了。宁可判红，不给静默漏网（教训：永远 PASS 的门禁最危险）。
		t.Errorf("%s 里没抽到任何 opsmesh_*/process_* 表达式引用——文件被清空或格式变了，请同步本测试", filepath.Base(path))
		return nil
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// TestShippedAlertRulesReferenceExportedMetrics：出厂规则引用的每个本项目序列，
// 都必须在被抓取的那份 exposition 里可见。
func TestShippedAlertRulesReferenceExportedMetrics(t *testing.T) {
	exported := expositionNames(t)
	ruleFiles := []string{
		filepath.Join("..", "..", "deploy", "monitoring", "prometheus-alerts.yml"),
		filepath.Join("..", "..", "deploy", "helm", "opsmesh", "templates", "prometheusrule.yaml"),
	}
	for _, f := range ruleFiles {
		for _, name := range referencedMetrics(t, f) {
			if exported[name] {
				continue
			}
			if why, ok := externalSeries[name]; ok {
				t.Logf("跳过 %s：%s", name, why)
				continue
			}
			if name == "process_cpu_seconds_total" && runtime.GOOS != "linux" {
				// 该序列只在 Linux 输出（读 /proc/self/stat）。CI 是 Linux ⇒ 会真校验；
				// 本机 Windows 跳过是如实反映产品行为，不是放水。
				t.Logf("跳过 %s：非 Linux 平台不输出该序列", name)
				continue
			}
			t.Errorf("出厂规则引用了抓取面上不存在的序列：%s（来自 %s）\n"+
				"    后果：这条告警在 Prometheus 里恒为 no data，语法合法也不报错，"+
				"客户以为有告警、实际永远不会触发", name, filepath.Base(f))
		}
	}
}

// TestShippedDashboardsReferenceExportedMetrics：Grafana 面板同罪同判。
// 面板引用错名字的表现是"图表面板 No data"——比不触发告警更显眼，但同样静默。
func TestShippedDashboardsReferenceExportedMetrics(t *testing.T) {
	exported := expositionNames(t)
	dash := filepath.Join("..", "..", "deploy", "monitoring", "grafana", "dashboards", "opsmesh-overview.json")
	for _, name := range referencedMetrics(t, dash) {
		if exported[name] {
			continue
		}
		if why, ok := externalSeries[name]; ok {
			t.Logf("跳过 %s：%s", name, why)
			continue
		}
		if name == "process_cpu_seconds_total" && runtime.GOOS != "linux" {
			t.Logf("跳过 %s：非 Linux 平台不输出该序列", name)
			continue
		}
		t.Errorf("出厂仪表盘引用了抓取面上不存在的序列：%s（面板会显示 No data）", name)
	}
}

// docMetricRowRe 匹配 operations.md §4.1 表格行的第一列（`| \`opsmesh_xxx{label}\` | 类型 | 说明 |`）。
var docMetricRowRe = regexp.MustCompile("(?m)^\\|\\s*`([a-z_][a-z0-9_]*)(?:\\{[^`]*\\})?`\\s*\\|")

// TestDocumentedMetricsAreExported：文档里承诺给客户的每个指标，必须真的能抓到。
//
// 为什么也要管文档：此前 operations.md 的指标表里写着 opsmesh_alerts_total{severity}、
// opsmesh_grpc_request_total、opsmesh_leader_elections_total 三条**代码里从未实现**的指标。
// 照文档接告警/接面板的人会拿到永久空数据，而规则文件与代码测试都看不见这件事——
// 因为错的是"承诺"那一侧。
func TestDocumentedMetricsAreExported(t *testing.T) {
	exported := expositionNames(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "operations.md"))
	if err != nil {
		t.Fatalf("读 docs/operations.md 失败: %v", err)
	}
	seen := map[string]bool{}
	for _, m := range docMetricRowRe.FindAllStringSubmatch(string(raw), -1) {
		seen[m[1]] = true
	}
	if len(seen) < 10 {
		t.Fatalf("文档表格里只解析到 %d 个指标名（解析失效会导致该门禁空转）", len(seen))
	}
	for name := range seen {
		if exported[name] {
			continue
		}
		if name == "process_cpu_seconds_total" && runtime.GOOS != "linux" {
			t.Logf("跳过 %s：非 Linux 平台不输出该序列", name)
			continue
		}
		t.Errorf("operations.md 承诺了指标 %s，但 /metrics 里没有它——按文档接监控会拿到永久空序列", name)
	}
}

// TestMetricsPortsServeIdenticalExposition 守住"两个端口一份渲染"。

// 只做上面的单侧对账还不够：若哪天又出现"某指标只在 8080 有、而 Prometheus 抓 9091"，
// 单侧检查照样全绿。这里让两个 handler 各渲染一次并按序列名比对。
func TestMetricsPortsServeIdenticalExposition(t *testing.T) {
	s := contractTestServer()
	s.metrics.RecordHTTP("GET", "/api/v1/devices", "200", 0.01)
	s.metrics.IncTask("done")

	webReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	webReq.RemoteAddr = "127.0.0.1:56790"
	webRec := httptest.NewRecorder()
	s.handlePrometheusMetrics(webRec, webReq)
	if webRec.Code != http.StatusOK {
		t.Fatalf("Web 端口 /metrics status=%d", webRec.Code)
	}

	s.metricsPort = 0 // 由内核分配端口，避免并发测试撞车
	srv, lis, err := s.buildMetrics()
	if err != nil {
		t.Fatalf("buildMetrics 失败: %v", err)
	}
	defer lis.Close()
	go func() { _ = srv.Serve(lis) }()
	defer func() { _ = srv.Close() }()

	tcpAddr, ok := lis.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("metrics 监听地址不是 *net.TCPAddr：%T", lis.Addr())
	}
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", tcpAddr.Port))
	if err != nil {
		t.Fatalf("抓 metrics 端口失败: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读 metrics 端口响应失败: %v", err)
	}

	web := expositionNameSet(webRec.Body.String())
	mport := expositionNameSet(string(raw))
	if !slices.Equal(web, mport) {
		onlyWeb := slices.DeleteFunc(slices.Clone(web), func(n string) bool { return slices.Contains(mport, n) })
		onlyM := slices.DeleteFunc(slices.Clone(mport), func(n string) bool { return slices.Contains(web, n) })
		t.Errorf("两个端口的 /metrics 序列集合不一致：只在 Web 端口有 %v；只在 metrics 端口有 %v\n"+
			"    同一名指标在两个端口的存在性/类型不同，正是永不触发告警与空面板的成因", onlyWeb, onlyM)
	}
}

// expositionNameSet 把 exposition 文本压成"排序后的指标名列表"。
// 刻意丢掉取值：go_goroutines / 内存读数 / PID 这类运行期数字在两次抓取之间本就会变，
// 拿它们比对只会造出偶发失败。本契约关心的是**有哪些序列**。
func expositionNameSet(body string) []string {
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := line
		if i := strings.IndexAny(name, "{ "); i >= 0 {
			name = name[:i]
		}
		seen[name] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
