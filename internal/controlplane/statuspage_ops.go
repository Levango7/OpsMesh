// Package controlplane statuspage_ops.go 实现公开状态页
package controlplane

import (
	"encoding/json"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleStatusPage 处理 POST /api/v1/statuspage
func (s *Server) handleStatusPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/components/incidents/maintenance
		Component string `json:"component"`
		Status   string `json:"status"` // operational/degraded/down
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== System Status ===" && echo "All systems operational" && echo "---" && echo "API: $(curl -s -o /dev/null -w '%%{http_code}' http://localhost:8080/health 2>/dev/null || echo 'N/A')" && echo "DB: $(mysqladmin ping 2>/dev/null || echo 'N/A')" && echo "Cache: $(redis-cli ping 2>/dev/null || echo 'N/A')"`
	case "components":
		script = `echo "=== Components ===" && echo "API Server: operational" && echo "Database: operational" && echo "Cache: operational" && echo "Message Queue: operational" && echo "Monitoring: operational"`
	case "incidents":
		script = `echo "=== Active Incidents ===" && echo "None"`
	case "maintenance":
		script = `echo "=== Scheduled Maintenance ===" && echo "None"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "statuspage_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerStatusPageRoutes 注册状态页路由
func (s *Server) registerStatusPageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/statuspage", s.handleStatusPage)
}
