// Package controlplane servicediscovery_ops.go 实现服务发现管理 (Consul/Eureka/Nacos)
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleServiceDiscovery 处理 POST /api/v1/servicediscovery
func (s *Server) handleServiceDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // consul/eureka/nacos
		Action   string `json:"action"` // status/services/kv-get/kv-set/kv-delete/health
		Key      string `json:"key"`
		Value    string `json:"value"`
		Service  string `json:"service"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Type {
	case "consul":
		switch req.Action {
		case "status":
			script = `consul members 2>/dev/null && echo "---" && consul operator raft list-peers 2>/dev/null || echo "Consul not available"`
		case "services":
			script = `consul catalog services 2>/dev/null || echo "Consul not available"`
		case "kv-get":
			if req.Key == "" {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
				return
			}
			script = fmt.Sprintf(`consul kv get %s 2>/dev/null || echo "Key not found"`, req.Key)
		case "kv-set":
			if req.Key == "" || req.Value == "" {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "key and value are required"})
				return
			}
			script = fmt.Sprintf(`consul kv put %s %s 2>/dev/null && echo "Key set"`, req.Key, req.Value)
		case "kv-delete":
			if req.Key == "" {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
				return
			}
			script = fmt.Sprintf(`consul kv delete %s 2>/dev/null && echo "Key deleted"`, req.Key)
		case "health":
			script = `consul monitor 2>/dev/null | head -10 || echo "Consul not available"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "eureka":
		switch req.Action {
		case "status":
			script = `curl -s http://localhost:8761/eureka/apps 2>/dev/null | head -20 || echo "Eureka not available"`
		case "services":
			script = `curl -s http://localhost:8761/eureka/apps 2>/dev/null | grep -o '<name>[^<]*' | sed 's/<name>//' || echo "Eureka not available"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "nacos":
		switch req.Action {
		case "status":
			script = `curl -s http://localhost:8848/nacos/v1/ns/instance/list 2>/dev/null | head -20 || echo "Nacos not available"`
		case "services":
			script = `curl -s "http://localhost:8848/nacos/v1/ns/service/list?pageNo=1&pageSize=100" 2>/dev/null | head -20 || echo "Nacos not available"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + req.Type})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "servicediscovery_"+req.Type+"_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerServiceDiscoveryRoutes 注册服务发现路由
func (s *Server) registerServiceDiscoveryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/servicediscovery", s.handleServiceDiscovery)
}
