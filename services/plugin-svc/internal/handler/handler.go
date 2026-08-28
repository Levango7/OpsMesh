package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/services/plugin-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/plugin-svc/internal/service"
)

// Handler handles HTTP requests for the plugin marketplace.
type Handler struct {
	svc *service.Service
}

// NewHandler creates a new Handler.
func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers all HTTP routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/ready", h.handleReady)
	mux.HandleFunc("/api/v1/plugins", h.handlePlugins)
	mux.HandleFunc("/api/v1/plugins/", h.handlePluginRouting)
	mux.HandleFunc("/api/v1/plugins/search", h.handleSearch)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) handlePlugins(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listPlugins(w, r)
	case http.MethodPost:
		h.createPlugin(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (h *Handler) handlePluginRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/plugins/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "plugin id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "plugin id required"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.getPlugin(w, r, id)
		case http.MethodPut:
			h.updatePlugin(w, r, id)
		case http.MethodDelete:
			h.deletePlugin(w, r, id)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "install":
		h.installPlugin(w, r, id)
	case "uninstall":
		h.uninstallPlugin(w, r, id)
	case "upgrade":
		h.upgradePlugin(w, r, id)
	case "versions":
		h.getVersions(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

func (h *Handler) listPlugins(w http.ResponseWriter, r *http.Request) {
	plugins := h.svc.ListPlugins()
	writeJSON(w, http.StatusOK, map[string]interface{}{"plugins": plugins})
}

func (h *Handler) createPlugin(w http.ResponseWriter, r *http.Request) {
	var p models.Plugin
	if err := decodeJSONBody(r, &p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	created, err := h.svc.CreatePlugin(&p)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) getPlugin(w http.ResponseWriter, r *http.Request, id string) {
	p, err := h.svc.GetPlugin(id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) updatePlugin(w http.ResponseWriter, r *http.Request, id string) {
	var p models.Plugin
	if err := decodeJSONBody(r, &p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	p.ID = id
	updated, err := h.svc.UpdatePlugin(&p)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) deletePlugin(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.svc.DeletePlugin(id); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) installPlugin(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	updated, err := h.svc.InstallPlugin(id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) uninstallPlugin(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	updated, err := h.svc.UninstallPlugin(id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) upgradePlugin(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		Version     string `json:"version"`
		Checksum    string `json:"checksum"`
		DownloadURL string `json:"downloadURL"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	updated, err := h.svc.UpgradePlugin(id, body.Version, body.Checksum, body.DownloadURL)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) getVersions(w http.ResponseWriter, r *http.Request, id string) {
	versions, err := h.svc.GetVersions(id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"versions": versions})
}

func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var query models.SearchQuery
	if err := decodeJSONBody(r, &query); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	results := h.svc.SearchPlugins(query)
	writeJSON(w, http.StatusOK, map[string]interface{}{"plugins": results})
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case err == service.ErrPluginNotFound:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case err == service.ErrPluginInvalid:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case err == service.ErrPluginExists:
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case err == service.ErrPluginInstalled:
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case err == service.ErrPluginNotInstalled:
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case err == service.ErrInvalidURL, err == service.ErrSSRFBlocked:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case err == service.ErrVersionNotFound:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func decodeJSONBody(r *http.Request, v interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
