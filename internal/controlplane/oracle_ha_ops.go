// oracle_ha_ops.go Oracle HA / RAC / Data Guard 高级运维。
package controlplane

import (
	"fmt"
	"net/http"

	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// oracleHAActions Oracle HA action 白名单。
var oracleHAActions = map[string]bool{
	"dg-status": true, "dg-lag": true, "dg-switchover": true, "dg-failover": true, "dg-reinstate": true, "dg-convert-physical": true, "dg-convert-logical": true,
	"dg-broker-status": true, "dg-broker-enable": true, "dg-broker-disable": true, "dg-broker-failover": true, "dg-broker-switchover": true,
	"adg-open-readonly": true, "adg-status": true,
	"rac-status": true, "rac-nodes": true, "rac-add-node": true, "rac-remove-node": true, "rac-voting": true, "rac-asm": true, "rac-scan": true, "rac-health": true, "rac-crs-check": true,
	"rac-gi-status": true, "rac-gi-start": true, "rac-gi-stop": true,
	"flashback-database-restore": true, "point-in-time-recovery": true,
}

// handleOracleHA 处理 POST /api/v1/db/oracle/ha。
func (s *Server) handleOracleHA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		DB       string `json:"db"`
		Standby  string `json:"standby"`
		Mode     string `json:"mode"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !oracleHAActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	db := req.DB
	if db == "" {
		db = "ORCL"
	}
	if !oracleIdentRe.MatchString(db) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid db"})
		return
	}
	if req.Standby != "" && !oracleIdentRe.MatchString(req.Standby) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid standby"})
		return
	}
	if req.Mode != "" && !oracleIdentRe.MatchString(req.Mode) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid mode"})
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

	script := buildOracleHAScript(req.Action, db, req.Standby, req.Mode)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "oracle_ha_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.oracle.ha", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_oracle_ha_" + req.Action,
		Target: db, Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID + " standby=" + req.Standby + " mode=" + req.Mode),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": req.Action})
}

func buildOracleHAScript(action, db, standby, mode string) string {
	var b strings.Builder
	b.WriteString("set -e\n")

	sbV := standby
	if sbV == "" {
		sbV = "STDBY"
	}
	modeV := mode
	if modeV == "" {
		modeV = "normal"
	}
	switch action {
	case "dg-status":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT * FROM v$dataguard_status;\nSQL\n")
	case "dg-lag":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT name, value, time_computed FROM v$dataguard_stats;\nSQL\n")
	case "dg-switchover":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER DATABASE COMMIT TO SWITCHOVER TO PRIMARY;\nSQL\n")
	case "dg-failover":
		if !oracleIdentRe.MatchString(sbV) {
			sbV = "STDBY"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER DATABASE FAILOVER TO " + sbV + ";\nSQL\n")
	case "dg-reinstate":
		if !oracleIdentRe.MatchString(sbV) {
			sbV = "STDBY"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER DATABASE REINSTATE " + sbV + ";\nSQL\n")
	case "dg-convert-physical":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER DATABASE CONVERT TO PHYSICAL STANDBY;\nSQL\n")
	case "dg-convert-logical":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER DATABASE CONVERT TO LOGICAL STANDBY;\nSQL\n")
	case "dg-broker-status":
		b.WriteString("dgmgrl -echo \"SHOW CONFIGURATION;\"\n")
	case "dg-broker-enable":
		b.WriteString("dgmgrl -echo \"ENABLE CONFIGURATION;\"\n")
	case "dg-broker-disable":
		b.WriteString("dgmgrl -echo \"DISABLE CONFIGURATION;\"\n")
	case "dg-broker-failover":
		if !oracleIdentRe.MatchString(sbV) {
			sbV = "STDBY"
		}
		b.WriteString("dgmgrl -echo \"FAILOVER TO " + sbV + ";\"\n")
	case "dg-broker-switchover":
		if !oracleIdentRe.MatchString(sbV) {
			sbV = "STDBY"
		}
		b.WriteString("dgmgrl -echo \"SWITCHOVER TO " + sbV + ";\"\n")
	case "adg-open-readonly":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER DATABASE OPEN READ ONLY;\nSQL\n")
	case "adg-status":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT open_mode FROM v$database;\nSQL\n")
	case "rac-status":
		b.WriteString("crsctl check cluster -all\n")
	case "rac-nodes":
		b.WriteString("olsnodes -n -i -s\n")
	case "rac-add-node":
		if !oracleIdentRe.MatchString(sbV) {
			sbV = "STDBY"
		}
		b.WriteString("addNode.sh -silent \"CLUSTER_NEW_NODES={" + sbV + "}\"\n")
	case "rac-remove-node":
		if !oracleIdentRe.MatchString(sbV) {
			sbV = "STDBY"
		}
		b.WriteString("crsctl delete node -n " + sbV + "\n")
	case "rac-voting":
		b.WriteString("crsctl query css votedisk\n")
	case "rac-asm":
		b.WriteString("asmcmd lsdis\n")
	case "rac-scan":
		b.WriteString("srvctl config scan\n")
	case "rac-health":
		b.WriteString("cluvfy comp healthcheck -collect cluster -bestpractice -deviations\n")
	case "rac-crs-check":
		b.WriteString("crsctl check crs\n")
	case "rac-gi-status":
		b.WriteString("crsctl check cluster\n")
	case "rac-gi-start":
		b.WriteString("crsctl start cluster -all\n")
	case "rac-gi-stop":
		b.WriteString("crsctl stop cluster -all\n")
	case "flashback-database-restore":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nFLASHBACK DATABASE TO " + shellQuote(modeV) + ";\nSQL\n")
	case "point-in-time-recovery":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nRECOVER DATABASE UNTIL TIME " + shellQuote(modeV) + ";\nSQL\n")
	}
	fmt.Fprintf(&b, "echo 'oracle ha %s done'\n", action)
	return b.String()
}
