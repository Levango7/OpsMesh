// Package logstore provides the log storage backend abstraction and in-memory implementation.
// This is a local copy of the OpsMesh logstore interfaces for the log-svc microservice.
package logstore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// LogStore is the backend abstraction for log storage.
// Memory / SQL 可写；Elasticsearch / Loki 只读（日志由采集器直推，见 SupportsAppend）。
type LogStore interface {
	Append(ctx context.Context, e *Entry) error
	Query(ctx context.Context, q Query) ([]Entry, error)
	Close() error
}

// maxQueryLimit is the hard limit for single query results.
const maxQueryLimit = 1000

// ErrAppendUnsupported 表示该后端按设计不接受 OpsMesh 侧写入（日志由 filebeat / promtail /
// fluent-bit 等采集器直推）。它必须是**显式错误**而不是 return nil：
// 返回 nil 会让调用方以为写入成功，agent 日志与任务输出就变成静默丢失。
var ErrAppendUnsupported = errors.New(
	"该后端不接受 OpsMesh 侧写入：Elasticsearch/Loki 的日志须由 filebeat/promtail/fluent-bit 等采集器直推；" +
		"若需 OpsMesh 写入日志（agent 上报 / 任务输出），请把 log-backend 配成 memory 或 sql")

// appendUnsupported 由"只读"后端实现。
type appendUnsupported interface{ AppendUnsupported() bool }

// SupportsAppend 报告该后端是否接受 OpsMesh 侧写入。
// 未知实现按"支持"处理，避免给 memory/sql 后端凭空加限制。
// 调用方应在写入入口**之前**问一次，而不是等 Append 逐条回错。
func SupportsAppend(ls LogStore) bool {
	a, ok := ls.(appendUnsupported)
	return !ok || !a.AppendUnsupported()
}

// Entry is a single log record.
type Entry struct {
	ID        int64     `json:"id"`
	TenantID  string    `json:"tenantID"`
	DeviceID  string    `json:"deviceID"`
	AgentID   string    `json:"agentID"`
	TaskID    string    `json:"taskID,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
}

// Query is the log search criteria.
type Query struct {
	TenantID string
	DeviceID string
	AgentID  string
	Level    string
	Source   string
	Keyword  string
	Q        string
	From     time.Time
	To       time.Time
	Limit    int
	Offset   int
}

// NewMemory creates an in-memory ring buffer backend.
func NewMemory(cap int) *MemoryLogStore {
	if cap <= 0 {
		cap = 5000
	}
	return &MemoryLogStore{buf: make([]Entry, 0, cap), cap: cap}
}

// MemoryLogStore is a concurrent-safe in-memory ring buffer.
type MemoryLogStore struct {
	mu  sync.RWMutex
	buf []Entry
	cap int
	seq int64
}

// Append writes a log entry, auto-assigning timestamp and ID.
func (m *MemoryLogStore) Append(_ context.Context, e *Entry) error {
	if e == nil {
		return nil
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	e.ID = m.seq
	cp := *e
	m.buf = append(m.buf, cp)
	if len(m.buf) > m.cap {
		m.buf = m.buf[len(m.buf)-m.cap:]
	}
	return nil
}

// Query searches log entries by criteria, returns newest-first.
func (m *MemoryLogStore) Query(_ context.Context, q Query) ([]Entry, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}
	if q.Offset < 0 {
		q.Offset = 0
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Entry, 0, limit)
	skipped := 0
	for i := len(m.buf) - 1; i >= 0; i-- {
		if !matchEntry(m.buf[i], q) {
			continue
		}
		if skipped < q.Offset {
			skipped++
			continue
		}
		out = append(out, m.buf[i])
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Close releases resources (noop for memory backend).
func (m *MemoryLogStore) Close() error { return nil }

// matchEntry checks if an entry matches the query criteria.
func matchEntry(e Entry, q Query) bool {
	if q.TenantID != "" && e.TenantID != q.TenantID {
		return false
	}
	if q.DeviceID != "" && e.DeviceID != q.DeviceID {
		return false
	}
	if q.AgentID != "" && e.AgentID != q.AgentID {
		return false
	}
	if q.Level != "" && !strings.EqualFold(e.Level, q.Level) {
		return false
	}
	if q.Source != "" && !strings.EqualFold(e.Source, q.Source) {
		return false
	}
	if q.Keyword != "" {
		if !strings.Contains(strings.ToLower(e.Message), strings.ToLower(q.Keyword)) {
			return false
		}
	}
	if !q.From.IsZero() && e.Timestamp.Before(q.From) {
		return false
	}
	if !q.To.IsZero() && e.Timestamp.After(q.To) {
		return false
	}
	return true
}

// NewMemoryWithIndex creates an in-memory backend with inverted index.
// The index is a no-op in this simplified version (Query uses linear scan).
func NewMemoryWithIndex(cap int) *MemoryLogStore {
	return NewMemory(cap)
}
