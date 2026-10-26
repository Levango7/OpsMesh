// Package agent 记忆系统
package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// MemoryEntry 记忆条目
type MemoryEntry struct {
	SessionID string    `json:"sessionId"`
	Input     string    `json:"input"`
	Output    string    `json:"output"`
	Timestamp time.Time `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// MemoryManager 记忆管理器
type MemoryManager struct {
	mu          sync.RWMutex
	shortTerm   map[string][]MemoryEntry // sessionID -> entries
	longTerm    map[string][]MemoryEntry // userID -> entries
	teamKnowledge map[string][]MemoryEntry // teamID -> entries
	maxShort    int
	maxLong     int
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

// GetShortTerm 获取短期记忆 (滑动窗口)
func (m *MemoryManager) GetShortTerm(sessionID string) []MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := m.shortTerm[sessionID]
	if len(entries) > m.maxShort {
		entries = entries[len(entries)-m.maxShort:]
	}
	out := make([]MemoryEntry, len(entries))
	copy(out, entries)
	return out
}

// AddShortTerm 添加短期记忆
func (m *MemoryManager) AddShortTerm(sessionID, input, output string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry := MemoryEntry{
		SessionID: sessionID,
		Input:     input,
		Output:    output,
		Timestamp: time.Now(),
	}
	m.shortTerm[sessionID] = append(m.shortTerm[sessionID], entry)

	// 滑动窗口：超过 maxShort 时移除最早的
	if len(m.shortTerm[sessionID]) > m.maxShort*2 {
		m.shortTerm[sessionID] = m.shortTerm[sessionID][m.maxShort:]
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

// ClearSession 清除会话记忆
func (m *MemoryManager) ClearSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.shortTerm, sessionID)
}

// SummarizeSession 生成会话摘要 (用于长期记忆)
func (m *MemoryManager) SummarizeSession(sessionID string) string {
	entries := m.GetShortTerm(sessionID)
	if len(entries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("会话摘要:\n")
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("- Q: %s\n  A: %s\n", truncate(e.Input, 80), truncate(e.Output, 120)))
	}
	return sb.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
