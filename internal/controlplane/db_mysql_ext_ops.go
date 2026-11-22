// db_mysql_ext_ops.go MySQL 高级运维（用户/权限/角色/Schema/表/索引/慢查询/InnoDB/GTID/binlog/进程/变量/字符集/pt-online-schema-change/表空间）。
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

// mysqlExtActions MySQL 高级运维 action 白名单。
var mysqlExtActions = map[string]bool{
	"user-list": true, "user-create": true, "user-drop": true, "user-grant": true, "user-revoke": true, "user-show-grants": true,
	"priv-list": true, "priv-grant": true, "priv-revoke": true,
	"role-create": true, "role-drop": true, "role-grant": true,
	"schema-list": true, "table-list": true, "table-create": true, "table-drop": true, "table-alter": true, "table-truncate": true, "table-optimize": true, "table-analyze": true,
	"index-list": true, "index-create": true, "index-drop": true, "index-rebuild": true,
	"slow-query-log": true, "slow-query-list": true, "slow-query-clear": true,
	"innodb-status": true, "innodb-buffer-pool": true, "innodb-locks": true, "innodb-transactions": true,
	"gtid-status": true, "binlog-list": true, "binlog-purge": true, "binlog-flush": true,
	"process-list": true, "kill-query": true, "kill-connection": true,
	"variable-get": true, "variable-set": true, "variable-list": true,
	"charset-list": true, "collation-list": true,
	"pt-osc": true, "pt-archiver": true,
	"tablespace-list": true, "tablespace-create": true, "tablespace-drop": true,
}

// mysqlNameRe MySQL 标识符白名单：字母数字与下划线。
var mysqlNameRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// handleMySQLExt 处理 POST /api/v1/db/mysql/ext。
func (s *Server) handleMySQLExt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID  string      `json:"deviceID"`
		Action    string      `json:"action"`
		DB        string      `json:"db"`
		Table     string      `json:"table"`
		User      string      `json:"user"`
		Host      string      `json:"host"`
		Port      interface{} `json:"port"`
		Password  string      `json:"password"`
		NewUser   string      `json:"newUser"`
		Role      string      `json:"role"`
		Priv      string      `json:"priv"`
		Index     string      `json:"index"`
		Column    string      `json:"column"`
		Charset   string      `json:"charset"`
		Collation string      `json:"collation"`
		Variable  string      `json:"variable"`
		Value     string      `json:"value"`
		ID        string      `json:"id"`
		Query     string      `json:"query"`
		Detail    string      `json:"detail"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !mysqlExtActions[req.Action] {
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
	for _, kv := range []struct {
		name, val string
	}{
		{"table", req.Table}, {"user", req.User}, {"newUser", req.NewUser}, {"role", req.Role},
	} {
		if kv.val != "" && !mysqlNameRe.MatchString(kv.val) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid " + kv.name})
			return
		}
	}
	// host/port/password validation
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

	script := buildMySQLExtScript(req.Action, db, req.Table, req.User, host, port, req.Password, req.NewUser, req.Role, req.Priv, req.Index, req.Column, req.Charset, req.Collation, req.Variable, req.Value, req.ID, req.Query, req.Detail)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "mysql_ext_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.mysql.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_mysql_ext_" + req.Action,
		Target: db, Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID + " table=" + req.Table + " user=" + req.User),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": req.Action})
}

func mysqlCLI(host, port, user, password string) string {
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "3306"
	}
	if user == "" {
		user = "root"
	}
	base := "mysql -h " + shellQuote(host) + " -P " + shellQuote(port) + " -u " + shellQuote(user)
	if password != "" {
		base += " -p" + shellQuote(password)
	}
	return base
}

func buildMySQLExtScript(action, db, table, user, host, port, password, newUser, role, priv, index, column, charset, collation, variable, value, id, query, detail string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	cli := mysqlCLI(host, port, user, password)
	dbQ := shellQuote(db)
	tbl := table
	if tbl == "" {
		tbl = "t"
	}
	usr := user
	if usr == "" {
		usr = "root"
	}
	switch action {
	case "user-list":
		fmt.Fprintf(&b, "%s -e \"SELECT user,host FROM mysql.user;\"\n", cli)
	case "user-create":
		nu := newUser
		if nu == "" {
			nu = "appuser"
		}
		fmt.Fprintf(&b, "%s -e \"CREATE USER %s@'%%' IDENTIFIED BY %s;\"\n", cli, shellQuote(nu), shellQuote(detail))
	case "user-drop":
		fmt.Fprintf(&b, "%s -e \"DROP USER IF EXISTS %s@'%%';\"\n", cli, shellQuote(usr))
	case "user-grant":
		fmt.Fprintf(&b, "%s -e \"GRANT %s ON %s.* TO %s@'%%';\"\n", cli, shellQuote(priv), dbQ, shellQuote(usr))
	case "user-revoke":
		fmt.Fprintf(&b, "%s -e \"REVOKE %s ON %s.* FROM %s@'%%';\"\n", cli, shellQuote(priv), dbQ, shellQuote(usr))
	case "user-show-grants":
		fmt.Fprintf(&b, "%s -e \"SHOW GRANTS FOR %s@'%%';\"\n", cli, shellQuote(usr))
	case "priv-list":
		fmt.Fprintf(&b, "%s -e \"SHOW PRIVILEGES;\"\n", cli)
	case "priv-grant":
		fmt.Fprintf(&b, "%s -e \"GRANT %s ON %s.%s TO %s@'%%';\"\n", cli, shellQuote(priv), dbQ, shellQuote(tbl), shellQuote(usr))
	case "priv-revoke":
		fmt.Fprintf(&b, "%s -e \"REVOKE %s ON %s.%s FROM %s@'%%';\"\n", cli, shellQuote(priv), dbQ, shellQuote(tbl), shellQuote(usr))
	case "role-create":
		rn := role
		if rn == "" {
			rn = "app_role"
		}
		fmt.Fprintf(&b, "%s -e \"CREATE ROLE %s;\"\n", cli, shellQuote(rn))
	case "role-drop":
		rn := role
		if rn == "" {
			rn = "app_role"
		}
		fmt.Fprintf(&b, "%s -e \"DROP ROLE IF EXISTS %s;\"\n", cli, shellQuote(rn))
	case "role-grant":
		rn := role
		if rn == "" {
			rn = "app_role"
		}
		fmt.Fprintf(&b, "%s -e \"GRANT %s TO %s@'%%';\"\n", cli, shellQuote(rn), shellQuote(usr))
	case "schema-list":
		fmt.Fprintf(&b, "%s -e \"SHOW DATABASES;\"\n", cli)
	case "table-list":
		fmt.Fprintf(&b, "%s -e \"SHOW TABLES FROM %s;\"\n", cli, dbQ)
	case "table-create":
		fmt.Fprintf(&b, "%s -e \"CREATE TABLE IF NOT EXISTS %s.%s (id BIGINT PRIMARY KEY AUTO_INCREMENT) ENGINE=InnoDB;\"\n", cli, dbQ, shellQuote(tbl))
	case "table-drop":
		fmt.Fprintf(&b, "%s -e \"DROP TABLE IF EXISTS %s.%s;\"\n", cli, dbQ, shellQuote(tbl))
	case "table-alter":
		fmt.Fprintf(&b, "%s -e \"ALTER TABLE %s.%s %s;\"\n", cli, dbQ, shellQuote(tbl), shellQuote(detail))
	case "table-truncate":
		fmt.Fprintf(&b, "%s -e \"TRUNCATE TABLE %s.%s;\"\n", cli, dbQ, shellQuote(tbl))
	case "table-optimize":
		fmt.Fprintf(&b, "%s -e \"OPTIMIZE TABLE %s.%s;\"\n", cli, dbQ, shellQuote(tbl))
	case "table-analyze":
		fmt.Fprintf(&b, "%s -e \"ANALYZE TABLE %s.%s;\"\n", cli, dbQ, shellQuote(tbl))
	case "index-list":
		fmt.Fprintf(&b, "%s -e \"SHOW INDEX FROM %s.%s;\"\n", cli, dbQ, shellQuote(tbl))
	case "index-create":
		in := index
		if in == "" {
			in = "idx_" + tbl
		}
		fmt.Fprintf(&b, "%s -e \"CREATE INDEX %s ON %s.%s(%s);\"\n", cli, shellQuote(in), dbQ, shellQuote(tbl), shellQuote(column))
	case "index-drop":
		in := index
		if in == "" {
			in = "idx_" + tbl
		}
		fmt.Fprintf(&b, "%s -e \"DROP INDEX %s ON %s.%s;\"\n", cli, shellQuote(in), dbQ, shellQuote(tbl))
	case "index-rebuild":
		fmt.Fprintf(&b, "%s -e \"ALTER TABLE %s.%s ENGINE=InnoDB;\"\n", cli, dbQ, shellQuote(tbl))
	case "slow-query-log":
		fmt.Fprintf(&b, "%s -e \"SHOW VARIABLES LIKE 'slow_query_log%%';\"\n", cli)
	case "slow-query-list":
		fmt.Fprintf(&b, "%s -e \"SELECT * FROM mysql.slow_log ORDER BY start_time DESC LIMIT 50;\"\n", cli)
	case "slow-query-clear":
		fmt.Fprintf(&b, "%s -e \"TRUNCATE TABLE mysql.slow_log;\"\n", cli)
	case "innodb-status":
		fmt.Fprintf(&b, "%s -e \"SHOW ENGINE INNODB STATUS\\G\"\n", cli)
	case "innodb-buffer-pool":
		fmt.Fprintf(&b, "%s -e \"SHOW STATUS LIKE 'Innodb_buffer_pool%%';\"\n", cli)
	case "innodb-locks":
		fmt.Fprintf(&b, "%s -e \"SELECT * FROM information_schema.INNODB_LOCKS;\"\n", cli)
	case "innodb-transactions":
		fmt.Fprintf(&b, "%s -e \"SELECT * FROM information_schema.INNODB_TRX;\"\n", cli)
	case "gtid-status":
		fmt.Fprintf(&b, "%s -e \"SHOW GLOBAL VARIABLES LIKE 'gtid%%';\"\n", cli)
	case "binlog-list":
		fmt.Fprintf(&b, "%s -e \"SHOW BINARY LOGS;\"\n", cli)
	case "binlog-purge":
		fmt.Fprintf(&b, "%s -e \"PURGE BINARY LOGS BEFORE NOW() - INTERVAL 7 DAY;\"\n", cli)
	case "binlog-flush":
		fmt.Fprintf(&b, "%s -e \"FLUSH BINARY LOGS;\"\n", cli)
	case "process-list":
		fmt.Fprintf(&b, "%s -e \"SHOW FULL PROCESSLIST;\"\n", cli)
	case "kill-query":
		fmt.Fprintf(&b, "%s -e \"KILL QUERY %s;\"\n", cli, shellQuote(id))
	case "kill-connection":
		fmt.Fprintf(&b, "%s -e \"KILL %s;\"\n", cli, shellQuote(id))
	case "variable-get":
		vn := variable
		if vn == "" {
			vn = "max_connections"
		}
		fmt.Fprintf(&b, "%s -e \"SHOW VARIABLES LIKE '%s';\"\n", cli, shellQuote(vn))
	case "variable-set":
		vn := variable
		if vn == "" {
			vn = "max_connections"
		}
		fmt.Fprintf(&b, "%s -e \"SET GLOBAL %s=%s;\"\n", cli, shellQuote(vn), shellQuote(value))
	case "variable-list":
		fmt.Fprintf(&b, "%s -e \"SHOW VARIABLES;\"\n", cli)
	case "charset-list":
		fmt.Fprintf(&b, "%s -e \"SHOW CHARACTER SET;\"\n", cli)
	case "collation-list":
		fmt.Fprintf(&b, "%s -e \"SHOW COLLATION;\"\n", cli)
	case "pt-osc":
		fmt.Fprintf(&b, "pt-online-schema-change --alter %s D=%s,t=%s --execute\n", shellQuote(detail), dbQ, shellQuote(tbl))
	case "pt-archiver":
		fmt.Fprintf(&b, "pt-archiver --source h=%s,D=%s,t=%s --purge --limit 1000\n", shellQuote(host), dbQ, shellQuote(tbl))
	case "tablespace-list":
		fmt.Fprintf(&b, "%s -e \"SELECT * FROM information_schema.INNODB_TABLESPACES;\"\n", cli)
	case "tablespace-create":
		ts := detail
		if ts == "" {
			ts = "ts_" + tbl
		}
		fmt.Fprintf(&b, "%s -e \"CREATE TABLESPACE %s ADD DATAFILE '%s.ibd';\"\n", cli, shellQuote(ts), shellQuote(ts))
	case "tablespace-drop":
		ts := detail
		if ts == "" {
			ts = "ts_" + tbl
		}
		fmt.Fprintf(&b, "%s -e \"DROP TABLESPACE %s;\"\n", cli, shellQuote(ts))
	}
	fmt.Fprintf(&b, "echo 'mysql ext %s done'\n", action)
	return b.String()
}
