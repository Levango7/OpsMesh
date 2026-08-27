// http_infra.go — HTTP 基础设施：请求体限制与租户上下文解析（防 DoS + 租户隔离）。
package controlplane

import (
	"opsmesh/internal/controlplane/paginate"
	"encoding/json"
	"net/http"
	"strings"

	"opsmesh/internal/authctx"
)

// maxBodyBytes 限制请求体大小（防 DoS：拒绝超大 body 直接 413，避免 JSON 解析拖垮内存）。
const maxBodyBytes = 1 << 20 // 1 MiB

// decodeJSONBody 在 MaxBytesReader 约束下解析 JSON 请求体（请求体大小限制）。
// 替换所有裸 json.NewDecoder(r.Body).Decode 调用，统一防超大请求体。
// 注意：仅做大小限制，不启用 DisallowUnknownFields，避免破坏前端多传字段的既有兼容行为。
func decodeJSONBody(w http.ResponseWriter, r *http.Request, v interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	return json.NewDecoder(r.Body).Decode(v)
}

// requireTenantContext 提取并校验网关注入的租户身份上下文（认证防御）。
//
// 行为矩阵（修复 1+2：增加 Bearer token 回退与交叉校验）：
//   - 头非空（X-Tenant-ID 已注入）：
//   - token 也携带 tenant_id 且一致 → 返回 actx, true
//   - token 也携带 tenant_id 但不一致 → 403 Forbidden（防绕过网关伪造租户头）
//   - token 无 tenant_id → 返回 actx, true（仅头注入，向后兼容）
//   - 头为空且 token 携带 tenant_id → 回退到 token 中的 tenant_id，返回 actx, true
//   - 头为空且 token 无 tenant_id 且 requireAuth=true → 401 Unauthorized
//   - 头为空且 token 无 tenant_id 且 requireAuth=false 且 demo=true → 自动填充 default/demo
//   - 头为空且 token 无 tenant_id 且 requireAuth=false 且 demo=false → 400 Bad Request
//
// 安全语义：Bearer token 中的 tenant_id 与 X-Tenant-ID 头交叉校验，防绕过网关伪造租户头；
// 头空时回退到 token，支持无网关直连场景（用户中心登录后直接访问 API）。
// 调用方应在 ok=false 时直接 return（响应已写入）。
func (s *Server) requireTenantContext(w http.ResponseWriter, r *http.Request) (authctx.Context, bool) {
	actx := authctx.FromHTTPHeader(r.Header)
	// 修复 1+2：从 Bearer token/Cookie 提取 tenant_id 作为回退/交叉校验。
	tokenTenant, tokenUser := s.tenantFromBearer(r)
	if actx.TenantID != "" {
		// 头非空：若 token 也携带 tenant_id，校验两者一致，防绕过网关伪造租户头。
		if tokenTenant != "" && tokenTenant != actx.TenantID {
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch between X-Tenant-ID header and JWT claims"})
			return actx, false
		}
		return actx, true
	}
	// 头空：回退到 token 中的 tenant_id（支持无网关直连场景）。
	if tokenTenant != "" {
		actx.TenantID = tokenTenant
		if actx.UserID == "" && tokenUser != "" {
			actx.UserID = tokenUser
		}
		return actx, true
	}
	if s.requireAuth {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return actx, false
	}
	if s.cfg != nil && s.cfg.Demo {
		// demo 模式放宽：未携带身份头时填充默认租户/用户，便于本地一键体验。
		actx.TenantID = "default"
		if actx.UserID == "" {
			actx.UserID = "demo"
		}
		return actx, true
	}
	// 非生产非 demo 模式：拒绝空租户头，防越权伪造。
	paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing X-Tenant-ID header (tenant context required)"})
	return actx, false
}

// tenantFromBearer 从 Authorization: Bearer <token> 或 HttpOnly Cookie 中提取 tenant_id/user_id。
// 用于 requireTenantContext 的 token 回退与交叉校验（修复 1+2）。
// token 缺失/无效时返回空串（不阻断，由调用方决定后续行为）。
func (s *Server) tenantFromBearer(r *http.Request) (tenantID, userID string) {
	tokenStr, err := extractBearer(r)
	if err != nil {
		// 回退 HttpOnly Cookie（与 userFromToken 一致，双 Cookie 方案）。
		if ck, ckErr := r.Cookie(accessTokenCookieName); ckErr == nil && strings.TrimSpace(ck.Value) != "" {
			tokenStr = ck.Value
		} else {
			return "", ""
		}
	}
	claims, err := authctx.ParseHSJWT(tokenStr, s.jwtSecret)
	if err != nil {
		return "", ""
	}
	return claims.TenantID, claims.UserID
}
