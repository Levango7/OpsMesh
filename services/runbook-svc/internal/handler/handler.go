package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/service"
)

// Handler provides HTTP handlers for the runbook service.
type Handler struct {
	svc *service.Service
}

// NewHandler creates a new Handler.
func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers all HTTP routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/runbooks", h.handleRunbooks)
	mux.HandleFunc("/api/v1/runbooks/", h.handleRunbookDetail)
	mux.HandleFunc("/api/v1/trigger/webhook", h.handleWebhook)
	mux.HandleFunc("/api/v1/health", h.handleHealth)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleRunbooks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createRunbook(w, r)
	case http.MethodGet:
		h.listRunbooks(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleRunbookDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/runbooks/")
	if path == "" || path == "runbooks" {
		h.handleRunbooks(w, r)
		return
	}

	// Handle /api/v1/runbooks/{id}/trigger and /api/v1/runbooks/{id}/history
	if idx := strings.Index(path, "/"); idx > 0 {
		id := path[:idx]
		suffix := path[idx+1:]
		switch suffix {
		case "trigger":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			h.triggerRunbook(w, r, id)
		case "history":
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			h.getHistory(w, r, id)
		default:
			writeError(w, http.StatusNotFound, "not found")
		}
		return
	}

	// /api/v1/runbooks/{id}
	switch r.Method {
	case http.MethodGet:
		h.getRunbook(w, r, path)
	case http.MethodPut:
		h.updateRunbook(w, r, path)
	case http.MethodDelete:
		h.deleteRunbook(w, r, path)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) createRunbook(w http.ResponseWriter, r *http.Request) {
	var rb models.Runbook
	if err := json.NewDecoder(r.Body).Decode(&rb); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	created, err := h.svc.CreateRunbook(r.Context(), &rb)
	if err != nil {
		if err == service.ErrRunbookInvalid {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) listRunbooks(w http.ResponseWriter, r *http.Request) {
	runbooks, err := h.svc.ListRunbooks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, runbooks)
}

func (h *Handler) getRunbook(w http.ResponseWriter, r *http.Request, id string) {
	rb, err := h.svc.GetRunbook(r.Context(), id)
	if err != nil {
		if err == service.ErrRunbookNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rb)
}

func (h *Handler) updateRunbook(w http.ResponseWriter, r *http.Request, id string) {
	var rb models.Runbook
	if err := json.NewDecoder(r.Body).Decode(&rb); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	rb.ID = id

	updated, err := h.svc.UpdateRunbook(r.Context(), &rb)
	if err != nil {
		if err == service.ErrRunbookNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if err == service.ErrRunbookInvalid {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) deleteRunbook(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.svc.DeleteRunbook(r.Context(), id); err != nil {
		if err == service.ErrRunbookNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) triggerRunbook(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		TriggeredBy string `json:"triggered_by"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	record, err := h.svc.TriggerRunbook(r.Context(), id, body.TriggeredBy)
	if err != nil {
		switch err {
		case service.ErrRunbookNotFound:
			writeError(w, http.StatusNotFound, err.Error())
		case service.ErrRunbookDisabled:
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) getHistory(w http.ResponseWriter, r *http.Request, id string) {
	history, err := h.svc.GetHistory(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func (h *Handler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var payload models.WebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	records, err := h.svc.HandleWebhook(r.Context(), &payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Printf("Webhook triggered %d runbook(s) from %s", len(records), payload.Source)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"triggered": len(records),
		"records":   records,
	})
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
