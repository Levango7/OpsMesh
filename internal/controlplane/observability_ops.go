// Package controlplane observability_ops.go 实现可观测性运维 (Grafana/Thanos/Zipkin/Prometheus)
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleObservability 处理 POST /api/v1/observability
func (s *Server) handleObservability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // deploy-grafana/deploy-thanos/deploy-zipkin/status
		Tool     string `json:"tool"`   // grafana/thanos/zipkin/prometheus
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "deploy-grafana":
		version := req.Version
		if version == "" {
			version = "10.4.0"
		}
		script = fmt.Sprintf(`docker run -d --name grafana -p 3000:3000 grafana/grafana:%s 2>/dev/null && echo "Grafana %s deployed"`, version, version)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "observability_deploy-grafana")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-grafana"})

	case "deploy-thanos":
		version := req.Version
		if version == "" {
			version = "0.35.0"
		}
		script = fmt.Sprintf(`docker run -d --name thanos -p 10902:10902 thanosio/thanos:%s query 2>/dev/null && echo "Thanos %s deployed"`, version, version)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "observability_deploy-thanos")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-thanos"})

	case "deploy-zipkin":
		version := req.Version
		if version == "" {
			version = "2.24"
		}
		script = fmt.Sprintf(`docker run -d --name zipkin -p 9411:9411 openzipkin/zipkin:%s 2>/dev/null && echo "Zipkin %s deployed"`, version, version)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "observability_deploy-zipkin")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-zipkin"})

	case "status":
		script = `echo "=== Prometheus ===" && curl -s http://localhost:9090/-/healthy 2>/dev/null && echo "Prometheus healthy" || echo "Prometheus not available" && echo "=== Grafana ===" && curl -s http://localhost:3000/api/health 2>/dev/null && echo "Grafana healthy" || echo "Grafana not available" && echo "=== Thanos ===" && curl -s http://localhost:10902/-/healthy 2>/dev/null && echo "Thanos healthy" || echo "Thanos not available" && echo "=== Zipkin ===" && curl -s http://localhost:9411/health 2>/dev/null && echo "Zipkin healthy" || echo "Zipkin not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "observability_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// registerObservabilityRoutes 注册可观测性路由
func (s *Server) registerObservabilityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/observability", s.handleObservability)
}
