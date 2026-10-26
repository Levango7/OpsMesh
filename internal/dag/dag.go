// Package dag 提供 M5 作业编排的 DAG（有向无环图）引擎：
//   - 拓扑排序（Kahn 算法），检测环路；
//   - 依赖就绪判定：任务的前置依赖全部 done 时才可下发；
//   - 任务图校验：自依赖、缺失依赖、环路。
//
// 设计原则：内核只做"依赖就绪判定 + 拓扑顺序"，不下发执行（下发由 store/registry 负责）。
// 任务的 DependsOn 字段已在 proto.Task 预留（MVP 仅记录，M5 起真正生效）。
package dag

import (
	"fmt"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// indexByID 将任务列表按 TaskID 建索引。
func indexByID(tasks []*proto.Task) map[string]*proto.Task {
	m := make(map[string]*proto.Task, len(tasks))
	for _, t := range tasks {
		m[t.TaskID] = t
	}
	return m
}

// AllDepsDone 判定任务 t 的全部前置依赖是否都已 done。
// deps 缺失（不在图中）视为未就绪（保守：不阻塞整体，但依赖本身不成立）。
//
// 保留给既有调用方使用；新代码在"依赖是否已可判定终局"的场景应改用 AllDepsTerminal /
// AnyDepFailed —— AllDepsDone 要求每个依赖都恰好 done，上游 failed/cancelled 时永远为 false，
// 使依赖它的 blocked 任务既不能被释放、也不会失败，永久停留在 blocked。
func AllDepsDone(t *proto.Task, byID map[string]*proto.Task) bool {
	for _, dep := range t.DependsOn {
		d, ok := byID[dep]
		if !ok {
			return false // 依赖任务不存在
		}
		if d.Status != "done" {
			return false
		}
	}
	return true
}

// isTerminalStatus 判定任务状态是否为终态（不再变化）。
// 终态集合：done / failed / cancelled / rejected / dead_letter。
func isTerminalStatus(status string) bool {
	switch status {
	case "done", "failed", "cancelled", "rejected", "dead_letter":
		return true
	}
	return false
}

// AllDepsTerminal 判定任务 t 的全部前置依赖是否都已达终态。
// 语义比 AllDepsDone 宽：只要每个依赖都走到了"不再变化"的状态（含 failed/cancelled）即为 true。
// deps 缺失（不在图中）仍视为未就绪——与 AllDepsDone 的保守口径一致，缺依赖不能凭空判定终局。
//
// 用途：依赖释放判定。上游失败/取消时下游必须被显式解决（失败并进死信），
// 而不是永远 blocked——它既不可领取，也不出现在死信视图，没有任何看护路径能发现它。
func AllDepsTerminal(t *proto.Task, byID map[string]*proto.Task) bool {
	for _, dep := range t.DependsOn {
		d, ok := byID[dep]
		if !ok {
			return false // 依赖任务不存在
		}
		if !isTerminalStatus(d.Status) {
			return false
		}
	}
	return true
}

// AnyDepFailed 判定任务 t 的前置依赖中是否存在"非成功"的终态依赖，返回其 ID。
// 非成功终态指 failed / cancelled / rejected / dead_letter（即上游没走到 done）。
// 全部依赖均为 done（或无依赖）时返回 ("", false)。
//
// 用途：下游的处置分流——任一上游失败/取消 ⇒ 下游不可能再执行，应置 failed 并进死信；
// 全部 done ⇒ 下游照常释放为 pending 进入可下发队列。
func AnyDepFailed(t *proto.Task, byID map[string]*proto.Task) (string, bool) {
	for _, dep := range t.DependsOn {
		d, ok := byID[dep]
		if !ok {
			continue // 缺失依赖不判定为失败（由 AllDepsTerminal 卡住就绪判定）
		}
		if isTerminalStatus(d.Status) && d.Status != "done" {
			return d.TaskID, true
		}
	}
	return "", false
}

// ReadyIDs 返回当前可下发（依赖全部 done）的任务 ID 列表（不修改入参）。
// 仅当 t.DependsOn 非空且全部 done 时该任务才就绪；无依赖任务始终就绪。
func ReadyIDs(tasks []*proto.Task) []string {
	byID := indexByID(tasks)
	var out []string
	for _, t := range tasks {
		if len(t.DependsOn) == 0 {
			out = append(out, t.TaskID)
			continue
		}
		if AllDepsDone(t, byID) {
			out = append(out, t.TaskID)
		}
	}
	return out
}

// TopoOrder 对任务图做拓扑排序，返回有序 TaskID 列表。
// 存在环路时返回 error（附带参与环路的节点）。
func TopoOrder(tasks []*proto.Task) ([]string, error) {
	byID := indexByID(tasks)
	indeg := make(map[string]int, len(tasks))
	for _, t := range tasks {
		indeg[t.TaskID] = 0
	}
	// 统计入度（仅统计图中存在的依赖）
	for _, t := range tasks {
		seen := make(map[string]struct{})
		for _, dep := range t.DependsOn {
			if _, ok := byID[dep]; !ok {
				continue // 缺失依赖不计入度（由 Validate 报告）
			}
			if _, dup := seen[dep]; dup {
				continue
			}
			seen[dep] = struct{}{}
			indeg[t.TaskID]++
		}
	}
	var queue []string
	for id, d := range indeg {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	var order []string
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		order = append(order, cur)
		for _, t := range tasks {
			contains := false
			for _, dep := range t.DependsOn {
				if dep == cur {
					contains = true
					break
				}
			}
			if !contains {
				continue
			}
			indeg[t.TaskID]--
			if indeg[t.TaskID] == 0 {
				queue = append(queue, t.TaskID)
			}
		}
	}
	if len(order) != len(tasks) {
		// 找出未进入 order 的节点（环路成员）
		inOrder := make(map[string]struct{}, len(order))
		for _, id := range order {
			inOrder[id] = struct{}{}
		}
		var cycle []string
		for _, t := range tasks {
			if _, ok := inOrder[t.TaskID]; !ok {
				cycle = append(cycle, t.TaskID)
			}
		}
		return nil, fmt.Errorf("dag: 检测到环路，涉及任务 %v", cycle)
	}
	return order, nil
}

// Validate 校验任务图合法性：自依赖、缺失依赖、环路。
// 返回首个遇到的错误（不阻断后续，但调用方应据此拒绝非法编排）。
func Validate(tasks []*proto.Task) error {
	byID := indexByID(tasks)
	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if dep == t.TaskID {
				return fmt.Errorf("dag: 任务 %s 存在自依赖", t.TaskID)
			}
			if _, ok := byID[dep]; !ok {
				return fmt.Errorf("dag: 任务 %s 依赖缺失的任务 %s", t.TaskID, dep)
			}
		}
	}
	if _, err := TopoOrder(tasks); err != nil {
		return err
	}
	return nil
}
