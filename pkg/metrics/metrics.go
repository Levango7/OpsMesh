// Package metrics provides a zero-dependency Prometheus RED metrics registry
// with an HTTP handler for /metrics scraping.
//
// Metrics:
//   - http_requests_total{method,path,status} - Counter（路径已归一化 + 基数熔断）
//   - http_request_duration_seconds{method,path} - Histogram
//   - active_connections - Gauge（由 HTTPMiddleware 按在途请求真实喂数）
//   - service_info{service} - Gauge=1（Init 传入的服务名，用于区分同名序列的来源）
//   - business_metrics{name,...} - Gauge（Set 语义：当前值）
//   - business_metrics_total{name,...} - Counter（Add 语义：自进程启动以来的累计量）
//
// **类型与函数必须配对使用**：`*_total` 的名字只允许出现在 counter 家族里，
// 也就是只能配 AddBusinessMetric。SetBusinessMetric 写过一次的前科是把 1 存成当前值，
// 于是 `business_metrics{name="task_claims_total"}` 恒为 1，`rate()/increase()` 全无意义
// ——见 docs/commercial-readiness-review-2026-09-25.md §25。
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

// 基数上限（与 internal/metrics 的同名控制保持同一口径）。
//
// 未鉴权/半开放入口上的任意 URL 都会成为一个 (method,path,status) 时序；无上限意味着
// 扫描器遍历随机路径即可让 reqTotal/reqHist 无限增长直至 OOM。控制面在 P1-5 已经补过
// 这一层，而微服务共用的本包此前**一个上限都没有**（且中间件直接把 r.URL.Path 当标签），
// 所以真正的暴露面在微服务侧。
const (
	maxHTTPSeries     = 2000
	maxBusinessSeries = 2000
	otherPathLabel    = ":other"
	maxPathLen        = 200
	maxPathSegmentLen = 48
	maxLabelValueLen  = 64
)

// Registry holds all metric state.
type Registry struct {
	mu       sync.Mutex
	service  string
	reqTotal map[string]uint64          // "method|path|status" -> count
	reqHist  map[string]*histogramStats // "method|path" -> histogram
	conn     int64                      // active connections
	business map[string]businessEntry   // "name|label1=val1|..." -> value + labels

	// httpSeries 已分配的 HTTP 时序键集合（上限判定），httpDropped 因超限被折叠的请求数。
	httpSeries  map[string]struct{}
	httpDropped uint64
	// bizSeries 已分配的业务时序键集合（gauge + counter 合算），bizDropped 折叠次数。
	bizSeries  map[string]struct{}
	bizDropped uint64
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
//
// 服务名会渲染成 service_info{service="..."}——以前这个参数只是存进字段再没人读，
// 等于一个"声明了但从不生效"的哑按钮（与 §22 抓到的 --skip-images 同类）。
func Init(serviceName string) {
	defaultRegistry = &Registry{
		service:    serviceName,
		reqTotal:   make(map[string]uint64),
		reqHist:    make(map[string]*histogramStats),
		business:   make(map[string]businessEntry),
		httpSeries: make(map[string]struct{}),
		bizSeries:  make(map[string]struct{}),
	}
}

// RecordHTTPRequest records a single HTTP request: increments the counter
// and observes the duration on the histogram.
//
// path 会先归一化再判基数，所以调用方**不需要**自己替换 ID。
func RecordHTTPRequest(method, path, status string, durationSeconds float64) {
	if defaultRegistry == nil {
		return
	}
	defaultRegistry.recordHTTPRequest(method, path, status, durationSeconds)
}

func (r *Registry) recordHTTPRequest(method, path, status string, durationSeconds float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	k := r.httpSeriesKey(method, NormalizePath(path), status)
	r.reqTotal[k]++

	// 直方图按 (method|path) 归并：状态码对延迟分布无区分意义，把 status 放进直方图
	// 会让同一请求在两个键空间里各占一份基数。
	// **必须从 k 派生而不是从入参 method 拼**：k 在超限分支里已被折叠成
	// (:other|:other|status)，用原始 method 会绕开上限、把直方图键空间重新撑开。
	histKey := k[:strings.LastIndexByte(k, '|')]
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

// httpSeriesKey 解析一次时序键，并在超限时折叠（调用方须持锁）。
//
//   - 方法**总是**先收敛到 7 个标准方法 + :other：method 由请求方自选，
//     只在超限时才收敛意味着"任意方法文本"仍可先占满 2000 个名额；
//   - 未达上限：登记真实键；
//   - 已达上限：路径折叠为 :other，并累加 httpDropped。
//
// 折叠后的键空间有界：方法 8 种 × 路径 1 种 × 状态码（net/http 产生，有限集）。
func (r *Registry) httpSeriesKey(method, path, status string) string {
	m := collapseHTTPMethod(method)
	k := m + "|" + path + "|" + status
	if _, ok := r.httpSeries[k]; ok {
		return k
	}
	if len(r.httpSeries) < maxHTTPSeries {
		r.httpSeries[k] = struct{}{}
		return k
	}
	r.httpDropped++
	k = m + "|" + otherPathLabel + "|" + status
	r.httpSeries[k] = struct{}{}
	return k
}

// collapseHTTPMethod 把非标准 HTTP 方法收敛为 :other——method 由请求方自选，
// 不收敛的话攻击者用任意方法文本就能撑大折叠后的键空间。
func collapseHTTPMethod(method string) string {
	switch method {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS":
		return method
	default:
		return otherPathLabel
	}
}

// RecordActiveConnections adjusts the active-connections gauge by delta.
// 由 HTTPMiddleware 按"进入 +1 / 离开 -1"喂数，不再是没有调用方的恒零仪表。
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

// SetBusinessMetric 写入一个**仪表值（gauge）**：本次给的就是当前值，覆盖旧值。
// 适用："现在有多少台设备在线""当前成功率"。名字**不要**以 _total 结尾。
func SetBusinessMetric(name string, value float64, labels map[string]string) {
	if defaultRegistry == nil {
		return
	}
	defaultRegistry.setBusinessMetric(name, value, labels)
}

// AddBusinessMetric 累加一个**计数器（counter）**：自进程启动以来的累计量，单调不减。
// 适用：事件次数（心跳数、领取数、失败数）。渲染进 business_metrics_total 家族，
// 名字以 _total 结尾是**约定**而非语义来源——类型由家族决定，所以配 rate()/increase() 才对。
func AddBusinessMetric(name string, delta float64, labels map[string]string) {
	if defaultRegistry == nil {
		return
	}
	defaultRegistry.addBusinessMetric(name, delta, labels)
}

func (r *Registry) setBusinessMetric(name string, value float64, labels map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := r.bizKey("g:"+name, labels)
	r.business[key] = businessEntry{value: value, labels: labelsOf(key)}
}

func (r *Registry) addBusinessMetric(name string, delta float64, labels map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := r.bizKey("c:"+name, labels)
	e := r.business[key]
	e.value += delta
	if e.labels == nil {
		e.labels = labelsOf(key)
	}
	r.business[key] = e
}

// bizKey 解析业务时序键（调用方须持锁）。
//
// 键前缀 "g:"/"c:" 让同名 gauge 与 counter 不互相覆盖；标签值先经 sanitizeLabelValue。
// 超过 maxBusinessSeries 后折叠成 folded=":other"——折叠键自己也占一个名额，
// 这样 business_metrics_series 报出的就是真实占用数（与 HTTP 侧同一口径）。
func (r *Registry) bizKey(prefixedName string, labels map[string]string) string {
	key := buildBusinessKey(prefixedName, labels)
	if _, ok := r.bizSeries[key]; ok {
		return key
	}
	if len(r.bizSeries) < maxBusinessSeries {
		r.bizSeries[key] = struct{}{}
		return key
	}
	r.bizDropped++
	folded := buildBusinessKey(prefixedName, map[string]string{"folded": otherPathLabel})
	r.bizSeries[folded] = struct{}{}
	return folded
}

// NormalizePath 归一化 URL 路径，避免路径进入指标标签时造成无界基数。
//   - 空/根路径原样；整路径超长 → /:overlong
//   - 纯数字段、超长段、含 [A-Za-z0-9._~-] 以外字符的段 → :id
//
// 未匹配路由（404）同样经过本函数，故对任意输入必须是 O(len) 且输出空间受控。
// 口径与 internal/controlplane/server_middleware.go 的 normalizePath 一致；
// 两处并存是已知重复（统一属 §23 第 5 项"HTTP 指标命名统一"那一批）。
func NormalizePath(p string) string {
	if p == "" || p == "/" {
		return p
	}
	if len(p) > maxPathLen {
		return "/:overlong"
	}
	parts := strings.Split(p, "/")
	changed := false
	for i, part := range parts {
		if part == "" || part == ":id" {
			continue
		}
		if len(part) > maxPathSegmentLen || !pathSegmentSafe(part) || allDigits(part) {
			parts[i] = ":id"
			changed = true
		}
	}
	if !changed {
		return p
	}
	return strings.Join(parts, "/")
}

func pathSegmentSafe(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '~' || c == '-':
		default:
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// sanitizeLabelValue 收紧标签取值：非白名单字符或超长一律换成 :other。
// 标签值一旦来自调用方传入的 ID/URL 片段，不设限就等于把基数交给客户端。
func sanitizeLabelValue(v string) string {
	if v == "" {
		return v
	}
	if len(v) > maxLabelValueLen {
		return otherPathLabel
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '~' || c == '-':
		default:
			return otherPathLabel
		}
	}
	return v
}

// buildBusinessKey builds a canonical key from name + sorted labels.
// 键前缀 "g:"/"c:" 让同名 gauge 与 counter 不互相覆盖。
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
		parts = append(parts, k+"="+sanitizeLabelValue(labels[k]))
	}
	return name + "|" + strings.Join(parts, "|")
}

// labelsOf 从键里还原标签集（渲染用）。
func labelsOf(key string) map[string]string {
	idx := strings.IndexByte(key, '|')
	if idx < 0 {
		return nil
	}
	out := make(map[string]string)
	for _, p := range strings.Split(key[idx+1:], "|") {
		if k, v, ok := strings.Cut(p, "="); ok {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// businessName 取键里的指标名（去掉 g:/c: 前缀与标签段）。
func businessName(key string) string {
	name := key
	if idx := strings.IndexByte(key, '|'); idx >= 0 {
		name = key[:idx]
	}
	if len(name) > 2 && (name[:2] == "g:" || name[:2] == "c:") {
		name = name[2:]
	}
	return name
}

// isCounterKey 判定键属于 counter 家族。
func isCounterKey(key string) bool { return strings.HasPrefix(key, "c:") }

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

	// --- service_info ---
	// Init 传入的服务名以前从不落地，这里补上：抓取面上多个微服务产出的家族名完全相同
	// （无前缀），没有 job 标签重写的场景（如本地 curl 对比）就分不出来源。
	b = append(b, "# HELP service_info Constant 1 carrying the service name given to Init.\n"...)
	b = append(b, "# TYPE service_info gauge\n"...)
	b = append(b, fmt.Sprintf("service_info{service=%q} 1\n", r.service)...)

	// --- active_connections ---
	b = append(b, "# HELP active_connections Current number of in-flight HTTP requests.\n"...)
	b = append(b, "# TYPE active_connections gauge\n"...)
	b = append(b, fmt.Sprintf("active_connections %d\n", r.conn)...)

	// --- 基数熔断可观测（超限必须看得见，否则折叠等于悄悄丢数据）---
	b = append(b, "# HELP http_metrics_series Number of HTTP metric series currently allocated.\n"...)
	b = append(b, "# TYPE http_metrics_series gauge\n"...)
	b = append(b, fmt.Sprintf("http_metrics_series %d\n", len(r.httpSeries))...)
	b = append(b, "# HELP http_metrics_series_dropped_total HTTP requests folded into :other because the series cap was reached.\n"...)
	b = append(b, "# TYPE http_metrics_series_dropped_total counter\n"...)
	b = append(b, fmt.Sprintf("http_metrics_series_dropped_total %d\n", r.httpDropped)...)
	b = append(b, "# HELP business_metrics_series Number of business metric series currently allocated.\n"...)
	b = append(b, "# TYPE business_metrics_series gauge\n"...)
	b = append(b, fmt.Sprintf("business_metrics_series %d\n", len(r.bizSeries))...)
	b = append(b, "# HELP business_metrics_series_dropped_total Business metric writes folded because the series cap was reached.\n"...)
	b = append(b, "# TYPE business_metrics_series_dropped_total counter\n"...)
	b = append(b, fmt.Sprintf("business_metrics_series_dropped_total %d\n", r.bizDropped)...)

	// --- business_metrics（gauge）与 business_metrics_total（counter）---
	// 同一个 map 存两类，按键前缀 g:/c: 分流：以前只有 Set 语义，导致 9 处
	// "每次事件写 1" 的调用点产出的 *_total 其实是恒为 1 的 gauge（rate() 无意义）。
	b = append(b, "# HELP business_metrics Current value of custom business gauges.\n"...)
	b = append(b, "# TYPE business_metrics gauge\n"...)
	bizKeys := make([]string, 0, len(r.business))
	for k := range r.business {
		bizKeys = append(bizKeys, k)
	}
	sort.Strings(bizKeys)
	writeBiz := func(k string) {
		e := r.business[k]
		family := "business_metrics"
		if isCounterKey(k) {
			family = "business_metrics_total"
		}
		name := businessName(k)
		keys := make([]string, 0, len(e.labels))
		for lk := range e.labels {
			keys = append(keys, lk)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys)+1)
		parts = append(parts, fmt.Sprintf("name=%q", name))
		for _, lk := range keys {
			parts = append(parts, fmt.Sprintf("%s=%q", lk, e.labels[lk]))
		}
		b = append(b, fmt.Sprintf("%s{%s} %v\n", family, strings.Join(parts, ","), e.value)...)
	}
	for _, k := range bizKeys {
		if !isCounterKey(k) {
			writeBiz(k)
		}
	}
	b = append(b, "# HELP business_metrics_total Cumulative count of business events since process start.\n"...)
	b = append(b, "# TYPE business_metrics_total counter\n"...)
	for _, k := range bizKeys {
		if isCounterKey(k) {
			writeBiz(k)
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
// for every request passing through it, and keeps active_connections equal to
// the number of requests currently in flight.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		RecordActiveConnections(1)
		defer RecordActiveConnections(-1)
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
