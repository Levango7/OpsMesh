// Package controlplane db_perf_ops.go 实现数据库性能指标管理
// QPS/TPS/RT/并发数/吞吐量/慢查询/锁等待/连接数
package controlplane

import (
	"encoding/json"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleDBPerf 处理 POST /api/v1/db/perf
func (s *Server) handleDBPerf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // mysql/postgresql/mongodb/redis
		Action   string `json:"action"` // qps/tps/rt/concurrency/throughput/slow-queries/locks/connections
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Type {
	case "mysql":
		switch req.Action {
		case "qps":
			script = `mysql -e "SHOW GLOBAL STATUS LIKE 'Questions';" 2>/dev/null && echo "---" && mysql -e "SHOW GLOBAL STATUS LIKE 'Com_select';" 2>/dev/null`
		case "tps":
			script = `mysql -e "SHOW GLOBAL STATUS LIKE 'Com_insert';" 2>/dev/null && echo "---" && mysql -e "SHOW GLOBAL STATUS LIKE 'Com_update';" 2>/dev/null && echo "---" && mysql -e "SHOW GLOBAL STATUS LIKE 'Com_delete';" 2>/dev/null`
		case "rt":
			script = `mysql -e "SHOW GLOBAL STATUS LIKE 'Slow_queries';" 2>/dev/null && echo "---" && mysql -e "SHOW VARIABLES LIKE 'long_query_time';" 2>/dev/null`
		case "concurrency":
			script = `mysql -e "SHOW GLOBAL STATUS LIKE 'Threads_connected';" 2>/dev/null && echo "---" && mysql -e "SHOW GLOBAL STATUS LIKE 'Threads_running';" 2>/dev/null && echo "---" && mysql -e "SHOW VARIABLES LIKE 'max_connections';" 2>/dev/null`
		case "throughput":
			script = `mysql -e "SHOW GLOBAL STATUS LIKE 'Bytes_sent';" 2>/dev/null && echo "---" && mysql -e "SHOW GLOBAL STATUS LIKE 'Bytes_received';" 2>/dev/null`
		case "slow-queries":
			script = `mysql -e "SELECT * FROM information_schema.PROCESSLIST WHERE TIME > 5 ORDER BY TIME DESC;" 2>/dev/null | head -20 || echo "MySQL not available"`
		case "locks":
			script = `mysql -e "SHOW ENGINE INNODB STATUS\G" 2>/dev/null | grep -i "lock\|wait" | head -20 || echo "MySQL not available"`
		case "connections":
			script = `mysql -e "SHOW PROCESSLIST;" 2>/dev/null | head -30 || echo "MySQL not available"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "postgresql", "postgres":
		switch req.Action {
		case "qps":
			script = `psql -c "SELECT sum(calls) AS total_calls FROM pg_stat_statements;" 2>/dev/null || echo "PostgreSQL not available"`
		case "tps":
			script = `psql -c "SELECT xact_commit, xact_rollback FROM pg_stat_database WHERE datname=current_database();" 2>/dev/null || echo "PostgreSQL not available"`
		case "rt":
			script = `psql -c "SELECT mean_exec_time, max_exec_time, calls FROM pg_stat_statements ORDER BY mean_exec_time DESC LIMIT 10;" 2>/dev/null || echo "PostgreSQL not available"`
		case "concurrency":
			script = `psql -c "SELECT count(*) AS active_connections, (SELECT setting::int FROM pg_settings WHERE name='max_connections') AS max_connections FROM pg_stat_activity;" 2>/dev/null || echo "PostgreSQL not available"`
		case "throughput":
			script = `psql -c "SELECT blks_read, blks_hit, tup_returned, tup_fetched, tup_inserted, tup_updated, tup_deleted FROM pg_stat_database WHERE datname=current_database();" 2>/dev/null || echo "PostgreSQL not available"`
		case "slow-queries":
			script = `psql -c "SELECT query, mean_exec_time, calls FROM pg_stat_statements ORDER BY mean_exec_time DESC LIMIT 10;" 2>/dev/null || echo "PostgreSQL not available"`
		case "locks":
			script = `psql -c "SELECT * FROM pg_locks WHERE NOT granted;" 2>/dev/null || echo "PostgreSQL not available"`
		case "connections":
			script = `psql -c "SELECT * FROM pg_stat_activity;" 2>/dev/null | head -30 || echo "PostgreSQL not available"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "mongodb", "mongo":
		switch req.Action {
		case "qps":
			script = `mongosh --eval "db.serverStatus().opcounters" 2>/dev/null || echo "MongoDB not available"`
		case "tps":
			script = `mongosh --eval "db.serverStatus().opcountersRepl" 2>/dev/null || echo "MongoDB not available"`
		case "rt":
			script = `mongosh --eval "db.serverStatus().metrics.operation" 2>/dev/null || echo "MongoDB not available"`
		case "concurrency":
			script = `mongosh --eval "db.serverStatus().connections" 2>/dev/null || echo "MongoDB not available"`
		case "throughput":
			script = `mongosh --eval "db.serverStatus().network" 2>/dev/null || echo "MongoDB not available"`
		case "slow-queries":
			script = `mongosh --eval "db.system.profile.find().sort({ts:-1}).limit(10)" 2>/dev/null || echo "MongoDB not available"`
		case "locks":
			script = `mongosh --eval "db.serverStatus().locks" 2>/dev/null || echo "MongoDB not available"`
		case "connections":
			script = `mongosh --eval "db.currentOp()" 2>/dev/null | head -30 || echo "MongoDB not available"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "redis":
		switch req.Action {
		case "qps":
			script = `redis-cli info stats 2>/dev/null | grep -i "ops_per_sec\|total_commands_processed"`
		case "tps":
			script = `redis-cli info stats 2>/dev/null | grep -i "total_commands_processed\|rejected_connections"`
		case "rt":
			script = `redis-cli --latency 2>/dev/null | head -5 || echo "Redis not available"`
		case "concurrency":
			script = `redis-cli info clients 2>/dev/null | grep -i "connected_clients\|blocked_clients"`
		case "throughput":
			script = `redis-cli info stats 2>/dev/null | grep -i "total_net_input_bytes\|total_net_output_bytes"`
		case "slow-queries":
			script = `redis-cli slowlog get 10 2>/dev/null || echo "No slowlog entries"`
		case "connections":
			script = `redis-cli client list 2>/dev/null | head -20`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + req.Type})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "db_perf_"+req.Type+"_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerDBPerfRoutes 注册数据库性能路由
func (s *Server) registerDBPerfRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/db/perf", s.handleDBPerf)
}
