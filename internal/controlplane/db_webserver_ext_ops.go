// db_webserver_ext_ops.go Web 服务器高级运维（Apache/Tomcat/Jetty/OpenResty/Nginx：虚拟主机/模块/SSL/应用/连接器/JVM/上游/Lua/监控/调优等）。
package controlplane

import (
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

var webserverExtTypes = map[string]bool{
	"apache": true, "httpd": true, "tomcat": true, "jetty": true,
	"openresty": true, "nginx": true,
}

var webserverExtActions = map[string]bool{
	"vhost-list": true, "vhost-create": true, "vhost-drop": true,
	"module-list": true, "module-enable": true, "module-disable": true,
	"ssl-status": true, "ssl-cert-list": true, "ssl-cert-install": true,
	"graceful-restart": true, "config-test": true, "access-log": true, "error-log": true,
	"status": true, "tuning": true,
	"app-list": true, "app-deploy": true, "app-undeploy": true, "app-start": true, "app-stop": true,
	"connector-list": true, "jvm-status": true, "thread-pool": true,
	"server-status": true, "handler-list": true, "context-list": true,
	"nginx-config-test": true, "nginx-reload": true, "nginx-status": true,
	"upstream-list": true, "upstream-add": true, "upstream-remove": true,
	"lua-module-list": true, "graceful-reload": true,
}

func (s *Server) handleWebServerExt(w http.ResponseWriter, r *http.Request) {
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
	wType := getStr(req, "type")
	if wType == "" {
		wType = "nginx"
	}
	if !webserverExtTypes[wType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + wType})
		return
	}
	action := getStr(req, "action")
	if action == "" || !extSafeRe.MatchString(action) || !webserverExtActions[action] {
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

	script := "set -e\n" + "echo " + opsEcho("webserver %s ext %s done", wType, action) + "\n"
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, wType+"_ext_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.webserver.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_webserver_ext_" + action,
		Target: wType, Detail: sanitizeAuditDetail("deviceID=" + opsSQLString(deviceID) + " type=" + opsSQLString(wType) + " action=" + opsSQLString(action)),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
