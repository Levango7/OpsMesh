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
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/extension"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// allowedGatewayBackendSchemes 网关后端 scheme 白名单（L1 输入校验）。
// 仅允许 http/https，拒绝 file:// / ftp:// / ws:// 等不安全或不支持协议。
//
// grpc 的取舍（TD-66 勘测 2026-10-11）：数据面 handleGatewayProxy 只支持 http/https
// （grpc:// 建了也 502）。原先创建时允许 grpc:// ⇒ 「配了才发现不可用」——模型与实现不一致。
// 收紧为创建时即拒；将来数据面支持 gRPC 转发时再把白名单放开（连同数据面一起改）。
var allowedGatewayBackendSchemes = map[string]bool{
	"http":  true,
	"https": true,
}

// validateGatewayTargetBackend 校验 targetBackend 格式（L1 输入校验）。
// 格式要求：scheme://host:port，scheme ∈ {http,https}，host:port 可被 net.SplitHostPort 解析。
// 返回 ok=false 时 errmsg 为人类可读错误原因。
func validateGatewayTargetBackend(target string) (ok bool, errmsg string) {
	u, err := url.Parse(target)
	if err != nil || u.Scheme == "" {
		return false, "invalid targetBackend: parse failed"
	}
	if !allowedGatewayBackendSchemes[u.Scheme] {
		return false, "invalid targetBackend scheme: " + u.Scheme + " (want http|https)"
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
	breaker *extension.CircuitBreaker // TD-66 P2：熔断器（每路由一个）
}

// gatewayState API 网关运行期状态（Server 持有，进程级）。
//
// 设计要点：
//   - routes 按 tenantID -> ruleID -> entry 组织，按租户隔离；loaded 记录该租户是否
//     已从 store 加载到缓存（store 是事实源，缓存是读加速——TD-66 P1）；
//   - stats 为进程级统计计数器（多副本各自统计，未跨副本聚合）；
//   - mu 保护 routes + stats + loaded 并发安全。
type gatewayState struct {
	mu     sync.RWMutex
	routes map[string]map[string]*gatewayRouteEntry // tenantID -> ruleID -> entry
	loaded map[string]bool                          // tenantID -> 是否已从 store 加载（TD-66 P1）
	stats  extension.GatewayStats
}

// newGatewayState 构造一个空的网关状态。
func newGatewayState() *gatewayState {
	return &gatewayState{
		routes: make(map[string]map[string]*gatewayRouteEntry),
		loaded: make(map[string]bool),
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
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListGatewayRoutes 处理 GET /api/v1/gateway/routes：列出路由规则。
// TD-66 P1：从 store 加载该租户路由（事实源），进程内缓存仅作读加速。
func (s *Server) handleListGatewayRoutes(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "gateway:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	gw := s.ensureGateway()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	// 未加载过该租户 ⇒ 先从 store 读（重启后首次 list 即恢复全部路由）。
	if !gw.loaded[actx.TenantID] {
		rules := s.store.ListGatewayRoutes(actx.TenantID)
		if gw.routes[actx.TenantID] == nil {
			gw.routes[actx.TenantID] = make(map[string]*gatewayRouteEntry, len(rules))
		}
		for _, g := range rules {
			rule := gatewayToRouteRule(g)
			gw.routes[actx.TenantID][rule.ID] = &gatewayRouteEntry{
				rule:    rule,
				limiter: extension.NewRateLimiter(rule.RateLimitPerSec),
				breaker: extension.NewCircuitBreaker(),
			}
		}
		gw.loaded[actx.TenantID] = true
	}
	rules := make([]*extension.RouteRule, 0, len(gw.routes[actx.TenantID]))
	for _, e := range gw.routes[actx.TenantID] {
		rules = append(rules, e.rule)
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"routes": rules})
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
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.PathPrefix == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "pathPrefix is required"})
		return
	}
	if body.TargetBackend == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "targetBackend is required"})
		return
	}
	// L1 输入校验：targetBackend 格式 scheme://host:port，scheme ∈ {http,https,grpc}。
	if ok, msg := validateGatewayTargetBackend(body.TargetBackend); !ok {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
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
		breaker: extension.NewCircuitBreaker(),
	}
	// TD-66 P1：先落 store（事实源），失败即 5xx——不写"假成功"（重启后路由丢失
	// 会让用户误以为配好了）。store 成功后再更新进程内缓存。
	if err := s.store.SaveGatewayRoute(routeRuleToGateway(&body)); err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError,
			map[string]string{"error": "persist gateway route failed: " + err.Error()})
		return
	}
	gw := s.ensureGateway()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.routes[actx.TenantID] == nil {
		gw.routes[actx.TenantID] = make(map[string]*gatewayRouteEntry)
	}
	gw.routes[actx.TenantID][body.ID] = entry
	gw.loaded[actx.TenantID] = true
	// 审计：记录创建人（H9 写路径审计补齐，与 automation/webhook/slo 风格一致）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "gateway_route_create", Target: body.ID, Detail: sanitizeAuditDetail("name=" + body.Name),
	})
	paginate.WriteJSON(w, http.StatusCreated, entry.rule)
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
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "route id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "route id required"})
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
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
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
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
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
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "route not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, entry.rule)
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
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	gw := s.ensureGateway()
	gw.mu.Lock()
	defer gw.mu.Unlock()
	// 未加载过该租户 ⇒ 先从 store 读（TD-66 P1：读路径不假设缓存热）。
	if !gw.loaded[actx.TenantID] {
		if err := s.loadTenantRoutesLocked(gw, actx.TenantID); err != nil {
			paginate.WriteJSON(w, http.StatusInternalServerError,
				map[string]string{"error": "load gateway routes failed: " + err.Error()})
			return
		}
	}
	tenantRoutes := gw.routes[actx.TenantID]
	entry, exists := tenantRoutes[id]
	if !exists || entry == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "route not found"})
		return
	}
	// 保留不可改字段。
	body.ID = entry.rule.ID
	body.TenantID = entry.rule.TenantID
	body.CreatedAt = entry.rule.CreatedAt
	body.UpdatedAt = time.Now()
	// TD-66 P1：先落 store 再更新缓存（与 create 同序——store 失败即 5xx，不留假成功）。
	if err := s.store.SaveGatewayRoute(routeRuleToGateway(&body)); err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError,
			map[string]string{"error": "persist gateway route failed: " + err.Error()})
		return
	}
	newEntry := &gatewayRouteEntry{
		rule:    &body,
		limiter: extension.NewRateLimiter(body.RateLimitPerSec),
		breaker: extension.NewCircuitBreaker(),
	}
	tenantRoutes[id] = newEntry
	// 审计：记录更新人（H9 写路径审计补齐，与 automation/webhook/slo 风格一致）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "gateway_route_update", Target: id, Detail: sanitizeAuditDetail("name=" + body.Name),
	})
	paginate.WriteJSON(w, http.StatusOK, newEntry.rule)
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
	// 未加载过该租户 ⇒ 先从 store 读（TD-66 P1：删除也可能命中重启前配的路由）。
	if !gw.loaded[actx.TenantID] {
		if err := s.loadTenantRoutesLocked(gw, actx.TenantID); err != nil {
			paginate.WriteJSON(w, http.StatusInternalServerError,
				map[string]string{"error": "load gateway routes failed: " + err.Error()})
			return
		}
	}
	tenantRoutes := gw.routes[actx.TenantID]
	if _, exists := tenantRoutes[id]; !exists {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "route not found"})
		return
	}
	// TD-66 P1：先删 store 再删缓存（与 create/update 同序，store 失败即 5xx）。
	if !s.store.DeleteGatewayRoute(actx.TenantID, id) {
		paginate.WriteJSON(w, http.StatusInternalServerError,
			map[string]string{"error": "delete gateway route failed (store)"})
		return
	}
	delete(tenantRoutes, id)
	// 审计：记录删除人（H9 写路径审计补齐，与 automation/webhook/slo 风格一致）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "gateway_route_delete", Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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
	// 未加载过该租户 ⇒ 先从 store 读（TD-66 P1）。
	if !gw.loaded[actx.TenantID] {
		if err := s.loadTenantRoutesLocked(gw, actx.TenantID); err != nil {
			paginate.WriteJSON(w, http.StatusInternalServerError,
				map[string]string{"error": "load gateway routes failed: " + err.Error()})
			return
		}
	}
	tenantRoutes := gw.routes[actx.TenantID]
	entry, exists := tenantRoutes[id]
	if !exists || entry == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "route not found"})
		return
	}
	entry.rule.Enabled = enable
	entry.rule.UpdatedAt = time.Now()
	// TD-66 P1：启停也要落 store（否则重启后 gain 回到旧状态）。
	if err := s.store.SaveGatewayRoute(routeRuleToGateway(entry.rule)); err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError,
			map[string]string{"error": "persist gateway route failed: " + err.Error()})
		return
	}
	// 审计：记录启停人（H9 写路径审计补齐，与 automation/webhook/slo 风格一致）。
	auditAction := "gateway_route_enable"
	if !enable {
		auditAction = "gateway_route_disable"
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: auditAction, Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, entry.rule)
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
	paginate.WriteJSON(w, http.StatusOK, stats)
}

// randGatewayRouteID 生成随机网关路由 ID（"gw-route-" + 16 字节 hex）。
func randGatewayRouteID() string {
	return randHexID("gw-route")
}

// isTimeoutErr 判定传输错误是否超时（net.Error 的 Timeout()，覆盖 context.DeadlineExceeded）。
func isTimeoutErr(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// 路由转发超时（TD-66 P4）的默认值与下限。
const (
	defaultGatewayTimeoutMs = 30000
	minGatewayTimeoutMs     = 100
)

// gwStatResponseWriter 捕获响应状态码，供 handleGatewayProxy 统计错误数。
// 默认 status=200（http.ResponseWriter 未显式 WriteHeader 时隐式 200）。
type gwStatResponseWriter struct {
	http.ResponseWriter
	status int
}

// WriteHeader 记录状态码后透传。
func (w *gwStatResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// handleGatewayProxy 实现 API 网关数据面：按 PathPrefix+Methods 匹配 enabled 路由，
// 用 httputil.ReverseProxy 反向代理转发到 targetBackend。
//
// 设计要点：
//   - 挂载 /gw/ 前缀：剥前缀后用剩余路径按 extension.MatchRoute 匹配；
//   - 命中 enabled 路由 → 构造 ReverseProxy 转发；路由限流（RateLimitPerSec>0）超出返回 429；
//   - 统计：每次请求递增 TotalRequests；错误（404 无路由 / 429 限流 / >=500 代理失败）递增 TotalErrors；
//     维护 AvgLatencyMs 增量平均（newAvg = oldAvg + (latency-oldAvg)/count）；
//   - grpc:// 后端不支持（数据面仅 http/https），返回 502 并计入错误。
//
// 鉴权（P0 修复）：本 handler 曾是全仓唯一不做鉴权的反向代理——无凭证请求可直接经管理员
// 配置的路由访问内部后端、绕过全部 RBAC，且因聚合 gw.routes 全量而天然跨租户。
// 现与 service_proxy.go 的 handleServiceProxy 对齐，走同一条双闸：
//  1. subsystemAuthorize = requireTenantContext（租户头/令牌交叉校验）
//     + requireProd（JWT / API Key / 网关注入角色 / 联邦验签 / demo 五路身份）；
//  2. 路由池只取 actx.TenantID 名下的路由，不再全租户聚合（gw.routes 在
//     handleCreateGatewayRoute 创建时即以 actx.TenantID 为键，此处只是把该键用起来）；
//  3. TargetBackend 过 ValidateWebhookURL(allowPrivate=true)：网关后端本就是内网微服务，
//     故放行私网/环回，但该模式仍硬拒链路本地/云元数据/0.0.0.0/8，且对域名做 DNS 解析，
//     以拦住"域名解析到 169.254.169.254"这条 rebinding 形态。
func (s *Server) handleGatewayProxy(w http.ResponseWriter, r *http.Request) {
	// 权限按方法语义分级：只读方法要 gateway:read，其余（POST/PUT/DELETE…）要 gateway:write。
	perm := "gateway:read"
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		perm = "gateway:write"
	}
	actx, ok := s.subsystemAuthorize(w, r, perm)
	if !ok {
		return
	}

	gw := s.ensureGateway()

	// 剥 /gw 前缀得到路由匹配路径（如 /gw/api/v1/devices → /api/v1/devices）。
	path := strings.TrimPrefix(r.URL.Path, "/gw")
	if path == "" {
		path = "/"
	}

	// 只汇总**本租户**的路由（原实现遍历 gw.routes 全量，任意租户凭证可命中他租户路由）。
	// TD-66 P1：重启后该租户的缓存可能尚未建立——数据面首次请求即按需从 store 加载，
	// 否则重启后第一批 /gw/ 请求会因缓存为空而 404（以为路由丢了）。
	gw.mu.Lock()
	if !gw.loaded[actx.TenantID] {
		if err := s.loadTenantRoutesLocked(gw, actx.TenantID); err != nil {
			gw.mu.Unlock()
			log.Printf("controlplane: /gw/ 加载租户路由失败: %v", err)
			paginate.WriteJSON(w, http.StatusServiceUnavailable,
				map[string]string{"error": "load gateway routes failed"})
			return
		}
	}
	routes := make([]*extension.RouteRule, 0, len(gw.routes[actx.TenantID]))
	for _, e := range gw.routes[actx.TenantID] {
		if e != nil && e.rule != nil {
			routes = append(routes, e.rule)
		}
	}
	gw.mu.Unlock()
	rule := extension.MatchRoute(routes, path, r.Method)

	// 每次请求递增 TotalRequests（先于后续错误分支，保证统计覆盖所有请求）。
	started := time.Now()
	gw.mu.Lock()
	gw.stats.TotalRequests++
	gw.mu.Unlock()

	rw := &gwStatResponseWriter{ResponseWriter: w, status: http.StatusOK}
	// 请求结束后统计：错误计数 + 平均延迟增量更新 + Prometheus 埋点（TD-66 P3）。
	defer func() {
		latencyMs := time.Since(started).Seconds() * 1000
		gw.mu.Lock()
		if rw.status == http.StatusNotFound || rw.status == http.StatusTooManyRequests || rw.status >= http.StatusInternalServerError {
			gw.stats.TotalErrors++
		}
		if count := gw.stats.TotalRequests; count > 1 {
			gw.stats.AvgLatencyMs += (latencyMs - gw.stats.AvgLatencyMs) / float64(count)
		} else {
			gw.stats.AvgLatencyMs = latencyMs
		}
		gw.mu.Unlock()
		// Prometheus（TD-66 P3）：每路由一个时序（route/tenant/status 标签）。
		// 未命中路由用 route=":noroute"——同样要看见"配错前缀被打了多少次"。
		// 走 s.metrics 的专用方法而非通用 RecordHTTP：路径归一化会把不同路由折叠成
		// 同一个键，网关级别的路由区分度就没了。
		routeLabel := ":noroute"
		if rule != nil {
			routeLabel = rule.ID
		}
		if s.metrics != nil { // 测试构造的 Server 可能无注册表（白盒直构）
			s.metrics.RecordGatewayRequest(routeLabel, actx.TenantID, rw.status, latencyMs)
		}
	}()

	// 无命中路由 → 404。
	if rule == nil {
		paginate.WriteJSON(rw, http.StatusNotFound, map[string]string{"error": "no gateway route matches " + path})
		return
	}

	// 路由级限流（RateLimitPerSec>0 时令牌桶；超出返回 429）。
	gw.mu.RLock()
	entry := gw.routes[rule.TenantID][rule.ID]
	gw.mu.RUnlock()
	if entry != nil && !entry.limiter.Allow() {
		paginate.WriteJSON(rw, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded for route " + rule.ID})
		return
	}

	// 熔断判定（TD-66 P2）：后端连续 5 次 5xx/传输错误 ⇒ 该路由熔断 30s，
	// 期间请求 fast-fail 503 不再打到后端；冷却过半开放行**一个**试探，成功则恢复。
	if entry != nil && entry.breaker != nil && !entry.breaker.Allow() {
		paginate.WriteJSON(rw, http.StatusServiceUnavailable,
			map[string]string{"error": "gateway route circuit open (backend failing): " + rule.ID})
		return
	}

	// 后端解析：数据面仅支持 http/https；grpc:// 与非法 scheme 返回 502。
	target, err := url.Parse(rule.TargetBackend)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		paginate.WriteJSON(rw, http.StatusBadGateway, map[string]string{"error": "unsupported targetBackend: " + rule.TargetBackend})
		return
	}
	// 投递期复检 SSRF：路由是持久化配置，创建时校验过不等于此刻仍安全——
	// 域名的 DNS 记录可随时变更（rebinding）。allowPrivate=true 放行内网微服务后端，
	// 但仍硬拒链路本地/云元数据/0.0.0.0/8。
	//
	// 拒绝原因只进服务端日志，不进响应体：ValidateWebhookURL 的错误里含解析出的
	// IP 与拒绝类别，回吐等于把内网地址段与网关拓扑送给未授权调用方。
	if err := ValidateWebhookURL(rule.TargetBackend, true); err != nil {
		log.Printf("controlplane: /gw/ 后端 SSRF 复检拒绝（target 不入日志：属用户配置数据）: %v", err)
		paginate.WriteJSON(rw, http.StatusBadGateway, map[string]string{"error": "gateway target rejected by egress policy"})
		return
	}
	// TD-66 P4：单路由超时。RouteRule.TimeoutMs 为 0 时用默认 30s；
	// 用自定义 Transport 的 ResponseHeaderTimeout——只约束"等响应头"，
	// 不切断已建立的长响应（流式后端不会被误杀），且不小于已配置的限流周期语义。
	timeoutMs := defaultGatewayTimeoutMs
	if rule.TimeoutMs > 0 {
		timeoutMs = rule.TimeoutMs
	}
	if timeoutMs < minGatewayTimeoutMs {
		timeoutMs = minGatewayTimeoutMs // 防误配 0/极短值把本栈后端全部超时
	}
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{
		Scheme: target.Scheme, Host: target.Host,
	})
	proxy.Transport = &http.Transport{ResponseHeaderTimeout: time.Duration(timeoutMs) * time.Millisecond}
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		// 转发时去掉 /gw 前缀，后端按 PathPrefix 语义接收原始路径。
		req.URL.Path = path
		req.URL.RawPath = ""
	}
	// 传输层错误（连不上/超时）也计失败——半开试探与 closed 下的偶发都靠它驱动熔断。
	baseErrHandler := proxy.ErrorHandler
	proxy.ErrorHandler = func(rw2 http.ResponseWriter, req *http.Request, err error) {
		if entry != nil && entry.breaker != nil {
			entry.breaker.RecordFailure()
		}
		// 超时给 504 而不是 502：两者对运维的含义完全不同（"后端慢/挂住"vs"后端连不上"），
		// 混成一个码会让"路由 timeoutMs 配短了"与"后端崩了"在告警上无法区分。
		if isTimeoutErr(err) {
			paginate.WriteJSON(rw2, http.StatusGatewayTimeout, map[string]string{"error": "gateway route timeout"})
			return
		}
		if baseErrHandler != nil {
			baseErrHandler(rw2, req, err)
			return
		}
		paginate.WriteJSON(rw2, http.StatusBadGateway, map[string]string{"error": "gateway backend unavailable"})
	}
	// 按后端响应状态喂熔断器：5xx 记失败、其余记成功。
	// 4xx 是**业务应答**不是后端故障——把"后端好好的但请求有问题"算失败会造成误熔断。
	proxy.ModifyResponse = func(resp *http.Response) error {
		if entry == nil || entry.breaker == nil {
			return nil
		}
		if resp.StatusCode >= http.StatusInternalServerError {
			entry.breaker.RecordFailure()
			return nil
		}
		entry.breaker.RecordSuccess()
		return nil
	}
	proxy.ServeHTTP(rw, r)
}
