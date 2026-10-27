// Package agent 专用 Agent 实现
package agent

import (
	"context"
	"sync"
	"time"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
)

// ============================================================================
// 根因分析 Agent
// ============================================================================

type RootCauseAgent struct {
	*BaseAgent
}

func NewRootCauseAgent(gw *gateway.Gateway, tools *ToolRegistry, mem *MemoryManager) *RootCauseAgent {
	cfg := AgentConfig{
		Type:        AgentTypeRootCause,
		Name:        "根因分析 Agent",
		Description: "分析告警和故障，定位根因。支持拓扑关联、日志/指标关联、历史相似案例匹配。",
		Model:       "gpt-4o",
		MaxSteps:    8,
		Tools:       []string{"query_device", "query_metrics", "search_knowledge", "web_search"},
		Temperature: 0.2,
	}
	return &RootCauseAgent{BaseAgent: NewBaseAgent(cfg, gw, tools, mem)}
}

// ============================================================================
// 智能降噪 Agent
// ============================================================================

type NoiseReductionAgent struct {
	*BaseAgent
}

func NewNoiseReductionAgent(gw *gateway.Gateway, tools *ToolRegistry, mem *MemoryManager) *NoiseReductionAgent {
	cfg := AgentConfig{
		Type:        AgentTypeNoise,
		Name:        "智能降噪 Agent",
		Description: "告警聚类、抑制、季节性识别、依赖感知降噪。",
		Model:       "gpt-4o-mini",
		MaxSteps:    5,
		Tools:       []string{"query_device", "query_metrics", "search_knowledge"},
		Temperature: 0.1,
	}
	return &NoiseReductionAgent{BaseAgent: NewBaseAgent(cfg, gw, tools, mem)}
}

// ============================================================================
// 自然语言查询 Agent
// ============================================================================

type NLQueryAgent struct {
	*BaseAgent
}

func NewNLQueryAgent(gw *gateway.Gateway, tools *ToolRegistry, mem *MemoryManager) *NLQueryAgent {
	cfg := AgentConfig{
		Type:        AgentTypeNLQuery,
		Name:        "自然语言查询 Agent",
		Description: "将自然语言转换为 PromQL/SQL 查询，执行并解释结果。",
		Model:       "gpt-4o-mini",
		MaxSteps:    6,
		Tools:       []string{"query_metrics", "query_device", "search_knowledge"},
		Temperature: 0.1,
	}
	return &NLQueryAgent{BaseAgent: NewBaseAgent(cfg, gw, tools, mem)}
}

// ============================================================================
// 自动修复 Agent
// ============================================================================

type RemediationAgent struct {
	*BaseAgent
}

func NewRemediationAgent(gw *gateway.Gateway, tools *ToolRegistry, mem *MemoryManager) *RemediationAgent {
	cfg := AgentConfig{
		Type:        AgentTypeRemediation,
		Name:        "自动修复 Agent",
		Description: "基于根因分析结果，匹配 Runbook，生成修复方案，执行修复（需审批）。",
		Model:       "gpt-4o",
		MaxSteps:    10,
		Tools:       []string{"query_device", "query_metrics", "search_knowledge", "execute_command", "create_ticket", "web_search"},
		Temperature: 0.2,
	}
	return &RemediationAgent{BaseAgent: NewBaseAgent(cfg, gw, tools, mem)}
}

// ============================================================================
// 容量规划 Agent
// ============================================================================

type CapacityAgent struct {
	*BaseAgent
}

func NewCapacityAgent(gw *gateway.Gateway, tools *ToolRegistry, mem *MemoryManager) *CapacityAgent {
	cfg := AgentConfig{
		Type:        AgentTypeCapacity,
		Name:        "容量规划 Agent",
		Description: "容量趋势预测、异常容量检测、扩容建议。",
		Model:       "gpt-4o-mini",
		MaxSteps:    6,
		Tools:       []string{"query_metrics", "query_device", "search_knowledge"},
		Temperature: 0.2,
	}
	return &CapacityAgent{BaseAgent: NewBaseAgent(cfg, gw, tools, mem)}
}

// ============================================================================
// Agent 工厂与编排器
// ============================================================================

// AgentFactory Agent 工厂
type AgentFactory struct {
	gateway    *gateway.Gateway
	tools      *ToolRegistry
	memory     *MemoryManager
	classifier *IntentClassifier
}

func NewAgentFactory(gw *gateway.Gateway, tools *ToolRegistry, mem *MemoryManager) *AgentFactory {
	return &AgentFactory{
		gateway:    gw,
		tools:      tools,
		memory:     mem,
		classifier: NewIntentClassifier(gw),
	}
}

// CreateAgent 根据类型创建 Agent
func (f *AgentFactory) CreateAgent(agentType AgentType) Agent {
	switch agentType {
	case AgentTypeRootCause:
		return NewRootCauseAgent(f.gateway, f.tools, f.memory)
	case AgentTypeNoise:
		return NewNoiseReductionAgent(f.gateway, f.tools, f.memory)
	case AgentTypeNLQuery:
		return NewNLQueryAgent(f.gateway, f.tools, f.memory)
	case AgentTypeRemediation:
		return NewRemediationAgent(f.gateway, f.tools, f.memory)
	case AgentTypeCapacity:
		return NewCapacityAgent(f.gateway, f.tools, f.memory)
	default:
		return NewGeneralAgent(f.gateway, f.tools, f.memory)
	}
}

// GeneralAgent 通用 Agent
type GeneralAgent struct {
	*BaseAgent
}

func NewGeneralAgent(gw *gateway.Gateway, tools *ToolRegistry, mem *MemoryManager) *GeneralAgent {
	cfg := AgentConfig{
		Type:        AgentTypeGeneral,
		Name:        "通用运维 Agent",
		Description: "处理通用运维任务，包括设备管理、任务执行、配置查询等。",
		Model:       "gpt-4o-mini",
		MaxSteps:    5,
		Tools:       []string{"query_device", "query_metrics", "search_knowledge", "create_ticket"},
		Temperature: 0.3,
	}
	return &GeneralAgent{BaseAgent: NewBaseAgent(cfg, gw, tools, mem)}
}

// ============================================================================
// Agent Orchestrator 编排器
// ============================================================================

// Orchestrator Agent 编排器
type Orchestrator struct {
	factory    *AgentFactory
	classifier *IntentClassifier
	memory     *MemoryManager
}

func NewOrchestrator(gw *gateway.Gateway, tools *ToolRegistry, mem *MemoryManager) *Orchestrator {
	return &Orchestrator{
		factory:    NewAgentFactory(gw, tools, mem),
		classifier: NewIntentClassifier(gw),
		memory:     mem,
	}
}

// Execute 执行用户请求（自动分类 + 路由到对应 Agent）
func (o *Orchestrator) Execute(ctx context.Context, input string, agentCtx AgentContext) (*AgentResult, error) {
	// 1. 意图分类
	agentType, confidence, err := o.classifier.Classify(ctx, input)
	if err != nil {
		agentType = AgentTypeGeneral
		confidence = 0.5
	}

	// 2. 创建 Agent
	agent := o.factory.CreateAgent(agentType)

	// 3. 执行
	result, err := agent.Run(ctx, input, agentCtx)
	if result != nil {
		result.Metadata["intentConfidence"] = confidence
		result.Metadata["agentType"] = string(agentType)
	}

	return result, err
}

// ExecuteDAG 执行复杂任务 DAG (多 Agent 协作)
func (o *Orchestrator) ExecuteDAG(ctx context.Context, input string, agentCtx AgentContext) (*DAGResult, error) {
	// 1. 拆解任务
	subTasks, err := o.classifier.DecomposePrompt(ctx, input)
	if err != nil {
		return nil, err
	}

	// 2. 构建 DAG
	dag := NewTaskDAG(subTasks)

	// 3. 拓扑排序 + 并行执行
	results := make(map[string]*AgentResult)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, task := range dag.TopologicalOrder() {
		// 等待依赖完成
		for _, dep := range task.Dependencies {
			for {
				mu.Lock()
				_, done := results[dep]
				mu.Unlock()
				if done {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}

		wg.Add(1)
		go func(t SubTask) {
			defer wg.Done()
			agent := o.factory.CreateAgent(t.AgentType)
			result, err := agent.Run(ctx, t.Description, agentCtx)
			if err != nil {
				result = &AgentResult{AgentType: t.AgentType, Success: false, Error: err.Error()}
			}
			mu.Lock()
			results[t.ID] = result
			mu.Unlock()
		}(task)
	}
	wg.Wait()

	// 4. 汇总结果
	return &DAGResult{
		SubTasks: subTasks,
		Results:  results,
		Success:  allSuccess(results),
	}, nil
}

// DAGResult DAG 执行结果
type DAGResult struct {
	SubTasks []SubTask               `json:"subTasks"`
	Results  map[string]*AgentResult `json:"results"`
	Success  bool                    `json:"success"`
}

// TaskDAG 任务有向无环图
type TaskDAG struct {
	tasks []SubTask
}

func NewTaskDAG(tasks []SubTask) *TaskDAG {
	return &TaskDAG{tasks: tasks}
}

func (d *TaskDAG) TopologicalOrder() []SubTask {
	// 简化：按优先级降序排序 (实际应做拓扑排序)
	sorted := make([]SubTask, len(d.tasks))
	copy(sorted, d.tasks)
	// 按优先级降序
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Priority > sorted[i].Priority {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	return sorted
}

func allSuccess(results map[string]*AgentResult) bool {
	for _, r := range results {
		if !r.Success {
			return false
		}
	}
	return true
}
