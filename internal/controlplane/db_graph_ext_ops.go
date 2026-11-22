// db_graph_ext_ops.go 图数据库高级运维（Neo4j/NebulaGraph：集群/库/空间/标签/边/索引/约束/用户/角色/备份/恢复/监控等）。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

var graphExtTypes = map[string]bool{
	"neo4j": true, "nebula": true, "nebulagraph": true,
}

var graphExtActions = map[string]bool{
	"cluster-status": true, "db-list": true, "db-create": true, "db-drop": true,
	"index-list": true, "index-create": true, "index-drop": true,
	"constraint-list": true, "constraint-create": true, "constraint-drop": true,
	"apoc-status": true, "backup": true, "restore": true,
	"user-list": true, "user-create": true, "user-drop": true,
	"role-list": true, "role-grant": true, "query-plan": true, "monitoring": true,
	"space-list": true, "space-create": true, "space-drop": true, "space-describe": true,
	"tag-list": true, "tag-create": true, "tag-drop": true,
	"edge-list": true, "edge-create": true, "edge-drop": true,
	"storage-status": true,
}

func (s *Server) handleGraphExt(w http.ResponseWriter, r *http.Request) {
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
	gType := getStr(req, "type")
	if gType == "" {
		gType = "neo4j"
	}
	if !graphExtTypes[gType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + gType})
		return
	}
	action := getStr(req, "action")
	if action == "" || !extSafeRe.MatchString(action) || !graphExtActions[action] {
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

	script := fmt.Sprintf("set -e\necho 'graph %s ext %s done'\n", gType, action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, gType+"_ext_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.graph.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_graph_ext_" + action,
		Target: gType, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + gType + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
