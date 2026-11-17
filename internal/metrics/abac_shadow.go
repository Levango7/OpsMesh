// abac_shadow.go ABAC 影子模式（批次50）的 Prometheus 序列。
//
// 序列：opsmesh_abac_shadow_decisions_total{decision,method}
//   - decision：allow（策略允许）/ deny（含 default-deny 的 would-deny）/ error（评估器 panic）；
//   - method：POST/PUT/DELETE/PATCH（shadow 只观察变更方法）。
//
// 固定标签笛卡尔积全量渲染（含 0 值），与 sigVerifs/pluginHooks 同约定：
// 冷启动/未开启 shadow 时序列存在且为 0，仪表盘与告警不需要处理 absent()。
// 刻意不引入 path 标签：路径基数由请求方控制，会突破指标基数上限；
// 「would-deny 集中在哪些路径」从结构化日志的 path 字段聚合。
package metrics

import "fmt"

// shadow 决策与方法的固定标签取值（与 controlplane abac_shadow.go 保持一致）。
var (
	abacShadowDecisionValues = []string{"allow", "deny", "error"}
	abacShadowMethodValues   = []string{"POST", "PUT", "DELETE", "PATCH"}
)

// IncABACShadowDecision 累加一次 shadow 决策计数。
// decision/method 来自 controlplane 的固定枚举，不接受任意请求文本。
func (m *M) IncABACShadowDecision(decision, method string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.abacShadow == nil {
		m.abacShadow = make(map[string]uint64)
	}
	m.abacShadow[decision+"|"+method]++
}

// appendABACShadowMetrics 输出 shadow 决策计数。调用方已持锁。
// 0 值也全量渲染（decision × method 笛卡尔积）。
func (m *M) appendABACShadowMetrics(b []byte) []byte {
	b = append(b, "# HELP opsmesh_abac_shadow_decisions_total ABAC 影子模式决策次数（只观察不强制；deny=would-deny；error=评估器 panic）\n"...)
	b = append(b, "# TYPE opsmesh_abac_shadow_decisions_total counter\n"...)
	for _, decision := range abacShadowDecisionValues {
		for _, method := range abacShadowMethodValues {
			b = append(b, fmt.Sprintf("opsmesh_abac_shadow_decisions_total{decision=%q,method=%q} %d\n",
				decision, method, m.abacShadow[decision+"|"+method])...)
		}
	}
	return b
}
