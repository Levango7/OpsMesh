
// dag.go 提供 cron 任务编排依赖（DAG）能力：
//   - DAG 定义任务间依赖关系（有向无环图）；
//   - TopoSort 返回按依赖顺序执行的拓扑序列；
//   - DetectCycle 校验 DAG 是否含环（含环返回错误）；
//   - ReadyNodes 返回当前可执行节点（前置全部完成）。
//
// 与 schedule.go 协同：调度器在派生任务实例时按 DAG 拓扑顺序解锁后续任务，
// 实现任务编排依赖（M5 增强）。
package cron

import (
	"errors"
	"fmt"
	"sort"
)

// ErrDAGCycle DAG 含环（不可拓扑排序）。
var ErrDAGCycle = errors.New("cron/dag: cycle detected")

// ErrDAGNodeNotFound 节点不存在。
var ErrDAGNodeNotFound = errors.New("cron/dag: node not found")

// DAG 任务编排有向无环图。
//
// 节点为任务 ID，边表示"from 依赖 to"（to 完成后 from 才可执行）。
// 线程安全由调用方（如 Scheduler）保证；本类型仅提供纯算法。
type DAG struct {
	// nodes 所有节点 ID 集合（去重）。
	nodes map[string]struct{}
	// deps edges[from] = {to1, to2, ...}，from 依赖 to1/to2。
	edges map[string]map[string]struct{}
}

// NewDAG 构造空 DAG。
func NewDAG() *DAG {
	return &DAG{
		nodes: make(map[string]struct{}),
		edges: make(map[string]map[string]struct{}),
	}
}

// AddNode 添加节点（幂等）。
func (d *DAG) AddNode(id string) {
	d.nodes[id] = struct{}{}
	if d.edges[id] == nil {
		d.edges[id] = make(map[string]struct{})
	}
}

// AddEdge 添加依赖边：from 依赖 to（to 完成后 from 才可执行）。
// 自动添加两端节点。from==to 视为自环，返回 ErrDAGCycle。
func (d *DAG) AddEdge(from, to string) error {
	if from == to {
		return ErrDAGCycle
	}
	d.AddNode(from)
	d.AddNode(to)
	d.edges[from][to] = struct{}{}
	// 添加后立即校验是否引入环。
	if d.HasCycle() {
		// 回滚边并返回错误。
		delete(d.edges[from], to)
		return ErrDAGCycle
	}
	return nil
}

// Nodes 返回所有节点 ID（排序后便于稳定测试）。
func (d *DAG) Nodes() []string {
	out := make([]string, 0, len(d.nodes))
	for k := range d.nodes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Dependencies 返回 from 的直接依赖集合（排序后）。
func (d *DAG) Dependencies(from string) []string {
	m := d.edges[from]
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// HasCycle 检测 DAG 是否含环（DFS 三色标记法）。
func (d *DAG) HasCycle() bool {
	// 0=白（未访问），1=灰（在当前 DFS 路径），2=黑（已完成）。
	color := make(map[string]int, len(d.nodes))
	for n := range d.nodes {
		if color[n] == 0 {
			if dfsCycle(d, n, color) {
				return true
			}
		}
	}
	return false
}

// dfsCycle 从 u 出发 DFS，发现灰节点即环。
func dfsCycle(d *DAG, u string, color map[string]int) bool {
	color[u] = 1
	for v := range d.edges[u] {
		if color[v] == 1 {
			return true
		}
		if color[v] == 0 && dfsCycle(d, v, color) {
			return true
		}
	}
	color[u] = 2
	return false
}

// TopoSort 返回拓扑序列（依赖在前，被依赖在后）。
// 含环返回 ErrDAGCycle。多解时按节点 ID 升序保证稳定输出。
func (d *DAG) TopoSort() ([]string, error) {
	if d.HasCycle() {
		return nil, ErrDAGCycle
	}
	// 入度：indeg[x] = 依赖 x 的节点数 = x 在多少个 edges[*] 中出现。
	indeg := make(map[string]int, len(d.nodes))
	for n := range d.nodes {
		indeg[n] = 0
	}
	for _, deps := range d.edges {
		for to := range deps {
			indeg[to]++
		}
	}
	// 注：上面计算的是"被依赖数"，与拓扑排序所需相反。
	// 拓扑排序需要"入度=前置依赖数"=len(edges[node])。
	indeg = make(map[string]int, len(d.nodes))
	for n := range d.nodes {
		indeg[n] = len(d.edges[n])
	}
	// 入度为 0 的节点（无前置依赖）入队，按 ID 升序保证稳定。
	var ready []string
	for n, deg := range indeg {
		if deg == 0 {
			ready = append(ready, n)
		}
	}
	sort.Strings(ready)
	out := make([]string, 0, len(d.nodes))
	// 反向邻接表：rdeps[to] = {from1, from2, ...}，to 完成后解锁 from。
	rdeps := make(map[string][]string, len(d.nodes))
	for from, deps := range d.edges {
		for to := range deps {
			rdeps[to] = append(rdeps[to], from)
		}
	}
	for len(ready) > 0 {
		// 取最小 ID（稳定排序）。
		sort.Strings(ready)
		n := ready[0]
		ready = ready[1:]
		out = append(out, n)
		for _, from := range rdeps[n] {
			indeg[from]--
			if indeg[from] == 0 {
				ready = append(ready, from)
			}
		}
	}
	if len(out) != len(d.nodes) {
		return nil, fmt.Errorf("cron/dag: topo sort incomplete (got %d, want %d)", len(out), len(d.nodes))
	}
	return out, nil
}

// ReadyNodes 返回当前可执行节点：前置依赖全部在 done 集合中且自身不在 done。
// done 为已完成节点集合。返回结果按 ID 升序。
func (d *DAG) ReadyNodes(done map[string]struct{}) []string {
	out := make([]string, 0)
	for n := range d.nodes {
		if _, ok := done[n]; ok {
			continue
		}
		ready := true
		for dep := range d.edges[n] {
			if _, ok := done[dep]; !ok {
				ready = false
				break
			}
		}
		if ready {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Validate 校验 DAG 合法性：节点非空、无环。
func (d *DAG) Validate() error {
	if len(d.nodes) == 0 {
		return errors.New("cron/dag: empty DAG")
	}
	if d.HasCycle() {
		return ErrDAGCycle
	}
	return nil
}