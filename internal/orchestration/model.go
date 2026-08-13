// Package orchestration 实现 M5 作业编排中心：可视化 DAG 工作流 + 定时调度。
// 设计原则：本包只做「DAG 定义 / 校验 / 展开为底层任务 / 运行态归组」，
// 不下发执行——展开后的节点任务交给 store.CreateTask（复用 proto.Task.DependsOn
// 与 per-agent releaseDeps 引擎驱动依赖就绪），避免反向依赖 controlplane/Registry。
package orchestration

import (
	"encoding/json"
	"fmt"
	"time"
)

// 工作流状态机：
//
//	draft  → active（可定时调度）/ paused（暂停调度）
//	active → running（某次运行进行中）→ success / failed（最近一次运行终态，回到 active 等待下次）
const (
	StatusDraft   = "draft"
	StatusActive  = "active"
	StatusPaused  = "paused"
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusFailed  = "failed"
)

// 节点类型常量（Type 字段取值，透传给底层任务或在本层处理）。
const (
	NodeShell     = "shell"     // shell 命令
	NodeFile      = "file"      // 文件分发
	NodeService   = "service"   // 服务管理
	NodeWorkflow  = "workflow"  // 子工作流引用
	NodeCondition = "condition" // 条件分支
)

// validNodeTypes 是允许的节点类型集合（供 ValidateNodes 校验）。
var validNodeTypes = map[string]struct{}{
	NodeShell:     {},
	NodeFile:      {},
	NodeService:   {},
	NodeWorkflow:  {},
	NodeCondition: {},
}

// WorkflowNode 是 DAG 中的一个节点（对应一次底层任务下发）。
type WorkflowNode struct {
	ID        string   `json:"id"`        // 节点 ID（同工作流内唯一）
	Name      string   `json:"name"`      // 展示名（可选）
	Type      string   `json:"type"`      // shell / file / service / workflow / condition
	Command   string   `json:"command"`   // shell: 命令；file: 内容；service: 动作
	Path      string   `json:"path"`      // file 类型：目标路径；service 类型：服务名（可选）
	DependsOn []string `json:"dependsOn"` // 前置节点 ID（DAG 边）

	// 节点级超时（秒，0=不超时）。下发到底层任务后由执行器强制终止。
	Timeout int `json:"timeout,omitempty"`
	// 重试次数（0=不重试）。失败后按 RetryDelay 间隔重试，达次数后置失败。
	RetryCount int `json:"retryCount,omitempty"`
	// 重试延迟（秒）。两次重试之间的等待间隔。
	RetryDelay int `json:"retryDelay,omitempty"`

	// 条件分支表达式（Type="condition" 时使用）。
	// 语法：${nodeID.status} == "success" 或 ${nodeID.exitCode} == 0
	// 支持 && 和 || 组合。
	Condition string `json:"condition,omitempty"`
	// 条件为 true 时执行的节点 ID 列表。
	ThenNodes []string `json:"thenNodes,omitempty"`
	// 条件为 false 时执行的节点 ID 列表。
	ElseNodes []string `json:"elseNodes,omitempty"`

	// 子工作流 ID（Type="workflow" 时使用）。引用另一 WorkflowDef.ID 作为子流程展开执行。
	SubWorkflowID int64 `json:"subWorkflowID,omitempty"`
}

// WorkflowDef 是 M5 作业编排的工作流定义（可视化 DAG + 定时）。
type WorkflowDef struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	AgentID       string    `json:"agentID"` // 目标执行 agent（MVP：单 agent 承载整图，复用 per-agent DAG 引擎）
	TenantID      string    `json:"tenantID"`
	DAG           string    `json:"dag"`  // JSON []WorkflowNode
	Cron          string    `json:"cron"` // 5 字段 cron 表达式（空=不定时）
	Status        string    `json:"status"`
	LastRunAt     time.Time `json:"lastRunAt"`
	LastRunStatus string    `json:"lastRunStatus"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Nodes 解析 DAG JSON 为节点切片（供校验与展开）。
func (w *WorkflowDef) Nodes() ([]WorkflowNode, error) {
	if w.DAG == "" {
		return nil, nil
	}
	var ns []WorkflowNode
	if err := json.Unmarshal([]byte(w.DAG), &ns); err != nil {
		return nil, fmt.Errorf("workflow dag JSON 非法: %w", err)
	}
	return ns, nil
}

// Valid 基础校验：必须指定名称与目标 agent。
func (w *WorkflowDef) Valid() bool {
	return w.Name != "" && w.AgentID != ""
}

// ValidateNodes 校验 DAG 节点合法性（节点类型 / 子工作流引用 / 条件表达式 /
// 超时与重试参数 / 节点 ID 唯一 / DependsOn 引用存在）。
// 不重复 dag.Validate 的环/自依赖检测（handler.validateDAG 已覆盖），
// 本方法聚焦节点级语义校验，与 dag.Validate 互补。
// 返回首个遇到的错误，nil 表示通过。
func (w *WorkflowDef) ValidateNodes() error {
	ns, err := w.Nodes()
	if err != nil {
		return err
	}
	return validateNodes(ns)
}

// validateNodes 是 ValidateNodes 的纯函数实现（便于单测直接构造节点切片）。
func validateNodes(ns []WorkflowNode) error {
	// 节点 ID 唯一性 + 建索引（供 DependsOn 引用存在性校验）。
	seen := make(map[string]struct{}, len(ns))
	for i := range ns {
		n := ns[i]
		if n.ID == "" {
			return fmt.Errorf("workflow node[%d] id 为空", i)
		}
		if _, dup := seen[n.ID]; dup {
			return fmt.Errorf("workflow node id 重复: %s", n.ID)
		}
		seen[n.ID] = struct{}{}

		// 节点类型必须有效。
		if _, ok := validNodeTypes[n.Type]; !ok {
			return fmt.Errorf("workflow node %s: 非法类型 %q", n.ID, n.Type)
		}

		// 子工作流引用：Type="workflow" 时 SubWorkflowID 必须 > 0。
		if n.Type == NodeWorkflow && n.SubWorkflowID <= 0 {
			return fmt.Errorf("workflow node %s: type=workflow 必须指定 subWorkflowID>0", n.ID)
		}

		// 条件分支：Type="condition" 时 Condition 不能为空。
		if n.Type == NodeCondition && n.Condition == "" {
			return fmt.Errorf("workflow node %s: type=condition 必须指定 condition 表达式", n.ID)
		}

		// 超时与重试参数非负。
		if n.Timeout < 0 {
			return fmt.Errorf("workflow node %s: timeout=%d 不能为负", n.ID, n.Timeout)
		}
		if n.RetryCount < 0 {
			return fmt.Errorf("workflow node %s: retryCount=%d 不能为负", n.ID, n.RetryCount)
		}
		if n.RetryDelay < 0 {
			return fmt.Errorf("workflow node %s: retryDelay=%d 不能为负", n.ID, n.RetryDelay)
		}
	}

	// DependsOn 引用的节点必须存在（不在图中视为悬空引用）。
	for i := range ns {
		n := ns[i]
		for _, dep := range n.DependsOn {
			if _, ok := seen[dep]; !ok {
				return fmt.Errorf("workflow node %s: dependsOn 引用不存在的节点 %s", n.ID, dep)
			}
		}
	}
	return nil
}

// WorkflowRun 记录工作流一次运行的节点状态快照（执行历史与回放）。
// 由 Trigger 创建（Status=running），由 Reconcile 在终态时更新 Status 与 FinishedAt。
// NodeStates 从 TasksByParent 收集：TaskID 去掉 prefix（"wf-<id>-"）得到 nodeID，value 为节点任务状态。
type WorkflowRun struct {
	ID         int64             `json:"id"`
	WorkflowID int64             `json:"workflowID"`
	TenantID   string            `json:"tenantID"`
	StartedAt  time.Time         `json:"startedAt"`
	FinishedAt time.Time         `json:"finishedAt"`
	Status     string            `json:"status"`      // running / success / failed
	NodeStates map[string]string `json:"nodeStates"`  // nodeID -> status (pending/running/done/failed)
}
