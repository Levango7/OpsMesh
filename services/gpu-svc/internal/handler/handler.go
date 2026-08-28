package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/service"
)

// Handler handles HTTP requests for the GPU service.
type Handler struct {
	svc *service.Service
}

// NewHandler creates a new HTTP handler.
func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers all HTTP routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// GPU Node Management
	mux.HandleFunc("/api/v1/gpu/nodes", h.handleNodes)
	mux.HandleFunc("/api/v1/gpu/nodes/", h.handleNodeDetail)

	// GPU Resources
	mux.HandleFunc("/api/v1/gpu/resources", h.handleResources)
	mux.HandleFunc("/api/v1/gpu/resources/per-node", h.handlePerNodeResources)

	// GPU Metrics
	mux.HandleFunc("/api/v1/gpu/metrics/", h.handleMetrics)

	// Workload Management
	mux.HandleFunc("/api/v1/gpu/workloads", h.handleWorkloads)
	mux.HandleFunc("/api/v1/gpu/workloads/", h.handleWorkloadDetail)

	// Scheduling
	mux.HandleFunc("/api/v1/gpu/schedule", h.handleSchedule)
	mux.HandleFunc("/api/v1/gpu/schedule/policies", h.handleSchedulePolicies)
	mux.HandleFunc("/api/v1/gpu/schedule/queue", h.handleScheduleQueue)

	// Model Management
	mux.HandleFunc("/api/v1/gpu/models", h.handleModels)
	mux.HandleFunc("/api/v1/gpu/models/", h.handleModelDetail)

	// Quota
	mux.HandleFunc("/api/v1/gpu/quotas", h.handleQuotas)
	mux.HandleFunc("/api/v1/gpu/quotas/", h.handleQuotaDetail)
}

func (h *Handler) handleNodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var node models.GPUNode
		if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		registered, err := h.svc.RegisterNode(&node)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, registered)

	case http.MethodGet:
		nodes := h.svc.ListNodes()
		writeJSON(w, http.StatusOK, nodes)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleNodeDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/gpu/nodes/")
	if path == "" || path == "health" {
		writeError(w, http.StatusBadRequest, "node ID required")
		return
	}

	// Handle /api/v1/gpu/nodes/{id}/health
	if strings.HasSuffix(path, "/health") {
		id := strings.TrimSuffix(path, "/health")
		health, err := h.svc.GetNodeHealth(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, health)
		return
	}

	id := path
	switch r.Method {
	case http.MethodGet:
		node, err := h.svc.GetNode(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, node)

	case http.MethodPut:
		var node models.GPUNode
		if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		node.ID = id
		updated, err := h.svc.UpdateNode(&node)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)

	case http.MethodDelete:
		if err := h.svc.UnregisterNode(id); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	summary := h.svc.GetResourceSummary()
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) handlePerNodeResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	resources := h.svc.GetNodeResources()
	writeJSON(w, http.StatusOK, resources)
}

func (h *Handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	nodeID := strings.TrimPrefix(r.URL.Path, "/api/v1/gpu/metrics/")
	gpuCount := 4
	if gc := r.URL.Query().Get("gpu_count"); gc != "" {
		if n, err := strconv.Atoi(gc); err == nil {
			gpuCount = n
		}
	}
	metrics, err := h.svc.GetGPUMetrics(nodeID, gpuCount)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (h *Handler) handleWorkloads(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var wl models.Workload
		if err := json.NewDecoder(r.Body).Decode(&wl); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		submitted, err := h.svc.SubmitWorkload(&wl)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, submitted)

	case http.MethodGet:
		status := r.URL.Query().Get("status")
		workloads := h.svc.ListWorkloads(status)
		writeJSON(w, http.StatusOK, workloads)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleWorkloadDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/gpu/workloads/")

	// Handle /api/v1/gpu/workloads/{id}/scale
	if strings.HasSuffix(path, "/scale") {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		id := strings.TrimSuffix(path, "/scale")
		var req struct {
			Replicas int `json:"replicas"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := h.svc.ScaleWorkload(id, req.Replicas); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	id := path
	switch r.Method {
	case http.MethodGet:
		wl, err := h.svc.GetWorkload(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, wl)

	case http.MethodPut:
		var wl models.Workload
		if err := json.NewDecoder(r.Body).Decode(&wl); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		wl.ID = id
		updated, err := h.svc.UpdateWorkload(&wl)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)

	case http.MethodDelete:
		if err := h.svc.CancelWorkload(id); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	results := h.svc.TriggerScheduling()
	writeJSON(w, http.StatusOK, results)
}

func (h *Handler) handleSchedulePolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		policies := h.svc.GetSchedulingPolicies()
		writeJSON(w, http.StatusOK, policies)

	case http.MethodPut:
		var policies []models.SchedulingPolicy
		if err := json.NewDecoder(r.Body).Decode(&policies); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := h.svc.SetSchedulingPolicies(policies); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusOK)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleScheduleQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	queue := h.svc.GetScheduleQueue()
	writeJSON(w, http.StatusOK, queue)
}

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		model, err := h.svc.PullModel(req.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, model)

	case http.MethodGet:
		models := h.svc.ListModels()
		writeJSON(w, http.StatusOK, models)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleModelDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/gpu/models/")

	// Handle /api/v1/gpu/models/{name}/serve
	if strings.HasSuffix(path, "/serve") {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		name := strings.TrimSuffix(path, "/serve")
		var req struct {
			NodeID   string `json:"node_id"`
			Port     int    `json:"port"`
			Replicas int    `json:"replicas"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		model, err := h.svc.ServeModel(name, req.NodeID, req.Port, req.Replicas)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, model)
		return
	}

	// Handle /api/v1/gpu/models/{name}/status
	if strings.HasSuffix(path, "/status") {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		name := strings.TrimSuffix(path, "/status")
		status, err := h.svc.GetModelStatus(name)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
		return
	}

	name := path
	switch r.Method {
	case http.MethodDelete:
		if err := h.svc.RemoveModel(name); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleQuotas(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		quotas := h.svc.ListQuotas()
		writeJSON(w, http.StatusOK, quotas)

	case http.MethodPost:
		var q models.GPUQuota
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := h.svc.SetQuota(&q); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, q)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleQuotaDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID := strings.TrimPrefix(r.URL.Path, "/api/v1/gpu/quotas/")
	usage, err := h.svc.GetQuotaUsage(tenantID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
