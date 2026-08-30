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
	"strings"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	deployv1 "github.com/Levango7/OpsMesh/services/deploy-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/aiworkload"
	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/cloud"
	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/k8s"
	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/server"
	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/deploy-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/deploy-svc/pkg/config"
)

func main() {
	cfg := config.Load()

	// Initialize Kubernetes client with graceful fallback.
	k8sClient := k8s.NewClient(k8s.ClientConfig{})
	if k8sClient.IsConnected() {
		log.Println("[main] Kubernetes client connected — using real K8s API")
	} else {
		log.Println("[main] Kubernetes client in simulated mode — no cluster available")
	}

	// Store 初始化：StoreType=sql 且 DSN 非空时接 MySQL（自动建表）；失败或未配置回退内存。
	var st store.Store = store.NewMemoryStore()
	if cfg.StoreType == "sql" && cfg.DSN != "" {
		if ms, err := store.NewMySQLStore(cfg.DSN); err != nil {
			log.Printf("MySQL store 初始化失败，回退 memory: %v", err)
		} else {
			st = ms
			log.Printf("MySQL store 已启用")
		}
	}
	svc := service.NewService(st)
	srv := server.NewServer(svc)
	aiMgr := aiworkload.NewManager()

	// Pass K8s client to cloud providers for real API integration.
	cloud.SetK8sClient(k8sClient)

	grpcServer := grpc.NewServer()
	deployv1.RegisterDeploymentServiceServer(grpcServer, srv)
	deployv1.RegisterTemplateServiceServer(grpcServer, srv)
	deployv1.RegisterStrategyServiceServer(grpcServer, srv)
	deployv1.RegisterCanaryServiceServer(grpcServer, srv)

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("opsmesh.deploy.v1.DeploymentService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.deploy.v1.TemplateService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.deploy.v1.StrategyService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.deploy.v1.CanaryService", grpc_health_v1.HealthCheckResponse_SERVING)

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

	mux.HandleFunc("/api/v1/cloud/providers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"providers": cloud.ListProviders(),
		})
	})

	mux.HandleFunc("/api/v1/cloud/validate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var cfg cloud.DeploymentConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		p, err := cloud.NewProvider(cfg.Provider)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		if err := p.Validate(cfg); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid":    true,
			"provider": cfg.Provider,
		})
	})

	mux.HandleFunc("/api/v1/deployments/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/deployments/")
		parts := strings.SplitN(path, "/deploy-to/", 2)
		if len(parts) != 2 {
			http.Error(w, `{"error":"use /api/v1/deployments/{id}/deploy-to/{provider}"}`, http.StatusBadRequest)
			return
		}
		deploymentID := parts[0]
		providerType := parts[1]

		var cfg cloud.DeploymentConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		cfg.DeploymentID = deploymentID

		p, err := cloud.NewProvider(providerType)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		result, err := p.Deploy(cfg)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	// AI Workload endpoints

	mux.HandleFunc("/api/v1/ai-workloads", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var req struct {
				TenantID        string                     `json:"tenant_id"`
				Name            string                     `json:"name"`
				Type            string                     `json:"type"`
				ModelName       string                     `json:"model_name"`
				GPURequirements aiworkload.GPURequirements `json:"gpu_requirements"`
				Replicas        int                        `json:"replicas"`
				MaxReplicas     int                        `json:"max_replicas"`
				ContainerImage  string                     `json:"container_image"`
				EnvVars         map[string]string          `json:"env_vars"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %s"}`, err.Error()), http.StatusBadRequest)
				return
			}
			wl := &aiworkload.AIWorkload{
				TenantID:        req.TenantID,
				Name:            req.Name,
				Type:            req.Type,
				ModelName:       req.ModelName,
				GPURequirements: req.GPURequirements,
				Replicas:        req.Replicas,
				MaxReplicas:     req.MaxReplicas,
				ContainerImage:  req.ContainerImage,
				EnvVars:         req.EnvVars,
			}
			if wl.Replicas == 0 {
				wl.Replicas = 1
			}
			if wl.MaxReplicas < wl.Replicas {
				wl.MaxReplicas = wl.Replicas
			}
			deployed, err := aiMgr.Deploy(wl)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(deployed)

		case http.MethodGet:
			tenantID := r.URL.Query().Get("tenant_id")
			status := r.URL.Query().Get("status")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"workloads": aiMgr.List(tenantID, status),
			})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/v1/ai-workloads/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/ai-workloads/")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) < 1 || parts[0] == "" {
			http.Error(w, `{"error":"workload id required"}`, http.StatusBadRequest)
			return
		}
		id := parts[0]
		tenantID := r.URL.Query().Get("tenant_id")

		if len(parts) == 2 {
			switch parts[1] {
			case "scale":
				if r.Method != http.MethodPost {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
					return
				}
				var req struct {
					TargetReplicas int `json:"target_replicas"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %s"}`, err.Error()), http.StatusBadRequest)
					return
				}
				scaled, err := aiMgr.Scale(id, tenantID, req.TargetReplicas)
				if err != nil {
					http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(scaled)
				return

			case "logs":
				if r.Method != http.MethodGet {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
					return
				}
				logs, err := aiMgr.GetLogs(id, tenantID)
				if err != nil {
					http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"logs": logs})
				return

			default:
				http.Error(w, `{"error":"unknown sub-resource"}`, http.StatusBadRequest)
				return
			}
		}

		if r.Method != http.MethodGet && r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if r.Method == http.MethodDelete {
			stopped, err := aiMgr.Stop(id, tenantID)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(stopped)
			return
		}

		wl, err := aiMgr.GetStatus(id, tenantID)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(wl)
	})

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

	healthServer.SetServingStatus("opsmesh.deploy.v1.DeploymentService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.deploy.v1.TemplateService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.deploy.v1.StrategyService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.deploy.v1.CanaryService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	grpcServer.GracefulStop()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
