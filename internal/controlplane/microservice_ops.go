// Package controlplane microservice_ops.go 实现微服务治理运维
// 支持：Dubbo、Spring Cloud、Apollo、Nacos、Sentinel、Seata
package controlplane

import (
	"encoding/json"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleMicroservice 处理 POST /api/v1/microservice
func (s *Server) handleMicroservice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // dubbo/spring-cloud/apollo/nacos/sentinel/seata
		Action   string `json:"action"` // status/services/configs/routes
		Service  string `json:"service"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Type {
	case "dubbo":
		switch req.Action {
		case "status":
			script = `echo "=== Dubbo Status ===" && (curl -s http://localhost:8080/dubbo 2>/dev/null | head -10 || echo "Dubbo Admin not available") && echo "---" && (jps | grep -i dubbo || echo "Dubbo not running")`
		case "services":
			script = `echo "=== Dubbo Services ===" && (curl -s http://localhost:8080/dubbo/services 2>/dev/null | head -20 || echo "Dubbo Admin not available")`
		case "config":
			script = `echo "=== Dubbo Config ===" && (cat /opt/dubbo/conf/dubbo.properties 2>/dev/null || echo "Dubbo config not found")`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "spring-cloud":
		switch req.Action {
		case "status":
			script = `echo "=== Spring Cloud Status ===" && (curl -s http://localhost:8888/actuator/health 2>/dev/null | head -10 || echo "Spring Cloud not available")`
		case "routes":
			script = `echo "=== Gateway Routes ===" && (curl -s http://localhost:8888/actuator/gateway/routes 2>/dev/null | head -30 || echo "Gateway not available")`
		case "config":
			script = `echo "=== Spring Cloud Config ===" && (curl -s http://localhost:8888/actuator/env 2>/dev/null | head -20 || echo "Config not available")`
		case "refresh":
			script = `curl -s -X POST http://localhost:8888/actuator/refresh 2>/dev/null && echo "Config refreshed" || echo "Refresh failed"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "apollo":
		switch req.Action {
		case "status":
			script = `echo "=== Apollo Status ===" && (curl -s http://localhost:8070 2>/dev/null | head -5 || echo "Apollo not available")`
		case "namespaces":
			script = `echo "=== Apollo Namespaces ===" && (curl -s http://localhost:8070/openapi/v1/envs 2>/dev/null | head -20 || echo "Apollo not available")`
		case "releases":
			script = `echo "=== Apollo Releases ===" && (curl -s http://localhost:8070/openapi/v1/apps 2>/dev/null | head -20 || echo "Apollo not available")`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "nacos":
		switch req.Action {
		case "status":
			script = `echo "=== Nacos Status ===" && (curl -s http://localhost:8848/nacos/v1/ns/operator/metrics 2>/dev/null | head -10 || echo "Nacos not available")`
		case "services":
			script = `echo "=== Nacos Services ===" && (curl -s "http://localhost:8848/nacos/v1/ns/service/list?pageNo=1&pageSize=100" 2>/dev/null | head -20 || echo "Nacos not available")`
		case "config":
			script = `echo "=== Nacos Config ===" && (curl -s "http://localhost:8848/nacos/v1/cs/configs?dataId=&group=&pageNo=1&pageSize=100" 2>/dev/null | head -20 || echo "Nacos not available")`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "sentinel":
		switch req.Action {
		case "status":
			script = `echo "=== Sentinel Status ===" && (curl -s http://localhost:8719 2>/dev/null | head -10 || echo "Sentinel not available")`
		case "rules":
			script = `echo "=== Sentinel Rules ===" && (curl -s http://localhost:8719/getRules?type=flow 2>/dev/null | head -20 || echo "Sentinel not available")`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "seata":
		switch req.Action {
		case "status":
			script = `echo "=== Seata Status ===" && (curl -s http://localhost:7091 2>/dev/null | head -10 || echo "Seata not available")`
		case "transactions":
			script = `echo "=== Seata Transactions ===" && (curl -s http://localhost:7091/transactions 2>/dev/null | head -20 || echo "Seata not available")`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + req.Type})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "microservice_"+req.Type+"_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerMicroserviceRoutes 注册微服务治理路由
func (s *Server) registerMicroserviceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/microservice", s.handleMicroservice)
}
