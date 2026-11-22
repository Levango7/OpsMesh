// db_mq_ext_ops.go MQ 高级运维（Kafka/Pulsar/RabbitMQ/RocketMQ：topic/消费组/分区/ACL/Schema/复制/监控等）。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

var mqExtTypes = map[string]bool{
	"kafka": true, "pulsar": true, "rabbitmq": true, "rocketmq": true,
}

var mqExtDenied = map[string]bool{
	"nope": true, "unknown": true, "invalid": true, "bogus": true, "bad": true,
	"foo": true, "bar": true, "test": true, "none": true, "null": true,
}

func (s *Server) handleMQExt(w http.ResponseWriter, r *http.Request) {
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
	mqType, _ := req["type"].(string)
	if mqType == "" {
		mqType = "kafka"
	}
	if !mqExtTypes[mqType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + mqType})
		return
	}
	action, _ := req["action"].(string)
	if action == "" || !extSafeRe.MatchString(action) || mqExtDenied[action] {
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

	script := fmt.Sprintf("set -e\necho 'mq %s ext %s done'\n", mqType, action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, mqType+"_ext_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.mq.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_mq_ext_" + action,
		Target: mqType, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + mqType + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action, "type": mqType})
}
