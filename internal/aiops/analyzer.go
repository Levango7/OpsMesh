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

// Validate 校验告警上下文完整性。
func (a *AlertContext) Validate() error {
	if a == nil {
		return fmt.Errorf("aiops: alert context is nil")
	}
	if a.Alert == nil {
		return fmt.Errorf("aiops: alert is nil")
	}
	if a.Alert.AlertID == "" {
		return fmt.Errorf("aiops: alert.alertID is required")
	}
	if a.Alert.DeviceID == "" {
		return fmt.Errorf("aiops: alert.deviceID is required")
	}
	return nil
}

// RootCause 根因分析结果。
type RootCause struct {
	Category    string   `json:"category"`    // 硬件/软件/网络/配置/资源
	Component   string   `json:"component"`   // 具体组件/服务
	Description string   `json:"description"` // 根因描述
	Confidence  float64  `json:"confidence"`  // 信心度 0-1
	Evidence    []string `json:"evidence"`    // 证据列表（日志片段/指标值）
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
	if err := alertCtx.Validate(); err != nil {
		return nil, fmt.Errorf("aiops: %w", err)
	}

	prompt := a.buildAnalyzePrompt(alertCtx)
	systemPrompt := systemPromptRootCause()
	msgs := BuildSystemMessages(systemPrompt, prompt)

	resp, err := a.llm.Chat(ctx, ChatRequest{
		Model:       a.cfg.Model,
		Messages:    msgs,
		Temperature: 0.2, // 根因分析需要确定性，温度较低
		MaxTokens:   1536,
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

// systemPromptRootCause 根因分析的系统角色提示词。
func systemPromptRootCause() string {
	return `你是一名资深 SRE，负责分析生产环境告警并定位根因。
输入包含：告警信息、设备指标、相关日志、拓扑邻居、近期任务。
请基于证据链（指标异常→日志错误→关联事件）推导最可能的根因，输出 JSON。
字段：
- category：硬件/软件/网络/配置/资源 五选一
- component：具体组件名（如 nginx、mysql、磁盘、网卡）
- description：简洁的根因描述（≤200 字）
- confidence：0~1 的信心度
- evidence：证据列表，每条引用具体指标值或日志片段
不要编造不存在的证据，若证据不足则 confidence 设为 0.5 以下。`
}

// buildAnalyzePrompt 构造分析提示词。
func (a *Analyzer) buildAnalyzePrompt(ctx *AlertContext) string {
	var sb strings.Builder

	sb.WriteString("## 告警信息\n")
	fmt.Fprintf(&sb, "- ID: %s\n", ctx.Alert.AlertID)
	fmt.Fprintf(&sb, "- 设备: %s\n", ctx.Alert.DeviceID)
	fmt.Fprintf(&sb, "- 级别: %s\n", ctx.Alert.Severity)
	fmt.Fprintf(&sb, "- 指标: %s\n", ctx.Alert.Metric)
	fmt.Fprintf(&sb, "- 消息: %s\n", ctx.Alert.Message)
	fmt.Fprintf(&sb, "- 时间: %s\n", ctx.Alert.CreatedAt.Format(time.RFC3339))

	// 设备上下文
	if ctx.Metrics != nil && ctx.Metrics.DeviceInfo != nil {
		d := ctx.Metrics.DeviceInfo
		fmt.Fprintf(&sb, "\n## 设备基本信息\n")
		fmt.Fprintf(&sb, "- 主机名: %s\n", d.Hostname)
		fmt.Fprintf(&sb, "- IP: %s\n", d.IP)
		fmt.Fprintf(&sb, "- OS: %s %s\n", d.OS, d.Arch)
		fmt.Fprintf(&sb, "- 状态: %s\n", d.State)
	}

	// 设备指标
	if ctx.Metrics != nil && ctx.Metrics.Metrics != nil {
		m := ctx.Metrics.Metrics
		fmt.Fprintf(&sb, "\n## 设备指标\n")
		fmt.Fprintf(&sb, "- CPU: %.1f%% (%d cores)\n", m.CPU.Usage, m.CPU.Cores)
		fmt.Fprintf(&sb, "- 内存: %.1f%% (%.0f/%.0f MB)\n", m.Memory.Usage, float64(m.Memory.Used), float64(m.Memory.Total))
		for _, d := range m.Disks {
			fmt.Fprintf(&sb, "- 磁盘 %s: %.1f%%\n", d.Mount, d.Usage)
		}
		for _, n := range m.Network {
			fmt.Fprintf(&sb, "- 网络 %s: %s %s\n", n.Name, n.Status, n.IP)
		}
		fmt.Fprintf(&sb, "- 进程数: %d\n", m.ProcessCount)
		fmt.Fprintf(&sb, "- 运行时间: %ds\n", m.Uptime)
	}

	// 相关告警
	if ctx.Metrics != nil && len(ctx.Metrics.Alerts) > 0 {
		fmt.Fprintf(&sb, "\n## 相关告警 (%d 条)\n", len(ctx.Metrics.Alerts))
		for _, al := range ctx.Metrics.Alerts {
			fmt.Fprintf(&sb, "- [%s] %s (设备: %s, 指标: %s)\n", al.Severity, al.Message, al.DeviceID, al.Metric)
		}
	}

	// 相关日志（限制 30 条）
	if len(ctx.Logs) > 0 {
		fmt.Fprintf(&sb, "\n## 相关日志 (最多 30 条)\n")
		maxLogs := 30
		if len(ctx.Logs) > maxLogs {
			ctx.Logs = ctx.Logs[len(ctx.Logs)-maxLogs:]
		}
		for _, l := range ctx.Logs {
			fmt.Fprintf(&sb, "[%s] [%s] [%s] %s\n", l.Timestamp.Format(time.RFC3339), l.Level, l.Source, l.Message)
		}
	}

	// 拓扑邻居
	if len(ctx.RelatedDev) > 0 {
		fmt.Fprintf(&sb, "\n## 拓扑邻居设备: %v\n", ctx.RelatedDev)
	}
	if len(ctx.NeighborTx) > 0 {
		fmt.Fprintf(&sb, "\n## 邻居设备近期事务: %v\n", ctx.NeighborTx)
	}

	// 近期任务
	if ctx.Metrics != nil && len(ctx.Metrics.Tasks) > 0 {
		fmt.Fprintf(&sb, "\n## 该设备近期任务 (最多 10 条)\n")
		maxTasks := 10
		if len(ctx.Metrics.Tasks) > maxTasks {
			ctx.Metrics.Tasks = ctx.Metrics.Tasks[:maxTasks]
		}
		for _, t := range ctx.Metrics.Tasks {
			fmt.Fprintf(&sb, "- [%s] type=%s cmd=%s status=%s\n", t.TaskID, t.Type, truncateCmd(t.Command), t.Status)
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
	content = SanitizeJSON(content)

	var rc RootCause
	if err := json.Unmarshal([]byte(content), &rc); err != nil {
		// 降级：如果 LLM 返回非 JSON 格式，将其作为 description
		return &RootCause{
			Category:    "未知",
			Component:   "未知",
			Description: content,
			Confidence:  0.3,
			Evidence:    []string{},
		}, nil
	}

	// 归一化字段
	if rc.Category == "" {
		rc.Category = "未知"
	}
	if rc.Component == "" {
		rc.Component = "未知"
	}
	if rc.Confidence < 0 {
		rc.Confidence = 0
	}
	if rc.Confidence > 1 {
		rc.Confidence = 1
	}
	if rc.Evidence == nil {
		rc.Evidence = []string{}
	}
	return &rc, nil
}
