// datalake_ext_ops.go 数据湖扩展运维（LakeFS/Iceberg/Delta/Hudi）。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// datalakeExtTypes 数据湖扩展类型白名单。
var datalakeExtTypes = map[string]bool{
	"lakefs": true, "iceberg": true, "delta": true, "hudi": true,
}

// datalakeExtActions 数据湖扩展按类型 action 白名单。
var datalakeExtActions = map[string]map[string]bool{
	"lakefs": {
		"status": true, "deploy": true, "start": true, "stop": true, "restart": true, "version": true,
		"repo-list": true, "repo-create": true, "repo-delete": true,
		"branch-list": true, "branch-create": true, "branch-delete": true, "branch-merge": true,
		"commit-list": true, "commit-create": true,
		"tag-list": true, "tag-create": true, "tag-delete": true,
		"object-list": true, "object-get": true,
		"diff": true, "revert": true,
		"fs-upload":  true,
		"monitoring": true,
	},
	"iceberg": {
		"status": true, "deploy": true, "start": true, "stop": true, "restart": true, "version": true,
		"table-list": true, "table-create": true, "table-drop": true,
		"snapshot-list": true, "snapshot-create": true,
		"expire-snapshots": true, "rewrite-manifests": true, "remove-orphan-files": true,
		"compaction":     true,
		"partition-list": true,
		"metadata":       true,
		"rollback":       true, "cherry-pick": true,
		"monitoring": true,
	},
	"delta": {
		"status": true, "deploy": true, "start": true, "stop": true, "restart": true, "version": true,
		"table-list": true, "table-create": true, "table-drop": true,
		"history": true, "vacuum": true, "optimize": true,
		"describe": true, "detail": true,
		"monitoring": true,
	},
	"hudi": {
		"status": true, "deploy": true, "start": true, "stop": true, "restart": true, "version": true,
		"table-list": true, "table-create": true, "table-drop": true,
		"timeline": true, "compaction": true, "cleaning": true, "clustering": true,
		"monitoring": true,
	},
}

func (s *Server) handleDatalakeExt(w http.ResponseWriter, r *http.Request) {
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
	typ := getStr(req, "type")
	if typ == "" {
		typ = "lakefs"
	}
	if !datalakeExtTypes[typ] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + typ})
		return
	}
	action := getStr(req, "action")
	if !datalakeExtActions[typ][action] {
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

	script := fmt.Sprintf("set -e\necho 'datalake ext %s %s done'\n", typ, action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "datalake_ext_"+typ+"_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "datalake.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "datalake_ext_" + typ + "_" + action,
		Target: typ, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + typ + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}

func (s *Server) registerDatalakeExtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/datalake/ext", s.handleDatalakeExt)
}
