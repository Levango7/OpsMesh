// db_spark_flink_ops.go Spark / Flink 作业管理。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

var sparkFlinkNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

var sparkFlinkTypes = map[string]bool{
	"spark": true, "flink": true,
}

var sparkFlinkActions = map[string]bool{
	// Spark
	"status": true, "submit": true, "kill": true, "logs": true, "app-list": true,
	"app-status": true, "history-server": true, "spark-shell": true, "sql": true,
	"thrift-server": true, "streaming": true, "structured-streaming": true,
	"checkpoint-status": true, "executor-list": true, "executor-kill": true,
	"config": true, "monitoring": true,
	// Flink
	"job-list": true, "job-status": true, "savepoint": true, "restore": true,
	"checkpoint-list": true, "taskmanager-list": true, "taskmanager-kill": true,
	"web-ui": true, "sql-client": true,
}

func (s *Server) handleSparkFlink(w http.ResponseWriter, r *http.Request) {
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
	sfType, _ := req["type"].(string)
	if sfType == "" {
		sfType = "spark"
	}
	if !sparkFlinkTypes[sfType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + sfType})
		return
	}
	action, _ := req["action"].(string)
	if action == "" || !sparkFlinkNameRe.MatchString(action) || !sparkFlinkActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + action})
		return
	}
	for k, v := range req {
		str, ok := v.(string)
		if !ok || str == "" {
			continue
		}
		if !sparkFlinkNameRe.MatchString(str) {
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

	script := fmt.Sprintf("set -e\necho 'sparkflink %s %s done'\n", sfType, action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "sparkflink_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.sparkflink", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_sparkflink_" + action,
		Target: sfType, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + sfType + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
