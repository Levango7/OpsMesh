// db_mysql_ha_ops.go MySQL HA 运维（复制/GTID/半同步/组复制/ProxySQL/MaxScale/PITR/Orchestrator）。
package controlplane

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// mysqlHAActions MySQL HA action 白名单。
var mysqlHAActions = map[string]bool{
	"replication-status": true, "replication-lag": true, "replication-start": true, "replication-stop": true, "replication-reset": true,
	"gtid-status": true, "gtid-purged": true, "gtid-executed": true, "gtid-wait": true,
	"semi-sync-status": true, "semi-sync-enable": true, "semi-sync-disable": true,
	"group-replication-status": true, "group-replication-start": true, "group-replication-stop": true, "group-replication-add": true, "group-replication-remove": true,
	"proxysql-status": true, "proxysql-backend-list": true, "proxysql-backend-add": true, "proxysql-backend-remove": true,
	"maxscale-status": true, "maxscale-server-list": true,
	"pitr-prepare": true, "pitr-restore": true, "binlog-point-in-time": true,
	"orchestrator-status": true, "orchestrator-topology": true, "orchestrator-recover": true,
}

// handleMySQLHA 处理 POST /api/v1/db/mysql/ha。
func (s *Server) handleMySQLHA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string      `json:"deviceID"`
		Action   string      `json:"action"`
		DB       string      `json:"db"`
		Host     string      `json:"host"`
		Port     interface{} `json:"port"`
		User     string      `json:"user"`
		Password string      `json:"password"`
		Standby  string      `json:"standby"`
		Mode     string      `json:"mode"`
		Detail   string      `json:"detail"`
		ID       string      `json:"id"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !mysqlHAActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	db := req.DB
	if db == "" {
		db = "mysql"
	}
	if !mysqlNameRe.MatchString(db) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid db"})
		return
	}
	if req.User != "" && !mysqlNameRe.MatchString(req.User) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user"})
		return
	}
	if req.Standby != "" && !mysqlNameRe.MatchString(req.Standby) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid standby"})
		return
	}
	host := req.Host
	if host == "" {
		host = "localhost"
	}
	if !opsHostRe.MatchString(host) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid host"})
		return
	}
	port := ""
	switch v := req.Port.(type) {
	case nil:
		port = "3306"
	case string:
		if v == "" {
			port = "3306"
		} else {
			if _, err := strconv.Atoi(v); err != nil {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid port"})
				return
			}
			port = v
		}
	case float64:
		port = strconv.Itoa(int(v))
	case int:
		port = strconv.Itoa(v)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid port"})
		return
	}
	if port == "" {
		port = "3306"
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

	script := buildMySQLHAScript(req.Action, db, host, port, req.User, req.Password, req.Standby, req.Mode, req.Detail, req.ID)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "mysql_ha_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.mysql.ha", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_mysql_ha_" + req.Action,
		Target: db, Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID + " standby=" + req.Standby + " mode=" + req.Mode),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": req.Action})
}

func buildMySQLHAScript(action, db, host, port, user, password, standby, mode, detail, id string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	cli := mysqlCLI(host, port, user, password)
	sb := standby
	if sb == "" {
		sb = "replica"
	}
	switch action {
	case "replication-status":
		fmt.Fprintf(&b, "%s -e \"SHOW SLAVE STATUS\\G\"\n", cli)
	case "replication-lag":
		fmt.Fprintf(&b, "%s -e \"SHOW SLAVE STATUS\\G\" | grep Seconds_Behind_Master\n", cli)
	case "replication-start":
		fmt.Fprintf(&b, "%s -e \"START SLAVE;\"\n", cli)
	case "replication-stop":
		fmt.Fprintf(&b, "%s -e \"STOP SLAVE;\"\n", cli)
	case "replication-reset":
		fmt.Fprintf(&b, "%s -e \"RESET SLAVE ALL;\"\n", cli)
	case "gtid-status":
		fmt.Fprintf(&b, "%s -e \"SHOW GLOBAL VARIABLES LIKE 'gtid%%';\"\n", cli)
	case "gtid-purged":
		fmt.Fprintf(&b, "%s -e \"SHOW GLOBAL VARIABLES LIKE 'gtid_purged';\"\n", cli)
	case "gtid-executed":
		fmt.Fprintf(&b, "%s -e \"SHOW GLOBAL VARIABLES LIKE 'gtid_executed';\"\n", cli)
	case "gtid-wait":
		fmt.Fprintf(&b, "%s -e \"SELECT WAIT_FOR_EXECUTED_GTID_SET(%s, 60);\"\n", cli, shellQuote(detail))
	case "semi-sync-status":
		fmt.Fprintf(&b, "%s -e \"SHOW VARIABLES LIKE 'rpl_semi_sync%%';\"\n", cli)
	case "semi-sync-enable":
		fmt.Fprintf(&b, "%s -e \"SET GLOBAL rpl_semi_sync_master_enabled=1; SET GLOBAL rpl_semi_sync_slave_enabled=1;\"\n", cli)
	case "semi-sync-disable":
		fmt.Fprintf(&b, "%s -e \"SET GLOBAL rpl_semi_sync_master_enabled=0; SET GLOBAL rpl_semi_sync_slave_enabled=0;\"\n", cli)
	case "group-replication-status":
		fmt.Fprintf(&b, "%s -e \"SELECT * FROM performance_schema.replication_group_members;\"\n", cli)
	case "group-replication-start":
		fmt.Fprintf(&b, "%s -e \"START GROUP_REPLICATION;\"\n", cli)
	case "group-replication-stop":
		fmt.Fprintf(&b, "%s -e \"STOP GROUP_REPLICATION;\"\n", cli)
	case "group-replication-add":
		fmt.Fprintf(&b, "%s -e \"SELECT group_replication_set_as_primary('%s');\"\n", cli, shellQuote(sb))
	case "group-replication-remove":
		fmt.Fprintf(&b, "%s -e \"SELECT group_replication_reset_member_actions();\"\n", cli)
	case "proxysql-status":
		fmt.Fprintf(&b, "mysql -h 127.0.0.1 -P 6032 -u admin -padmin -e \"SELECT * FROM runtime_mysql_servers;\"\n")
	case "proxysql-backend-list":
		fmt.Fprintf(&b, "mysql -h 127.0.0.1 -P 6032 -u admin -padmin -e \"SELECT hostgroup_id,hostname,port,status FROM mysql_servers;\"\n")
	case "proxysql-backend-add":
		fmt.Fprintf(&b, "mysql -h 127.0.0.1 -P 6032 -u admin -padmin -e \"INSERT INTO mysql_servers(hostgroup_id,hostname,port) VALUES(1,%s,%s); LOAD MYSQL SERVERS TO RUNTIME; SAVE MYSQL SERVERS TO DISK;\"\n", shellQuote(sb), shellQuote(detail))
	case "proxysql-backend-remove":
		fmt.Fprintf(&b, "mysql -h 127.0.0.1 -P 6032 -u admin -padmin -e \"DELETE FROM mysql_servers WHERE hostname=%s; LOAD MYSQL SERVERS TO RUNTIME; SAVE MYSQL SERVERS TO DISK;\"\n", shellQuote(sb))
	case "maxscale-status":
		fmt.Fprintf(&b, "maxctrl list servers\n")
	case "maxscale-server-list":
		fmt.Fprintf(&b, "maxctrl list servers\n")
	case "pitr-prepare":
		fmt.Fprintf(&b, "%s -e \"FLUSH TABLES WITH READ LOCK;\"\n", cli)
	case "pitr-restore":
		fmt.Fprintf(&b, "%s -e \"UNLOCK TABLES;\"\n", cli)
	case "binlog-point-in-time":
		fmt.Fprintf(&b, "mysqlbinlog --start-datetime=%s %s | mysql -h %s -P %s -u %s\n", shellQuote(detail), shellQuote(mode), shellQuote(host), shellQuote(port), shellQuote(user))
	case "orchestrator-status":
		fmt.Fprintf(&b, "orchestrator -c clusters\n")
	case "orchestrator-topology":
		fmt.Fprintf(&b, "orchestrator -c topology -i %s\n", shellQuote(detail))
	case "orchestrator-recover":
		fmt.Fprintf(&b, "orchestrator -c recover -i %s\n", shellQuote(detail))
	}
	fmt.Fprintf(&b, "echo 'mysql ha %s done'\n", action)
	return b.String()
}
