// http_infra.go — HTTP 基础设施：请求体限制与租户上下文解析（防 DoS + 租户隔离）。
package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/authctx"
	"opsmesh/internal/logx"
)

// maxBodyBytes 限制请求体大小（防 DoS：拒绝超大 body 直接 413，避免 JSON 解析拖垮内存）。
const maxBodyBytes = 1 << 20 // 1 MiB

// ============================================================================
// 错误信息脱敏（防内部信息泄露）
// ============================================================================

// internalErrorBody 内部错误对外暴露的固定文案。
// store/db/k8s/内部组件返回的 err 可能包含表名、SQL 片段、文件路径、集群内部地址等，
// 直接回吐客户端等于把部署拓扑交给攻击者，因此统一替换为该固定文案。
const internalErrorBody = "internal server error"

// writeInternalError 内部错误统一出口（500）。
//
// 语义：原始 err 只写入服务端日志（带 traceID，保留排障能力），客户端仅收到固定
// 脱敏文案。HTTP 状态码与响应字段名 {"error": ...} 保持不变，不破坏前端与既有测试。
//
// op 为排障定位用的操作名（如 "k8s.listPods"），仅进日志、不进响应。
func writeInternalError(ctx context.Context, w http.ResponseWriter, op string, err error) {
	logx.Error(ctx, "internal error: "+op, err)
	paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": internalErrorBody})
}

// writeSanitizedError 指定状态码 + 指定脱敏文案的错误出口。
//
// 用于「错误来自内部组件，但业务契约上以 4xx/502 表达」的端点：保持既有状态码不变
// （不破坏测试），仅去掉响应体中的内部细节，原始 err 进服务端日志。
func writeSanitizedError(ctx context.Context, w http.ResponseWriter, status int, op, clientMsg string, err error) {
	logx.Error(ctx, "sanitized error: "+op, err)
	paginate.WriteJSON(w, status, map[string]string{"error": clientMsg})
}

// decodeJSONBody 在 MaxBytesReader 约束下解析 JSON 请求体（请求体大小限制）。
// 替换所有裸 json.NewDecoder(r.Body).Decode 调用，统一防超大请求体。
// 注意：仅做大小限制，不启用 DisallowUnknownFields，避免破坏前端多传字段的既有兼容行为。
func decodeJSONBody(w http.ResponseWriter, r *http.Request, v interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	return json.NewDecoder(r.Body).Decode(v)
}

// requireTenantContext 提取并校验网关注入的租户身份上下文（认证防御）。
//
// 行为矩阵（修复 1+2 + CI E2E-sec 越权漏洞真修）：
//   - 头非空（X-Tenant-ID 已注入）：
//   - token 也携带 tenant_id 且一致 → 返回 actx, true
//   - token 也携带 tenant_id 但不一致 → 403 Forbidden（防绕过网关伪造租户头）
//   - 无 token 且 requireAuth=true 且未开 --trust-gateway-headers → 401（防只伪造头冒充租户）
//   - 无 token 且（requireAuth=false 或显式 --trust-gateway-headers=true）→ 返回 actx, true
//   - 头为空且 token 携带 tenant_id → 回退到 token 中的 tenant_id，返回 actx, true
//   - 头为空且 token 无 tenant_id 且 requireAuth=true → 401 Unauthorized
//   - 头为空且 token 无 tenant_id 且 requireAuth=false 且 demo=true → 自动填充 default/demo
//   - 头为空且 token 无 tenant_id 且 requireAuth=false 且 demo=false → 400 Bad Request
//
// 安全语义：
//   - Bearer token 中的 tenant_id 与 X-Tenant-ID 头交叉校验，防绕过网关伪造租户头；
//   - **头非空但无任何凭证**：requireAuth 开启时拒绝（CI E2E-sec 实测捕获的越权——
//     此前该分支直接放行，攻击者只发 X-Tenant-ID: victim 头即可冒充任意租户。
//     信任边界：requireAuth=true 意味着"所有请求必须携带可验证身份"（网关注入头
//     须由可信网关剥离重发+配合网关层认证，或直接走 Bearer token）；requireAuth=false
//     保留头直通（内网信任环境原有语义，由部署方自行保证网关前置）。
//
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
		// 头非空但无凭证（无 token/cookie）——CI E2E-sec 实测捕获的越权漏网分支：
		// 此前直接放行，攻击者只发 X-Tenant-ID: victim 头即可冒充任意租户。
		// 处置：requireAuth 下默认拒绝；仅当部署方显式 --trust-gateway-headers=true
		// （声明有可信网关认证后剥离凭证只留头转发，即 README IAM 路径 B）才放行。
		// requireAuth=false 保留内网头直通（部署方自行保证前置）。
		if tokenTenant == "" && s.requireAuth && !(s.cfg != nil && s.cfg.TrustGatewayHeaders) {
			paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "identity header without verifiable credential (require-auth; enable --trust-gateway-headers if behind a trusted gateway)"})
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
