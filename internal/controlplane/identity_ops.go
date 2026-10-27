// Package controlplane identity_ops.go 实现身份与访问管理 (LDAP/AD/SSO)
package controlplane

import (
	"encoding/json"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleIdentity 处理 POST /api/v1/identity
func (s *Server) handleIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // ldap-status/ldap-config/sso-status/sso-config
		Provider string `json:"provider"` // ldap/ad/saml/oidc
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "ldap-status":
		script = `echo "=== LDAP/AD Status ===" && (systemctl status slapd 2>/dev/null | head -3 || systemctl status sssd 2>/dev/null | head -3 || echo "LDAP/AD not running") && echo "---" && (ldapsearch -x -H ldap://localhost -b "" -s base 2>/dev/null | head -5 || echo "LDAP query not available")`
	case "ldap-config":
		script = `echo "=== LDAP Configuration ===" && (cat /etc/ldap/ldap.conf 2>/dev/null || cat /etc/openldap/ldap.conf 2>/dev/null || echo "LDAP config not found") && echo "---" && (cat /etc/sssd/sssd.conf 2>/dev/null || echo "SSSD config not found")`
	case "sso-status":
		script = `echo "=== SSO/OIDC Status ===" && (curl -s http://localhost:8080/realms/master/.well-known/openid-configuration 2>/dev/null | head -5 || echo "Keycloak not available")`
	case "sso-config":
		script = `echo "=== SSO Configuration Guide ===" && echo "1. Deploy Keycloak/Okta/Auth0" && echo "2. Configure realm and client" && echo "3. Set redirect URIs" && echo "4. Configure token claims" && echo "5. Test login flow"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "identity_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerIdentityRoutes 注册身份与访问管理路由
func (s *Server) registerIdentityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/identity", s.handleIdentity)
}
