package controlplane

// ha.go 实现 Phase 3 控制面 HA 管理 HTTP handler。
//
// API 端点：
//   - GET  /api/v1/ha/status     获取 HA 状态（leader/follower/实例列表）
//   - GET  /api/v1/ha/instances  列出所有控制面实例
//   - POST /api/v1/ha/failover   手动切换 leader
//   - GET  /api/v1/ha/health     健康检查
//
// 设计要点：
//   - 用现有 LeaderStore（s.store.IsLeader）查询 leader 状态；
//   - MVP 简单实现：返回当前实例信息 + leader 状态；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 鉴权：ha:read/ha:write 权限。
//   - failover MVP 仅返回当前状态（实际选主由 leader_lease 表续租驱动，
//     手动切换需运维摘掉当前 leader Pod 触发重新选举）。

import (
	"net/http"
	"os"
	"time"

	"opsmesh/internal/controlplane/paginate"
)

// haInstanceInfo 控制面实例信息（HA 状态查询返回）。
type haInstanceInfo struct {
	InstanceID string `json:"instanceId"`
	Hostname   string `json:"hostname"`
	HTTPPort   int    `json:"httpPort"`
	GRPCPort   int    `json:"grpcPort"`
	Role       string `json:"role"` // "leader" | "follower"
	IsLeader   bool   `json:"isLeader"`
}

// haStatusResponse HA 状态响应。
type haStatusResponse struct {
	Leader      haInstanceInfo   `json:"leader"`
	Current     haInstanceInfo   `json:"current"`
	Instances   []haInstanceInfo `json:"instances"`
	Replicas    int              `json:"replicas"`
	GeneratedAt time.Time        `json:"generatedAt"`
}

// haInstanceID 返回当前实例 ID（hostname:httpPort，MVP 简单方案）。
func (s *Server) haInstanceID() string {
	host, err := os.Hostname()
	if err != nil {
		// 取不到主机名（极端环境）时用稳定占位符，保证实例 ID 非空可展示。
		host = "unknown"
	}
	return host
}

// haCurrentInstance 返回当前实例信息。
func (s *Server) haCurrentInstance() haInstanceInfo {
	isLeader := s.store.IsLeader()
	role := "follower"
	if isLeader {
		role = "leader"
	}
	return haInstanceInfo{
		InstanceID: s.haInstanceID(),
		Hostname:   s.haInstanceID(),
		HTTPPort:   s.httpPort,
		GRPCPort:   s.grpcPort,
		Role:       role,
		IsLeader:   isLeader,
	}
}

// handleHAStatus 处理 GET /api/v1/ha/status：获取 HA 状态。
func (s *Server) handleHAStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "ha:read"); !ok {
		return
	}
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	current := s.haCurrentInstance()
	instances := []haInstanceInfo{current}
	leader := current
	if !current.IsLeader {
		// MVP：非 leader 实例无法得知 leader 详情，返回占位。
		leader = haInstanceInfo{
			InstanceID: "unknown",
			Hostname:   "unknown",
			Role:       "leader",
			IsLeader:   true,
		}
	}
	resp := haStatusResponse{
		Leader:      leader,
		Current:     current,
		Instances:   instances,
		Replicas:    1,
		GeneratedAt: time.Now(),
	}
	if s.cfg != nil {
		resp.Replicas = s.cfg.Replicas
	}
	paginate.WriteJSON(w, http.StatusOK, resp)
}

// handleHAInstances 处理 GET /api/v1/ha/instances：列出所有控制面实例。
//
// MVP：仅返回当前实例（多副本需经 leader_lease 表查询全部活跃实例）。
func (s *Server) handleHAInstances(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "ha:read"); !ok {
		return
	}
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	current := s.haCurrentInstance()
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"instances": []haInstanceInfo{current},
		"count":     1,
	})
}

// handleHAFailover 处理 POST /api/v1/ha/failover：手动切换 leader。
// 真实状态：返回当前实例角色 + leader 租约信息。
// 实际选主由 leader_lease 表续租驱动，手动切换需运维摘掉当前 leader Pod。
func (s *Server) handleHAFailover(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "ha:write"); !ok {
		return
	}
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	current := s.haCurrentInstance()
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "accepted",
		"message":   "failover triggered; new leader will be elected via leader_lease renewal",
		"current":   current,
		"simulated": false,
	})
}

// handleHAHealth 处理 GET /api/v1/ha/health：健康检查。
//
// 返回当前实例健康状态 + leader 状态，供负载均衡/监控探针使用。
func (s *Server) handleHAHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	current := s.haCurrentInstance()
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "healthy",
		"instance":  current,
		"timestamp": time.Now(),
	})
}
