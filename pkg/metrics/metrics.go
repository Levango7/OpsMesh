// Package metrics provides a zero-dependency Prometheus RED metrics registry
// with an HTTP handler for /metrics scraping.
//
// Metrics:
//   - http_requests_total{method,path,status} - Counter
//   - http_request_duration_seconds{method,path} - Histogram
//   - active_connections - Gauge
//   - business_metrics{name,tenant_id,...} - Gauge
//
// No external dependencies are introduced; all metrics are rendered in
// Prometheus text exposition format.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Default histogram buckets (seconds), matching prometheus.DefBuckets.
var defaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Registry holds all metric state.
type Registry struct {
	mu       sync.Mutex
	service  string
	reqTotal map[string]uint64          // "method|path|status" -> count
	reqHist  map[string]*histogramStats // "method|path" -> histogram
	conn     int64                      // active connections
	queue    int64                      // queue depth
	business map[string]businessEntry   // "name|label1=val1|..." -> value + labels
}

type histogramStats struct {
	buckets []uint64 // len == len(defaultBuckets)+1; last is +Inf
	sum     float64
	count   uint64
}

type businessEntry struct {
	value  float64
	labels map[string]string
}

var defaultRegistry *Registry

// Init initializes the global registry with the given service name.
// Safe to call once at startup; subsequent calls overwrite the registry.
func Init(serviceName string) {
	defaultRegistry = &Registry{
		service:  serviceName,
		reqTotal: make(map[string]uint64),
		reqHist:  make(map[string]*histogramStats),
		business: make(map[string]businessEntry),
	}
}

// RecordHTTPRequest records a single HTTP request: increments the counter
// and observes the duration on the histogram.
func RecordHTTPRequest(method, path, status string, durationSeconds float64) {
	if defaultRegistry == nil {
		return
	}
	defaultRegistry.recordHTTPRequest(method, path, status, durationSeconds)
}

func (r *Registry) recordHTTPRequest(method, path, status string, durationSeconds float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := method + "|" + path + "|" + status
	r.reqTotal[key]++

	histKey := method + "|" + path
	h, ok := r.reqHist[histKey]
	if !ok {
		h = &histogramStats{buckets: make([]uint64, len(defaultBuckets)+1)}
		r.reqHist[histKey] = h
	}
	idx := len(defaultBuckets)
	for i, b := range defaultBuckets {
		if durationSeconds <= b {
			idx = i
			break
		}
	}
	h.buckets[idx]++
	h.sum += durationSeconds
	h.count++
}

// RecordActiveConnections adjusts the active-connections gauge by delta.
func RecordActiveConnections(delta int) {
	if defaultRegistry == nil {
		return
	}
	defaultRegistry.recordActiveConnections(delta)
}

func (r *Registry) recordActiveConnections(delta int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conn += int64(delta)
}

// RecordQueueDepth sets the queue-depth gauge to the given value.
func RecordQueueDepth(depth int) {
	if defaultRegistry == nil {
		return
	}
	defaultRegistry.recordQueueDepth(depth)
}

func (r *Registry) recordQueueDepth(depth int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queue = int64(depth)
}

// RecordBusinessMetric sets a business gauge metric with custom labels.
func RecordBusinessMetric(name string, value float64, labels map[string]string) {
	if defaultRegistry == nil {
		return
	}
	defaultRegistry.recordBusinessMetric(name, value, labels)
}

func (r *Registry) recordBusinessMetric(name string, value float64, labels map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := buildBusinessKey(name, labels)
	r.business[key] = businessEntry{value: value, labels: labels}
}

// buildBusinessKey builds a canonical key from name + sorted labels.
func buildBusinessKey(name string, labels map[string]string) string {
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

// GetHandler returns an http.Handler that serves /metrics.
// All other paths return 404.
func GetHandler() http.Handler {
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

	// --- http_requests_total ---
	b = append(b, "# HELP http_requests_total Total number of HTTP requests.\n"...)
	b = append(b, "# TYPE http_requests_total counter\n"...)
	reqKeys := make([]string, 0, len(r.reqTotal))
	for k := range r.reqTotal {
		reqKeys = append(reqKeys, k)
	}
	sort.Strings(reqKeys)
	for _, k := range reqKeys {
		method, path, status := splitReqKey(k)
		b = append(b, fmt.Sprintf(
			`http_requests_total{method=%q,path=%q,status=%q} %d`+"\n",
			method, path, status, r.reqTotal[k])...)
	}

	// --- http_request_duration_seconds ---
	b = append(b, "# HELP http_request_duration_seconds HTTP request latency in seconds.\n"...)
	b = append(b, "# TYPE http_request_duration_seconds histogram\n"...)
	histKeys := make([]string, 0, len(r.reqHist))
	for k := range r.reqHist {
		histKeys = append(histKeys, k)
	}
	sort.Strings(histKeys)
	for _, k := range histKeys {
		method, path := splitHistKey(k)
		h := r.reqHist[k]
		var cumulative uint64
		for i, le := range defaultBuckets {
			cumulative += h.buckets[i]
			b = append(b, fmt.Sprintf(
				`http_request_duration_seconds_bucket{method=%q,path=%q,le=%q} %d`+"\n",
				method, path, formatBucket(le), cumulative)...)
		}
		cumulative += h.buckets[len(defaultBuckets)]
		b = append(b, fmt.Sprintf(
			`http_request_duration_seconds_bucket{method=%q,path=%q,le="+Inf"} %d`+"\n",
			method, path, cumulative)...)
		b = append(b, fmt.Sprintf(
			`http_request_duration_seconds_sum{method=%q,path=%q} %f`+"\n",
			method, path, h.sum)...)
		b = append(b, fmt.Sprintf(
			`http_request_duration_seconds_count{method=%q,path=%q} %d`+"\n",
			method, path, h.count)...)
	}

	// --- active_connections ---
	b = append(b, "# HELP active_connections Current number of active connections.\n"...)
	b = append(b, "# TYPE active_connections gauge\n"...)
	b = append(b, fmt.Sprintf("active_connections %d\n", r.conn)...)

	// --- queue_depth ---
	b = append(b, "# HELP queue_depth Current queue depth.\n"...)
	b = append(b, "# TYPE queue_depth gauge\n"...)
	b = append(b, fmt.Sprintf("queue_depth %d\n", r.queue)...)

	// --- business_metrics ---
	b = append(b, "# HELP business_metrics Custom business metrics.\n"...)
	b = append(b, "# TYPE business_metrics gauge\n"...)
	bizKeys := make([]string, 0, len(r.business))
	for k := range r.business {
		bizKeys = append(bizKeys, k)
	}
	sort.Strings(bizKeys)
	for _, k := range bizKeys {
		e := r.business[k]
		name := k
		if idx := strings.IndexByte(k, '|'); idx >= 0 {
			name = k[:idx]
		}
		if len(e.labels) > 0 {
			keys := make([]string, 0, len(e.labels))
			for lk := range e.labels {
				keys = append(keys, lk)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, lk := range keys {
				parts = append(parts, fmt.Sprintf("%s=%q", lk, e.labels[lk]))
			}
			b = append(b, fmt.Sprintf("business_metrics{name=%q,%s} %v\n", name, strings.Join(parts, ","), e.value)...)
		} else {
			b = append(b, fmt.Sprintf("business_metrics{name=%q} %v\n", name, e.value)...)
		}
	}

	return string(b)
}

// splitReqKey splits "method|path|status" into three parts.
func splitReqKey(k string) (method, path, status string) {
	first := strings.IndexByte(k, '|')
	if first < 0 {
		return k, "", ""
	}
	last := strings.LastIndexByte(k, '|')
	if last == first {
		return k[:first], k[first+1:], ""
	}
	return k[:first], k[first+1 : last], k[last+1:]
}

// splitHistKey splits "method|path" into two parts.
func splitHistKey(k string) (method, path string) {
	idx := strings.IndexByte(k, '|')
	if idx < 0 {
		return k, ""
	}
	return k[:idx], k[idx+1:]
}

// formatBucket formats a bucket boundary for a Prometheus label.
func formatBucket(le float64) string {
	return strconv.FormatFloat(le, 'g', -1, 64)
}

// HTTPMiddleware returns a middleware that records method/path/status/duration
// for every request passing through it.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(ww, r)
		duration := time.Since(start).Seconds()
		RecordHTTPRequest(r.Method, r.URL.Path, strconv.Itoa(ww.statusCode), duration)
	})
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}
