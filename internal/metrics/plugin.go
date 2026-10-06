// plugin.go — 插件扩展点调用的可观测序列（TD-62 独立进程插件模型）。
//
// # 为什么必须有这三个序列
//
// 远程插件是"控制面在运行期同步调用一个外部进程"：它一定会有不可达、超时、返回 5xx、
// decision=deny 四种结局。若这些只写日志，运维侧看到的就只是"配置改不动了"，
// 而 pre 钩子的 fail-closed 语义（见 internal/plugin/remote.go）让"插件挂了"与
// "插件投了反对票"在客户端长得一模一样。计数器把这两件事分开：
//
//	outcome="denied"  插件在线且明确拒绝 —— 业务语义，通常是策略正确工作
//	outcome="error"   调用本身失败（不可达/超时/非 2xx/坏 JSON）—— 运维故障，要告警
//
// # 标签取值为什么是固定枚举
//
// 与 opsmesh_agent_signature_verifications_total 同一条判据（P1-5 基数控制）：
// **插件名刻意不进标签**。插件名来自运维自写的清单文件，是自由文本，
// 拼错一个字母或多接十个插件都会新增时序；把基数控制权交给配置文件，
// 与"算法声明由 agent 自选所以不能入标签"是同一种错误。插件名进日志与审计，
// 定位到人靠日志；定位到"哪个扩展点在失败"靠 hook 标签。
package metrics

import "fmt"

// pluginHookValues 扩展点标签的固定取值集合。
//
// ⚠️ 必须与 internal/plugin/hooks.go 的 AllHooks() 一一对应，
// 由 internal/gates/plugin_metric_labels_gate_test.go 强制：漂移即判红。
// 未在此列出的入参一律归入 pluginHookOther（可见但不新增时序）。
var pluginHookValues = []string{"config.preSet", "config.postSet", "task.preClaim"}

// pluginHookOther 是未登记扩展点的收敛标签。
const pluginHookOther = "__other__"

// pluginOutcomeValues 调用结局的固定取值集合（顺序即渲染顺序）。
var pluginOutcomeValues = []string{"ok", "denied", "error"}

// SetPluginRemotePlugins 写入当前已注册的独立进程插件数量（快照 gauge）。
//
// 为什么需要它而不是只看调用计数：能力"接了但没配"与"配了没被调用"是两种不同状态，
// 前者应为 0、后者应为 N 且调用计数恒 0。升级/交付验收要能一眼区分。
func (m *M) SetPluginRemotePlugins(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pluginRemoteCount = int64(n)
}

// IncPluginHook 记录一次扩展点调用结局。hook/outcome 均经收敛，不产生新时序。
func (m *M) IncPluginHook(hook, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pluginHooks == nil {
		m.pluginHooks = make(map[string]uint64)
	}
	// 未知结局按故障计：宁可高估运维问题，也不让一次真实失败落进不告警的桶。
	m.pluginHooks[oneOf(hook, pluginHookValues, pluginHookOther)+"|"+
		oneOf(outcome, pluginOutcomeValues, "error")]++
}

// oneOf 把自由文本标签值收敛到固定集合，不在集合内则归入 fallback。
func oneOf(v string, set []string, fallback string) string {
	for _, x := range set {
		if x == v {
			return v
		}
	}
	return fallback
}

// appendPluginMetrics 输出插件扩展点序列。调用方已持锁。
// 0 值也全量渲染（hook × outcome 笛卡尔积），使冷启动阶段告警与面板有序列可比较。
func (m *M) appendPluginMetrics(b []byte) []byte {
	b = append(b, "# HELP opsmesh_plugin_remote_plugins 已注册的独立进程插件数（0=能力已交付但本次部署未启用）\n"...)
	b = append(b, "# TYPE opsmesh_plugin_remote_plugins gauge\n"...)
	b = append(b, fmt.Sprintf("opsmesh_plugin_remote_plugins %d\n", m.pluginRemoteCount)...)

	b = append(b, "# HELP opsmesh_plugin_hook_calls_total 插件扩展点调用次数（按扩展点/结局；ok=放行，denied=插件明确拒绝，error=调用失败/不可达/坏响应）\n"...)
	b = append(b, "# TYPE opsmesh_plugin_hook_calls_total counter\n"...)
	hooks := append(append([]string{}, pluginHookValues...), pluginHookOther)
	for _, h := range hooks {
		for _, o := range pluginOutcomeValues {
			b = append(b, fmt.Sprintf("opsmesh_plugin_hook_calls_total{hook=%q,outcome=%q} %d\n",
				h, o, m.pluginHooks[h+"|"+o])...)
		}
	}
	return b
}
