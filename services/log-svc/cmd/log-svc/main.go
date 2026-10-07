// log-svc is the log microservice for OpsMesh.
// It provides gRPC APIs for log operations with support for multiple backends.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"opsmesh.io/log-svc/internal/server"
	"opsmesh.io/log-svc/internal/service"
	"opsmesh.io/log-svc/pkg/config"
	"opsmesh.io/log-svc/pkg/logstore"

	applog "github.com/Levango7/OpsMesh/pkg/log"
	"github.com/Levango7/OpsMesh/pkg/metrics"
)

// 版本信息（TD-76）：log-svc 模块路径是 opsmesh.io/log-svc（历史命名），
// 不在 github.com/Levango7/OpsMesh 路径树下，按 Go 的 internal 可见性
// 规则**不能**引用根模块的 internal/version——故本服务自带版本变量，
// 由 Dockerfile.service 的 -ldflags -X opsmesh.io/log-svc/cmd/log-svc.version
// 注入（源码直构时回默认值）。其余 11 个服务模块路径在 OpsMesh 树下，
// 共享根模块 internal/version。
var (
	version = "dev"
	commit  = "dev"
	date    = "unknown"
)

func main() {
	// P1-6 结构化日志统一：把标准库 log 接入统一 JSON 管道（级别由 OPSMESH_LOG_LEVEL 控制）。
	// 必须最先调用——早于任何日志输出。
	lgr := applog.Init("log-svc")
	cfg := config.DefaultConfig()

	// Override from environment if needed
	if addr := os.Getenv("LOG_SVC_GRPC_ADDR"); addr != "" {
		cfg.Server.Address = addr
	}
	cfg.Health.Address = resolveHealthAddr(cfg.Health.Address, lgr)
	if backend := os.Getenv("LOG_SVC_BACKEND"); backend != "" {
		cfg.LogStore.Backend = backend
	}
	// 后端端点：此前仅有 backend 可选而端点无任何环境变量入口，
	// 导致容器化部署下 loki/es 永远拿到空 endpoint、sql 拿到空 DSN。
	if v := os.Getenv("LOG_SVC_LOKI_ENDPOINT"); v != "" {
		cfg.LogStore.Loki.Endpoint = v
	}
	if v := os.Getenv("LOG_SVC_ES_ENDPOINT"); v != "" {
		cfg.LogStore.ES.Endpoint = v
	}
	if v := os.Getenv("LOG_SVC_ES_INDEX"); v != "" {
		cfg.LogStore.ES.Index = v
	}
	if v := os.Getenv("LOG_SVC_SQL_DSN"); v != "" {
		cfg.LogStore.SQL.DSN = v
	}

	metrics.Init("log-svc")

	// Initialize logstore backend
	store, err := initLogStore(cfg)
	if err != nil {
		lgr.Fatalf("Failed to initialize log store: %v", err)
	}
	defer store.Close()

	// Create service
	svc := service.NewService(store)

	// Create gRPC server
	grpcServer := server.NewGRPCServer(svc)

	// Start gRPC listener
	lis, err := net.Listen("tcp", cfg.Server.Address)
	if err != nil {
		lgr.Fatalf("Failed to listen on %s: %v", cfg.Server.Address, err)
	}

	// Start health check server
	healthServer := newHealthServer(cfg.Health.Address, store)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)

	// Start gRPC server in goroutine
	go func() {
		log.Printf("gRPC server starting on %s", cfg.Server.Address)
		if err := grpcServer.Serve(lis); err != nil {
			errCh <- fmt.Errorf("gRPC server error: %w", err)
		}
	}()

	// Start health server in goroutine
	go func() {
		log.Printf("Health check server starting on %s", cfg.Health.Address)
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("health server error: %w", err)
		}
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-sigCh:
		log.Printf("Received signal %v, shutting down...", sig)
	case err := <-errCh:
		log.Printf("Server error: %v", err)
	}

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	grpcServer.GracefulStop()
	_ = healthServer.Shutdown(shutdownCtx)

	log.Println("Server stopped gracefully")
}

// initLogStore creates a logstore backend based on configuration.
func initLogStore(cfg *config.Config) (logstore.LogStore, error) {
	switch cfg.LogStore.Backend {
	case "memory":
		if cfg.LogStore.Memory.EnableIndex {
			return logstore.NewMemoryWithIndex(cfg.LogStore.Memory.Capacity), nil
		}
		return logstore.NewMemory(cfg.LogStore.Memory.Capacity), nil

	case "sql":
		// ensureParseTime：自备 DSN 不保证带 parseTime=true，缺了 time 列扫描即报错，
		// 服务会静默退回内存存储（见 dsn.go 注释）。
		db, err := sql.Open("mysql", ensureParseTime(cfg.LogStore.SQL.DSN))
		if err != nil {
			return nil, fmt.Errorf("failed to open MySQL connection: %w", err)
		}
		db.SetMaxOpenConns(cfg.LogStore.SQL.MaxOpenConns)
		db.SetMaxIdleConns(cfg.LogStore.SQL.MaxIdleConns)
		db.SetConnMaxLifetime(cfg.LogStore.SQL.ConnMaxLifetime)

		// Test connection
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			return nil, fmt.Errorf("failed to ping MySQL: %w", err)
		}

		store, err := logstore.NewSQL(db)
		if err != nil {
			return nil, fmt.Errorf("failed to create SQL store: %w", err)
		}
		return store, nil

	case "loki":
		return logstore.NewLokiStore(cfg.LogStore.Loki.Endpoint), nil

	case "es":
		return logstore.NewESStore(cfg.LogStore.ES.Endpoint, cfg.LogStore.ES.Index), nil

	default:
		return nil, fmt.Errorf("unknown backend: %s", cfg.LogStore.Backend)
	}
}

// resolveHealthAddr 解析健康/指标 HTTP 监听地址，规范键优先、历史键回退（TD-77）。
//
// 为什么需要它：本服务此前只认 LOG_SVC_HEALTH_ADDR（地址形式 ":8105"），
// 而其余 11 个服务统一用 <NAME>_SVC_HTTP_PORT（纯端口）。统一后本服务也读
// LOG_SVC_HTTP_PORT，键名与其余服务一致，compose 的宿主侧与容器侧可用同一个键。
//
// 两个键语义不同——ADDR 是完整监听地址（可含 IP），PORT 是纯端口号，
// 所以这里不是简单改名，而是「PORT → ":PORT"」的转换。ADDR 键继续可用，
// 因为它额外承载了「只监听某个 IP」这种 PORT 表达不了的需求。
func resolveHealthAddr(fallback string, lgr *applog.Logger) string {
	if v := os.Getenv("LOG_SVC_HTTP_PORT"); v != "" {
		if _, err := strconv.Atoi(v); err == nil {
			return ":" + v
		}
		lgr.Warn(context.Background(),
			fmt.Sprintf("环境变量 LOG_SVC_HTTP_PORT=%q 不是合法端口号，已忽略并回退默认值", v))
	}
	if v := os.Getenv("LOG_SVC_HEALTH_ADDR"); v != "" {
		lgr.Warn(context.Background(),
			fmt.Sprintf("环境变量 LOG_SVC_HEALTH_ADDR 已废弃，请改用 LOG_SVC_HTTP_PORT（值 %s 已生效）；该键将在下个版本摘除", v))
		return v
	}
	return fallback
}

// newHealthServer creates an HTTP health check server.
//
// 路径口径（TD-77）：全仓微服务的规范路径是 /health（存活）+ /ready（就绪）。
// 本服务此前只提供 /healthz+/readyz，为避免打断既有外部探针，两者同时注册、
// 指向同一个 handler——别名不是拷贝实现，故两份路径永远同生同死。
// 旧路径计划在下个版本摘除，摘除前请先确认无外部依赖（见 docs/tech-debt.md TD-77）。
func newHealthServer(addr string, store logstore.LogStore) *http.Server {
	mux := http.NewServeMux()
	handleHealth := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	}
	handleReady := func(w http.ResponseWriter, r *http.Request) {
		// Check store health
		if store != nil {
			// Try a simple query to verify store is working
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			_, err := store.Query(ctx, logstore.Query{Limit: 1})
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprintf(w, `{"status":"not_ready","error":"%s"}`, err.Error())
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ready"}`)
	}
	// 规范路径。
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/ready", handleReady)
	// 兼容别名（TD-77 过渡期，指向同一 handler）。
	mux.HandleFunc("/healthz", handleHealth)
	mux.HandleFunc("/readyz", handleReady)
	// 版本面（TD-76）：与 controlplane 的 GET /version 对齐——不 exec 进
	// 容器即可确认实例版本。Dockerfile.service 的 -ldflags
	// -X opsmesh.io/log-svc/cmd/log-svc.version 注入此前是死注入
	// （服务二进制无版本符号，链接器无符号可改）；本端点使其生效。
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"service":   "log-svc",
			"version":   version,
			"commit":    commit,
			"date":      date,
			"goVersion": runtime.Version(),
			"goos":      runtime.GOOS,
			"goarch":    runtime.GOARCH,
		})
	})
	mux.Handle("/metrics", metrics.GetHandler())

	return &http.Server{
		Addr:              addr,
		Handler:           metrics.HTTPMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
}
