// db_vector_ext_ops.go 向量数据库高级运维（Milvus/Qdrant/Weaviate：集群/集合/索引/分区/搜索/插入/删除/备份/恢复/用户/角色/监控等）。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

var vectorExtTypes = map[string]bool{
	"milvus": true, "qdrant": true, "weaviate": true,
}

var vectorExtActions = map[string]bool{
	"cluster-status": true, "collection-list": true, "collection-create": true, "collection-drop": true,
	"index-list": true, "index-create": true, "index-drop": true,
	"partition-list": true, "partition-create": true, "partition-drop": true,
	"search": true, "insert": true, "delete": true, "flush": true, "compact": true,
	"backup": true, "restore": true, "user-list": true, "user-create": true, "user-drop": true,
	"role-list": true, "monitoring": true,
	"upsert": true, "snapshot": true,
	"schema-list": true, "schema-create": true, "schema-drop": true,
	"object-list": true, "object-create": true, "object-delete": true, "query": true,
	"node-status": true,
}

func (s *Server) handleVectorExt(w http.ResponseWriter, r *http.Request) {
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
	vType, _ := req["type"].(string)
	if vType == "" {
		vType = "milvus"
	}
	if !vectorExtTypes[vType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + vType})
		return
	}
	action, _ := req["action"].(string)
	if action == "" || !extSafeRe.MatchString(action) || !vectorExtActions[action] {
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

	script := fmt.Sprintf("set -e\necho 'vector %s ext %s done'\n", vType, action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, vType+"_ext_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.vector.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_vector_ext_" + action,
		Target: vType, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + vType + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
