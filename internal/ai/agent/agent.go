// Package agent 提供 AI Agent 编排引擎：ReAct 执行器、工具调用框架、专用 Agent、记忆系统、意图分类。
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
)

// AgentType Agent 类型
type AgentType string

const (
	AgentTypeRootCause    AgentType = "root_cause"    // 根因分析
	AgentTypeNoise        AgentType = "noise"          // 智能降噪
	AgentTypeNLQuery      AgentType = "nl_query"       // 自然语言查询
	AgentTypeRemediation  AgentType = "remediation"    // 自动修复
	AgentTypeCapacity     AgentType = "capacity"       // 容量规划
	AgentTypeGeneral      AgentType = "general"        // 通用 Agent
)

// AgentConfig Agent 配置
type AgentConfig struct {
	Type        AgentType `json:"type"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Model       string    `json:"model"`       // 使用的 LLM 模型
	MaxSteps    int       `json:"maxSteps"`    // 最大 ReAct 步数
	Tools       []string  `json:"tools"`       // 可用工具列表
	Temperature float64   `json:"temperature"`
}

// AgentContext Agent 执行上下文
type AgentContext struct {
	UserID    string                 `json:"userId"`
	TenantID  string                 `json:"tenantId"`
	SessionID string                 `json:"sessionId"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// AgentResult Agent 执行结果
type AgentResult struct {
	AgentType   AgentType              `json:"agentType"`
	Success     bool                   `json:"success"`
	Output      string                 `json:"output"`
	Steps       []ReActStep            `json:"steps"`
	ToolCalls   []ToolCallRecord       `json:"toolCalls"`
	Duration    time.Duration          `json:"duration"`
	TokenUsage  int                    `json:"tokenUsage"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Error       string                 `json:"error,omitempty"`
}

// ReActStep ReAct 单步
type ReActStep struct {
	Step        int    `json:"step"`
	Thought     string `json:"thought"`
	Action      string `json:"action"`
	ActionInput string `json:"actionInput"`
	Observation string `json:"observation"`
}

// ToolCallRecord 工具调用记录
type ToolCallRecord struct {
	ToolName   string                 `json:"toolName"`
	Input      map[string]interface{} `json:"input"`
	Output     interface{}            `json:"output"`
	Duration   time.Duration          `json:"duration"`
	Error      string                 `json:"error,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
}

// Agent Agent 接口
type Agent interface {
	Type() AgentType
	Config() AgentConfig
	Run(ctx context.Context, input string, agentCtx AgentContext) (*AgentResult, error)
}

// BaseAgent 基础 Agent 实现
type BaseAgent struct {
	config   AgentConfig
	gateway  *gateway.Gateway
	tools    *ToolRegistry
	memory   *MemoryManager
}

func NewBaseAgent(cfg AgentConfig, gw *gateway.Gateway, tools *ToolRegistry, mem *MemoryManager) *BaseAgent {
	return &BaseAgent{
		config:   cfg,
		gateway:  gw,
		tools:    tools,
		memory:   mem,
	}
}

func (a *BaseAgent) Type() AgentType { return a.config.Type }
func (a *BaseAgent) Config() AgentConfig { return a.config }

// Run 执行 ReAct 循环
func (a *BaseAgent) Run(ctx context.Context, input string, agentCtx AgentContext) (*AgentResult, error) {
	start := time.Now()
	result := &AgentResult{
		AgentType: a.config.Type,
		Steps:     make([]ReActStep, 0),
		ToolCalls: make([]ToolCallRecord, 0),
		Metadata:  make(map[string]interface{}),
	}

	// 构建系统提示词
	systemPrompt := a.buildSystemPrompt(agentCtx)

	// 获取历史记忆
	history := a.memory.GetShortTerm(agentCtx.SessionID)
	messages := []gateway.Message{
		{Role: gateway.RoleSystem, Content: systemPrompt},
	}
	for _, h := range history {
		messages = append(messages, gateway.Message{Role: gateway.RoleUser, Content: h.Input})
		messages = append(messages, gateway.Message{Role: gateway.RoleAssistant, Content: h.Output})
	}
	messages = append(messages, gateway.Message{Role: gateway.RoleUser, Content: input})

	// ReAct 循环
	for step := 0; step < a.config.MaxSteps; step++ {
		// 1. LLM 思考
		resp, err := a.gateway.Complete(ctx, gateway.CompletionRequest{
			Model:       a.config.Model,
			Messages:    messages,
			Temperature: a.config.Temperature,
			MaxTokens:   2048,
		})
		if err != nil {
			result.Error = fmt.Sprintf("LLM 调用失败: %v", err)
			result.Duration = time.Since(start)
			return result, err
		}

		choice := resp.Choices[0]
		thought := choice.Message.Content
		result.TokenUsage += resp.Usage.TotalTokens

		// 2. 解析 Action
		action, actionInput := parseAction(thought)

		stepRecord := ReActStep{
			Step:        step + 1,
			Thought:     thought,
			Action:      action,
			ActionInput: actionInput,
		}

		// 3. 如果是 Final Answer，结束
		if action == "final_answer" || action == "" {
			result.Success = true
			result.Output = actionInput
			if result.Output == "" {
				result.Output = thought
			}
			result.Steps = append(result.Steps, stepRecord)
			break
		}

		// 4. 执行工具调用
		observation, toolRecord, err := a.executeTool(ctx, action, actionInput, agentCtx)
		if err != nil {
			observation = fmt.Sprintf("工具执行失败: %v", err)
		}
		if toolRecord != nil {
			result.ToolCalls = append(result.ToolCalls, *toolRecord)
		}
		stepRecord.Observation = observation
		result.Steps = append(result.Steps, stepRecord)

		// 5. 将 observation 加入消息
		messages = append(messages, gateway.Message{Role: gateway.RoleAssistant, Content: thought})
		messages = append(messages, gateway.Message{Role: gateway.RoleUser, Content: "Observation: " + observation})
	}

	// 保存到记忆
	a.memory.AddShortTerm(agentCtx.SessionID, input, result.Output)

	result.Duration = time.Since(start)
	return result, nil
}

func (a *BaseAgent) buildSystemPrompt(agentCtx AgentContext) string {
	return fmt.Sprintf(`你是 OpsMesh 运维平台的 %s Agent。

你的职责：%s

可用工具：%s

执行规则：
1. 仔细分析用户输入，理解其意图
2. 如果需要查询信息，使用工具获取数据
3. 基于获取的数据进行分析，给出结论
4. 如果无法确定，明确说明不确定性
5. 最终回答以 "Final Answer:" 开头

当前上下文：
- 用户: %s
- 租户: %s
- 会话: %s

请逐步思考，每步输出 Thought/Action/Action Input/Observation，最后给出 Final Answer。`,
		a.config.Name, a.config.Description, strings.Join(a.config.Tools, ", "),
		agentCtx.UserID, agentCtx.TenantID, agentCtx.SessionID)
}

func (a *BaseAgent) executeTool(ctx context.Context, action, actionInput string, agentCtx AgentContext) (string, *ToolCallRecord, error) {
	start := time.Now()

	// 解析 action input 为 map
	var input map[string]interface{}
	if actionInput != "" {
		json.Unmarshal([]byte(actionInput), &input)
	}
	if input == nil {
		input = make(map[string]interface{})
	}

	tool, ok := a.tools.Get(action)
	if !ok {
		return "", nil, fmt.Errorf("未知工具: %s", action)
	}

	// 权限检查
	if !tool.CheckPermission(agentCtx) {
		return "", nil, fmt.Errorf("权限不足: %s", action)
	}

	// 执行
	output, err := tool.Execute(ctx, input, agentCtx)
	record := &ToolCallRecord{
		ToolName:   action,
		Input:      input,
		Output:     output,
		Duration:   time.Since(start),
		Timestamp:  time.Now(),
	}
	if err != nil {
		record.Error = err.Error()
	}

	outputStr := fmt.Sprintf("%v", output)
	if err != nil {
		outputStr = fmt.Sprintf("Error: %v", err)
	}
	return outputStr, record, err
}

func parseAction(thought string) (string, string) {
	// 解析 LLM 输出中的 Action 和 Action Input
	lines := strings.Split(thought, "\n")
	var action, actionInput string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Action:") {
			action = strings.TrimSpace(strings.TrimPrefix(line, "Action:"))
		}
		if strings.HasPrefix(line, "Action Input:") {
			actionInput = strings.TrimSpace(strings.TrimPrefix(line, "Action Input:"))
		}
	}
	// 检查 Final Answer
	if strings.Contains(thought, "Final Answer:") {
		idx := strings.Index(thought, "Final Answer:")
		return "final_answer", strings.TrimSpace(thought[idx+len("Final Answer:"):])
	}
	return action, actionInput
}

// ============================================================================
// 意图分类器
// ============================================================================

// IntentClassifier 意图分类器
type IntentClassifier struct {
	gateway *gateway.Gateway
}

func NewIntentClassifier(gw *gateway.Gateway) *IntentClassifier {
	return &IntentClassifier{gateway: gw}
}

// Classify 分类用户意图，返回 Agent 类型
func (ic *IntentClassifier) Classify(ctx context.Context, input string) (AgentType, float64, error) {
	prompt := fmt.Sprintf(`你是一个运维意图分类器。根据用户输入，判断其意图属于以下哪一类：

1. root_cause - 故障根因分析（如：分析告警、排查故障、定位问题）
2. noise - 智能降噪（如：告警风暴、告警聚类、重复告警）
3. nl_query - 自然语言查询（如：查询指标、查日志、查设备状态）
4. remediation - 自动修复（如：修复故障、执行修复方案、回滚）
5. capacity - 容量规划（如：容量预测、资源规划、扩容建议）
6. general - 通用运维（如：日常操作、配置管理、其他）

用户输入: %s

请只输出 JSON 格式：{"type": "xxx", "confidence": 0.95}`, input)

	resp, err := ic.gateway.Complete(ctx, gateway.CompletionRequest{
		Model:       "gpt-4o-mini",
		Messages:    []gateway.Message{{Role: gateway.RoleUser, Content: prompt}},
		Temperature: 0.1,
		MaxTokens:   100,
	})
	if err != nil {
		return AgentTypeGeneral, 0.5, err
	}

	var result struct {
		Type       string  `json:"type"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &result); err != nil {
		// 尝试从文本中提取
		for _, t := range []AgentType{AgentTypeRootCause, AgentTypeNoise, AgentTypeNLQuery, AgentTypeRemediation, AgentTypeCapacity} {
			if strings.Contains(strings.ToLower(resp.Choices[0].Message.Content), string(t)) {
				return t, 0.7, nil
			}
		}
		return AgentTypeGeneral, 0.5, nil
	}

	return AgentType(result.Type), result.Confidence, nil
}

// DecomposePrompt 将复杂提示词拆解为子任务 DAG
func (ic *IntentClassifier) DecomposePrompt(ctx context.Context, input string) ([]SubTask, error) {
	prompt := fmt.Sprintf(`你是一个运维任务拆解器。将用户的复杂运维请求拆解为可执行的子任务 DAG。

用户请求: %s

请输出 JSON 数组，每个元素包含：
- id: 子任务唯一标识
- description: 子任务描述
- agentType: 使用的 Agent 类型 (root_cause/noise/nl_query/remediation/capacity/general)
- dependencies: 依赖的子任务 ID 列表
- priority: 优先级 (1-10)

只输出 JSON 数组，不要其他内容。`, input)

	resp, err := ic.gateway.Complete(ctx, gateway.CompletionRequest{
		Model:       "gpt-4o-mini",
		Messages:    []gateway.Message{{Role: gateway.RoleUser, Content: prompt}},
		Temperature: 0.2,
		MaxTokens:   1000,
	})
	if err != nil {
		return nil, err
	}

	var tasks []SubTask
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &tasks); err != nil {
		// 返回单任务
		return []SubTask{{ID: "task-1", Description: input, AgentType: AgentTypeGeneral, Priority: 5}}, nil
	}
	return tasks, nil
}

// SubTask 子任务
type SubTask struct {
	ID           string   `json:"id"`
	Description  string   `json:"description"`
	AgentType    AgentType `json:"agentType"`
	Dependencies []string `json:"dependencies"`
	Priority     int      `json:"priority"`
}
