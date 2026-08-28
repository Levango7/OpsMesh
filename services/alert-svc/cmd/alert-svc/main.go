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

	alertv1 "github.com/Levango7/OpsMesh/services/alert-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/engine"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/notify"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/server"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/alert-svc/pkg/config"
	"opsmesh/pkg/circuit"
	"opsmesh/pkg/metrics"
	"opsmesh/pkg/security"
	"opsmesh/pkg/trace"
)

func main() {
	cfg := config.Load()

	metrics.Init("alert-svc")

	shutdown, err := trace.InitTracer("alert-svc", cfg.OTelEndpoint)
	if err != nil {
		log.Fatalf("Failed to initialize tracer: %v", err)
	}
	defer shutdown(context.Background())

	st := store.NewMemoryStore()
	eng := engine.NewEngine(nil)

	svc := service.NewService(eng, st)

	var cb *circuit.Breaker
	if cfg.PagerDutyEnabled {
		pdClient := notify.NewPagerDutyClient(cfg.PagerDutyRoutingKey, cfg.PagerDutyAPIURL, 10*time.Second, false)
		cb = circuit.New("pagerduty", 5, 30*time.Second)
		svc.SetNotifier(pdClient)
		svc.SetCircuitBreaker(cb)
		log.Printf("PagerDuty notifications enabled (routing key: %s)", maskRoutingKey(cfg.PagerDutyRoutingKey))
	}

	srv := server.NewServer(svc)

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(trace.GRPCServerInterceptor()),
	)
	alertv1.RegisterAlertServiceServer(grpcServer, srv)

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("opsmesh.alert.v1.AlertService", grpc_health_v1.HealthCheckResponse_SERVING)

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

	corsConfig := security.CORSConfig{
		AllowedOrigins: []string{"https://opsmesh.io", "https://app.opsmesh.io"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
		AllowedHeaders: []string{"Content-Type", "Authorization", "X-User-ID"},
	}

	var handler http.Handler = mux
	handler = metrics.HTTPMiddleware(handler)
	handler = security.SecurityHeadersMiddleware()(handler)
	handler = security.ConnectionLimit(100)(handler)
	handler = security.RequestSizeLimit(1 << 20)(handler)
	handler = security.IPRateLimit(60, time.Minute)(handler)
	handler = security.UserRateLimit(120, time.Minute)(handler)
	handler = corsConfig.Middleware()(handler)
	handler = trace.HTTPMiddleware("opsmesh/alert-svc")(handler)

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

	healthServer.SetServingStatus("opsmesh.alert.v1.AlertService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	grpcServer.GracefulStop()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}

// maskRoutingKey returns a masked version of the routing key for logging.
func maskRoutingKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
