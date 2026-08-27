// log-svc is the log microservice for OpsMesh.
// It provides gRPC APIs for log operations with support for multiple backends.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"opsmesh.io/log-svc/internal/server"
	"opsmesh.io/log-svc/internal/service"
	"opsmesh.io/log-svc/pkg/config"
	"opsmesh.io/log-svc/pkg/logstore"
)

func main() {
	cfg := config.DefaultConfig()

	// Override from environment if needed
	if addr := os.Getenv("LOG_SVC_GRPC_ADDR"); addr != "" {
		cfg.Server.Address = addr
	}
	if addr := os.Getenv("LOG_SVC_HEALTH_ADDR"); addr != "" {
		cfg.Health.Address = addr
	}
	if backend := os.Getenv("LOG_SVC_BACKEND"); backend != "" {
		cfg.LogStore.Backend = backend
	}

	// Initialize logstore backend
	store, err := initLogStore(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize log store: %v", err)
	}
	defer store.Close()

	// Create service
	svc := service.NewService(store)

	// Create gRPC server
	grpcServer := server.NewGRPCServer(svc)

	// Start gRPC listener
	lis, err := net.Listen("tcp", cfg.Server.Address)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", cfg.Server.Address, err)
	}

	// Start health check server
	healthServer := newHealthServer(cfg.Health.Address, store)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)

	// Start gRPC server in goroutine
	go func() {
		log.Printf("gRPC server starting on %s", cfg.Server.Address)
		if err := grpcServer.Serve(lis); err != nil {
			errCh <- fmt.Errorf("gRPC server error: %w", err)
		}
	}()

	// Start health server in goroutine
	go func() {
		log.Printf("Health check server starting on %s", cfg.Health.Address)
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("health server error: %w", err)
		}
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-sigCh:
		log.Printf("Received signal %v, shutting down...", sig)
	case err := <-errCh:
		log.Printf("Server error: %v", err)
	}

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	grpcServer.GracefulStop()
	_ = healthServer.Shutdown(shutdownCtx)

	log.Println("Server stopped gracefully")
}

// initLogStore creates a logstore backend based on configuration.
func initLogStore(cfg *config.Config) (logstore.LogStore, error) {
	switch cfg.LogStore.Backend {
	case "memory":
		if cfg.LogStore.Memory.EnableIndex {
			return logstore.NewMemoryWithIndex(cfg.LogStore.Memory.Capacity), nil
		}
		return logstore.NewMemory(cfg.LogStore.Memory.Capacity), nil

	case "sql":
		db, err := sql.Open("mysql", cfg.LogStore.SQL.DSN)
		if err != nil {
			return nil, fmt.Errorf("failed to open MySQL connection: %w", err)
		}
		db.SetMaxOpenConns(cfg.LogStore.SQL.MaxOpenConns)
		db.SetMaxIdleConns(cfg.LogStore.SQL.MaxIdleConns)
		db.SetConnMaxLifetime(cfg.LogStore.SQL.ConnMaxLifetime)

		// Test connection
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			return nil, fmt.Errorf("failed to ping MySQL: %w", err)
		}

		store, err := logstore.NewSQL(db)
		if err != nil {
			return nil, fmt.Errorf("failed to create SQL store: %w", err)
		}
		return store, nil

	case "loki":
		return logstore.NewLokiStore(cfg.LogStore.Loki.Endpoint), nil

	case "es":
		return logstore.NewESStore(cfg.LogStore.ES.Endpoint, cfg.LogStore.ES.Index), nil

	default:
		return nil, fmt.Errorf("unknown backend: %s", cfg.LogStore.Backend)
	}
}

// newHealthServer creates an HTTP health check server.
func newHealthServer(addr string, store logstore.LogStore) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// Check store health
		if store != nil {
			// Try a simple query to verify store is working
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			_, err := store.Query(ctx, logstore.Query{Limit: 1})
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprintf(w, `{"status":"not_ready","error":"%s"}`, err.Error())
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ready"}`)
	})

	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}
