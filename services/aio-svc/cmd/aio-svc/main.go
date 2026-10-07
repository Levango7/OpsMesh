// aio-svc 提供 AIOps 智能引擎 HTTP API。
// 5 个核心引擎：异常检测、根因分析、告警降噪、预测告警、智能巡检。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/Levango7/OpsMesh/internal/version"
	applog "github.com/Levango7/OpsMesh/pkg/log"
	"github.com/Levango7/OpsMesh/pkg/metrics"
	"github.com/Levango7/OpsMesh/services/aio-svc/internal/anomaly"
	"github.com/Levango7/OpsMesh/services/aio-svc/internal/gpuanomaly"
	"github.com/Levango7/OpsMesh/services/aio-svc/internal/inspection"
	"github.com/Levango7/OpsMesh/services/aio-svc/internal/noise"
	"github.com/Levango7/OpsMesh/services/aio-svc/internal/prediction"
	"github.com/Levango7/OpsMesh/services/aio-svc/internal/prometheus"
	"github.com/Levango7/OpsMesh/services/aio-svc/internal/rootcause"
	"github.com/Levango7/OpsMesh/services/aio-svc/internal/slo"
)

func main() {
	// P1-6 结构化日志统一：把标准库 log 接入统一 JSON 管道（级别由 OPSMESH_LOG_LEVEL 控制）。
	// 必须最先调用——早于任何日志输出。
	lgr := applog.Init("aio-svc")
	port := envPort("AIO_SVC_HTTP_PORT", "AIO_SVC_PORT", 8100, lgr)
	readTimeout := envDuration("AIO_SVC_READ_TIMEOUT", 30*time.Second)
	writeTimeout := envDuration("AIO_SVC_WRITE_TIMEOUT", 60*time.Second)
	shutdownTimeout := envDuration("AIO_SVC_SHUTDOWN_TIMEOUT", 10*time.Second)

	metrics.Init("aio-svc")

	// 初始化 5 个引擎。
	detector := anomaly.NewDetector()
	analyzer := rootcause.NewAnalyzer()
	reducer := noise.NewReducer()
	predictor := prediction.NewPredictor()
	inspector := inspection.NewInspector()

	// 初始化 SLO 管理器并注册默认规则。
	sloManager := slo.NewManager()
	sloManager.AddRule(slo.SLORule{Name: "api-availability", Target: 99.9, Window: "30d", SLIType: slo.SLIAvailability})
	sloManager.AddRule(slo.SLORule{Name: "api-error-rate", Target: 99.0, Window: "7d", SLIType: slo.SLIErrorRate})
	sloManager.AddRule(slo.SLORule{Name: "api-latency", Target: 99.5, Window: "14d", SLIType: slo.SLILatency, Threshold: 200})

	gpuDetector := gpuanomaly.NewDetector()

	// Initialize Prometheus client with configurable URL.
	promURL := os.Getenv("PROMETHEUS_URL")
	promClient := prometheus.NewClient(promURL, 10*time.Second)
	if promClient.Available() {
		// G706：URL 来自 env，且 taint 分析无法被消毒函数说服——干脆不把
		// 地址打进日志（连接目标在配置里可见，日志只报状态）。
		log.Printf("[aio-svc] Prometheus connected")
	} else if promURL != "" {
		log.Printf("[aio-svc] Prometheus unreachable, using simulated mode")
	} else {
		log.Printf("[aio-svc] Prometheus disabled (set PROMETHEUS_URL to enable)")
	}

	mux := http.NewServeMux()

	// 健康检查。
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		// 这里原本写死 {"engines":"5/5"}：不查任何依赖、不看数据源，永远为真，
		// 于是"就绪"变成了一个装饰性字符串（#61 那一类"声明了但不成立"的对外面）。
		// 现在报的是能报的事实：五个引擎在本进程内确实实现了（结构事实），
		// 而它们能不能拿到真实指标取决于 Prometheus 是否可达——可达性如实标出来。
		// 消费方（compose/k8s 探针）只看状态码，改响应体不影响探针。
		src, sim := promClient.Source(), promClient.IsSimulated()
		note := "数据源为模拟：引擎结论来自推断，不是观测"
		if !sim {
			note = "数据源为真实 Prometheus 查询"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":              "ready",
			"engines_implemented": 5,
			"data_source":         src,
			"simulated":           sim,
			"note":                note,
		})
	})
	// 版本面（TD-76）：与 controlplane 的 GET /version 对齐——不 exec 进
	// 容器即可确认实例版本。Dockerfile.service 的 -ldflags
	// -X internal/version.Version 注入此前是死注入（服务二进制不引用
	// internal/version，链接器无符号可改）；本端点使其生效。
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"service":   "aio-svc",
			"version":   version.Version,
			"commit":    version.Commit,
			"date":      version.Date,
			"goVersion": runtime.Version(),
			"goos":      runtime.GOOS,
			"goarch":    runtime.GOARCH,
		})
	})
	mux.Handle("/metrics", metrics.GetHandler())

	// 异常检测。
	mux.HandleFunc("/api/v1/anomaly/detect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			DeviceID string    `json:"device_id"`
			Metric   string    `json:"metric"`
			Values   []float64 `json:"values"`
			Method   string    `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if req.Method == "" {
			req.Method = "zscore"
		}
		indices, scores := detector.Detect(req.Values, req.Method)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"device_id": req.DeviceID, "metric": req.Metric,
			"anomaly_indices": indices, "scores": scores, "method": req.Method,
		})
	})

	// 批量异常检测。
	mux.HandleFunc("/api/v1/anomaly/batch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var reqs []struct {
			DeviceID string    `json:"device_id"`
			Metric   string    `json:"metric"`
			Values   []float64 `json:"values"`
			Method   string    `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		results := make([]map[string]interface{}, 0, len(reqs))
		for _, req := range reqs {
			if req.Method == "" {
				req.Method = "zscore"
			}
			indices, scores := detector.Detect(req.Values, req.Method)
			results = append(results, map[string]interface{}{
				"device_id": req.DeviceID, "metric": req.Metric,
				"anomaly_indices": indices, "scores": scores, "method": req.Method,
			})
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"results": results})
	})

	// 根因分析。
	mux.HandleFunc("/api/v1/rootcause/analyze", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			AlertID string            `json:"alert_id"`
			Events  []rootcause.Event `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		result := analyzer.AnalyzeRootCause(req.AlertID, req.Events)
		writeJSON(w, http.StatusOK, result)
	})

	// 告警聚类。
	mux.HandleFunc("/api/v1/noise/cluster", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var alerts []noise.Alert
		if err := json.NewDecoder(r.Body).Decode(&alerts); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		clusters := reducer.ClusterAlerts(alerts)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"clusters": clusters, "original_count": len(alerts), "cluster_count": len(clusters),
		})
	})

	// 抖动检测。
	mux.HandleFunc("/api/v1/noise/flapping", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			AlertID string             `json:"alert_id"`
			Window  int                `json:"window_seconds"`
			States  []noise.AlertState `json:"states"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		window := time.Duration(req.Window) * time.Second
		if window == 0 {
			window = 5 * time.Minute
		}
		result := reducer.DetectFlapping(req.AlertID, window, req.States)
		writeJSON(w, http.StatusOK, result)
	})

	// 告警压缩。
	mux.HandleFunc("/api/v1/noise/compress", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var alerts []noise.Alert
		if err := json.NewDecoder(r.Body).Decode(&alerts); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		compressed, removed, kept, err := reducer.CompressAlerts(alerts)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"compressed": compressed, "removed": removed, "kept": kept,
		})
	})

	// 容量预测。
	mux.HandleFunc("/api/v1/prediction/capacity", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			DeviceID  string    `json:"device_id"`
			Metric    string    `json:"metric"`
			Values    []float64 `json:"values"`
			Horizon   int       `json:"horizon"`
			Threshold float64   `json:"threshold"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		result := predictor.PredictCapacity(req.Values, req.Horizon)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"device_id": req.DeviceID, "metric": req.Metric,
			"predicted_values": result.PredictedValues,
			"slope":            result.Slope, "intercept": result.Intercept,
			"r_squared": result.Rsquared, "horizon": req.Horizon,
		})
	})

	// 趋势预测。
	mux.HandleFunc("/api/v1/prediction/trend", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			DeviceID string    `json:"device_id"`
			Metric   string    `json:"metric"`
			Values   []float64 `json:"values"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		result := predictor.PredictTrend(req.Values)
		writeJSON(w, http.StatusOK, result)
	})

	// 智能巡检。
	mux.HandleFunc("/api/v1/inspection/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			TenantID  string   `json:"tenant_id"`
			DeviceIDs []string `json:"device_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		report := inspector.RunInspection(req.TenantID, req.DeviceIDs)
		writeJSON(w, http.StatusOK, report)
	})

	// 风险评分。
	mux.HandleFunc("/api/v1/inspection/risk", func(w http.ResponseWriter, r *http.Request) {
		deviceID := r.URL.Query().Get("device_id")
		tenantID := r.URL.Query().Get("tenant_id")
		score := inspector.GetRiskScore(tenantID, deviceID)
		writeJSON(w, http.StatusOK, score)
	})

	// SLO 评估。
	mux.HandleFunc("/api/v1/slo/evaluate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			GoodCount  int `json:"good_count"`
			TotalCount int `json:"total_count"`
			ErrorCount int `json:"error_count"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if req.TotalCount == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "total_count must be > 0"})
			return
		}
		results := sloManager.EvaluateAll(req.GoodCount, req.TotalCount, req.ErrorCount)
		writeJSON(w, http.StatusOK, map[string]interface{}{"results": results})
	})

	// SLO 状态总览。
	mux.HandleFunc("/api/v1/slo/status", func(w http.ResponseWriter, r *http.Request) {
		overview := sloManager.GetStatusOverview()
		writeJSON(w, http.StatusOK, overview)
	})

	// SLO 错误预算计算。
	mux.HandleFunc("/api/v1/slo/error-budget", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			CurrentValue float64 `json:"current_value"`
			Target       float64 `json:"target"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		budget := sloManager.CalculateErrorBudget(req.CurrentValue, req.Target)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"current_value": req.CurrentValue,
			"target":        req.Target,
			"error_budget":  budget,
		})
	})

	// SLO 消耗速率趋势。
	mux.HandleFunc("/api/v1/slo/burn-rate", func(w http.ResponseWriter, r *http.Request) {
		trends := sloManager.GetBurnRateTrends()
		writeJSON(w, http.StatusOK, map[string]interface{}{"burn_rate_trends": trends})
	})

	// GPU 异常检测。
	mux.HandleFunc("/api/v1/gpu/anomaly/detect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var metrics []gpuanomaly.GPUMetric
		if err := json.NewDecoder(r.Body).Decode(&metrics); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		anomalies := gpuDetector.FullGPUScan(metrics)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"anomalies": anomalies, "count": len(anomalies),
		})
	})

	// GPU 异常历史。
	mux.HandleFunc("/api/v1/gpu/anomaly/history", func(w http.ResponseWriter, r *http.Request) {
		history := gpuDetector.GetHistory()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"history": history, "count": len(history),
		})
	})

	// GPU 健康报告。
	mux.HandleFunc("/api/v1/gpu/health/", func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Path[len("/api/v1/gpu/health/"):]
		if nodeID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node_id is required"})
			return
		}
		report := gpuDetector.GetHealthReport(nodeID)
		writeJSON(w, http.StatusOK, report)
	})

	// GPU 指标摄入。
	mux.HandleFunc("/api/v1/gpu/metrics/ingest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var metrics []gpuanomaly.GPUMetric
		if err := json.NewDecoder(r.Body).Decode(&metrics); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		gpuDetector.IngestMetrics(metrics)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ingested": len(metrics), "status": "ok",
		})
	})

	// Prometheus 状态。
	mux.HandleFunc("/api/v1/prometheus/status", func(w http.ResponseWriter, r *http.Request) {
		status := map[string]interface{}{
			"available": promClient.Available(),
			"simulated": !promClient.Available(),
		}
		if url := os.Getenv("PROMETHEUS_URL"); url != "" {
			status["url"] = url
		}
		writeJSON(w, http.StatusOK, status)
	})

	// Prometheus 节点 CPU 查询。
	mux.HandleFunc("/api/v1/prometheus/cpu", func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Query().Get("node_id")
		samples, err := promClient.GetCPUUsage(nodeID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeSamples(w, promClient, samples)
	})

	// Prometheus 节点内存查询。
	mux.HandleFunc("/api/v1/prometheus/memory", func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Query().Get("node_id")
		samples, err := promClient.GetMemoryUsage(nodeID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeSamples(w, promClient, samples)
	})

	// Prometheus 节点磁盘查询。
	mux.HandleFunc("/api/v1/prometheus/disk", func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Query().Get("node_id")
		samples, err := promClient.GetDiskUsage(nodeID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeSamples(w, promClient, samples)
	})

	// Prometheus GPU 利用率查询。
	mux.HandleFunc("/api/v1/prometheus/gpu", func(w http.ResponseWriter, r *http.Request) {
		samples, err := promClient.GetGPUUtilization()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeSamples(w, promClient, samples)
	})

	// Prometheus 自定义 PromQL 查询。
	mux.HandleFunc("/api/v1/prometheus/query", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		result, err := promClient.Query(req.Query, time.Now())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      metrics.HTTPMiddleware(mux),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	go func() {
		log.Printf("[aio-svc] AIOps 引擎启动 :%d (5 engines ready)", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			lgr.Fatalf("[aio-svc] 启动失败: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[aio-svc] 优雅停机...")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("[aio-svc] 停机失败: %v", err)
	}
	log.Println("[aio-svc] 已停止")
}

// samplesResponse 是 /api/v1/prometheus/{cpu,memory,disk,gpu} 的统一响应形状。
//
// 为什么必须带 source/simulated（#61 造假面之三）：Prometheus 不可达或未配置
// PROMETHEUS_URL 时，client 会退回 simulatedXXX 生成的数值，而这些数值形状与真实
// 指标完全一样（时间序列、合理取值）。修前响应只有 {"samples":[...]}，调用方无从
// 区分"节点真实 CPU"和"客户端编出来的一条曲线"，前端拿它画容量趋势就是在画虚构。
type samplesResponse struct {
	Samples   []prometheus.MetricSample `json:"samples"`
	Source    string                    `json:"source"`
	Simulated bool                      `json:"simulated"`
	Note      string                    `json:"note,omitempty"`
}

// writeSamples 输出带来源标注的样本响应。判定只问 client，不在此处重复猜。
func writeSamples(w http.ResponseWriter, c *prometheus.Client, samples []prometheus.MetricSample) {
	resp := samplesResponse{Samples: samples, Source: c.Source(), Simulated: c.IsSimulated()}
	if resp.Simulated {
		resp.Note = "samples 由 aio-svc 生成（PROMETHEUS_URL 未配置或 Prometheus 不可达），不是观测值，勿用于容量与告警决策"
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// envPort 读取 HTTP 监听端口，规范键优先、历史键回退（TD-77）。
//
// 为什么需要它：本服务此前只读 AIO_SVC_PORT，而 compose 发布宿主端口用的键是
// AIO_SVC_HTTP_PORT——两个名字各管一头，改 .env 只动一半。统一到
// AIO_SVC_HTTP_PORT 后，键名与其余 11 个服务一致，宿主侧与容器侧同一个键。
//
// 兼容期保留旧键：命中旧键时打一条 deprecation 警告而不是静默忽略，
// 这样手工拼部署的人会立刻知道自己踩的是即将摘除的键。
func envPort(canonicalKey, legacyKey string, defaultVal int, lgr *applog.Logger) int {
	if v := os.Getenv(canonicalKey); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	if v := os.Getenv(legacyKey); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			lgr.Warn(context.Background(),
				fmt.Sprintf("环境变量 %s 已废弃，请改用 %s（值 %d 已生效）；该键将在下个版本摘除",
					legacyKey, canonicalKey, n))
			return n
		}
	}
	return defaultVal
}

func envDuration(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultVal
}
