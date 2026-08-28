package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/engine"
	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/handler"
	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/service"
	"github.com/Levango7/OpsMesh/services/workflow-svc/pkg/config"
)

// workflowStore is an in-memory store for workflow definitions.
type workflowStore struct {
	mu        sync.RWMutex
	workflows map[string]*models.Workflow
}

func newWorkflowStore() *workflowStore {
	return &workflowStore{
		workflows: make(map[string]*models.Workflow),
	}
}

func (s *workflowStore) CreateWorkflow(wf *models.Workflow) *models.Workflow {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workflows[wf.ID] = wf
	return wf
}

func (s *workflowStore) GetWorkflow(id string) *models.Workflow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.workflows[id]
}

func (s *workflowStore) ListWorkflows() []*models.Workflow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*models.Workflow, 0, len(s.workflows))
	for _, wf := range s.workflows {
		result = append(result, wf)
	}
	return result
}

func (s *workflowStore) UpdateWorkflow(wf *models.Workflow) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.workflows[wf.ID]; !ok {
		return false
	}
	s.workflows[wf.ID] = wf
	return true
}

func (s *workflowStore) DeleteWorkflow(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.workflows[id]; !ok {
		return false
	}
	delete(s.workflows, id)
	return true
}

func main() {
	cfg := config.Load()

	st := newWorkflowStore()
	e := engine.NewEngine()
	svc := service.NewService(st, e)
	h := handler.NewHandler(svc)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: mux,
	}

	go func() {
		log.Printf("Starting workflow-svc HTTP server on :%d", cfg.HTTPPort)
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
