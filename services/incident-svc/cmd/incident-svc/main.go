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
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/aggregate"
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/handler"
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/incident-svc/pkg/config"
)

func main() {
	// P1-6 结构化日志统一：把标准库 log 接入统一 JSON 管道（级别由 OPSMESH_LOG_LEVEL 控制）。
	// 必须最先调用——早于任何日志输出。
	lgr := applog.Init("incident-svc")
	cfg := config.Load()

	// Store 初始化：StoreType=sql 且 DSN 非空时接 MySQL（自动建表）；失败或未配置回退内存。
	var storeImpl models.IncidentStore = models.NewMemoryStore()
	if cfg.StoreType == "sql" && cfg.DSN != "" {
		if ms, err := store.NewMySQLStore(cfg.DSN); err != nil {
			// StoreType=sql 且 DSN 已显式配置 = 运维明确要求持久化存储。此时回退内存会让
			// 服务看起来正常（/health 仍 200）却在重启后丢光数据，属静默数据丢失陷阱；
			// 故直接阻断启动（对齐 controlplane --production 与 task-svc 的 fail-fast 策略）。
			lgr.Fatalf("MySQL store 初始化失败，停止启动: %v", err)
		} else {
			storeImpl = ms
			log.Printf("MySQL store 已启用")
		}
	}
	eng := aggregate.NewEngine(cfg.AggregationWindow)
	svc := service.NewService(storeImpl, eng)
	h := handler.NewHandler(svc)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: mux,
	}

	go func() {
		log.Printf("Starting incident-svc HTTP server on :%d", cfg.HTTPPort)
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
