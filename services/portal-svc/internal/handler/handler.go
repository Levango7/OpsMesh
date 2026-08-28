package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/services/portal-svc/internal/cost"
	"github.com/Levango7/OpsMesh/services/portal-svc/internal/service"
)

// Handler handles HTTP requests for the portal.
type Handler struct {
	svc      *service.Service
	alloc    *cost.Allocator
}

// NewHandler creates a new Handler.
func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc, alloc: cost.NewAllocator()}
}

// RegisterRoutes registers all API routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Resource requests
	mux.HandleFunc("/api/v1/requests", h.handleRequests)
	mux.HandleFunc("/api/v1/requests/", h.handleRequestDetail)

	// Cost optimization
	mux.HandleFunc("/api/v1/cost/utilization", h.handleUtilization)
	mux.HandleFunc("/api/v1/cost/recommendations", h.handleRecommendations)
	mux.HandleFunc("/api/v1/cost/savings", h.handleSavings)
	mux.HandleFunc("/api/v1/cost/budget", h.handleBudget)

	// Cost allocation / chargeback
	mux.HandleFunc("/api/v1/cost/allocate", h.handleCostAllocate)
	mux.HandleFunc("/api/v1/cost/allocation-report", h.handleAllocationReport)
	mux.HandleFunc("/api/v1/cost/allocation-rules", h.handleAllocationRules)

	// Quota management
	mux.HandleFunc("/api/v1/quotas", h.handleQuotas)
	mux.HandleFunc("/api/v1/quotas/", h.handleQuotaDetail)

	// Dashboard
	mux.HandleFunc("/api/v1/dashboard/stats", h.handleDashboardStats)
	mux.HandleFunc("/api/v1/dashboard/activity", h.handleDashboardActivity)
}

// ============================================================================
// Resource Requests
// ============================================================================

func (h *Handler) handleRequests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createRequest(w, r)
	case http.MethodGet:
		h.listRequests(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleRequestDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/requests/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "request ID required")
		return
	}

	// Handle approve/reject sub-paths
	if strings.HasSuffix(path, "/approve") {
		id := strings.TrimSuffix(path, "/approve")
		if r.Method == http.MethodPost {
			h.approveRequest(w, r, id)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if strings.HasSuffix(path, "/reject") {
		id := strings.TrimSuffix(path, "/reject")
		if r.Method == http.MethodPost {
			h.rejectRequest(w, r, id)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	id := path
	switch r.Method {
	case http.MethodGet:
		h.getRequest(w, r, id)
	case http.MethodPut:
		h.updateRequest(w, r, id)
	case http.MethodDelete:
		h.cancelRequest(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

type createRequestInput struct {
	TenantID    string `json:"tenant_id"`
	Requester   string `json:"requester"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ResourceType string `json:"resource_type"`
	CPU         int    `json:"cpu"`
	MemoryGB    int    `json:"memory_gb"`
	StorageGB   int    `json:"storage_gb"`
}

func (h *Handler) createRequest(w http.ResponseWriter, r *http.Request) {
	var in createRequestInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req, err := h.svc.CreateRequest(in.TenantID, in.Requester, in.Title, in.Description, in.ResourceType, in.CPU, in.MemoryGB, in.StorageGB)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, req)
}

func (h *Handler) listRequests(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	status := r.URL.Query().Get("status")
	requests := h.svc.ListRequests(tenantID, status)
	writeJSON(w, http.StatusOK, requests)
}

func (h *Handler) getRequest(w http.ResponseWriter, r *http.Request, id string) {
	req, err := h.svc.GetRequest(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

type updateRequestInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	CPU         int    `json:"cpu"`
	MemoryGB    int    `json:"memory_gb"`
	StorageGB   int    `json:"storage_gb"`
}

func (h *Handler) updateRequest(w http.ResponseWriter, r *http.Request, id string) {
	var in updateRequestInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req, err := h.svc.UpdateRequest(id, in.Title, in.Description, in.CPU, in.MemoryGB, in.StorageGB)
	if err != nil {
		if err == service.ErrRequestNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) cancelRequest(w http.ResponseWriter, r *http.Request, id string) {
	req, err := h.svc.CancelRequest(id)
	if err != nil {
		if err == service.ErrRequestNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

type approvalInput struct {
	Approver string `json:"approver"`
	Note     string `json:"note"`
}

func (h *Handler) approveRequest(w http.ResponseWriter, r *http.Request, id string) {
	var in approvalInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req, err := h.svc.ApproveRequest(id, in.Approver, in.Note)
	if err != nil {
		if err == service.ErrRequestNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) rejectRequest(w http.ResponseWriter, r *http.Request, id string) {
	var in approvalInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req, err := h.svc.RejectRequest(id, in.Approver, in.Note)
	if err != nil {
		if err == service.ErrRequestNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// ============================================================================
// Cost Optimization
// ============================================================================

func (h *Handler) handleUtilization(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	u, err := h.svc.GetUtilization(tenantID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *Handler) handleRecommendations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	recs := h.svc.GetRecommendations(tenantID)
	writeJSON(w, http.StatusOK, recs)
}

func (h *Handler) handleSavings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	s, err := h.svc.GetSavingsAnalysis(tenantID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *Handler) handleBudget(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tenantID := r.URL.Query().Get("tenant_id")
		b, err := h.svc.GetBudget(tenantID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, b)
	case http.MethodPost:
		var in struct {
			TenantID       string  `json:"tenant_id"`
			MonthlyLimit   float64 `json:"monthly_limit"`
			AlertThreshold float64 `json:"alert_threshold"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		b, err := h.svc.SetBudget(in.TenantID, in.MonthlyLimit, in.AlertThreshold)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, b)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ============================================================================
// Quota Management
// ============================================================================

func (h *Handler) handleQuotas(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		quotas := h.svc.ListQuotas()
		writeJSON(w, http.StatusOK, quotas)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleQuotaDetail(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimPrefix(r.URL.Path, "/api/v1/quotas/")
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "tenantID required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Check if this is a usage request
		if strings.HasSuffix(tenantID, "/usage") {
			id := strings.TrimSuffix(tenantID, "/usage")
			usage, err := h.svc.GetQuotaUsage(id)
			if err != nil {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, usage)
			return
		}
		q, err := h.svc.GetQuota(tenantID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, q)
	case http.MethodPut:
		var in struct {
			MaxCPU      int `json:"max_cpu"`
			MaxMemoryGB int `json:"max_memory_gb"`
			MaxStorageGB int `json:"max_storage_gb"`
			MaxRequests int `json:"max_requests"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		q, err := h.svc.UpdateQuota(tenantID, in.MaxCPU, in.MaxMemoryGB, in.MaxStorageGB, in.MaxRequests)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, q)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ============================================================================
// Dashboard
// ============================================================================

func (h *Handler) handleDashboardStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	stats := h.svc.GetDashboardStats(tenantID)
	writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) handleDashboardActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	limit := 20
	activity := h.svc.GetRecentActivity(tenantID, limit)
	writeJSON(w, http.StatusOK, activity)
}

// ============================================================================
// Cost Allocation / Chargeback
// ============================================================================

func (h *Handler) handleCostAllocate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in struct {
		Dimension string          `json:"dimension"`
		TotalCost float64         `json:"total_cost"`
		Entries   []cost.CostEntry `json:"entries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	report, err := h.alloc.AllocateCosts(cost.Dimension(in.Dimension), in.TotalCost, in.Entries)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *Handler) handleAllocationReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	dimension := r.URL.Query().Get("dimension")
	totalCost := 0.0
	if v := r.URL.Query().Get("total_cost"); v != "" {
		fmt.Sscanf(v, "%f", &totalCost)
	}
	report, err := h.alloc.GetAllocationReport(cost.Dimension(dimension), totalCost, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *Handler) handleAllocationRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rules := h.alloc.GetAllocationRules()
		writeJSON(w, http.StatusOK, rules)
	case http.MethodPost:
		var rules []cost.AllocationRule
		if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		h.alloc.SetAllocationRules(rules)
		writeJSON(w, http.StatusCreated, map[string]string{"status": "rules updated"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ============================================================================
// Helpers
// ============================================================================

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
