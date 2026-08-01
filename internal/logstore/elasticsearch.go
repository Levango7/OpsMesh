// Package logstore Elasticsearch 后端适配（M4-4B）。
//
// Elasticsearch 是分布式搜索分析引擎，使用 DSL（JSON）查询。
// 本适配层仅实现查询接口（Query）：将 OpsMesh Query 翻译为 ES bool query DSL，
// 调用 POST /<index>/_search HTTP API，解析 hits 为 Entry 切片。
// Append 为 noop——日志由 agent 通过 filebeat / fluent-bit / ES bulk API 直接推送，
// 控制面仅做查询检索（与 Memory/SQL 后端写入语义解耦）。
package logstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ESStore 是 Elasticsearch 后端适配，实现 LogStore 接口。
//
// 查询路径：OpsMesh Query → ES bool query DSL → POST /<index>/_search → 解析 hits。
// 写入路径：noop（日志由 agent 经 filebeat 推送，控制面不写入 ES）。
type ESStore struct {
	endpoint string       // ES base URL（如 http://es:9200），不含路径
	index    string       // ES 索引名（如 opsmesh-logs）
	client   *http.Client // 复用 HTTP 连接池
}

// NewESStore 构造 ES 后端。
// endpoint 为 ES base URL（如 http://es:9200），index 为索引名（如 opsmesh-logs）。
// 默认 HTTP 超时 30s。
func NewESStore(endpoint, index string) *ESStore {
	return &ESStore{
		endpoint: strings.TrimRight(endpoint, "/"),
		index:    index,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Append 在 ES 后端为 noop：日志由 agent 通过 filebeat / fluent-bit / ES bulk 直接推送，
// 控制面仅做查询。返回 nil 以满足 LogStore 接口契约。
func (s *ESStore) Append(_ context.Context, _ *Entry) error { return nil }

// Query 翻译为 ES DSL，POST /<index>/_search，解析 hits。
//
// 翻译规则：
//   - TenantID/DeviceID/AgentID/Level/Source → bool.must 的 term 子句（精确匹配）
//   - Keyword → bool.must 的 match_phrase 子句（短语匹配，避免分词拆散）
//   - From/To → bool.filter 的 range 子句（不参与评分，性能更好）
//   - Limit → size（受 maxQueryLimit 约束）
//   - Offset → from（ES 原生支持分页）
//
// 返回顺序：timestamp desc（最新在前），与 Memory/SQL 语义一致。
func (s *ESStore) Query(ctx context.Context, q Query) ([]Entry, error) {
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

	body := s.buildDSL(q, limit)
	u := fmt.Sprintf("%s/%s/_search", s.endpoint, url.PathEscape(s.index))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("es search: status=%d body=%s", resp.StatusCode, string(b))
	}

	var sr esSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("es decode: %w", err)
	}

	out := make([]Entry, 0, len(sr.Hits.Hits))
	for _, h := range sr.Hits.Hits {
		e := Entry{
			TenantID: h.Source.TenantID,
			DeviceID: h.Source.DeviceID,
			AgentID:  h.Source.AgentID,
			TaskID:   h.Source.TaskID,
			Level:    h.Source.Level,
			Source:   h.Source.Source,
			Message:  h.Source.Message,
		}
		if h.Source.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, h.Source.Timestamp); err == nil {
				e.Timestamp = t.UTC()
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// buildDSL 构造 ES query JSON（bool query + sort + from/size）。
//
// DSL 结构：
//
//	{
//	  "query": {"bool": {"must": [...], "filter": [{"range": {"timestamp": {...}}}]}},
//	  "sort": [{"timestamp": "desc"}],
//	  "from": <offset>, "size": <limit>
//	}
func (s *ESStore) buildDSL(q Query, limit int) []byte {
	var must []map[string]interface{}
	if q.TenantID != "" {
		must = append(must, map[string]interface{}{"term": map[string]string{"tenant_id": q.TenantID}})
	}
	if q.DeviceID != "" {
		must = append(must, map[string]interface{}{"term": map[string]string{"device_id": q.DeviceID}})
	}
	if q.AgentID != "" {
		must = append(must, map[string]interface{}{"term": map[string]string{"agent_id": q.AgentID}})
	}
	if q.Level != "" {
		must = append(must, map[string]interface{}{"term": map[string]string{"level": q.Level}})
	}
	if q.Source != "" {
		must = append(must, map[string]interface{}{"term": map[string]string{"source": q.Source}})
	}
	if q.Keyword != "" {
		// match_phrase 避免分词拆散多词关键字（如 "disk full" 不应匹配 "disk" + "full" 分离的文档）。
		must = append(must, map[string]interface{}{"match_phrase": map[string]string{"message": q.Keyword}})
	}

	boolQ := map[string]interface{}{}
	if len(must) > 0 {
		boolQ["must"] = must
	}

	// 时间范围用 filter（不参与评分，性能更好且不污染相关性排序）。
	if !q.From.IsZero() || !q.To.IsZero() {
		rng := map[string]interface{}{}
		if !q.From.IsZero() {
			rng["gte"] = q.From.UTC().Format(time.RFC3339Nano)
		}
		if !q.To.IsZero() {
			rng["lte"] = q.To.UTC().Format(time.RFC3339Nano)
		}
		boolQ["filter"] = []map[string]interface{}{
			{"range": map[string]interface{}{"timestamp": rng}},
		}
	}

	dsl := map[string]interface{}{
		"query": map[string]interface{}{"bool": boolQ},
		"sort":  []map[string]string{{"timestamp": "desc"}},
		"from":  q.Offset,
		"size":  limit,
	}
	b, _ := json.Marshal(dsl)
	return b
}

// Close 释放底层资源（noop，http.Client 由 GC 回收）。
func (s *ESStore) Close() error { return nil }

// esSearchResponse ES _search 响应（仅解析 hits 部分，忽略 aggregations 等）。
type esSearchResponse struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Hits []esHit `json:"hits"`
	} `json:"hits"`
}

// esHit 单条 ES 命中。
type esHit struct {
	ID     string  `json:"_id"`
	Source esEntry `json:"_source"`
}

// esEntry ES 文档 _source 字段（与 Entry 字段映射，snake_case 命名）。
type esEntry struct {
	TenantID  string `json:"tenant_id"`
	DeviceID  string `json:"device_id"`
	AgentID   string `json:"agent_id"`
	TaskID    string `json:"task_id"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"` // RFC3339Nano 字符串，解析后赋给 Entry.Timestamp
}