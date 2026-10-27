// Package controlplane redis_cache_ops.go 实现 Redis 缓存场景运维
// 覆盖：缓存穿透/雪崩/击穿检测、RDB/AOF 配置、切片集群、扩缩容、故障转移
package controlplane

import (
	"encoding/json"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleRedisCacheScenario 处理 POST /api/v1/cache/redis/scenario
func (s *Server) handleRedisCacheScenario(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // detect-penetration/detect-avalanche/detect-breakdown/config-rdb/config-aof/slice-cluster/scale/failover
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "detect-penetration":
		script = `echo "=== 缓存穿透检测 ===" && echo "检测空值缓存命中率..." && redis-cli info stats 2>/dev/null | grep -i "keyspace_hits\|keyspace_misses" && echo "---" && echo "检测布隆过滤器..." && redis-cli bf.exists bf:keys "test" 2>/dev/null || echo "Bloom filter not configured" && echo "---" && echo "建议：启用空值缓存 (null cache) 或布隆过滤器"`
	case "detect-avalanche":
		script = `echo "=== 缓存雪崩检测 ===" && echo "检测 TTL 分布..." && redis-cli --scan --pattern "*" 2>/dev/null | head -20 && echo "---" && echo "检测过期键数量..." && redis-cli info keyspace 2>/dev/null && echo "---" && echo "建议：TTL 添加随机抖动 (jitter)、多级缓存、熔断降级"`
	case "detect-breakdown":
		script = `echo "=== 缓存击穿检测 ===" && echo "检测热点 Key 访问频率..." && redis-cli --hotkeys 2>/dev/null | head -20 || echo "Hot keys detection not available" && echo "---" && echo "建议：互斥锁 (mutex)、逻辑过期、singleflight"`
	case "config-rdb":
		script = `echo "=== RDB 配置 ===" && redis-cli config get save 2>/dev/null && redis-cli config get dbfilename 2>/dev/null && redis-cli config get dir 2>/dev/null && echo "---" && echo "RDB 策略: save 900 1 300 10 60 10000 (900s内1次写/300s内10次写/60s内10000次写)"`
	case "config-aof":
		script = `echo "=== AOF 配置 ===" && redis-cli config get appendonly 2>/dev/null && redis-cli config get appendfsync 2>/dev/null && redis-cli config get auto-aof-rewrite-percentage 2>/dev/null && echo "---" && echo "AOF 策略: appendfsync everysec (每秒同步), auto-aof-rewrite-percentage 100"`
	case "slice-cluster":
		script = `echo "=== 切片集群 ===" && redis-cli --cluster check localhost:6379 2>/dev/null || echo "Cluster not available" && echo "---" && redis-cli cluster info 2>/dev/null && echo "---" && redis-cli cluster nodes 2>/dev/null | head -20`
	case "scale":
		script = `echo "=== 扩缩容 ===" && echo "添加节点: redis-cli --cluster add-node <new_host>:<port> <existing_host>:<port>" && echo "移除节点: redis-cli --cluster del-node <existing_host>:<port> <node_id>" && echo "重新分片: redis-cli --cluster reshard <host>:<port> --cluster-from <from_id> --cluster-to <to_id> --cluster-slots <num> --cluster-yes"`
	case "failover":
		script = `echo "=== 故障转移 ===" && echo "Sentinel failover: redis-cli -p 26379 sentinel failover mymaster" && echo "Cluster failover: redis-cli --cluster failover" && echo "Manual: redis-cli cluster failover (on replica)"`

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_cache_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerRedisCacheScenarioRoutes 注册 Redis 缓存场景路由
func (s *Server) registerRedisCacheScenarioRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/cache/redis/scenario", s.handleRedisCacheScenario)
}
