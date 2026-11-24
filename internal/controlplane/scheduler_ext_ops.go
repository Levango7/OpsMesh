// scheduler_ext_ops.go 调度系统扩展运维（Azkaban/DolphinScheduler）。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// schedulerExtTypes 调度扩展类型白名单。
var schedulerExtTypes = map[string]bool{
	"azkaban": true, "dolphinscheduler": true,
}

// schedulerExtActions 调度扩展按类型 action 白名单。
var schedulerExtActions = map[string]map[string]bool{
	"azkaban": {
		"status": true, "deploy": true, "start": true, "stop": true, "restart": true, "version": true,
		"project-list": true, "project-create": true, "project-delete": true,
		"flow-list": true, "flow-run": true, "flow-upload": true,
		"execution-list": true, "execution-status": true, "execution-cancel": true, "execution-logs": true,
		"schedule-list": true, "schedule-create": true, "schedule-delete": true,
		"sla-list": true, "sla-create": true, "sla-delete": true,
		"job-list": true, "job-status": true,
		"user-list": true, "user-create": true,
		"monitoring": true,
	},
	"dolphinscheduler": {
		"status": true, "deploy": true, "start": true, "stop": true, "restart": true, "version": true,
		"project-list": true, "project-create": true, "project-delete": true,
		"workflow-list": true, "workflow-create": true, "workflow-run": true,
		"task-list": true, "task-create": true,
		"schedule-list": true, "schedule-create": true, "schedule-delete": true,
		"tenant-list": true, "user-list": true,
		"monitoring": true,
	},
}

func (s *Server) handleSchedulerExt(w http.ResponseWriter, r *http.Request) {
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
		typ = "azkaban"
	}
	if !schedulerExtTypes[typ] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + typ})
		return
	}
	action := getStr(req, "action")
	if !schedulerExtActions[typ][action] {
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

	script := fmt.Sprintf("set -e\necho 'scheduler ext %s %s done'\n", typ, action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "scheduler_ext_"+typ+"_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "scheduler.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "scheduler_ext_" + typ + "_" + action,
		Target: typ, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " type=" + typ + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}

func (s *Server) registerSchedulerExtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/scheduler/ext", s.handleSchedulerExt)
}
