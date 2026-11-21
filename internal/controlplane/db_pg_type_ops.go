// db_pg_type_ops.go PostgreSQL 自定义类型运维。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// pgTypeActions PostgreSQL 自定义类型 action 白名单。
var pgTypeActions = map[string]bool{
	"types-list":            true,
	"enums-list":            true,
	"domains-list":          true,
	"type-create-enum":      true,
	"type-create-domain":    true,
	"type-create-composite": true,
	"type-drop":             true,
	"type-info":             true,
}

// pgTypeKinds PostgreSQL 类型种类白名单。
var pgTypeKinds = map[string]bool{
	"enum":      true,
	"domain":    true,
	"composite": true,
}

// pgTypeValuesRe 是枚举值列表白名单。
var pgTypeValuesRe = regexp.MustCompile(`^[A-Za-z0-9 _,'"\-]+$`)

// handleDBPGTypes 处理 POST /api/v1/db/postgresql/types。
func (s *Server) handleDBPGTypes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		DB       string `json:"db"`
		TypeName string `json:"typeName"`
		TypeKind string `json:"typeKind"`
		Values   string `json:"values"`
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
	if !pgTypeActions[req.Action] {
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
	if req.TypeKind != "" {
		if !pgTypeKinds[req.TypeKind] {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid typeKind"})
			return
		}
	}
	if req.TypeName != "" {
		if _, err := opsDBName("typeName", req.TypeName); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if req.Values != "" {
		if !pgTypeValuesRe.MatchString(req.Values) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid values"})
			return
		}
	}
	schema := req.Schema
	if schema == "" {
		schema = "public"
	}
	if _, err := opsDBName("schema", schema); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	switch req.Action {
	case "type-create-enum", "type-create-domain", "type-create-composite", "type-drop", "type-info":
		if req.TypeName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "typeName is required"})
			return
		}
	}
	if req.Action == "type-create-enum" {
		if req.Values == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "values is required"})
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

	script := buildDBPGTypesScript(req.Action, db, req.TypeName, req.TypeKind, req.Values, schema)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "db_pg_types_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.pg.types", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_pg_types_" + req.Action,
		Target: db, Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID + " type=" + req.TypeName),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": req.Action})
}

func buildDBPGTypesScript(action, db, typeName, typeKind, values, schema string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	dbQ := shellQuote(db)
	switch action {
	case "types-list":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT typname, typtype FROM pg_type WHERE typtype IN ('e','d','c') ORDER BY typname;\"\n", dbQ))
	case "enums-list":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT t.typname, e.enumlabel FROM pg_type t JOIN pg_enum e ON t.oid = e.enumtypid ORDER BY t.typname, e.enumsortorder;\"\n", dbQ))
	case "domains-list":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT typname, pg_get_constraintdef(oid) FROM pg_type WHERE typtype='d' ORDER BY typname;\"\n", dbQ))
	case "type-create-enum":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"CREATE TYPE %s.%s AS ENUM (%s);\"\n", dbQ, schema, typeName, values))
	case "type-create-domain":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"CREATE DOMAIN %s.%s AS text;\"\n", dbQ, schema, typeName))
	case "type-create-composite":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"CREATE TYPE %s.%s AS (id int, name text);\"\n", dbQ, schema, typeName))
	case "type-drop":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"DROP TYPE IF EXISTS %s.%s CASCADE;\"\n", dbQ, schema, typeName))
	case "type-info":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT typname, typtype, typnotnull FROM pg_type WHERE typname=%s;\"\n", dbQ, shellQuote(typeName)))
	}
	fmt.Fprintf(&b, "echo 'postgresql types %s done'\n", action)
	return b.String()
}
