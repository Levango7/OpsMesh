// Package agent 工具调用框架
package agent

import (
	"context"
	"sync"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
)

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
	deviceID, _ := input["device_id"].(string)
	// 实际调用 device-svc API
	return map[string]interface{}{
		"device_id": deviceID,
		"hostname":  "web-01",
		"ip":        "192.168.1.10",
		"status":    "online",
		"cpu":       "45%",
		"memory":    "62%",
		"disk":      "78%",
	}, nil
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
	target, _ := input["target"].(string)
	metric, _ := input["metric"].(string)
	return map[string]interface{}{
		"target": target,
		"metric": metric,
		"value":  "75.3",
		"unit":   "%",
		"trend":  "increasing",
	}, nil
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
	deviceID, _ := input["device_id"].(string)
	command, _ := input["command"].(string)
	// 实际调用 task-svc 创建任务
	return map[string]interface{}{
		"task_id":   "task-12345",
		"device_id": deviceID,
		"command":   command,
		"status":    "pending_approval",
		"message":   "命令已提交，等待审批",
	}, nil
}

// SearchKnowledgeTool 搜索知识库
type SearchKnowledgeTool struct {
	retriever interface {
		Retrieve(ctx context.Context, query string, filter map[string]interface{}) (interface{}, error)
	}
}

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
	query, _ := input["query"].(string)
	return map[string]interface{}{
		"results": []map[string]interface{}{
			{"title": "Nginx 502 排查 Runbook", "content": "1. 检查 upstream 配置... 2. 检查后端服务状态..."},
			{"title": "CPU 高负载处理指南", "content": "1. top 查看进程... 2. 检查 cron 任务..."},
		},
		"query": query,
	}, nil
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
	query, _ := input["query"].(string)
	return map[string]interface{}{
		"results": []map[string]interface{}{
			{"title": "CVE-2024-XXXX: Nginx 漏洞公告", "snippet": "影响版本 1.24.x..."},
		},
		"query": query,
	}, nil
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
	title, _ := input["title"].(string)
	return map[string]interface{}{
		"ticket_id": "TKT-2024-001",
		"title":     title,
		"status":    "open",
	}, nil
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
