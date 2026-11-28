// monitor_ext_ops.go 监控系统扩展运维（Zabbix/Nightingale/SkyWalking/Nagios）。
package controlplane

import (
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// monitorExtTypes 监控扩展类型白名单。
var monitorExtTypes = map[string]bool{
	"zabbix": true, "nightingale": true, "skywalking": true, "nagios": true,
}

// monitorExtActions 监控扩展按类型 action 白名单。
var monitorExtActions = map[string]map[string]bool{
	"zabbix": {
		"status": true, "deploy": true, "start": true, "stop": true, "restart": true, "version": true,
		"host-list": true, "host-create": true, "host-delete": true,
		"hostgroup-list": true, "hostgroup-create": true,
		"template-list": true, "template-link": true,
		"trigger-list": true, "trigger-create": true, "trigger-delete": true,
		"item-list": true, "item-create": true,
		"user-list": true, "user-create": true, "user-delete": true,
		"media-list": true, "media-create": true,
		"alert-list": true,
		"monitoring": true,
	},
	"nightingale": {
		"status": true, "deploy": true, "start": true, "stop": true, "restart": true, "version": true,
		"metric-query":    true,
		"alert-rule-list": true, "alert-rule-create": true, "alert-rule-delete": true,
		"dashboard-list": true, "dashboard-create": true,
		"user-list": true, "user-create": true, "user-delete": true,
		"monitoring": true,
	},
	"skywalking": {
		"status": true, "deploy": true, "start": true, "stop": true, "restart": true, "version": true,
		"service-list": true, "instance-list": true, "endpoint-list": true,
		"trace-list": true, "trace-get": true,
		"alarm-list": true, "alarm-create": true, "alarm-delete": true,
		"monitoring": true,
	},
	"nagios": {
		"status": true, "deploy": true, "start": true, "stop": true, "restart": true, "version": true,
		"host-list": true, "host-create": true, "host-delete": true,
		"service-list": true, "service-create": true, "service-delete": true,
		"command-list": true,
		"contact-list": true, "contact-create": true, "contact-delete": true,
		"monitoring": true,
	},
}

func (s *Server) handleMonitorExt(w http.ResponseWriter, r *http.Request) {
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
		typ = "zabbix"
	}
	if !monitorExtTypes[typ] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + typ})
		return
	}
	action := getStr(req, "action")
	if !monitorExtActions[typ][action] {
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

	script := "set -e\n" + "echo " + opsEcho("monitor ext %s %s done", typ, action) + "\n"
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "monitor_ext_"+typ+"_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "monitor.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "monitor_ext_" + opsSQLString(typ) + "_" + opsSQLString(action),
		Target: typ, Detail: sanitizeAuditDetail("deviceID=" + opsSQLString(deviceID) + " type=" + opsSQLString(typ) + " action=" + opsSQLString(action)),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
