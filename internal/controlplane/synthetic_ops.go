// Package controlplane synthetic_ops.go 实现合成监控 (Uptime Checks)
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleSynthetic 处理 POST /api/v1/synthetic
func (s *Server) handleSynthetic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // create/list/delete/run
		Name     string `json:"name"`
		URL      string `json:"url"`
		Type     string `json:"type"`     // http/tcp/icmp
		Interval int    `json:"interval"` // seconds
		Timeout  int    `json:"timeout"`  // seconds
		Expected int    `json:"expected"` // expected HTTP status code
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "create":
		if req.Name == "" || req.URL == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and url are required"})
			return
		}
		probeType := req.Type
		if probeType == "" {
			probeType = "http"
		}
		interval := req.Interval
		if interval <= 0 {
			interval = 60
		}
		timeout := req.Timeout
		if timeout <= 0 {
			timeout = 10
		}
		expected := req.Expected
		if expected <= 0 {
			expected = 200
		}
		script = fmt.Sprintf(`echo "Creating synthetic monitor: %s (%s) -> %s (interval=%ds, timeout=%ds, expected=%d)"`, req.Name, probeType, req.URL, interval, timeout, expected)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "synthetic_create")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create"})

	case "list":
		script = `echo "=== Synthetic Monitors ===" && echo "No monitors configured (use create to add)" && echo "=== Active Probes ===" && echo "None"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "synthetic_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	case "delete":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`echo "Deleting synthetic monitor: %s"`, req.Name)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "synthetic_delete")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete"})

	case "run":
		if req.URL == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
			return
		}
		probeType := req.Type
		if probeType == "" {
			probeType = "http"
		}
		var script string
		switch probeType {
		case "http":
			script = fmt.Sprintf(`echo "=== HTTP Probe ===" && curl -s -o /dev/null -w "%%{http_code} %%{time_total}s" --max-time 10 "%s" 2>/dev/null || echo "Probe failed"`, req.URL)
		case "tcp":
			script = fmt.Sprintf(`echo "=== TCP Probe ===" && (timeout 5 bash -c "echo > /dev/tcp/%s/%s" 2>/dev/null && echo "TCP OK" || echo "TCP FAILED")`, req.URL, "80")
		case "icmp":
			script = fmt.Sprintf(`echo "=== ICMP Probe ===" && ping -c 1 -W 5 %s 2>/dev/null && echo "ICMP OK" || echo "ICMP FAILED"`, req.URL)
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "synthetic_run")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "run"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// registerSyntheticRoutes 注册合成监控路由
func (s *Server) registerSyntheticRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/synthetic", s.handleSynthetic)
}
