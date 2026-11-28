// oracle_ext_ops.go Oracle 高级运维（ASM/表空间/PDB/CDB/闪回/分区/网络/权限/隔离锁/报告/TDE/归档日志/统计）。
package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// oracleExtActions Oracle 高级运维 action 白名单。
var oracleExtActions = map[string]bool{
	"asm-diskgroups": true, "asm-rebalance": true, "asm-add-disk": true, "asm-drop-disk": true, "asm-status": true,
	"tbs-list": true, "tbs-create": true, "tbs-drop": true, "tbs-resize": true, "tbs-offline": true, "tbs-online": true,
	"pdb-list": true, "pdb-create": true, "pdb-open": true, "pdb-close": true, "pdb-drop": true, "pdb-save-state": true,
	"cdb-info": true, "pdb-plugin": true, "pdb-unplug": true,
	"flashback-query": true, "flashback-table": true, "flashback-database": true, "flashback-enable": true, "flashback-disable": true,
	"partition-list": true, "partition-create-range": true, "partition-create-list": true, "partition-create-hash": true, "partition-create-interval": true, "partition-drop": true, "partition-split": true, "partition-merge": true,
	"net-listener-status": true, "net-services": true, "net-registration": true,
	"priv-list": true, "priv-grant": true, "priv-revoke": true, "role-list": true, "role-create": true, "role-drop": true,
	"iso-level": true, "lock-list": true, "long-running": true, "blocking-chain": true,
	"awr-report": true, "ash-report": true, "addm-report": true, "sqlplan": true,
	"tde-status": true, "tde-wallet-open": true, "tde-wallet-close": true, "tde-key-rotate": true,
	"archive-log-status": true, "archive-log-switch": true, "archive-log-list": true,
	"stats-gather": true, "stats-delete": true, "stats-lock": true, "stats-unlock": true,
}

// oracleIdentRe Oracle 标识符白名单：字母开头，仅含字母数字与 _ $ #，长度 <= 30。
var oracleIdentRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_$#]{0,29}$`)

// oracleDetailRe 通用 detail 字段安全白名单（拒绝 shell 元字符）。
var oracleDetailRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.,:/@#-]{0,127}$`)

// handleOracleExt 处理 POST /api/v1/db/oracle/ext。
func (s *Server) handleOracleExt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID   string `json:"deviceID"`
		Action     string `json:"action"`
		DB         string `json:"db"`
		PDB        string `json:"pdb"`
		Tablespace string `json:"tablespace"`
		Table      string `json:"table"`
		Partition  string `json:"partition"`
		Type       string `json:"type"`
		Detail     string `json:"detail"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !oracleExtActions[req.Action] {
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
	for _, kv := range []struct {
		name, val string
	}{
		{"pdb", req.PDB}, {"tablespace", req.Tablespace}, {"table", req.Table},
		{"partition", req.Partition}, {"type", req.Type},
	} {
		if kv.val != "" && !oracleIdentRe.MatchString(kv.val) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid " + kv.name})
			return
		}
	}
	if req.Detail != "" && !oracleDetailRe.MatchString(req.Detail) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid detail"})
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

	script := buildOracleExtScript(req.Action, db, req.PDB, req.Tablespace, req.Table, req.Partition, req.Type, req.Detail)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "oracle_ext_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "db.oracle.ext", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "db_oracle_ext_" + req.Action,
		Target: db, Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID + " pdb=" + req.PDB + " tablespace=" + req.Tablespace + " table=" + req.Table),
	})
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": task.TaskID, "action": req.Action})
}

func buildOracleExtScript(action, db, pdb, tablespace, table, partition, typ, detail string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	pdbV := pdb
	if pdbV == "" {
		pdbV = "ORCLPDB1"
	}
	tsV := tablespace
	if tsV == "" {
		tsV = "USERS"
	}
	tblV := table
	if tblV == "" {
		tblV = "T"
	}
	partV := partition
	if partV == "" {
		partV = "P1"
	}
	switch action {
	case "asm-diskgroups":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT group_number, name, state, type, total_mb, free_mb FROM v$asm_diskgroup;\nSQL\n")
	case "asm-rebalance":
		if !oracleIdentRe.MatchString(tsV) {
			tsV = "USERS"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER DISKGROUP " + tsV + " REBALANCE POWER 4;\nSQL\n")
	case "asm-add-disk":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER DISKGROUP " + tsV + " ADD DISK " + shellQuote(detail) + ";\nSQL\n")
	case "asm-drop-disk":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER DISKGROUP " + tsV + " DROP DISK " + shellQuote(detail) + ";\nSQL\n")
	case "asm-status":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT * FROM v$asm_operation;\nSQL\n")
	case "tbs-list":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT tablespace_name, status, contents FROM dba_tablespaces;\nSQL\n")
	case "tbs-create":
		if !oracleIdentRe.MatchString(tsV) {
			tsV = "USERS"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nCREATE TABLESPACE " + tsV + " DATAFILE '/u01/app/oracle/oradata/" + tsV + ".dbf' SIZE 100M AUTOEXTEND ON;\nSQL\n")
	case "tbs-drop":
		if !oracleIdentRe.MatchString(tsV) {
			tsV = "USERS"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nDROP TABLESPACE " + tsV + " INCLUDING CONTENTS AND DATAFILES;\nSQL\n")
	case "tbs-resize":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER DATABASE DATAFILE '/u01/app/oracle/oradata/" + tsV + ".dbf' RESIZE " + shellQuote(detail) + ";\nSQL\n")
	case "tbs-offline":
		if !oracleIdentRe.MatchString(tsV) {
			tsV = "USERS"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER TABLESPACE " + tsV + " OFFLINE;\nSQL\n")
	case "tbs-online":
		if !oracleIdentRe.MatchString(tsV) {
			tsV = "USERS"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER TABLESPACE " + tsV + " ONLINE;\nSQL\n")
	case "pdb-list":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSHOW PDBS;\nSQL\n")
	case "pdb-create":
		if !oracleIdentRe.MatchString(pdbV) {
			pdbV = "ORCLPDB1"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nCREATE PLUGGABLE DATABASE " + pdbV + " ADMIN USER pdbadmin IDENTIFIED BY \"ChangeMe\";\nSQL\n")
	case "pdb-open":
		if !oracleIdentRe.MatchString(pdbV) {
			pdbV = "ORCLPDB1"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER PLUGGABLE DATABASE " + pdbV + " OPEN;\nSQL\n")
	case "pdb-close":
		if !oracleIdentRe.MatchString(pdbV) {
			pdbV = "ORCLPDB1"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER PLUGGABLE DATABASE " + pdbV + " CLOSE;\nSQL\n")
	case "pdb-drop":
		if !oracleIdentRe.MatchString(pdbV) {
			pdbV = "ORCLPDB1"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nDROP PLUGGABLE DATABASE " + pdbV + " INCLUDING DATAFILES;\nSQL\n")
	case "pdb-save-state":
		if !oracleIdentRe.MatchString(pdbV) {
			pdbV = "ORCLPDB1"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER PLUGGABLE DATABASE " + pdbV + " SAVE STATE;\nSQL\n")
	case "cdb-info":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT name, cdb, open_mode FROM v$database;\nSQL\n")
	case "pdb-plugin":
		if !oracleIdentRe.MatchString(pdbV) {
			pdbV = "ORCLPDB1"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER PLUGGABLE DATABASE " + pdbV + " UNPLUG INTO '/tmp/" + pdbV + ".xml';\nSQL\n")
	case "pdb-unplug":
		if !oracleIdentRe.MatchString(pdbV) {
			pdbV = "ORCLPDB1"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER PLUGGABLE DATABASE " + pdbV + " UNPLUG INTO '/tmp/" + pdbV + ".xml';\nSQL\n")
	case "flashback-query":
		if !oracleIdentRe.MatchString(tblV) {
			tblV = "T"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT * FROM " + tblV + " AS OF TIMESTAMP (SYSTIMESTAMP - INTERVAL '10' MINUTE);\nSQL\n")
	case "flashback-table":
		if !oracleIdentRe.MatchString(tblV) {
			tblV = "T"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nFLASHBACK TABLE " + tblV + " TO TIMESTAMP (SYSTIMESTAMP - INTERVAL '10' MINUTE);\nSQL\n")
	case "flashback-database":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nFLASHBACK DATABASE TO TIMESTAMP (SYSTIMESTAMP - INTERVAL '10' MINUTE);\nSQL\n")
	case "flashback-enable":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER DATABASE FLASHBACK ON;\nSQL\n")
	case "flashback-disable":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER DATABASE FLASHBACK OFF;\nSQL\n")
	case "partition-list":
		if !oracleIdentRe.MatchString(tblV) {
			tblV = "T"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT table_name, partition_name, high_value FROM dba_tab_partitions WHERE table_name='" + tblV + "';\nSQL\n")
	case "partition-create-range":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER TABLE " + tblV + " ADD PARTITION " + partV + " VALUES LESS THAN (" + shellQuote(detail) + ");\nSQL\n")
	case "partition-create-list":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER TABLE " + tblV + " ADD PARTITION " + partV + " VALUES (" + shellQuote(detail) + ");\nSQL\n")
	case "partition-create-hash":
		if !oracleIdentRe.MatchString(tblV) {
			tblV = "T"
		}
		if !oracleIdentRe.MatchString(partV) {
			partV = "P1"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER TABLE " + tblV + " ADD PARTITION " + partV + ";\nSQL\n")
	case "partition-create-interval":
		if !oracleIdentRe.MatchString(tblV) {
			tblV = "T"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER TABLE " + tblV + " SET INTERVAL (NUMTOYMINTERVAL(1,'MONTH'));\nSQL\n")
	case "partition-drop":
		if !oracleIdentRe.MatchString(tblV) {
			tblV = "T"
		}
		if !oracleIdentRe.MatchString(partV) {
			partV = "P1"
		}
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER TABLE " + tblV + " DROP PARTITION " + partV + ";\nSQL\n")
	case "partition-split":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER TABLE " + tblV + " SPLIT PARTITION " + partV + " AT (" + shellQuote(detail) + ") INTO (PARTITION " + partV + "_A, PARTITION " + partV + "_B);\nSQL\n")
	case "partition-merge":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER TABLE " + tblV + " MERGE PARTITIONS " + partV + ", " + shellQuote(detail) + " INTO PARTITION " + partV + "_M;\nSQL\n")
	case "net-listener-status":
		b.WriteString("lsnrctl status\n")
	case "net-services":
		b.WriteString("cat $ORACLE_HOME/network/admin/tnsnames.ora\n")
	case "net-registration":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT * FROM v$listener_network;\nSQL\n")
	case "priv-list":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT grantee, privilege FROM dba_sys_privs;\nSQL\n")
	case "priv-grant":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nGRANT " + shellQuote(detail) + " TO " + shellQuote(tblV) + ";\nSQL\n")
	case "priv-revoke":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nREVOKE " + shellQuote(detail) + " FROM " + shellQuote(tblV) + ";\nSQL\n")
	case "role-list":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT role FROM dba_roles;\nSQL\n")
	case "role-create":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nCREATE ROLE " + shellQuote(tblV) + ";\nSQL\n")
	case "role-drop":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nDROP ROLE " + shellQuote(tblV) + ";\nSQL\n")
	case "iso-level":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT * FROM v$transaction;\nSQL\n")
	case "lock-list":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT * FROM v$lock WHERE block = 1;\nSQL\n")
	case "long-running":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT sid, serial#, opname, sofar, totalwork FROM v$session_longops WHERE sofar <> totalwork;\nSQL\n")
	case "blocking-chain":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT * FROM v$wait_chains;\nSQL\n")
	case "awr-report":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\n@?/rdbms/admin/awrrpt.sql\nSQL\n")
	case "ash-report":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\n@?/rdbms/admin/ashrpt.sql\nSQL\n")
	case "addm-report":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\n@?/rdbms/admin/addmrpt.sql\nSQL\n")
	case "sqlplan":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT * FROM table(dbms_xplan.display_cursor);\nSQL\n")
	case "tde-status":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT * FROM v$encryption_wallet;\nSQL\n")
	case "tde-wallet-open":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nADMINISTER KEY MANAGEMENT SET KEYSTORE OPEN IDENTIFIED BY \"wallet_pass\";\nSQL\n")
	case "tde-wallet-close":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nADMINISTER KEY MANAGEMENT SET KEYSTORE CLOSE;\nSQL\n")
	case "tde-key-rotate":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nADMINISTER KEY MANAGEMENT SET KEY IDENTIFIED BY \"wallet_pass\" WITH BACKUP;\nSQL\n")
	case "archive-log-status":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nARCHIVE LOG LIST;\nSQL\n")
	case "archive-log-switch":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nALTER SYSTEM SWITCH LOGFILE;\nSQL\n")
	case "archive-log-list":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nSELECT sequence#, name, archived, status FROM v$archived_log ORDER BY sequence#;\nSQL\n")
	case "stats-gather":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nEXEC DBMS_STATS.GATHER_DATABASE_STATS;\nSQL\n")
	case "stats-delete":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nEXEC DBMS_STATS.DELETE_DATABASE_STATS;\nSQL\n")
	case "stats-lock":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nEXEC DBMS_STATS.LOCK_SCHEMA_STATS('" + opsSQLString(db) + "');\nSQL\n")
	case "stats-unlock":
		b.WriteString("sqlplus -s / as sysdba <<'SQL'\nEXEC DBMS_STATS.UNLOCK_SCHEMA_STATS('" + opsSQLString(db) + "');\nSQL\n")
	}
	fmt.Fprintf(&b, "echo 'oracle ext %s done'\n", action)
	return b.String()
}
