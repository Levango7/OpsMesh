// webcontainer_ext_ops.go Web 容器扩展运维（WebSphere/WebLogic）。
package controlplane

import (
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// webcontainerExtTypes Web 容器扩展类型白名单。
var webcontainerExtTypes = map[string]bool{
	"websphere": true, "weblogic": true,
}

// webcontainerExtActions Web 容器扩展按类型 action 白名单。
var webcontainerExtActions = map[string]map[string]bool{
	"websphere": {
		"status": true, "deploy": true, "start": true, "stop": true, "restart": true, "version": true,
		"server-list": true, "server-create": true, "server-start": true, "server-stop": true,
		"app-list": true, "app-install": true, "app-uninstall": true, "app-start": true, "app-stop": true,
		"jvm-config": true,
		"jdbc-list":  true, "jdbc-create": true,
		"cluster-list": true,
		"node-list":    true, "cell-status": true,
		"monitoring": true,
	},
	"weblogic": {
		"status": true, "deploy": true, "start": true, "stop": true, "restart": true, "version": true,
		"server-list": true, "server-create": true, "server-start": true, "server-stop": true,
		"app-list": true, "app-deploy": true, "app-undeploy": true, "app-start": true, "app-stop": true,
		"jvm-config": true,
		"jdbc-list":  true, "jdbc-create": true,
		"cluster-list": true, "cluster-create": true,
		"domain-status": true,
		"monitoring":    true,
	},
}

func (s *Server) handleWebcontainerExt(w http.ResponseWriter, r *http.Request) {
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
		typ = "websphere"
	}
	if !webcontainerExtTypes[typ] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + typ})
		return
	}
	action := getStr(req, "action")
	if !webcontainerExtActions[typ][action] {
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

	script := "set -e\n" + "echo " + opsEcho("webcontainer ext %s %s done", typ, action) + "\n"
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "webcontainer_ext_"+typ+"_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "webcontainer.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "webcontainer_ext_" + opsSQLString(typ) + "_" + opsSQLString(action),
		Target: typ, Detail: sanitizeAuditDetail("deviceID=" + opsSQLString(deviceID) + " type=" + opsSQLString(typ) + " action=" + opsSQLString(action)),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}

func (s *Server) registerWebcontainerExtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/webcontainer/ext", s.handleWebcontainerExt)
}
