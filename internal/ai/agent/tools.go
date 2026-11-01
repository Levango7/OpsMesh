// Package agent 工具调用框架
package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
)

// ErrToolNotWired 表示工具尚未接入真实后端。
//
// 默认工具集此前一律返回硬编码样例（hostname web-01、ip 192.168.1.10、
// task_id task-12345、metric 75.3、CVE-2024-XXXX），调用方拿到 200 与
// 「看起来像真的」的数据，LLM 还会把它当观测结果继续推理并写进结论——
// 比直接失败更危险。现在显式报未接线：HTTP 侧映射 501，/tools 标 implemented:false。
var ErrToolNotWired = errors.New("工具尚未接入后端")

// NotWiredTool 由未接线的工具实现，供 /tools 输出 implemented 判据
// （前端据此置灰，而不是摆出一列「能点但全是假数据」的工具）。
type NotWiredTool interface {
	Tool
	NotWired() bool
}

// IsToolWired 报告工具是否已接入后端；未实现 NotWiredTool 的按已接入处理。
func IsToolWired(t Tool) bool {
	nw, ok := t.(NotWiredTool)
	return !ok || !nw.NotWired()
}

// notWired 统一包装哨兵，保留「哪个工具没接」的可观测性。
func notWired(name string) error {
	return fmt.Errorf("%s: %w", name, ErrToolNotWired)
}

// NotWired 报告六个内置工具均未接入后端（契约保留，供 LLM 选择与前端置灰）。
func (t *QueryDeviceTool) NotWired() bool     { return true }
func (t *QueryMetricsTool) NotWired() bool    { return true }
func (t *ExecuteCommandTool) NotWired() bool  { return true }
func (t *SearchKnowledgeTool) NotWired() bool { return true }
func (t *WebSearchTool) NotWired() bool       { return true }
func (t *CreateTicketTool) NotWired() bool    { return true }

// Tool 工具接口
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	CheckPermission(agentCtx AgentContext) bool
	Execute(ctx context.Context, input map[string]interface{}, agentCtx AgentContext) (interface{}, error)
}

// ToolRegistry 工具注册表
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

func (tr *ToolRegistry) Register(tool Tool) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.tools[tool.Name()] = tool
}

func (tr *ToolRegistry) Get(name string) (Tool, bool) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	t, ok := tr.tools[name]
	return t, ok
}

func (tr *ToolRegistry) List() []Tool {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	out := make([]Tool, 0, len(tr.tools))
	for _, t := range tr.tools {
		out = append(out, t)
	}
	return out
}

func (tr *ToolRegistry) ToGatewayTools() []gateway.Tool {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	out := make([]gateway.Tool, 0, len(tr.tools))
	for _, t := range tr.tools {
		tool := gateway.Tool{}
		tool.Type = "function"
		tool.Function.Name = t.Name()
		tool.Function.Description = t.Description()
		tool.Function.Parameters = t.Parameters()
		out = append(out, tool)
	}
	return out
}

// ============================================================================
// 内置工具实现
// ============================================================================

// QueryDeviceTool 查询设备信息
type QueryDeviceTool struct{}

func (t *QueryDeviceTool) Name() string { return "query_device" }
func (t *QueryDeviceTool) Description() string {
	return "查询设备详细信息，包括 CPU/内存/磁盘/网络指标、状态、Agent 信息"
}
func (t *QueryDeviceTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"device_id": map[string]interface{}{
				"type":        "string",
				"description": "设备 ID",
			},
		},
		"required": []string{"device_id"},
	}
}
func (t *QueryDeviceTool) CheckPermission(agentCtx AgentContext) bool { return true }
func (t *QueryDeviceTool) Execute(ctx context.Context, input map[string]interface{}, agentCtx AgentContext) (interface{}, error) {
	return nil, notWired(t.Name())
}

// QueryMetricsTool 查询监控指标
type QueryMetricsTool struct{}

func (t *QueryMetricsTool) Name() string { return "query_metrics" }
func (t *QueryMetricsTool) Description() string {
	return "查询设备或集群的监控指标时序数据"
}
func (t *QueryMetricsTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"target": map[string]interface{}{
				"type":        "string",
				"description": "查询目标 (device_id 或 cluster_id)",
			},
			"metric": map[string]interface{}{
				"type":        "string",
				"description": "指标名称 (cpu/memory/disk/network)",
			},
			"range": map[string]interface{}{
				"type":        "string",
				"description": "时间范围 (15m/1h/2h/6h/24h)",
			},
		},
		"required": []string{"target", "metric"},
	}
}
func (t *QueryMetricsTool) CheckPermission(agentCtx AgentContext) bool { return true }
func (t *QueryMetricsTool) Execute(ctx context.Context, input map[string]interface{}, agentCtx AgentContext) (interface{}, error) {
	return nil, notWired(t.Name())
}

// ExecuteCommandTool 执行远程命令 (需要审批)
type ExecuteCommandTool struct{}

func (t *ExecuteCommandTool) Name() string { return "execute_command" }
func (t *ExecuteCommandTool) Description() string {
	return "在远程设备执行 shell 命令（高风险操作，需要审批）"
}
func (t *ExecuteCommandTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"device_id": map[string]interface{}{
				"type":        "string",
				"description": "目标设备 ID",
			},
			"command": map[string]interface{}{
				"type":        "string",
				"description": "要执行的命令",
			},
		},
		"required": []string{"device_id", "command"},
	}
}
func (t *ExecuteCommandTool) CheckPermission(agentCtx AgentContext) bool {
	// 需要 task:write 权限
	return true // 简化：实际应检查 agentCtx.Metadata["permissions"]
}
func (t *ExecuteCommandTool) Execute(ctx context.Context, input map[string]interface{}, agentCtx AgentContext) (interface{}, error) {
	return nil, notWired(t.Name())
}

// SearchKnowledgeTool 搜索知识库
type SearchKnowledgeTool struct{}

func (t *SearchKnowledgeTool) Name() string { return "search_knowledge" }
func (t *SearchKnowledgeTool) Description() string {
	return "搜索运维知识库（Runbook/告警历史/设备文档/变更记录）"
}
func (t *SearchKnowledgeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "搜索查询",
			},
			"top_k": map[string]interface{}{
				"type":        "integer",
				"description": "返回结果数量",
			},
		},
		"required": []string{"query"},
	}
}
func (t *SearchKnowledgeTool) CheckPermission(agentCtx AgentContext) bool { return true }
func (t *SearchKnowledgeTool) Execute(ctx context.Context, input map[string]interface{}, agentCtx AgentContext) (interface{}, error) {
	return nil, notWired(t.Name())
}

// WebSearchTool 联网搜索
type WebSearchTool struct{}

func (t *WebSearchTool) Name() string { return "web_search" }
func (t *WebSearchTool) Description() string {
	return "联网搜索最新信息（CVE/厂商公告/社区最佳实践）"
}
func (t *WebSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "搜索关键词",
			},
		},
		"required": []string{"query"},
	}
}
func (t *WebSearchTool) CheckPermission(agentCtx AgentContext) bool { return true }
func (t *WebSearchTool) Execute(ctx context.Context, input map[string]interface{}, agentCtx AgentContext) (interface{}, error) {
	return nil, notWired(t.Name())
}

// CreateTicketTool 创建工单
type CreateTicketTool struct{}

func (t *CreateTicketTool) Name() string { return "create_ticket" }
func (t *CreateTicketTool) Description() string {
	return "创建运维工单（故障/变更/请求）"
}
func (t *CreateTicketTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title": map[string]interface{}{
				"type":        "string",
				"description": "工单标题",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "工单描述",
			},
			"priority": map[string]interface{}{
				"type":        "string",
				"description": "优先级 (low/medium/high/critical)",
			},
		},
		"required": []string{"title", "description"},
	}
}
func (t *CreateTicketTool) CheckPermission(agentCtx AgentContext) bool { return true }
func (t *CreateTicketTool) Execute(ctx context.Context, input map[string]interface{}, agentCtx AgentContext) (interface{}, error) {
	return nil, notWired(t.Name())
}

// DefaultToolRegistry 创建默认工具注册表
func DefaultToolRegistry() *ToolRegistry {
	tr := NewToolRegistry()
	tr.Register(&QueryDeviceTool{})
	tr.Register(&QueryMetricsTool{})
	tr.Register(&ExecuteCommandTool{})
	tr.Register(&SearchKnowledgeTool{})
	tr.Register(&WebSearchTool{})
	tr.Register(&CreateTicketTool{})
	return tr
}

// containsString 是小集合的去重辅助（工具名列表本就几项）。
func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
