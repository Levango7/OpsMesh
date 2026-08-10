package logstore

import (
	"context"
	"strings"
	"sync"
	"time"
)


// MemoryLogStore 内存环形缓冲实现（并发安全，O(1) 追加 / O(n) 检索）。
// MVP 默认后端；进程重启即丢，生产应切 SQL（U-04 数据本地化）。
type MemoryLogStore struct {
	mu  sync.RWMutex
	buf []Entry
	cap int
	seq int64
}

// Append 写入一条日志，自动补时间戳与单调递增 ID，超出容量丢弃最旧。
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
	cp := *e
	cp.ID = m.seq
	m.buf = append(m.buf, cp)
	if len(m.buf) > m.cap {
		// 环形裁剪：丢弃超出部分（保留最新 cap 条）。
		m.buf = m.buf[len(m.buf)-m.cap:]
	}
	return nil
}

// Query 按条件检索；从最新到最旧遍历，便于“最近 N 条”。
// 返回切片按时间倒序（最新在前）。
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
	// 从最新到最旧遍历；命中条数先跳过 Offset 条，再收集 Limit 条。
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

// Close 内存后端无可释放资源。
func (m *MemoryLogStore) Close() error { return nil }

// matchEntry 判定单条日志是否命中查询条件。
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
		hay := strings.ToLower(e.Message)
		kw := strings.ToLower(q.Keyword)
		if !strings.Contains(hay, kw) {
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
