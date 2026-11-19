package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/escalation"
)

// EscalationHandler provides HTTP handlers for escalation and on-call endpoints.
type EscalationHandler struct {
	escalator *escalation.Escalator
}

// NewEscalationHandler creates a new EscalationHandler.
func NewEscalationHandler(esc *escalation.Escalator) *EscalationHandler {
	return &EscalationHandler{escalator: esc}
}

// RegisterRoutes registers all escalation HTTP routes on the given mux.
func (h *EscalationHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/escalation/policies", h.handlePolicies)
	mux.HandleFunc("/api/v1/escalation/active", h.handleActiveEscalations)
	mux.HandleFunc("/api/v1/oncall/schedules", h.handleSchedules)
	mux.HandleFunc("/api/v1/oncall/current", h.handleOnCallCurrent)
}

func (h *EscalationHandler) handlePolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createPolicy(w, r)
	case http.MethodGet:
		h.listPolicies(w, r)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *EscalationHandler) createPolicy(w http.ResponseWriter, r *http.Request) {
	var policy escalation.EscalationPolicy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.escalator.AddPolicy(&policy); err != nil {
		logx.Error(r.Context(), "Failed to create escalation policy", err, "service", "alert-svc", "tenantID", policy.TenantID)
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, policy)
}

func (h *EscalationHandler) listPolicies(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	policies := h.escalator.ListPolicies(tenantID)
	if policies == nil {
		policies = []*escalation.EscalationPolicy{}
	}
	writeJSON(w, http.StatusOK, policies)
}

func (h *EscalationHandler) handleActiveEscalations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	escalations := h.escalator.ListActiveEscalations()
	if escalations == nil {
		escalations = []*escalation.EscalationState{}
	}
	writeJSON(w, http.StatusOK, escalations)
}

func (h *EscalationHandler) handleSchedules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createSchedule(w, r)
	case http.MethodGet:
		h.listSchedules(w, r)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *EscalationHandler) createSchedule(w http.ResponseWriter, r *http.Request) {
	var schedule escalation.OnCallSchedule
	if err := json.NewDecoder(r.Body).Decode(&schedule); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.escalator.AddSchedule(&schedule); err != nil {
		logx.Error(r.Context(), "Failed to create on-call schedule", err, "service", "alert-svc", "tenantID", schedule.TenantID)
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, schedule)
}

func (h *EscalationHandler) listSchedules(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	schedules := h.escalator.ListSchedules(tenantID)
	if schedules == nil {
		schedules = []*escalation.OnCallSchedule{}
	}
	writeJSON(w, http.StatusOK, schedules)
}

func (h *EscalationHandler) handleOnCallCurrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	scheduleID := r.URL.Query().Get("schedule_id")
	tenantID := r.URL.Query().Get("tenant_id")
	at := time.Now()

	var entry *escalation.OnCallEntry
	if scheduleID != "" {
		entry = h.escalator.GetOnCall(scheduleID, at)
	} else if tenantID != "" {
		entry = h.escalator.GetOnCallForTenant(tenantID, at)
	} else {
		writeJSONError(w, http.StatusBadRequest, "schedule_id or tenant_id required")
		return
	}

	if entry == nil {
		writeJSONError(w, http.StatusNotFound, "no on-call found")
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logx.Error(context.Background(), "Failed to encode JSON response", err, "service", "alert-svc")
	}
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]string{"error": msg}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logx.Error(context.Background(), "Failed to encode JSON error", err, "service", "alert-svc")
	}
}
