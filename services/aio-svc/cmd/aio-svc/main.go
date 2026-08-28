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
	"strconv"
	"syscall"
	"time"

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
	port := envInt("AIO_SVC_PORT", 8100)
	readTimeout := envDuration("AIO_SVC_READ_TIMEOUT", 30*time.Second)
	writeTimeout := envDuration("AIO_SVC_WRITE_TIMEOUT", 60*time.Second)
	shutdownTimeout := envDuration("AIO_SVC_SHUTDOWN_TIMEOUT", 10*time.Second)

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
	promClient := prometheus.NewClient(os.Getenv("PROMETHEUS_URL"), 10*time.Second)
	if promClient.Available() {
		log.Printf("[aio-svc] Prometheus connected: %s", os.Getenv("PROMETHEUS_URL"))
	} else if os.Getenv("PROMETHEUS_URL") != "" {
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
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "engines": "5/5"})
	})

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
			AlertID string               `json:"alert_id"`
			Events  []rootcause.Event    `json:"events"`
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
			AlertID string              `json:"alert_id"`
			Window  int                 `json:"window_seconds"`
			States  []noise.AlertState  `json:"states"`
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
			"slope": result.Slope, "intercept": result.Intercept,
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
		writeJSON(w, http.StatusOK, map[string]interface{}{"samples": samples})
	})

	// Prometheus 节点内存查询。
	mux.HandleFunc("/api/v1/prometheus/memory", func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Query().Get("node_id")
		samples, err := promClient.GetMemoryUsage(nodeID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"samples": samples})
	})

	// Prometheus 节点磁盘查询。
	mux.HandleFunc("/api/v1/prometheus/disk", func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Query().Get("node_id")
		samples, err := promClient.GetDiskUsage(nodeID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"samples": samples})
	})

	// Prometheus GPU 利用率查询。
	mux.HandleFunc("/api/v1/prometheus/gpu", func(w http.ResponseWriter, r *http.Request) {
		samples, err := promClient.GetGPUUtilization()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"samples": samples})
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
		Handler:      mux,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	go func() {
		log.Printf("[aio-svc] AIOps 引擎启动 :%d (5 engines ready)", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[aio-svc] 启动失败: %v", err)
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

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func envInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
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
