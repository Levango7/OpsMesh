package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	applog "github.com/Levango7/OpsMesh/pkg/log"
	"github.com/Levango7/OpsMesh/pkg/metrics"
	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/handler"
	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/runner"
	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/runbook-svc/pkg/config"
)

func main() {
	// P1-6 结构化日志统一：把标准库 log 接入统一 JSON 管道（级别由 OPSMESH_LOG_LEVEL 控制）。
	// 必须最先调用——早于任何日志输出。
	lgr := applog.Init("runbook-svc")
	cfg := config.Load()

	metrics.Init("runbook-svc")

	// 存储初始化：sql 模式任何失败都 fail-fast——声明了持久化却静默退回内存，
	// runbook 重启即丢（TD-68 同哲学）。
	var st store.RunbookStore = store.NewMemoryStore()
	if cfg.StoreType == "sql" {
		if cfg.DSN == "" {
			lgr.Fatalf("RUNBOOK_SVC_STORE_TYPE=sql 需要 RUNBOOK_SVC_DSN")
		}
		ms, err := store.NewMySQLStore(cfg.DSN)
		if err != nil {
			lgr.Fatalf("MySQL store 初始化失败，停止启动: %v", err)
		}
		st = ms
		log.Printf("MySQL store 已启用")
		defer func() { _ = ms.Close() }()
	}
	r := runner.NewRunner()
	svc := service.NewService(st, r)
	h := handler.NewHandler(svc)

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.GetHandler())
	h.RegisterRoutes(mux)

	httpServer := &http.Server{
		ReadHeaderTimeout: 15 * time.Second, // G112 Slowloris 防护
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           metrics.HTTPMiddleware(mux),
	}

	go func() {
		log.Printf("Starting runbook-svc HTTP server on :%d", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			lgr.Fatalf("HTTP server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
