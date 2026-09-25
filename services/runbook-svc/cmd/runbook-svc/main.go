package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	applog "github.com/Levango7/OpsMesh/pkg/log"
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

	st := store.NewMemoryStore()
	r := runner.NewRunner()
	svc := service.NewService(st, r)
	h := handler.NewHandler(svc)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: mux,
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
