// db_pg_gis_ops.go PostgreSQL PostGIS/GIS 运维。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// pgGISActions PostgreSQL GIS action 白名单。
var pgGISActions = map[string]bool{
	"postgis-check":        true,
	"extension-install":    true,
	"spatial-columns":      true,
	"spatial-index-create": true,
	"spatial-index-drop":   true,
	"geometry-type":        true,
	"srid-list":            true,
	"spatial-query-sample": true,
	"geometry-validate":    true,
	"geometry-transform":   true,
	"spatial-stats":        true,
}

// pgGISIndexTypes PostgreSQL 空间索引类型白名单。
var pgGISIndexTypes = map[string]bool{
	"gist":   true,
	"spgist": true,
	"brin":   true,
}

// handleDBPGGIS 处理 POST /api/v1/db/postgresql/gis。
func (s *Server) handleDBPGGIS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID   string `json:"deviceID"`
		Action     string `json:"action"`
		DB         string `json:"db"`
		Schema     string `json:"schema"`
		TableName  string `json:"tableName"`
		GeomColumn string `json:"geomColumn"`
		SRID       string `json:"srid"`
		IndexType  string `json:"indexType"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !pgGISActions[req.Action] {
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
	schema := req.Schema
	if schema == "" {
		schema = "public"
	}
	if _, err := opsDBName("schema", schema); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.TableName != "" {
		if _, err := opsDBName("tableName", req.TableName); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if req.GeomColumn != "" {
		if _, err := opsDBName("geomColumn", req.GeomColumn); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if req.SRID != "" {
		if !regexp.MustCompile(`^\d+$`).MatchString(req.SRID) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid srid"})
			return
		}
	}
	if req.IndexType != "" {
		if !pgGISIndexTypes[req.IndexType] {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid indexType"})
			return
		}
	}

	switch req.Action {
	case "spatial-columns", "spatial-index-create", "spatial-index-drop", "geometry-type", "spatial-query-sample", "geometry-validate", "geometry-transform", "spatial-stats":
		if req.TableName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "tableName is required"})
			return
		}
	}
	if req.Action == "spatial-index-create" && req.GeomColumn == "" {
		req.GeomColumn = "geom"
	}
	if req.SRID == "" {
		req.SRID = "4326"
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

	script := buildDBPGGISScript(req.Action, db, schema, req.TableName, req.GeomColumn, req.SRID, req.IndexType)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "db_pg_gis_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.pg.gis", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_pg_gis_" + req.Action,
		Target: db, Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID + " table=" + req.TableName + " geom=" + req.GeomColumn),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": req.Action})
}

func buildDBPGGISScript(action, db, schema, tableName, geomColumn, srid, indexType string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	dbQ := shellQuote(db)
	it := indexType
	if it == "" {
		it = "gist"
	}
	sridInt, _ := strconv.Atoi(srid)
	if sridInt == 0 {
		sridInt = 4326
	}
	switch action {
	case "postgis-check":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT PostGIS_Full_Version();\"\n", dbQ))
	case "extension-install":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"CREATE EXTENSION IF NOT EXISTS postgis;\"\n", dbQ))
	case "spatial-columns":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT f_table_schema, f_table_name, f_geometry_column, coord_dimension, srid, type FROM geometry_columns WHERE f_table_schema=%s;\"\n", dbQ, shellQuote(schema)))
	case "spatial-index-create":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"CREATE INDEX %s_%s_idx ON %s.%s USING %s (%s);\"\n", dbQ, tableName, geomColumn, schema, tableName, it, geomColumn))
	case "spatial-index-drop":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"DROP INDEX IF EXISTS %s_%s_idx;\"\n", dbQ, tableName, geomColumn))
	case "geometry-type":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT DISTINCT GeometryType(%s) FROM %s.%s;\"\n", dbQ, geomColumn, schema, tableName))
	case "srid-list":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT srid, auth_name, auth_srid, srtext FROM spatial_ref_sys ORDER BY srid LIMIT 50;\"\n", dbQ))
	case "spatial-query-sample":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT %s, ST_AsText(%s) FROM %s.%s LIMIT 10;\"\n", dbQ, geomColumn, geomColumn, schema, tableName))
	case "geometry-validate":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT count(*) FILTER (WHERE NOT ST_IsValid(%s)) AS invalid FROM %s.%s;\"\n", dbQ, geomColumn, schema, tableName))
	case "geometry-transform":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT ST_Transform(%s, %d) FROM %s.%s LIMIT 5;\"\n", dbQ, geomColumn, sridInt, schema, tableName))
	case "spatial-stats":
		b.WriteString(fmt.Sprintf("psql -d %s -c \"SELECT count(*) AS total, ST_Extent(%s) AS extent FROM %s.%s;\"\n", dbQ, geomColumn, schema, tableName))
	}
	fmt.Fprintf(&b, "echo 'postgresql gis %s done'\n", action)
	return b.String()
}
