// abac_enforce.go ABAC 定向强制（批次53）的 Prometheus 序列。
//
// 序列：opsmesh_abac_enforce_decisions_total{decision,method}
//   - decision：allow（放行）/ deny（显式 deny 或 default-deny 的 403）/ error（评估器 panic 后 fail-closed）；
//   - method：POST/PUT/DELETE/PATCH（白名单只允许变更方法）。
//
// 与 shadow 指标同约定：标签取值是固定小集合，渲染时输出完整笛卡尔积（含 0 值），
// 仪表盘不需要处理 absent()。刻意不带 path 标签：强制面只覆盖显式白名单，
// 路径基数有限但仍由请求路径推导，保持与 shadow 一致的基数策略。
package metrics

import "fmt"

// enforce 决策与方法的固定标签取值（与 controlplane abac_enforce.go 保持一致）。
var (
	abacEnforceDecisionValues = []string{"allow", "deny", "error"}
	abacEnforceMethodValues   = []string{"POST", "PUT", "DELETE", "PATCH"}
)

// IncABACEnforceDecision 累加一次定向强制决策计数。
// decision/method 来自 controlplane 的固定枚举，不接受任意请求文本。
func (m *M) IncABACEnforceDecision(decision, method string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.abacEnforce == nil {
		m.abacEnforce = make(map[string]uint64)
	}
	m.abacEnforce[decision+"|"+method]++
}

// appendABACEnforceMetrics 输出定向强制决策计数。调用方已持锁。
func (m *M) appendABACEnforceMetrics(b []byte) []byte {
	b = append(b, "# HELP opsmesh_abac_enforce_decisions_total ABAC 定向强制决策次数（仅显式白名单路由；deny=403；error=评估器 panic 后 fail-closed）\n"...)
	b = append(b, "# TYPE opsmesh_abac_enforce_decisions_total counter\n"...)
	for _, decision := range abacEnforceDecisionValues {
		for _, method := range abacEnforceMethodValues {
			b = append(b, fmt.Sprintf("opsmesh_abac_enforce_decisions_total{decision=%q,method=%q} %d\n",
				decision, method, m.abacEnforce[decision+"|"+method])...)
		}
	}
	return b
}
