package gates

// hpa_metric_gate_test.go 拦"HPA 引用一个根本没产出的指标"这类假自动扩缩容。
//
// 真实缺陷（#62）：deploy/k8s/hpa/gpu-svc-hpa.yaml 曾按 `type: Pods` 的
// `opsmesh_gpu_queue_depth` 扩容，而这个名字在代码里零命中。HPA 对这种情况
// **不报错**，只是永远拿不到 desired 值 ⇒ `kubectl get hpa` 一个 <unknown>，
// 运维以为弹性伸缩配好了。这类"配了但恒不生效"正是本仓反复出的形态。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// podsMetricName 抓 `type: Pods` 指标段里的 metric.name。
//
// 必须逐层锚定（pods: → metric: → name:）：`behavior.policies` 里也有
// `- type: Pods / value: N`（那是"每次增减几个副本"，不是指标），
// 用宽松正则会把紧跟其后的 Resource 段 name: cpu 误抓成 Pods 指标。
var podsMetricName = regexp.MustCompile(`(?s)type:\s*Pods\s*\n\s*pods:\s*\n\s*metric:\s*\n\s*name:\s*([A-Za-z0-9_:]+)`)

func TestHPAPodMetricsMustBeProduced(t *testing.T) {
	root := repoRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "deploy", "k8s", "hpa", "*.yaml"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("deploy/k8s/hpa 下没有清单——扫描面塌了，本门禁会空转")
	}

	// 收集代码里真实产出的指标名（字面量），以及 adapter 侧允许的前缀写法。
	produced := producedMetricNames(t, root)

	checked := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读 %s: %v", f, err)
		}
		for _, m := range podsMetricName.FindAllStringSubmatch(string(b), -1) {
			name := m[1]
			checked++
			if !metricProduced(produced, name) {
				t.Errorf("%s 引用 Pods 指标 %q，但代码里没有任何地方产出它——HPA 会静默不扩容。"+
					"要么改成 Resource cpu，要么先导出该指标并配 prometheus-adapter 映射", f, name)
			}
		}
	}
	t.Logf("扫描 %d 个 HPA 清单，Pods 型指标引用 %d 处（0 处也合法：Resource cpu 是开箱可用的那条路）",
		len(files), checked)
}

// producedMetricNames 扫 services/ 与 pkg/ 下的 Go 源码，收集
// SetBusinessMetric/AddBusinessMetric/SetGauge 的字面量指标名。
func producedMetricNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	var files []string
	for _, dir := range []string{"services", "pkg", "internal"} {
		base := filepath.Join(root, dir)
		// 整树遍历：按固定层级猜路径会因目录深度漏文件，而"扫不到"在这里的表现
		// 正是门禁静默判绿——最坏的一种失败。
		if err := filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			files = append(files, p)
			return nil
		}); err != nil {
			t.Fatalf("遍历 %s: %v", base, err)
		}
	}
	lit := regexp.MustCompile(`(?:SetBusinessMetric|AddBusinessMetric|SetGauge|observeMetric)\(\s*"([a-z0-9_]+)"`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range lit.FindAllStringSubmatch(string(b), -1) {
			out[m[1]] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("一个业务指标名都没扫到——扫描面坏了，本门禁会空转判绿")
	}
	return out
}

func metricProduced(produced map[string]bool, name string) bool {
	if produced[name] {
		return true
	}
	// adapter 常给指标加前缀（如 opsmesh_ / custom:opsmesh_），允许"去掉非字母数字下划线
	// 前缀后与产出名一致"这一种映射写法。
	trimmed := name
	for {
		i := strings.IndexAny(trimmed, ":_")
		if i < 0 {
			break
		}
		if produced[trimmed[i+1:]] {
			return true
		}
		trimmed = trimmed[i+1:]
	}
	return false
}
