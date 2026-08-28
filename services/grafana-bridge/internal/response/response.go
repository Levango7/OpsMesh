package response

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Levango7/OpsMesh/services/grafana-bridge/internal/models"
)

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// WriteError writes an error response.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// WriteTimeseriesResponse writes a Grafana timeseries response.
// Format: [{"target": "series_name", "datapoints": [[value, timestamp_ms], ...]}]
func WriteTimeseriesResponse(w http.ResponseWriter, series []models.TimeSeries) {
	type datapoint struct {
		Value     float64 `json:"-"`
		Timestamp int64   `json:"-"`
	}
	type seriesResponse struct {
		Target     string      `json:"target"`
		Datapoints [][]float64 `json:"datapoints"`
	}

	out := make([]seriesResponse, 0, len(series))
	for _, s := range series {
		points := make([][]float64, 0, len(s.Points))
		for _, p := range s.Points {
			points = append(points, []float64{p.Value, float64(p.Timestamp.UnixMilli())})
		}
		out = append(out, seriesResponse{
			Target:     s.Target,
			Datapoints: points,
		})
	}

	WriteJSON(w, http.StatusOK, out)
}

// WriteTableResponse writes a Grafana table response.
// Format: [{"columns": [{"text": "col1", "type": "string"}], "rows": [["val1", "val2"]], "type": "table"}]
func WriteTableResponse(w http.ResponseWriter, columns []string, rows [][]string) {
	type columnDef struct {
		Text string `json:"text"`
		Type string `json:"type"`
	}

	cols := make([]columnDef, 0, len(columns))
	for _, c := range columns {
		cols = append(cols, columnDef{Text: c, Type: "string"})
	}

	out := []map[string]interface{}{
		{
			"columns": cols,
			"rows":    rows,
			"type":    "table",
		},
	}

	WriteJSON(w, http.StatusOK, out)
}

// WriteAnnotationsResponse writes a Grafana annotations response.
// Format: [{"time": timestamp_ms, "title": "...", "text": "...", "tags": [...]}]
func WriteAnnotationsResponse(w http.ResponseWriter, annotations []models.Annotation) {
	type annotationResponse struct {
		Time  int64    `json:"time"`
		Title string   `json:"title"`
		Text  string   `json:"text"`
		Tags  []string `json:"tags"`
	}

	out := make([]annotationResponse, 0, len(annotations))
	for _, a := range annotations {
		out = append(out, annotationResponse{
			Time:  a.Time.UnixMilli(),
			Title: a.Title,
			Text:  a.Text,
			Tags:  a.Tags,
		})
	}

	WriteJSON(w, http.StatusOK, out)
}

// WriteSearchResponse writes the search response (list of available metrics).
// Format: ["metric1", "metric2", ...]
func WriteSearchResponse(w http.ResponseWriter, targets []models.SearchTarget) {
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		out = append(out, t.Text)
	}
	WriteJSON(w, http.StatusOK, out)
}

// WriteTagKeysResponse writes the tag keys response.
// Format: [{"type": "string", "text": "key_name"}, ...]
func WriteTagKeysResponse(w http.ResponseWriter, keys []models.TagKey) {
	WriteJSON(w, http.StatusOK, keys)
}

// WriteTagValuesResponse writes the tag values response.
// Format: [{"text": "value"}, ...]
func WriteTagValuesResponse(w http.ResponseWriter, values []models.TagValue) {
	WriteJSON(w, http.StatusOK, values)
}

// WriteHealthResponse writes a health check response.
func WriteHealthResponse(w http.ResponseWriter) {
	WriteJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "grafana-bridge",
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}
