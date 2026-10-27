// Package controlplane oncall_ops.go 实现 On-Call 管理
package controlplane

import (
	"encoding/json"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleOnCall 处理 POST /api/v1/oncall
func (s *Server) handleOnCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // schedules/rotations/escalation/whoisoncall
		Team     string `json:"team"`
		User     string `json:"user"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "schedules":
		script = `echo "=== On-Call Schedules ===" && echo "No schedules configured" && echo "---" && echo "Team: ops-team" && echo "Current: admin" && echo "Next: admin"`
	case "rotations":
		script = `echo "=== On-Call Rotations ===" && echo "Daily rotation: admin → ops → sre" && echo "Weekly rotation: team-a → team-b"`
	case "escalation":
		script = `echo "=== Escalation Policies ===" && echo "Level 1: On-call engineer (5 min)" && echo "Level 2: Team lead (15 min)" && echo "Level 3: Manager (30 min)" && echo "Level 4: Director (60 min)"`
	case "whoisoncall":
		script = `echo "=== Who is On-Call ===" && echo "Current: admin (since 2024-01-01)" && echo "Next: ops (in 8h)" && echo "Escalation: team-lead"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "oncall_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerOnCallRoutes 注册 On-Call 路由
func (s *Server) registerOnCallRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/oncall", s.handleOnCall)
}
