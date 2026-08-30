package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	taskv1 "github.com/Levango7/OpsMesh/services/task-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/server"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/task-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/task-svc/pkg/config"
	"opsmesh/pkg/circuit"
	"opsmesh/pkg/compress"
	"opsmesh/pkg/metrics"
	"opsmesh/pkg/ratelimit"
	"opsmesh/pkg/trace"
)

func main() {
	cfg := config.Load()

	metrics.Init("task-svc")

	shutdown, err := trace.InitTracer("task-svc", cfg.OTelEndpoint)
	if err != nil {
		log.Fatalf("Failed to initialize tracer: %v", err)
	}
	defer shutdown(context.Background())

	// Store 初始化：StoreType=sql 且 DSN 非空时接 MySQL（自动建表）；失败或未配置回退内存。
	// MemoryStore / MySQLStore 均实现 TaskStore、ScheduleStore、ResultStore、BatchStore 四个接口。
	memStore := store.NewMemoryStore()
	var (
		ts store.TaskStore     = memStore
		ss store.ScheduleStore = memStore
		rs store.ResultStore   = memStore
		bs store.BatchStore    = memStore
	)
	if cfg.StoreType == "sql" && cfg.DSN != "" {
		if ms, err := store.NewMySQLStore(cfg.DSN); err != nil {
			log.Printf("MySQL store 初始化失败，回退 memory: %v", err)
		} else {
			ts, ss, rs, bs = ms, ms, ms, ms
			log.Printf("MySQL store 已启用")
			defer func() { _ = ms.Close() }()
		}
	}
	svc := service.NewService(ts, ss, rs, bs)

	cb := circuit.New("task-execution", 5, 30*time.Second)
	svc.SetCircuitBreaker(cb)

	srv := server.NewServer(svc)

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			ratelimit.GRPCInterceptor(),
		),
	)
	taskv1.RegisterTaskServiceServer(grpcServer, srv)
	taskv1.RegisterScheduleServiceServer(grpcServer, srv)
	taskv1.RegisterResultServiceServer(grpcServer, srv)
	taskv1.RegisterBatchServiceServer(grpcServer, srv)

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("opsmesh.task.v1.TaskService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.task.v1.ScheduleService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.task.v1.ResultService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.task.v1.BatchService", grpc_health_v1.HealthCheckResponse_SERVING)

	grpcLis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
	if err != nil {
		log.Fatalf("Failed to listen on gRPC port %d: %v", cfg.GRPCPort, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.Handle("/metrics", metrics.GetHandler())

	var handler http.Handler = mux
	handler = metrics.HTTPMiddleware(handler)
	handler = ratelimit.Middleware()(handler)
	handler = compress.Middleware()(handler)
	handler = trace.HTTPMiddleware("opsmesh/task-svc")(handler)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: handler,
	}

	go func() {
		log.Printf("Starting gRPC server on :%d", cfg.GRPCPort)
		if err := grpcServer.Serve(grpcLis); err != nil {
			log.Fatalf("gRPC server failed: %v", err)
		}
	}()

	go func() {
		log.Printf("Starting HTTP health server on :%d", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")

	healthServer.SetServingStatus("opsmesh.task.v1.TaskService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.task.v1.ScheduleService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.task.v1.ResultService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.task.v1.BatchService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	grpcServer.GracefulStop()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
