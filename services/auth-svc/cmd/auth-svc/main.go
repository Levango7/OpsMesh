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

	authv1 "github.com/Levango7/OpsMesh/services/auth-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/auth"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/server"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/auth-svc/pkg/config"
	"opsmesh/pkg/security"
	"opsmesh/pkg/tenant"
	"opsmesh/pkg/trace"
)

func main() {
	cfg := config.Load()

	shutdown, err := trace.InitTracer("auth-svc", cfg.OTelEndpoint)
	if err != nil {
		log.Fatalf("Failed to initialize tracer: %v", err)
	}
	defer shutdown(context.Background())

	// Store 初始化：StoreType=sql 且 DSN 非空时接 MySQL（自动建表）；失败或未配置回退内存。
	var st store.Store = store.NewMemoryStore()
	if cfg.StoreType == "sql" && cfg.DSN != "" {
		if ms, err := store.NewMySQLStore(cfg.DSN); err != nil {
			log.Printf("MySQL store 初始化失败，回退 memory: %v", err)
		} else {
			st = ms
			log.Printf("MySQL store 已启用")
			defer func() { _ = st.Close() }()
		}
	}
	eng := auth.NewEngine(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)

	svc := service.NewService(eng, st)
	srv := server.NewServer(svc)

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			tenant.GRPCInterceptor(),
			trace.GRPCServerInterceptor(),
		),
	)
	authv1.RegisterAuthServiceServer(grpcServer, srv)
	authv1.RegisterUserServiceServer(grpcServer, srv)
	authv1.RegisterRoleServiceServer(grpcServer, srv)
	authv1.RegisterPermissionServiceServer(grpcServer, srv)

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("opsmesh.auth.v1.AuthService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.auth.v1.UserService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.auth.v1.RoleService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.auth.v1.PermissionService", grpc_health_v1.HealthCheckResponse_SERVING)

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

	corsConfig := security.CORSConfig{
		AllowedOrigins: []string{"https://opsmesh.io", "https://app.opsmesh.io"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
		AllowedHeaders: []string{"Content-Type", "Authorization", "X-User-ID"},
	}

	var handler http.Handler = mux
	handler = security.SecurityHeadersMiddleware()(handler)
	handler = security.ConnectionLimit(100)(handler)
	handler = security.RequestSizeLimit(1 << 20)(handler)
	handler = security.IPRateLimit(60, time.Minute)(handler)
	handler = security.UserRateLimit(120, time.Minute)(handler)
	handler = corsConfig.Middleware()(handler)
	handler = tenant.Middleware(cfg.JWTSecret)(handler)
	handler = trace.HTTPMiddleware("opsmesh/auth-svc")(handler)

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
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

	healthServer.SetServingStatus("opsmesh.auth.v1.AuthService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.auth.v1.UserService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.auth.v1.RoleService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.auth.v1.PermissionService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	grpcServer.GracefulStop()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
