package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/Levango7/OpsMesh/services/grafana-bridge/internal/query"
	"github.com/Levango7/OpsMesh/services/grafana-bridge/internal/response"
	"github.com/Levango7/OpsMesh/services/grafana-bridge/internal/service"
)

// Handler handles HTTP requests for the Grafana JSON datasource.
type Handler struct {
	svc *service.Service
}

// NewHandler creates a new Handler.
func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers all Grafana JSON datasource routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.handleRoot)
	mux.HandleFunc("/search", h.handleSearch)
	mux.HandleFunc("/query", h.handleQuery)
	mux.HandleFunc("/annotations", h.handleAnnotations)
	mux.HandleFunc("/tag-keys", h.handleTagKeys)
	mux.HandleFunc("/tag-values", h.handleTagValues)
}

// handleRoot handles the health check endpoint (GET /).
func (h *Handler) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	response.WriteHealthResponse(w)
}

// handleSearch handles POST /search - returns available metric names.
func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	targets := h.svc.Search()
	response.WriteSearchResponse(w, targets)
}

// handleQuery handles POST /query - executes time-series or table queries.
func (h *Handler) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	req, err := query.ParseQueryRequest(body)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	from := req.Range.From
	to := req.Range.To

	if from.IsZero() {
		from = time.Now().Add(-1 * time.Hour)
	}
	if to.IsZero() {
		to = time.Now()
	}

	// Process each target
	allSeries := make([]struct {
		Target     string      `json:"target"`
		Datapoints [][]float64 `json:"datapoints"`
	}, 0)
	var tableCols []string
	var tableRows [][]string
	hasTable := false

	for _, t := range req.Targets {
		pq := query.ParseTarget(t, from, to, req.IntervalMs)
		series, cols, rows := h.svc.Query(pq)

		if cols != nil {
			tableCols = cols
			tableRows = rows
			hasTable = true
		}

		for _, s := range series {
			type seriesResp struct {
				Target     string      `json:"target"`
				Datapoints [][]float64 `json:"datapoints"`
			}
			points := make([][]float64, 0, len(s.Points))
			for _, p := range s.Points {
				points = append(points, []float64{p.Value, float64(p.Timestamp.UnixMilli())})
			}
			allSeries = append(allSeries, seriesResp{
				Target:     s.Target,
				Datapoints: points,
			})
		}
	}

	if hasTable {
		response.WriteTableResponse(w, tableCols, tableRows)
	} else {
		// Write timeseries
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(allSeries)
	}
}

// handleAnnotations handles POST /annotations - returns alert annotations.
func (h *Handler) handleAnnotations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	from := time.Now().Add(-1 * time.Hour)
	to := time.Now()

	if len(body) > 0 {
		var req query.AnnotationsRequest
		if err := json.Unmarshal(body, &req); err == nil {
			if !req.Range.From.IsZero() {
				from = req.Range.From
			}
			if !req.Range.To.IsZero() {
				to = req.Range.To
			}
		}
	}

	annotations := h.svc.Annotations(from, to)
	response.WriteAnnotationsResponse(w, annotations)
}

// handleTagKeys handles POST /tag-keys - returns available tag keys.
func (h *Handler) handleTagKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	keys := h.svc.TagKeys()
	response.WriteTagKeysResponse(w, keys)
}

// handleTagValues handles POST /tag-values - returns values for a tag key.
func (h *Handler) handleTagValues(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "failed to parse request body")
		return
	}

	values := h.svc.TagValues(req.Key)
	response.WriteTagValuesResponse(w, values)
}
