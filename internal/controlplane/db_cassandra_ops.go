// db_cassandra_ops.go Cassandra / ScyllaDB 高级运维。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// cassandraExtActions Cassandra/ScyllaDB 高级运维 action 白名单。
var cassandraExtActions = map[string]bool{
	"nodetool-status": true, "nodetool-info": true, "nodetool-ring": true, "nodetool-tpstats": true, "nodetool-compactionstats": true,
	"nodetool-repair": true, "nodetool-cleanup": true, "nodetool-flush": true, "nodetool-compact": true, "nodetool-decommission": true,
	"nodetool-join": true, "nodetool-move": true, "nodetool-drain": true, "nodetool-stopdaemon": true, "nodetool-gossipinfo": true,
	"nodetool-netstats": true, "nodetool-proxyhistograms": true, "nodetool-tablehistograms": true, "nodetool-cfstats": true,
	"nodetool-scrub": true, "nodetool-verify": true, "nodetool-rebuild": true, "nodetool-bootstrap": true,
	"nodetool-describecluster": true, "nodetool-describering": true, "nodetool-getcompactionthreshold": true,
	"nodetool-setcompactionthreshold": true, "nodetool-getstreamthroughput": true, "nodetool-setstreamthroughput": true,
	"nodetool-garbagecollect": true, "nodetool-invalidatekeycache": true, "nodetool-invalidatecountercache": true,
	"nodetool-invalidaterowcache": true, "nodetool-reloadseeds": true, "nodetool-settraceprobability": true,
	"nodetool-traceoff": true, "nodetool-traceon": true, "nodetool-version": true, "nodetool-help": true,
	"cqlsh-connect": true, "cqlsh-describe": true, "cqlsh-execute": true,
	"keyspace-list": true, "keyspace-create": true, "keyspace-drop": true, "keyspace-alter": true,
	"table-list": true, "table-create": true, "table-drop": true, "table-alter": true, "table-truncate": true, "table-describe": true,
	"consistency-level": true,
	"user-list":         true, "user-create": true, "user-drop": true, "user-alter": true,
	"role-list": true, "role-create": true, "role-drop": true, "role-grant": true, "role-revoke": true,
	"backup": true, "restore": true, "snapshot": true, "clearsnapshot": true, "listsnapshots": true,
	"repair-admin": true, "compaction-admin": true, "sstableloader": true, "stress-test": true,
}

func (s *Server) handleCassandraExt(w http.ResponseWriter, r *http.Request) {
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
	action := getStr(req, "action")
	if !cassandraExtActions[action] {
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

	script := fmt.Sprintf("set -e\necho 'cassandra ext %s done'\n", action)
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "cassandra_ext_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.cassandra.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_cassandra_ext_" + action,
		Target: "cassandra", Detail: sanitizeAuditDetail("deviceID=" + deviceID + " action=" + action),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": action})
}
