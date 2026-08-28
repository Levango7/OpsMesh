package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	configv1 "github.com/Levango7/OpsMesh/services/config-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/drift"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/rotation"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/server"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/config-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/config-svc/pkg/config"
)

func main() {
	cfg := config.Load()

	st := store.NewMemoryStore(cfg.EncryptionKey, cfg.MaxHistorySize)
	svc := service.NewService(st)
	srv := server.NewServer(svc)
	det := drift.NewDetector(st)
	rot := rotation.NewManager(st)

	grpcServer := grpc.NewServer()
	configv1.RegisterConfigServiceServer(grpcServer, srv)
	configv1.RegisterSecretServiceServer(grpcServer, srv)
	configv1.RegisterNotifyChannelServiceServer(grpcServer, srv)
	configv1.RegisterTemplateServiceServer(grpcServer, srv)

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("opsmesh.config.v1.ConfigService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.config.v1.SecretService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.config.v1.NotifyChannelService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.config.v1.TemplateService", grpc_health_v1.HealthCheckResponse_SERVING)

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

	registerDriftHandlers(mux, det)
	registerRotationHandlers(mux, rot)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: mux,
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

	healthServer.SetServingStatus("opsmesh.config.v1.ConfigService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.config.v1.SecretService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.config.v1.NotifyChannelService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.config.v1.TemplateService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	grpcServer.GracefulStop()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}

func registerDriftHandlers(mux *http.ServeMux, det *drift.Detector) {
	mux.HandleFunc("/api/v1/drift/rules", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var req struct {
				ConfigKey     string `json:"configKey"`
				ExpectedValue string `json:"expectedValue"`
				Comparison    string `json:"comparison"`
				TenantID      string `json:"tenantId"`
				Description   string `json:"description"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
				return
			}
			if req.ConfigKey == "" || req.TenantID == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "configKey and tenantId are required"})
				return
			}
			compType := drift.ComparisonType(req.Comparison)
			if compType == "" {
				compType = drift.ComparisonExact
			}
			rule := det.RegisterRule(req.ConfigKey, req.ExpectedValue, compType, req.TenantID, req.Description)
			writeJSON(w, http.StatusCreated, rule)

		case http.MethodGet:
			rules := det.ListRules()
			writeJSON(w, http.StatusOK, rules)

		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	})

	mux.HandleFunc("/api/v1/drift/rules/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/drift/rules/")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rule ID is required"})
			return
		}
		if !det.UnregisterRule(id) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	})

	mux.HandleFunc("/api/v1/drift/check", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		results, err := det.ScanAll()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, results)
	})

	mux.HandleFunc("/api/v1/drift/history", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		history := det.GetDriftHistory()
		writeJSON(w, http.StatusOK, history)
	})

	mux.HandleFunc("/api/v1/drift/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		status := det.GetStatus()
		writeJSON(w, http.StatusOK, status)
	})
}

func registerRotationHandlers(mux *http.ServeMux, rot *rotation.Manager) {
	mux.HandleFunc("/api/v1/rotation/policies", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var req struct {
				TenantID  string `json:"tenantId"`
				SecretKey string `json:"secretKey"`
				Interval  string `json:"interval"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
				return
			}
			if req.TenantID == "" || req.SecretKey == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenantId and secretKey are required"})
				return
			}
			interval := 24 * time.Hour
			if req.Interval != "" {
				d, err := time.ParseDuration(req.Interval)
				if err != nil {
					if hours, err2 := strconv.Atoi(req.Interval); err2 == nil {
						d = time.Duration(hours) * time.Hour
					} else {
						writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid interval format"})
						return
					}
				}
				interval = d
			}
			policy, err := rot.RegisterPolicy(req.TenantID, req.SecretKey, interval)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusCreated, policy)

		case http.MethodGet:
			policies := rot.ListPolicies()
			writeJSON(w, http.StatusOK, policies)

		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	})

	mux.HandleFunc("/api/v1/rotation/policies/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/rotation/policies/")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "policy ID is required"})
			return
		}
		if !rot.UnregisterPolicy(id) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "policy not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	})

	mux.HandleFunc("/api/v1/rotation/rotate/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/rotation/rotate/")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "policy ID is required"})
			return
		}
		result, err := rot.RotateSecret(id)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/v1/rotation/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		status := rot.GetStatus()
		writeJSON(w, http.StatusOK, status)
	})

	mux.HandleFunc("/api/v1/rotation/due", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		secrets := rot.ListSecretsDueForRotation()
		writeJSON(w, http.StatusOK, secrets)
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
