package logstore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MemoryLogStore 内存环形缓冲实现（并发安全，O(1) 追加 / O(n) 检索）。
// MVP 默认后端；进程重启即丢，生产应切 SQL（数据本地化）。
//
// index 字段为可选倒排索引：通过 NewMemoryWithIndex 启用。
// 启用后 Append 同步加入索引（含环形裁剪同步移除），
// 全文本检索通过 SearchFullText 方法；Query 保持原有逻辑（向后兼容）。
type MemoryLogStore struct {
	mu    sync.RWMutex
	buf   []Entry
	cap   int
	seq   int64
	index *InvertedIndex // 可选倒排索引（nil 表示未启用）
}

// Append 写入一条日志，自动补时间戳与单调递增 ID，超出容量丢弃最旧。
// 若启用倒排索引，同步加入索引；环形裁剪时同步移除被丢弃的文档。
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
		dropped := m.buf[:len(m.buf)-m.cap]
		m.buf = m.buf[len(m.buf)-m.cap:]
		// 同步从倒排索引移除被丢弃的文档。
		if m.index != nil {
			for i := range dropped {
				m.index.Remove(dropped[i].ID)
			}
		}
	}
	// 同步加入倒排索引。
	if m.index != nil {
		m.index.Add(cp.ID, cp.Message)
	}
	return nil
}

// Query 按条件检索；从最新到最旧遍历，便于“最近 N 条”。
// 返回切片按时间倒序（最新在前）。
//
// 当 q.Q 非空时，先解析为查询 AST（KQL/Lucene 风格），并优先于 Keyword：
//   - 基础条件（TenantID/DeviceID/AgentID/Level/Source/From/To）仍由 matchEntry 过滤
//   - 结构化条件由 AST 的 Match 方法逐条过滤
//   - Keyword 被忽略（Q 优先），避免对 message 重复过滤
//
// 当 q.Q 为空时，保持原有 Keyword LIKE 行为（向后兼容）。
//
// ParseQuery 失败时返回包装错误，handler 层应映射为 400 Bad Request。
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

	// 结构化查询语法：q.Q 非空时解析为 AST，并清空 Keyword（Q 优先）。
	var qnode QueryNode
	if q.Q != "" {
		var err error
		qnode, err = ParseQuery(q.Q)
		if err != nil {
			return nil, fmt.Errorf("解析结构化查询失败: %w", err)
		}
		// Q 优先于 Keyword，清空 Keyword 以避免 matchEntry 重复过滤 message。
		q.Keyword = ""
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
		// 结构化查询 AST 过滤（基础条件已由 matchEntry 处理）。
		if qnode != nil && !qnode.Match(&m.buf[i]) {
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

// SearchFullText 全文本检索（需启用倒排索引，即通过 NewMemoryWithIndex 构造）。
//
// 支持六种搜索模式（按优先级依次判断，互斥）：
//   - Phrase：短语查询（位置连续）
//   - And：布尔 AND（同时包含所有 term）
//   - Or：布尔 OR（包含任一 term）
//   - Not：布尔 NOT（不包含此 term）
//   - Wildcard：通配符查询（* 任意序列，? 单字符）
//   - Term：单 term 搜索
//
// 返回结果按 TF-IDF 降序排列，并经 Base 基础条件（TenantID/DeviceID 等）过滤。
// 未启用索引时返回 ErrIndexDisabled。
func (m *MemoryLogStore) SearchFullText(_ context.Context, q FullTextQuery) ([]Entry, error) {
	if m.index == nil {
		return nil, ErrIndexDisabled
	}
	// 按优先级选择搜索模式。
	var docIDs []int64
	switch {
	case q.Phrase != "":
		docIDs = m.index.SearchPhrase(q.Phrase)
	case len(q.And) > 0:
		docIDs = m.index.SearchAnd(q.And)
	case len(q.Or) > 0:
		docIDs = m.index.SearchOr(q.Or)
	case q.Not != "":
		docIDs = m.index.SearchNot(q.Not)
	case q.Wildcard != "":
		docIDs = m.index.SearchWildcard(q.Wildcard)
	case q.Term != "":
		docIDs = m.index.Search(q.Term)
	default:
		return nil, ErrEmptyQuery
	}
	if len(docIDs) == 0 {
		return []Entry{}, nil
	}
	// 基础条件过滤：忽略 Base.Keyword 与 Base.Q（文本检索由本结构字段驱动）。
	base := q.Base
	base.Keyword = ""
	base.Q = ""
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	// 建 id -> entry 映射，按 TF-IDF 排序的 docIDs 顺序取命中。
	entryMap := make(map[int64]Entry, len(m.buf))
	for i := range m.buf {
		entryMap[m.buf[i].ID] = m.buf[i]
	}
	out := make([]Entry, 0, limit)
	for _, id := range docIDs {
		e, ok := entryMap[id]
		if !ok {
			continue
		}
		if !matchEntry(e, base) {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

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
