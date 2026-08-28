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
	cfg := config.Load()

	nodeMgr := node.NewManager(nil)
	sched := scheduler.NewScheduler(nil)
	wlMgr := workload.NewManager(nil)
	oc := ollama.NewClient(cfg.OllamaURL, 30*time.Second)
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

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: mux,
	}

	go func() {
		log.Printf("Starting gpu-svc HTTP server on :%d", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down gpu-svc...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("gpu-svc stopped")
}
