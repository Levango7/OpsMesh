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
//   draft  → active（可定时调度）/ paused（暂停调度）
//   active → running（某次运行进行中）→ success / failed（最近一次运行终态，回到 active 等待下次）
const (
	StatusDraft    = "draft"
	StatusActive   = "active"
	StatusPaused   = "paused"
	StatusRunning  = "running"
	StatusSuccess  = "success"
	StatusFailed   = "failed"
)

// WorkflowNode 是 DAG 中的一个节点（对应一次底层任务下发）。
type WorkflowNode struct {
	ID        string   `json:"id"`        // 节点 ID（同工作流内唯一）
	Name      string   `json:"name"`      // 展示名（可选）
	Type      string   `json:"type"`      // shell / file / service（透传给底层任务）
	Command   string   `json:"command"`   // shell: 命令；file: 内容；service: 动作
	Path      string   `json:"path"`      // file 类型：目标路径；service 类型：服务名（可选）
	DependsOn []string `json:"dependsOn"` // 前置节点 ID（DAG 边）
}

// WorkflowDef 是 M5 作业编排的工作流定义（可视化 DAG + 定时）。
type WorkflowDef struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	AgentID      string    `json:"agentID"`     // 目标执行 agent（MVP：单 agent 承载整图，复用 per-agent DAG 引擎）
	TenantID     string    `json:"tenantID"`
	DAG          string    `json:"dag"`         // JSON []WorkflowNode
	Cron         string    `json:"cron"`        // 5 字段 cron 表达式（空=不定时）
	Status       string    `json:"status"`
	LastRunAt    time.Time `json:"lastRunAt"`
	LastRunStatus string    `json:"lastRunStatus"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
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
