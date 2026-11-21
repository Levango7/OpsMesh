// db_pg_fdw_ops.go PostgreSQL 外部数据包装器（FDW）运维。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// pgFDWActions PostgreSQL FDW action 白名单。
var pgFDWActions = map[string]bool{
	"servers-list":  true,
	"server-create": true,
	"server-drop":   true,
	"tables-list":   true,
	"table-create":  true,
	"table-drop":    true,
	"import-schema": true,
	"mappings-list": true,
}

// pgFDWOptionsRe 是 FDW OPTIONS 白名单。
var pgFDWOptionsRe = regexp.MustCompile(`^[A-Za-z0-9 _=,'".\-]+$`)

// handleDBPGFDW 处理 POST /api/v1/db/postgresql/fdw。
func (s *Server) handleDBPGFDW(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID     string `json:"deviceID"`
		Action       string `json:"action"`
		DB           string `json:"db"`
		ServerName   string `json:"serverName"`
		FDWName      string `json:"fdwName"`
		ForeignTable string `json:"foreignTable"`
		LocalTable   string `json:"localTable"`
		Options      string `json:"options"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !pgFDWActions[req.Action] {
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
	for _, f := range []struct {
		name, val string
	}{
		{"serverName", req.ServerName},
		{"fdwName", req.FDWName},
		{"foreignTable", req.ForeignTable},
		{"localTable", req.LocalTable},
	} {
		if f.val != "" {
			if _, err := opsDBName(f.name, f.val); err != nil {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
	}
	if req.Options != "" {
		if !pgFDWOptionsRe.MatchString(req.Options) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid options"})
			return
		}
	}

	switch req.Action {
	case "server-create", "server-drop":
		if req.ServerName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "serverName is required"})
			return
		}
	}
	if req.Action == "server-create" {
		if req.FDWName == "" {
			req.FDWName = "postgres_fdw"
		}
	}
	switch req.Action {
	case "table-create", "table-drop":
		if req.ForeignTable == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "foreignTable is required"})
			return
		}
	}
	if req.Action == "import-schema" {
		if req.ServerName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "serverName is required"})
			return
		}
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

	script := buildDBPGFDWScript(req.Action, db, req.ServerName, req.FDWName, req.ForeignTable, req.LocalTable, req.Options)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "db_pg_fdw_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.pg.fdw", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_pg_fdw_" + req.Action,
		Target: db, Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID + " server=" + req.ServerName + " table=" + req.ForeignTable),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": req.Action})
}

func buildDBPGFDWScript(action, db, serverName, fdwName, foreignTable, localTable, options string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	dbQ := shellQuote(db)
	opts := options
	if opts == "" {
		opts = "host 'localhost', dbname 'postgres'"
	}
	switch action {
	case "servers-list":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT srvname, srvowner, srvfdw FROM pg_foreign_server ORDER BY srvname;\"\n", dbQ))
	case "server-create":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"CREATE SERVER %s FOREIGN DATA WRAPPER %s OPTIONS (%s);\"\n", dbQ, serverName, fdwName, opts))
	case "server-drop":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"DROP SERVER IF EXISTS %s CASCADE;\"\n", dbQ, serverName))
	case "tables-list":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT foreign_table_schema, foreign_table_name, foreign_server_name FROM information_schema.foreign_tables ORDER BY foreign_table_name;\"\n", dbQ))
	case "table-create":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"CREATE FOREIGN TABLE %s (id int) SERVER %s OPTIONS (%s);\"\n", dbQ, foreignTable, serverName, opts))
	case "table-drop":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"DROP FOREIGN TABLE IF EXISTS %s;\"\n", dbQ, foreignTable))
	case "import-schema":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"IMPORT FOREIGN SCHEMA public FROM SERVER %s INTO public;\"\n", dbQ, serverName))
	case "mappings-list":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT umuser, umserver, umoptions FROM pg_user_mappings ORDER BY umserver;\"\n", dbQ))
	}
	fmt.Fprintf(&b, "echo 'postgresql fdw %s done'\n", action)
	return b.String()
}
