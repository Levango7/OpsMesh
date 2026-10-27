// Package controlplane k8s_gateway_ops.go 实现 K8s Gateway API (gateway.networking.k8s.io) 运维
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleK8sGatewayAPI 处理 POST /api/v1/k8s/gateway-api
func (s *Server) handleK8sGatewayAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list-gatewayclasses/list-gateways/list-httproutes/list-grpcroutes/list-tcproutes/status
		Name     string `json:"name"`
		Namespace string `json:"namespace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	var script string
	switch req.Action {
	case "list-gatewayclasses":
		script = `kubectl get gatewayclasses 2>/dev/null || echo "Gateway API not installed"`
	case "list-gateways":
		script = fmt.Sprintf(`kubectl get gateways -n %s 2>/dev/null || echo "No gateways in namespace %s"`, ns, ns)
	case "list-httproutes":
		script = fmt.Sprintf(`kubectl get httproutes -n %s 2>/dev/null || echo "No HTTPRoutes in namespace %s"`, ns, ns)
	case "list-grpcroutes":
		script = fmt.Sprintf(`kubectl get grpcroutes -n %s 2>/dev/null || echo "No GRPCRoutes in namespace %s"`, ns, ns)
	case "list-tcproutes":
		script = fmt.Sprintf(`kubectl get tcproutes -n %s 2>/dev/null || echo "No TCPRoutes in namespace %s"`, ns, ns)
	case "status":
		script = `echo "=== Gateway API Status ===" && kubectl get crd | grep gateway.networking.k8s.io 2>/dev/null && echo "Gateway API installed" || echo "Gateway API not installed" && echo "---" && kubectl get gatewayclasses 2>/dev/null || echo "No GatewayClasses"`
	case "install":
		script = `kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.1.0/standard-install.yaml 2>/dev/null && echo "Gateway API installed" || echo "Installation failed"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "k8s_gateway-api_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerK8sGatewayAPIRoutes 注册 K8s Gateway API 路由
func (s *Server) registerK8sGatewayAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/k8s/gateway-api", s.handleK8sGatewayAPI)
}
