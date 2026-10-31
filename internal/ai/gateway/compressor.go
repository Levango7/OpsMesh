// Package aigateway 上下文压缩器
package aigateway

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ContextCompressor 上下文压缩器
type ContextCompressor struct {
	maxTokens int
}

func NewContextCompressor(maxTokens int) *ContextCompressor {
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	return &ContextCompressor{maxTokens: maxTokens}
}

// Compress 压缩消息列表，保持在 token 预算内
// 策略：1) 保留系统消息 2) 保留最近 N 轮 3) 早期消息摘要化 4) 截断超长内容
func (c *ContextCompressor) Compress(messages []Message) []Message {
	if estimateTokens(messages) <= c.maxTokens {
		return messages
	}

	// 分离系统消息和对话消息
	var systemMsgs []Message
	var chatMsgs []Message
	for _, m := range messages {
		if m.Role == RoleSystem {
			systemMsgs = append(systemMsgs, m)
		} else {
			chatMsgs = append(chatMsgs, m)
		}
	}

	// 估算系统消息 token
	systemTokens := estimateTokens(systemMsgs)
	// 输出预留按预算比例取并夹住 500 上限：此前硬编码 500，预算小于 500+系统消息时
	// remaining 直接变负，一条对话都留不下（连用户最新那句都被摘要吞掉）。
	reserve := c.maxTokens / 4
	if reserve > 500 {
		reserve = 500
	}
	remaining := c.maxTokens - systemTokens - reserve
	if remaining < 0 {
		remaining = 0
	}

	// 保留最近的对话 (从后向前)
	var kept []Message
	var early []Message
	tokens := 0
	for i := len(chatMsgs) - 1; i >= 0; i-- {
		msgTokens := estimateMessageTokens(chatMsgs[i])
		if tokens+msgTokens <= remaining || len(kept) == 0 {
			// 最近一条无条件保留：丢了它，模型就答不上当前这一问。
			kept = append([]Message{chatMsgs[i]}, kept...)
			tokens += msgTokens
		} else {
			early = append([]Message{chatMsgs[i]}, early...)
		}
	}

	// 如果有早期消息，生成摘要
	if len(early) > 0 {
		summary := c.summarize(early)
		summaryMsg := Message{
			Role:    RoleSystem,
			Content: "早期对话摘要: " + summary,
		}
		kept = append([]Message{summaryMsg}, kept...)
	}

	return append(systemMsgs, kept...)
}

// summarize 生成早期对话摘要
func (c *ContextCompressor) summarize(msgs []Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&sb, "[%s] %s\n", m.Role, truncateStr(m.Content, 100))
	}
	summary := sb.String()
	return truncateStr(summary, 500)
}

func estimateTokens(messages []Message) int {
	total := 0
	for _, m := range messages {
		total += estimateMessageTokens(m)
	}
	return total
}

func estimateMessageTokens(m Message) int {
	// 简化估算：每 4 个字符约 1 token
	return len(m.Content)/4 + 4 // +4 for role overhead
}

// truncateStr 按字节上限截断，但在 UTF-8 字符边界回退：中文对话被切在半字符上，
// 送进模型的上下文与落盘日志都会带非法字符。
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
