// Package logstore Loki 后端适配（M4-4B）。
//
// Loki 是 Grafana 出品的日志聚合系统，使用 LogQL 查询语言。
// 本适配层仅实现查询接口（Query）：将 OpsMesh Query 翻译为 LogQL，
// 调用 Loki /loki/api/v1/query_range HTTP API，解析 stream 结果为 Entry 切片。
// Append 为 noop——日志由 agent 通过 promtail / loki-push 直接推送至 Loki，
// 控制面仅做查询检索（与 Memory/SQL 后端写入语义解耦，避免控制面成为日志写入瓶颈）。
package logstore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LokiStore 是 Grafana Loki 后端适配，实现 LogStore 接口。
//
// 查询路径：OpsMesh Query → LogQL → GET /loki/api/v1/query_range → 解析 streams。
// 写入路径：noop（日志由 agent 经 promtail 推送，控制面不写入 Loki）。
type LokiStore struct {
	endpoint string       // Loki base URL（如 http://loki:3100），不含路径
	client   *http.Client // 复用 HTTP 连接池
	labelApp string       // 流标签 app 的值（默认 "opsmesh"）
}

// NewLokiStore 构造 Loki 后端。
// endpoint 为 Loki base URL（如 http://loki:3100），尾部 / 会被裁剪。
// 默认 HTTP 超时 30s（查询大时间窗时建议外层用 context.WithTimeout 覆盖）。
func NewLokiStore(endpoint string) *LokiStore {
	return &LokiStore{
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   &http.Client{Timeout: 30 * time.Second},
		labelApp: "opsmesh",
	}
}

// Append 在 Loki 后端为 noop：日志由 agent 通过 promtail / loki-push 直接推送至 Loki，
// 控制面仅做查询。返回 nil 以满足 LogStore 接口契约。
func (s *LokiStore) Append(_ context.Context, _ *Entry) error { return nil }

// Query 将 OpsMesh 查询翻译为 LogQL，调用 Loki query_range API，解析 stream 结果。
//
// 翻译规则：
//   - TenantID/DeviceID/AgentID/Level/Source → LogQL 流标签选择器 {app="opsmesh", tenant_id="t1", ...}
//   - Keyword → 日志管道 |= "keyword"
//   - From/To → query_range 的 start/end 参数（unix nano）；未指定时回退最近 24h
//   - Limit → query_range 的 limit（受 maxQueryLimit 约束）
//   - Offset → 客户端切片（请求 limit+offset 条，丢弃前 offset 条）
//
// 返回顺序：最新在前（direction=backward），与 Memory/SQL 语义一致。
func (s *LokiStore) Query(ctx context.Context, q Query) ([]Entry, error) {
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

	logql := s.buildLogQL(q)

	params := url.Values{}
	params.Set("query", logql)
	// 多取 offset 条用于客户端切片分页（Loki 无原生 offset）。
	params.Set("limit", strconv.Itoa(limit+q.Offset))
	params.Set("direction", "backward") // 最新在前，匹配 Memory/SQL 语义
	// 时间范围：未指定 From 时回退最近 24h（Loki query_range 默认仅最近 1h，覆盖面太窄）。
	start := q.From
	if start.IsZero() {
		start = time.Now().Add(-24 * time.Hour)
	}
	end := q.To
	if end.IsZero() {
		end = time.Now()
	}
	params.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	params.Set("end", strconv.FormatInt(end.UnixNano(), 10))

	u := s.endpoint + "/loki/api/v1/query_range?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("loki query_range: status=%d body=%s", resp.StatusCode, string(body))
	}

	var lr lokiQueryRangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return nil, fmt.Errorf("loki decode: %w", err)
	}
	if lr.Status != "success" {
		return nil, fmt.Errorf("loki status=%q error=%s", lr.Status, lr.Error)
	}

	// 解析 streams：每个 stream 含一组标签 + 多条日志行。
	out := make([]Entry, 0, limit)
	for _, stream := range lr.Data.Result {
		tenantID := stream.Stream["tenant_id"]
		deviceID := stream.Stream["device_id"]
		agentID := stream.Stream["agent_id"]
		taskID := stream.Stream["task_id"]
		level := stream.Stream["level"]
		source := stream.Stream["source"]
		for _, v := range stream.Values {
			if len(v) < 2 {
				continue
			}
			ts, _ := strconv.ParseInt(v[0], 10, 64)
			e := Entry{
				TenantID:  tenantID,
				DeviceID:  deviceID,
				AgentID:   agentID,
				TaskID:    taskID,
				Level:     level,
				Source:    source,
				Timestamp: time.Unix(0, ts).UTC(),
				Message:   parseLokiLine(v[1]),
			}
			out = append(out, e)
		}
	}

	// 客户端 offset 切片（backward 模式下 out 已最新在前）。
	if q.Offset >= len(out) {
		return []Entry{}, nil
	}
	out = out[q.Offset:]
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// buildLogQL 翻译 OpsMesh Query 为 LogQL。
//
// 例：{app="opsmesh", tenant_id="t1", agent_id="a1", level="error"} |= "boom"
// 标签顺序固定（app 在前），便于测试与缓存命中。
func (s *LokiStore) buildLogQL(q Query) string {
	labels := []string{fmt.Sprintf(`app=%q`, s.labelApp)}
	if q.TenantID != "" {
		labels = append(labels, fmt.Sprintf(`tenant_id=%q`, q.TenantID))
	}
	if q.DeviceID != "" {
		labels = append(labels, fmt.Sprintf(`device_id=%q`, q.DeviceID))
	}
	if q.AgentID != "" {
		labels = append(labels, fmt.Sprintf(`agent_id=%q`, q.AgentID))
	}
	if q.Level != "" {
		labels = append(labels, fmt.Sprintf(`level=%q`, q.Level))
	}
	if q.Source != "" {
		labels = append(labels, fmt.Sprintf(`source=%q`, q.Source))
	}
	expr := "{" + strings.Join(labels, ", ") + "}"
	if q.Keyword != "" {
		expr += fmt.Sprintf(` |= %q`, q.Keyword)
	}
	return expr
}

// parseLokiLine 解析 Loki 日志行：优先按 JSON 解析 {"message": "..."}（promtail json stage 输出），
// 解析失败则原样返回（纯文本行）。
func parseLokiLine(line string) string {
	var m map[string]interface{}
	if json.Unmarshal([]byte(line), &m) == nil {
		if msg, ok := m["message"].(string); ok {
			return msg
		}
	}
	return line
}

// Close 释放底层资源（noop，http.Client 由 GC 回收）。
func (s *LokiStore) Close() error { return nil }

// lokiQueryRangeResponse Loki /loki/api/v1/query_range 响应。
type lokiQueryRangeResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"` // 每元素 [unix-nano, log-line]
		} `json:"result"`
	} `json:"data"`
}