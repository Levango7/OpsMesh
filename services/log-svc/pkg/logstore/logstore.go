// Package logstore provides the log storage backend abstraction and in-memory implementation.
// This is a local copy of the OpsMesh logstore interfaces for the log-svc microservice.
package logstore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// LogStore is the backend abstraction for log storage.
type LogStore interface {
	Append(ctx context.Context, e *Entry) error
	Query(ctx context.Context, q Query) ([]Entry, error)
	Close() error
}

// maxQueryLimit is the hard limit for single query results.
const maxQueryLimit = 1000

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

// validateQuery checks if a query is valid.
func validateQuery(q Query) error {
	if q.Limit < 0 {
		return fmt.Errorf("limit must be non-negative")
	}
	if q.Offset < 0 {
		return fmt.Errorf("offset must be non-negative")
	}
	return nil
}

// NewMemoryWithIndex creates an in-memory backend with inverted index.
// The index is a no-op in this simplified version (Query uses linear scan).
func NewMemoryWithIndex(cap int) *MemoryLogStore {
	return NewMemory(cap)
}
