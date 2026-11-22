// db_pg_mvcc_ops.go PostgreSQL MVCC / 表膨胀诊断与维护。
package controlplane

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// pgMVCCActions PostgreSQL MVCC/bloat action 白名单。
var pgMVCCActions = map[string]bool{
	"bloat-estimate":      true,
	"vacuum":              true,
	"vacuum-full":         true,
	"analyze":             true,
	"autovacuum-settings": true,
	"freeze-age":          true,
	"reindex":             true,
	"dead-tuples":         true,
}

// handleDBPGMVCC 处理 POST /api/v1/db/postgresql/mvcc。
func (s *Server) handleDBPGMVCC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		DB       string `json:"db"`
		Table    string `json:"table"`
		Schema   string `json:"schema"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !pgMVCCActions[req.Action] {
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
	table := req.Table
	if table != "" {
		if _, err := opsDBName("table", table); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	schema := req.Schema
	if schema != "" {
		if _, err := opsDBName("schema", schema); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	} else {
		schema = "public"
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

	script := buildDBPGMVCScript(req.Action, db, table, schema)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "db_pg_mvcc_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.pg.mvcc", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_pg_mvcc_" + req.Action,
		Target: db, Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID + " table=" + table + " schema=" + schema),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": req.Action})
}

func buildDBPGMVCScript(action, db, table, schema string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	dbQ := shellQuote(db)
	switch action {
	case "bloat-estimate":
		if table != "" {
			fmt.Fprintf(&b, "psql -d %s -c \"SELECT schemaname, relname, n_dead_tup, n_live_tup, pg_size_pretty(pg_total_relation_size(relid)) AS size FROM pg_stat_user_tables WHERE relname=%s;\"\n", dbQ, shellQuote(table))
		} else {
			fmt.Fprintf(&b, "psql -d %s -c \"SELECT schemaname, relname, n_dead_tup, n_live_tup, pg_size_pretty(pg_total_relation_size(relid)) AS size FROM pg_stat_user_tables ORDER BY n_dead_tup DESC;\"\n", dbQ)
		}
	case "vacuum":
		if table != "" {
			fmt.Fprintf(&b, "psql -d %s -c \"VACUUM (VERBOSE) %s.%s;\"\n", dbQ, schema, table)
		} else {
			fmt.Fprintf(&b, "psql -d %s -c \"VACUUM (VERBOSE);\"\n", dbQ)
		}
	case "vacuum-full":
		if table != "" {
			fmt.Fprintf(&b, "psql -d %s -c \"VACUUM FULL %s.%s;\"\n", dbQ, schema, table)
		} else {
			fmt.Fprintf(&b, "psql -d %s -c \"VACUUM FULL;\"\n", dbQ)
		}
	case "analyze":
		if table != "" {
			fmt.Fprintf(&b, "psql -d %s -c \"ANALYZE %s.%s;\"\n", dbQ, schema, table)
		} else {
			fmt.Fprintf(&b, "psql -d %s -c \"ANALYZE;\"\n", dbQ)
		}
	case "autovacuum-settings":
		fmt.Fprintf(&b, "psql -d %s -c \"SELECT name, setting FROM pg_settings WHERE name LIKE 'autovacuum%%' ORDER BY name;\"\n", dbQ)
	case "freeze-age":
		fmt.Fprintf(&b, "psql -d %s -c \"SELECT datname, age(datfrozenxid) AS freeze_age FROM pg_database ORDER BY freeze_age DESC;\"\n", dbQ)
		if table != "" {
			fmt.Fprintf(&b, "psql -d %s -c \"SELECT relname, age(relfrozenxid) AS freeze_age FROM pg_class WHERE relname=%s;\"\n", dbQ, shellQuote(table))
		}
	case "reindex":
		if table != "" {
			fmt.Fprintf(&b, "psql -d %s -c \"REINDEX TABLE %s.%s;\"\n", dbQ, schema, table)
		} else {
			fmt.Fprintf(&b, "psql -d %s -c \"REINDEX DATABASE %s;\"\n", dbQ, db)
		}
	case "dead-tuples":
		if table != "" {
			fmt.Fprintf(&b, "psql -d %s -c \"SELECT schemaname, relname, n_live_tup, n_dead_tup, last_vacuum, last_autovacuum FROM pg_stat_user_tables WHERE relname=%s;\"\n", dbQ, shellQuote(table))
		} else {
			fmt.Fprintf(&b, "psql -d %s -c \"SELECT schemaname, relname, n_live_tup, n_dead_tup, last_vacuum, last_autovacuum FROM pg_stat_user_tables ORDER BY n_dead_tup DESC;\"\n", dbQ)
		}
	}
	fmt.Fprintf(&b, "echo 'postgresql mvcc %s done'\n", action)
	return b.String()
}
