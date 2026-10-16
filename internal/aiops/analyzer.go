package aiops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// SeverityLevel 告警等级。
type SeverityLevel string

const (
	SeverityLevelCritical SeverityLevel = "critical"
	SeverityLevelWarning  SeverityLevel = "warning"
	SeverityLevelInfo     SeverityLevel = "info"
)

// DeviceContext 设备上下文：设备信息 + 最近指标 + 状态。
type DeviceContext struct {
	DeviceInfo *proto.DeviceInfo    `json:"deviceInfo"`
	Metrics    *proto.DeviceMetrics `json:"metrics,omitempty"`
	Tasks      []*proto.Task        `json:"tasks,omitempty"`  // 近期任务记录
	Alerts     []*proto.Alert       `json:"alerts,omitempty"` // 设备相关告警
	Uptime     int64                `json:"uptime"`           // 运行时长（秒）
}

// LogEntry 单条日志。
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`   // INFO/WARN/ERROR/FATAL
	Source    string    `json:"source"`  // 日志来源（服务名/模块名）
	Message   string    `json:"message"` // 日志正文
}

// AlertContext 告警上下文。
type AlertContext struct {
	Alert      *proto.Alert   `json:"alert"`
	Logs       []*LogEntry    `json:"logs,omitempty"`           // 告警窗口内相关日志
	Metrics    *DeviceContext `json:"metrics,omitempty"`        // 告警设备的指标上下文
	RelatedDev []string       `json:"relatedDevices,omitempty"` // 拓扑邻居设备 ID
	NeighborTx []string       `json:"neighborTasks,omitempty"`  // 邻居设备近期任务
}

// RootCause 根因分析结果。
type RootCause struct {
	Category    string   `json:"category"`    // 硬件/软件/网络/配置/资源
	Component   string   `json:"component"`   // 具体组件/服务
	Description string   `json:"description"` // 根因描述
	Confidence  float64  `json:"confidence"`  // 信心度 0-1
	Evidence    []string `json:"evidence"`    // 证据列表（日志片段/指标值）
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

// Analyzer 故障分析器：将告警 + 日志 + 指标 + 拓扑上下文交给 LLM，获取根因分析。
type Analyzer struct {
	llm LLM
	cfg Config
}

// NewAnalyzer 构造故障分析器。
func NewAnalyzer(llm LLM, cfg Config) *Analyzer {
	return &Analyzer{llm: llm, cfg: cfg}
}

// Analyze 执行根因分析。
func (a *Analyzer) Analyze(ctx context.Context, alertCtx *AlertContext) (*RootCause, error) {
	if alertCtx == nil || alertCtx.Alert == nil {
		return nil, fmt.Errorf("aiops: alert context is nil")
	}

	prompt := a.buildAnalyzePrompt(alertCtx)
	resp, err := a.llm.Chat(ctx, ChatRequest{
		Model:       a.cfg.Model,
		Messages:    []Message{{Role: "user", Content: prompt}},
		Temperature: a.cfg.Temperature,
		MaxTokens:   a.cfg.MaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("aiops: analyze LLM call: %w", err)
	}

	rc, err := parseRootCause(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("aiops: parse root cause: %w", err)
	}
	return rc, nil
}

// buildAnalyzePrompt 构造分析提示词。
func (a *Analyzer) buildAnalyzePrompt(ctx *AlertContext) string {
	var sb strings.Builder

	sb.WriteString("你是一名资深 SRE，负责分析生产环境告警。请分析以下告警的根因，输出 JSON。\n\n")

	// 告警基本信息
	sb.WriteString(fmt.Sprintf("## 告警信息\n"))
	sb.WriteString(fmt.Sprintf("- ID: %s\n", ctx.Alert.AlertID))
	sb.WriteString(fmt.Sprintf("- 设备: %s\n", ctx.Alert.DeviceID))
	sb.WriteString(fmt.Sprintf("- 级别: %s\n", ctx.Alert.Severity))
	sb.WriteString(fmt.Sprintf("- 指标: %s\n", ctx.Alert.Metric))
	sb.WriteString(fmt.Sprintf("- 消息: %s\n", ctx.Alert.Message))
	sb.WriteString(fmt.Sprintf("- 时间: %s\n", ctx.Alert.CreatedAt.Format(time.RFC3339)))

	// 设备上下文
	if ctx.Metrics != nil && ctx.Metrics.DeviceInfo != nil {
		d := ctx.Metrics.DeviceInfo
		sb.WriteString(fmt.Sprintf("\n## 设备基本信息\n"))
		sb.WriteString(fmt.Sprintf("- 主机名: %s\n", d.Hostname))
		sb.WriteString(fmt.Sprintf("- IP: %s\n", d.IP))
		sb.WriteString(fmt.Sprintf("- OS: %s %s\n", d.OS, d.Arch))
		sb.WriteString(fmt.Sprintf("- 状态: %s\n", d.State))
	}

	// 设备指标
	if ctx.Metrics != nil && ctx.Metrics.Metrics != nil {
		m := ctx.Metrics.Metrics
		sb.WriteString(fmt.Sprintf("\n## 设备指标\n"))
		sb.WriteString(fmt.Sprintf("- CPU: %.1f%% (%d cores)\n", m.CPU.Usage, m.CPU.Cores))
		sb.WriteString(fmt.Sprintf("- 内存: %.1f%% (%.0f/%.0f MB)\n", m.Memory.Usage, float64(m.Memory.Used), float64(m.Memory.Total)))
		for _, d := range m.Disks {
			sb.WriteString(fmt.Sprintf("- 磁盘 %s: %.1f%%\n", d.Mount, d.Usage))
		}
		for _, n := range m.Network {
			sb.WriteString(fmt.Sprintf("- 网络 %s: %s %s\n", n.Name, n.Status, n.IP))
		}
		sb.WriteString(fmt.Sprintf("- 进程数: %d\n", m.ProcessCount))
		sb.WriteString(fmt.Sprintf("- 运行时间: %ds\n", m.Uptime))
	}

	// 相关告警
	if ctx.Metrics != nil && len(ctx.Metrics.Alerts) > 0 {
		sb.WriteString(fmt.Sprintf("\n## 相关告警 (%d 条)\n", len(ctx.Metrics.Alerts)))
		for _, al := range ctx.Metrics.Alerts {
			sb.WriteString(fmt.Sprintf("- [%s] %s (设备: %s, 指标: %s)\n", al.Severity, al.Message, al.DeviceID, al.Metric))
		}
	}

	// 相关日志（限制 30 条）
	if len(ctx.Logs) > 0 {
		sb.WriteString(fmt.Sprintf("\n## 相关日志 (最多 30 条)\n"))
		maxLogs := 30
		if len(ctx.Logs) > maxLogs {
			ctx.Logs = ctx.Logs[len(ctx.Logs)-maxLogs:]
		}
		for _, l := range ctx.Logs {
			sb.WriteString(fmt.Sprintf("[%s] [%s] [%s] %s\n", l.Timestamp.Format(time.RFC3339), l.Level, l.Source, l.Message))
		}
	}

	// 拓扑邻居
	if len(ctx.RelatedDev) > 0 {
		sb.WriteString(fmt.Sprintf("\n## 拓扑邻居设备: %v\n", ctx.RelatedDev))
	}
	if len(ctx.NeighborTx) > 0 {
		sb.WriteString(fmt.Sprintf("\n## 邻居设备近期事务: %v\n", ctx.NeighborTx))
	}

	// 近期任务
	if ctx.Metrics != nil && len(ctx.Metrics.Tasks) > 0 {
		sb.WriteString(fmt.Sprintf("\n## 该设备近期任务 (最多 10 条)\n"))
		maxTasks := 10
		if len(ctx.Metrics.Tasks) > maxTasks {
			ctx.Metrics.Tasks = ctx.Metrics.Tasks[:maxTasks]
		}
		for _, t := range ctx.Metrics.Tasks {
			sb.WriteString(fmt.Sprintf("- [%s] type=%s cmd=%s status=%s\n", t.TaskID, t.Type, truncateCmd(t.Command), t.Status))
		}
	}

	sb.WriteString("\n## 输出格式 (JSON)\n")
	sb.WriteString(`{"category":"硬件|软件|网络|配置|资源","component":"具体组件","description":"根因描述","confidence":0.95,"evidence":["证据1","证据2"]}`)

	return sb.String()
}

func truncateCmd(s string) string {
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

// parseRootCause 解析 LLM 返回的 JSON 为 RootCause。
func parseRootCause(content string) (*RootCause, error) {
	// LLM 可能在代码块中返回 JSON
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var rc RootCause
	if err := json.Unmarshal([]byte(content), &rc); err != nil {
		// 降级：如果 LLM 返回非 JSON 格式，将其作为 description
		return &RootCause{
			Category:    "未知",
			Component:   "未知",
			Description: content,
			Confidence:  0.5,
			Evidence:    []string{},
		}, nil
	}
	return &rc, nil
}
