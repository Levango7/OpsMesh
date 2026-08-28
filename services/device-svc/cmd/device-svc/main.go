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
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	devicev1 "github.com/Levango7/OpsMesh/services/device-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/catalog"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/server"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/device-svc/pkg/config"
)

func main() {
	cfg := config.Load()

	st := store.NewMemoryStore()

	svc := service.NewService(st, st, st, st)
	srv := server.NewServer(svc)

	grpcServer := grpc.NewServer()
	devicev1.RegisterDeviceServiceServer(grpcServer, srv)
	devicev1.RegisterAgentServiceServer(grpcServer, srv)
	devicev1.RegisterCMDBServiceServer(grpcServer, srv)
	devicev1.RegisterDiscoveryServiceServer(grpcServer, srv)

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("opsmesh.device.v1.DeviceService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.device.v1.AgentService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.device.v1.CMDBService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("opsmesh.device.v1.DiscoveryService", grpc_health_v1.HealthCheckResponse_SERVING)

	grpcLis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
	if err != nil {
		log.Fatalf("Failed to listen on gRPC port %d: %v", cfg.GRPCPort, err)
	}

	cat := catalog.NewCatalog()
	seedCatalog(cat)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.HandleFunc("/api/v1/catalog/topology", func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.URL.Query().Get("tenantID")
		graph := cat.BuildTopology(tenantID)
		writeJSON(w, http.StatusOK, graph)
	})
	mux.HandleFunc("/api/v1/catalog/nodes/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/v1/catalog/nodes/"):]
		node, err := cat.GetNode(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, node)
	})
	mux.HandleFunc("/api/v1/catalog/relations/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/v1/catalog/relations/"):]
		rels, err := cat.GetRelations(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, rels)
	})
	mux.HandleFunc("/api/v1/catalog/impact/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/v1/catalog/impact/"):]
		impact, err := cat.GetImpactAnalysis(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, impact)
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

	healthServer.SetServingStatus("opsmesh.device.v1.DeviceService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.device.v1.AgentService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.device.v1.CMDBService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("opsmesh.device.v1.DiscoveryService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	grpcServer.GracefulStop()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func seedCatalog(c *catalog.Catalog) {
	c.AddNode(&catalog.CatalogNode{ID: "host-001", Name: "web-server-01", Type: "host", Status: "online", Metadata: map[string]string{"tenantID": "default"}})
	c.AddNode(&catalog.CatalogNode{ID: "svc-001", Name: "auth-service", Type: "service", Status: "running", Metadata: map[string]string{"tenantID": "default"}})
	c.AddNode(&catalog.CatalogNode{ID: "db-001", Name: "postgres-main", Type: "database", Status: "online", Metadata: map[string]string{"tenantID": "default"}})
	c.AddEdge(&catalog.CatalogEdge{From: "svc-001", To: "host-001", RelationType: "runs_on"})
	c.AddEdge(&catalog.CatalogEdge{From: "svc-001", To: "db-001", RelationType: "depends_on"})
}
