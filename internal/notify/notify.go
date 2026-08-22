// Package notify 告警通知核心：聚合/抑制 + 多通道推送（Webhook/Email/Slack/企业微信）。
//
// 本文件实现 告警通知增强：
//   - AlertAggregator：同源告警聚合（5 分钟窗口）+ 级别抑制（critical 抑制同源 warning）
//   - Slack 通道：Webhook URL 含 slack.com 域名时自动识别，Block Kit 格式
//   - 企业微信通道：Webhook URL 含 qyapi.weixin.qq.com 域名时自动识别，markdown 格式
//   - Email 通道：net/smtp 发送，配置缺失时跳过
package notify

import (
	"bytes"
	"context"
	"fmt"
	"net/smtp"
	"net/url"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/proto"
	"opsmesh/internal/secrets"
)

// ============================================================================
// 告警聚合与抑制
// ============================================================================

// AggregateWindow 同源告警聚合窗口（5 分钟）：相同 metric+device 在窗口内只推送一次。
const AggregateWindow = 5 * time.Minute

// aggregatorEntry 聚合缓存条目：记录最近一次推送时间与已推送的最高级别。
type aggregatorEntry struct {
	lastPushed time.Time
	severity   string // 已推送的最高级别（critical > warning）
}

// AlertAggregator 告警聚合器：防告警风暴。
//   - 同源聚合：相同 metric+device 在 AggregateWindow 内只推送一次。
//   - 级别抑制：高级别（critical）已触发时抑制同源低级别（warning）。
//
// 用 sync.RWMutex 保护 entries 并发安全（notifyLoop 单 goroutine 调用，
// 但 Cleanup 可能由独立 goroutine 周期执行，故需互斥保护）。
type AlertAggregator struct {
	mu      sync.RWMutex
	entries map[string]aggregatorEntry // key: metric+":"+deviceID
}

// NewAlertAggregator 构造聚合器。
func NewAlertAggregator() *AlertAggregator {
	return &AlertAggregator{entries: make(map[string]aggregatorEntry)}
}

// severityRank 返回级别排序：critical=2, warning=1, 其他=0。
func severityRank(s string) int {
	switch s {
	case "critical":
		return 2
	case "warning":
		return 1
	default:
		return 0
	}
}

// aggregateKey 返回告警的聚合键：metric+":"+deviceID。
// metric 为空时回退到 message（兼容未设置 metric 的旧告警，如 task dead-letter）。
func aggregateKey(a *proto.Alert) string {
	metric := a.Metric
	if metric == "" {
		metric = a.Message
	}
	return metric + ":" + a.DeviceID
}

// Allow 判断告警是否应被推送（true=推送，false=抑制/聚合跳过）。
// 放行时同时更新聚合缓存（记录推送时间与级别）。
//
// 规则：
//  1. 同源键不存在或已过期（超出 AggregateWindow）→ 放行
//  2. 同源键在窗口内且当前级别 <= 已推送级别 → 抑制（false）
//  3. 同源键在窗口内但当前级别更高 → 放行（升级告警不应被聚合吞掉）
func (ag *AlertAggregator) Allow(a *proto.Alert, now time.Time) bool {
	key := aggregateKey(a)
	ag.mu.Lock()
	defer ag.mu.Unlock()
	if e, exists := ag.entries[key]; exists {
		// 同源聚合：窗口内只推送一次。
		if now.Sub(e.lastPushed) < AggregateWindow {
			// 级别抑制：当前告警级别 <= 已推送级别时抑制。
			if severityRank(a.Severity) <= severityRank(e.severity) {
				return false
			}
			// 当前级别更高，放行并更新（升级告警透传）。
		}
	}
	// 放行：更新缓存。
	ag.entries[key] = aggregatorEntry{lastPushed: now, severity: a.Severity}
	return true
}

// Cleanup 清理过期条目（lastPushed 早于 cutoff）。周期调用防止内存泄漏。
func (ag *AlertAggregator) Cleanup(cutoff time.Time) {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	for k, e := range ag.entries {
		if e.lastPushed.Before(cutoff) {
			delete(ag.entries, k)
		}
	}
}

// ============================================================================
// Slack 通道（Block Kit 格式）
// ============================================================================

// slackBlockKit 构造 Slack Block Kit 消息体（JSON serializable）。
// 文档：https://api.slack.com/messaging/webhooks
func slackBlockKit(a *proto.Alert) map[string]interface{} {
	emoji := "⚠️"
	if a.Severity == "critical" {
		emoji = "🔴"
	}
	return map[string]interface{}{
		"blocks": []map[string]interface{}{
			{
				"type": "header",
				"text": map[string]interface{}{
					"type": "plain_text",
					"text": emoji + " OpsMesh 告警",
				},
			},
			{
				"type": "section",
				"text": map[string]interface{}{
					"type": "mrkdwn",
					"text": fmt.Sprintf(
						"*严重级别*: %s\n*设备*: %s\n*Agent*: %s\n*时间*: %s\n\n%s",
						a.Severity, a.DeviceID, a.AgentID,
						a.CreatedAt.Format("2006-01-02 15:04:05"),
						a.Message,
					),
				},
			},
		},
	}
}

// ============================================================================
// 企业微信通道（markdown 格式）
// ============================================================================

// wecomMarkdown 构造企业微信群机器人 markdown 消息体（JSON serializable）。
// 文档：https://developer.work.weixin.qq.com/document/path/91770
func wecomMarkdown(a *proto.Alert) map[string]interface{} {
	emoji := "⚠️"
	if a.Severity == "critical" {
		emoji = "🔴"
	}
	return map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]interface{}{
			"content": fmt.Sprintf(
				"## %s OpsMesh 告警\n\n"+
					"> **严重级别**: %s\n"+
					"> **设备**: %s\n"+
					"> **Agent**: %s\n"+
					"> **时间**: %s\n\n"+
					"%s",
				emoji, a.Severity, a.DeviceID, a.AgentID,
				a.CreatedAt.Format("2006-01-02 15:04:05"),
				a.Message,
			),
		},
	}
}

// ============================================================================
// 通道识别与 Webhook 分发
// ============================================================================

// matchDomain 判断 host 是否等于 domain 或为其子域（host == domain 或 host 以 "."+domain 结尾）。
// 用于 DetectChannelByURL 的精确域名匹配，避免 strings.Contains 的子串误判
// （如 strings.Contains("evil-slack.com.attacker.com", "slack.com") 会误命中）。
func matchDomain(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}

// DetectChannelByURL 根据 webhook URL 域名自动识别通道：
//   - slack.com → "slack"
//   - qyapi.weixin.qq.com → "wecom"（企业微信）
//   - 其他/解析失败 → ""（回退到显式 notifierType）
func DetectChannelByURL(webhookURL string) string {
	if webhookURL == "" {
		return ""
	}
	u, err := url.Parse(webhookURL)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Host)
	if matchDomain(host, "slack.com") {
		return "slack"
	}
	if matchDomain(host, "qyapi.weixin.qq.com") {
		return "wecom"
	}
	return ""
}

// PostWebhook 按 URL 域名自动识别通道 / 显式 notifierType 分发 Webhook 推送。
// 支持通道：generic / feishu / dingtalk / slack / wecom。
// URL 域名识别优先，无法识别时回退到 notifierType（generic/feishu/dingtalk）。
func PostWebhook(notifierType, webhookURL string, a *proto.Alert) error {
	if webhookURL == "" || a == nil {
		return nil
	}
	ch := DetectChannelByURL(webhookURL)
	if ch == "" {
		ch = notifierType
	}
	switch ch {
	case "slack":
		return postJSON(webhookURL, slackBlockKit(a))
	case "wecom":
		return postJSON(webhookURL, wecomMarkdown(a))
	default:
		return PostByType(ch, webhookURL, a) // generic / feishu / dingtalk
	}
}

// ============================================================================
// Email 通道（net/smtp）
// ============================================================================

// EmailConfig 邮件通道配置。必填字段（Host/Port/From/To）任一为空时视为未配置，SendEmail 跳过。
type EmailConfig struct {
	Host string // SMTP 服务器地址（如 smtp.example.com）
	Port int    // SMTP 端口（如 25/465/587）
	User string // SMTP 用户名（认证用；空=匿名发送）
	Pass string // SMTP 密码（认证用）
	From string // 发件人地址（如 opsmesh@example.com）
	To   string // 收件人列表（逗号分隔，如 ops1@example.com,ops2@example.com）
}

// Enabled 返回邮件通道是否已配置（必填字段非空）。
func (c *EmailConfig) Enabled() bool {
	return c != nil && c.Host != "" && c.Port > 0 && c.From != "" && c.To != ""
}

// SendEmail 通过 SMTP 发送告警邮件。配置缺失（!Enabled）或 alert 为 nil 时跳过（返回 nil）。
// 超时 10s（net/smtp 无原生 context 支持，用 goroutine + select 模拟）。
func SendEmail(cfg *EmailConfig, a *proto.Alert) error {
	if !cfg.Enabled() || a == nil {
		return nil
	}
	recipients := strings.Split(cfg.To, ",")
	for i := range recipients {
		recipients[i] = strings.TrimSpace(recipients[i])
	}
	subject := fmt.Sprintf("[OpsMesh 告警][%s] %s", a.Severity, a.DeviceID)
	body := fmt.Sprintf(
		"告警ID: %s\n严重级别: %s\n设备: %s\nAgent: %s\n时间: %s\n\n消息:\n%s",
		a.AlertID, a.Severity, a.DeviceID, a.AgentID,
		a.CreatedAt.Format("2006-01-02 15:04:05"),
		a.Message,
	)
	msg := buildRFC822(cfg.From, recipients, subject, body)
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	var auth smtp.Auth
	if cfg.User != "" {
		auth = smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- smtp.SendMail(addr, auth, cfg.From, recipients, []byte(msg))
	}()
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("notify: send email: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("notify: send email timeout (%s)", addr)
	}
}

// sanitizeEmailField 移除邮件字段中的 CR/LF，防止邮件头注入
// （如 subject 含 "\r\nBcc: attacker@x.com" 会注入 Bcc 头）。
// 告警的 DeviceID/Message 来自设备上报，可能含恶意换行，故 subject/body 均需转义。
func sanitizeEmailField(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

// buildRFC822 构造 RFC 822 邮件正文（含必要头：From/To/Subject/MIME/Content-Type）。
// subject 与 body 经 sanitizeEmailField 转义，移除 CR/LF 防止邮件头注入。
func buildRFC822(from string, to []string, subject, body string) string {
	subject = sanitizeEmailField(subject)
	body = sanitizeEmailField(body)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(body)
	return buf.String()
}

// ============================================================================
// 多通道统一推送
// ============================================================================

// Channels 多通道配置（Webhook + Email）。任一通道配置缺失时自动跳过。
type Channels struct {
	NotifierType string       // Webhook 显式类型（generic/feishu/dingtalk）；URL 无法识别域名时回退到此值
	WebhookURL   string       // Webhook URL（空=关闭 Webhook 通道）
	Email        *EmailConfig // 邮件通道配置（nil 或 !Enabled=关闭邮件通道）
}

// Push 通过所有已配置通道推送告警。任一通道失败不影响其他通道，所有错误聚合返回。
// 无任何通道配置时返回 nil（静默跳过）。
func (c *Channels) Push(a *proto.Alert) error {
	if a == nil {
		return nil
	}
	var errs []string
	// Webhook 推送。
	if c.WebhookURL != "" {
		if err := PostWebhook(c.NotifierType, c.WebhookURL, a); err != nil {
			errs = append(errs, fmt.Sprintf("webhook: %v", err))
		}
	}
	// 邮件推送。
	if c.Email != nil && c.Email.Enabled() {
		if err := SendEmail(c.Email, a); err != nil {
			errs = append(errs, fmt.Sprintf("email: %v", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("notify: push errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ============================================================================
// Notifier：多渠道 + 模板 + 重试 + 去重 集成
// ============================================================================

// Notifier 通知管理器：集成多渠道推送、模板渲染、重试策略、消息去重。
//
// 设计：
//   - channels：已配置的渠道列表（Channel 接口实例）；空=无渠道，Notify 静默返回 nil。
//   - templates：模板存储；Notify 可按 templateID 渲染消息（NotifyWithTemplate）。
//   - dedup：去重器；nil=关闭去重。IsDuplicate 返回 true 时 Notify 跳过发送。
//   - retry：重试策略；nil=不重试（单次发送）。
//   - secretProvider：密钥提供者；nil=不启用密钥外置，BuildChannel 退化为明文构造。
//
// 并发安全：channels 在构造后不变（启动期一次性注入）；templates/dedup 内部自带互斥。
type Notifier struct {
	channels       []Channel              // 已配置渠道列表
	templates      *TemplateStore         // 模板存储（nil=无模板）
	dedup          *Deduplicator          // 去重器（nil=关闭去重）
	retry          *RetryPolicy           // 重试策略（nil=不重试）
	secretProvider secrets.SecretProvider // 密钥提供者（nil=不启用密钥外置）
}

// NotifierOption Notifier 构造选项（函数选项模式）。
type NotifierOption func(*Notifier)

// WithChannels 注入渠道列表。
func WithChannels(chs ...Channel) NotifierOption {
	return func(n *Notifier) { n.channels = append(n.channels, chs...) }
}

// WithTemplates 注入模板存储。
func WithTemplates(t *TemplateStore) NotifierOption {
	return func(n *Notifier) { n.templates = t }
}

// WithDedup 启用去重（ttl <= 0 时使用默认 5 分钟）。
func WithDedup(ttl time.Duration) NotifierOption {
	return func(n *Notifier) { n.dedup = NewDeduplicator(ttl) }
}

// WithRetry 启用重试（policy 为 nil 时使用 DefaultRetryPolicy）。
func WithRetry(policy *RetryPolicy) NotifierOption {
	return func(n *Notifier) {
		if policy == nil {
			policy = &DefaultRetryPolicy
		}
		n.retry = policy
	}
}

// WithSecretProvider 注入密钥提供者。
// provider 为 nil 时无操作（保持向后兼容，BuildChannel 退化为明文构造）。
// 注入后 BuildChannel 会用 WithSecret 版本构造渠道，解析 ${key} 格式密钥引用。
func WithSecretProvider(provider secrets.SecretProvider) NotifierOption {
	return func(n *Notifier) { n.secretProvider = provider }
}

// NewNotifier 构造 Notifier。默认无渠道、无模板、无去重、无重试（通过选项注入）。
func NewNotifier(opts ...NotifierOption) *Notifier {
	n := &Notifier{}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

// AddChannel 追加渠道（运行期动态添加，如热加载配置）。
func (n *Notifier) AddChannel(ch Channel) {
	if ch != nil {
		n.channels = append(n.channels, ch)
	}
}

// Channels 返回已配置渠道列表（只读，修改不影响内部）。
func (n *Notifier) Channels() []Channel {
	return n.channels
}

// Templates 返回模板存储（可能为 nil）。
func (n *Notifier) Templates() *TemplateStore {
	return n.templates
}

// SecretProvider 返回已注入的密钥提供者（可能为 nil）。
// 调用方（如 controlplane.buildChannel）据此决定用 WithSecret 版本还是原版构造渠道。
func (n *Notifier) SecretProvider() secrets.SecretProvider {
	return n.secretProvider
}

// BuildChannel 按渠道配置构造 Channel 实例。
// secretProvider 非空时用 NewChannelWithSecret 解析 ${key} 密钥引用；
// 为空时退化为 NewChannel（明文构造，向后兼容）。
// 用于将 Notifier 的 secretProvider 透传到渠道构造流程，避免调用方单独持有 provider。
func (n *Notifier) BuildChannel(cfg ChannelConfig) (Channel, error) {
	if n.secretProvider != nil {
		return NewChannelWithSecret(cfg, n.secretProvider)
	}
	return NewChannel(cfg)
}

// Notify 通过所有渠道推送消息。集成去重与重试。
//
// 行为：
//   - msg 为 nil → 静默返回 nil
//   - 去重启用且 IsDuplicate(msg)=true → 跳过发送，返回 nil
//   - 无渠道 → 静默返回 nil
//   - 每个渠道独立发送（失败不影响其他渠道）；重试启用时按 RetryPolicy 重试
//   - 所有渠道失败 → 聚合错误返回；部分失败 → 聚合错误返回；全部成功 → nil
func (n *Notifier) Notify(msg *Message) error {
	if msg == nil {
		return nil
	}
	// 去重检查（滑动窗口语义）。
	if n.dedup != nil && n.dedup.IsDuplicate(msg) {
		return nil
	}
	return n.sendToAll(msg)
}

// NotifyWithTemplate 按 templateID 渲染模板后推送。
//
// 渲染失败 → 返回 error（不发送）。
// 渲染成功 → 走 Notify 流程（去重 + 多渠道 + 重试）。
func (n *Notifier) NotifyWithTemplate(templateID string, data interface{}) error {
	if n.templates == nil {
		return fmt.Errorf("notify: no template store configured")
	}
	msg, err := n.templates.RenderByID(templateID, data)
	if err != nil {
		return err
	}
	return n.Notify(msg)
}

// sendToAll 向所有渠道发送消息（带可选重试）。错误聚合返回。
func (n *Notifier) sendToAll(msg *Message) error {
	if len(n.channels) == 0 {
		return nil
	}
	var errs []string
	for i, ch := range n.channels {
		var err error
		if n.retry != nil {
			err = SendWithRetry(ch, msg, n.retry)
		} else {
			err = ch.Send(msg)
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("channel[%d]: %v", i, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("notify: %s", strings.Join(errs, "; "))
	}
	return nil
}

// CleanupDedup 触发去重器清理过期条目。去重未启用时返回 0。
// 应由调用方周期调用（如每分钟一次）防止内存泄漏。
func (n *Notifier) CleanupDedup() int {
	if n.dedup == nil {
		return 0
	}
	return n.dedup.Cleanup()
}

// AlertToMessage 将 proto.Alert 转换为通用 Message（便于走 Notifier.Notify 流程）。
//
// 转换规则：
//   - Title: "[OpsMesh][<severity>] <deviceID>"
//   - Body: 渲染告警字段为 markdown 正文
//   - Format: "markdown"
//   - Severity/Source/Timestamp: 从 alert 提取
//   - Data: 原 alert 指针（供渠道定制渲染）
func AlertToMessage(a *proto.Alert) *Message {
	if a == nil {
		return nil
	}
	return &Message{
		Title:     fmt.Sprintf("[OpsMesh][%s] %s", a.Severity, a.DeviceID),
		Body:      fmt.Sprintf("**严重级别**: %s\n**设备**: %s\n**Agent**: %s\n**时间**: %s\n\n%s", a.Severity, a.DeviceID, a.AgentID, a.CreatedAt.Format("2006-01-02 15:04:05"), a.Message),
		Format:    "markdown",
		Severity:  a.Severity,
		Source:    a.DeviceID,
		Timestamp: a.CreatedAt,
		Data:      a,
	}
}
