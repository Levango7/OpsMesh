// Package controlplane db_backup_ops.go 实现数据库备份/恢复运维
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleDBBackup 处理 POST /api/v1/db/backup
func (s *Server) handleDBBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // mysql/postgresql/mongodb/redis
		Action   string `json:"action"` // backup/restore/list
		DB       string `json:"db"`
		Table    string `json:"table"`
		Path     string `json:"path"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	path := req.Path
	if path == "" {
		path = "/var/backups/db"
	}

	switch req.Type {
	case "mysql":
		var script string
		switch req.Action {
		case "backup":
			db := req.DB
			if db == "" {
				db = "--all-databases"
			}
			script = fmt.Sprintf(`mkdir -p %s && mysqldump -h %s -P %d -u %s -p%s %s > %s/mysql_$(date +%%Y%%m%%d).sql 2>/dev/null && echo "MySQL backup created"`, path, req.Host, req.Port, req.User, req.Password, db, path)
			taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_backup_mysql_backup")
			if !ok {
				return
			}
			paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "backup"})
		case "restore":
			script = fmt.Sprintf(`mysql -h %s -P %d -u %s -p%s %s < %s/mysql_*.sql 2>/dev/null && echo "MySQL restored"`, req.Host, req.Port, req.User, req.Password, req.DB, path)
			taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_backup_mysql_restore")
			if !ok {
				return
			}
			paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "restore"})
		case "list":
			script = fmt.Sprintf(`ls -lh %s/mysql_*.sql 2>/dev/null || echo "No backup files found"`, path)
			taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_backup_mysql_list")
			if !ok {
				return
			}
			paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		}

	case "postgresql", "postgres":
		var script string
		switch req.Action {
		case "backup":
			db := req.DB
			if db == "" {
				db = "postgres"
			}
			script = fmt.Sprintf(`mkdir -p %s && PGPASSWORD=%s pg_dump -h %s -p %d -U %s -d %s -Fc -f %s/postgres_$(date +%%Y%%m%%d).dump 2>/dev/null && echo "PostgreSQL backup created"`, path, req.Password, req.Host, req.Port, req.User, db, path)
			taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_backup_postgres_backup")
			if !ok {
				return
			}
			paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "backup"})
		case "restore":
			script = fmt.Sprintf(`PGPASSWORD=%s pg_restore -h %s -p %d -U %s -d %s %s/postgres_*.dump 2>/dev/null && echo "PostgreSQL restored"`, req.Password, req.Host, req.Port, req.User, req.DB, path)
			taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_backup_postgres_restore")
			if !ok {
				return
			}
			paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "restore"})
		case "list":
			script = fmt.Sprintf(`ls -lh %s/postgres_*.dump 2>/dev/null || echo "No backup files found"`, path)
			taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_backup_postgres_list")
			if !ok {
				return
			}
			paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		}

	case "mongodb", "mongo":
		var script string
		switch req.Action {
		case "backup":
			db := req.DB
			if db == "" {
				db = "admin"
			}
			script = fmt.Sprintf(`mkdir -p %s && mongodump --host %s --port %d --username %s --password %s --db %s --out %s/mongo_$(date +%%Y%%m%%d) 2>/dev/null && echo "MongoDB backup created"`, path, req.Host, req.Port, req.User, req.Password, db, path)
			taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_backup_mongo_backup")
			if !ok {
				return
			}
			paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "backup"})
		case "restore":
			script = fmt.Sprintf(`mongorestore --host %s --port %d --username %s --password %s --db %s %s/mongo_*/ 2>/dev/null && echo "MongoDB restored"`, req.Host, req.Port, req.User, req.Password, req.DB, path)
			taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_backup_mongo_restore")
			if !ok {
				return
			}
			paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "restore"})
		case "list":
			script = fmt.Sprintf(`ls -d %s/mongo_* 2>/dev/null || echo "No backup files found"`, path)
			taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_backup_mongo_list")
			if !ok {
				return
			}
			paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		}

	case "redis":
		var script string
		switch req.Action {
		case "backup":
			script = fmt.Sprintf(`mkdir -p %s && redis-cli save 2>/dev/null && cp /var/lib/redis/dump.rdb %s/redis_$(date +%%Y%%m%%d).rdb && echo "Redis backup created"`, path, path)
			taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_backup_redis_backup")
			if !ok {
				return
			}
			paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "backup"})
		case "restore":
			script = fmt.Sprintf(`cp %s/redis_*.rdb /var/lib/redis/dump.rdb && systemctl restart redis && echo "Redis restored"`, path)
			taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_backup_redis_restore")
			if !ok {
				return
			}
			paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "restore"})
		case "list":
			script = fmt.Sprintf(`ls -lh %s/redis_*.rdb 2>/dev/null || echo "No backup files found"`, path)
			taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_backup_redis_list")
			if !ok {
				return
			}
			paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		}

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown db type: " + req.Type})
	}
}

// registerDBBackupRoutes 注册数据库备份路由
func (s *Server) registerDBBackupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/db/backup", s.handleDBBackup)
}
