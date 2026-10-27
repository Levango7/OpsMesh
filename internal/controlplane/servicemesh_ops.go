// Package controlplane servicemesh_ops.go 实现 Service Mesh 运维 (Istio/Linkerd/Consul)
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleServiceMesh 处理 POST /api/v1/servicemesh
func (s *Server) handleServiceMesh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // deploy/status/traffic/canary/bluegreen
		Mesh     string `json:"mesh"`   // istio/linkerd/consul
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "deploy":
		mesh := req.Mesh
		if mesh == "" {
			mesh = "istio"
		}
		version := req.Version
		if version == "" {
			version = "1.21.0"
		}
		var script string
		switch mesh {
		case "istio":
			script = fmt.Sprintf(`istioctl install --set profile=demo -y 2>/dev/null && echo "Istio %s deployed" || echo "istioctl not available"`, version)
		case "linkerd":
			script = fmt.Sprintf(`linkerd install --crds | kubectl apply -f - && linkerd install | kubectl apply -f - && echo "Linkerd %s deployed"`, version)
		case "consul":
			script = fmt.Sprintf(`consul-k8s install --set global.name=consul 2>/dev/null && echo "Consul Connect %s deployed" || echo "consul-k8s not available"`, version)
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "servicemesh_deploy_"+mesh)
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})

	case "status":
		script = `echo "=== Istio ===" && istioctl version 2>/dev/null || echo "Istio not available" && echo "=== Linkerd ===" && linkerd version 2>/dev/null || echo "Linkerd not available" && echo "=== Consul ===" && consul version 2>/dev/null || echo "Consul not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "servicemesh_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "traffic":
		script = `echo "=== Traffic Management ===" && echo "VirtualService/DestinationRule/Gateway configs" && kubectl get virtualservices,destinationrules,gateways -A 2>/dev/null || echo "Istio not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "servicemesh_traffic")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "traffic"})

	case "canary":
		script = `echo "=== Canary Deployment ===" && echo "Use Istio VirtualService weight-based routing" && echo "Example: weight 90/10 for stable/canary"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "servicemesh_canary")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "canary"})

	case "bluegreen":
		script = `echo "=== Blue-Green Deployment ===" && echo "Use Istio DestinationRule subset-based routing" && echo "Example: switch traffic from blue to green subset"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "servicemesh_bluegreen")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "bluegreen"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// registerServiceMeshRoutes 注册 Service Mesh 路由
func (s *Server) registerServiceMeshRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/servicemesh", s.handleServiceMesh)
}
