// Package agent 记忆系统
package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// MemoryEntry 记忆条目
type MemoryEntry struct {
	SessionID string                 `json:"sessionId"`
	Input     string                 `json:"input"`
	Output    string                 `json:"output"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// MemoryManager 记忆管理器
type MemoryManager struct {
	mu            sync.RWMutex
	shortTerm     map[string][]MemoryEntry // 租户作用域的会话键 -> entries
	longTerm      map[string][]MemoryEntry // userID -> entries
	teamKnowledge map[string][]MemoryEntry // teamID -> entries
	maxShort      int
	maxLong       int
}

// sessionKey 给 sessionId 加上租户前缀。
// sessionId 由客户端自报，只用它做键 ⇒ 猜到一个号就能读到、甚至删掉别的租户的
// 会话记忆（含告警上下文、设备信息这类敏感内容）。租户前缀由**已验签**的上下文提供。
func sessionKey(tenantID, sessionID string) string {
	return tenantID + "\x00" + sessionID
}

func NewMemoryManager() *MemoryManager {
	return &MemoryManager{
		shortTerm:     make(map[string][]MemoryEntry),
		longTerm:      make(map[string][]MemoryEntry),
		teamKnowledge: make(map[string][]MemoryEntry),
		maxShort:      20,  // 短期记忆最多 20 轮
		maxLong:       100, // 长期记忆最多 100 条
	}
}

// GetShortTerm 获取短期记忆 (滑动窗口)，按租户作用域取。
func (m *MemoryManager) GetShortTerm(tenantID, sessionID string) []MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := m.shortTerm[sessionKey(tenantID, sessionID)]
	if len(entries) > m.maxShort {
		entries = entries[len(entries)-m.maxShort:]
	}
	out := make([]MemoryEntry, len(entries))
	copy(out, entries)
	return out
}

// AddShortTerm 添加短期记忆
func (m *MemoryManager) AddShortTerm(tenantID, sessionID, input, output string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := sessionKey(tenantID, sessionID)
	entry := MemoryEntry{
		SessionID: sessionID,
		Input:     input,
		Output:    output,
		Timestamp: time.Now(),
	}
	m.shortTerm[key] = append(m.shortTerm[key], entry)

	// 滑动窗口：超过 maxShort 时移除最早的
	if len(m.shortTerm[key]) > m.maxShort*2 {
		m.shortTerm[key] = m.shortTerm[key][m.maxShort:]
	}
}

// GetLongTerm 获取长期记忆
func (m *MemoryManager) GetLongTerm(userID string) []MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := m.longTerm[userID]
	out := make([]MemoryEntry, len(entries))
	copy(out, entries)
	return out
}

// AddLongTerm 添加长期记忆
func (m *MemoryManager) AddLongTerm(userID string, entry MemoryEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry.Timestamp = time.Now()
	m.longTerm[userID] = append(m.longTerm[userID], entry)

	if len(m.longTerm[userID]) > m.maxLong {
		m.longTerm[userID] = m.longTerm[userID][len(m.longTerm[userID])-m.maxLong:]
	}
}

// GetTeamKnowledge 获取团队知识
func (m *MemoryManager) GetTeamKnowledge(teamID string) []MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := m.teamKnowledge[teamID]
	out := make([]MemoryEntry, len(entries))
	copy(out, entries)
	return out
}

// AddTeamKnowledge 添加团队知识
func (m *MemoryManager) AddTeamKnowledge(teamID string, entry MemoryEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry.Timestamp = time.Now()
	m.teamKnowledge[teamID] = append(m.teamKnowledge[teamID], entry)
}

// ClearSession 清除会话记忆（租户作用域内）
func (m *MemoryManager) ClearSession(tenantID, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.shortTerm, sessionKey(tenantID, sessionID))
}

// SummarizeSession 生成会话摘要 (用于长期记忆)
func (m *MemoryManager) SummarizeSession(tenantID, sessionID string) string {
	entries := m.GetShortTerm(tenantID, sessionID)
	if len(entries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("会话摘要:\n")
	for _, e := range entries {
		fmt.Fprintf(&sb, "- Q: %s\n  A: %s\n", truncate(e.Input, 80), truncate(e.Output, 120))
	}
	return sb.String()
}

// truncate 按字节上限截断并在 UTF-8 字符边界回退：会话摘要里几乎都是中文，
// 硬切会把多字节字符切成非法序列（写进 LLM 上下文或日志后表现为乱码/替换符）。
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
