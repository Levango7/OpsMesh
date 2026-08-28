package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/services/incident-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/incident-svc/internal/service"
)

// Handler provides HTTP handlers for the incident service.
type Handler struct {
	svc *service.Service
}

// NewHandler creates a new Handler.
func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers all HTTP routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/incidents", h.handleIncidents)
	mux.HandleFunc("/api/v1/incidents/", h.handleIncidentDetail)
	mux.HandleFunc("/api/v1/alerts/ingest", h.handleAlertIngest)
	mux.HandleFunc("/api/v1/metrics/response", h.handleMetrics)
	mux.HandleFunc("/api/v1/health", h.handleHealth)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleIncidents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createIncident(w, r)
	case http.MethodGet:
		h.listIncidents(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleIncidentDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/incidents/")
	if path == "" || path == "incidents" {
		h.handleIncidents(w, r)
		return
	}

	parts := strings.SplitN(path, "/", 2)
	id := parts[0]

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.getIncident(w, r, id)
		case http.MethodPut:
			h.updateIncident(w, r, id)
		case http.MethodDelete:
			h.deleteIncident(w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	sub := parts[1]
	switch sub {
	case "events":
		switch r.Method {
		case http.MethodPost:
			h.addTimelineEvent(w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case "timeline":
		switch r.Method {
		case http.MethodGet:
			h.getTimeline(w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case "resolve":
		switch r.Method {
		case http.MethodPost:
			h.resolveIncident(w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case "close":
		switch r.Method {
		case http.MethodPost:
			h.closeIncident(w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case "postmortem":
		switch r.Method {
		case http.MethodGet:
			h.getPostmortem(w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) createIncident(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Severity    string   `json:"severity"`
		DeviceIDs   []string `json:"device_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	severity := models.Severity(req.Severity)
	if severity == "" {
		severity = models.SeverityMedium
	}

	inc, err := h.svc.CreateIncident(req.Title, req.Description, severity, req.DeviceIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, inc)
}

func (h *Handler) listIncidents(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	severity := r.URL.Query().Get("severity")

	incidents := h.svc.ListIncidents(status, models.Severity(severity))
	writeJSON(w, http.StatusOK, incidents)
}

func (h *Handler) getIncident(w http.ResponseWriter, r *http.Request, id string) {
	inc, err := h.svc.GetIncident(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, inc)
}

func (h *Handler) updateIncident(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Assignee    string `json:"assignee"`
		Severity    string `json:"severity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	inc, err := h.svc.UpdateIncident(id, req.Title, req.Description, req.Assignee, models.Severity(req.Severity))
	if err != nil {
		if err == service.ErrIncidentNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, inc)
}

func (h *Handler) deleteIncident(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.svc.DeleteIncident(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) addTimelineEvent(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Type        string `json:"type"`
		Description string `json:"description"`
		Author      string `json:"author"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	ev, err := h.svc.AddTimelineEvent(id, req.Type, req.Description, req.Author)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, ev)
}

func (h *Handler) getTimeline(w http.ResponseWriter, r *http.Request, id string) {
	events, err := h.svc.GetTimeline(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (h *Handler) resolveIncident(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Author string `json:"author"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	inc, err := h.svc.ResolveIncident(id, req.Author)
	if err != nil {
		if err == service.ErrIncidentNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, inc)
}

func (h *Handler) closeIncident(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Author string `json:"author"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	inc, err := h.svc.CloseIncident(id, req.Author)
	if err != nil {
		if err == service.ErrIncidentNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, inc)
}

func (h *Handler) getPostmortem(w http.ResponseWriter, r *http.Request, id string) {
	pm, err := h.svc.GeneratePostmortem(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pm)
}

func (h *Handler) handleAlertIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var alert models.Alert
	if err := json.NewDecoder(r.Body).Decode(&alert); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	inc, err := h.svc.IngestAlert(&alert)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, inc)
}

func (h *Handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	metrics := h.svc.GetResponseMetrics()
	writeJSON(w, http.StatusOK, metrics)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
