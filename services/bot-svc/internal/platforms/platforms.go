package platforms

import (
	"encoding/json"
	"fmt"
	"strings"
)

// asString/asMap：webhook payload 是异构 JSON，缺键/类型不符时取零值是既定语义——
// 用 helper 收敛 comma-ok 断言，替代散落的 `_` 丢弃（errcheck check-blank）。
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// Platform represents an enterprise messaging platform.
type Platform string

const (
	Wecom    Platform = "wecom"
	Feishu   Platform = "feishu"
	Slack    Platform = "slack"
	Dingtalk Platform = "dingtalk"
)

// WebhookPayload represents the generic incoming webhook payload.
type WebhookPayload struct {
	Platform Platform
	UserID   string
	Text     string
	Raw      map[string]any
}

// OutgoingMessage represents a formatted response message.
type OutgoingMessage struct {
	Platform Platform       `json:"-"`
	Content  string         `json:"content"`
	Format   string         `json:"format,omitempty"`
	Extra    map[string]any `json:"extra,omitempty"`
}

// Formatter defines platform-specific message formatting.
type Formatter interface {
	FormatResponse(result string) *OutgoingMessage
	ParseWebhook(body []byte) (*WebhookPayload, error)
	VerifyToken(token string, expected string) bool
}

// getFormatter returns the formatter for the given platform.
func getFormatter(p Platform) Formatter {
	switch p {
	case Wecom:
		return &wecomFormatter{}
	case Feishu:
		return &feishuFormatter{}
	case Slack:
		return &slackFormatter{}
	case Dingtalk:
		return &dingtalkFormatter{}
	default:
		return &slackFormatter{}
	}
}

// FormatResponse formats a response for the given platform.
func FormatResponse(p Platform, result string) *OutgoingMessage {
	return getFormatter(p).FormatResponse(result)
}

// ParseWebhook parses a webhook payload for the given platform.
func ParseWebhook(p Platform, body []byte) (*WebhookPayload, error) {
	return getFormatter(p).ParseWebhook(body)
}

// VerifyToken verifies the webhook token for the given platform.
func VerifyToken(p Platform, token, expected string) bool {
	return getFormatter(p).VerifyToken(token, expected)
}

// Wecom formatter

type wecomFormatter struct{}

func (f *wecomFormatter) FormatResponse(result string) *OutgoingMessage {
	lines := strings.Split(result, "\n")
	var mdLines []string
	for _, line := range lines {
		mdLines = append(mdLines, "> "+line)
	}
	return &OutgoingMessage{
		Platform: Wecom,
		Content:  strings.Join(mdLines, "\n"),
		Format:   "markdown",
	}
}

func (f *wecomFormatter) ParseWebhook(body []byte) (*WebhookPayload, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid wecom payload: %w", err)
	}
	text := asString(raw["text"])
	userID := asString(raw["userid"])
	return &WebhookPayload{
		Platform: Wecom,
		UserID:   userID,
		Text:     text,
		Raw:      raw,
	}, nil
}

func (f *wecomFormatter) VerifyToken(token, expected string) bool {
	return token != "" && token == expected
}

// Feishu formatter

type feishuFormatter struct{}

func (f *feishuFormatter) FormatResponse(result string) *OutgoingMessage {
	return &OutgoingMessage{
		Platform: Feishu,
		Content:  result,
		Format:   "markdown",
	}
}

func (f *feishuFormatter) ParseWebhook(body []byte) (*WebhookPayload, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid feishu payload: %w", err)
	}
	event := asMap(raw["event"])
	message := asMap(event["message"])
	text := asString(message["text"])
	sender := asMap(event["sender"])
	senderID := asMap(sender["sender_id"])
	userID := asString(senderID["open_id"])
	return &WebhookPayload{
		Platform: Feishu,
		UserID:   userID,
		Text:     text,
		Raw:      raw,
	}, nil
}

func (f *feishuFormatter) VerifyToken(token, expected string) bool {
	return token != "" && token == expected
}

// Slack formatter

type slackFormatter struct{}

func (f *slackFormatter) FormatResponse(result string) *OutgoingMessage {
	return &OutgoingMessage{
		Platform: Slack,
		Content:  "```\n" + result + "\n```",
		Format:   "mrkdwn",
	}
}

func (f *slackFormatter) ParseWebhook(body []byte) (*WebhookPayload, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		// Slack sends URL-encoded form data; try as plain text
		text := string(body)
		if idx := strings.Index(text, "text="); idx >= 0 {
			value := text[idx+5:]
			if amp := strings.Index(value, "&"); amp >= 0 {
				value = value[:amp]
			}
			return &WebhookPayload{
				Platform: Slack,
				UserID:   "",
				Text:     value,
				Raw:      map[string]any{"text": value},
			}, nil
		}
		return nil, fmt.Errorf("invalid slack payload: %w", err)
	}
	text := asString(raw["text"])
	userID := asString(raw["user_id"])
	command := asString(raw["command"])
	if command != "" && text == "" {
		text = command + " " + text
	}
	return &WebhookPayload{
		Platform: Slack,
		UserID:   userID,
		Text:     text,
		Raw:      raw,
	}, nil
}

func (f *slackFormatter) VerifyToken(token, expected string) bool {
	return token != "" && token == expected
}

// Dingtalk formatter

type dingtalkFormatter struct{}

func (f *dingtalkFormatter) FormatResponse(result string) *OutgoingMessage {
	return &OutgoingMessage{
		Platform: Dingtalk,
		Content:  result,
		Format:   "markdown",
		Extra:    map[string]any{"msgtype": "markdown"},
	}
}

func (f *dingtalkFormatter) ParseWebhook(body []byte) (*WebhookPayload, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid dingtalk payload: %w", err)
	}
	text := asMap(raw["text"])
	content := asString(text["content"])
	userID := asString(raw["senderId"])
	return &WebhookPayload{
		Platform: Dingtalk,
		UserID:   userID,
		Text:     content,
		Raw:      raw,
	}, nil
}

func (f *dingtalkFormatter) VerifyToken(token, expected string) bool {
	return token != "" && token == expected
}
