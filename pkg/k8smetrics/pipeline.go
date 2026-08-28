// Package k8smetrics provides a background metrics collection pipeline that
// periodically gathers service-level metrics and exposes them for Kubernetes
// HPA custom metric scaling decisions.
package k8smetrics

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// ServiceEndpoint describes a service to collect metrics from.
type ServiceEndpoint struct {
	Name   string
	URL    string // e.g. "http://task-svc.opsmesh.svc:9091"
	Client *http.Client
}

// metricCollector defines how to collect a specific metric from a service response.
type metricCollector struct {
	metricName string
	extract    func(body []byte, svcName string) (value float64, labels map[string]string, err error)
}

// pipelineState holds the running state of the metrics collection pipeline.
type pipelineState struct {
	mu         sync.Mutex
	interval   time.Duration
	endpoints  []ServiceEndpoint
	collectors []metricCollector
	httpClient *http.Client
}

var activePipeline *pipelineState

// StartMetricsPipeline starts a background goroutine that periodically collects
// metrics from configured services and registers them with the k8smetrics registry.
// The pipeline runs until the provided context is cancelled.
//
// The interval determines how often metrics are collected. A typical value is 15s
// to match the Prometheus Adapter's default scrape interval.
func StartMetricsPipeline(ctx context.Context, interval time.Duration) {
	if defaultRegistry == nil {
		Init()
	}

	activePipeline = &pipelineState{
		interval:  interval,
		endpoints: defaultEndpoints(),
		collectors: []metricCollector{
			{metricName: "opsmesh_gpu_queue_depth", extract: extractGPUQueueDepth},
			{metricName: "opsmesh_task_pending_count", extract: extractTaskPendingCount},
			{metricName: "opsmesh_device_heartbeat_lag", extract: extractDeviceHeartbeatLag},
			{metricName: "opsmesh_alert_firing_count", extract: extractAlertFiringCount},
		},
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	go activePipeline.run(ctx)
}

// run is the main collection loop.
func (p *pipelineState) run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	// Collect immediately on start.
	p.collectAll()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.collectAll()
		}
	}
}

// collectAll iterates over all endpoints and collectors, fetching metrics
// and registering them with the global registry.
func (p *pipelineState) collectAll() {
	for _, ep := range p.endpoints {
		body, err := p.fetchMetrics(ep)
		if err != nil {
			continue
		}
		for _, c := range p.collectors {
			value, labels, err := c.extract(body, ep.Name)
			if err != nil {
				continue
			}
			SetCustomMetric(c.metricName, value, labels)
		}
	}
}

// fetchMetrics retrieves the /metrics endpoint from a service.
func (p *pipelineState) fetchMetrics(ep ServiceEndpoint) ([]byte, error) {
	client := p.httpClient
	if ep.Client != nil {
		client = ep.Client
	}

	url := fmt.Sprintf("%s/metrics", ep.URL)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, ep.URL)
	}

	return io.ReadAll(resp.Body)
}

// defaultEndpoints returns the default service endpoints within the opsmesh namespace.
func defaultEndpoints() []ServiceEndpoint {
	return []ServiceEndpoint{
		{Name: "gpu-svc", URL: "http://gpu-svc.opsmesh.svc:9091"},
		{Name: "task-svc", URL: "http://task-svc.opsmesh.svc:9091"},
		{Name: "device-svc", URL: "http://device-svc.opsmesh.svc:9091"},
		{Name: "alert-svc", URL: "http://alert-svc.opsmesh.svc:9091"},
	}
}

// --- Metric extractors -----------------------------------------------------
// Each extractor parses the Prometheus text exposition format response body
// and returns the metric value with optional labels. These are simplified
// parsers that look for the metric name in the response.

// extractGPUQueueDepth extracts opsmesh_gpu_queue_depth from the response.
func extractGPUQueueDepth(body []byte, svcName string) (float64, map[string]string, error) {
	value, err := parseMetricValue(body, "opsmesh_gpu_queue_depth")
	if err != nil {
		return 0, nil, err
	}
	return value, map[string]string{"service": svcName}, nil
}

// extractTaskPendingCount extracts opsmesh_task_pending_count from the response.
func extractTaskPendingCount(body []byte, svcName string) (float64, map[string]string, error) {
	value, err := parseMetricValue(body, "opsmesh_task_pending_count")
	if err != nil {
		return 0, nil, err
	}
	return value, map[string]string{"service": svcName}, nil
}

// extractDeviceHeartbeatLag extracts opsmesh_device_heartbeat_lag from the response.
func extractDeviceHeartbeatLag(body []byte, svcName string) (float64, map[string]string, error) {
	value, err := parseMetricValue(body, "opsmesh_device_heartbeat_lag")
	if err != nil {
		return 0, nil, err
	}
	return value, map[string]string{"service": svcName}, nil
}

// extractAlertFiringCount extracts opsmesh_alert_firing_count from the response.
func extractAlertFiringCount(body []byte, svcName string) (float64, map[string]string, error) {
	value, err := parseMetricValue(body, "opsmesh_alert_firing_count")
	if err != nil {
		return 0, nil, err
	}
	return value, map[string]string{"service": svcName}, nil
}

// parseMetricValue scans the Prometheus text exposition format for a metric
// with the given name (no labels) and returns its value. If the metric has
// labels, it sums all instances.
func parseMetricValue(body []byte, metricName string) (float64, error) {
	var total float64
	var found bool

	lines := splitLines(string(body))
	for _, line := range lines {
		line = trimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}

		// Parse: metric_name{labels} value [timestamp]
		// or:   metric_name value [timestamp]
		name, value, err := parseLine(line)
		if err != nil {
			continue
		}
		if name != metricName {
			continue
		}
		total += value
		found = true
	}

	if !found {
		return 0, fmt.Errorf("metric %q not found", metricName)
	}
	return total, nil
}

// parseLine parses a single Prometheus exposition line into (name, value).
func parseLine(line string) (string, float64, error) {
	// Find the metric name end: either '{' or first space.
	nameEnd := -1
	for i := 0; i < len(line); i++ {
		if line[i] == '{' || line[i] == ' ' {
			nameEnd = i
			break
		}
	}
	if nameEnd < 0 {
		return "", 0, fmt.Errorf("invalid line: %s", line)
	}

	name := line[:nameEnd]

	// Find the value: after '}' if labels present, or after name.
	rest := line[nameEnd:]
	if len(rest) > 0 && rest[0] == '{' {
		// Skip past '}'.
		if idx := findByte(rest, '}'); idx >= 0 {
			rest = rest[idx+1:]
		} else {
			return "", 0, fmt.Errorf("unclosed labels: %s", line)
		}
	} else {
		// Skip past the space after name.
		rest = rest[1:]
	}
	// Trim any leading whitespace between labels/name and value.
	rest = trimSpace(rest)

	// The rest starts with the value; take the first whitespace-separated token.
	valueStr := rest
	if idx := findByte(rest, ' '); idx >= 0 {
		valueStr = rest[:idx]
	}

	var value float64
	if _, err := fmt.Sscanf(valueStr, "%g", &value); err != nil {
		return "", 0, fmt.Errorf("invalid value %q: %w", valueStr, err)
	}

	return name, value, nil
}

// splitLines splits a string by newline characters.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// trimSpace removes leading and trailing whitespace from a string.
func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// findByte returns the index of the first occurrence of b in s, or -1.
func findByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
