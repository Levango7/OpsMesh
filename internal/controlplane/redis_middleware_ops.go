// redis_middleware_ops.go Redis 中间件运维管理（Sentinel/Cluster）。
package controlplane

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// ============================================================================
// Redis Sentinel 管理（monitor/failover）
// ============================================================================

// redisSentinelReq Redis Sentinel 操作请求体。
type redisSentinelReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // monitor/failover
	Master   string `json:"master"` // master 名称（sentinel monitor 用）
	Host     string `json:"host"`   // sentinel 地址（host:port，默认 localhost:26379）
}

// handleMiddlewareRedisSentinel 处理 POST /api/v1/middleware/redis/sentinel。
func (s *Server) handleMiddlewareRedisSentinel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	var req redisSentinelReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Host == "" {
		req.Host = "localhost:26379"
	}
	allowed := map[string]bool{"monitor": true, "failover": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (monitor/failover)"})
		return
	}
	script := buildRedisSentinelScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "redis_sentinel_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.redis.sentinel", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "redis_sentinel_" + req.Action, Target: req.Master,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s host=%s", req.DeviceID, req.Host)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildRedisSentinelScript(req redisSentinelReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	fmt.Fprintf(&b, "SENTINEL_HOST=%q\n", req.Host)
	switch req.Action {
	case "monitor":
		if req.Master == "" {
			b.WriteString("echo 'sentinel monitor 需指定 master 名称' && exit 1\n")
		} else {
			fmt.Fprintf(&b, "redis-cli -h %s SENTINEL monitor %s 127.0.0.1 6379 2\n", req.Host, req.Master)
			fmt.Fprintf(&b, "echo 'Sentinel 已监控 master: %s'\n", req.Master)
		}
	case "failover":
		if req.Master == "" {
			b.WriteString("echo 'sentinel failover 需指定 master 名称' && exit 1\n")
		} else {
			fmt.Fprintf(&b, "redis-cli -h %s SENTINEL failover %s\n", req.Host, req.Master)
			fmt.Fprintf(&b, "echo 'Sentinel 已触发 master %s 故障转移'\n", req.Master)
		}
	}
	return b.String()
}

// ============================================================================
// Redis Cluster 管理（nodes/slots/rebalance）
// ============================================================================

// redisClusterReq Redis Cluster 操作请求体。
type redisClusterReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // nodes/slots/rebalance
	Host     string `json:"host"`   // cluster 节点地址（host:port，默认 localhost:6379）
}

// handleMiddlewareRedisCluster 处理 POST /api/v1/middleware/redis/cluster。
func (s *Server) handleMiddlewareRedisCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	var req redisClusterReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Host == "" {
		req.Host = "localhost:6379"
	}
	allowed := map[string]bool{"nodes": true, "slots": true, "rebalance": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (nodes/slots/rebalance)"})
		return
	}
	script := buildRedisClusterScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "redis_cluster_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.redis.cluster", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "redis_cluster_" + req.Action, Target: req.Host,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildRedisClusterScript(req redisClusterReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	fmt.Fprintf(&b, "CLUSTER_HOST=%q\n", req.Host)
	switch req.Action {
	case "nodes":
		fmt.Fprintf(&b, "redis-cli -h %s CLUSTER NODES\n", req.Host)
	case "slots":
		fmt.Fprintf(&b, "redis-cli -h %s CLUSTER SLOTS\n", req.Host)
	case "rebalance":
		fmt.Fprintf(&b, "echo 'rebalance 需在 redis-cli --cluster rebalance 中手动执行，host=%s'\n", req.Host)
	}
	fmt.Fprintf(&b, "echo 'Redis Cluster %s 操作完成 (host=%s)\n", req.Action, req.Host)
	return b.String()
}
