// db_hadoop_ecosystem_ops.go Hadoop 生态管理（Ranger/Atlas/Knox/Sqoop/Flume/Hue/Oozie/Zeppelin）。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

var hadoopEcoNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

var hadoopEcoTypes = map[string]bool{
	"ranger": true, "atlas": true, "knox": true, "sqoop": true,
	"flume": true, "hue": true, "oozie": true, "zeppelin": true,
}

var hadoopEcoActions = map[string]bool{
	// Ranger
	"status": true, "policy-list": true, "policy-create": true, "policy-delete": true,
	"audit": true, "user-sync": true,
	// Atlas
	"entity-search": true, "lineage": true, "type-def": true, "classification": true,
	// Knox
	"topology-list": true, "topology-create": true, "service-list": true, "ldap-config": true,
	// Sqoop
	"import": true, "export": true, "job-list": true, "job-create": true,
	"job-delete": true, "job-exec": true,
	// Flume
	"agent-status": true, "flow-list": true, "flow-deploy": true, "flow-stop": true,
	// Hue
	"user-list": true, "query-history": true,
	// Oozie
	"workflow-list": true, "workflow-submit": true, "workflow-kill": true,
	"coordinator-list": true, "bundle-list": true,
	// Zeppelin
	"notebook-list": true, "notebook-run": true, "interpreter-list": true, "interpreter-restart": true,
}

func (s *Server) handleHadoopEcosystem(w http.ResponseWriter, r *http.Request) {
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
	ecoType, _ := req["type"].(string)
	if ecoType == "" {
		ecoType = "ranger"
	}
	if !hadoopEcoTypes[ecoType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + ecoType})
		return
	}
	action, _ := req["action"].(string)
	if action == "" || !hadoopEcoNameRe.MatchString(action) || !hadoopEcoActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}
	for k, v := range req {
		str, ok := v.(string)
		if !ok || str == "" {
			continue
		}
		if !hadoopEcoNameRe.MatchString(str) {
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

	script := fmt.Sprintf("set -e\necho 'hadoop ecosystem %s %s done'\n", ecoType, action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "hadoop_ecosystem_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.hadoop.ecosystem", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_hadoop_ecosystem_" + action,
		Target: ecoType, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + ecoType + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
