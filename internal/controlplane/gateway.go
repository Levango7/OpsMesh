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
//   - 用 s.requireTenantContext(w, r) 提取租户；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 用 decodeJSONBody 解析请求体；
//   - 鉴权：需 gateway:read/gateway:write 权限。
//
// MVP 内存实现：路由规则保存在 Server.gatewayRoutes map（按 tenantID 隔离），
// 不持久化到 Store（网关路由为运行期配置，重启重置）。
// 统计为内存计数器，进程级聚合（多副本各自统计，未做跨副本聚合）。

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/extension"
	"opsmesh/internal/proto"
)

// allowedGatewayBackendSchemes 网关后端 scheme 白名单（L1 输入校验）。
// 仅允许 http/https/grpc，拒绝 file:// / ftp:// / ws:// 等不安全或不支持协议。
var allowedGatewayBackendSchemes = map[string]bool{
	"http":  true,
	"https": true,
	"grpc":  true,
}

// validateGatewayTargetBackend 校验 targetBackend 格式（L1 输入校验）。
// 格式要求：scheme://host:port，scheme ∈ {http,https,grpc}，host:port 可被 net.SplitHostPort 解析。
// 返回 ok=false 时 errmsg 为人类可读错误原因。
func validateGatewayTargetBackend(target string) (ok bool, errmsg string) {
	u, err := url.Parse(target)
	if err != nil || u.Scheme == "" {
		return false, "invalid targetBackend: parse failed"
	}
	if !allowedGatewayBackendSchemes[u.Scheme] {
		return false, "invalid targetBackend scheme: " + u.Scheme + " (want http|https|grpc)"
	}
	// u.Host 应为 host:port 形式；net.SplitHostPort 强制校验端口存在且格式合法。
	if u.Host == "" {
		return false, "invalid targetBackend: missing host:port"
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		return false, "invalid targetBackend host:port: " + err.Error()
	}
	return true, ""
}

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
//
// 并发安全：用 s.gatewayOnce.Do 保证只构造一次。即使多个 handler goroutine
// 同时进入，sync.Once 也会让构造函数只执行一次，其余调用直接拿到已赋值的 s.gateway。
// 不依赖外部 mu，避免与 gatewayState.mu 嵌套加锁。
func (s *Server) ensureGateway() *gatewayState {
	s.gatewayOnce.Do(func() {
		if s.gateway == nil {
			s.gateway = newGatewayState()
		}
	})
	return s.gateway
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
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	gw := s.ensureGateway()
	gw.mu.RLock()
	defer gw.mu.RUnlock()
	tenantRoutes := gw.routes[actx.TenantID]
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
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
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
	// L1 输入校验：targetBackend 格式 scheme://host:port，scheme ∈ {http,https,grpc}。
	if ok, msg := validateGatewayTargetBackend(body.TargetBackend); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	now := time.Now()
	body.ID = randGatewayRouteID()
	body.TenantID = actx.TenantID
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
	if gw.routes[actx.TenantID] == nil {
		gw.routes[actx.TenantID] = make(map[string]*gatewayRouteEntry)
	}
	gw.routes[actx.TenantID][body.ID] = entry
	// 审计：记录创建人（H9 写路径审计补齐，与 automation/webhook/slo 风格一致）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "gateway_route_create", Target: body.ID, Detail: sanitizeAuditDetail("name=" + body.Name),
	})
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
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	gw := s.ensureGateway()
	gw.mu.RLock()
	defer gw.mu.RUnlock()
	tenantRoutes := gw.routes[actx.TenantID]
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
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
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
	tenantRoutes := gw.routes[actx.TenantID]
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
	// 审计：记录更新人（H9 写路径审计补齐，与 automation/webhook/slo 风格一致）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "gateway_route_update", Target: id, Detail: sanitizeAuditDetail("name=" + body.Name),
	})
	writeJSON(w, http.StatusOK, newEntry.rule)
}

// handleDeleteGatewayRoute 处理 DELETE /api/v1/gateway/routes/{id}：删除路由。
func (s *Server) handleDeleteGatewayRoute(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "gateway:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	gw := s.ensureGateway()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	tenantRoutes := gw.routes[actx.TenantID]
	if _, exists := tenantRoutes[id]; !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "route not found"})
		return
	}
	delete(tenantRoutes, id)
	// 审计：记录删除人（H9 写路径审计补齐，与 automation/webhook/slo 风格一致）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "gateway_route_delete", Target: id,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleGatewayRouteToggle 处理 POST /api/v1/gateway/routes/{id}/enable|disable：启停路由。
func (s *Server) handleGatewayRouteToggle(w http.ResponseWriter, r *http.Request, id string, enable bool) {
	caller, ok := s.requirePermission(w, r, "gateway:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	gw := s.ensureGateway()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	tenantRoutes := gw.routes[actx.TenantID]
	entry, exists := tenantRoutes[id]
	if !exists || entry == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "route not found"})
		return
	}
	entry.rule.Enabled = enable
	entry.rule.UpdatedAt = time.Now()
	// 审计：记录启停人（H9 写路径审计补齐，与 automation/webhook/slo 风格一致）。
	auditAction := "gateway_route_enable"
	if !enable {
		auditAction = "gateway_route_disable"
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: auditAction, Target: id,
	})
	writeJSON(w, http.StatusOK, entry.rule)
}

// handleGatewayStats 处理 GET /api/v1/gateway/stats：网关统计。
func (s *Server) handleGatewayStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "gateway:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	gw := s.ensureGateway()
	gw.mu.RLock()
	defer gw.mu.RUnlock()
	// 统计当前租户的活跃路由数。
	tenantRoutes := gw.routes[actx.TenantID]
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


