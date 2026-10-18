package aiops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DangerousCmdPatterns 危险命令模式（匹配任一即拦截下发）。
var DangerousCmdPatterns = []string{
	"rm -rf",
	"rm -r ",
	"mkfs",
	"dd if=",
	"> /dev/sda",
	"shutdown",
	"reboot",
	"init 0",
	"init 6",
	":(){ :|:& };:", // fork bomb
	"chmod -R 777 /",
	"chown -R root /",
	"mv / /dev/null",
	"> /etc/passwd",
	"> /etc/shadow",
	"curl .* | sh",
	"wget .* | sh",
}

// IsDangerousCommand 检查命令是否包含危险模式。
func IsDangerousCommand(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, pat := range DangerousCmdPatterns {
		if strings.Contains(lower, strings.ToLower(pat)) {
			return true
		}
	}
	return false
}

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

// RemediationPlan 修复方案。
type RemediationPlan struct {
	Title         string               `json:"title"`              // 修复标题
	Steps         []RemediationStep    `json:"steps"`              // 执行步骤
	Commands      []RemediationCommand `json:"commands"`           // 命令列表
	DAGNodes      []DAGNodeSpec        `json:"dagNodes,omitempty"` // DAG 节点（复杂修复）
	Safety        RemediationSafety    `json:"safety"`             // 安全建议
	EstimatedTime string               `json:"estimatedTime"`      // 预估耗时
}

// RemediationStep 修复步骤（人工确认用）。
type RemediationStep struct {
	Order     int    `json:"order"`
	Title     string `json:"title"`
	Action    string `json:"action"`    // execute_task / restart / scale / isolate
	Target    string `json:"target"`    // 目标设备/服务
	Command   string `json:"command"`   // shell 命令（Action=execute_task 时）
	Condition string `json:"condition"` // 执行条件（用于 DAG 条件分支）
	VerifyCmd string `json:"verifyCmd"` // 验证命令
}

// RemediationCommand 待执行的命令。
type RemediationCommand struct {
	AgentID  string `json:"agentID"`  // 目标 agent（设备）
	Command  string `json:"command"`  // shell 命令
	Type     string `json:"type"`     // shell/service/file (default shell)
	Timeout  int    `json:"timeout"`  // 超时秒
	Priority int    `json:"priority"` // 优先级 1-10
}

// DAGNodeSpec DAG 节点规范（用于生成多步骤修复工作流）。
type DAGNodeSpec struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Type       string   `json:"type"` // shell/service/file
	Command    string   `json:"command"`
	Path       string   `json:"path"`
	DependsOn  []string `json:"dependsOn"`
	RetryCount int      `json:"retryCount"`
	RetryDelay int      `json:"retryDelay"`
	Condition  string   `json:"condition,omitempty"`
	ThenNodes  []string `json:"thenNodes,omitempty"`
	ElseNodes  []string `json:"elseNodes,omitempty"`
}

// RemediationSafety 安全建议。
type RemediationSafety struct {
	RequiresConfirm bool     `json:"requiresConfirm"` // 是否需要人工确认
	RollbackCmd     string   `json:"rollbackCmd"`     // 回滚命令
	RiskLevel       string   `json:"riskLevel"`       // low/medium/high
	Notes           []string `json:"notes"`           // 安全提示
}

// Generate 生成修复方案。
func (g *RemediationGenerator) Generate(ctx context.Context, req *RemediationRequest) (*RemediationPlan, error) {
	if req == nil || req.RootCause == nil {
		return nil, fmt.Errorf("aiops: remediation request is nil")
	}

	prompt := g.buildRemediationPrompt(req)
	systemPrompt := systemPromptRemediation()
	msgs := BuildSystemMessages(systemPrompt, prompt)

	resp, err := g.llm.Chat(ctx, ChatRequest{
		Model:       g.cfg.Model,
		Messages:    msgs,
		Temperature: 0.3,
		MaxTokens:   2048,
	})
	if err != nil {
		return nil, fmt.Errorf("aiops: remediation LLM call: %w", err)
	}

	plan, err := parseRemediationPlan(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("aiops: parse remediation plan: %w", err)
	}

	// 安全校验与补全
	plan = g.sanitizePlan(plan, req)
	if plan.EstimatedTime == "" {
		plan.EstimatedTime = "5-10m"
	}

	return plan, nil
}

// systemPromptRemediation 修复生成的系统角色提示词。
func systemPromptRemediation() string {
	return `你是一名资深 SRE，负责生成生产环境故障修复方案。
输入包含：根因分析结果、设备上下文、告警信息、修复约束。
请输出可执行的修复方案，JSON 格式。
字段：
- title：修复标题（简洁）
- steps：人工执行步骤，每条含 order/title/action/target/command/condition/verifyCmd
- commands：待下发给 agent 的命令，每条含 agentID/command/type/timeout/priority
- dagNodes：多步骤修复的 DAG 节点（复杂场景）
- safety：{requiresConfirm: bool, rollbackCmd: string, riskLevel: low|medium|high, notes: []}
- estimatedTime：预估耗时（如 "5-10m"）

安全规则：
1. riskLevel 为 high 时，requiresConfirm 必须为 true
2. 危险命令（rm -rf/mkfs/dd/reboot/shutdown 等）不得出现在 commands 中
3. 每条命令必须指定 agentID 和超时
4. 若证据不足，riskLevel 设为 high，requiresConfirm 设为 true`
}

// buildRemediationPrompt 构造修复生成提示词。
func (g *RemediationGenerator) buildRemediationPrompt(req *RemediationRequest) string {
	var sb strings.Builder

	sb.WriteString("## 根因分析\n")
	fmt.Fprintf(&sb, "- 类别: %s\n", req.RootCause.Category)
	fmt.Fprintf(&sb, "- 组件: %s\n", req.RootCause.Component)
	fmt.Fprintf(&sb, "- 描述: %s\n", req.RootCause.Description)
	fmt.Fprintf(&sb, "- 信心度: %.0f%%\n", req.RootCause.Confidence*100)
	fmt.Fprintf(&sb, "- 证据: %v\n", req.RootCause.Evidence)

	if req.AlertContext != nil && req.AlertContext.Alert != nil {
		sb.WriteString("\n## 告警上下文\n")
		fmt.Fprintf(&sb, "- 告警 ID: %s\n", req.AlertContext.Alert.AlertID)
		fmt.Fprintf(&sb, "- 设备: %s\n", req.AlertContext.Alert.DeviceID)
		fmt.Fprintf(&sb, "- 级别: %s\n", req.AlertContext.Alert.Severity)
		fmt.Fprintf(&sb, "- 指标: %s\n", req.AlertContext.Alert.Metric)
		fmt.Fprintf(&sb, "- 消息: %s\n", req.AlertContext.Alert.Message)
	}

	if req.DeviceContext != nil {
		sb.WriteString("\n## 设备上下文\n")
		if req.DeviceContext.DeviceInfo != nil {
			d := req.DeviceContext.DeviceInfo
			fmt.Fprintf(&sb, "- 主机名: %s\n", d.Hostname)
			fmt.Fprintf(&sb, "- IP: %s\n", d.IP)
			fmt.Fprintf(&sb, "- OS: %s %s\n", d.OS, d.Arch)
			fmt.Fprintf(&sb, "- 状态: %s\n", d.State)
		}
		if req.DeviceContext.Metrics != nil {
			m := req.DeviceContext.Metrics
			fmt.Fprintf(&sb, "- CPU: %.1f%%\n", m.CPU.Usage)
			fmt.Fprintf(&sb, "- 内存: %.1f%%\n", m.Memory.Usage)
			fmt.Fprintf(&sb, "- 运行时间: %ds\n", m.Uptime)
		}
	}

	sb.WriteString("\n## 约束\n")
	fmt.Fprintf(&sb, "- 最大步骤数: %d\n", req.Constraints.MaxSteps)
	fmt.Fprintf(&sb, "- 风险容忍度: %s\n", req.Constraints.RiskTolerance)
	fmt.Fprintf(&sb, "- 需要审批: %t\n", req.Constraints.RequireApproval)
	fmt.Fprintf(&sb, "- 超时: %ds\n", req.Constraints.TimeoutSec)
	fmt.Fprintf(&sb, "- 自动执行: %t\n", req.Constraints.AutoExecute)

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

// sanitizePlan 安全校验与补全：拦截危险命令、补全缺失字段、调整安全等级。
func (g *RemediationGenerator) sanitizePlan(plan *RemediationPlan, req *RemediationRequest) *RemediationPlan {
	if plan == nil {
		return plan
	}

	// 补全缺失字段
	if plan.Safety.RiskLevel == "" {
		plan.Safety.RiskLevel = "low"
	}

	// 检查命令中的危险模式
	hasDangerous := false
	safeCommands := make([]RemediationCommand, 0, len(plan.Commands))
	for _, cmd := range plan.Commands {
		if IsDangerousCommand(cmd.Command) {
			hasDangerous = true
			continue // 丢弃危险命令
		}
		if cmd.Type == "" {
			cmd.Type = "shell"
		}
		if cmd.Timeout <= 0 {
			cmd.Timeout = 30
		}
		if cmd.Priority <= 0 {
			cmd.Priority = 5
		}
		safeCommands = append(safeCommands, cmd)
	}
	plan.Commands = safeCommands

	// 若存在危险命令被拦截，提升风险等级并要求确认
	if hasDangerous {
		plan.Safety.RiskLevel = "high"
		plan.Safety.RequiresConfirm = true
		plan.Safety.Notes = append(plan.Safety.Notes, "检测并拦截了危险命令，已从执行列表中移除")
	}

	// 根据约束调整
	if req.Constraints.RiskTolerance == "low" && plan.Safety.RiskLevel == "high" {
		plan.Safety.RequiresConfirm = true
	}

	if plan.Safety.RiskLevel == "high" && !plan.Safety.RequiresConfirm {
		plan.Safety.RequiresConfirm = true
	}

	if plan.Safety.Notes == nil {
		plan.Safety.Notes = []string{}
	}

	return plan
}

// GenerateFromNaturalLanguage 自然语言生成修复计划。
func (g *RemediationGenerator) GenerateFromNaturalLanguage(ctx context.Context, input string, ctx_ *DeviceContext) (*RemediationPlan, error) {
	prompt := g.buildNLGPrompt(input, ctx_)
	resp, err := g.llm.Chat(ctx, ChatRequest{
		Model:       g.cfg.Model,
		Messages:    BuildSystemMessages(systemPromptNLG(), prompt),
		Temperature: 0.3,
		MaxTokens:   2048,
	})
	if err != nil {
		return nil, fmt.Errorf("aiops: NLG LLM call: %w", err)
	}

	plan, err := parseRemediationPlan(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("aiops: parse NLG remediation plan: %w", err)
	}
	plan = g.sanitizePlan(plan, &RemediationRequest{})
	return plan, nil
}

// systemPromptNLG 自然语言转任务的系统提示词。
func systemPromptNLG() string {
	return `你是一名运维工程师助手，负责将自然语言描述转化为结构化的运维操作任务。
输出 JSON：{"title":"操作标题","steps":[...],"commands":[{"agentID":"...","command":"shell 命令","type":"shell","timeout":30,"priority":5}]}
安全规则：危险命令（rm -rf/mkfs/dd/reboot/shutdown 等）不得生成；每条命令需 agentID 和超时。`
}

// buildNLGPrompt 构造自然语言转结构化任务的提示词。
func (g *RemediationGenerator) buildNLGPrompt(input string, ctx_ *DeviceContext) string {
	var sb strings.Builder
	sb.WriteString("## 用户输入\n")
	sb.WriteString(input)
	sb.WriteString("\n\n")

	if ctx_ != nil {
		sb.WriteString("## 设备上下文\n")
		if ctx_.DeviceInfo != nil {
			d := ctx_.DeviceInfo
			fmt.Fprintf(&sb, "- 主机名: %s\n", d.Hostname)
			fmt.Fprintf(&sb, "- IP: %s\n", d.IP)
			fmt.Fprintf(&sb, "- OS: %s %s\n", d.OS, d.Arch)
		}
		if ctx_.Metrics != nil {
			m := ctx_.Metrics
			fmt.Fprintf(&sb, "- CPU: %.1f%%\n", m.CPU.Usage)
			fmt.Fprintf(&sb, "- 内存: %.1f%%\n", m.Memory.Usage)
			fmt.Fprintf(&sb, "- 运行时间: %ds\n", m.Uptime)
		}
	}

	sb.WriteString("\n## 输出格式 (JSON)\n")
	sb.WriteString(`{"title":"操作标题","steps":[{"order":1,"title":"步骤1","action":"execute_task","target":"agent01","command":"shell 命令","verifyCmd":"验证"}],"commands":[{"agentID":"agent01","command":"shell 命令","type":"shell","timeout":30,"priority":5}]}`)

	return sb.String()
}

// parseRemediationPlan 解析 LLM 返回的 JSON 为 RemediationPlan。
func parseRemediationPlan(content string) (*RemediationPlan, error) {
	content = SanitizeJSON(content)

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
