// Package prometheus provides a client for querying Prometheus HTTP API.
// It supports instant queries, range queries, label discovery, and predefined
// metric helpers for node and GPU monitoring.
package prometheus

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// readAllString 尽力读取响应体用于错误消息；读取失败返回空串——
// 错误消息里少一段 body 不值得让调用链多一层错误传播。
func readAllString(r io.Reader) string {
	b, err := io.ReadAll(r)
	if err != nil {
		return ""
	}
	return string(b)
}

// MetricSample represents a single timestamped metric value.
type MetricSample struct {
	Labels map[string]string `json:"labels"`
	Value  float64           `json:"value"`
	Time   time.Time         `json:"time"`
}

// QueryResult holds the response from an instant query.
type QueryResult struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []json.RawMessage `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// RangeResult holds the response from a range query.
type RangeResult struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values []json.RawMessage `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// Client communicates with a Prometheus server via its HTTP API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
	available  bool
}

// NewClient creates a Prometheus client. If baseURL is empty, the client
// operates in simulated mode and all query methods return simulated data.
func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout:   timeout,
		available: false,
	}
	if baseURL != "" {
		c.available = c.IsAvailable()
	}
	return c
}

// IsAvailable checks whether the Prometheus server is reachable.
func (c *Client) IsAvailable() bool {
	if c.baseURL == "" {
		return false
	}
	resp, err := c.httpClient.Get(c.baseURL + "/-/healthy")
	if err != nil {
		c.available = false
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	c.available = resp.StatusCode == http.StatusOK
	return c.available
}

// Available returns the last known availability status.
func (c *Client) Available() bool {
	return c.available
}

// 数据来源标识。**这两个字符串是对外口径**（API 响应与文档都按字面量匹配），
// 改名必须同步改调用方与测试。
const (
	// SourcePrometheus：数值来自真实 Prometheus 查询。
	SourcePrometheus = "prometheus"
	// SourceSimulated：数值由本包 simulatedXXX 生成，**不是观测值**。
	SourceSimulated = "simulated"
)

// IsSimulated 报告本 client 当前是否会返回生成数值。
//
// 判定：未配置 baseURL，或最近一次健康检查没通过。
// 之所以由 client 来说，而不是每个调用方各自猜：调用点各猜一次迟早有一处猜错，
// 而"把生成的数值当成观测值交付出去"正是本服务最该防的口径事故（#61 造假面之三）。
func (c *Client) IsSimulated() bool {
	return c.baseURL == "" || !c.available
}

// Source 返回数据来源标识（与 IsSimulated 同一判定，避免两处口径分叉）。
func (c *Client) Source() string {
	if c.IsSimulated() {
		return SourceSimulated
	}
	return SourcePrometheus
}

// Query executes an instant PromQL query at the given time.
func (c *Client) Query(query string, t time.Time) (QueryResult, error) {
	if c.baseURL == "" {
		return QueryResult{Status: "simulated"}, nil
	}

	u, err := url.Parse(c.baseURL + "/api/v1/query")
	if err != nil {
		return QueryResult{}, fmt.Errorf("invalid base URL: %w", err)
	}
	q := u.Query()
	q.Set("query", query)
	if !t.IsZero() {
		q.Set("time", fmt.Sprintf("%d", t.Unix()))
	}
	u.RawQuery = q.Encode()

	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		return QueryResult{}, fmt.Errorf("prometheus query failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return QueryResult{}, fmt.Errorf("prometheus returned %d: %s", resp.StatusCode, readAllString(resp.Body))
	}

	var result QueryResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return QueryResult{}, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

// QueryRange executes a range PromQL query.
func (c *Client) QueryRange(query, start, end, step string) (RangeResult, error) {
	if c.baseURL == "" {
		return RangeResult{Status: "simulated"}, nil
	}

	u, err := url.Parse(c.baseURL + "/api/v1/query_range")
	if err != nil {
		return RangeResult{}, fmt.Errorf("invalid base URL: %w", err)
	}
	q := u.Query()
	q.Set("query", query)
	q.Set("start", start)
	q.Set("end", end)
	q.Set("step", step)
	u.RawQuery = q.Encode()

	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		return RangeResult{}, fmt.Errorf("prometheus query_range failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return RangeResult{}, fmt.Errorf("prometheus returned %d: %s", resp.StatusCode, readAllString(resp.Body))
	}

	var result RangeResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return RangeResult{}, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

// GetMetricNames returns all label names from Prometheus.
func (c *Client) GetMetricNames() ([]string, error) {
	if c.baseURL == "" {
		return []string{"node_cpu_seconds_total", "node_memory_MemTotal_bytes", "node_memory_MemAvailable_bytes"}, nil
	}

	resp, err := c.httpClient.Get(c.baseURL + "/api/v1/labels")
	if err != nil {
		return nil, fmt.Errorf("prometheus labels query failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned %d: %s", resp.StatusCode, readAllString(resp.Body))
	}

	var result struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode labels response: %w", err)
	}
	return result.Data, nil
}

// GetSeries returns series matching the given selector.
func (c *Client) GetSeries(selector string) ([]string, error) {
	if c.baseURL == "" {
		return []string{"node_cpu_seconds_total", "node_memory_MemTotal_bytes"}, nil
	}

	u, err := url.Parse(c.baseURL + "/api/v1/series")
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	q := u.Query()
	q.Set("match[]", selector)
	u.RawQuery = q.Encode()

	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("prometheus series query failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned %d: %s", resp.StatusCode, readAllString(resp.Body))
	}

	var result struct {
		Status string              `json:"status"`
		Data   []map[string]string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode series response: %w", err)
	}

	series := make([]string, 0, len(result.Data))
	for _, s := range result.Data {
		if name, ok := s["__name__"]; ok {
			series = append(series, name)
		}
	}
	return series, nil
}

// GetCPUUsage returns CPU usage percentage for a node.
func (c *Client) GetCPUUsage(nodeID string) ([]MetricSample, error) {
	query := `100 - avg(irate(node_cpu_seconds_total{mode="idle"}[5m])) * 100`
	if nodeID != "" {
		query = fmt.Sprintf(`100 - avg(irate(node_cpu_seconds_total{mode="idle",instance="%s"}[5m])) * 100`, nodeID)
	}

	result, err := c.Query(query, time.Now())
	if err != nil {
		return nil, err
	}
	if result.Status == "simulated" {
		return simulatedCPU(nodeID), nil
	}
	return extractSamples(result), nil
}

// GetMemoryUsage returns memory usage in bytes for a node.
func (c *Client) GetMemoryUsage(nodeID string) ([]MetricSample, error) {
	query := "node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes"
	if nodeID != "" {
		query = fmt.Sprintf(`node_memory_MemTotal_bytes{instance="%s"} - node_memory_MemAvailable_bytes{instance="%s"}`, nodeID, nodeID)
	}

	result, err := c.Query(query, time.Now())
	if err != nil {
		return nil, err
	}
	if result.Status == "simulated" {
		return simulatedMemory(nodeID), nil
	}
	return extractSamples(result), nil
}

// GetDiskUsage returns available disk space in bytes.
func (c *Client) GetDiskUsage(nodeID string) ([]MetricSample, error) {
	query := "node_filesystem_avail_bytes"
	if nodeID != "" {
		query = fmt.Sprintf(`node_filesystem_avail_bytes{instance="%s"}`, nodeID)
	}

	result, err := c.Query(query, time.Now())
	if err != nil {
		return nil, err
	}
	if result.Status == "simulated" {
		return simulatedDisk(nodeID), nil
	}
	return extractSamples(result), nil
}

// GetGPUUtilization returns GPU utilization percentage.
func (c *Client) GetGPUUtilization() ([]MetricSample, error) {
	query := "nvidia_gpu_utilization_gpu"

	result, err := c.Query(query, time.Now())
	if err != nil {
		return nil, err
	}
	if result.Status == "simulated" {
		return simulatedGPU(), nil
	}
	return extractSamples(result), nil
}

// extractSamples converts a QueryResult into MetricSample slice.
func extractSamples(qr QueryResult) []MetricSample {
	samples := make([]MetricSample, 0, len(qr.Data.Result))
	for _, r := range qr.Data.Result {
		if len(r.Value) < 2 {
			continue
		}
		var ts float64
		var val float64
		// Prometheus 契约：value=[<unix秒>, "<数值字符串>"]——时间戳是数字、
		// 数值是字符串。此前 `_ =` 吞掉「字符串→float64」的必然失败，
		// 所有样本的值都被静默写成 0（监控数据恒为零的真凶）。
		// 时间戳/数值解析失败的样本直接跳过：0 值样本会污染下游统计。
		if err := json.Unmarshal(r.Value[0], &ts); err != nil {
			continue
		}
		if err := json.Unmarshal(r.Value[1], &val); err != nil {
			var s string
			if err2 := json.Unmarshal(r.Value[1], &s); err2 != nil {
				continue
			}
			if val, err = strconv.ParseFloat(s, 64); err != nil {
				continue
			}
		}
		samples = append(samples, MetricSample{
			Labels: r.Metric,
			Value:  val,
			Time:   time.Unix(int64(ts), 0),
		})
	}
	return samples
}

// Simulated data generators for fallback mode.

func simulatedCPU(nodeID string) []MetricSample {
	now := time.Now()
	id := nodeID
	if id == "" {
		id = "node-1"
	}
	return []MetricSample{
		{Labels: map[string]string{"instance": id, "cpu": "0"}, Value: 45.2, Time: now},
		{Labels: map[string]string{"instance": id, "cpu": "1"}, Value: 62.8, Time: now},
		{Labels: map[string]string{"instance": id, "cpu": "2"}, Value: 33.1, Time: now},
		{Labels: map[string]string{"instance": id, "cpu": "3"}, Value: 71.5, Time: now},
	}
}

func simulatedMemory(nodeID string) []MetricSample {
	now := time.Now()
	id := nodeID
	if id == "" {
		id = "node-1"
	}
	return []MetricSample{
		{Labels: map[string]string{"instance": id}, Value: 8589934592, Time: now},
	}
}

func simulatedDisk(nodeID string) []MetricSample {
	now := time.Now()
	id := nodeID
	if id == "" {
		id = "node-1"
	}
	return []MetricSample{
		{Labels: map[string]string{"instance": id, "mountpoint": "/"}, Value: 107374182400, Time: now},
		{Labels: map[string]string{"instance": id, "mountpoint": "/data"}, Value: 214748364800, Time: now},
	}
}

func simulatedGPU() []MetricSample {
	now := time.Now()
	return []MetricSample{
		{Labels: map[string]string{"gpu": "0", "model": "A100"}, Value: 78.3, Time: now},
		{Labels: map[string]string{"gpu": "1", "model": "A100"}, Value: 42.1, Time: now},
		{Labels: map[string]string{"gpu": "2", "model": "V100"}, Value: 91.7, Time: now},
		{Labels: map[string]string{"gpu": "3", "model": "V100"}, Value: 15.4, Time: now},
	}
}
