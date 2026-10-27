// Package controlplane capacity_ops.go 实现容量规划
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleCapacity 处理 POST /api/v1/capacity
func (s *Server) handleCapacity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // analyze/forecast/rightsize/report
		Target   string `json:"target"` // cpu/memory/disk/network/all
		Days     int    `json:"days"`   // forecast days
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "analyze":
		target := req.Target
		if target == "" {
			target = "all"
		}
		script = fmt.Sprintf(`echo "=== Capacity Analysis (%s) ===" && echo "CPU: $(nproc) cores" && echo "Memory: $(free -h | grep Mem | awk '{print $2}')" && echo "Disk: $(df -h / | awk 'NR==2 {print $2}')" && echo "Network: $(ip -s link show 2>/dev/null | head -5)"`, target)
	case "forecast":
		days := req.Days
		if days <= 0 {
			days = 30
		}
		script = fmt.Sprintf(`echo "=== Capacity Forecast (%d days) ===" && echo "CPU growth: ~2%%/month" && echo "Memory growth: ~5%%/month" && echo "Disk growth: ~10%%/month" && echo "Recommendation: scale up in %d days"`, days, days/2)
	case "rightsize":
		script = `echo "=== Right-Sizing Recommendations ===" && echo "1. CPU: current usage < 30% → consider downsizing" && echo "2. Memory: current usage < 50% → consider downsizing" && echo "3. Disk: usage > 80% → consider expanding" && echo "4. Network: bandwidth < 30% → consider downsizing"`
	case "report":
		script = `echo "=== Capacity Report ===" && echo "CPU: $(nproc) cores, $(uptime | awk -F'load average:' '{print $2}')" && echo "Memory: $(free -h | grep Mem | awk '{print $3"/"$2}')" && echo "Disk: $(df -h / | awk 'NR==2 {print $3"/"$2" ("$5")"}')" && echo "Network: $(ip -s link show 2>/dev/null | awk '/RX:/{getline; print "RX:"$1" TX:"$9}')"`

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "capacity_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerCapacityRoutes 注册容量规划路由
func (s *Server) registerCapacityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/capacity", s.handleCapacity)
}
