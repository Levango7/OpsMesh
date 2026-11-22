// db_pg_partition_ops.go PostgreSQL 分区表运维。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// pgPartitionActions PostgreSQL 分区 action 白名单。
var pgPartitionActions = map[string]bool{
	"list-partitions": true,
	"create-range":    true,
	"create-list":     true,
	"create-hash":     true,
	"attach":          true,
	"detach":          true,
	"drop-partition":  true,
	"partman-status":  true,
	"partman-create":  true,
	"partman-run":     true,
}

// pgPartitionStrategies PostgreSQL 分区策略白名单。
var pgPartitionStrategies = map[string]bool{
	"range": true,
	"list":  true,
	"hash":  true,
}

// pgPartitionBoundSpecRe 是分区边界规格白名单。
var pgPartitionBoundSpecRe = regexp.MustCompile(`^[A-Za-z0-9 _,'()\-]+$`)

// handleDBPGPartition 处理 POST /api/v1/db/postgresql/partition。
func (s *Server) handleDBPGPartition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID      string `json:"deviceID"`
		Action        string `json:"action"`
		DB            string `json:"db"`
		TableName     string `json:"tableName"`
		PartitionName string `json:"partitionName"`
		BoundSpec     string `json:"boundSpec"`
		Strategy      string `json:"strategy"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !pgPartitionActions[req.Action] {
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
	if req.Strategy != "" {
		if !pgPartitionStrategies[req.Strategy] {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid strategy"})
			return
		}
	}
	if req.TableName != "" {
		if _, err := opsDBName("tableName", req.TableName); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if req.PartitionName != "" {
		if _, err := opsDBName("partitionName", req.PartitionName); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if req.BoundSpec != "" {
		if !pgPartitionBoundSpecRe.MatchString(req.BoundSpec) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid boundSpec"})
			return
		}
	}

	switch req.Action {
	case "create-range", "create-list", "create-hash", "attach", "detach", "drop-partition":
		if req.TableName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "tableName is required"})
			return
		}
	}
	switch req.Action {
	case "create-range", "create-list", "create-hash", "attach", "detach", "drop-partition":
		if req.PartitionName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "partitionName is required"})
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

	script := buildDBPGPartitionScript(req.Action, db, req.TableName, req.PartitionName, req.BoundSpec, req.Strategy)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "db_pg_partition_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.pg.partition", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_pg_partition_" + req.Action,
		Target: db, Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID + " table=" + req.TableName + " partition=" + req.PartitionName),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": req.Action})
}

func buildDBPGPartitionScript(action, db, tableName, partitionName, boundSpec, strategy string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	dbQ := shellQuote(db)
	bs := boundSpec
	if bs == "" {
		bs = "DEFAULT"
	}
	switch action {
	case "list-partitions":
		if tableName != "" {
			fmt.Fprintf(&b, "psql -d %s -c \"SELECT inhrelid::regclass AS partition_name, pg_get_expr(pg_class.relpartbound, inhrelid) AS bound FROM pg_inherits JOIN pg_class ON pg_class.oid = inhrelid WHERE inhparent = %s::regclass;\"\n", dbQ, shellQuote(tableName))
		} else {
			fmt.Fprintf(&b, "psql -d %s -c \"SELECT schemaname, tablename, partitionname FROM pg_partitions ORDER BY schemaname, tablename;\"\n", dbQ)
		}
	case "create-range":
		fmt.Fprintf(&b, "psql -d %s -c \"CREATE TABLE %s PARTITION OF %s FOR VALUES %s;\"\n", dbQ, partitionName, tableName, bs)
	case "create-list":
		fmt.Fprintf(&b, "psql -d %s -c \"CREATE TABLE %s PARTITION OF %s FOR VALUES IN %s;\"\n", dbQ, partitionName, tableName, bs)
	case "create-hash":
		fmt.Fprintf(&b, "psql -d %s -c \"CREATE TABLE %s PARTITION OF %s FOR VALUES WITH (MODULUS 4, REMAINDER 0);\"\n", dbQ, partitionName, tableName)
	case "attach":
		fmt.Fprintf(&b, "psql -d %s -c \"ALTER TABLE %s ATTACH PARTITION %s %s;\"\n", dbQ, tableName, partitionName, bs)
	case "detach":
		fmt.Fprintf(&b, "psql -d %s -c \"ALTER TABLE %s DETACH PARTITION %s;\"\n", dbQ, tableName, partitionName)
	case "drop-partition":
		fmt.Fprintf(&b, "psql -d %s -c \"DROP TABLE IF EXISTS %s;\"\n", dbQ, partitionName)
	case "partman-status":
		fmt.Fprintf(&b, "psql -d %s -c \"SELECT * FROM partman.part_config ORDER BY parent_table;\"\n", dbQ)
	case "partman-create":
		fmt.Fprintf(&b, "psql -d %s -c \"SELECT partman.create_parent('public.%s', 'created_at', 'native', '1 day');\"\n", dbQ, tableName)
	case "partman-run":
		fmt.Fprintf(&b, "psql -d %s -c \"SELECT partman.run_maintenance_proc();\"\n", dbQ)
	}
	fmt.Fprintf(&b, "echo 'postgresql partition %s done'\n", action)
	return b.String()
}
