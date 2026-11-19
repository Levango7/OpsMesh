// loadgen is a dependency-free HTTP load generator for the OpsMesh control plane.
//
// Usage:
//
//	go run scripts/loadtest/loadgen.go -base http://127.0.0.1:18080 -vus 50 -duration 60s
//
// SLO (same as scripts/loadtest/k6-baseline.js):
//
//	overall p99 < 300ms and error rate < 0.001 (2xx/3xx count as success)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	p99Limit     = 300 * time.Millisecond
	maxErrorRate = 0.001
)

type endpoint struct {
	name      string
	path      string
	protected bool
}

type epStat struct {
	durations []time.Duration
	status    map[int]int
	netErrs   int
	count     int
}

func newEpStat() *epStat {
	return &epStat{status: make(map[int]int)}
}

func (s *epStat) add(d time.Duration, status int, netErr bool) {
	s.count++
	s.durations = append(s.durations, d)
	if netErr {
		s.netErrs++
		return
	}
	s.status[status]++
}

func (s *epStat) failures() int {
	failed := s.netErrs
	for code, n := range s.status {
		if code >= 400 {
			failed += n
		}
	}
	return failed
}

type workerStats struct {
	per map[string]*epStat
}

func newWorkerStats(names []string) *workerStats {
	per := make(map[string]*epStat, len(names))
	for _, n := range names {
		per[n] = newEpStat()
	}
	return &workerStats{per: per}
}

func (w *workerStats) record(name string, d time.Duration, status int, netErr bool) {
	st := w.per[name]
	if st == nil {
		st = newEpStat()
		w.per[name] = st
	}
	st.add(d, status, netErr)
}

type epSummary struct {
	Endpoint        string         `json:"endpoint"`
	Count           int            `json:"count"`
	RPS             float64        `json:"rps"`
	P50Ms           float64        `json:"p50_ms"`
	P95Ms           float64        `json:"p95_ms"`
	P99Ms           float64        `json:"p99_ms"`
	Errors          int            `json:"errors"`
	ErrorRate       float64        `json:"error_rate"`
	StatusHistogram map[string]int `json:"status_histogram"`
}

type sloResult struct {
	P99MsLimit   float64  `json:"p99_ms_limit"`
	MaxErrorRate float64  `json:"max_error_rate"`
	Pass         bool     `json:"pass"`
	Reasons      []string `json:"reasons,omitempty"`
}

type summary struct {
	Base           string      `json:"base"`
	VUs            int         `json:"vus"`
	Duration       string      `json:"duration"`
	StartedAt      string      `json:"started_at"`
	ElapsedSeconds float64     `json:"elapsed_seconds"`
	Overall        epSummary   `json:"overall"`
	Endpoints      []epSummary `json:"endpoints"`
	SLO            sloResult   `json:"slo"`
}

func main() {
	base := flag.String("base", "http://127.0.0.1:18080", "target base URL")
	tenant := flag.String("tenant", "default", "X-Tenant-ID header for protected endpoints")
	token := flag.String("token", "", "optional bearer token")
	vus := flag.Int("vus", 50, "number of concurrent virtual users")
	duration := flag.Duration("duration", 60*time.Second, "total run duration")
	timeout := flag.Duration("timeout", 5*time.Second, "per-request timeout")
	think := flag.Duration("think", time.Second, "pause between endpoint sweeps (k6 parity; 0 = unthrottled)")
	flag.Parse()

	if *vus <= 0 || *duration <= 0 || *timeout <= 0 || *think < 0 {
		fmt.Fprintln(os.Stderr, "loadgen: -vus, -duration and -timeout must be positive; -think must be >= 0")
		os.Exit(2)
	}
	baseURL := strings.TrimRight(*base, "/")
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		fmt.Fprintf(os.Stderr, "loadgen: invalid -base %q: need absolute http(s) URL\n", *base)
		os.Exit(2)
	}

	endpoints := []endpoint{
		{name: "healthz", path: "/healthz"},
		{name: "readyz", path: "/readyz"},
		{name: "devices", path: "/api/v1/devices?page=1&pageSize=20", protected: true},
		{name: "tasks", path: "/api/v1/tasks?page=1&pageSize=20", protected: true},
		{name: "tenants", path: "/api/v1/tenants?page=1&pageSize=20", protected: true},
	}
	names := make([]string, 0, len(endpoints)+1)
	for _, ep := range endpoints {
		names = append(names, ep.name)
	}
	names = append(names, "overall")

	client := newClient(*vus, *timeout)
	defer client.CloseIdleConnections()

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runCtx, cancel := context.WithTimeout(sigCtx, *duration)
	defer cancel()

	started := time.Now()
	results := make([]*workerStats, *vus)
	var wg sync.WaitGroup
	wg.Add(*vus)
	for i := 0; i < *vus; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = runWorker(runCtx, client, baseURL, endpoints, *timeout, *think, *tenant, *token)
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(started)

	merged := mergeStats(results, names)
	overall := summarize("overall", merged["overall"], elapsed)
	perEndpoint := make([]epSummary, 0, len(endpoints))
	for _, ep := range endpoints {
		perEndpoint = append(perEndpoint, summarize(ep.name, merged[ep.name], elapsed))
	}

	slo := sloResult{P99MsLimit: p99Limit.Seconds() * 1000, MaxErrorRate: maxErrorRate, Pass: true}
	if overall.P99Ms >= slo.P99MsLimit {
		slo.Pass = false
		slo.Reasons = append(slo.Reasons, fmt.Sprintf("overall p99 %.2fms >= %.0fms", overall.P99Ms, slo.P99MsLimit))
	}
	if overall.ErrorRate >= maxErrorRate {
		slo.Pass = false
		slo.Reasons = append(slo.Reasons, fmt.Sprintf("overall error rate %.5f >= %.3f", overall.ErrorRate, maxErrorRate))
	}

	printMarkdown(perEndpoint, overall)
	out := summary{
		Base:           baseURL,
		VUs:            *vus,
		Duration:       duration.String(),
		StartedAt:      started.UTC().Format(time.RFC3339),
		ElapsedSeconds: round(elapsed.Seconds(), 3),
		Overall:        overall,
		Endpoints:      perEndpoint,
		SLO:            slo,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	fmt.Println("\n## JSON summary")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "loadgen: encode summary: %v\n", err)
		os.Exit(2)
	}

	if !slo.Pass {
		os.Exit(1)
	}
}

func newClient(vus int, timeout time.Duration) *http.Client {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          vus * 4,
		MaxIdleConnsPerHost:   vus * 2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &http.Client{Transport: tr}
}

func runWorker(ctx context.Context, client *http.Client, base string, endpoints []endpoint, timeout, think time.Duration, tenant, token string) *workerStats {
	names := make([]string, 0, len(endpoints)+1)
	for _, ep := range endpoints {
		names = append(names, ep.name)
	}
	names = append(names, "overall")
	stats := newWorkerStats(names)

	for ctx.Err() == nil {
		for _, ep := range endpoints {
			if ctx.Err() != nil {
				return stats
			}
			d, status, netErr := doRequest(ctx, client, base, ep, timeout, tenant, token)
			if ctx.Err() != nil {
				return stats
			}
			stats.record(ep.name, d, status, netErr)
			stats.record("overall", d, status, netErr)
		}
		if think > 0 {
			timer := time.NewTimer(think)
			select {
			case <-ctx.Done():
				timer.Stop()
				return stats
			case <-timer.C:
			}
		}
	}
	return stats
}

func doRequest(ctx context.Context, client *http.Client, base string, ep endpoint, timeout time.Duration, tenant, token string) (time.Duration, int, bool) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base+ep.path, nil)
	if err != nil {
		return 0, 0, true
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	if ep.protected {
		if tenant != "" {
			req.Header.Set("X-Tenant-ID", tenant)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return time.Since(start), 0, true
	}
	_, copyErr := io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	d := time.Since(start)
	if copyErr != nil {
		return d, resp.StatusCode, true
	}
	return d, resp.StatusCode, false
}

func mergeStats(results []*workerStats, names []string) map[string]*epStat {
	merged := make(map[string]*epStat, len(names))
	for _, n := range names {
		merged[n] = newEpStat()
	}
	for _, ws := range results {
		if ws == nil {
			continue
		}
		for name, src := range ws.per {
			dst := merged[name]
			if dst == nil {
				dst = newEpStat()
				merged[name] = dst
			}
			dst.count += src.count
			dst.netErrs += src.netErrs
			dst.durations = append(dst.durations, src.durations...)
			for code, n := range src.status {
				dst.status[code] += n
			}
		}
	}
	return merged
}

func summarize(name string, st *epStat, elapsed time.Duration) epSummary {
	if st == nil {
		st = newEpStat()
	}
	sorted := append([]time.Duration(nil), st.durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	secs := elapsed.Seconds()
	if secs <= 0 {
		secs = 1
	}
	hist := make(map[string]int, len(st.status)+1)
	for code, n := range st.status {
		hist[fmt.Sprintf("%d", code)] = n
	}
	if st.netErrs > 0 {
		hist["network_error"] = st.netErrs
	}
	failures := st.failures()
	rate := 0.0
	if st.count > 0 {
		rate = float64(failures) / float64(st.count)
	}
	return epSummary{
		Endpoint:        name,
		Count:           st.count,
		RPS:             round(float64(st.count)/secs, 2),
		P50Ms:           round(durationMs(percentile(sorted, 0.50)), 3),
		P95Ms:           round(durationMs(percentile(sorted, 0.95)), 3),
		P99Ms:           round(durationMs(percentile(sorted, 0.99)), 3),
		Errors:          failures,
		ErrorRate:       round(rate, 6),
		StatusHistogram: hist,
	}
}

func percentile(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(q*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func durationMs(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func round(v float64, digits int) float64 {
	scale := math.Pow(10, float64(digits))
	return math.Round(v*scale) / scale
}

func printMarkdown(perEndpoint []epSummary, overall epSummary) {
	fmt.Println("## Markdown row")
	fmt.Println("| endpoint | count | rps | p50 (ms) | p95 (ms) | p99 (ms) | error rate | status codes |")
	fmt.Println("|---|---|---|---|---|---|---|---|")
	for _, s := range perEndpoint {
		printRow(s)
	}
	printRow(overall)
}

func printRow(s epSummary) {
	fmt.Printf("| %s | %d | %.2f | %.2f | %.2f | %.2f | %.4f | %s |\n",
		s.Endpoint, s.Count, s.RPS, s.P50Ms, s.P95Ms, s.P99Ms, s.ErrorRate, formatHistogram(s.StatusHistogram))
}

func formatHistogram(hist map[string]int) string {
	keys := make([]string, 0, len(hist))
	for k := range hist {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", k, hist[k]))
	}
	return strings.Join(parts, " ")
}
