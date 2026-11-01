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
