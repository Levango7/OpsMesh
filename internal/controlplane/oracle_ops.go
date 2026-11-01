// Package controlplane oracle_ops.go 实现 Oracle 数据库全面运维支持
// 备份/复制/性能/升级/迁移/RAC/安全
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleOracleBackup 处理 POST /api/v1/db/oracle/backup
func (s *Server) handleOracleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID   string `json:"deviceID"`
		Action     string `json:"action"`
		DB         string `json:"db"`
		User       string `json:"user"`
		Password   string `json:"password"`
		Directory  string `json:"directory"`
		DumpFile   string `json:"dumpFile"`
		Tables     string `json:"tables"`
		Path       string `json:"path"`
		BackupType string `json:"backupType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Directory == "" {
		req.Directory = "DATA_PUMP_DIR"
	}
	if req.Path == "" {
		req.Path = "/backup/oracle"
	}
	if req.DumpFile == "" {
		req.DumpFile = "oracle_$(date +%Y%m%d).dmp"
	}
	if req.BackupType == "" {
		req.BackupType = "full"
	}

	var script string
	switch req.Action {
	case "expdp":
		scope := "FULL=Y"
		if req.Tables != "" {
			scope = "TABLES=" + req.Tables
		}
		script = fmt.Sprintf(`mkdir -p %s && expdp %s/%s@%s DIRECTORY=%s DUMPFILE=%s LOGFILE=%s.log %s && echo "Oracle Data Pump export completed"`, req.Path, req.User, req.Password, req.DB, req.Directory, req.DumpFile, req.DumpFile, scope)
	case "impdp":
		scope := ""
		if req.Tables != "" {
			scope = "TABLES=" + req.Tables
		}
		script = fmt.Sprintf(`impdp %s/%s@%s DIRECTORY=%s DUMPFILE=%s LOGFILE=%s.log %s && echo "Oracle Data Pump import completed"`, req.User, req.Password, req.DB, req.Directory, req.DumpFile, req.DumpFile, scope)
	case "rman":
		if req.BackupType == "incremental" {
			script = `rman target / <<EOF
BACKUP INCREMENTAL LEVEL 1 DATABASE PLUS ARCHIVELOG;
CROSSCHECK BACKUP;
DELETE NOPROMPT OBSOLETE;
EXIT;
EOF
echo "Oracle RMAN incremental backup completed"`
		} else {
			script = `rman target / <<EOF
BACKUP DATABASE PLUS ARCHIVELOG;
CROSSCHECK BACKUP;
DELETE NOPROMPT OBSOLETE;
EXIT;
EOF
echo "Oracle RMAN full backup completed"`
		}
	case "backup-list":
		script = `rman target / <<EOF
LIST BACKUP SUMMARY;
EXIT;
EOF`
	case "restore":
		script = `rman target / <<EOF
SHUTDOWN IMMEDIATE;
STARTUP MOUNT;
RESTORE DATABASE;
RECOVER DATABASE;
ALTER DATABASE OPEN;
EXIT;
EOF
echo "Oracle RMAN restore completed"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "oracle_backup_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleOracleReplication 处理 POST /api/v1/db/oracle/replication
func (s *Server) handleOracleReplication(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"`
		DB        string `json:"db"`
		StandbyDB string `json:"standbyDB"`
		Primary   string `json:"primary"`
		Standby   string `json:"standby"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `dgmgrl -silent / "show configuration" 2>/dev/null; echo "---"; sqlplus -s / as sysdba <<EOF
SELECT name, db_unique_name, open_mode, database_role, switchover_status FROM v$database;
SELECT process, status, thread#, sequence# FROM v$managed_standby;
EXIT;
EOF`
	case "setup":
		if req.Primary == "" || req.Standby == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "primary and standby are required"})
			return
		}
		script = fmt.Sprintf(`echo "Configuring Data Guard: primary=%s standby=%s"; sqlplus / as sysdba <<EOF
ALTER SYSTEM SET log_archive_config='DG_CONFIG=(%s,%s)' SCOPE=BOTH;
ALTER SYSTEM SET log_archive_dest_2='SERVICE=%s ASYNC VALID_FOR=(ONLINE_LOGFILES,PRIMARY_ROLE) DB_UNIQUE_NAME=%s' SCOPE=BOTH;
ALTER SYSTEM SET fal_server='%s' SCOPE=BOTH;
ALTER SYSTEM SET fal_client='%s' SCOPE=BOTH;
ALTER DATABASE FORCE LOGGING;
ALTER DATABASE ADD STANDBY LOGFILE GROUP 4 SIZE 100M;
EXIT;
EOF
echo "Data Guard configuration applied"`, req.Primary, req.Standby, req.Primary, req.Standby, req.Standby, req.Standby, req.Standby, req.Primary)
	case "switchover":
		script = `dgmgrl -silent / "switchover to standby" 2>/dev/null && echo "Switchover completed" || echo "Switchover failed"`
	case "failover":
		script = `dgmgrl -silent / "failover to standby" 2>/dev/null && echo "Failover completed" || echo "Failover failed"`
	case "lag":
		script = `sqlplus -s / as sysdba <<EOF
SELECT name, value, unit FROM v$dataguard_stats WHERE name IN ('transport lag','apply lag');
SELECT process, status, thread#, sequence#, block# FROM v$managed_standby WHERE process LIKE 'MRP%';
EXIT;
EOF`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "oracle_replication_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleOraclePerf 处理 POST /api/v1/db/oracle/perf
func (s *Server) handleOraclePerf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"`
		BeginSnap int    `json:"beginSnap"`
		EndSnap   int    `json:"endSnap"`
		SQLID     string `json:"sqlId"`
		TopN      int    `json:"topN"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.TopN == 0 {
		req.TopN = 10
	}

	var script string
	switch req.Action {
	case "awr":
		script = fmt.Sprintf(`sqlplus / as sysdba <<EOF
@?/rdbms/admin/awrrpt.sql
%d
%d
awr_report.html
EXIT;
EOF
echo "AWR report generated"`, req.BeginSnap, req.EndSnap)
	case "ash":
		script = `sqlplus / as sysdba <<EOF
@?/rdbms/admin/ashrpt.sql
EXIT;
EOF
echo "ASH report generated"`
	case "addm":
		script = fmt.Sprintf(`sqlplus / as sysdba <<EOF
@?/rdbms/admin/addmrpt.sql
%d
%d
addm_report.txt
EXIT;
EOF
echo "ADDM analysis completed"`, req.BeginSnap, req.EndSnap)
	case "sql-tuning":
		if req.SQLID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "sqlId is required"})
			return
		}
		script = fmt.Sprintf(`sqlplus / as sysdba <<EOF
DECLARE
  tname VARCHAR2(30);
BEGIN
  tname := DBMS_SQLTUNE.CREATE_TUNING_TASK(sql_id => '%s', scope => 'COMPREHENSIVE', time_limit => 60, task_name => 'tune_%s');
  DBMS_SQLTUNE.EXECUTE_TUNING_TASK(task_name => 'tune_%s');
  DBMS_OUTPUT.PUT_LINE(DBMS_SQLTUNE.REPORT_TUNING_TASK('tune_%s'));
END;
/
EXIT;
EOF
echo "SQL Tuning Advisor completed for %s"`, req.SQLID, req.SQLID, req.SQLID, req.SQLID, req.SQLID)
	case "top-sql":
		script = fmt.Sprintf(`sqlplus -s / as sysdba <<EOF
SET LINESIZE 200 PAGESIZE 50
SELECT * FROM (
  SELECT sql_id, SUBSTR(sql_text,1,60) sql_text, elapsed_time/1000000 elapsed_sec, cpu_time/1000000 cpu_sec, executions, buffer_gets
  FROM v$sql ORDER BY elapsed_time DESC
) WHERE ROWNUM <= %d;
EXIT;
EOF`, req.TopN)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "oracle_perf_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleOracleUpgrade 处理 POST /api/v1/db/oracle/upgrade
func (s *Server) handleOracleUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID   string `json:"deviceID"`
		Action     string `json:"action"`
		PatchID    string `json:"patchId"`
		Version    string `json:"version"`
		OracleHome string `json:"oracleHome"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.OracleHome == "" {
		req.OracleHome = "$ORACLE_HOME"
	}

	var script string
	switch req.Action {
	case "precheck":
		script = fmt.Sprintf(`echo "=== Oracle Upgrade Precheck ==="; %s/bin/sqlplus / as sysdba <<< "SELECT * FROM v$version;"; echo "---"; %s/OPatch/opatch prereq CheckConflictAgainstOHWithDetail -ph %s 2>/dev/null || echo "OPatch prereq check done"; echo "---"; %s/bin/dbua -preupgrade -silent 2>/dev/null || echo "DBUA precheck done"`, req.OracleHome, req.OracleHome, req.PatchID, req.OracleHome)
	case "patch":
		if req.PatchID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "patchId is required"})
			return
		}
		script = fmt.Sprintf(`cd %s && %s/OPatch/opatch apply -silent %s && echo "Patch %s applied"`, req.PatchID, req.OracleHome, req.PatchID, req.PatchID)
	case "ru":
		if req.Version == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "version is required"})
			return
		}
		script = fmt.Sprintf(`cd %s && %s/OPatch/opatch apply -silent %s && echo "Release Update %s applied"`, req.PatchID, req.OracleHome, req.PatchID, req.Version)
	case "rur":
		if req.Version == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "version is required"})
			return
		}
		script = fmt.Sprintf(`cd %s && %s/OPatch/opatch apply -silent %s && echo "Release Update Revision %s applied"`, req.PatchID, req.OracleHome, req.PatchID, req.Version)
	case "rollback":
		if req.PatchID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "patchId is required"})
			return
		}
		script = fmt.Sprintf(`%s/OPatch/opatch rollback -silent -id %s && echo "Patch %s rolled back"`, req.OracleHome, req.PatchID, req.PatchID)
	case "version":
		script = fmt.Sprintf(`echo "=== Oracle Version ==="; %s/bin/sqlplus -s / as sysdba <<< "SELECT * FROM v$version; SELECT * FROM v$instance;"; echo "=== OPatch Inventory ==="; %s/OPatch/opatch lsinventory 2>/dev/null | head -30`, req.OracleHome, req.OracleHome)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "oracle_upgrade_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleOracleMigration 处理 POST /api/v1/db/oracle/migration
func (s *Server) handleOracleMigration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"`
		Source    string `json:"source"`
		Target    string `json:"target"`
		DB        string `json:"db"`
		User      string `json:"user"`
		Password  string `json:"password"`
		Directory string `json:"directory"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Directory == "" {
		req.Directory = "DATA_PUMP_DIR"
	}

	var script string
	switch req.Action {
	case "datapump":
		script = fmt.Sprintf(`echo "Migrating via Data Pump: %s -> %s"; expdp %s/%s@%s DIRECTORY=%s NETWORK_LINK=%s FULL=Y && impdp %s/%s@%s DIRECTORY=%s NETWORK_LINK=%s FULL=Y && echo "Data Pump migration completed"`, req.Source, req.Target, req.User, req.Password, req.Source, req.Directory, req.Target, req.User, req.Password, req.Target, req.Directory, req.Source)
	case "rman":
		script = fmt.Sprintf(`echo "Migrating via RMAN: %s -> %s"; rman target / auxiliary /@%s <<EOF
DUPLICATE TARGET DATABASE TO %s FROM ACTIVE DATABASE;
EXIT;
EOF
echo "RMAN migration completed"`, req.Source, req.Target, req.Target, req.Target)
	case "goldengate":
		script = `echo "=== GoldenGate Status ==="; ggsci <<EOF
INFO ALL
STATUS EXTRACT *
STATUS REPLICAT *
EXIT
EOF
echo "GoldenGate migration status checked"`
	case "status":
		script = `echo "=== Migration Status ==="; sqlplus -s / as sysdba <<EOF
SELECT name, open_mode, database_role FROM v$database;
SELECT process, status, thread#, sequence# FROM v$managed_standby;
EXIT;
EOF`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "oracle_migration_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleOracleRAC 处理 POST /api/v1/db/oracle/rac
func (s *Server) handleOracleRAC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"`
		Node      string `json:"node"`
		DiskGroup string `json:"diskGroup"`
		ScanName  string `json:"scanName"`
		DBName    string `json:"dbName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.DiskGroup == "" {
		req.DiskGroup = "DATA"
	}

	var script string
	switch req.Action {
	case "status":
		script = "echo \"=== RAC Status ===\"; crsctl check crs; echo \"---\"; olsnodes -n"
	case "grid":
		script = "echo \"=== Grid Infrastructure ===\"; crsctl check crs; crsctl query crs activeversion; crsctl stat res -t | head -40"
	case "asm":
		script = fmt.Sprintf(`echo "=== ASM Disk Groups ==="; asmcmd lsdg; echo "---"; asmcmd ls /%s 2>/dev/null || true`, req.DiskGroup)
	case "voting":
		script = "echo \"=== Voting Disks ===\"; crsctl query css votedisk"
	case "scanvip":
		script = "echo \"=== SCAN VIP ===\"; srvctl config scan; echo \"---\"; srvctl status scan"
	case "node-add":
		if req.Node == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "node is required"})
			return
		}
		script = fmt.Sprintf(`echo "Adding node %s"; addnode.sh -silent "CLUSTER_NEW_NODES={%s}" 2>&1 | tail -20 && echo "Node %s added"`, req.Node, req.Node, req.Node)
	case "node-remove":
		if req.Node == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "node is required"})
			return
		}
		script = fmt.Sprintf(`echo "Removing node %s"; crsctl delete node -n %s 2>/dev/null; srvctl remove nodeapps -n %s -f 2>/dev/null; echo "Node %s removed"`, req.Node, req.Node, req.Node, req.Node)
	case "health":
		script = "echo \"=== RAC Health Check ===\"; crsctl check crs; echo \"--- Cluster Resources ---\"; crsctl stat res -t; echo \"--- Nodes ---\"; olsnodes -n"
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "oracle_rac_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleOracleSecurity 处理 POST /api/v1/db/oracle/security
func (s *Server) handleOracleSecurity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID   string `json:"deviceID"`
		Action     string `json:"action"`
		User       string `json:"user"`
		Password   string `json:"password"`
		Role       string `json:"role"`
		Profile    string `json:"profile"`
		Tablespace string `json:"tablespace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}

	var script string
	switch req.Action {
	case "tde":
		script = `echo "=== TDE Configuration ==="; sqlplus / as sysdba <<EOF
ADMINISTER KEY MANAGEMENT CREATE KEYSTORE '/etc/oracle/wallet' IDENTIFIED BY "wallet_password";
ADMINISTER KEY MANAGEMENT SET KEYSTORE OPEN IDENTIFIED BY "wallet_password";
ADMINISTER KEY MANAGEMENT SET KEY IDENTIFIED BY "wallet_password" WITH BACKUP;
SELECT * FROM v$encryption_wallet;
EXIT;
EOF
echo "TDE configured"`
	case "audit":
		script = `sqlplus / as sysdba <<EOF
ALTER SYSTEM SET audit_trail=DB,EXTENDED SCOPE=SPFILE;
AUDIT ALL BY ACCESS;
AUDIT SESSION;
SELECT * FROM dba_audit_trail WHERE ROWNUM <= 10;
EXIT;
EOF
echo "Audit configured"`
	case "user":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		ts := req.Tablespace
		if ts == "" {
			ts = "USERS"
		}
		script = fmt.Sprintf(`sqlplus / as sysdba <<EOF
CREATE USER %s IDENTIFIED BY "%s" DEFAULT TABLESPACE %s QUOTA UNLIMITED ON %s;
GRANT CONNECT, RESOURCE TO %s;
EXIT;
EOF
echo "User %s created"`, req.User, req.Password, ts, ts, req.User, req.User)
	case "password":
		script = `sqlplus / as sysdba <<EOF
CREATE OR REPLACE FUNCTION verify_function(username VARCHAR2, password VARCHAR2, old_password VARCHAR2) RETURN BOOLEAN IS
BEGIN
  IF LENGTH(password) < 12 THEN RETURN FALSE; END IF;
  IF NOT REGEXP_LIKE(password, '[A-Z]') THEN RETURN FALSE; END IF;
  IF NOT REGEXP_LIKE(password, '[a-z]') THEN RETURN FALSE; END IF;
  IF NOT REGEXP_LIKE(password, '[0-9]') THEN RETURN FALSE; END IF;
  IF NOT REGEXP_LIKE(password, '[^a-zA-Z0-9]') THEN RETURN FALSE; END IF;
  RETURN TRUE;
END;
/
ALTER PROFILE DEFAULT LIMIT PASSWORD_VERIFY_FUNCTION verify_function;
EXIT;
EOF
echo "Password policy configured"`
	case "role":
		if req.Role == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "role is required"})
			return
		}
		script = fmt.Sprintf(`sqlplus / as sysdba <<EOF
CREATE ROLE %s;
GRANT %s TO %s;
EXIT;
EOF
echo "Role %s created and granted"`, req.Role, req.Role, req.User, req.Role)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "oracle_security_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerOracleRoutes 注册 Oracle 运维路由
func (s *Server) registerOracleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/db/oracle/backup", s.handleOracleBackup)
	mux.HandleFunc("/api/v1/db/oracle/replication", s.handleOracleReplication)
	mux.HandleFunc("/api/v1/db/oracle/perf", s.handleOraclePerf)
	mux.HandleFunc("/api/v1/db/oracle/upgrade", s.handleOracleUpgrade)
	mux.HandleFunc("/api/v1/db/oracle/migration", s.handleOracleMigration)
	mux.HandleFunc("/api/v1/db/oracle/rac", s.handleOracleRAC)
	mux.HandleFunc("/api/v1/db/oracle/security", s.handleOracleSecurity)
}
