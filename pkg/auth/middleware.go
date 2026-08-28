// HTTP 中间件：JWT 校验、权限检查、服务间认证。
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// authContextKey 用于在 request context 中存放已校验的 ServiceClaims。
type authContextKey int

const authenticatedClaimsKey authContextKey = iota

// ErrMissingToken 表示请求未携带 Authorization Bearer token。
var ErrMissingToken = errors.New("auth: 未携带 Authorization Bearer token")

// ErrInvalidToken 表示 token 校验失败。
var ErrInvalidToken = errors.New("auth: token 无效或已过期")

// AuthMiddleware 从 Authorization: Bearer <token> 提取并校验 JWT，
// 校验通过后把 ServiceClaims 注入 request context。
func AuthMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := extractBearer(r.Header)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			claims, err := ValidateServiceToken(token, secret)
			if err != nil {
				http.Error(w, ErrInvalidToken.Error(), http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), authenticatedClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequirePermission 检查经过 AuthMiddleware 的请求是否具备指定权限。
// 必须在 AuthMiddleware 之后调用（依赖 context 中的 ServiceClaims）。
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(authenticatedClaimsKey).(*ServiceClaims)
			if !ok || claims == nil {
				http.Error(w, "auth: 未认证", http.StatusUnauthorized)
				return
			}
			if !hasPermission(claims.Permissions, permission) {
				http.Error(w, "auth: 权限不足", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ServiceAuthMiddleware 专用于服务间调用：校验 JWT 并验证 service_id 非空。
// 比 AuthMiddleware 更严格，要求 token 必须包含有效 service 身份。
func ServiceAuthMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := extractBearer(r.Header)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			claims, err := ValidateServiceToken(token, secret)
			if err != nil {
				http.Error(w, ErrInvalidToken.Error(), http.StatusUnauthorized)
				return
			}
			if claims.ServiceID == "" {
				http.Error(w, "auth: 服务身份缺失", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), authenticatedClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromRequest 从经过 AuthMiddleware 的 request context 提取 ServiceClaims。
func ClaimsFromRequest(r *http.Request) (*ServiceClaims, bool) {
	claims, ok := r.Context().Value(authenticatedClaimsKey).(*ServiceClaims)
	return claims, ok
}

// extractBearer 从 Authorization 头提取 Bearer token。
func extractBearer(h http.Header) (string, error) {
	v := h.Get("Authorization")
	if v == "" {
		return "", ErrMissingToken
	}
	const prefix = "Bearer "
	if len(v) < len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return "", errors.New("auth: Authorization 头非 Bearer 格式")
	}
	token := strings.TrimSpace(v[len(prefix):])
	if token == "" {
		return "", ErrMissingToken
	}
	return token, nil
}

// hasPermission 检查权限列表是否包含指定权限。
func hasPermission(perms []string, target string) bool {
	for _, p := range perms {
		if p == target {
			return true
		}
	}
	return false
}
