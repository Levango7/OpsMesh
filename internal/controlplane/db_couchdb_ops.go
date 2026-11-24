// db_couchdb_ops.go CouchDB 高级运维。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// couchDBExtActions CouchDB 高级运维 action 白名单。
var couchDBExtActions = map[string]bool{
	"status": true, "deploy": true, "start": true, "stop": true, "restart": true, "version": true,
	"db-list": true, "db-create": true, "db-delete": true, "db-info": true,
	"doc-list": true, "doc-get": true, "doc-create": true, "doc-update": true, "doc-delete": true,
	"view-list": true, "view-create": true, "view-delete": true,
	"index-list": true, "index-create": true, "index-delete": true,
	"replication-status": true, "replication-create": true, "replication-delete": true,
	"compaction": true, "purge": true,
	"user-list": true, "user-create": true, "user-delete": true,
	"role-list": true,
	"backup":    true, "restore": true,
	"monitoring": true,
}

func (s *Server) handleCouchDB(w http.ResponseWriter, r *http.Request) {
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
	if !couchDBExtActions[action] {
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

	script := fmt.Sprintf("set -e\necho 'couchdb ext %s done'\n", action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "couchdb_ext_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.couchdb.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_couchdb_ext_" + action,
		Target: "couchdb", Detail: sanitizeAuditDetail("deviceID=" + deviceID + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
