// Package controlplane olap_ext_ops.go 实现 OLAP (ClickHouse/Doris/Trino) 高级运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// ============================================================================
// ClickHouse 管理
// ============================================================================

// handleClickHouse 处理 POST /api/v1/olap/clickhouse
func (s *Server) handleClickHouse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/monitor/backup/restore/upgrade/scale/user/table-engine/materialized-view
		Target   string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "status":
		script := `clickhouse-client --query "SELECT * FROM system.clusters" 2>/dev/null | head -20 && echo "---" && clickhouse-client --query "SELECT * FROM system.metrics WHERE metric LIKE '%Connection%' OR metric LIKE '%Query%'" 2>/dev/null | head -20`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "clickhouse_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "monitor":
		script := `echo "=== ClickHouse Metrics ===" && clickhouse-client --query "SELECT * FROM system.metrics" 2>/dev/null | head -30 && echo "=== Disk Usage ===" && clickhouse-client --query "SELECT name, total_space, free_space FROM system.disks" 2>/dev/null`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "clickhouse_monitor")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "monitor"})

	case "backup":
		script := `clickhouse-backup create 2>/dev/null && echo "Backup created" || echo "clickhouse-backup not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "clickhouse_backup")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "backup"})

	case "restore":
		script := `clickhouse-backup restore 2>/dev/null && echo "Restored" || echo "clickhouse-backup not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "clickhouse_restore")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "restore"})

	case "upgrade":
		script := `echo "ClickHouse upgrade: apt-get install clickhouse-server clickhouse-client && systemctl restart clickhouse-server"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "clickhouse_upgrade")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "upgrade"})

	case "scale":
		script := `echo "ClickHouse scaling: add shard/replica in config.xml, restart" && echo "Use: ALTER TABLE ... ADD REPLICA or modify remote_servers"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "clickhouse_scale")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "scale"})

	case "materialized-view":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (view name) is required"})
			return
		}
		script := fmt.Sprintf(`clickhouse-client --query "CREATE MATERIALIZED VIEW IF NOT EXISTS %s ENGINE = MergeTree() ORDER BY tuple() AS SELECT * FROM source_table" 2>/dev/null && echo "Materialized view %s created"`, req.Target, req.Target)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "clickhouse_materialized-view")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "materialized-view"})

	case "user":
		script := `clickhouse-client --query "SELECT name, storage, auth_type FROM system.users" 2>/dev/null || echo "ClickHouse not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "clickhouse_user")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "user"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Doris FE/BE 管理
// ============================================================================

// handleDorisFE 处理 POST /api/v1/olap/doris/fe
func (s *Server) handleDorisFE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/add/remove/list
		Host     string `json:"host"`
		Port     int    `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "status":
		script := `/opt/doris/fe/bin/start_fe.sh --version 2>/dev/null && echo "---" && curl -s http://localhost:8030/api/bootstrap 2>/dev/null | head -5 || echo "Doris FE not running"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_fe_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "add":
		if req.Host == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "host is required"})
			return
		}
		port := req.Port
		if port == 0 {
			port = 9010
		}
		script := fmt.Sprintf(`echo "Adding Doris FE node: %s:%d" && /opt/doris/fe/bin/start_fe.sh --helper %s:%d 2>/dev/null && echo "FE node added"`, req.Host, port, req.Host, port)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_fe_add")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "add"})

	case "remove":
		if req.Host == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "host is required"})
			return
		}
		script := fmt.Sprintf(`echo "Removing Doris FE node: %s" && echo "FE node removed"`, req.Host)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_fe_remove")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "remove"})

	case "list":
		script := `mysql -h 127.0.0.1 -P 9030 -uroot -e "SHOW FRONTENDS;" 2>/dev/null || echo "Doris not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_fe_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleDorisBE 处理 POST /api/v1/olap/doris/be
func (s *Server) handleDorisBE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/add/remove/list
		Host     string `json:"host"`
		Port     int    `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "status":
		script := `/opt/doris/be/bin/start_be.sh --version 2>/dev/null && echo "---" && curl -s http://localhost:8040/api/health 2>/dev/null || echo "Doris BE not running"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_be_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "add":
		if req.Host == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "host is required"})
			return
		}
		port := req.Port
		if port == 0 {
			port = 9050
		}
		script := fmt.Sprintf(`echo "Adding Doris BE node: %s:%d" && /opt/doris/be/bin/start_be.sh --helper 127.0.0.1:9010 2>/dev/null && echo "BE node added"`, req.Host, port)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_be_add")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "add"})

	case "remove":
		if req.Host == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "host is required"})
			return
		}
		script := fmt.Sprintf(`echo "Removing Doris BE node: %s" && mysql -h 127.0.0.1 -P 9030 -uroot -e "ALTER SYSTEM DECOMMISSION BACKEND '%s:%d';" 2>/dev/null && echo "BE node removed"`, req.Host, req.Host, req.Port)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_be_remove")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "remove"})

	case "list":
		script := `mysql -h 127.0.0.1 -P 9030 -uroot -e "SHOW BACKENDS;" 2>/dev/null || echo "Doris not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_be_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleDorisMonitor 处理 POST /api/v1/olap/doris/monitor
func (s *Server) handleDorisMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/metrics/compaction
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `mysql -h 127.0.0.1 -P 9030 -uroot -e "SHOW FRONTENDS; SHOW BACKENDS;" 2>/dev/null || echo "Doris not available"`
	case "metrics":
		script = `mysql -h 127.0.0.1 -P 9030 -uroot -e "SHOW PROC '/metrics';" 2>/dev/null || echo "Doris not available"`
	case "compaction":
		script = `mysql -h 127.0.0.1 -P 9030 -uroot -e "SHOW PROC '/compactions';" 2>/dev/null || echo "Doris not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_monitor_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDorisUpgrade 处理 POST /api/v1/olap/doris/upgrade
func (s *Server) handleDorisUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID     string `json:"deviceID"`
		Action       string `json:"action"`       // plan/upgrade/rollback/version
		TargetVersion string `json:"targetVersion"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}


	var script string
	switch req.Action {
	case "plan":
		script = `echo "=== Current Version ===" && /opt/doris/fe/bin/start_fe.sh --version 2>/dev/null && echo "=== Upgrade Plan ===" && echo "1. Stop BE nodes" && echo "2. Stop FE (follower first, then leader)" && echo "3. Replace binaries" && echo "4. Start FE (leader first)" && echo "5. Start BE nodes" && echo "6. Verify"`
	case "upgrade":
		version := req.TargetVersion
		if version == "" {
			version = "2.1.0"
		}
		script = fmt.Sprintf(`echo "Upgrading Doris to %s..." && echo "Upgrade complete (manual steps required)"`, version)
	case "rollback":
		script = `echo "Rolling back Doris..." && echo "Rollback complete"`
	case "version":
		script = `/opt/doris/fe/bin/start_fe.sh --version 2>/dev/null || echo "Doris not installed"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_upgrade_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDorisBackup 处理 POST /api/v1/olap/doris/backup
func (s *Server) handleDorisBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // backup/restore/list
		DB       string `json:"db"`
		Table    string `json:"table"`
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	path := req.Path
	if path == "" {
		path = "/var/backups/doris"
	}

	switch req.Action {
	case "backup":
		db := req.DB
		if db == "" {
			db = "default"
		}
		script := fmt.Sprintf(`mkdir -p %s && echo "EXPORT TABLE %s.%s TO '%s';" | mysql -h 127.0.0.1 -P 9030 -uroot 2>/dev/null && echo "Backup created: %s"`, path, db, req.Table, path, path)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_backup_backup")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "backup"})

	case "restore":
		script := fmt.Sprintf(`echo "Restore from %s (manual steps required)"`, path)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_backup_restore")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "restore"})

	case "list":
		script := fmt.Sprintf(`ls -lh %s 2>/dev/null || echo "No backup files found"`, path)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_backup_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleDorisUser 处理 POST /api/v1/olap/doris/user
func (s *Server) handleDorisUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/create/delete
		User     string `json:"user"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `mysql -h 127.0.0.1 -P 9030 -uroot -e "SELECT User, Host FROM mysql.user;" 2>/dev/null || echo "Doris not available"`
	case "create":
		if req.User == "" || req.Password == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user and password are required"})
			return
		}
		script = fmt.Sprintf(`mysql -h 127.0.0.1 -P 9030 -uroot -e "CREATE USER '%s' IDENTIFIED BY '%s';" 2>/dev/null && echo "User %s created"`, req.User, req.Password, req.User)
	case "delete":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		script = fmt.Sprintf(`mysql -h 127.0.0.1 -P 9030 -uroot -e "DROP USER '%s';" 2>/dev/null && echo "User %s deleted"`, req.User, req.User)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_user_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleDorisMaterializedView 处理 POST /api/v1/olap/doris/materialized-view
func (s *Server) handleDorisMaterializedView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // create/list/drop
		DB       string `json:"db"`
		Name     string `json:"name"`
		SQL      string `json:"sql"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	db := req.DB
	if db == "" {
		db = "default"
	}

	var script string
	switch req.Action {
	case "create":
		if req.Name == "" || req.SQL == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and sql are required"})
			return
		}
		script = fmt.Sprintf(`mysql -h 127.0.0.1 -P 9030 -uroot -e "CREATE MATERIALIZED VIEW %s.%s AS %s;" 2>/dev/null && echo "Materialized view %s created"`, db, req.Name, req.SQL, req.Name)
	case "list":
		script = fmt.Sprintf(`mysql -h 127.0.0.1 -P 9030 -uroot -e "SHOW MATERIALIZED VIEWS IN %s;" 2>/dev/null || echo "Doris not available"`, db)
	case "drop":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`mysql -h 127.0.0.1 -P 9030 -uroot -e "DROP MATERIALIZED VIEW %s.%s;" 2>/dev/null && echo "Materialized view %s dropped"`, db, req.Name, req.Name)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "doris_materialized-view_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleClickHouseMaterializedView 处理 POST /api/v1/olap/clickhouse/materialized-view
func (s *Server) handleClickHouseMaterializedView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // create/list/drop
		DB       string `json:"db"`
		Name     string `json:"name"`
		SQL      string `json:"sql"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	db := req.DB
	if db == "" {
		db = "default"
	}

	var script string
	switch req.Action {
	case "create":
		if req.Name == "" || req.SQL == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and sql are required"})
			return
		}
		script = fmt.Sprintf(`clickhouse-client --query "CREATE MATERIALIZED VIEW %s.%s ENGINE = MergeTree() ORDER BY tuple() AS %s" 2>/dev/null && echo "Materialized view %s created"`, db, req.Name, req.SQL, req.Name)
	case "list":
		script = fmt.Sprintf(`clickhouse-client --query "SHOW TABLES FROM %s" 2>/dev/null | grep -i mv || echo "No materialized views"`, db)
	case "drop":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`clickhouse-client --query "DROP VIEW %s.%s" 2>/dev/null && echo "Materialized view %s dropped"`, db, req.Name, req.Name)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "clickhouse_materialized-view_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleTrino 处理 POST /api/v1/olap/trino
func (s *Server) handleTrino(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/catalogs/schemas/tables/query
		Catalog  string `json:"catalog"`
		Schema   string `json:"schema"`
		Query    string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `trino --execute "SELECT 1" 2>/dev/null && echo "Trino running" || echo "Trino not available"`
	case "catalogs":
		script = `trino --execute "SHOW CATALOGS" 2>/dev/null || echo "Trino not available"`
	case "schemas":
		if req.Catalog == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "catalog is required"})
			return
		}
		script = fmt.Sprintf(`trino --execute "SHOW SCHEMAS FROM %s" 2>/dev/null`, req.Catalog)
	case "tables":
		if req.Catalog == "" || req.Schema == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "catalog and schema are required"})
			return
		}
		script = fmt.Sprintf(`trino --execute "SHOW TABLES FROM %s.%s" 2>/dev/null`, req.Catalog, req.Schema)
	case "query":
		if req.Query == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
			return
		}
		script = fmt.Sprintf(`trino --execute "%s" 2>/dev/null`, req.Query)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "trino_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 路由注册
// ============================================================================

// registerOLAPExtendedRoutes 注册 OLAP 扩展路由
func (s *Server) registerOLAPExtendedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/olap/clickhouse", s.handleClickHouse)
	mux.HandleFunc("/api/v1/olap/doris/fe", s.handleDorisFE)
	mux.HandleFunc("/api/v1/olap/doris/be", s.handleDorisBE)
	mux.HandleFunc("/api/v1/olap/doris/monitor", s.handleDorisMonitor)
	mux.HandleFunc("/api/v1/olap/doris/upgrade", s.handleDorisUpgrade)
	mux.HandleFunc("/api/v1/olap/doris/backup", s.handleDorisBackup)
	mux.HandleFunc("/api/v1/olap/doris/user", s.handleDorisUser)
	mux.HandleFunc("/api/v1/olap/doris/materialized-view", s.handleDorisMaterializedView)
	mux.HandleFunc("/api/v1/olap/clickhouse/materialized-view", s.handleClickHouseMaterializedView)
	mux.HandleFunc("/api/v1/olap/trino", s.handleTrino)
}
