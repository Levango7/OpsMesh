// leader_metrics.go — leader 判据与受其门控的后台循环可见性。
//
// 背景（2026-11-23 审计 §2.F）：TD-60 把选主权交给 task-svc 后，控制面本地
// leader_lease 不再续租，`SQLStore.IsLeader()` 在 SQL 模式下恒 false；readyz 已改走
// electorClient，但 schedule/reclaim/archive/audit/deploy/workflow 等循环仍以
// `s.store.IsLeader()` 为门控 ⇒ 这些循环静默不执行。
//
// 本文件只做「可见性」：不替业务侧决定谁是 leader，也不改变任何循环行为。
// 两个序列：
//   - opsmesh_leader_status：当前实例角色（-1=未知 / 0=follower / 1=leader）；
//   - opsmesh_leader_gated_loop_total{loop,result}：每个 leader-only 循环本轮
//     ran 还是 skipped。SQL 模式下若 leader_status=1 而某循环 skipped 持续增长，
//     就是 §2.F 的静默失效直接证据。
//
// 标签取值固定小集合，零值全量输出（避免 absent() 陷阱）。
package metrics

import "fmt"

// leaderStatusUnknown 表示选主结论不可确认（electorClient RPC 失败等）。
// 刻意用 -1 而不是 0：0 是明确的 follower，-1 是「不知道」，混用会让告警误判。
const leaderStatusUnknown = int64(-1)

// leaderGatedLoopNames 是受 leader 门控的后台循环固定集合。
// 未知输入折叠到 unknown，保证标签基数有界。
var leaderGatedLoopNames = []string{
	"archive", "audit", "autoprovision", "deploy", "reclaim", "schedule", "timeout", "workflow", "unknown",
}

// leaderGatedLoopResults 是循环本轮结果的固定集合。
var leaderGatedLoopResults = []string{"ran", "skipped"}

// SetLeaderStatus 记录当前实例的选主判定结果。
//
// known=false 表示结论不可确认（选主 RPC 失败），状态置为 -1；
// 调用方不应在失败时把 unknown 当 follower 处理。
func (m *M) SetLeaderStatus(isLeader, known bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !known {
		m.leaderStatus = leaderStatusUnknown
		return
	}
	if isLeader {
		m.leaderStatus = 1
	} else {
		m.leaderStatus = 0
	}
}

// IncLeaderGatedLoop 记录一个 leader-only 后台循环本轮的执行结果。
// loop 与 result 都会收敛到固定集合，未知取值不产生新标签。
func (m *M) IncLeaderGatedLoop(loop, result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.leaderGatedLoops == nil {
		m.leaderGatedLoops = make(map[string]uint64)
	}
	m.leaderGatedLoops[sanitizeLeaderLoop(loop)+"|"+sanitizeLeaderResult(result)]++
}

func sanitizeLeaderLoop(v string) string {
	for _, s := range leaderGatedLoopNames {
		if v == s {
			return s
		}
	}
	return "unknown"
}

func sanitizeLeaderResult(v string) string {
	for _, s := range leaderGatedLoopResults {
		if v == s {
			return s
		}
	}
	return "skipped"
}

// appendLeaderMetrics 输出 leader 状态与循环执行计数。调用方已持锁。
func (m *M) appendLeaderMetrics(b []byte) []byte {
	b = append(b, "# HELP opsmesh_leader_status 当前实例角色（-1=选主结论未知；0=follower；1=leader）\n"...)
	b = append(b, "# TYPE opsmesh_leader_status gauge\n"...)
	b = append(b, fmt.Sprintf("opsmesh_leader_status %d\n", m.leaderStatus)...)

	b = append(b, "# HELP opsmesh_leader_gated_loop_total leader-only 后台循环本轮执行/跳过次数（skipped 持续增长而 leader_status=1 时，说明门控判据与选主结论脱节）\n"...)
	b = append(b, "# TYPE opsmesh_leader_gated_loop_total counter\n"...)
	for _, loop := range leaderGatedLoopNames {
		for _, result := range leaderGatedLoopResults {
			b = append(b, fmt.Sprintf("opsmesh_leader_gated_loop_total{loop=%q,result=%q} %d\n",
				loop, result, m.leaderGatedLoops[loop+"|"+result])...)
		}
	}
	return b
}
