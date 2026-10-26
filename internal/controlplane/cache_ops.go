// Package controlplane cache_ops.go 实现缓存系统全面运维场景
// 支持：Redis (单机/主从/Sentinel/Cluster), Memcached, Apache Ignite
// 场景：部署、生命周期、集群管理、持久化、淘汰策略、调优、监控、安全、备份恢复
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// ============================================================================
// Redis 部署
// ============================================================================

// handleCacheDeploy 处理 POST /api/v1/cache/deploy
func (s *Server) handleCacheDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`     // redis/memcached/ignite
		Mode     string `json:"mode"`     // standalone/master-slave/sentinel/cluster
		Version  string `json:"version"`  // e.g. 7.2
		Port     int    `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Type == "" {
		req.Type = "redis"
	}

	switch req.Type {
	case "redis":
		version := req.Version
		if version == "" {
			version = "7.2"
		}
		mode := req.Mode
		if mode == "" {
			mode = "standalone"
		}
		port := req.Port
		if port == 0 {
			port = 6379
		}
		var script string
		switch mode {
		case "standalone":
			script = fmt.Sprintf(`apt-get install -y redis-server 2>/dev/null || yum install -y redis 2>/dev/null && sed -i 's/^port .*/port %d/' /etc/redis/redis.conf && systemctl enable redis-server && systemctl start redis-server && echo "Redis %s deployed (standalone, port %d)"`, port, version, port)
		case "cluster":
			script = fmt.Sprintf(`apt-get install -y redis-server 2>/dev/null && echo "Redis %s installed (cluster mode: use redis-cli --cluster create)"`, version)
		default:
			script = fmt.Sprintf(`apt-get install -y redis-server 2>/dev/null && systemctl enable redis-server && systemctl start redis-server && echo "Redis %s deployed (%s)"`, version, mode)
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "cache_deploy_redis")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})

	case "memcached":
		version := req.Version
		if version == "" {
			version = "1.6.29"
		}
		port := req.Port
		if port == 0 {
			port = 11211
		}
		script := fmt.Sprintf(`apt-get install -y memcached 2>/dev/null || yum install -y memcached 2>/dev/null && sed -i 's/^-p .*/-p %d/' /etc/memcached.conf 2>/dev/null && systemctl enable memcached && systemctl start memcached && echo "Memcached %s deployed (port %d)"`, port, version, port)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "cache_deploy_memcached")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})

	case "ignite":
		version := req.Version
		if version == "" {
			version = "2.16.0"
		}
		script := fmt.Sprintf(`wget -q https://archive.apache.org/dist/ignite/%s/apache-ignite-%s-bin.zip -O /tmp/ignite.zip && unzip -q /tmp/ignite.zip -d /opt/ && ln -sfn /opt/apache-ignite-%s-bin /opt/ignite && echo "Apache Ignite %s deployed"`, version, version, version, version)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "cache_deploy_ignite")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + req.Type})
	}
}

// ============================================================================
// Redis 生命周期
// ============================================================================

// handleCacheLifecycle 处理 POST /api/v1/cache/lifecycle
func (s *Server) handleCacheLifecycle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // redis/memcached/ignite
		Action   string `json:"action"` // start/stop/restart/status
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Type == "" {
		req.Type = "redis"
	}

	var script string
	switch req.Type {
	case "redis":
		switch req.Action {
		case "start":
			script = "systemctl start redis-server && echo 'Redis started'"
		case "stop":
			script = "systemctl stop redis-server && echo 'Redis stopped'"
		case "restart":
			script = "systemctl restart redis-server && echo 'Redis restarted'"
		case "status":
			script = "systemctl status redis-server 2>/dev/null || redis-cli ping"
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "memcached":
		switch req.Action {
		case "start":
			script = "systemctl start memcached && echo 'Memcached started'"
		case "stop":
			script = "systemctl stop memcached && echo 'Memcached stopped'"
		case "restart":
			script = "systemctl restart memcached && echo 'Memcached restarted'"
		case "status":
			script = "systemctl status memcached 2>/dev/null || echo 'Memcached not running'"
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	case "ignite":
		switch req.Action {
		case "start":
			script = "/opt/ignite/bin/ignite.sh /opt/ignite/config/default-config.xml & echo 'Ignite started'"
		case "stop":
			script = "pkill -f ignite 2>/dev/null && echo 'Ignite stopped'"
		case "status":
			script = "jps | grep -i ignite || echo 'Ignite not running'"
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
			return
		}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + req.Type})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "cache_lifecycle_"+req.Type+"_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Redis Cluster 管理
// ============================================================================

// handleRedisCluster 处理 POST /api/v1/cache/redis/cluster
func (s *Server) handleRedisCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // create/add-node/remove-node/reshard/nodes/slots/rebalance
		Nodes    string `json:"nodes"`
		Replicas int    `json:"replicas"`
		Slot     int    `json:"slot"`
		FromNode string `json:"fromNode"`
		ToNode   string `json:"toNode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "create":
		if req.Nodes == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "nodes is required (host:port,host:port,...)"})
			return
		}
		replicas := req.Replicas
		if replicas <= 0 {
			replicas = 1
		}
		script := fmt.Sprintf(`redis-cli --cluster create %s --cluster-replicas %d --cluster-yes 2>/dev/null && echo "Redis cluster created"`, req.Nodes, replicas)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_cluster_create")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create"})

	case "add-node":
		if req.Nodes == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "nodes is required"})
			return
		}
		script := fmt.Sprintf(`redis-cli --cluster add-node %s localhost:6379 2>/dev/null && echo "Node added"`, req.Nodes)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_cluster_add-node")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "add-node"})

	case "remove-node":
		if req.Nodes == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "nodes is required"})
			return
		}
		script := fmt.Sprintf(`redis-cli --cluster del-node localhost:6379 %s 2>/dev/null && echo "Node removed"`, req.Nodes)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_cluster_remove-node")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "remove-node"})

	case "reshard":
		if req.Slot <= 0 || req.FromNode == "" || req.ToNode == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "slot, fromNode, toNode are required"})
			return
		}
		script := fmt.Sprintf(`redis-cli --cluster reshard localhost:6379 --cluster-from %s --cluster-to %s --cluster-slots %d --cluster-yes 2>/dev/null && echo "Resharded %d slots"`, req.FromNode, req.ToNode, req.Slot, req.Slot)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_cluster_reshard")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "reshard"})

	case "nodes":
		script := `redis-cli --cluster check localhost:6379 2>/dev/null || redis-cli cluster nodes 2>/dev/null`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_cluster_nodes")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "nodes"})

	case "slots":
		script := `redis-cli cluster slots 2>/dev/null || echo "Cluster not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_cluster_slots")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "slots"})

	case "rebalance":
		script := `redis-cli --cluster rebalance localhost:6379 2>/dev/null && echo "Cluster rebalanced"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_cluster_rebalance")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "rebalance"})

	case "failover":
		script := `redis-cli --cluster failover 2>/dev/null && echo "Failover triggered"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_cluster_failover")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "failover"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Redis Sentinel 管理 (增强版)
// ============================================================================

// handleRedisSentinel 处理 POST /api/v1/cache/redis/sentinel
func (s *Server) handleRedisSentinel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // monitor/failover/status/masters/slaves
		Master   string `json:"master"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Quorum   int    `json:"quorum"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "monitor":
		if req.Master == "" || req.Host == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "master and host are required"})
			return
		}
		port := req.Port
		if port == 0 {
			port = 6379
		}
		quorum := req.Quorum
		if quorum == 0 {
			quorum = 2
		}
		script := fmt.Sprintf(`redis-cli -p 26379 sentinel monitor %s %s %d %d 2>/dev/null && echo "Sentinel monitoring %s"`, req.Master, req.Host, port, quorum, req.Master)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_sentinel_monitor")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "monitor"})

	case "failover":
		if req.Master == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "master is required"})
			return
		}
		script := fmt.Sprintf(`redis-cli -p 26379 sentinel failover %s 2>/dev/null && echo "Failover triggered for %s"`, req.Master, req.Master)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_sentinel_failover")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "failover"})

	case "status":
		script := `redis-cli -p 26379 info sentinel 2>/dev/null | head -20 || echo "Sentinel not running"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_sentinel_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "masters":
		script := `redis-cli -p 26379 sentinel masters 2>/dev/null`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_sentinel_masters")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "masters"})

	case "slaves":
		if req.Master == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "master is required"})
			return
		}
		script := fmt.Sprintf(`redis-cli -p 26379 sentinel slaves %s 2>/dev/null`, req.Master)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_sentinel_slaves")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "slaves"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Redis 持久化 (RDB/AOF)
// ============================================================================

// handleRedisPersistence 处理 POST /api/v1/cache/redis/persistence
func (s *Server) handleRedisPersistence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // rdb-save/rdb-bgsave/aof-rewrite/aof-status/config
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "rdb-save":
		script := `redis-cli save 2>/dev/null && echo "RDB saved (blocking)" || echo "Redis not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_persistence_rdb-save")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "rdb-save"})

	case "rdb-bgsave":
		script := `redis-cli bgsave 2>/dev/null && echo "RDB background save started" || echo "Redis not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_persistence_rdb-bgsave")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "rdb-bgsave"})

	case "aof-rewrite":
		script := `redis-cli bgrewriteaof 2>/dev/null && echo "AOF rewrite started" || echo "Redis not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_persistence_aof-rewrite")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "aof-rewrite"})

	case "aof-status":
		script := `redis-cli info persistence 2>/dev/null | grep -i "aof\|rdb" || echo "Redis not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_persistence_aof-status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "aof-status"})

	case "config":
		script := `redis-cli config get save 2>/dev/null; redis-cli config get appendonly 2>/dev/null; redis-cli config get appendfsync 2>/dev/null`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_persistence_config")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "config"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Redis 淘汰策略 & 调优
// ============================================================================

// handleRedisTuning 处理 POST /api/v1/cache/redis/tuning
func (s *Server) handleRedisTuning(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID    string `json:"deviceID"`
		Action      string `json:"action"` // get/set/analyze/recommend
		MaxMemory   string `json:"maxMemory"`   // e.g. 2gb
		Eviction    string `json:"eviction"`    // noeviction/allkeys-lru/volatile-lru/etc
		MaxClients  int    `json:"maxClients"`
		IOThreads   int    `json:"ioThreads"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "get":
		script := `redis-cli config get maxmemory 2>/dev/null; redis-cli config get maxmemory-policy 2>/dev/null; redis-cli config get maxclients 2>/dev/null; redis-cli config get io-threads 2>/dev/null`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_tuning_get")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "get"})

	case "set":
		configs := ""
		if req.MaxMemory != "" {
			configs += fmt.Sprintf("maxmemory %s ", req.MaxMemory)
		}
		if req.Eviction != "" {
			configs += fmt.Sprintf("maxmemory-policy %s ", req.Eviction)
		}
		if req.MaxClients > 0 {
			configs += fmt.Sprintf("maxclients %d ", req.MaxClients)
		}
		if req.IOThreads > 0 {
			configs += fmt.Sprintf("io-threads %d ", req.IOThreads)
		}
		if configs == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one config is required"})
			return
		}
		script := fmt.Sprintf(`redis-cli config set %s 2>/dev/null && redis-cli config rewrite 2>/dev/null && echo "Tuning applied"`, configs)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_tuning_set")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "set"})

	case "analyze":
		script := `echo "=== Memory ===" && redis-cli info memory 2>/dev/null | head -15 && echo "=== Stats ===" && redis-cli info stats 2>/dev/null | head -15 && echo "=== Clients ===" && redis-cli info clients 2>/dev/null | head -10`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_tuning_analyze")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "analyze"})

	case "recommend":
		script := `echo "=== Recommendations ===" && echo "1. maxmemory: 75%% of available RAM" && echo "2. maxmemory-policy: allkeys-lru (cache) / volatile-lru (session)" && echo "3. maxclients: 10000" && echo "4. io-threads: 4-8 (multi-core)" && echo "5. tcp-backlog: 511" && echo "6. timeout: 300 (idle client timeout)" && echo "7. Enable AOF for durability" && echo "8. Use cluster mode for HA"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_tuning_recommend")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "recommend"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Redis 监控
// ============================================================================

// handleRedisMonitor 处理 POST /api/v1/cache/redis/monitor
func (s *Server) handleRedisMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // info/slowlog/stats/clients/replication
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "info":
		script := `redis-cli info 2>/dev/null | head -40`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_monitor_info")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "info"})

	case "slowlog":
		script := `redis-cli slowlog get 10 2>/dev/null || echo "No slowlog entries"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_monitor_slowlog")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "slowlog"})

	case "stats":
		script := `redis-cli info stats 2>/dev/null`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_monitor_stats")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "stats"})

	case "clients":
		script := `redis-cli client list 2>/dev/null | head -20`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_monitor_clients")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "clients"})

	case "replication":
		script := `redis-cli info replication 2>/dev/null`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_monitor_replication")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "replication"})

	case "memory":
		script := `redis-cli info memory 2>/dev/null`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_monitor_memory")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "memory"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Redis 安全 (ACL)
// ============================================================================

// handleRedisSecurity 处理 POST /api/v1/cache/redis/security
func (s *Server) handleRedisSecurity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list-users/add-user/delete-user/set-acl
		User     string `json:"user"`
		Password string `json:"password"`
		Rules    string `json:"rules"` // ACL rules
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "list-users":
		script := `redis-cli acl list 2>/dev/null || echo "ACL not enabled"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_security_list-users")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-users"})

	case "add-user":
		if req.User == "" || req.Password == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user and password are required"})
			return
		}
		rules := req.Rules
		if rules == "" {
			rules = "+@all ~*"
		}
		script := fmt.Sprintf(`redis-cli acl setuser %s on >%s %s 2>/dev/null && echo "User %s created"`, req.User, req.Password, rules, req.User)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_security_add-user")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "add-user"})

	case "delete-user":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		script := fmt.Sprintf(`redis-cli acl deluser %s 2>/dev/null && echo "User %s deleted"`, req.User, req.User)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_security_delete-user")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete-user"})

	case "set-acl":
		if req.User == "" || req.Rules == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user and rules are required"})
			return
		}
		script := fmt.Sprintf(`redis-cli acl setuser %s %s 2>/dev/null && echo "ACL updated for %s"`, req.User, req.Rules, req.User)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_security_set-acl")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "set-acl"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Redis 备份/恢复
// ============================================================================

// handleRedisBackup 处理 POST /api/v1/cache/redis/backup
func (s *Server) handleRedisBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // backup/restore/list
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	path := req.Path
	if path == "" {
		path = "/var/lib/redis/dump.rdb"
	}

	switch req.Action {
	case "backup":
		script := fmt.Sprintf(`redis-cli save 2>/dev/null && cp /var/lib/redis/dump.rdb %s && echo "Backup saved to %s"`, path, path)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_backup_backup")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "backup"})

	case "restore":
		script := fmt.Sprintf(`cp %s /var/lib/redis/dump.rdb && systemctl restart redis-server && echo "Restored from %s"`, path, path)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_backup_restore")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "restore"})

	case "list":
		script := fmt.Sprintf(`ls -lh %s 2>/dev/null || echo "No backup files found"`, path)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_backup_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Memcached 管理
// ============================================================================

// handleMemcached 处理 POST /api/v1/cache/memcached
func (s *Server) handleMemcached(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // stats/slabs/items/flush
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "stats":
		script := `echo "stats" | nc localhost 11211 2>/dev/null | head -30 || echo "Memcached not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "memcached_stats")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "stats"})

	case "slabs":
		script := `echo "stats slabs" | nc localhost 11211 2>/dev/null | head -30 || echo "Memcached not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "memcached_slabs")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "slabs"})

	case "items":
		script := `echo "stats items" | nc localhost 11211 2>/dev/null | head -30 || echo "Memcached not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "memcached_items")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "items"})

	case "flush":
		script := `echo "flush_all" | nc localhost 11211 2>/dev/null && echo "Memcached flushed"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "memcached_flush")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "flush"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Apache Ignite 管理
// ============================================================================

// handleIgnite 处理 POST /api/v1/cache/ignite
func (s *Server) handleIgnite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/caches/list-nodes
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "status":
		script := `jps | grep -i ignite || echo "Ignite not running" && echo "---" && curl -s http://localhost:8080/ignite?cmd=version 2>/dev/null || echo "REST not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "ignite_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "caches":
		script := `curl -s http://localhost:8080/ignite?cmd=cache 2>/dev/null || echo "Ignite REST not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "ignite_caches")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "caches"})

	case "list-nodes":
		script := `curl -s http://localhost:8080/ignite?cmd=top 2>/dev/null || echo "Ignite REST not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "ignite_list-nodes")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-nodes"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 路由注册
// ============================================================================

// registerCacheRoutes 注册缓存路由
func (s *Server) registerCacheRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/cache/deploy", s.handleCacheDeploy)
	mux.HandleFunc("/api/v1/cache/lifecycle", s.handleCacheLifecycle)
	mux.HandleFunc("/api/v1/cache/redis/cluster", s.handleRedisCluster)
	mux.HandleFunc("/api/v1/cache/redis/sentinel", s.handleRedisSentinel)
	mux.HandleFunc("/api/v1/cache/redis/persistence", s.handleRedisPersistence)
	mux.HandleFunc("/api/v1/cache/redis/tuning", s.handleRedisTuning)
	mux.HandleFunc("/api/v1/cache/redis/monitor", s.handleRedisMonitor)
	mux.HandleFunc("/api/v1/cache/redis/security", s.handleRedisSecurity)
	mux.HandleFunc("/api/v1/cache/redis/backup", s.handleRedisBackup)
	mux.HandleFunc("/api/v1/cache/memcached", s.handleMemcached)
	mux.HandleFunc("/api/v1/cache/ignite", s.handleIgnite)
}
