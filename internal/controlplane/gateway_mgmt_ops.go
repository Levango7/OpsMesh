// Package controlplane gateway_mgmt_ops.go 实现 API 网关管理 (Kong/APISIX/Nginx)
package controlplane

import (
	"encoding/json"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleGatewayMgmt 处理 POST /api/v1/gateway/mgmt
func (s *Server) handleGatewayMgmt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // kong/apisix/nginx
		Action   string `json:"action"` // status/routes/services/plugins/consumers
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Type {
	case "kong":
		switch req.Action {
		case "status":
			script = `curl -s http://localhost:8001/ 2>/dev/null | head -10 || echo "Kong not available"`
		case "routes":
			script = `curl -s http://localhost:8001/routes 2>/dev/null | head -30 || echo "Kong not available"`
		case "services":
			script = `curl -s http://localhost:8001/services 2>/dev/null | head -30 || echo "Kong not available"`
		case "plugins":
			script = `curl -s http://localhost:8001/plugins 2>/dev/null | head -30 || echo "Kong not available"`
		case "consumers":
			script = `curl -s http://localhost:8001/consumers 2>/dev/null | head -30 || echo "Kong not available"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "apisix":
		switch req.Action {
		case "status":
			script = `curl -s http://localhost:9080/apisix/admin/routes 2>/dev/null -H "X-API-KEY: edd1c9f034335f136f87ad84b6257e8e" | head -10 || echo "APISIX not available"`
		case "routes":
			script = `curl -s http://localhost:9080/apisix/admin/routes 2>/dev/null -H "X-API-KEY: edd1c9f034335f136f87ad84b6257e8e" | head -30 || echo "APISIX not available"`
		case "services":
			script = `curl -s http://localhost:9080/apisix/admin/services 2>/dev/null -H "X-API-KEY: edd1c9f034335f136f87ad84b6257e8e" | head -30 || echo "APISIX not available"`
		case "plugins":
			script = `curl -s http://localhost:9080/apisix/admin/plugins 2>/dev/null -H "X-API-KEY: edd1c9f034335f136f87ad84b6257e8e" | head -30 || echo "APISIX not available"`
		case "consumers":
			script = `curl -s http://localhost:9080/apisix/admin/consumers 2>/dev/null -H "X-API-KEY: edd1c9f034335f136f87ad84b6257e8e" | head -30 || echo "APISIX not available"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "nginx":
		switch req.Action {
		case "status":
			script = `nginx -t 2>&1 && echo "Nginx config OK" && systemctl status nginx 2>/dev/null | head -5 || echo "Nginx not available"`
		case "reload":
			script = `nginx -t 2>&1 && systemctl reload nginx && echo "Nginx reloaded" || echo "Nginx reload failed"`
		case "upstreams":
			script = `nginx -T 2>/dev/null | grep -A5 "upstream" | head -30 || echo "Nginx not available"`
		case "vhosts":
			script = `ls /etc/nginx/sites-enabled/ 2>/dev/null || ls /etc/nginx/conf.d/ 2>/dev/null || echo "Nginx not available"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + req.Type})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "gateway_mgmt_"+req.Type+"_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerGatewayMgmtRoutes 注册 API 网关管理路由
func (s *Server) registerGatewayMgmtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/gateway/mgmt", s.handleGatewayMgmt)
}
