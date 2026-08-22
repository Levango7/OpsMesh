// Package notify 通知渠道扩展。
//
// 本文件实现统一的 Channel 接口与 5 个具体渠道：
//   - DingTalkChannel：钉钉群机器人 webhook（可选加签 secret）
//   - WeChatWorkChannel：企业微信群机器人 webhook
//   - FeishuChannel：飞书群机器人 webhook（可选加签 secret）
//   - SlackChannel：Slack incoming webhook
//   - EmailChannel：邮件 SMTP
//
// 设计原则：
//   - 所有 webhook 调用复用 webhook.go 的 postJSON（10s 超时、JSON body、status<300 视为成功）。
//   - 钉钉/飞书加签 secret 非空时按官方算法计算签名并拼接到 URL（timestamp&sign）。
//   - Email 复用 notify.go 的 buildRFC822 + net/smtp.SendMail（10s 超时）。
//   - Message 同时兼容通用通知（Title/Body）与告警语义（Severity/Source/Timestamp）。
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"strings"
	"time"

	"opsmesh/internal/secrets"
)

// ============================================================================
// 统一消息模型与渠道接口
// ============================================================================

// Message 通用通知消息。所有渠道的 Send 入参，解耦具体告警/任务/系统事件来源。
//
// 字段语义：
//   - Title：消息标题（如 "[OpsMesh 告警][critical] dev-1"）；模板渲染后填入。
//   - Body：消息正文（markdown/text/html，由 Format 决定渲染方式）。
//   - Format：正文格式（"markdown"/"text"/"html"）；渠道按自身能力选择渲染策略。
//   - Severity：严重级别（"critical"/"warning"/"info"）；用于 emoji/颜色选择，空=info。
//   - Source：消息来源标识（如设备 ID / agent ID / 任务 ID）。
//   - Timestamp：消息产生时间；渠道格式化时使用。
//   - Data：原始数据（如 *proto.Alert），供渠道做定制化渲染（如飞书卡片）。
type Message struct {
	Title     string      // 消息标题
	Body      string      // 消息正文
	Format    string      // markdown / text / html
	Severity  string      // critical / warning / info
	Source    string      // 来源标识（设备/agent/任务 ID）
	Timestamp time.Time   // 消息时间
	Data      interface{} // 原始数据（如 *proto.Alert），供渠道定制渲染
}

// Channel 通知渠道接口。所有渠道实现 Send 方法，由 Notifier 统一调度。
type Channel interface {
	// Send 将消息推送到目标渠道。失败时返回 error，由 RetryPolicy 决定是否重试。
	Send(msg *Message) error
}

// severityEmoji 按严重级别返回 emoji 前缀（critical=🔴，warning=⚠️，其他=ℹ️）。
func severityEmoji(sev string) string {
	switch sev {
	case "critical":
		return "🔴"
	case "warning":
		return "⚠️"
	default:
		return "ℹ️"
	}
}

// formatTime 格式化时间为 "2006-01-02 15:04:05"（零值返回当前时间）。
func formatTime(t time.Time) string {
	if t.IsZero() {
		return time.Now().Format("2006-01-02 15:04:05")
	}
	return t.Format("2006-01-02 15:04:05")
}

// ============================================================================
// 钉钉群机器人 webhook
// ============================================================================

// DingTalkChannel 钉钉群机器人 webhook 通道。
//
// 文档：https://open.dingtalk.com/document/robots/custom-robot-access
//
// 加签模式（secret 非空）：URL 拼接 &timestamp=<ts>&sign=<sign>，
// sign = base64(HMAC-SHA256(secret, ts+"\n"+secret))。
// 安全建议生产开启加签，防止 URL 泄露后被他人随意推送。
type DingTalkChannel struct {
	webhookURL string // 钉钉机器人 webhook URL（含 access_token）
	secret     string // 加签密钥（空=不加签，仅 access_token 模式）
}

// NewDingTalkChannel 构造钉钉渠道。webhookURL 为空时 Send 直接返回 nil（静默跳过）。
func NewDingTalkChannel(webhookURL, secret string) *DingTalkChannel {
	return &DingTalkChannel{webhookURL: webhookURL, secret: secret}
}

// Send 推送钉钉 markdown 消息。webhookURL 为空或 msg 为 nil 时静默返回 nil。
func (c *DingTalkChannel) Send(msg *Message) error {
	if c.webhookURL == "" || msg == nil {
		return nil
	}
	title := msg.Title
	if title == "" {
		title = "OpsMesh 通知"
	}
	text := msg.Body
	if text == "" {
		// Body 为空时按通用字段构造默认正文。
		text = fmt.Sprintf(
			"## %s %s\n\n- **级别**: %s\n- **来源**: %s\n- **时间**: %s",
			severityEmoji(msg.Severity), title, msg.Severity, msg.Source, formatTime(msg.Timestamp),
		)
	}
	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]interface{}{
			"title": fmt.Sprintf("%s %s", severityEmoji(msg.Severity), title),
			"text":  text,
		},
	}
	return postJSON(c.signedURL(), payload)
}

// signedURL 计算加签 URL。secret 为空时直接返回原 URL。
// 钉钉加签算法：timestamp=毫秒，stringToSign=timestamp+"\n"+secret，
// sign=base64(HMAC-SHA256(secret, stringToSign))，URL 追加 &timestamp=&sign=。
func (c *DingTalkChannel) signedURL() string {
	if c.secret == "" {
		return c.webhookURL
	}
	ts := fmt.Sprintf("%d", time.Now().UnixMilli())
	stringToSign := ts + "\n" + c.secret
	h := hmac.New(sha256.New, []byte(c.secret))
	h.Write([]byte(stringToSign))
	sign := base64.StdEncoding.EncodeToString(h.Sum(nil))
	sep := "&"
	if !strings.Contains(c.webhookURL, "?") {
		sep = "?"
	}
	return fmt.Sprintf("%s%stimestamp=%s&sign=%s", c.webhookURL, sep, ts, sign)
}

// ============================================================================
// 企业微信群机器人 webhook
// ============================================================================

// WeChatWorkChannel 企业微信群机器人 webhook 通道。
//
// 文档：https://developer.work.weixin.qq.com/document/path/91770
//
// 仅支持 markdown / text 两种 msgtype；本实现按 msg.Format 选择：
//   - Format="text" → text 消息（content=msg.Body）
//   - 其他 → markdown 消息（content=渲染后的 markdown 正文）
type WeChatWorkChannel struct {
	webhookURL string // 企业微信群机器人 webhook URL
}

// NewWeChatWorkChannel 构造企业微信渠道。webhookURL 为空时 Send 静默返回 nil。
func NewWeChatWorkChannel(webhookURL string) *WeChatWorkChannel {
	return &WeChatWorkChannel{webhookURL: webhookURL}
}

// Send 推送企业微信消息。webhookURL 为空或 msg 为 nil 时静默返回 nil。
func (c *WeChatWorkChannel) Send(msg *Message) error {
	if c.webhookURL == "" || msg == nil {
		return nil
	}
	title := msg.Title
	if title == "" {
		title = "OpsMesh 通知"
	}
	content := msg.Body
	if content == "" {
		content = fmt.Sprintf(
			"## %s %s\n\n> **级别**: %s\n> **来源**: %s\n> **时间**: %s",
			severityEmoji(msg.Severity), title, msg.Severity, msg.Source, formatTime(msg.Timestamp),
		)
	}
	var payload map[string]interface{}
	if msg.Format == "text" {
		payload = map[string]interface{}{
			"msgtype": "text",
			"text":    map[string]interface{}{"content": content},
		}
	} else {
		payload = map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]interface{}{
				"content": fmt.Sprintf("%s %s\n\n%s", severityEmoji(msg.Severity), title, content),
			},
		}
	}
	return postJSON(c.webhookURL, payload)
}

// ============================================================================
// 飞书群机器人 webhook
// ============================================================================

// FeishuChannel 飞书群机器人 webhook 通道。
//
// 文档：https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-components
//
// 加签模式（secret 非空）：payload 追加 timestamp 字段（秒）与 sign 字段，
// sign = base64(HMAC-SHA256(secret, ts+"\n"+secret))，ts 为秒级时间戳字符串。
type FeishuChannel struct {
	webhookURL string // 飞书群机器人 webhook URL
	secret     string // 加签密钥（空=不加签）
}

// NewFeishuChannel 构造飞书渠道。webhookURL 为空时 Send 静默返回 nil。
func NewFeishuChannel(webhookURL, secret string) *FeishuChannel {
	return &FeishuChannel{webhookURL: webhookURL, secret: secret}
}

// Send 推送飞书 interactive 卡片消息。webhookURL 为空或 msg 为 nil 时静默返回 nil。
func (c *FeishuChannel) Send(msg *Message) error {
	if c.webhookURL == "" || msg == nil {
		return nil
	}
	title := msg.Title
	if title == "" {
		title = "OpsMesh 通知"
	}
	content := msg.Body
	if content == "" {
		content = fmt.Sprintf(
			"**级别**: %s\n**来源**: %s\n**时间**: %s",
			msg.Severity, msg.Source, formatTime(msg.Timestamp),
		)
	}
	// 飞书卡片 header 颜色按级别选择：critical=red，warning=orange，其他=blue。
	template := "blue"
	switch msg.Severity {
	case "critical":
		template = "red"
	case "warning":
		template = "orange"
	}
	payload := map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"header": map[string]interface{}{
				"title": map[string]interface{}{
					"tag":     "plain_text",
					"content": fmt.Sprintf("%s %s", severityEmoji(msg.Severity), title),
				},
				"template": template,
			},
			"elements": []map[string]interface{}{
				{
					"tag":     "markdown",
					"content": content,
				},
			},
		},
	}
	// 加签：飞书要求 timestamp（秒）与 sign 同在 payload 顶层。
	if c.secret != "" {
		ts := fmt.Sprintf("%d", time.Now().Unix())
		stringToSign := ts + "\n" + c.secret
		h := hmac.New(sha256.New, []byte(c.secret))
		h.Write([]byte(stringToSign))
		sign := base64.StdEncoding.EncodeToString(h.Sum(nil))
		payload["timestamp"] = ts
		payload["sign"] = sign
	}
	return postJSON(c.webhookURL, payload)
}

// ============================================================================
// Slack incoming webhook
// ============================================================================

// SlackChannel Slack incoming webhook 通道。
//
// 文档：https://api.slack.com/messaging/webhooks
//
// channel 字段非空时在 payload 中指定目标频道（覆盖 webhook URL 默认频道）。
// 使用 Block Kit 格式：header block + section block（mrkdwn）。
type SlackChannel struct {
	webhookURL string // Slack incoming webhook URL
	channel    string // 目标频道（空=使用 webhook URL 默认频道）
}

// NewSlackChannel 构造 Slack 渠道。webhookURL 为空时 Send 静默返回 nil。
func NewSlackChannel(webhookURL, channel string) *SlackChannel {
	return &SlackChannel{webhookURL: webhookURL, channel: channel}
}

// Send 推送 Slack Block Kit 消息。webhookURL 为空或 msg 为 nil 时静默返回 nil。
func (c *SlackChannel) Send(msg *Message) error {
	if c.webhookURL == "" || msg == nil {
		return nil
	}
	title := msg.Title
	if title == "" {
		title = "OpsMesh 通知"
	}
	text := msg.Body
	if text == "" {
		text = fmt.Sprintf(
			"*级别*: %s\n*来源*: %s\n*时间*: %s",
			msg.Severity, msg.Source, formatTime(msg.Timestamp),
		)
	}
	payload := map[string]interface{}{
		"blocks": []map[string]interface{}{
			{
				"type": "header",
				"text": map[string]interface{}{
					"type": "plain_text",
					"text": fmt.Sprintf("%s %s", severityEmoji(msg.Severity), title),
				},
			},
			{
				"type": "section",
				"text": map[string]interface{}{
					"type": "mrkdwn",
					"text": text,
				},
			},
		},
	}
	if c.channel != "" {
		payload["channel"] = c.channel
	}
	return postJSON(c.webhookURL, payload)
}

// ============================================================================
// 邮件 SMTP 通道
// ============================================================================

// EmailChannel 邮件 SMTP 通道。
//
// 复用 notify.go 的 buildRFC822 + net/smtp.SendMail，10s 超时（goroutine + select 模拟）。
// Format="html" 时 Content-Type 设为 text/html，否则 text/plain。
type EmailChannel struct {
	smtpHost string   // SMTP 服务器地址（如 smtp.example.com）
	smtpPort int      // SMTP 端口（如 25/465/587）
	username string   // SMTP 用户名（空=匿名发送）
	password string   // SMTP 密码
	from     string   // 发件人地址
	to       []string // 收件人列表
}

// NewEmailChannel 构造邮件渠道。smtpHost 为空或端口 <=0 时 Send 静默返回 nil。
func NewEmailChannel(smtpHost string, smtpPort int, username, password, from string, to []string) *EmailChannel {
	return &EmailChannel{
		smtpHost: smtpHost,
		smtpPort: smtpPort,
		username: username,
		password: password,
		from:     from,
		to:       to,
	}
}

// Enabled 返回邮件通道是否已配置（必填字段非空）。
func (c *EmailChannel) Enabled() bool {
	return c != nil && c.smtpHost != "" && c.smtpPort > 0 && c.from != "" && len(c.to) > 0
}

// Send 通过 SMTP 发送邮件。配置缺失（!Enabled）或 msg 为 nil 时静默返回 nil。
func (c *EmailChannel) Send(msg *Message) error {
	if !c.Enabled() || msg == nil {
		return nil
	}
	subject := msg.Title
	if subject == "" {
		subject = "OpsMesh 通知"
	}
	body := msg.Body
	if body == "" {
		body = fmt.Sprintf(
			"级别: %s\n来源: %s\n时间: %s",
			msg.Severity, msg.Source, formatTime(msg.Timestamp),
		)
	}
	msgBytes := buildRFC822WithFormat(c.from, c.to, subject, body, msg.Format)
	addr := fmt.Sprintf("%s:%d", c.smtpHost, c.smtpPort)
	var auth smtp.Auth
	if c.username != "" {
		auth = smtp.PlainAuth("", c.username, c.password, c.smtpHost)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- smtp.SendMail(addr, auth, c.from, c.to, []byte(msgBytes))
	}()
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("notify: email channel send: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("notify: email channel timeout (%s)", addr)
	}
}

// buildRFC822WithFormat 构造 RFC 822 邮件正文，按 format 选择 Content-Type。
// format="html" → text/html；其他 → text/plain（默认）。
// subject 与 body 经 sanitizeEmailField 转义，防止邮件头注入。
func buildRFC822WithFormat(from string, to []string, subject, body, format string) string {
	subject = sanitizeEmailField(subject)
	// html 正文保留换行（HTML 渲染时换行无意义），仅转义 subject 的 CR/LF。
	if format != "html" {
		body = sanitizeEmailField(body)
	}
	contentType := "text/plain; charset=UTF-8"
	if format == "html" {
		contentType = "text/html; charset=UTF-8"
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	buf.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: %s\r\n", contentType)
	buf.WriteString("\r\n")
	buf.WriteString(body)
	return buf.String()
}

// ============================================================================
// 渠道工厂
// ============================================================================

// ChannelConfig 渠道配置（与 config.go 的 NotifyChannelConfig 解耦，notify 包内部使用）。
// 由 Notifier 通过 NewChannel 工厂转换为具体 Channel 实例。
type ChannelConfig struct {
	Type       string   // dingtalk / wechat / feishu / slack / email
	WebhookURL string   // webhook URL（dingtalk/wechat/feishu/slack 用）
	Secret     string   // 加签密钥（dingtalk/feishu 用）
	Channel    string   // Slack 频道（slack 用）
	SMTPHost   string   // SMTP 主机（email 用）
	SMTPPort   int      // SMTP 端口（email 用）
	Username   string   // SMTP 用户名（email 用）
	Password   string   // SMTP 密码（email 用）
	From       string   // 发件人（email 用）
	To         []string // 收件人列表（email 用）
}

// NewChannel 渠道工厂：按 type 构造具体 Channel 实例。
//
// 支持类型：dingtalk / wechat / feishu / slack / email。
// 未知类型返回 nil + error（启动期 fail-fast 由调用方决定是否忽略）。
// 必填字段缺失（如 email 缺 host）时返回 nil + error。
func NewChannel(cfg ChannelConfig) (Channel, error) {
	switch cfg.Type {
	case "dingtalk":
		return NewDingTalkChannel(cfg.WebhookURL, cfg.Secret), nil
	case "wechat":
		return NewWeChatWorkChannel(cfg.WebhookURL), nil
	case "feishu":
		return NewFeishuChannel(cfg.WebhookURL, cfg.Secret), nil
	case "slack":
		return NewSlackChannel(cfg.WebhookURL, cfg.Channel), nil
	case "email":
		ch := NewEmailChannel(cfg.SMTPHost, cfg.SMTPPort, cfg.Username, cfg.Password, cfg.From, cfg.To)
		if !ch.Enabled() {
			return nil, fmt.Errorf("notify: email channel config incomplete (host/port/from/to required)")
		}
		return ch, nil
	default:
		return nil, fmt.Errorf("notify: unknown channel type %q", cfg.Type)
	}
}

// ============================================================================
// 密钥外置构造函数
//
// 提供 WithSecret 后缀的渠道构造函数，支持从 SecretProvider 解析 ${key} 格式密钥引用。
// 设计原则：
//   - 向后兼容：provider 为 nil 或参数为明文时，行为与原版 NewXxxChannel 完全一致。
//   - 解析失败时返回 error（fail-fast），避免运行期用引用串当密钥导致鉴权失败。
//   - 仅解析密钥相关参数（webhookURL/secret），非敏感参数（如 Slack channel 名）不解析。
// ============================================================================

// NewDingTalkChannelWithSecret 构造钉钉渠道，支持密钥引用解析。
// webhookURL/secret 为 ${key} 格式时从 provider 解析，明文直接使用。
// provider 为 nil 时退化为明文直接使用（向后兼容）。
// 解析失败（如 provider 返回错误）时返回 error，避免误把引用串当密钥。
func NewDingTalkChannelWithSecret(webhookURL, secret string, provider secrets.SecretProvider) (*DingTalkChannel, error) {
	resolvedURL, err := secrets.ResolveSecret(webhookURL, provider)
	if err != nil {
		return nil, fmt.Errorf("notify: 解析钉钉 webhookURL 失败: %w", err)
	}
	resolvedSecret, err := secrets.ResolveSecret(secret, provider)
	if err != nil {
		return nil, fmt.Errorf("notify: 解析钉钉 secret 失败: %w", err)
	}
	return NewDingTalkChannel(resolvedURL, resolvedSecret), nil
}

// NewWeChatWorkChannelWithSecret 构造企业微信渠道，支持密钥引用解析。
// webhookURL 为 ${key} 格式时从 provider 解析，明文直接使用。
// provider 为 nil 时退化为明文直接使用（向后兼容）。
func NewWeChatWorkChannelWithSecret(webhookURL string, provider secrets.SecretProvider) (*WeChatWorkChannel, error) {
	resolvedURL, err := secrets.ResolveSecret(webhookURL, provider)
	if err != nil {
		return nil, fmt.Errorf("notify: 解析企业微信 webhookURL 失败: %w", err)
	}
	return NewWeChatWorkChannel(resolvedURL), nil
}

// NewFeishuChannelWithSecret 构造飞书渠道，支持密钥引用解析。
// webhookURL/secret 为 ${key} 格式时从 provider 解析，明文直接使用。
// provider 为 nil 时退化为明文直接使用（向后兼容）。
func NewFeishuChannelWithSecret(webhookURL, secret string, provider secrets.SecretProvider) (*FeishuChannel, error) {
	resolvedURL, err := secrets.ResolveSecret(webhookURL, provider)
	if err != nil {
		return nil, fmt.Errorf("notify: 解析飞书 webhookURL 失败: %w", err)
	}
	resolvedSecret, err := secrets.ResolveSecret(secret, provider)
	if err != nil {
		return nil, fmt.Errorf("notify: 解析飞书 secret 失败: %w", err)
	}
	return NewFeishuChannel(resolvedURL, resolvedSecret), nil
}

// NewSlackChannelWithSecret 构造 Slack 渠道，支持密钥引用解析。
// webhookURL 为 ${key} 格式时从 provider 解析，明文直接使用。
// channel 为非敏感参数（频道名），不参与解析，直接透传。
// provider 为 nil 时退化为明文直接使用（向后兼容）。
func NewSlackChannelWithSecret(webhookURL, channel string, provider secrets.SecretProvider) (*SlackChannel, error) {
	resolvedURL, err := secrets.ResolveSecret(webhookURL, provider)
	if err != nil {
		return nil, fmt.Errorf("notify: 解析 Slack webhookURL 失败: %w", err)
	}
	return NewSlackChannel(resolvedURL, channel), nil
}

// NewChannelWithSecret 渠道工厂（密钥外置版）：按 type 构造具体 Channel 实例，
// 对密钥相关字段（webhookURL/secret）通过 SecretProvider 解析 ${key} 引用。
//
// 与 NewChannel 的差异：密钥参数经 ResolveSecret 解析；非敏感参数（Slack channel）直接透传。
// provider 为 nil 时退化为 NewChannel（向后兼容）。
// email 渠道的 password 也参与解析（SMTP 密码属于敏感信息）。
func NewChannelWithSecret(cfg ChannelConfig, provider secrets.SecretProvider) (Channel, error) {
	switch cfg.Type {
	case "dingtalk":
		return NewDingTalkChannelWithSecret(cfg.WebhookURL, cfg.Secret, provider)
	case "wechat", "wecom":
		return NewWeChatWorkChannelWithSecret(cfg.WebhookURL, provider)
	case "feishu", "lark":
		return NewFeishuChannelWithSecret(cfg.WebhookURL, cfg.Secret, provider)
	case "slack":
		return NewSlackChannelWithSecret(cfg.WebhookURL, cfg.Channel, provider)
	case "email":
		// SMTP 密码属于敏感信息，支持密钥引用解析。
		resolvedPass, err := secrets.ResolveSecret(cfg.Password, provider)
		if err != nil {
			return nil, fmt.Errorf("notify: 解析 email password 失败: %w", err)
		}
		ch := NewEmailChannel(cfg.SMTPHost, cfg.SMTPPort, cfg.Username, resolvedPass, cfg.From, cfg.To)
		if !ch.Enabled() {
			return nil, fmt.Errorf("notify: email channel config incomplete (host/port/from/to required)")
		}
		return ch, nil
	default:
		return nil, fmt.Errorf("notify: unknown channel type %q", cfg.Type)
	}
}
