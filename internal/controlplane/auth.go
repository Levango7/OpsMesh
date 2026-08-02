// auth.go 实现用户中心 HTTP handler：注册/登录/查询当前用户 + 用户/角色/权限 CRUD。
//
// 与 server.go 中已有的"网关注入身份"模式（authctx.FromHTTPHeader）互补：
//   - 网关注入模式：内核不自鉴权，身份由前置网关（APISIX/IAM）注入 X-Tenant-ID 等头；
//   - 用户中心模式：内核自行管理用户/角色/权限，登录/注册后签发 JWT，后续请求携带 token 鉴权。
//
// 两种模式并存：网关注入用于 agent↔控制面 gRPC 通道（已有），用户中心用于 B/S 仪表盘登录（新增）。
//
// 鉴权流程：
//  1. POST /api/v1/auth/register | login → 校验凭据 → 签发 JWT（HS256）→ 返回 {token, user}；
//  2. 后续请求携带 Authorization: Bearer <token>；
//  3. 用户管理 API（/api/v1/users、/api/v1/roles）从 token 提取用户 → 校验 user:read/write 权限；
//  4. /api/v1/auth/me 从 token 提取用户信息返回。
//
// 安全要点：
//   - 密码 bcrypt 哈希（绝不存明文）；最短 6 字符；
//   - JWT 密钥来自 config.JWTSecret（空=随机生成，重启后旧 token 失效）；
//   - 用户管理 API 需认证 + 权限校验（user:read/user:write/user:delete）；
//   - 错误响应统一 {"error": "message"} 格式。
package controlplane

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/authctx"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"

	"golang.org/x/crypto/bcrypt"
)

// minPasswordLen 密码最短长度（安全基线）。
const minPasswordLen = 6

// jwtTokenExpiry JWT token 有效期（24h，与 SignJWT 默认一致）。
const jwtTokenExpiry = 24 * time.Hour

// randHexID 生成随机十六进制 ID（16 字节，crypto/rand 密码学安全）。
// 用于用户/角色 ID 分配（调用方未填 ID 时）。
func randHexID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 熵源失败回退时间戳（降级但可容忍，唯一性由 store 索引兜底）。
		return prefix + "-" + hex.EncodeToString([]byte(time.Now().Format("20060102150405"))) + "fallback"
	}
	return prefix + "-" + hex.EncodeToString(b)
}

// hashPassword 用 bcrypt 哈希密码（DefaultCost=10）。
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// verifyPassword 校验 bcrypt 哈希与明文密码是否匹配。
func verifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ============================================================================
// loginGuard 登录/注册防爆破 + 限流（P1-4，进程内实现）。
//   - 限流：按客户端 IP 令牌桶，约束单位时间登录/注册尝试次数，防撞库与 DoS。
//   - 防爆破：按用户名累计失败次数，超阈值临时锁定账号，挫败密码爆破。
//
// 设计为进程内（单副本足够）；多副本 HA 部署下各副本独立计数，建议后续以 Redis
// 共享计数（接口此处保持稳定即可平滑替换）。
// ============================================================================

const (
	loginRateBurst  = 10            // 令牌桶容量（瞬时允许的最大尝试数）
	loginRateRefill = 1.0 / 3.0     // 令牌补充速率（每秒），约每 3s 1 个，≈20/min
	loginMaxFails   = 5             // 单账号允许的连续失败次数
	loginFailWindow = 15 * time.Minute // 失败计数滑动窗口
	loginLockDur    = 15 * time.Minute // 账号锁定时长
)

type loginGuard struct {
	mu    sync.Mutex
	ips   map[string]*rateRec // 客户端 IP -> 限流令牌桶
	fails map[string]*failRec // 用户名 -> 失败计数记录
}

type rateRec struct {
	tokens float64
	last   time.Time
}

type failRec struct {
	count       int
	firstAt     time.Time
	lockedUntil time.Time
}

func newLoginGuard() *loginGuard {
	return &loginGuard{
		ips:   make(map[string]*rateRec),
		fails: make(map[string]*failRec),
	}
}

// allow 按 IP 令牌桶判断本次尝试是否被限流（true=放行）。
func (g *loginGuard) allow(ip string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	rec, ok := g.ips[ip]
	if !ok {
		rec = &rateRec{tokens: loginRateBurst, last: now}
		g.ips[ip] = rec
	}
	elapsed := now.Sub(rec.last).Seconds()
	rec.tokens += elapsed * loginRateRefill
	if rec.tokens > loginRateBurst {
		rec.tokens = loginRateBurst
	}
	rec.last = now
	if rec.tokens < 1 {
		return false // 令牌不足，限流
	}
	rec.tokens--
	return true
}

// recordFail 记录一次账号失败尝试；返回是否触发锁定。
func (g *loginGuard) recordFail(username string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	rec, ok := g.fails[username]
	if !ok || now.Sub(rec.firstAt) > loginFailWindow {
		rec = &failRec{count: 0, firstAt: now}
		g.fails[username] = rec
	}
	rec.count++
	if rec.count >= loginMaxFails {
		rec.lockedUntil = now.Add(loginLockDur)
		return true
	}
	return false
}

// locked 判断账号当前是否处于锁定状态。
func (g *loginGuard) locked(username string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	rec, ok := g.fails[username]
	if !ok {
		return false
	}
	return time.Now().Before(rec.lockedUntil)
}

// resetFail 登录成功后清除该账号失败计数（解锁）。
func (g *loginGuard) resetFail(username string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.fails, username)
}

// clientIP 提取客户端真实 IP：优先 X-Forwarded-For（网关/代理场景），否则去 RemoteAddr 端口。
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx > 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// userPermissions 展开用户经角色获得的全部权限字符串（去重）。
// 用户 → RoleIDs → 各 Role.Permissions → 合并去重。
func (s *Server) userPermissions(u *store.User) []string {
	if u == nil || len(u.RoleIDs) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, rid := range u.RoleIDs {
		r := s.store.GetRole(rid)
		if r == nil {
			continue
		}
		for _, p := range r.Permissions {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// userRoleNames 返回用户绑定的角色名列表（用于 JWT claims，便于前端展示）。
func (s *Server) userRoleNames(u *store.User) []string {
	if u == nil || len(u.RoleIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(u.RoleIDs))
	for _, rid := range u.RoleIDs {
		if r := s.store.GetRole(rid); r != nil {
			out = append(out, r.Name)
		}
	}
	return out
}

// issueUserToken 为用户签发 JWT token。
// claims 包含：用户 ID/用户名/角色 ID/权限/租户/过期时间。
func (s *Server) issueUserToken(u *store.User) (string, error) {
	claims := authctx.JWTClaims{
		UserID:      u.ID,
		Username:    u.Username,
		Roles:       u.RoleIDs,
		Permissions: s.userPermissions(u),
		TenantID:    "default", // 用户中心为平台级，统一 "default" 租户
		ExpiresAt:   time.Now().Add(jwtTokenExpiry),
	}
	return authctx.SignJWT(claims, s.jwtSecret)
}

// authResponse 登录/注册成功响应体。
type authResponse struct {
	Token string      `json:"token"`
	User  *store.User `json:"user"`
}

// ============================================================================
// 认证 handler：register / login / me
// ============================================================================

// handleAuthRegister 处理 POST /api/v1/auth/register：用户注册。
// 请求体：{username, password, email?}；密码最短 6 字符，bcrypt 哈希后存库。
// 成功返回 201 {token, user}；用户名重复返回 409。
func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// P1-4 限流：按客户端 IP 令牌桶约束注册频率，防滥用/枚举。
	if !s.loginGuard.allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests, slow down"})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Username == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	if len(body.Password) < minPasswordLen {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password too short (min 6 chars)"})
		return
	}
	// 用户名重复校验。
	if existing := s.store.GetUserByUsername(body.Username); existing != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "password hash failed: " + err.Error()})
		return
	}
	u := &store.User{
		ID:           randHexID("user"),
		Username:     body.Username,
		Email:        body.Email,
		PasswordHash: hash,
		Status:       "active",
		RoleIDs:      []string{"role-viewer"}, // 注册用户默认 viewer 角色
	}
	if s.store.CreateUser(u) == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	token, err := s.issueUserToken(u)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token sign failed: " + err.Error()})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: u.ID, Action: "user_register", Target: u.ID, Detail: "username=" + u.Username,
	})
	writeJSON(w, http.StatusCreated, authResponse{Token: token, User: u})
}

// handleAuthLogin 处理 POST /api/v1/auth/login：用户登录。
// 请求体：{username, password}；校验 bcrypt 哈希后签发 JWT。
// 成功返回 200 {token, user}；用户名不存在/密码错误返回 401。
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// P1-4 限流：按客户端 IP 令牌桶约束登录频率，防撞库与 DoS。
	if !s.loginGuard.allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests, slow down"})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Username == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	// P1-4 防爆破：账号处于锁定态时直接拒绝，避免继续尝试。
	if s.loginGuard.locked(body.Username) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "account temporarily locked due to too many failed attempts, try later"})
		return
	}
	u := s.store.GetUserByUsername(body.Username)
	if u == nil {
		// 用户名不存在也计入限流计数窗口（不暴露账号是否存在，同样走锁定逻辑防枚举）。
		s.loginGuard.recordFail(body.Username)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}
	if u.Status != "active" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "user disabled"})
		return
	}
	if !verifyPassword(u.PasswordHash, body.Password) {
		s.loginGuard.recordFail(body.Username)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}
	// 登录成功：清除失败计数（解锁）。
	s.loginGuard.resetFail(body.Username)
	token, err := s.issueUserToken(u)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token sign failed: " + err.Error()})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: u.ID, Action: "user_login", Target: u.ID, Detail: "username=" + u.Username,
	})
	writeJSON(w, http.StatusOK, authResponse{Token: token, User: u})
}

// handleAuthMe 处理 GET /api/v1/auth/me：返回当前登录用户信息。
// 从 Authorization: Bearer <token> 提取用户 ID，查库返回最新用户信息。
// 无 token / token 无效 / 用户不存在 → 401。
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	u, err := s.userFromToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// userFromToken 从请求的 Authorization: Bearer <token> 提取并验签 JWT，返回对应用户。
// 用于 /api/v1/auth/me 与用户管理 API 的鉴权。
// token 缺失/无效/用户不存在 → 返回 error。
func (s *Server) userFromToken(r *http.Request) (*store.User, error) {
	tokenStr, err := extractBearer(r)
	if err != nil {
		return nil, err
	}
	claims, err := authctx.ParseHSJWT(tokenStr, s.jwtSecret)
	if err != nil {
		return nil, err
	}
	u := s.store.GetUser(claims.UserID)
	if u == nil {
		return nil, errors.New("user not found")
	}
	return u, nil
}

// extractBearer 从 Authorization 头提取 Bearer token。
func extractBearer(r *http.Request) (string, error) {
	v := r.Header.Get("Authorization")
	if v == "" {
		return "", errors.New("missing Authorization header")
	}
	const prefix = "Bearer "
	if len(v) < len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return "", errors.New("Authorization header is not Bearer format")
	}
	token := strings.TrimSpace(v[len(prefix):])
	if token == "" {
		return "", errors.New("empty bearer token")
	}
	return token, nil
}

// requirePermission 鉴权中间件：从 token 提取用户，校验是否拥有指定权限。
// 返回 (user, ok)；ok=false 时已写入 401/403 响应，调用方应直接 return。
// 权限校验逻辑：展开用户经角色获得的权限，检查是否含 required 权限。
func (s *Server) requirePermission(w http.ResponseWriter, r *http.Request, required string) (*store.User, bool) {
	u, err := s.userFromToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return nil, false
	}
	perms := s.userPermissions(u)
	for _, p := range perms {
		if p == required {
			return u, true
		}
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "permission denied: " + required})
	return nil, false
}

// ============================================================================
// 用户管理 handler：/api/v1/users
// ============================================================================

// handleUsers 统一处理 /api/v1/users：
//   - GET：列出全部用户（需 user:read 权限）
//   - POST：创建用户（需 user:write 权限）
func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListUsers(w, r)
	case http.MethodPost:
		s.handleCreateUser(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListUsers 处理 GET /api/v1/users：列出全部用户（需 user:read 权限）。
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "user:read"); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"users": s.store.ListUsers()})
}

// handleCreateUser 处理 POST /api/v1/users：创建用户（需 user:write 权限）。
// 请求体：{username, password, email?, role_ids?}；密码最短 6 字符，bcrypt 哈希后存库。
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "user:write")
	if !ok {
		return
	}
	var body struct {
		Username string   `json:"username"`
		Password string   `json:"password"`
		Email    string   `json:"email"`
		RoleIDs  []string `json:"role_ids"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Username == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	if len(body.Password) < minPasswordLen {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password too short (min 6 chars)"})
		return
	}
	if s.store.GetUserByUsername(body.Username) != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "password hash failed: " + err.Error()})
		return
	}
	u := &store.User{
		ID:           randHexID("user"),
		Username:     body.Username,
		Email:        body.Email,
		PasswordHash: hash,
		Status:       "active",
		RoleIDs:      body.RoleIDs,
	}
	if s.store.CreateUser(u) == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "user_create", Target: u.ID, Detail: "username=" + u.Username,
	})
	writeJSON(w, http.StatusCreated, u)
}

// handleUserRouting 分派 /api/v1/users/{id} 子路径：
//   - PUT /api/v1/users/{id}：更新用户（需 user:write 权限）
//   - DELETE /api/v1/users/{id}：删除用户（需 user:delete 权限）
func (s *Server) handleUserRouting(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/users/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user id required"})
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateUser(w, r, id)
	case http.MethodDelete:
		s.handleDeleteUser(w, r, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleUpdateUser 处理 PUT /api/v1/users/{id}：更新用户 email/roles/status（需 user:write 权限）。
// 请求体：{email?, role_ids?, status?}；仅更新非空字段。
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "user:write")
	if !ok {
		return
	}
	var body struct {
		Email   string   `json:"email"`
		RoleIDs []string `json:"role_ids"`
		Status  string   `json:"status"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	existing := s.store.GetUser(id)
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if body.Email != "" {
		existing.Email = body.Email
	}
	if body.RoleIDs != nil {
		existing.RoleIDs = body.RoleIDs
	}
	if body.Status != "" {
		existing.Status = body.Status
	}
	if !s.store.UpdateUser(existing) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "user_update", Target: id, Detail: "updated via HTTP",
	})
	writeJSON(w, http.StatusOK, s.store.GetUser(id))
}

// handleDeleteUser 处理 DELETE /api/v1/users/{id}：删除用户（需 user:delete 权限）。
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "user:delete")
	if !ok {
		return
	}
	if s.store.GetUser(id) == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if !s.store.DeleteUser(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "user_delete", Target: id, Detail: "deleted via HTTP",
	})
	w.WriteHeader(http.StatusNoContent)
}

// ============================================================================
// 角色管理 handler：/api/v1/roles
// ============================================================================

// handleRoles 统一处理 /api/v1/roles：
//   - GET：列出全部角色（需 role:read 权限，但为简化前端展示，登录用户均可查看）
//   - POST：创建角色（需 role:write 权限）
func (s *Server) handleRoles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListRoles(w, r)
	case http.MethodPost:
		s.handleCreateRole(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListRoles 处理 GET /api/v1/roles：列出全部角色。
// 鉴权：仅需有效 token（登录用户均可查看角色列表，便于前端角色选择下拉框）。
func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	if _, err := s.userFromToken(r); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"roles": s.store.ListRoles()})
}

// handleCreateRole 处理 POST /api/v1/roles：创建角色（需 role:write 权限）。
// 请求体：{name, description, permissions[]}。
func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "role:write")
	if !ok {
		return
	}
	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role name is required"})
		return
	}
	role := &store.Role{
		ID:          randHexID("role"),
		Name:        body.Name,
		Description: body.Description,
		Permissions: body.Permissions,
	}
	if s.store.CreateRole(role) == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "role name already exists"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "role_create", Target: role.ID, Detail: "name=" + role.Name,
	})
	writeJSON(w, http.StatusCreated, role)
}

// handleRoleRouting 分派 /api/v1/roles/{id} 子路径：
//   - PUT /api/v1/roles/{id}：更新角色（需 role:write 权限）
//   - DELETE /api/v1/roles/{id}：删除角色（需 role:delete 权限）
func (s *Server) handleRoleRouting(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/roles/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role id required"})
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateRole(w, r, id)
	case http.MethodDelete:
		s.handleDeleteRole(w, r, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleUpdateRole 处理 PUT /api/v1/roles/{id}：更新角色 description/permissions（需 role:write 权限）。
func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "role:write")
	if !ok {
		return
	}
	var body struct {
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	existing := s.store.GetRole(id)
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "role not found"})
		return
	}
	if body.Description != "" {
		existing.Description = body.Description
	}
	if body.Permissions != nil {
		existing.Permissions = body.Permissions
	}
	if !s.store.UpdateRole(existing) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "role not found"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "role_update", Target: id, Detail: "updated via HTTP",
	})
	writeJSON(w, http.StatusOK, s.store.GetRole(id))
}

// handleDeleteRole 处理 DELETE /api/v1/roles/{id}：删除角色（需 role:delete 权限）。
func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "role:delete")
	if !ok {
		return
	}
	if s.store.GetRole(id) == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "role not found"})
		return
	}
	if !s.store.DeleteRole(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "role not found"})
		return
	}
	s.store.Audit(&proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "role_delete", Target: id, Detail: "deleted via HTTP",
	})
	w.WriteHeader(http.StatusNoContent)
}

// ============================================================================
// 权限查询 handler：/api/v1/permissions
// ============================================================================

// handlePermissions 处理 GET /api/v1/permissions：返回全部预定义权限。
// 鉴权：仅需有效 token（登录用户均可查看权限列表，便于前端权限选择）。
func (s *Server) handlePermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, err := s.userFromToken(r); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"permissions": s.store.ListPermissions()})
}
