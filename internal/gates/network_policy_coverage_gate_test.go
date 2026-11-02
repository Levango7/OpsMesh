package gates

// network_policy_coverage_gate_test.go 守的是另一类「加固写了但默认关着，而且没人表态」：
// 2026-11-02 实测（docs/semantic-rulings-2026-11-01.md 事实 2.5）——
//   - `deploy/helm/opsmesh/values.yaml` 里 `observability.networkPolicy.enabled: false`；
//   - `values-production.yaml` **完全没有这个键** ⇒ 生产安装沉默继承"关"；
//   - 模板只覆盖 controlplane 与 agent 两种 pod，**12 个微服务 pod 一条策略都没有**，
//     而模板头部注释自陈「默认拒绝所有入站」。
// 结果：ClusterIP 的微服务端口对集群内任意 pod 开放，租户闸又是 fail-open（省略 ?tenantID= 即跳过
// 归属校验）——注释承诺的边界与实发产物不一致，且没有任何东西会在有人把它改坏时说话。
//
// 本门禁不要求"必须开启"（翻默认值属现网变更，见 values-production 的理由注释），
// 它要求的是：**能力覆盖到每个组件** + **生产 values 必须显式表态并写理由**。
// "没人看过所以默认关"从此不能悄悄存在。

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	npTemplate = "deploy/helm/opsmesh/templates/network-policy.yaml"
	npValues   = "deploy/helm/opsmesh/values.yaml"
	npProdVals = "deploy/helm/opsmesh/values-production.yaml"
)

func TestNetworkPolicyCoversEveryComponent(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, npTemplate))
	if err != nil {
		t.Fatalf("读 %s: %v", npTemplate, err)
	}
	body := string(data)

	// 扫描面自证：策略必须仍整体挂在开关下——否则"默认关闭"的 values 注释就是假的，
	// 而本用例也会因为缺少特征而误判。
	if !strings.Contains(body, "{{- if .Values.observability.networkPolicy.enabled -}}") {
		t.Fatalf("%s 里找不到 networkPolicy.enabled 的开头守卫：模板与 values 的默认值口径已脱钩", npTemplate)
	}

	must := []struct {
		why  string
		frag string
	}{
		{"控制面 pod 策略", "component: controlplane"},
		{"agent pod 策略", "component: agent"},
		{"微服务 pod 策略必须按 values 的服务清单逐个渲染", `{{- range $name, $svc := .Values.services }}`},
		{"微服务策略必须同时限 Ingress 与 Egress", "- Egress"},
		{"微服务必须放行控制面（svcproxy 转发）", "component: controlplane"},
		{"agent 自举路径必须放行 device-svc（install.sh / provision register）", `eq $name "device_svc"`},
		{"微服务必须能出向 MySQL/Redis（storeType=sql 的落库路径）", "- port: 3306"},
	}
	for _, m := range must {
		if !strings.Contains(body, m.frag) {
			t.Errorf("%s 在 %s 中缺失（判据片段 %q）——策略覆盖面缩水就是这种红", m.why, npTemplate, m.frag)
		}
	}

	// 组件覆盖的对账：模板里出现的 component 标签必须至少是这三类 + 微服务 range。
	comps := regexp.MustCompile(`app\.kubernetes\.io/component: (\{\{[^}]*\}\}|[a-z-]+)`).FindAllStringSubmatch(body, -1)
	seen := map[string]bool{}
	for _, c := range comps {
		seen[c[1]] = true
	}
	if len(seen) < 3 {
		t.Errorf("只识别到 %d 种 component 选择器（%v）——特征正则失效，本门禁会空转判绿", len(seen), seen)
	}
	// 已知仍未覆盖的组件（如实记录，别让"全绿"被读成"全隔离"）：
	// mysql/redis 的 pod 本身没有策略（只作为放行目标出现）——DBA 直连与备份作业的路径未清，
	// 属后续项，不在本门禁的判据里。
	for _, need := range []string{"controlplane", "agent"} {
		if !seen[need] {
			t.Errorf("component %q 没有对应 NetworkPolicy", need)
		}
	}
	if !body2HasMicroRange(body) {
		t.Error("没有按 .Values.services 渲染的微服务策略——12 个微服务 pod 不受任何 NetworkPolicy 约束")
	}
}

func body2HasMicroRange(body string) bool {
	i := strings.Index(body, "range $name, $svc := .Values.services")
	if i < 0 {
		return false
	}
	return strings.Contains(body[i:], "kind: NetworkPolicy")
}

func TestNetworkPolicyFlagIsExplicitInProductionValues(t *testing.T) {
	root := repoRoot(t)

	base, err := os.ReadFile(filepath.Join(root, npValues))
	if err != nil {
		t.Fatalf("读 %s: %v", npValues, err)
	}
	if !strings.Contains(string(base), "networkPolicy:") {
		t.Fatalf("%s 里没有 networkPolicy 键——本门禁的特征需要随 values 结构同步", npValues)
	}

	prodData, err := os.ReadFile(filepath.Join(root, npProdVals))
	if err != nil {
		t.Fatalf("读 %s: %v", npProdVals, err)
	}
	prodLines := strings.Split(string(prodData), "\n")

	idx := -1
	for i, l := range prodLines {
		if strings.HasPrefix(strings.TrimSpace(l), "networkPolicy:") {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("%s 没有显式的 observability.networkPolicy 表态 ⇒ 生产安装沉默继承默认值。"+
			"要么开（并确认抓取来源与服务间互调矩阵），要么关（写清为什么），不能不表态。", npProdVals)
	}

	// 表态必须有值，且附近要有理由注释（只有一行 enabled 而无说明＝把问题从"沉默"改成"沉默但合法"）。
	window := prodLines[idx:]
	if len(window) > 16 {
		window = window[:16]
	}
	joined := strings.Join(window, "\n")
	if !regexp.MustCompile(`enabled:\s*(true|false)`).MatchString(joined) {
		t.Errorf("%s 的 networkPolicy 块里没有 enabled 值", npProdVals)
	}
	// 理由注释必须**就在本键上方且谈的是本键**：只要求"附近有注释"会被上一块（prometheusRule）的
	// 注释顶掉——变异实测过这个弱点（删掉理由注释仍然判绿），所以按关键词收口。
	// 窗口取 20 行：本键的理由块实测 11 行，10 行窗口会把首行（含关键词）切掉造成假红。
	above := strings.Join(prodLines[max(0, idx-20):idx], "\n")
	if !regexp.MustCompile(`(?m)^\s*#.*(networkPolicy|网络策略)`).MatchString(above) {
		t.Errorf("%s:%d 的 networkPolicy 缺**针对本键**的理由注释——显式表态必须写明为什么这么选"+
			"（翻 true 的前置条件也写在这里），否则等于把沉默换个位置", npProdVals, idx+1)
	}
}

// npStripLineComments 去掉整行 # 注释后再做结构断言：模板里刻意保留了对旧写法的引用
// （注释中写着 `podSelector: {}}` 以说明为什么删掉它），按原文扫描会把注释当实现。
func npStripLineComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// npPieceBetween 取 [start, 下一个 kind: NetworkPolicy) 之间的片段；找不到返回 false。
func npPieceBetween(body, start string) (string, bool) {
	i := strings.Index(body, start)
	if i < 0 {
		return "", false
	}
	rest := body[i+len(start):]
	j := strings.Index(rest, "kind: NetworkPolicy")
	if j < 0 {
		return "", false
	}
	return rest[:j], true
}

// TestNetworkPolicyHasNoBlanketPodSelector 守 2026-11-02 kind 实测暴露的放宽：
// `from: [{podSelector: {}}]` 本意"同命名空间任意 Pod"，实测连**跨命名空间**的 Pod 也放行。
// 结论是不再允许空白 peer——抓取来源必须显式配置（metricsFrom）。
func TestNetworkPolicyHasNoBlanketPodSelector(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, npTemplate))
	if err != nil {
		t.Fatalf("读 %s: %v", npTemplate, err)
	}
	rules := npStripLineComments(string(data))
	if strings.Contains(rules, "podSelector: {}") {
		t.Fatalf("%s 里仍有空白 podSelector（podSelector: {}）：按 kind 实测这等于对**任意命名空间**的 Pod 放行，"+
			"不是注释里写的\"仅同 ns\"。要用就得显式列出 peer（见 observability.networkPolicy.metricsFrom）", npTemplate)
	}
	// 反空转：剥注释后必须仍能识别出 podSelector 与 matchLabels，否则说明扫描特征失效、本用例在空转判绿。
	if !strings.Contains(rules, "podSelector:") || !strings.Contains(rules, "matchLabels:") {
		t.Fatal("剥注释后的模板里找不到 podSelector/matchLabels 特征——扫描面失效，本用例无法证明任何事")
	}
}

// TestNetworkPolicyControlplaneEgressAllowsProxiedServices 守 svcproxy 的转发路径：
// 控制面的 pod 策略同时限 Ingress+Egress，若 egress 不逐个放行已启用服务的 HTTP/gRPC 端口，
// 代理转发会被控制面自己的策略拦死（2026-11-02 kind 实测：cp→device-svc:8081 超时，整片 502）。
func TestNetworkPolicyControlplaneEgressAllowsProxiedServices(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, npTemplate))
	if err != nil {
		t.Fatalf("读 %s: %v", npTemplate, err)
	}
	section, ok := npPieceBetween(string(data), "}}-controlplane")
	if !ok {
		t.Fatal("定位不到 controlplane 策略段（模板结构变了，需同步本门禁）")
	}
	for _, frag := range []struct{ why, s string }{
		{"控制面策略必须仍限 Egress（否则下面的断言失去前提）", "- Egress"},
		{"控制面 egress 必须按已启用服务清单渲染（否则只覆盖写死的那几个）", "range $name, $svc := .Values.services"},
		{"控制面 egress 必须放行服务 HTTP 端口（svcproxy 转发目标）", "- port: {{ $svc.httpPort }}"},
		{"控制面 egress 必须放行服务 gRPC 端口（代理同时转发 gRPC）", "- port: {{ $svc.grpcPort }}"},
		{"控制面 egress 的放行目标必须按服务名选择 pod（不能放宽到整个 ns）", "app.kubernetes.io/component: {{ $name | replace \"_\" \"-\" }}"},
	} {
		if !strings.Contains(section, frag.s) {
			t.Errorf("controlplane 策略段缺失：%s（判据片段 %q）", frag.why, frag.s)
		}
	}
}

// TestNetworkPolicyAgentBootstrapPath 守 agent 自举：install.sh / provision register 直连
// device-svc（不经控制面代理），agent 只有出站策略 —— 不放行则首次纳管在网络层静默失败。
// 自举只走 HTTP；gRPC 端口不应顺带放行（2026-11-02 kind 实测该路径为 BLOCKED）。
func TestNetworkPolicyAgentBootstrapPath(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, npTemplate))
	if err != nil {
		t.Fatalf("读 %s: %v", npTemplate, err)
	}
	body := string(data)
	agent, ok := npPieceBetween(body, "}}-agent")
	if !ok {
		t.Fatal("定位不到 agent 策略段（模板结构变了，需同步本门禁）")
	}
	if !strings.Contains(agent, "if (.Values.services.device_svc).enabled") {
		t.Error("agent 的 device-svc 出站放行没有跟 device_svc.enabled 绑定：没装 device-svc 时会渲染一条指向不存在 pod 的规则")
	}
	if !strings.Contains(agent, "component: device-svc") || !strings.Contains(agent, ".Values.services.device_svc.httpPort") {
		t.Error("agent 自举路径缺失：必须放行 device-svc 的 HTTP 端口")
	}

	i := strings.Index(agent, "if (.Values.services.device_svc).enabled")
	if i < 0 {
		t.Fatal("device_svc 放行块不存在，无法检查端口面")
	}
	block := agent[i:]
	if j := strings.Index(block, "{{- end }}"); j >= 0 {
		block = block[:j]
	}
	if strings.Contains(block, "grpcPort") {
		t.Error("agent 自举只走 HTTP，device_svc 放行块里不应包含 gRPC 端口（实测该路径应保持 BLOCKED）")
	}
}

// TestNetworkPolicyMetricsScrapeIsOptIn 守 metrics 放行面：
// 模板必须把抓取来源绑到 metricsFrom，且 values 默认必须是空列表（默认不放行任何来源）。
func TestNetworkPolicyMetricsScrapeIsOptIn(t *testing.T) {
	root := repoRoot(t)

	tmpl, err := os.ReadFile(filepath.Join(root, npTemplate))
	if err != nil {
		t.Fatalf("读 %s: %v", npTemplate, err)
	}
	if !strings.Contains(string(tmpl), "and $svc.metrics $.Values.observability.networkPolicy.metricsFrom") {
		t.Error("metrics 放行没有同时受 $svc.metrics 与 metricsFrom 控制——默认可能又变成无条件放行")
	}

	vals, err := os.ReadFile(filepath.Join(root, npValues))
	if err != nil {
		t.Fatalf("读 %s: %v", npValues, err)
	}
	if !strings.Contains(string(vals), "metricsFrom: []") {
		t.Error("values.yaml 的 metricsFrom 默认值不是空列表：抓取来源必须由安装方显式声明")
	}
}

// TestNetworkPolicyProductionFlipRequiresExternalEgress 是给"生产默认翻 true"上的棘轮。
// 2026-11-02 kind 实测：策略生效时 cp→api.kilo.ai:443 被拦（未选中 pod 同目标连通），
// 而控制面进程需要出向 LLM 网关/webhook/federation。⇒ 翻 true 必须同时具备两件事：
// ① 模板提供可配置出向机制（controlplaneEgressExtra，默认空 ⇒ 不自带宽松默认）；
// ② 生产 values 真的声明了外部放行（含 ipBlock）。只有其一都判红。
func TestNetworkPolicyProductionFlipRequiresExternalEgress(t *testing.T) {
	root := repoRoot(t)

	prodData, err := os.ReadFile(filepath.Join(root, npProdVals))
	if err != nil {
		t.Fatalf("读 %s: %v", npProdVals, err)
	}
	lines := strings.Split(string(prodData), "\n")
	idx := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "networkPolicy:") {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("values-production 缺少 networkPolicy 表态（由另一个用例报告，这里不重复）")
	}
	enabled := false
	window := lines[idx:]
	if len(window) > 6 {
		window = window[:6]
	}
	if regexp.MustCompile(`enabled:\s*true`).MatchString(strings.Join(window, "\n")) {
		enabled = true
	}

	tmpl, err := os.ReadFile(filepath.Join(root, npTemplate))
	if err != nil {
		t.Fatalf("读 %s: %v", npTemplate, err)
	}
	if err := npCheckProdFlip(enabled, npStripLineComments(string(tmpl)), npStripLineComments(string(prodData))); err != nil {
		t.Fatal(err)
	}

	// 反空转：判断逻辑本身要被四条用例钉住（两条"缺一即红"的负例），
	// 否则当前 enabled=true 的实现会让某一边的缺失悄悄通过。
	cases := []struct {
		name    string
		enabled bool
		tmpl    string
		prod    string
		wantErr bool
	}{
		{"关闭时不要求任何机制", false, "", "", false},
		{"开启但模板无出向机制", true, "egress: []", "ipBlock:\n  cidr: 0.0.0.0/0", true},
		{"开启有机制但生产未声明 ipBlock", true, "controlplaneEgressExtra:", "enabled: true", true},
		{"开启+机制+生产 ipBlock", true, "controlplaneEgressExtra:", "ipBlock:\n  cidr: 0.0.0.0/0", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if gotErr := npCheckProdFlip(c.enabled, c.tmpl, c.prod); (gotErr != nil) != c.wantErr {
				t.Errorf("npCheckProdFlip 判定错误：err=%v", gotErr)
			}
		})
	}
}

func npCheckProdFlip(enabled bool, strippedTemplate, strippedProdValues string) error {
	if !enabled {
		return nil
	}
	if !strings.Contains(strippedTemplate, "controlplaneEgressExtra") {
		return fmt.Errorf("%s 已把 networkPolicy.enabled 翻成 true，但 %s 没有 controlplaneEgressExtra 出向机制："+
			"控制面的 LLM 网关/webhook/federation 出向会被默认拒绝语义拦死（2026-11-02 kind 实测 cp→api.kilo.ai:443 超时）",
			npProdVals, npTemplate)
	}
	if !strings.Contains(strippedProdValues, "ipBlock:") {
		return fmt.Errorf("%s 已把 networkPolicy.enabled 翻成 true，但没有声明任何 ipBlock 外部放行："+
			"模板提供的是默认空的机制，不声明等于出向仍被拦", npProdVals)
	}
	return nil
}

// TestNetworkPolicyExternalEgressIsOptIn 守"机制本身不自带宽松默认"：
// 模板必须用 with 渲染 controlplaneEgressExtra（空值不产出规则），剥掉注释后不得出现
// 任何 0.0.0.0/0 字面量；values.yaml 的默认值必须是空列表。
func TestNetworkPolicyExternalEgressIsOptIn(t *testing.T) {
	root := repoRoot(t)

	tmpl, err := os.ReadFile(filepath.Join(root, npTemplate))
	if err != nil {
		t.Fatalf("读 %s: %v", npTemplate, err)
	}
	body := string(tmpl)
	if !strings.Contains(body, "with .Values.observability.networkPolicy.controlplaneEgressExtra") {
		t.Error("模板没有用 with 渲染 controlplaneEgressExtra：机制缺失或改成无条件渲染")
	}
	if strings.Contains(npStripLineComments(body), "0.0.0.0/0") {
		t.Error("模板自带 0.0.0.0/0 字面量：外部出向必须由安装方在 values 里显式声明，不能藏在模板默认里")
	}

	vals, err := os.ReadFile(filepath.Join(root, npValues))
	if err != nil {
		t.Fatalf("读 %s: %v", npValues, err)
	}
	if !strings.Contains(string(vals), "controlplaneEgressExtra: []") {
		t.Error("values.yaml 的 controlplaneEgressExtra 默认值不是空列表：控制面外部出向默认必须不放行")
	}
}
