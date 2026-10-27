// Package controlplane db_replication_ops.go 实现数据库复制管理
package controlplane

import (
	"encoding/json"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleDBReplication 处理 POST /api/v1/db/replication
func (s *Server) handleDBReplication(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // mysql/postgresql/mongodb/redis
		Action   string `json:"action"` // status/lag/failover/promote
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Type {
	case "mysql":
		switch req.Action {
		case "status":
			script = `mysql -e "SHOW SLAVE STATUS\G" 2>/dev/null | grep -i "Slave_IO\|Slave_SQL\|Seconds_Behind\|Master_Host" || echo "MySQL replication not available"`
		case "lag":
			script = `mysql -e "SHOW SLAVE STATUS\G" 2>/dev/null | grep -i "Seconds_Behind" || echo "MySQL replication not available"`
		case "failover":
			script = `echo "MySQL failover: promote slave to master (manual steps required)"`
		case "promote":
			script = `mysql -e "STOP SLAVE; RESET SLAVE ALL;" 2>/dev/null && echo "Slave promoted to master" || echo "Promotion failed"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "postgresql", "postgres":
		switch req.Action {
		case "status":
			script = `psql -c "SELECT * FROM pg_stat_replication;" 2>/dev/null || echo "PostgreSQL replication not available"`
		case "lag":
			script = `psql -c "SELECT client_addr, state, sent_lsn, write_lsn, flush_lsn, replay_lsn, (extract(epoch from (now() - pg_last_xact_replay_timestamp())))::int AS lag_seconds FROM pg_stat_replication;" 2>/dev/null || echo "PostgreSQL replication not available"`
		case "failover":
			script = `echo "PostgreSQL failover: promote standby (manual steps required)"`
		case "promote":
			script = `pg_ctl promote -D /var/lib/postgresql/data 2>/dev/null && echo "Standby promoted" || echo "Promotion failed"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "mongodb", "mongo":
		switch req.Action {
		case "status":
			script = `mongosh --eval "rs.status()" 2>/dev/null | head -20 || echo "MongoDB replication not available"`
		case "lag":
			script = `mongosh --eval "rs.printReplicationInfo()" 2>/dev/null || echo "MongoDB replication not available"`
		case "failover":
			script = `mongosh --eval "rs.stepDown()" 2>/dev/null && echo "Failover triggered" || echo "Failover failed"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "redis":
		switch req.Action {
		case "status":
			script = `redis-cli info replication 2>/dev/null`
		case "lag":
			script = `redis-cli info replication 2>/dev/null | grep -i "lag\|offset"`
		case "failover":
			script = `redis-cli -p 26379 sentinel failover mymaster 2>/dev/null && echo "Failover triggered" || echo "Failover failed"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + req.Type})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_replication_"+req.Type+"_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerDBReplicationRoutes 注册数据库复制路由
func (s *Server) registerDBReplicationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/db/replication", s.handleDBReplication)
}
