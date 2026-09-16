// auth.go 实现用户中心的核心鉴权 helper 与中间件。
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
//
// 文件拆分（按 handler 域）：
//   - auth.go：类型定义 + 常量 + 核心认证入口（requirePermission/requireProd/authorizeByRoles/subsystemAuthorize）；
//   - auth_cookies.go：Cookie 管理；
//   - auth_tokens.go：刷新/改密/JWT 令牌管理 + 鉴权中间件（userFromToken/extractBearer/hasUserIdentityToken）；
//   - auth_password.go：密码哈希 + 默认 admin 弱口令轮换 + 强口令校验；
//   - auth_guard.go：登录守卫（loginGuard）+ clientIP + deviceFingerprint；
//   - auth_apikey.go：API Key 认证 + 用户权限展开 + 角色权限缓存；
//   - auth_login.go：注册/登录/登出/刷新/当前用户/改密 handler；
//   - auth_users.go：用户管理 handler（CRUD + 审批/拒绝）；
//   - auth_roles.go：角色管理 handler（CRUD）；
//   - auth_perms.go：权限查询 handler。
package controlplane

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/authctx"
	"opsmesh/internal/store"
)

// 双 HttpOnly Cookie 令牌方案（同源最简单且安全）：
//   - at（access token）：短期 JWT（15min），仅标识身份，XSS 窃取后利用窗口极小；
//   - rt（refresh token）：长期不透明随机串（7d），服务端可吊销/旋转，用于静默续期。
//
// 两者均为 HttpOnly + SameSite=Lax（防 XSS 读取 / 防 CSRF 跨站携带），同源由浏览器自动携带。
const (
	accessTokenCookieName  = "opsmesh_at"
	refreshTokenCookieName = "opsmesh_rt"
	accessTokenExpiry      = 15 * time.Minute
	refreshTokenExpiry     = 7 * 24 * time.Hour
)

// changePasswordTokenExpiry 改密令牌有效期（5 分钟）。
const changePasswordTokenExpiry = 5 * time.Minute

// bcryptCost bcrypt 哈希 cost（生产推荐基线 12，DefaultCost=10 偏低）。
const bcryptCost = 12

// changePasswordMinLen 改密新密码最短长度（强口令基线：8 字符）。
const changePasswordMinLen = 8

// apikeyPrefix API Key 明文前缀：platform.GenerateAPIKey 生成的 "om_" + 32 位随机 hex
// （共 35 字符）。前缀用于 requireProd 分发识别，避免 API Key 被误送 JWT 验签。
const apikeyPrefix = "om_"

// ============================================================================
// loginGuard 登录/注册防爆破 + 限流。
// ============================================================================

const (
	loginRateBurst  = 5                // 令牌桶容量（瞬时允许的最大尝试数）；收紧自 10，强化撞库防护
	loginRateRefill = 1.0 / 6.0        // 令牌补充速率（每秒），约每 6s 1 个，≈10/min；收紧自 1/3（≈20/min）
	loginMaxFails   = 5                // 单账号允许的连续失败次数
	loginFailWindow = 15 * time.Minute // 失败计数滑动窗口
	loginLockDur    = 15 * time.Minute // 账号锁定时长
)

type loginGuard struct {
	mu    sync.Mutex
	ips   map[string]*rateRec // 客户端 IP -> 限流令牌桶（进程内，多副本各自限流）
	done  chan struct{}       // stopSweep 关闭此 chan 通知 sweep goroutine 退出
	store store.SessionStore  // 失败计数 + 账号锁定经 SessionStore 共享
}

type rateRec struct {
	tokens float64
	last   time.Time
}

// authResponse 登录/注册成功响应体。
// MustChangePassword（安全债）：当用户首登须改密时为 true，前端据此弹出改密对话框。
// ChangePasswordToken：MustChangePassword=true 时签发的一次性短时效 token（5min），
// 仅用于 /api/v1/auth/change-password；此时不下发 access token（Token 字段为空、不下发 at Cookie），
// 改密成功后才签发正式 at + rt。
type authResponse struct {
	Token               string      `json:"token"`
	User                *store.User `json:"user"`
	MustChangePassword  bool        `json:"mustChangePassword"`
	ChangePasswordToken string      `json:"changePasswordToken,omitempty"`
}

// ============================================================================
// 核心鉴权中间件：requirePermission / requireProd / authorizeByRoles / subsystemAuthorize
// ============================================================================

// subsystemAuthorize 供 CMDB/部署/日志/编排等子包 handler 注入的统一鉴权回调（G1 鉴权修复）。
// 组合 requireTenantContext（租户上下文：头/Token 交叉校验，requireAuth 缺失租户 401）
// + requireProd（产品级 RBAC 权限闸：JWT/API Key/网关注入角色/联邦/demo）。
// perm 由各 handler 按方法语义传入（如 "cmdb:read" / "cmdb:write"）；
// 未认证/无权限时已写入响应并返回 ok=false，调用方应直接 return。
func (s *Server) subsystemAuthorize(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return actx, false
	}
	if _, ok := s.requireProd(w, r, perm); !ok {
		return actx, false
	}
	return actx, true
}

// requirePermission 鉴权中间件：从 token 提取用户，校验是否拥有指定权限。
// 返回 (user, ok)；ok=false 时已写入 401/403 响应，调用方应直接 return。
// 权限校验逻辑：展开用户经角色获得的权限，检查是否含 required 权限。
func (s *Server) requirePermission(w http.ResponseWriter, r *http.Request, required string) (*store.User, bool) {
	u, err := s.userFromToken(r)
	if err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return nil, false
	}
	// 强制改密：标记 MustChangePassword 的用户只能访问 /api/v1/auth/change-password
	// （该端点走 userFromToken，不经此处），其余受保护 API 一律拒绝，避免弱口令长期在线。
	if u.MustChangePassword {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "password change required (MUST_CHANGE_PASSWORD)"})
		return nil, false
	}
	perms := s.userPermissions(u)
	for _, p := range perms {
		if p == required {
			return u, true
		}
	}
	paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "permission denied: " + required})
	return nil, false
}

// requireProd 统一产品级 RBAC 鉴权闸：在 requireTenantContext（租户隔离）之后调用，
// 校验当前身份是否拥有 required 权限。兼容两种身份来源：
//   - 联邦入站（X-Federation-Forwarded=1）：必须经 verifyFederationRequest 验签 HMAC 通过，
//     信任来自可信控制面 peer 的请求（用户级 RBAC 已在来源控制面执行）；验签失败 → 403。
//   - Authorization: Bearer om_*（API Key 明文，H5 接入）或 X-API-Key 头非空：
//     走 authorizeByAPIKey（ValidateKey hash 比对 + 租户交叉校验 + HasScope）。该分支必须
//     先于 JWT 解析识别 om_ 前缀：API Key 明文形如 om_<32位hex>，若落入 JWT 路径会被
//     ParseHSJWT 当作非法 token 拒绝（401 语义混淆 + 浪费验签开销）。
//   - Authorization: Bearer（JWT，或 opsmesh_at Cookie）：走 JWT 路径（requirePermission）。
//   - 网关注入 X-User-Roles（角色名）：仅当 cfg.TrustGatewayHeaders=true 时才信任该头并展开为权限
//     集合后校验（authorizeByRoles）；否则该头被忽略，继续走 demo/拒绝路径。生产模式强制 false
//     （见 config.go），杜绝客户端自称 admin 即得 admin 权限的越权路径。
//   - demo 模式且无任何身份头：放行，保持本地一键体验的宽松语义（与 requireTenantContext 一致）。
//   - 其余：401。
//
// 返回 (user, ok)；ok=false 时已写入响应，调用方应直接 return。
func (s *Server) requireProd(w http.ResponseWriter, r *http.Request, required string) (*store.User, bool) {
	// 1. 联邦入站：必须验签 HMAC 通过才信任 peer（用户 RBAC 已在来源侧执行）。
	//    原实现仅判断头存在即放行，未验签，任意客户端伪造 X-Federation-Forwarded=1 即可绕过 RBAC。
	if r.Header.Get("X-Federation-Forwarded") == "1" {
		if err := s.verifyFederationRequest(r); err != nil {
			log.Printf("controlplane: requireProd 联邦验签失败: %v", err)
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "federation signature verification failed"})
			return nil, false
		}
		return nil, true
	}
	// 2.5. API Key 路径（H5 认证接入）：Authorization 以 "Bearer om_" 开头 或 X-API-Key 头非空。
	//    必须先于下方 Bearer JWT 解析执行 om_ 前缀识别（原因见函数注释）；X-API-Key 头在
	//    H5 前处于"有钥匙孔无锁"状态（无人处理），现统一收口到 authorizeByAPIKey 完成认证+授权。
	if key, ok := extractAPIKey(r); ok {
		return s.authorizeByAPIKey(w, r, required, key)
	}
	// 2. JWT Bearer / Cookie 路径：用户中心登录后携带的 token。
	auth := r.Header.Get("Authorization")
	hasBearer := strings.HasPrefix(auth, "Bearer ")
	hasCookie := false
	if ck, err := r.Cookie(accessTokenCookieName); err == nil && strings.TrimSpace(ck.Value) != "" {
		hasCookie = true
	}
	if hasBearer || hasCookie {
		return s.requirePermission(w, r, required)
	}
	// 3. 网关注入路径：仅当显式开启 TrustGatewayHeaders 时才信任 X-User-Roles 头。
	//    安全加固：默认 false（生产模式强制 false），防客户端伪造 X-User-Roles: admin 越权。
	//    若未开启则该头被忽略，继续向下走 demo 模式或拒绝路径。
	if s.cfg != nil && s.cfg.TrustGatewayHeaders && strings.TrimSpace(r.Header.Get("X-User-Roles")) != "" {
		return s.authorizeByRoles(w, r, required)
	}
	// 4. demo 模式宽松放行（无身份，自动填充 default/demo）。
	if s.cfg != nil && s.cfg.Demo {
		return nil, true
	}
	// 5. 无可用身份 → 拒绝。
	paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity (no bearer token or gateway role header)"})
	return nil, false
}

// authorizeByRoles 将网关注入/联邦转发的角色名展开为权限集合并校验 required。
func (s *Server) authorizeByRoles(w http.ResponseWriter, r *http.Request, required string) (*store.User, bool) {
	roleNames := authctx.FromHTTPHeader(r.Header).Roles
	if len(roleNames) == 0 {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "no roles in identity context"})
		return nil, false
	}
	// 动态查询角色权限映射，保证管理员修改角色权限后立即生效（无陈旧缓存）。
	rolePerms := getRolePermCache()
	for _, rn := range roleNames {
		for _, p := range rolePerms[rn] {
			if p == required {
				return nil, true
			}
		}
	}
	paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "permission denied: " + required})
	return nil, false
}
