// db_pg_ext_ops.go PostgreSQL 扩展生命周期运维（install/upgrade/drop/info/list）。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// pgExtensionNameRe 是 PostgreSQL 扩展名白名单：字母/数字/下划线，最长 63（PG 标识符上限）。
var pgExtensionNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// pgExtActions PostgreSQL 扩展生命周期 action 白名单。
var pgExtActions = map[string]bool{
	"list-installed": true,
	"list-available": true,
	"install":        true,
	"upgrade":        true,
	"drop":           true,
	"info":           true,
}

// handleDBPGExtensions 处理 POST /api/v1/db/postgresql/extensions。
func (s *Server) handleDBPGExtensions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID      string `json:"deviceID"`
		Action        string `json:"action"`
		ExtensionName string `json:"extensionName"`
		Version       string `json:"version"`
		Schema        string `json:"schema"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !pgExtActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	// 扩展名：list-* 不强制；其余 action 必填且白名单。
	needsExt := req.Action == "install" || req.Action == "upgrade" || req.Action == "drop" || req.Action == "info"
	if needsExt {
		if req.ExtensionName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "extensionName is required"})
			return
		}
		if !pgExtensionNameRe.MatchString(req.ExtensionName) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid extensionName"})
			return
		}
	}
	if req.ExtensionName != "" && !pgExtensionNameRe.MatchString(req.ExtensionName) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid extensionName"})
		return
	}
	// 版本号（可选）：宽松白名单，拒绝 shell 元字符。
	if req.Version != "" {
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`).MatchString(req.Version) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version"})
			return
		}
	}
	// schema（可选）：复用 opsDBName 白名单。
	if req.Schema != "" {
		if _, err := opsDBName("schema", req.Schema); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}

	actx, ok := s.subsystemAuthorize(w, r, "task:write")
	if !ok {
		return
	}
	// 跨租户校验。
	if dev := s.store.Device(req.DeviceID); dev != nil && dev.TenantID != "" && actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	if ag := s.store.Agent(req.DeviceID); ag != nil && ag.TenantID != "" && actx.TenantID != "" && ag.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}

	script := buildDBPGExtensionsScript(req.Action, req.ExtensionName, req.Version, req.Schema)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "db_pg_extensions_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.pg.extensions", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_pg_extensions_" + req.Action,
		Target: req.ExtensionName, Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID + " version=" + req.Version + " schema=" + req.Schema),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": req.Action})
}

func buildDBPGExtensionsScript(action, ext, version, schema string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	db := "postgres"
	schemaV := schema
	if schemaV == "" {
		schemaV = "public"
	}
	dbQ := shellQuote(db)
	switch action {
	case "list-installed":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT extname, extversion FROM pg_extension ORDER BY extname;\"\n", dbQ))
	case "list-available":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT name, default_version, installed_version FROM pg_available_extensions ORDER BY name;\"\n", dbQ))
	case "install":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"CREATE EXTENSION IF NOT EXISTS %s WITH SCHEMA %s;\"\n", dbQ, ext, schemaV))
		if version != "" {
			b.WriteString(fmt.Sprintf("psql -d %s -c \"ALTER EXTENSION %s UPDATE TO %s;\"\n", dbQ, ext, shellQuote(version)))
		}
	case "upgrade":
		if version != "" {
			b.WriteString(fmt.Sprintf("psql -d %s -c \"ALTER EXTENSION %s UPDATE TO %s;\"\n", dbQ, ext, shellQuote(version)))
		} else {
			b.WriteString(fmt.Sprintf("psql -d %s -c \"ALTER EXTENSION %s UPDATE;\"\n", dbQ, ext))
		}
	case "drop":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"DROP EXTENSION IF EXISTS %s;\"\n", dbQ, ext))
	case "info":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT extname, extversion, extrelocatable FROM pg_extension WHERE extname=%s;\"\n", dbQ, shellQuote(ext)))
		// PostGIS 诊断
		if ext == "postgis" {
			b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT PostGIS_Full_Version();\"\n", dbQ))
			b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT srid, auth_name, auth_srid, srtext FROM spatial_ref_sys LIMIT 5;\"\n", dbQ))
		}
	}
	fmt.Fprintf(&b, "echo 'postgresql extensions %s done'\n", action)
	return b.String()
}
