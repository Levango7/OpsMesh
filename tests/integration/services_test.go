// Package integration 提供 OpsMesh 微服务集成测试与跨模块链路校验。
//
// 本文件测试分两类，请勿混用：
//
//   - **A 类 · 纯算法链路（无需外部服务，默认执行）**：直接调用主模块 `internal/...`
//     的真实实现（异常检测、告警聚合、插件生命周期），断言真实计算结果。
//     原先这些用例只用 httptest 构造请求再丢弃 recorder，属于"假测试"（永远绿）。
//
//   - **B 类 · 微服务 HTTP 契约（需真实环境，默认跳过）**：对 `services/` 下独立
//     微服务发真实 HTTP 请求并断言状态码与响应体。未配置集成环境时 `t.Skip`，
//     保证 `go test ./tests/...` 在无环境时快速跳过而不失败。
//
// B 类由两个环境变量控制（二者都未设置即跳过），沿用 `internal/store/sql_test.go`
// 中 `OPSMESH_TEST_MYSQL_DSN` 的既有 skip 模式：
//
//   - `OPSMESH_TEST_SERVICES_ADDR=<host>`：按内置端口表（见 defaultServicePorts）
//     一次性展开 12 个服务的 base URL，如 `OPSMESH_TEST_SERVICES_ADDR=127.0.0.1`。
//
//   - `OPSMESH_INTEGRATION_BASE_URLS=<name>=<url>[,<name>=<url>...]`：逐个显式
//     指定服务地址，优先级高于前者；只配置了的才会被探测，如
//     `OPSMESH_INTEGRATION_BASE_URLS=controlplane=http://127.0.0.1:8080,auth-svc=http://127.0.0.1:8100`。
//
// 示例（docker compose 起栈后真实跑一遍）：
//
//	OPSMESH_TEST_SERVICES_ADDR=127.0.0.1 go test ./tests/... -run TestServiceHealth -v
//	OPSMESH_INTEGRATION_BASE_URLS=aio-svc=http://127.0.0.1:8107 go test ./tests/... -v
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/alertengine"
	"opsmesh/internal/plugin"
)

// 集成环境开关环境变量。
const (
	// envServicesAddr 按内置端口表批量展开服务地址（值为 host 或 host:port 的 host 部分）。
	envServicesAddr = "OPSMESH_TEST_SERVICES_ADDR"
	// envBaseURLs 逐个显式指定服务 base URL，优先级高于 envServicesAddr。
	envBaseURLs = "OPSMESH_INTEGRATION_BASE_URLS"
)

// defaultServicePorts 内置服务端口表（与 docker-compose / 各服务默认监听端口一致）。
var defaultServicePorts = []struct {
	name string
	port string
}{
	{"controlplane", "8080"},
	{"auth-svc", "8100"},
	{"device-svc", "8101"},
	{"task-svc", "8102"},
	{"alert-svc", "8103"},
	{"deploy-svc", "8104"},
	{"log-svc", "8105"},
	{"config-svc", "8106"},
	{"aio-svc", "8107"},
	{"plugin-svc", "8108"},
	{"portal-svc", "8109"},
	{"grafana-bridge", "8110"},
}

// integrationBaseURLs 解析集成环境配置，返回 服务名 -> base URL。
//
// 两个环境变量都为空时 t.Skip（B 类用例不失败、只跳过）；
// envServicesAddr 先按端口表展开全部默认服务，envBaseURLs 再逐个覆盖/追加。
func integrationBaseURLs(t *testing.T) map[string]string {
	t.Helper()
	addr := strings.TrimSpace(os.Getenv(envServicesAddr))
	explicit := strings.TrimSpace(os.Getenv(envBaseURLs))
	if addr == "" && explicit == "" {
		t.Skipf("未配置集成环境，跳过（设置 %s 或 %s 后启用真实 HTTP 断言）", envServicesAddr, envBaseURLs)
	}

	out := make(map[string]string, len(defaultServicePorts))
	if addr != "" {
		host := strings.TrimSuffix(addr, "/")
		if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
			host = "http://" + host
		}
		for _, svc := range defaultServicePorts {
			out[svc.name] = host + ":" + svc.port
		}
	}
	for _, item := range strings.Split(explicit, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, rawURL, ok := strings.Cut(item, "=")
		if !ok {
			t.Fatalf("%s 条目格式错误 %q，应为 <服务名>=<base url>（多个用英文逗号分隔）", envBaseURLs, item)
		}
		name = strings.TrimSpace(name)
		rawURL = strings.TrimSpace(rawURL)
		if name == "" || rawURL == "" {
			t.Fatalf("%s 条目 %q 的服务名或地址为空", envBaseURLs, item)
		}
		out[name] = strings.TrimSuffix(rawURL, "/")
	}
	return out
}

// requireService 返回指定服务的 base URL；该服务未配置时跳过当前用例。
func requireService(t *testing.T, urls map[string]string, name string) string {
	t.Helper()
	base, ok := urls[name]
	if !ok || base == "" {
		t.Skipf("未配置服务 %s 的集成地址，跳过（在 %s 中指定 %s=http://host:port，或设置 %s）",
			name, envBaseURLs, name, envServicesAddr)
	}
	return strings.TrimSuffix(base, "/")
}

// newIntegrationClient 构造集成测试用 HTTP 客户端（带整体超时，避免环境不可达时挂死）。
func newIntegrationClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

// doHTTP 发起真实 HTTP 请求，返回状态码与响应体（响应体上限 1 MiB）。
//
// 请求失败（连接拒绝/超时/DNS 失败）直接 Fatal：集成环境已声明配置就必须可用，
// 否则说明环境未就绪或端口表漂移，属于真实缺陷而非"可跳过"。
func doHTTP(t *testing.T, client *http.Client, method, url string, body []byte) (int, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		t.Fatalf("构造请求 %s %s 失败: %v", method, url, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("请求 %s %s 失败（集成环境未就绪或服务未监听？）: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("读取 %s %s 响应体失败: %v", method, url, err)
	}
	return resp.StatusCode, raw
}

// probeHealth 探测健康检查端点：先 /health，404 时回退 /healthz。
func probeHealth(t *testing.T, client *http.Client, base string) (code int, path string, body []byte) {
	t.Helper()
	code, body = doHTTP(t, client, http.MethodGet, base+"/health", nil)
	if code == http.StatusNotFound {
		code, body = doHTTP(t, client, http.MethodGet, base+"/healthz", nil)
		return code, "/healthz", body
	}
	return code, "/health", body
}

// assertJSON 断言响应体是合法 JSON 对象或数组（防"200 但返回 HTML/空串"）。
func assertJSON(t *testing.T, contextDesc string, body []byte) {
	t.Helper()
	if len(bytes.TrimSpace(body)) == 0 {
		t.Fatalf("%s：响应体为空，期望合法 JSON", contextDesc)
	}
	var any json.RawMessage
	if err := json.Unmarshal(body, &any); err != nil {
		t.Fatalf("%s：响应体不是合法 JSON: %v；前 200 字节=%q", contextDesc, err, truncate(string(body), 200))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}

// ============================================================================
// B 类：微服务 HTTP 契约（需集成环境，默认跳过）
// ============================================================================

// TestServiceHealth 对所有已配置服务发真实 GET /health（404 时回退 /healthz）并断言 200。
func TestServiceHealth(t *testing.T) {
	urls := integrationBaseURLs(t)
	client := newIntegrationClient()

	names := make([]string, 0, len(urls))
	for name := range urls {
		names = append(names, name)
	}
	sort.Strings(names) // 稳定输出，便于比对

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			base := strings.TrimSuffix(urls[name], "/")
			code, path, body := probeHealth(t, client, base)
			if code != http.StatusOK {
				t.Fatalf("服务 %s 健康检查失败：GET %s%s -> %d，期望 200；响应体=%s",
					name, base, path, code, truncate(string(body), 200))
			}
			t.Logf("服务 %s 健康检查通过：GET %s%s -> 200", name, base, path)
		})
	}
}

// TestMicroservicePipelineContracts 校验 aio-svc / plugin-svc 三个业务端点的真实 HTTP 契约。
//
// 只断言"端点存在且返回合法 JSON"——不断言业务数值，因为各服务的数据依赖其自身
// 存储后端，跨环境数值不稳定；业务正确性由各服务模块内单测保证。
func TestMicroservicePipelineContracts(t *testing.T) {
	urls := integrationBaseURLs(t)
	client := newIntegrationClient()

	detectBody, err := json.Marshal(map[string]interface{}{
		"device_id": "srv-01",
		"metric":    "cpu_usage",
		"values":    []float64{10, 11, 9, 10, 11, 9, 10, 11, 9, 10, 11, 9, 10, 11, 9, 10, 11, 9, 10, 100},
		"method":    "zscore",
	})
	if err != nil {
		t.Fatalf("构造异常检测请求体失败: %v", err)
	}
	compressBody, err := json.Marshal([]map[string]interface{}{
		{"id": "1", "rule_id": "cpu_high", "device_id": "srv-01-web", "severity": "critical"},
		{"id": "2", "rule_id": "cpu_high", "device_id": "srv-02-web", "severity": "critical"},
		{"id": "3", "rule_id": "cpu_high", "device_id": "srv-03-web", "severity": "critical"},
		{"id": "4", "rule_id": "mem_high", "device_id": "srv-01-web", "severity": "warning"},
	})
	if err != nil {
		t.Fatalf("构造告警降噪请求体失败: %v", err)
	}

	cases := []struct {
		service string
		name    string
		method  string
		path    string
		body    []byte
	}{
		{"aio-svc", "anomaly_detect", http.MethodPost, "/api/v1/anomaly/detect", detectBody},
		{"aio-svc", "noise_compress", http.MethodPost, "/api/v1/noise/compress", compressBody},
		{"plugin-svc", "plugin_list", http.MethodGet, "/api/v1/plugins", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := requireService(t, urls, tc.service)
			code, body := doHTTP(t, client, tc.method, base+tc.path, tc.body)
			if code != http.StatusOK {
				t.Fatalf("%s %s%s -> %d，期望 200；响应体=%s", tc.method, base, tc.path, code, truncate(string(body), 200))
			}
			assertJSON(t, fmt.Sprintf("%s %s%s", tc.method, base, tc.path), body)
		})
	}
}

// TestCostOptimizationPipeline 校验 portal-svc 成本推荐端点的真实 HTTP 契约。
func TestCostOptimizationPipeline(t *testing.T) {
	urls := integrationBaseURLs(t)
	base := requireService(t, urls, "portal-svc")
	client := newIntegrationClient()

	code, body := doHTTP(t, client, http.MethodGet, base+"/api/v1/cost/recommendations", nil)
	if code != http.StatusOK {
		t.Fatalf("成本优化链路失败：GET %s/api/v1/cost/recommendations -> %d，期望 200；响应体=%s",
			base, code, truncate(string(body), 200))
	}
	assertJSON(t, "GET /api/v1/cost/recommendations", body)
}

// TestResourceRequestPipeline 校验 portal-svc 资源申请列表端点的真实 HTTP 契约。
func TestResourceRequestPipeline(t *testing.T) {
	urls := integrationBaseURLs(t)
	base := requireService(t, urls, "portal-svc")
	client := newIntegrationClient()

	code, body := doHTTP(t, client, http.MethodGet, base+"/api/v1/requests", nil)
	if code != http.StatusOK {
		t.Fatalf("资源申请链路失败：GET %s/api/v1/requests -> %d，期望 200；响应体=%s",
			base, code, truncate(string(body), 200))
	}
	assertJSON(t, "GET /api/v1/requests", body)
}

// TestGrafanaQueryPipeline 校验 grafana-bridge 查询端点（POST /query）的真实 HTTP 契约。
func TestGrafanaQueryPipeline(t *testing.T) {
	urls := integrationBaseURLs(t)
	base := requireService(t, urls, "grafana-bridge")
	client := newIntegrationClient()

	reqBody, err := json.Marshal(map[string]interface{}{
		"range": map[string]interface{}{
			"from": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			"to":   time.Now().Format(time.RFC3339),
		},
		"targets": []map[string]interface{}{
			{"target": "cpu_usage", "type": "timeseries"},
		},
	})
	if err != nil {
		t.Fatalf("构造 Grafana 查询请求体失败: %v", err)
	}

	code, body := doHTTP(t, client, http.MethodPost, base+"/query", reqBody)
	if code != http.StatusOK {
		t.Fatalf("Grafana 查询链路失败：POST %s/query -> %d，期望 200；响应体=%s",
			base, code, truncate(string(body), 200))
	}
	// /query 返回时序数组（table 类型时返回对象），二者都是合法 JSON。
	assertJSON(t, "POST /query", body)
}

// ============================================================================
// A 类：纯算法链路（无需外部服务，默认执行）
// ============================================================================

// TestAnomalyDetectionPipeline 用真实基线检测器（internal/alertengine）跑异常检测链路。
//
// 断言点：
//  1. 基线窗口统计量正确（19 个点，均值 10，总体标准差 > 0）；
//  2. 突变值 100 被判为异常，基线内取值 10 不判异常；
//  3. 样本不足（n<2）时不判异常（Z-Score 无定义）。
func TestAnomalyDetectionPipeline(t *testing.T) {
	// 基线：10/11/9 三值循环的前 19 个点（均值 10，总体标准差 ≈0.795）。
	baseline := []float64{10, 11, 9, 10, 11, 9, 10, 11, 9, 10, 11, 9, 10, 11, 9, 10, 11, 9, 10}
	if len(baseline) != 19 {
		t.Fatalf("基线样本数 = %d，期望 19", len(baseline))
	}

	det := alertengine.NewBaselineDetector(20, 3.0) // 窗口 20，3σ 阈值
	for _, v := range baseline {
		det.Add(v)
	}

	mean, stdDev, count := det.Stats()
	if count != 19 {
		t.Fatalf("窗口样本数 = %d，期望 19（窗口容量 20，未溢出）", count)
	}
	if math.Abs(mean-10) > 1e-6 {
		t.Fatalf("基线均值 = %.6f，期望 10", mean)
	}
	if stdDev <= 0 {
		t.Fatalf("基线标准差 = %.6f，期望 > 0（否则 Z-Score 恒不触发）", stdDev)
	}

	z := math.Abs(100-mean) / stdDev
	if !det.IsAnomaly(100) {
		t.Fatalf("突变值 100 应判为异常（mean=%.3f stdDev=%.3f z=%.2f > 阈值 3.0）", mean, stdDev, z)
	}
	if z <= 3.0 {
		t.Fatalf("突变值 Z-Score = %.2f，期望 > 3.0（否则用例本身失效）", z)
	}
	if det.IsAnomaly(10) {
		t.Fatalf("基线内取值 10 不应判为异常（z=0）")
	}

	// 样本不足：单点窗口应恒返回 false（Z-Score 无定义，不能误报）。
	sparse := alertengine.NewBaselineDetector(20, 3.0)
	sparse.Add(10)
	if sparse.IsAnomaly(1000) {
		t.Fatalf("样本数不足（n=1）时不应判异常")
	}
}

// TestAlertNoiseReductionPipeline 用真实聚合器（internal/alertengine.Aggregator）跑告警降噪链路。
//
// 断言点：
//  1. 按 ruleID 分组：4 条告警（3×cpu_high + 1×mem_high）收敛为 2 组；
//  2. 分组键格式与组内事件归属正确；
//  3. maxGroup 限流生效（每组最多保留指定条数）；
//  4. 空输入返回空切片而非 nil。
func TestAlertNoiseReductionPipeline(t *testing.T) {
	events := []*alertengine.AlertEvent{
		{RuleID: "cpu_high", DeviceID: "srv-01-web", Severity: "critical", Labels: map[string]string{"ruleID": "cpu_high", "deviceID": "srv-01-web", "severity": "critical"}},
		{RuleID: "cpu_high", DeviceID: "srv-02-web", Severity: "critical", Labels: map[string]string{"ruleID": "cpu_high", "deviceID": "srv-02-web", "severity": "critical"}},
		{RuleID: "cpu_high", DeviceID: "srv-03-web", Severity: "critical", Labels: map[string]string{"ruleID": "cpu_high", "deviceID": "srv-03-web", "severity": "critical"}},
		{RuleID: "mem_high", DeviceID: "srv-01-web", Severity: "warning", Labels: map[string]string{"ruleID": "mem_high", "deviceID": "srv-01-web", "severity": "warning"}},
	}
	if len(events) != 4 {
		t.Fatalf("输入告警数 = %d，期望 4", len(events))
	}

	groups := alertengine.NewAggregator([]string{"ruleID"}, 0).Aggregate(events)
	if len(groups) != 2 {
		t.Fatalf("按 ruleID 分组数 = %d，期望 2（cpu_high / mem_high）", len(groups))
	}
	// 输出按 Key 升序：cpu_high 在 mem_high 之前。
	wantKeys := []string{"ruleID=cpu_high", "ruleID=mem_high"}
	for i, want := range wantKeys {
		if groups[i].Key != want {
			t.Fatalf("groups[%d].Key = %q，期望 %q", i, groups[i].Key, want)
		}
	}
	if len(groups[0].Events) != 3 {
		t.Fatalf("cpu_high 组内事件数 = %d，期望 3", len(groups[0].Events))
	}
	if len(groups[1].Events) != 1 {
		t.Fatalf("mem_high 组内事件数 = %d，期望 1", len(groups[1].Events))
	}
	for _, ev := range groups[0].Events {
		if ev.RuleID != "cpu_high" {
			t.Fatalf("cpu_high 组混入其他规则事件: %q", ev.RuleID)
		}
	}

	// maxGroup 限流：每组最多保留 2 条。
	limited := alertengine.NewAggregator([]string{"ruleID"}, 2).Aggregate(events)
	if len(limited) != 2 {
		t.Fatalf("限流后分组数 = %d，期望 2", len(limited))
	}
	if len(limited[0].Events) != 2 {
		t.Fatalf("限流后 cpu_high 组内事件数 = %d，期望 2（maxGroup=2）", len(limited[0].Events))
	}

	// 空输入：返回空切片而非 nil（调用方 range 安全）。
	empty := alertengine.NewAggregator([]string{"ruleID"}, 0).Aggregate(nil)
	if empty == nil {
		t.Fatal("空输入应返回非 nil 空切片")
	}
	if len(empty) != 0 {
		t.Fatalf("空输入分组数 = %d，期望 0", len(empty))
	}
}

// lifecyclePlugin 记录生命周期调用顺序的测试插件。
type lifecyclePlugin struct {
	name     string
	initCfg  any
	initDone bool
	closed   bool
}

func (p *lifecyclePlugin) Name() string    { return p.name }
func (p *lifecyclePlugin) Version() string { return "1.0.0" }
func (p *lifecyclePlugin) Init(cfg any) error {
	p.initCfg = cfg
	p.initDone = true
	return nil
}
func (p *lifecyclePlugin) Close() error {
	p.closed = true
	return nil
}

// TestPluginLifecyclePipeline 用真实插件管理器（internal/plugin.Manager）跑插件生命周期链路。
//
// 断言点：注册→Init→查询→钩子触发（含短路）→反注册→Close 的完整链路，
// 以及重复注册被拒、反注册不存在的插件返回 error。
func TestPluginLifecyclePipeline(t *testing.T) {
	mgr := plugin.NewManager()
	defer func() {
		if err := mgr.Close(); err != nil {
			t.Fatalf("Manager.Close: %v", err)
		}
	}()

	p := &lifecyclePlugin{name: "integration-demo"}
	cfg := map[string]string{"endpoint": "http://127.0.0.1:9100"}

	// 1. Register：成功并立即 Init。
	if err := mgr.Register(p, cfg); err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if !p.initDone {
		t.Fatal("Register 后插件 Init 未被调用")
	}
	if got := mgr.GetPlugin("integration-demo"); got == nil {
		t.Fatal("Register 后 GetPlugin 返回 nil")
	}
	if len(mgr.AllPlugins()) != 1 {
		t.Fatalf("AllPlugins 数量 = %d，期望 1", len(mgr.AllPlugins()))
	}

	// 2. 重复注册应被拒绝（按名去重，不覆盖）。
	if err := mgr.Register(&lifecyclePlugin{name: "integration-demo"}, nil); err == nil {
		t.Fatal("重复注册同名插件应返回 error")
	}

	// 3. FireHook：按注册顺序触发，可读取并修改 Payload。
	hook := plugin.Hook("integration.pipeline")
	var fired int
	if err := mgr.RegisterHook(hook, func(ev plugin.Event) error {
		fired++
		if ev.Payload != "ping" {
			t.Errorf("钩子收到 Payload = %v，期望 \"ping\"", ev.Payload)
		}
		return nil
	}); err != nil {
		t.Fatalf("RegisterHook 失败: %v", err)
	}
	if err := mgr.FireHook(context.Background(), hook, plugin.Event{Payload: "ping"}); err != nil {
		t.Fatalf("FireHook 失败: %v", err)
	}
	if fired != 1 {
		t.Fatalf("钩子触发次数 = %d，期望 1", fired)
	}

	// 4. FireHook 短路：handler 返回 error 阻断后续 handler。
	var second int
	if err := mgr.RegisterHook(hook, func(plugin.Event) error {
		second++
		return nil
	}); err != nil {
		t.Fatalf("RegisterHook（第二个）失败: %v", err)
	}
	blocker := plugin.Hook("integration.block")
	if err := mgr.RegisterHook(blocker, func(plugin.Event) error { return fmt.Errorf("阻断") }); err != nil {
		t.Fatalf("RegisterHook（blocker）失败: %v", err)
	}
	if err := mgr.RegisterHook(blocker, func(plugin.Event) error {
		second++
		return nil
	}); err != nil {
		t.Fatalf("RegisterHook（blocker 后续）失败: %v", err)
	}
	if err := mgr.FireHook(context.Background(), blocker, plugin.Event{}); err == nil {
		t.Fatal("handler 返回 error 时 FireHook 应返回 error（短路语义）")
	}
	if second != 0 {
		t.Fatalf("短路后不应执行后续 handler，但执行了 %d 次", second)
	}

	// 5. Unregister：调用 Close 并移除，再查应返回 nil。
	if err := mgr.Unregister("integration-demo"); err != nil {
		t.Fatalf("Unregister 失败: %v", err)
	}
	if !p.closed {
		t.Fatal("Unregister 后插件 Close 未被调用")
	}
	if got := mgr.GetPlugin("integration-demo"); got != nil {
		t.Fatalf("Unregister 后 GetPlugin 应返回 nil，got %v", got)
	}
	if err := mgr.Unregister("integration-demo"); err == nil {
		t.Fatal("反注册不存在的插件应返回 error")
	}
}
