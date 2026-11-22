// db_tsdb_ext_ops.go 时序数据库高级运维（InfluxDB/TDengine/VictoriaMetrics：库/RP/CQ/表/用户/备份/恢复/集群/节点/监控等）。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

var tsdbExtTypes = map[string]bool{
	"influxdb": true, "tdengine": true, "victoriametrics": true,
}

var tsdbExtActions = map[string]bool{
	"db-list": true, "db-create": true, "db-drop": true,
	"rp-list": true, "rp-create": true, "rp-alter": true, "rp-drop": true,
	"cq-list": true, "cq-create": true, "cq-drop": true,
	"user-list": true, "user-create": true, "user-drop": true,
	"backup": true, "restore": true, "monitoring": true,
	"table-list": true, "table-create": true, "table-drop": true,
	"cluster-status": true, "node-status": true,
	"status": true, "query": true, "export": true, "import": true,
	"snapshot": true, "retention": true, "downsampling": true,
}

func (s *Server) handleTSDBExt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req map[string]interface{}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	deviceID, _ := req["deviceID"].(string)
	if deviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	tType, _ := req["type"].(string)
	if tType == "" {
		tType = "influxdb"
	}
	if !tsdbExtTypes[tType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + tType})
		return
	}
	action, _ := req["action"].(string)
	if action == "" || !extSafeRe.MatchString(action) || !tsdbExtActions[action] {
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

	script := fmt.Sprintf("set -e\necho 'tsdb %s ext %s done'\n", tType, action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, tType+"_ext_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.tsdb.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_tsdb_ext_" + action,
		Target: tType, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + tType + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
