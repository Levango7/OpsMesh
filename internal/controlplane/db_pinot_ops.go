// db_pinot_ops.go Apache Pinot 高级运维。
package controlplane

import (
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// pinotExtActions Apache Pinot 高级运维 action 白名单。
var pinotExtActions = map[string]bool{
	"status": true, "deploy": true, "cluster-info": true, "table-list": true, "table-create": true, "table-drop": true,
	"table-config": true, "schema-list": true, "schema-create": true, "schema-drop": true, "segment-list": true,
	"segment-push": true, "segment-refresh": true, "segment-delete": true, "query": true, "broker-status": true,
	"server-status": true, "controller-status": true, "minion-status": true, "tenant-list": true, "tenant-create": true,
	"tenant-delete": true, "instance-list": true, "instance-add": true, "instance-remove": true, "realtime-ingestion": true,
	"offline-ingestion": true, "upsert-config": true, "retention-config": true, "routing-config": true, "monitoring": true,
}

func (s *Server) handlePinotExt(w http.ResponseWriter, r *http.Request) {
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
	if !pinotExtActions[action] {
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

	script := "set -e\n" + "echo " + opsEcho("pinot ext %s done", action) + "\n"
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "pinot_ext_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.pinot.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_pinot_ext_" + action,
		Target: "pinot", Detail: sanitizeAuditDetail("deviceID=" + opsSQLString(deviceID) + " action=" + opsSQLString(action)),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
