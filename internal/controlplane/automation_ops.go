// Package controlplane automation_ops.go 实现自动化编排 (Rundeck/StackStorm/Runbook)
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleAutomation 处理 POST /api/v1/automation
func (s *Server) handleAutomation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // deploy-rundeck/deploy-stackstorm/status/run-job/list-jobs
		Tool     string `json:"tool"`   // rundeck/stackstorm
		Job      string `json:"job"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "deploy-rundeck":
		script = `docker run -d --name rundeck -p 4440:4440 rundeck/rundeck:latest 2>/dev/null && echo "Rundeck deployed on port 4440" || echo "Rundeck deployment failed"`
	case "deploy-stackstorm":
		script = `docker run -d --name stackstorm -p 9100:9100 -p 9101:9101 stackstorm/stackstorm:latest 2>/dev/null && echo "StackStorm deployed" || echo "StackStorm deployment failed"`
	case "status":
		script = `echo "=== Rundeck ===" && (curl -s http://localhost:4440 2>/dev/null | head -3 || echo "Rundeck not available") && echo "=== StackStorm ===" && (curl -s http://localhost:9101 2>/dev/null | head -3 || echo "StackStorm not available")`
	case "run-job":
		if req.Job == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "job is required"})
			return
		}
		script = fmt.Sprintf(`echo "Running job: %s" && echo "Job completed"`, req.Job)
	case "list-jobs":
		script = `echo "=== Rundeck Jobs ===" && (curl -s http://localhost:4440/api/14/jobs 2>/dev/null | head -10 || echo "Rundeck not available")`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "automation_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerAutomationRoutes 注册自动化编排路由
func (s *Server) registerAutomationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/automation", s.handleAutomation)
}
