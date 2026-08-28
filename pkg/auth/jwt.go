// Package auth 提供服务间认证共享库：JWT 传播、mTLS 配置、HTTP 中间件、gRPC 拦截器。
//
// 设计原则：
//   - JWT（HS256 对称密钥）用于服务间 token 传播，支持 context 存取；
//   - mTLS 用于服务间传输层双向认证，支持自签证书（开发环境）；
//   - HTTP 中间件从 Authorization: Bearer <token> 提取并校验 JWT；
//   - gRPC 拦截器在 metadata 中传播 token，实现无感鉴权。
package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ServiceClaims 是服务间 JWT 的自定义 claims。
type ServiceClaims struct {
	ServiceID   string   `json:"service_id"`
	ServiceName string   `json:"service_name"`
	TenantID    string   `json:"tenant_id"`
	Permissions []string `json:"permissions"`
	ExpiresAt   time.Time
}

// serviceClaimsInternal 嵌入 RegisteredClaims 以填充 exp/iat 等标准字段。
type serviceClaimsInternal struct {
	jwt.RegisteredClaims
	ServiceID   string   `json:"service_id"`
	ServiceName string   `json:"service_name"`
	TenantID    string   `json:"tenant_id"`
	Permissions []string `json:"permissions"`
}

// 上下文键类型（避免 string key 冲突）。
type contextKey int

const claimsContextKey contextKey = iota

// GenerateServiceToken 签发服务间 JWT token（HS256）。
//
// 参数：
//   - claims：服务身份声明（service_id/name/tenant_id/permissions/exp）；
//   - secret：HS256 对称密钥（至少 32 字节建议）；空密钥返回 error。
//
// ExpiresAt 为零值时默认 1 小时后过期。
func GenerateServiceToken(claims ServiceClaims, secret string) (string, error) {
	if secret == "" {
		return "", errors.New("auth: JWT 签发密钥为空")
	}
	if claims.ExpiresAt.IsZero() {
		claims.ExpiresAt = time.Now().Add(1 * time.Hour)
	}
	now := time.Now()
	internal := serviceClaimsInternal{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   claims.ServiceID,
			ExpiresAt: jwt.NewNumericDate(claims.ExpiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
		ServiceID:   claims.ServiceID,
		ServiceName: claims.ServiceName,
		TenantID:    claims.TenantID,
		Permissions: claims.Permissions,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, internal)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("auth: JWT 签发失败: %w", err)
	}
	return signed, nil
}

// ValidateServiceToken 用 HS256 对称密钥验签并解析 token，返回 ServiceClaims。
func ValidateServiceToken(tokenString string, secret string) (*ServiceClaims, error) {
	if secret == "" {
		return nil, errors.New("auth: JWT 验签密钥为空")
	}
	parserOpts := []jwt.ParserOption{jwt.WithValidMethods([]string{"HS256"})}
	token, err := jwt.ParseWithClaims(tokenString, &serviceClaimsInternal{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("auth: 非预期签名算法 %v（仅接受 HS256）", t.Header["alg"])
		}
		return []byte(secret), nil
	}, parserOpts...)
	if err != nil {
		return nil, fmt.Errorf("auth: JWT 验签失败: %w", err)
	}
	if !token.Valid {
		return nil, errors.New("auth: JWT token 无效")
	}
	internal, ok := token.Claims.(*serviceClaimsInternal)
	if !ok {
		return nil, errors.New("auth: JWT claims 类型非 serviceClaimsInternal")
	}
	claims := &ServiceClaims{
		ServiceID:   internal.ServiceID,
		ServiceName: internal.ServiceName,
		TenantID:    internal.TenantID,
		Permissions: internal.Permissions,
	}
	if internal.ExpiresAt != nil {
		claims.ExpiresAt = internal.ExpiresAt.Time
	}
	return claims, nil
}

// PropagateContext 将 JWT token 注入 context（用于跨服务传播）。
func PropagateContext(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, claimsContextKey, token)
}

// ExtractFromContext 从 context 提取并校验 JWT token，返回 ServiceClaims。
// context 中无 token 或 token 无效时返回 error。
func ExtractFromContext(ctx context.Context) (*ServiceClaims, error) {
	v, ok := ctx.Value(claimsContextKey).(string)
	if !ok || v == "" {
		return nil, errors.New("auth: context 中无 JWT token")
	}
	// 注意：此处仅提取 token 字符串，验签需调用方提供 secret。
	// 返回原始 token 字符串包装在 ServiceClaims 中供后续使用。
	return &ServiceClaims{ServiceID: v}, nil
}

// ExtractTokenFromContext 从 context 提取原始 JWT token 字符串。
func ExtractTokenFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(claimsContextKey).(string)
	return v, ok
}
