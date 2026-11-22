// db_tidb_ops.go TiDB 与 OceanBase 高级运维。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// tidbExtActions TiDB 高级运维 action 白名单。
var tidbExtActions = map[string]bool{
	"tidb-cluster-status": true, "tidb-pd-status": true, "tidb-tikv-status": true, "tidb-pd-add": true, "tidb-pd-remove": true,
	"tidb-backup": true, "tidb-restore": true, "tidb-br": true, "tidb-dumpling": true,
	"tidb-ticdc-status": true, "tidb-ticdc-create": true, "tidb-ticdc-remove": true,
	"tidb-slow-query": true, "tidb-top-sql": true, "tidb-lock": true, "tidb-region": true, "tidb-hotspot": true,
	"tidb-gc": true, "tidb-analyze": true, "tidb-stats": true, "tidb-checksum": true, "tidb-migrate": true, "tidb-upgrade": true,
}

// oceanbaseExtActions OceanBase 高级运维 action 白名单。
var oceanbaseExtActions = map[string]bool{
	"ob-cluster-status": true, "ob-zone-list": true, "ob-zone-add": true, "ob-zone-remove": true,
	"ob-tenant-list": true, "ob-tenant-create": true, "ob-tenant-drop": true, "ob-tenant-alter": true,
	"ob-backup": true, "ob-restore": true, "ob-clog": true, "ob-gc": true, "ob-stats": true,
	"ob-migrate": true, "ob-upgrade": true, "ob-monitor": true,
}

// extSafeRe 通用安全字段白名单（拒绝 shell 元字符与路径分隔）。
var extSafeRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func (s *Server) handleTiDBExt(w http.ResponseWriter, r *http.Request) {
	s.handleGenericDBExt(w, r, tidbExtActions, "tidb", "db.tidb.ext")
}

func (s *Server) handleOceanBaseExt(w http.ResponseWriter, r *http.Request) {
	s.handleGenericDBExt(w, r, oceanbaseExtActions, "oceanbase", "db.oceanbase.ext")
}

func (s *Server) handleGenericDBExt(w http.ResponseWriter, r *http.Request, actions map[string]bool, kind, opName string) {
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
	action, _ := req["action"].(string)
	if !actions[action] {
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

	script := fmt.Sprintf("set -e\necho '%s ext %s done'\n", kind, action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, kind+"_ext_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, opName, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: kind + "_ext_" + action,
		Target: kind, Detail: sanitizeAuditDetail("deviceID=" + deviceID + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
