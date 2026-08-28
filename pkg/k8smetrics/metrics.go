// Package k8smetrics provides a Prometheus metrics adapter for Kubernetes
// Horizontal Pod Autoscaler custom metrics. It exposes application-level
// metrics (GPU queue depth, pending tasks, device heartbeat lag, firing
// alerts) in Prometheus text exposition format for the metrics-server /
// Prometheus Adapter to scrape.
//
// Pre-defined metrics:
//   - opsmesh_gpu_queue_depth    - GPU workload queue depth
//   - opsmesh_task_pending_count - Pending tasks count
//   - opsmesh_device_heartbeat_lag - Device heartbeat lag (seconds)
//   - opsmesh_alert_firing_count - Firing alerts count
//
// No external dependencies are introduced; all metrics are rendered in
// Prometheus text exposition format.
package k8smetrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// metricEntry holds a single custom metric value with its labels.
type metricEntry struct {
	value  float64
	labels map[string]string
}

// Registry holds all custom metric state.
type Registry struct {
	mu        sync.Mutex
	metrics   map[string]*metricDefinition
	instances map[string]metricEntry // "name|label1=val1|..." -> entry
}

type metricDefinition struct {
	name string
	help string
}

var defaultRegistry *Registry

// Init initializes the global registry and pre-defines the built-in metrics.
// Safe to call once at startup; subsequent calls overwrite the registry.
func Init() {
	defaultRegistry = &Registry{
		metrics:   make(map[string]*metricDefinition),
		instances: make(map[string]metricEntry),
	}
	RegisterCustomMetric("opsmesh_gpu_queue_depth", "GPU workload queue depth.")
	RegisterCustomMetric("opsmesh_task_pending_count", "Number of pending tasks awaiting execution.")
	RegisterCustomMetric("opsmesh_device_heartbeat_lag", "Device heartbeat lag in seconds since last check-in.")
	RegisterCustomMetric("opsmesh_alert_firing_count", "Number of currently firing alerts.")
}

// RegisterCustomMetric registers a custom metric with the given name and help text.
// If the metric is already registered, the help text is updated.
func RegisterCustomMetric(name, help string) {
	if defaultRegistry == nil {
		return
	}
	defaultRegistry.registerCustomMetric(name, help)
}

func (r *Registry) registerCustomMetric(name, help string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics[name] = &metricDefinition{name: name, help: help}
}

// SetCustomMetric sets the value of a custom metric with optional labels.
// The metric must have been registered first via RegisterCustomMetric or Init.
func SetCustomMetric(name string, value float64, labels map[string]string) {
	if defaultRegistry == nil {
		return
	}
	defaultRegistry.setCustomMetric(name, value, labels)
}

func (r *Registry) setCustomMetric(name string, value float64, labels map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.metrics[name]; !ok {
		return
	}
	key := buildKey(name, labels)
	r.instances[key] = metricEntry{value: value, labels: labels}
}

// GetMetricsHandler returns an http.Handler that serves /metrics in Prometheus
// text exposition format. All other paths return 404.
func GetMetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		if defaultRegistry == nil {
			http.Error(w, "metrics not initialized", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(defaultRegistry.render()))
	})
}

// render produces the Prometheus text exposition format output.
func (r *Registry) render() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var b []byte

	// Collect metric names in sorted order for deterministic output.
	names := make([]string, 0, len(r.metrics))
	for name := range r.metrics {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		def := r.metrics[name]

		// HELP line.
		b = append(b, fmt.Sprintf("# HELP %s %s\n", name, def.help)...)
		// TYPE line - all custom metrics are gauges.
		b = append(b, fmt.Sprintf("# TYPE %s gauge\n", name)...)

		// Collect all instances of this metric.
		var instances []string
		for key := range r.instances {
			if metricNameFromKey(key) == name {
				instances = append(instances, key)
			}
		}
		sort.Strings(instances)

		for _, key := range instances {
			entry := r.instances[key]
			if len(entry.labels) > 0 {
				keys := make([]string, 0, len(entry.labels))
				for lk := range entry.labels {
					keys = append(keys, lk)
				}
				sort.Strings(keys)
				parts := make([]string, 0, len(keys))
				for _, lk := range keys {
					parts = append(parts, fmt.Sprintf("%s=%q", lk, entry.labels[lk]))
				}
				b = append(b, fmt.Sprintf("%s{%s} %v\n", name, strings.Join(parts, ","), entry.value)...)
			} else {
				b = append(b, fmt.Sprintf("%s %v\n", name, entry.value)...)
			}
		}
	}

	return string(b)
}

// buildKey builds a canonical key from name + sorted labels.
func buildKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(labels))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	return name + "|" + strings.Join(parts, "|")
}

// metricNameFromKey extracts the metric name from a canonical key.
func metricNameFromKey(key string) string {
	if idx := strings.IndexByte(key, '|'); idx >= 0 {
		return key[:idx]
	}
	return key
}
