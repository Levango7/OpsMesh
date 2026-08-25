package controlplane


// gateway.go 实现 Phase 5 API 网关 HTTP handler（路由规则 CRUD + 启停 + 统计）。
//
// API 端点：
//   - GET    /api/v1/gateway/routes           列出路由规则
//   - POST   /api/v1/gateway/routes           创建路由规则
//   - GET    /api/v1/gateway/routes/{id}      路由规则详情
//   - PUT    /api/v1/gateway/routes/{id}      更新路由规则
//   - DELETE /api/v1/gateway/routes/{id}      删除路由规则
//   - POST   /api/v1/gateway/routes/{id}/enable  启用路由
//   - POST   /api/v1/gateway/routes/{id}/disable 禁用路由
//   - GET    /api/v1/gateway/stats            网关统计
//
// 设计要点（与 automation.go 风格一致）：
//   - 用 s.k8sTenantFromRequest(r) 提取租户；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 用 decodeJSONBody 解析请求体；
//   - 鉴权：需 gateway:read/gateway:write 权限。
//
// MVP 内存实现：路由规则保存在 Server.gatewayRoutes map（按 tenantID 隔离），
// 不持久化到 Store（网关路由为运行期配置，重启重置）。
// 统计为内存计数器，进程级聚合（多副本各自统计，未做跨副本聚合）。

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/extension"
)

// gatewayRouteEntry 网关路由规则条目（含 RateLimiter 实例）。
// RateLimiter 按 RateLimitPerSec 构造，运行期复用避免每次请求重建。
type gatewayRouteEntry struct {
	rule    *extension.RouteRule
	limiter *extension.RateLimiter
}

// gatewayState API 网关运行期状态（Server 持有，进程级）。
//
// 设计要点：
//   - routes 按 tenantID -> ruleID -> entry 组织，按租户隔离；
//   - stats 为进程级统计计数器（多副本各自统计，未跨副本聚合）；
//   - mu 保护 routes + stats 并发安全。
type gatewayState struct {
	mu     sync.RWMutex
	routes map[string]map[string]*gatewayRouteEntry // tenantID -> ruleID -> entry
	stats  extension.GatewayStats
}

// newGatewayState 构造一个空的网关状态。
func newGatewayState() *gatewayState {
	return &gatewayState{
		routes: make(map[string]map[string]*gatewayRouteEntry),
	}
}

// ensureGateway 确保 s.gateway 已初始化（测试场景直接构造 Server{} 时兜底）。
// 已初始化时直接返回；未初始化时构造一个空状态并赋值（并发安全：用 mu 保护赋值）。
// 注意：本方法用 sync.Once 语义保证只构造一次，避免覆盖已初始化的状态。
func (s *Server) ensureGateway() *gatewayState {
	if s.gateway != nil {
		return s.gateway
	}
	// 测试兜底：直接构造（单线程测试场景，无需 sync.Once）。
	s.gateway = newGatewayState()
	return s.gateway
}

// gatewayTenantFromRequest 提取请求归属租户（网关租户隔离）。
// 复用 k8sTenantFromRequest 的逻辑。
func (s *Server) gatewayTenantFromRequest(r *http.Request) string {
	return s.k8sTenantFromRequest(r)
}

// handleGatewayRoutes 统一处理 /api/v1/gateway/routes：
//   - GET：列出路由规则
//   - POST：创建路由规则
func (s *Server) handleGatewayRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListGatewayRoutes(w, r)
	case http.MethodPost:
		s.handleCreateGatewayRoute(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListGatewayRoutes 处理 GET /api/v1/gateway/routes：列出路由规则。
func (s *Server) handleListGatewayRoutes(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "gateway:read"); !ok {
		return
	}
	tenant := s.gatewayTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	gw := s.ensureGateway()
	gw.mu.RLock()
	defer gw.mu.RUnlock()
	tenantRoutes := gw.routes[tenant]
	rules := make([]*extension.RouteRule, 0, len(tenantRoutes))
	for _, e := range tenantRoutes {
		rules = append(rules, e.rule)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"routes": rules})
}

// handleCreateGatewayRoute 处理 POST /api/v1/gateway/routes：创建路由规则。
func (s *Server) handleCreateGatewayRoute(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "gateway:write")
	if !ok {
		return
	}
	tenant := s.gatewayTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	var body extension.RouteRule
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.PathPrefix == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pathPrefix is required"})
		return
	}
	if body.TargetBackend == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "targetBackend is required"})
		return
	}
	now := time.Now()
	body.ID = randGatewayRouteID()
	body.TenantID = tenant
	if body.CreatedAt.IsZero() {
		body.CreatedAt = now
	}
	body.UpdatedAt = now
	entry := &gatewayRouteEntry{
		rule:    &body,
		limiter: extension.NewRateLimiter(body.RateLimitPerSec),
	}
	gw := s.ensureGateway()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.routes[tenant] == nil {
		gw.routes[tenant] = make(map[string]*gatewayRouteEntry)
	}
	gw.routes[tenant][body.ID] = entry
	_ = caller
	writeJSON(w, http.StatusCreated, entry.rule)
}

// handleGatewayRouteRouting 分派 /api/v1/gateway/routes/{id} 子路径：
//   - GET    /api/v1/gateway/routes/{id}        路由详情
//   - PUT    /api/v1/gateway/routes/{id}        更新路由
//   - DELETE /api/v1/gateway/routes/{id}        删除路由
//   - POST   /api/v1/gateway/routes/{id}/enable  启用
//   - POST   /api/v1/gateway/routes/{id}/disable 禁用
func (s *Server) handleGatewayRouteRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/gateway/routes/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "route id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "route id required"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetGatewayRoute(w, r, id)
		case http.MethodPut:
			s.handleUpdateGatewayRoute(w, r, id)
		case http.MethodDelete:
			s.handleDeleteGatewayRoute(w, r, id)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "enable":
		s.handleGatewayRouteToggle(w, r, id, true)
	case "disable":
		s.handleGatewayRouteToggle(w, r, id, false)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetGatewayRoute 处理 GET /api/v1/gateway/routes/{id}：路由详情。
func (s *Server) handleGetGatewayRoute(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "gateway:read"); !ok {
		return
	}
	tenant := s.gatewayTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	gw := s.ensureGateway()
	gw.mu.RLock()
	defer gw.mu.RUnlock()
	tenantRoutes := gw.routes[tenant]
	entry, ok := tenantRoutes[id]
	if !ok || entry == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "route not found"})
		return
	}
	writeJSON(w, http.StatusOK, entry.rule)
}

// handleUpdateGatewayRoute 处理 PUT /api/v1/gateway/routes/{id}：更新路由。
func (s *Server) handleUpdateGatewayRoute(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "gateway:write")
	if !ok {
		return
	}
	tenant := s.gatewayTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	var body extension.RouteRule
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	gw := s.ensureGateway()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	tenantRoutes := gw.routes[tenant]
	entry, exists := tenantRoutes[id]
	if !exists || entry == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "route not found"})
		return
	}
	// 保留不可改字段。
	body.ID = entry.rule.ID
	body.TenantID = entry.rule.TenantID
	body.CreatedAt = entry.rule.CreatedAt
	body.UpdatedAt = time.Now()
	newEntry := &gatewayRouteEntry{
		rule:    &body,
		limiter: extension.NewRateLimiter(body.RateLimitPerSec),
	}
	tenantRoutes[id] = newEntry
	_ = caller
	writeJSON(w, http.StatusOK, newEntry.rule)
}

// handleDeleteGatewayRoute 处理 DELETE /api/v1/gateway/routes/{id}：删除路由。
func (s *Server) handleDeleteGatewayRoute(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "gateway:write")
	if !ok {
		return
	}
	tenant := s.gatewayTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	gw := s.ensureGateway()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	tenantRoutes := gw.routes[tenant]
	if _, exists := tenantRoutes[id]; !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "route not found"})
		return
	}
	delete(tenantRoutes, id)
	_ = caller
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleGatewayRouteToggle 处理 POST /api/v1/gateway/routes/{id}/enable|disable：启停路由。
func (s *Server) handleGatewayRouteToggle(w http.ResponseWriter, r *http.Request, id string, enable bool) {
	caller, ok := s.requirePermission(w, r, "gateway:write")
	if !ok {
		return
	}
	tenant := s.gatewayTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	gw := s.ensureGateway()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	tenantRoutes := gw.routes[tenant]
	entry, exists := tenantRoutes[id]
	if !exists || entry == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "route not found"})
		return
	}
	entry.rule.Enabled = enable
	entry.rule.UpdatedAt = time.Now()
	_ = caller
	writeJSON(w, http.StatusOK, entry.rule)
}

// handleGatewayStats 处理 GET /api/v1/gateway/stats：网关统计。
func (s *Server) handleGatewayStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "gateway:read"); !ok {
		return
	}
	tenant := s.gatewayTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	gw := s.ensureGateway()
	gw.mu.RLock()
	defer gw.mu.RUnlock()
	// 统计当前租户的活跃路由数。
	tenantRoutes := gw.routes[tenant]
	activeRoutes := 0
	for _, e := range tenantRoutes {
		if e.rule.Enabled {
			activeRoutes++
		}
	}
	stats := extension.GatewayStats{
		TotalRequests: gw.stats.TotalRequests,
		TotalErrors:   gw.stats.TotalErrors,
		AvgLatencyMs:  gw.stats.AvgLatencyMs,
		ActiveRoutes:  activeRoutes,
	}
	writeJSON(w, http.StatusOK, stats)
}

// randGatewayRouteID 生成随机网关路由 ID（"gw-route-" + 16 字节 hex）。
func randGatewayRouteID() string {
	return randHexID("gw-route")
}