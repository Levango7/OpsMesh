// Loki backend for log storage.
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

// LokiStore is a Grafana Loki-backed log store.
type LokiStore struct {
	endpoint string
	client   *http.Client
	labelApp string
}

// NewLokiStore creates a Loki-backed log store.
func NewLokiStore(endpoint string) *LokiStore {
	return &LokiStore{
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   &http.Client{Timeout: 30 * time.Second},
		labelApp: "opsmesh",
	}
}

// Append is a noop for Loki (logs are pushed by agents via promtail).
func (s *LokiStore) Append(_ context.Context, _ *Entry) error { return nil }

// Query translates OpsMesh Query to LogQL and calls Loki API.
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
	params.Set("limit", strconv.Itoa(limit+q.Offset))
	params.Set("direction", "backward")

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

	out := make([]Entry, 0, limit)
	for _, stream := range lr.Data.Result {
		for _, v := range stream.Values {
			if len(v) < 2 {
				continue
			}
			ts, tsErr := strconv.ParseInt(v[0], 10, 64)
			if tsErr != nil {
				continue
			}
			e := Entry{
				TenantID:  stream.Stream["tenant_id"],
				DeviceID:  stream.Stream["device_id"],
				AgentID:   stream.Stream["agent_id"],
				TaskID:    stream.Stream["task_id"],
				Level:     stream.Stream["level"],
				Source:    stream.Stream["source"],
				Timestamp: time.Unix(0, ts).UTC(),
				Message:   v[1],
			}
			out = append(out, e)
		}
	}

	if q.Offset >= len(out) {
		return []Entry{}, nil
	}
	out = out[q.Offset:]
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// buildLogQL translates Query to LogQL.
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

// Close releases resources (noop).
func (s *LokiStore) Close() error { return nil }

// Ensure LokiStore implements LogStore.
var _ LogStore = (*LokiStore)(nil)

// lokiQueryRangeResponse is the Loki /loki/api/v1/query_range response.
type lokiQueryRangeResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}
