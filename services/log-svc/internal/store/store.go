package store

import (
	"sync"
	"time"
)

// LogEntry represents a log entry in the store.
type LogEntry struct {
	ID        string
	TenantID  string
	DeviceID  string
	AgentID   string
	TaskID    string
	Level     string
	Source    string
	Message   string
	Timestamp time.Time
}

// AuditLog represents an audit log entry.
type AuditLog struct {
	ID        string
	TenantID  string
	UserID    string
	Action    string
	Target    string
	Detail    string
	Timestamp time.Time
}

// LogStore is the interface for log persistence.
type LogStore interface {
	// Agent logs
	AppendAgentLog(entry *LogEntry) error
	ListAgentLogs(tenantID, agentID, level string, limit int) []*LogEntry
	// Audit logs
	AppendAuditLog(entry *AuditLog) error
	ListAuditLogs(tenantID string, limit int) []*AuditLog
}

// MemoryStore is an in-memory implementation of LogStore.
type MemoryStore struct {
	mu        sync.RWMutex
	agentLogs []*LogEntry
	auditLogs []*AuditLog
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		agentLogs: make([]*LogEntry, 0),
		auditLogs: make([]*AuditLog, 0),
	}
}

// AppendAgentLog adds a log entry.
func (m *MemoryStore) AppendAgentLog(entry *LogEntry) error {
	if entry == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	m.agentLogs = append(m.agentLogs, entry)
	return nil
}

// ListAgentLogs returns agent logs filtered by tenant/agent/level.
func (m *MemoryStore) ListAgentLogs(tenantID, agentID, level string, limit int) []*LogEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*LogEntry, 0)
	for i := len(m.agentLogs) - 1; i >= 0; i-- {
		e := m.agentLogs[i]
		if tenantID != "" && e.TenantID != tenantID {
			continue
		}
		if agentID != "" && e.AgentID != agentID {
			continue
		}
		if level != "" && e.Level != level {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// AppendAuditLog adds an audit log entry.
func (m *MemoryStore) AppendAuditLog(entry *AuditLog) error {
	if entry == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	m.auditLogs = append(m.auditLogs, entry)
	return nil
}

// ListAuditLogs returns audit logs filtered by tenant.
func (m *MemoryStore) ListAuditLogs(tenantID string, limit int) []*AuditLog {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*AuditLog, 0)
	for i := len(m.auditLogs) - 1; i >= 0; i-- {
		e := m.auditLogs[i]
		if tenantID != "" && e.TenantID != tenantID {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
