// Package controlplane db_extended_ops.go 实现扩展数据库类型运维支持。
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleDBSQLite 处理 POST /api/v1/db/sqlite
func (s *Server) handleDBSQLite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		DB       string `json:"db"`
		Backup   string `json:"backup"`
		Dump     string `json:"dump"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	db := req.DB
	if db == "" {
		db = "/var/lib/sqlite/app.db"
	}
	backup := req.Backup
	if backup == "" {
		backup = "/var/backups/sqlite/app.backup"
	}
	dump := req.Dump
	if dump == "" {
		dump = "/var/backups/sqlite/app.dump.sql"
	}
	var script string
	switch req.Action {
	case "status":
		script = fmt.Sprintf(`echo "=== SQLite Status ===" && sqlite3 --version && echo "--- integrity ---" && sqlite3 %s "PRAGMA integrity_check;"`, shellQuote(db))
	case "deploy":
		script = `if command -v sqlite3 >/dev/null 2>&1; then echo 'SQLite3 already installed'; else apt-get install -y sqlite3; fi`
	case "backup":
		script = fmt.Sprintf(`mkdir -p $(dirname %s) && sqlite3 %s ".backup %s" && echo "backup via .backup done" && sqlite3 %s ".dump" > %s && echo "backup via .dump done"`, shellQuote(backup), shellQuote(db), shellQuote(backup), shellQuote(db), shellQuote(dump))
	case "restore":
		script = fmt.Sprintf(`sqlite3 %s ".read %s" && echo "restore done"`, shellQuote(db), shellQuote(dump))
	case "vacuum":
		script = fmt.Sprintf(`sqlite3 %s "VACUUM;" && echo "vacuum done"`, shellQuote(db))
	case "integrity":
		script = fmt.Sprintf(`sqlite3 %s "PRAGMA integrity_check;"`, shellQuote(db))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_sqlite_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDBMariaDB 处理 POST /api/v1/db/mariadb
func (s *Server) handleDBMariaDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID    string `json:"deviceID"`
		Action      string `json:"action"`
		DB          string `json:"db"`
		Host        string `json:"host"`
		Port        int    `json:"port"`
		User        string `json:"user"`
		Password    string `json:"password"`
		NewUser     string `json:"newUser"`
		NewPassword string `json:"newPassword"`
		Dump        string `json:"dump"`
		Master      string `json:"master"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	host := req.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := req.Port
	if port == 0 {
		port = 3306
	}
	db := req.DB
	if db == "" {
		db = "mysql"
	}
	dump := req.Dump
	if dump == "" {
		dump = "/var/backups/mariadb/dump.sql"
	}
	user := req.User
	if user == "" {
		user = "root"
	}
	var script string
	switch req.Action {
	case "status":
		script = `mariadb --version && systemctl status mariadb --no-pager`
	case "deploy":
		script = `if command -v mariadb >/dev/null 2>&1; then echo 'MariaDB already installed'; else apt-get install -y mariadb-server; fi`
	case "backup":
		script = fmt.Sprintf(`mkdir -p $(dirname %s) && mysqldump -h %s -P %d -u %s -p%s %s > %s && echo "MariaDB backup done"`, shellQuote(dump), shellQuote(host), port, shellQuote(user), shellQuote(req.Password), shellQuote(db), shellQuote(dump))
	case "restore":
		script = fmt.Sprintf(`mysql -h %s -P %d -u %s -p%s %s < %s && echo "MariaDB restore done"`, shellQuote(host), port, shellQuote(user), shellQuote(req.Password), shellQuote(db), shellQuote(dump))
	case "user":
		nu := req.NewUser
		if nu == "" {
			nu = "opsmesh"
		}
		np := req.NewPassword
		if np == "" {
			np = "ChangeMe123!"
		}
		script = fmt.Sprintf(`mysql -h %s -P %d -u %s -p%s -e "CREATE USER '%s'@'%%' IDENTIFIED BY '%s';" && echo "MariaDB user created"`, shellQuote(host), port, shellQuote(user), shellQuote(req.Password), nu, np)
	case "replication":
		script = fmt.Sprintf(`mysql -h %s -P %d -u %s -p%s -e "SHOW SLAVE STATUS"`, shellQuote(host), port, shellQuote(user), shellQuote(req.Password))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_mariadb_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDBClickHouse 处理 POST /api/v1/db/clickhouse
func (s *Server) handleDBClickHouse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID    string `json:"deviceID"`
		Action      string `json:"action"`
		Query       string `json:"query"`
		DB          string `json:"db"`
		User        string `json:"user"`
		Password    string `json:"password"`
		Backup      string `json:"backup"`
		NewUser     string `json:"newUser"`
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	backup := req.Backup
	if backup == "" {
		backup = "default_backup"
	}
	var script string
	switch req.Action {
	case "status":
		script = `clickhouse-client --version`
	case "deploy":
		script = `if command -v clickhouse-server >/dev/null 2>&1; then echo 'ClickHouse already installed'; else apt-get install -y clickhouse-server; fi`
	case "backup":
		script = fmt.Sprintf(`clickhouse-backup create %s && echo "ClickHouse backup done"`, shellQuote(backup))
	case "restore":
		script = fmt.Sprintf(`clickhouse-backup restore %s && echo "ClickHouse restore done"`, shellQuote(backup))
	case "user":
		nu := req.NewUser
		if nu == "" {
			nu = "opsmesh"
		}
		np := req.NewPassword
		if np == "" {
			np = "ChangeMe123!"
		}
		script = fmt.Sprintf(`clickhouse-client -q "CREATE USER %s IDENTIFIED WITH plaintext_password BY '%s';" && echo "ClickHouse user created"`, shellQuote(nu), np)
	case "query":
		q := req.Query
		if q == "" {
			q = "SELECT 1"
		}
		script = fmt.Sprintf(`clickhouse-client -q %s`, shellQuote(q))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_clickhouse_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDBInfluxDB 处理 POST /api/v1/db/influxdb
func (s *Server) handleDBInfluxDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID    string `json:"deviceID"`
		Action      string `json:"action"`
		Query       string `json:"query"`
		Org         string `json:"org"`
		Bucket      string `json:"bucket"`
		Token       string `json:"token"`
		BackupPath  string `json:"backupPath"`
		RestorePath string `json:"restorePath"`
		Username    string `json:"username"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	org := req.Org
	if org == "" {
		org = "opsmesh"
	}
	bucket := req.Bucket
	if bucket == "" {
		bucket = "default"
	}
	backupPath := req.BackupPath
	if backupPath == "" {
		backupPath = "/var/backups/influxdb"
	}
	restorePath := req.RestorePath
	if restorePath == "" {
		restorePath = "/var/backups/influxdb"
	}
	var script string
	switch req.Action {
	case "status":
		script = `influx version`
	case "deploy":
		script = `if command -v influx >/dev/null 2>&1; then echo 'InfluxDB already installed'; else apt-get install -y influxdb; fi`
	case "backup":
		script = fmt.Sprintf(`mkdir -p %s && influx backup %s && echo "InfluxDB backup done"`, shellQuote(backupPath), shellQuote(backupPath))
	case "restore":
		script = fmt.Sprintf(`influx restore %s && echo "InfluxDB restore done"`, shellQuote(restorePath))
	case "user":
		un := req.Username
		if un == "" {
			un = "opsmesh"
		}
		pw := req.Password
		if pw == "" {
			pw = "ChangeMe123!"
		}
		script = fmt.Sprintf(`influx user create --name %s --password %s --org %s && echo "InfluxDB user created"`, shellQuote(un), shellQuote(pw), shellQuote(org))
	case "query":
		q := req.Query
		if q == "" {
			q = "buckets()"
		}
		script = fmt.Sprintf(`influx query %s --org %s`, shellQuote(q), shellQuote(org))
	case "bucket":
		script = fmt.Sprintf(`influx bucket create --name %s --org %s && echo "InfluxDB bucket created"`, shellQuote(bucket), shellQuote(org))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_influxdb_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDBTimescaleDB 处理 POST /api/v1/db/timescaledb
func (s *Server) handleDBTimescaleDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		DB       string `json:"db"`
		Table    string `json:"table"`
		TimeCol  string `json:"timeCol"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		User     string `json:"user"`
		Password string `json:"password"`
		Dump     string `json:"dump"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	db := req.DB
	if db == "" {
		db = "postgres"
	}
	table := req.Table
	if table == "" {
		table = "metrics"
	}
	timeCol := req.TimeCol
	if timeCol == "" {
		timeCol = "time"
	}
	dump := req.Dump
	if dump == "" {
		dump = "/var/backups/timescaledb/dump.sql"
	}
	var script string
	switch req.Action {
	case "status":
		script = `psql -c "SELECT extversion FROM pg_extension WHERE extname='timescaledb'"`
	case "deploy":
		script = `if command -v timescaledb >/dev/null 2>&1; then echo 'TimescaleDB already installed'; else apt-get install -y timescaledb; fi`
	case "backup":
		script = fmt.Sprintf(`mkdir -p $(dirname %s) && pg_dump %s > %s && echo "TimescaleDB backup done"`, shellQuote(dump), shellQuote(db), shellQuote(dump))
	case "restore":
		script = fmt.Sprintf(`pg_restore -d %s %s && echo "TimescaleDB restore done"`, shellQuote(db), shellQuote(dump))
	case "hypertable":
		script = fmt.Sprintf(`psql -d %s -c "SELECT create_hypertable('%s','%s');" && echo "hypertable created"`, shellQuote(db), table, timeCol)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_timescaledb_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDBCockroachDB 处理 POST /api/v1/db/cockroachdb
func (s *Server) handleDBCockroachDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		DB       string `json:"db"`
		Join     string `json:"join"`
		Node     string `json:"node"`
		NodeID   string `json:"nodeID"`
		Backup   string `json:"backup"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	db := req.DB
	if db == "" {
		db = "defaultdb"
	}
	backup := req.Backup
	if backup == "" {
		backup = "nodelocal://1/backup"
	}
	var script string
	switch req.Action {
	case "status":
		script = `cockroach version`
	case "deploy":
		script = `wget -O /tmp/cockroach.tgz https://binaries.cockroachdb.com/cockroach-latest.linux-amd64.tgz && tar -xzf /tmp/cockroach.tgz -C /usr/local/bin/ --strip-components=1 && chmod +x /usr/local/bin/cockroach && echo "CockroachDB installed"`
	case "backup":
		script = fmt.Sprintf(`cockroach sql --insecure -e "BACKUP DATABASE %s TO '%s';" && echo "CockroachDB backup done"`, shellQuote(db), backup)
	case "restore":
		script = fmt.Sprintf(`cockroach sql --insecure -e "RESTORE DATABASE %s FROM '%s';" && echo "CockroachDB restore done"`, shellQuote(db), backup)
	case "node-add":
		join := req.Join
		if join == "" {
			join = "localhost:26257"
		}
		script = fmt.Sprintf(`cockroach node join --join=%s && echo "CockroachDB node added"`, shellQuote(join))
	case "node-remove":
		nid := req.NodeID
		if nid == "" {
			nid = "1"
		}
		script = fmt.Sprintf(`cockroach node decommission %s --wait=all && echo "CockroachDB node removed"`, shellQuote(nid))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_cockroachdb_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDBNeo4j 处理 POST /api/v1/db/neo4j
func (s *Server) handleDBNeo4j(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		DB       string `json:"db"`
		Query    string `json:"query"`
		Backup   string `json:"backup"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	db := req.DB
	if db == "" {
		db = "neo4j"
	}
	backup := req.Backup
	if backup == "" {
		backup = "/var/backups/neo4j"
	}
	var script string
	switch req.Action {
	case "status":
		script = `neo4j version`
	case "deploy":
		script = `if command -v neo4j >/dev/null 2>&1; then echo 'Neo4j already installed'; else apt-get install -y neo4j; fi`
	case "backup":
		script = fmt.Sprintf(`mkdir -p %s && neo4j-admin database dump %s --to-path=%s && echo "Neo4j backup done"`, shellQuote(backup), shellQuote(db), shellQuote(backup))
	case "restore":
		script = fmt.Sprintf(`neo4j-admin database load %s --from-path=%s && echo "Neo4j restore done"`, shellQuote(db), shellQuote(backup))
	case "query":
		q := req.Query
		if q == "" {
			q = "MATCH (n) RETURN n LIMIT 10"
		}
		script = fmt.Sprintf(`cypher-shell %s`, shellQuote(q))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_neo4j_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDBMinIO 处理 POST /api/v1/db/minio
func (s *Server) handleDBMinIO(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		Bucket   string `json:"bucket"`
		User     string `json:"user"`
		Password string `json:"password"`
		Source   string `json:"source"`
		Target   string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	bucket := req.Bucket
	if bucket == "" {
		bucket = "opsmesh"
	}
	user := req.User
	if user == "" {
		user = "opsmesh"
	}
	pw := req.Password
	if pw == "" {
		pw = "ChangeMe123!"
	}
	source := req.Source
	if source == "" {
		source = "/var/lib/minio/data"
	}
	target := req.Target
	if target == "" {
		target = "/var/backups/minio"
	}
	var script string
	switch req.Action {
	case "status":
		script = `minio --version`
	case "deploy":
		script = `wget -O /usr/local/bin/minio https://dl.min.io/server/minio/release/linux-amd64/minio && chmod +x /usr/local/bin/minio && echo "MinIO installed"`
	case "backup":
		script = fmt.Sprintf(`mc mirror %s %s && echo "MinIO backup done"`, shellQuote(source), shellQuote(target))
	case "bucket":
		script = fmt.Sprintf(`mc mb minio/%s && echo "MinIO bucket created"`, shellQuote(bucket))
	case "user":
		script = fmt.Sprintf(`mc admin user add minio %s %s && echo "MinIO user created"`, shellQuote(user), shellQuote(pw))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_minio_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerDBExtendedRoutes 注册扩展数据库运维路由。
func (s *Server) registerDBExtendedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/db/sqlite", s.handleDBSQLite)
	mux.HandleFunc("/api/v1/db/mariadb", s.handleDBMariaDB)
	mux.HandleFunc("/api/v1/db/clickhouse", s.handleDBClickHouse)
	mux.HandleFunc("/api/v1/db/influxdb", s.handleDBInfluxDB)
	mux.HandleFunc("/api/v1/db/timescaledb", s.handleDBTimescaleDB)
	mux.HandleFunc("/api/v1/db/cockroachdb", s.handleDBCockroachDB)
	mux.HandleFunc("/api/v1/db/neo4j", s.handleDBNeo4j)
	mux.HandleFunc("/api/v1/db/minio", s.handleDBMinIO)
}
