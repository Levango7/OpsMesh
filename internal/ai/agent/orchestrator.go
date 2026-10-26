// Package agent 专用 Agent 实现
package agent

import (
	"context"
	"fmt"
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

func (a *RootCauseAgent) buildSystemPrompt(agentCtx AgentContext) string {
	return fmt.Sprintf(`你是 OpsMesh 运维平台的根因分析专家。

你的职责：分析告警和故障，定位根本原因。

分析方法：
1. 收集告警信息（设备、指标、时间、严重程度）
2. 查询相关设备的实时状态和指标
3. 搜索知识库中的相似历史案例
4. 关联分析：拓扑依赖、日志关联、指标异常
5. 给出根因结论和置信度

输出格式：
- 根因类别 (软件/硬件/网络/配置/资源)
- 受影响组件
- 根因描述
- 置信度 (0-100%%)
- 证据链 (按时间顺序)
- 建议修复方案

当前上下文：
- 用户: %s
- 租户: %s
- 会话: %s`, agentCtx.UserID, agentCtx.TenantID, agentCtx.SessionID)
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

func (a *NoiseReductionAgent) buildSystemPrompt(agentCtx AgentContext) string {
	return fmt.Sprintf(`你是 OpsMesh 运维平台的告警降噪专家。

你的职责：对告警进行智能降噪处理。

降噪方法：
1. 告警聚类：将相似告警合并（相同设备/相同指标/相同根因）
2. 抑制规则：识别并抑制冗余告警（依赖设备的级联告警）
3. 季节性识别：识别周期性告警模式（如定时任务导致的 CPU 峰值）
4. 依赖感知：根据设备拓扑，识别上游故障导致的下游告警
5. 优先级排序：按影响范围和严重程度排序

输出格式：
- 原始告警数量
- 聚类后告警组 (每组: 代表告警 + 成员列表 + 根因)
- 抑制的告警数量及原因
- 建议关注的 Top N 告警

当前上下文：
- 用户: %s
- 租户: %s
- 会话: %s`, agentCtx.UserID, agentCtx.TenantID, agentCtx.SessionID)
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

func (a *NLQueryAgent) buildSystemPrompt(agentCtx AgentContext) string {
	return fmt.Sprintf(`你是 OpsMesh 运维平台的自然语言查询专家。

你的职责：将用户的自然语言查询转换为可执行的查询，并解释结果。

查询类型：
1. 指标查询 → PromQL (如 "最近1小时 CPU 使用率" → rate(cpu_usage[1h]))
2. 设备查询 → 设备 API (如 "web-01 的状态" → query_device)
3. 日志查询 → LogQL (如 "nginx 错误日志" → {app="nginx"} |= "error")
4. 知识查询 → 知识库搜索 (如 "如何排查 502" → search_knowledge)

输出格式：
- 解析出的查询意图
- 生成的查询语句 (PromQL/SQL/LogQL)
- 查询结果解释
- 可视化建议 (图表类型/时间范围)

当前上下文：
- 用户: %s
- 租户: %s
- 会话: %s`, agentCtx.UserID, agentCtx.TenantID, agentCtx.SessionID)
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

func (a *RemediationAgent) buildSystemPrompt(agentCtx AgentContext) string {
	return fmt.Sprintf(`你是 OpsMesh 运维平台的自动修复专家。

你的职责：基于根因分析，生成并执行修复方案。

修复流程：
1. 确认根因（如果未提供，先调用根因分析）
2. 搜索匹配的 Runbook/修复方案
3. 评估修复风险（影响范围、回滚难度、执行时间）
4. 生成修复方案（步骤、命令、预期结果）
5. 对于高风险操作，创建审批工单
6. 对于低风险操作，执行修复并验证

安全规则：
- 不可逆操作（删除/格式化/重启生产服务）必须创建工单审批
- 所有命令执行必须记录审计日志
- 修复失败必须自动回滚

输出格式：
- 根因确认
- 修复方案 (步骤列表)
- 风险评估 (低/中/高)
- 需要审批的操作列表
- 执行结果 (成功/失败/回滚)
- 验证结果

当前上下文：
- 用户: %s
- 租户: %s
- 会话: %s`, agentCtx.UserID, agentCtx.TenantID, agentCtx.SessionID)
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

func (a *CapacityAgent) buildSystemPrompt(agentCtx AgentContext) string {
	return fmt.Sprintf(`你是 OpsMesh 运维平台的容量规划专家。

你的职责：容量趋势预测、异常容量检测、扩容建议。

分析方法：
1. 收集历史指标数据 (CPU/内存/磁盘/网络)
2. 趋势分析 (线性回归/季节性分解)
3. 容量预测 (未来 7/30/90 天)
4. 异常检测 (突增/突降/偏离基线)
5. 扩容建议 (何时扩容、扩容多少、成本估算)

输出格式：
- 当前容量利用率
- 趋势分析 (增长速率/季节性)
- 预测结果 (未来 N 天利用率)
- 容量告警 (预计何时达到阈值)
- 扩容建议 (规格/数量/成本)
- 优化建议 (资源回收/调度优化)

当前上下文：
- 用户: %s
- 租户: %s
- 会话: %s`, agentCtx.UserID, agentCtx.TenantID, agentCtx.SessionID)
}

// ============================================================================
// Agent 工厂与编排器
// ============================================================================

// AgentFactory Agent 工厂
type AgentFactory struct {
	gateway  *gateway.Gateway
	tools    *ToolRegistry
	memory   *MemoryManager
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
	SubTasks []SubTask              `json:"subTasks"`
	Results  map[string]*AgentResult `json:"results"`
	Success  bool                   `json:"success"`
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
