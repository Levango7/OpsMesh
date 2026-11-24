// cache_ehcache_ops.go Ehcache 高级运维。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// ehcacheExtActions Ehcache 高级运维 action 白名单。
var ehcacheExtActions = map[string]bool{
	"status": true, "deploy": true, "start": true, "stop": true, "restart": true, "version": true,
	"cache-list": true, "cache-create": true, "cache-drop": true, "cache-clear": true,
	"cache-stats": true, "cache-config": true,
	"monitoring": true,
}

func (s *Server) handleEhcache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req map[string]interface{}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	deviceID := getStr(req, "deviceID")
	if deviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	action := getStr(req, "action")
	if !ehcacheExtActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}
	for k, v := range req {
		str, ok := v.(string)
		if !ok || str == "" {
			continue
		}
		if !extSafeRe.MatchString(str) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid " + k})
			return
		}
	}

	actx, ok := s.subsystemAuthorize(w, r, "task:write")
	if !ok {
		return
	}
	if dev := s.store.Device(deviceID); dev != nil && dev.TenantID != "" && actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	if ag := s.store.Agent(deviceID); ag != nil && ag.TenantID != "" && actx.TenantID != "" && ag.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}

	script := fmt.Sprintf("set -e\necho 'ehcache ext %s done'\n", action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "ehcache_ext_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "cache.ehcache.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "cache_ehcache_ext_" + action,
		Target: "ehcache", Detail: sanitizeAuditDetail("deviceID=" + deviceID + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
