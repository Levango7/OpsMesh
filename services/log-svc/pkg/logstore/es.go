// Elasticsearch backend for log storage.
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

// ESStore is an Elasticsearch-backed log store.
type ESStore struct {
	endpoint string
	index    string
	client   *http.Client
}

// NewESStore creates an Elasticsearch-backed log store.
func NewESStore(endpoint, index string) *ESStore {
	return &ESStore{
		endpoint: strings.TrimRight(endpoint, "/"),
		index:    index,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Append is a noop for ES (logs are pushed by agents via filebeat).
func (s *ESStore) Append(_ context.Context, _ *Entry) error { return nil }

// Query translates Query to ES DSL and calls ES search API.
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

// buildDSL constructs ES query JSON.
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
		must = append(must, map[string]interface{}{"match_phrase": map[string]string{"message": q.Keyword}})
	}

	boolQ := map[string]interface{}{}
	if len(must) > 0 {
		boolQ["must"] = must
	}

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

// Close releases resources (noop).
func (s *ESStore) Close() error { return nil }

// Ensure ESStore implements LogStore.
var _ LogStore = (*ESStore)(nil)

// esSearchResponse is the ES _search response.
type esSearchResponse struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Hits []esHit `json:"hits"`
	} `json:"hits"`
}

// esHit is a single ES hit.
type esHit struct {
	ID     string  `json:"_id"`
	Source esEntry `json:"_source"`
}

// esEntry is the ES document _source.
type esEntry struct {
	TenantID  string `json:"tenant_id"`
	DeviceID  string `json:"device_id"`
	AgentID   string `json:"agent_id"`
	TaskID    string `json:"task_id"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}
