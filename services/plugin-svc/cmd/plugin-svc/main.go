package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Levango7/OpsMesh/services/plugin-svc/internal/handler"
	"github.com/Levango7/OpsMesh/services/plugin-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/plugin-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/plugin-svc/pkg/config"
)

func main() {
	cfg := config.Load()

	// Store 初始化：StoreType=sql 且 DSN 非空时接 MySQL（自动建表）；失败或未配置回退内存。
	var st store.PluginStore = store.NewMemoryStore()
	if cfg.StoreType == "sql" && cfg.DSN != "" {
		if ms, err := store.NewMySQLStore(cfg.DSN); err != nil {
			// StoreType=sql 且 DSN 已显式配置 = 运维明确要求持久化存储。此时回退内存会让
			// 服务看起来正常（/health 仍 200）却在重启后丢光数据，属静默数据丢失陷阱；
			// 故直接阻断启动（对齐 controlplane --production 与 task-svc 的 fail-fast 策略）。
			log.Fatalf("MySQL store 初始化失败，停止启动: %v", err)
		} else {
			st = ms
			log.Printf("MySQL store 已启用")
		}
	}
	svc := service.NewService(st)
	h := handler.NewHandler(svc)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: mux,
	}

	go func() {
		log.Printf("Starting plugin-svc HTTP server on :%d", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
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
