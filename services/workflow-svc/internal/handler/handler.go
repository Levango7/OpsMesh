package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/service"
)

// Handler provides HTTP handlers for the workflow service.
type Handler struct {
	svc *service.Service
}

// NewHandler creates a new Handler.
func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers all HTTP routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/workflows", h.handleWorkflows)
	mux.HandleFunc("/api/v1/workflows/", h.handleWorkflowDetail)
	mux.HandleFunc("/api/v1/approvals/", h.handleApprovals)
	mux.HandleFunc("/api/v1/webhooks/", h.handleWebhooks)
	mux.HandleFunc("/api/v1/health", h.handleHealth)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createWorkflow(w, r)
	case http.MethodGet:
		h.listWorkflows(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleWorkflowDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/workflows/")
	if path == "" {
		h.handleWorkflows(w, r)
		return
	}

	// Handle /api/v1/workflows/{id}/execute and /api/v1/workflows/{id}/executions
	if idx := strings.Index(path, "/"); idx > 0 {
		id := path[:idx]
		suffix := path[idx+1:]
		switch suffix {
		case "execute":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			h.executeWorkflow(w, r, id)
		case "executions":
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			h.getExecutions(w, r, id)
		default:
			writeError(w, http.StatusNotFound, "not found")
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getWorkflow(w, r, path)
	case http.MethodPut:
		h.updateWorkflow(w, r, path)
	case http.MethodDelete:
		h.deleteWorkflow(w, r, path)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleApprovals(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/approvals/")
	if path == "" || !strings.Contains(path, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	idx := strings.Index(path, "/")
	id := path[:idx]
	action := path[idx+1:]

	switch action {
	case "approve":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.approveApproval(w, r, id)
	case "reject":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.rejectApproval(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) handleWebhooks(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/webhooks/")
	if path == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	h.receiveWebhook(w, r, path)
}

func (h *Handler) createWorkflow(w http.ResponseWriter, r *http.Request) {
	var wf models.Workflow
	if err := json.NewDecoder(r.Body).Decode(&wf); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	created, err := h.svc.CreateWorkflow(r.Context(), &wf)
	if err != nil {
		if err == service.ErrWorkflowInvalid {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) listWorkflows(w http.ResponseWriter, r *http.Request) {
	workflows, err := h.svc.ListWorkflows(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, workflows)
}

func (h *Handler) getWorkflow(w http.ResponseWriter, r *http.Request, id string) {
	wf, err := h.svc.GetWorkflow(r.Context(), id)
	if err != nil {
		if err == service.ErrWorkflowNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

func (h *Handler) updateWorkflow(w http.ResponseWriter, r *http.Request, id string) {
	var wf models.Workflow
	if err := json.NewDecoder(r.Body).Decode(&wf); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	wf.ID = id

	updated, err := h.svc.UpdateWorkflow(r.Context(), &wf)
	if err != nil {
		if err == service.ErrWorkflowNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if err == service.ErrWorkflowInvalid {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) deleteWorkflow(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.svc.DeleteWorkflow(r.Context(), id); err != nil {
		if err == service.ErrWorkflowNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) executeWorkflow(w http.ResponseWriter, r *http.Request, id string) {
	exec, err := h.svc.ExecuteWorkflow(r.Context(), id)
	if err != nil {
		switch err {
		case service.ErrWorkflowNotFound:
			writeError(w, http.StatusNotFound, err.Error())
		case service.ErrWorkflowInvalid:
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusAccepted, exec)
}

func (h *Handler) getExecutions(w http.ResponseWriter, r *http.Request, id string) {
	execs, err := h.svc.GetExecutions(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, execs)
}

func (h *Handler) approveApproval(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		ResolvedBy string `json:"resolved_by"`
		Comment    string `json:"comment"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if err := h.svc.ApproveApproval(r.Context(), id, body.ResolvedBy, body.Comment); err != nil {
		switch err {
		case service.ErrApprovalNotFound:
			writeError(w, http.StatusNotFound, err.Error())
		case service.ErrApprovalNotResolvable:
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	approval := h.svc.GetApproval(id)
	writeJSON(w, http.StatusOK, approval)
}

func (h *Handler) rejectApproval(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		ResolvedBy string `json:"resolved_by"`
		Comment    string `json:"comment"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if err := h.svc.RejectApproval(r.Context(), id, body.ResolvedBy, body.Comment); err != nil {
		switch err {
		case service.ErrApprovalNotFound:
			writeError(w, http.StatusNotFound, err.Error())
		case service.ErrApprovalNotResolvable:
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	approval := h.svc.GetApproval(id)
	writeJSON(w, http.StatusOK, approval)
}

func (h *Handler) receiveWebhook(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		ExecutionID string            `json:"execution_id"`
		NodeID      string            `json:"node_id"`
		Payload     map[string]string `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	cb, err := h.svc.HandleWebhookCallback(r.Context(), body.ExecutionID, body.NodeID, body.Payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Printf("Webhook callback received for execution %s node %s", body.ExecutionID, body.NodeID)
	writeJSON(w, http.StatusOK, cb)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("failed to encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
