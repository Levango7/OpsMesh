package query

import (
	"encoding/json"
	"fmt"
	"time"
)

// QueryRequest represents the full Grafana JSON datasource query request.
type QueryRequest struct {
	Targets    []Target `json:"targets"`
	Range      Range    `json:"range"`
	IntervalMs int64    `json:"intervalMs"`
}

// Target represents a single query target from Grafana.
type Target struct {
	Target string                 `json:"target"`
	Type   string                 `json:"type"`
	Data   map[string]interface{} `json:"data"`
}

// Range represents the time range of the query.
type Range struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// ParsedQuery holds the parsed query parameters.
type ParsedQuery struct {
	MetricType string
	MetricName string
	DeviceType string
	Tags       map[string]string
	From       time.Time
	To         time.Time
	Interval   time.Duration
	Format     string
}

// ParseQueryRequest parses the raw Grafana query request body into a structured form.
func ParseQueryRequest(body []byte) (*QueryRequest, error) {
	var req QueryRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("failed to parse query request: %w", err)
	}
	return &req, nil
}

// ParseTarget parses a single target into a ParsedQuery.
func ParseTarget(t Target, from, to time.Time, intervalMs int64) *ParsedQuery {
	pq := &ParsedQuery{
		MetricName: t.Target,
		From:       from,
		To:         to,
		Interval:   time.Duration(intervalMs) * time.Millisecond,
		Format:     t.Type,
		Tags:       make(map[string]string),
	}

	if t.Type == "" {
		pq.Format = "timeseries"
	}

	if v, ok := t.Data["metricType"].(string); ok {
		pq.MetricType = v
	}
	if v, ok := t.Data["deviceType"].(string); ok {
		pq.DeviceType = v
	}
	if v, ok := t.Data["tags"].(map[string]interface{}); ok {
		for k, val := range v {
			if s, ok := val.(string); ok {
				pq.Tags[k] = s
			}
		}
	}

	return pq
}

// ParseAnnotationsRequest parses the annotations request body.
func ParseAnnotationsRequest(body []byte) (*AnnotationsRequest, error) {
	var req AnnotationsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("failed to parse annotations request: %w", err)
	}
	return &req, nil
}

// AnnotationsRequest represents a Grafana annotations query.
type AnnotationsRequest struct {
	Range Range `json:"range"`
}
