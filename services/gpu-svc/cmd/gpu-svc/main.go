package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/version"
	applog "github.com/Levango7/OpsMesh/pkg/log"
	pkgmetrics "github.com/Levango7/OpsMesh/pkg/metrics"
	"github.com/Levango7/OpsMesh/pkg/trace"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/gpu"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/handler"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/metrics"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/node"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/ollama"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/quota"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/scheduler"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/workload"
	"github.com/Levango7/OpsMesh/services/gpu-svc/pkg/config"
)

func main() {
	// P1-6 结构化日志统一：把标准库 log 接入统一 JSON 管道（级别由 OPSMESH_LOG_LEVEL 控制）。
	// 必须最先调用——早于任何日志输出。
	lgr := applog.Init("gpu-svc")
	cfg := config.Load()

	pkgmetrics.Init("gpu-svc")

	shutdown, err := trace.InitTracer("gpu-svc", cfg.OTelEndpoint)
	if err != nil {
		lgr.Fatalf("Failed to initialize tracer: %v", err)
	}
	defer shutdown(context.Background())

	nvidiaDetector := gpu.NewNvidiaDetector()
	if nvidiaDetector.IsAvailable() {
		logx.Info(context.Background(), "NVIDIA GPU detector available - using real nvidia-smi data", "service", "gpu-svc")
		gpus, err := nvidiaDetector.DetectGPUs()
		if err != nil {
			logx.Warn(context.Background(), "nvidia-smi detection failed, falling back to simulated data", "service", "gpu-svc", "error", err)
		} else {
			logx.Info(context.Background(), "Detected NVIDIA GPUs", "service", "gpu-svc", "count", len(gpus))
			for _, g := range gpus {
				logx.Info(context.Background(), "Detected GPU", "service", "gpu-svc",
					"gpuID", g.ID, "name", g.Name, "memoryTotalMB", g.MemoryTotalMB, "driverVersion", g.DriverVersion)
			}
		}
	} else {
		logx.Warn(context.Background(), "nvidia-smi not found - using simulated GPU data", "service", "gpu-svc")
	}

	nodeMgr := node.NewManager(nil)
	sched := scheduler.NewScheduler(nil)
	wlMgr := workload.NewManager(nil)
	oc := ollama.NewClient(cfg.OllamaURL, 30*time.Second)
	if oc.IsAvailable() {
		logx.Info(context.Background(), "Ollama server reachable - using real HTTP API mode", "service", "gpu-svc", "url", cfg.OllamaURL)
	} else {
		logx.Warn(context.Background(), "Ollama server not reachable - falling back to simulated mode", "service", "gpu-svc", "url", cfg.OllamaURL)
	}
	qMgr := quota.NewManager(nil)
	collector := metrics.NewCollector(nil)

	svc := service.NewService(nodeMgr, sched, wlMgr, oc, qMgr, collector)
	h := handler.NewHandler(svc)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	// 版本面（TD-76）：与 controlplane 的 GET /version 对齐——不 exec 进
	// 容器即可确认实例版本。Dockerfile.service 的 -ldflags
	// -X internal/version.Version 注入此前是死注入（服务二进制不引用
	// internal/version，链接器无符号可改）；本端点使其生效。
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"service":   "gpu-svc",
			"version":   version.Version,
			"commit":    version.Commit,
			"date":      version.Date,
			"goVersion": runtime.Version(),
			"goos":      runtime.GOOS,
			"goarch":    runtime.GOARCH,
		})
	})
	mux.Handle("/metrics", pkgmetrics.GetHandler())

	var handler http.Handler = mux
	handler = pkgmetrics.HTTPMiddleware(handler)
	handler = trace.HTTPMiddleware("github.com/Levango7/OpsMesh/gpu-svc")(handler)

	httpServer := &http.Server{
		ReadHeaderTimeout: 15 * time.Second, // G112 Slowloris 防护
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           handler,
	}

	go func() {
		logx.Info(context.Background(), "Starting gpu-svc HTTP server", "service", "gpu-svc", "port", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			lgr.Fatalf("HTTP server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logx.Info(context.Background(), "Shutting down gpu-svc...", "service", "gpu-svc")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logx.Error(shutdownCtx, "HTTP server shutdown error", err, "service", "gpu-svc")
	}

	logx.Info(context.Background(), "gpu-svc stopped", "service", "gpu-svc")
}
