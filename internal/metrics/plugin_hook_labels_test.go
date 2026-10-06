// plugin_hook_labels_test.go 钉住「指标标签集合 ↔ 冻结扩展点清单」一致（TD-62）。
//
// # 为什么这条必须存在
//
// pluginHookValues 是手写字符串，AllHooks() 是另一个包里的另一份手写清单。
// 两者一旦分叉（新增扩展点时只改了 hooks.go），后果不是报错而是**静默**：
// 新扩展点的调用全部落进 hook="__other__"，
// 于是面板上那按扩展点拆开的曲线少一条、告警里针对具体扩展点的表达式永远看不到它，
// 而"能力已交付"的宣称照样成立。这类漂移只有对账能发现。
package metrics

import (
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/plugin"
)

func TestPluginHookLabelSetMatchesFrozenHooks(t *testing.T) {
	want := map[string]bool{}
	for _, h := range plugin.AllHooks() {
		want[string(h)] = true
	}
	got := map[string]bool{}
	for _, v := range pluginHookValues {
		if v == pluginHookOther {
			t.Errorf("保留标签 %q 不能出现在 pluginHookValues 里（它是收敛兜底，不是扩展点）", v)
		}
		got[v] = true
	}
	for _, h := range plugin.AllHooks() {
		if !got[string(h)] {
			t.Errorf("扩展点 %q 在 plugin.AllHooks() 里，但 metrics.pluginHookValues 没有它——"+
				"该扩展点的调用会静默落进 hook=%q，面板与告警都看不见它", h, pluginHookOther)
		}
	}
	for v := range got {
		if !want[v] {
			t.Errorf("metrics.pluginHookValues 里的 %q 不在 plugin.AllHooks() 中（清单被删掉但仍留在标签里）", v)
		}
	}
	if len(pluginOutcomeValues) < 3 {
		t.Errorf("outcome 标签至少要有 ok/denied/error 三值，才能区分「策略拒绝」与「运维故障」：%s",
			strings.Join(pluginOutcomeValues, ","))
	}
}

// TestPluginSeriesRenderAtZero 钉住"首次失败之前也要有序列"（同 P1-66 的教训）：
// 一次事件都没发生时，counter 族仍须按固定标签全量输出 0，
// 否则告警表达式在新装环境上无序列可比较。
func TestPluginSeriesRenderAtZero(t *testing.T) {
	out := New().Render()
	for _, h := range pluginHookValues {
		for _, o := range pluginOutcomeValues {
			ln := `opsmesh_plugin_hook_calls_total{hook="` + h + `",outcome="` + o + `"} 0`
			if !strings.Contains(out, ln) {
				t.Errorf("渲染里缺恒零序列：%s", ln)
			}
		}
	}
	if !strings.Contains(out, "opsmesh_plugin_remote_plugins 0") {
		t.Error("渲染里缺 opsmesh_plugin_remote_plugins（未启用插件时也必须能看到 0）")
	}
}

// TestIncPluginHookConvergesLabels 证明基数控制权不落在调用方参数上。
func TestIncPluginHookConvergesLabels(t *testing.T) {
	m := New()
	before := len(seriesLines(outLines(m)))
	m.IncPluginHook("config.preSet", "ok")
	m.IncPluginHook("一个没登记的扩展点", "随便什么结局")
	after := len(seriesLines(outLines(m)))
	if before != after {
		t.Errorf("任意标签值新增了时序（%d → %d）：基数失控", before, after)
	}
	out := m.Render()
	if !strings.Contains(out, `{hook="__other__",outcome="error"} 1`) {
		t.Errorf("未登记的扩展点应归入 __other__，未知结局应归入 error：%s", out)
	}
	if !strings.Contains(out, `{hook="config.preSet",outcome="ok"} 1`) {
		t.Error("合法标签未被计数")
	}
}

func outLines(m *M) []string { return strings.Split(m.Render(), "\n") }

func seriesLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, l)
	}
	return out
}
