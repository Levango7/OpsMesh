// db_pg_monitor_ops.go PostgreSQL 监控查询运维（stats/locks/long-running/connections/cache-hit/replication-lag/wal/checkpoints）。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// pgMonitorActions PostgreSQL 监控 action 白名单。
var pgMonitorActions = map[string]bool{
	"stats":           true,
	"locks":           true,
	"long-running":    true,
	"connections":     true,
	"cache-hit":       true,
	"replication-lag": true,
	"wal":             true,
	"checkpoints":     true,
}

// handleDBPGMonitor 处理 POST /api/v1/db/postgresql/monitor。
func (s *Server) handleDBPGMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"`
		DB        string `json:"db"`
		Threshold string `json:"threshold"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !pgMonitorActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	db := req.DB
	if db == "" {
		db = "postgres"
	}
	if _, err := opsDBName("db", db); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	threshold := req.Threshold
	if threshold == "" {
		threshold = "30"
	}
	if !regexp.MustCompile(`^\d+$`).MatchString(threshold) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid threshold"})
		return
	}

	actx, ok := s.subsystemAuthorize(w, r, "task:write")
	if !ok {
		return
	}
	if dev := s.store.Device(req.DeviceID); dev != nil && dev.TenantID != "" && actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	if ag := s.store.Agent(req.DeviceID); ag != nil && ag.TenantID != "" && actx.TenantID != "" && ag.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}

	script := buildDBPGMonitorScript(req.Action, db, threshold)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "db_pg_monitor_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.pg.monitor", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_pg_monitor_" + req.Action,
		Target: db, Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID + " threshold=" + threshold),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": req.Action})
}

func buildDBPGMonitorScript(action, db, threshold string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	dbQ := shellQuote(db)
	switch action {
	case "stats":
		fmt.Fprintf(&b, "psql -d %s -c \"SELECT datname, numbackends, xact_commit, xact_rollback, blks_read, blks_hit, tup_returned, tup_fetched, tup_inserted, tup_updated, tup_deleted, conflicts, temp_files, temp_bytes, deadlocks FROM pg_stat_database;\"\n", dbQ)
	case "locks":
		fmt.Fprintf(&b, "psql -d %s -c \"SELECT pid, locktype, relation::regclass, mode, granted FROM pg_locks;\"\n", dbQ)
		fmt.Fprintf(&b, "psql -d %s -c \"SELECT blocked_locks.pid AS blocked_pid, blocking_locks.pid AS blocking_pid, blocked_activity.query AS blocked_query, blocking_activity.query AS blocking_query FROM pg_catalog.pg_locks blocked_locks JOIN pg_catalog.pg_stat_activity blocked_activity ON blocked_activity.pid = blocked_locks.pid JOIN pg_catalog.pg_locks blocking_locks ON blocking_locks.locktype = blocked_locks.locktype AND blocking_locks.pid <> blocked_locks.pid JOIN pg_catalog.pg_stat_activity blocking_activity ON blocking_activity.pid = blocking_locks.pid WHERE NOT blocked_locks.granted;\"\n", dbQ)
	case "long-running":
		fmt.Fprintf(&b, "psql -d %s -c \"SELECT pid, usename, state, query_start, (now() - query_start) AS duration, query FROM pg_stat_activity WHERE state = 'active' AND (now() - query_start) > interval '%s seconds' ORDER BY query_start;\"\n", dbQ, threshold)
	case "connections":
		fmt.Fprintf(&b, "psql -d %s -c \"SELECT state, count(*) AS count FROM pg_stat_activity GROUP BY state;\"\n", dbQ)
	case "cache-hit":
		fmt.Fprintf(&b, "psql -d %s -c \"SELECT datname, (blks_hit::numeric / NULLIF(blks_hit + blks_read, 0)) * 100 AS cache_hit_ratio FROM pg_stat_database;\"\n", dbQ)
	case "replication-lag":
		fmt.Fprintf(&b, "psql -d %s -c \"SELECT pid, client_addr, state, sent_lsn, write_lsn, flush_lsn, replay_lsn, (sent_lsn - replay_lsn) AS replay_lag FROM pg_stat_replication;\"\n", dbQ)
	case "wal":
		fmt.Fprintf(&b, "psql -d %s -c \"SELECT wal_records, wal_fpi, wal_bytes, wal_buffers_full, wal_write, wal_sync, wal_write_time, wal_sync_time FROM pg_stat_wal;\"\n", dbQ)
	case "checkpoints":
		fmt.Fprintf(&b, "psql -d %s -c \"SELECT checkpoints_timed, checkpoints_req, checkpoint_write_time, checkpoint_sync_time, buffers_checkpoint, buffers_clean, maxwritten_clean, buffers_backend, buffers_backend_fsync, buffers_alloc FROM pg_stat_bgwriter;\"\n", dbQ)
	}
	fmt.Fprintf(&b, "echo 'postgresql monitor %s done'\n", action)
	return b.String()
}
