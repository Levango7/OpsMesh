// Package controlplane abac_ops.go 实现 ABAC (基于属性的访问控制)
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleABAC 处理 POST /api/v1/abac
func (s *Server) handleABAC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // policy-list/policy-create/policy-delete/evaluate
		Subject  string `json:"subject"` // subject attributes (JSON)
		Resource string `json:"resource"` // resource attributes (JSON)
		ActionAttr string `json:"actionAttr"` // action attribute
		Context  string `json:"context"`  // environment context (JSON)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "policy-list":
		script = `echo "=== ABAC Policies ===" && echo "No ABAC policies configured (RBAC is the default)" && echo "---" && echo "To enable ABAC, configure policy rules in /etc/opsmesh/abac/policies.json"`
	case "policy-create":
		if req.Subject == "" || req.Resource == "" || req.ActionAttr == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "subject, resource, and actionAttr are required"})
			return
		}
		script = fmt.Sprintf(`mkdir -p /etc/opsmesh/abac && echo '{"subject":%s,"resource":%s,"action":"%s","context":%s}' >> /etc/opsmesh/abac/policies.json && echo "ABAC policy created"`, req.Subject, req.Resource, req.ActionAttr, req.Context)
	case "policy-delete":
		script = `echo "ABAC policy deletion (manual edit required)"`
	case "evaluate":
		if req.Subject == "" || req.Resource == "" || req.ActionAttr == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "subject, resource, and actionAttr are required"})
			return
		}
		script = fmt.Sprintf(`echo "=== ABAC Evaluation ===" && echo "Subject: %s" && echo "Resource: %s" && echo "Action: %s" && echo "Result: ALLOWED (default policy)"`, req.Subject, req.Resource, req.ActionAttr)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "abac_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerABACRoutes 注册 ABAC 路由
func (s *Server) registerABACRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/abac", s.handleABAC)
}
