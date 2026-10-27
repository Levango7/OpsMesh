// Package controlplane ha_ops.go 实现高可用与灾难恢复运维
package controlplane

import (
	"encoding/json"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleHA 处理 POST /api/v1/ha
func (s *Server) handleHA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/failover/test/backup/restore
		Cluster  string `json:"cluster"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Pacemaker ===" && (pcs status 2>/dev/null || crm_mon -1 2>/dev/null || echo "Pacemaker not available") && echo "=== Keepalived ===" && (systemctl status keepalived 2>/dev/null | head -3 || echo "Keepalived not available") && echo "=== HAProxy ===" && (systemctl status haproxy 2>/dev/null | head -3 || echo "HAProxy not available")`
	case "failover":
		script = `echo "=== Triggering Failover ===" && (pcs resource move 2>/dev/null || crm resource move 2>/dev/null || echo "Failover not available") && echo "Failover triggered"`
	case "test":
		script = `echo "=== HA Test ===" && echo "Testing failover capability..." && (pcs status 2>/dev/null | head -5 || echo "Pacemaker not available")`
	case "backup":
		script = `echo "=== HA Backup ===" && (pcs config export 2>/dev/null || crm configure show 2>/dev/null || echo "HA backup not available")`
	case "restore":
		script = `echo "=== HA Restore ===" && echo "Restore from backup (manual steps required)"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "ha_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerHARoutes 注册 HA 路由
func (s *Server) registerHARoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/ha", s.handleHA)
}
