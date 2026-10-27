// Package aigateway 上下文压缩器
package aigateway

import (
	"fmt"
	"strings"
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
	remaining := c.maxTokens - systemTokens - 500 // 预留输出空间

	// 保留最近的对话 (从后向前)
	var kept []Message
	var early []Message
	tokens := 0
	for i := len(chatMsgs) - 1; i >= 0; i-- {
		msgTokens := estimateMessageTokens(chatMsgs[i])
		if tokens+msgTokens <= remaining {
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
	if len(summary) > 500 {
		summary = summary[:500] + "..."
	}
	return summary
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

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
