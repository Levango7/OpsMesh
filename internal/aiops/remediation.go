package aiops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// RemediationGenerator 修复生成器：根据根因生成修复方案（命令 + DAG）。
type RemediationGenerator struct {
	llm LLM
	cfg Config
}

// NewRemediationGenerator 构造修复生成器。
func NewRemediationGenerator(llm LLM, cfg Config) *RemediationGenerator {
	return &RemediationGenerator{llm: llm, cfg: cfg}
}

// RemediationRequest 修复生成请求。
type RemediationRequest struct {
	RootCause     *RootCause             `json:"rootCause"`
	DeviceContext *DeviceContext         `json:"deviceContext"`
	AlertContext  *AlertContext          `json:"alertContext,omitempty"`
	Constraints   RemediationConstraints `json:"constraints"`
}

// RemediationConstraints 修复约束。
type RemediationConstraints struct {
	MaxSteps        int    `json:"maxSteps"`        // 最大步骤数
	RiskTolerance   string `json:"riskTolerance"`   // low/medium/high
	RequireApproval bool   `json:"requireApproval"` // 高风险操作是否需要审批
	TimeoutSec      int    `json:"timeoutSec"`      // 单步超时（秒）
	AutoExecute     bool   `json:"autoExecute"`     // 是否自动执行（false=仅生成计划）
}

// Generate 生成修复方案。
func (g *RemediationGenerator) Generate(ctx context.Context, req *RemediationRequest) (*RemediationPlan, error) {
	if req == nil || req.RootCause == nil {
		return nil, fmt.Errorf("aiops: remediation request is nil")
	}

	prompt := g.buildRemediationPrompt(req)
	resp, err := g.llm.Chat(ctx, ChatRequest{
		Model:       g.cfg.Model,
		Messages:    []Message{{Role: "user", Content: prompt}},
		Temperature: g.cfg.Temperature,
		MaxTokens:   g.cfg.MaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("aiops: remediation LLM call: %w", err)
	}

	plan, err := parseRemediationPlan(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("aiops: parse remediation plan: %w", err)
	}

	// 填充缺失字段
	if plan.Safety.RequiresConfirm {
		req.Constraints.RequireApproval = true
	}
	if plan.EstimatedTime == "" {
		plan.EstimatedTime = "5-10m"
	}

	return plan, nil
}

// buildRemediationPrompt 构造修复生成提示词。
func (g *RemediationGenerator) buildRemediationPrompt(req *RemediationRequest) string {
	var sb strings.Builder

	sb.WriteString("你是一名资深 SRE，负责生成生产环境故障修复方案。\n\n")

	sb.WriteString("## 根因分析\n")
	sb.WriteString(fmt.Sprintf("- 类别: %s\n", req.RootCause.Category))
	sb.WriteString(fmt.Sprintf("- 组件: %s\n", req.RootCause.Component))
	sb.WriteString(fmt.Sprintf("- 描述: %s\n", req.RootCause.Description))
	sb.WriteString(fmt.Sprintf("- 信心度: %.0f%%\n", req.RootCause.Confidence*100))
	sb.WriteString(fmt.Sprintf("- 证据: %v\n", req.RootCause.Evidence))

	if req.AlertContext != nil && req.AlertContext.Alert != nil {
		sb.WriteString("\n## 告警上下文\n")
		sb.WriteString(fmt.Sprintf("- 告警 ID: %s\n", req.AlertContext.Alert.AlertID))
		sb.WriteString(fmt.Sprintf("- 设备: %s\n", req.AlertContext.Alert.DeviceID))
		sb.WriteString(fmt.Sprintf("- 级别: %s\n", req.AlertContext.Alert.Severity))
		sb.WriteString(fmt.Sprintf("- 指标: %s\n", req.AlertContext.Alert.Metric))
		sb.WriteString(fmt.Sprintf("- 消息: %s\n", req.AlertContext.Alert.Message))
	}

	if req.DeviceContext != nil {
		sb.WriteString("\n## 设备上下文\n")
		if req.DeviceContext.DeviceInfo != nil {
			d := req.DeviceContext.DeviceInfo
			sb.WriteString(fmt.Sprintf("- 主机名: %s\n", d.Hostname))
			sb.WriteString(fmt.Sprintf("- IP: %s\n", d.IP))
			sb.WriteString(fmt.Sprintf("- OS: %s %s\n", d.OS, d.Arch))
			sb.WriteString(fmt.Sprintf("- 状态: %s\n", d.State))
		}
		if req.DeviceContext.Metrics != nil {
			m := req.DeviceContext.Metrics
			sb.WriteString(fmt.Sprintf("- CPU: %.1f%%\n", m.CPU.Usage))
			sb.WriteString(fmt.Sprintf("- 内存: %.1f%%\n", m.Memory.Usage))
			sb.WriteString(fmt.Sprintf("- 运行时间: %ds\n", m.Uptime))
		}
	}

	sb.WriteString("\n## 约束\n")
	sb.WriteString(fmt.Sprintf("- 最大步骤数: %d\n", req.Constraints.MaxSteps))
	sb.WriteString(fmt.Sprintf("- 风险容忍度: %s\n", req.Constraints.RiskTolerance))
	sb.WriteString(fmt.Sprintf("- 需要审批: %t\n", req.Constraints.RequireApproval))
	sb.WriteString(fmt.Sprintf("- 超时: %ds\n", req.Constraints.TimeoutSec))
	sb.WriteString(fmt.Sprintf("- 自动执行: %t\n", req.Constraints.AutoExecute))

	sb.WriteString("\n## 输出格式 (JSON)\n")
	sb.WriteString(`{
  "title": "修复方案标题",
  "steps": [{"order":1,"title":"步骤1","action":"execute_task","target":"agentID","command":"shell 命令","condition":"","verifyCmd":"验证命令"}],
  "commands": [{"agentID":"target_agent","command":"shell 命令","type":"shell","timeout":30,"priority":5}],
  "dagNodes": [{"id":"n1","name":"步骤1","type":"shell","command":"cmd","dependsOn":[],"retryCount":3,"retryDelay":5}],
  "safety": {"requiresConfirm":false,"rollbackCmd":"","riskLevel":"low","notes":["安全提示"]}
}`)

	return sb.String()
}

// parseRemediationPlan 解析 LLM 返回的 JSON 为 RemediationPlan。
func parseRemediationPlan(content string) (*RemediationPlan, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var plan RemediationPlan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("aiops: invalid JSON from LLM: %w", err)
	}

	// 设置默认值
	if plan.Safety.RiskLevel == "" {
		plan.Safety.RiskLevel = "low"
	}
	if plan.EstimatedTime == "" {
		plan.EstimatedTime = "5-10m"
	}

	return &plan, nil
}

// GenerateFromNaturalLanguage 自然语言生成修复计划。
func (g *RemediationGenerator) GenerateFromNaturalLanguage(ctx context.Context, input string, ctx_ *DeviceContext) (*RemediationPlan, error) {
	prompt := g.buildNLGPrompt(input, ctx_)
	resp, err := g.llm.Chat(ctx, ChatRequest{
		Model:       g.cfg.Model,
		Messages:    []Message{{Role: "user", Content: prompt}},
		Temperature: g.cfg.Temperature,
		MaxTokens:   g.cfg.MaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("aiops: NLG LLM call: %w", err)
	}

	plan, err := parseRemediationPlan(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("aiops: parse NLG remediation plan: %w", err)
	}
	return plan, nil
}

// buildNLGPrompt 构造自然语言转结构化任务的提示词。
func (g *RemediationGenerator) buildNLGPrompt(input string, ctx_ *DeviceContext) string {
	var sb strings.Builder
	sb.WriteString("你是一名运维工程师助手，负责将自然语言描述转化为结构化的运维操作任务。\n\n")
	sb.WriteString(fmt.Sprintf("## 用户输入\n%s\n\n", input))

	if ctx_ != nil {
		sb.WriteString("## 设备上下文\n")
		if ctx_.DeviceInfo != nil {
			d := ctx_.DeviceInfo
			sb.WriteString(fmt.Sprintf("- 主机名: %s\n", d.Hostname))
			sb.WriteString(fmt.Sprintf("- IP: %s\n", d.IP))
			sb.WriteString(fmt.Sprintf("- OS: %s %s\n", d.OS, d.Arch))
		}
		if ctx_.Metrics != nil {
			m := ctx_.Metrics
			sb.WriteString(fmt.Sprintf("- CPU: %.1f%%\n", m.CPU.Usage))
			sb.WriteString(fmt.Sprintf("- 内存: %.1f%%\n", m.Memory.Usage))
			sb.WriteString(fmt.Sprintf("- 运行时间: %ds\n", m.Uptime))
		}
	}

	sb.WriteString("\n## 输出格式 (JSON)\n")
	sb.WriteString(`{"title":"操作标题","steps":[{"order":1,"title":"步骤1","action":"execute_task","target":"agent01","command":"shell 命令","verifyCmd":"验证"}],"commands":[{"agentID":"agent01","command":"shell 命令","type":"shell","timeout":30,"priority":5}]}`)

	return sb.String()
}

// Now 实现自然语言任务创建器（NLParser）
// NLParser 自然语言任务解析器。
type NLParser struct {
	llm LLM
	cfg Config
}

// NewNLParser 构造自然语言解析器。
func NewNLParser(llm LLM, cfg Config) *NLParser {
	return &NLParser{llm: llm, cfg: cfg}
}

// Parse 将自然语言描述解析为结构化任务。
func (p *NLParser) Parse(ctx context.Context, input string, devices []string) (*RemediationPlan, error) {
	gen := NewRemediationGenerator(p.llm, p.cfg)
	return gen.GenerateFromNaturalLanguage(ctx, input, nil)
}

// FormatDuration 格式化持续时间。
func FormatDuration(sec int) string {
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	if sec < 3600 {
		return fmt.Sprintf("%dm%ds", sec/60, sec%60)
	}
	return fmt.Sprintf("%dh%dm", sec/3600, (sec%3600)/60)
}
