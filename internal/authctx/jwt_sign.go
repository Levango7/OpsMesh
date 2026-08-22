// jwt_sign.go 实现 JWT 签发能力（HS256）。
//
// 与 authctx.go 中已有的 JWT 验签（RS256，网关公钥）互补：
//   - authctx.go：验签网关签发的 token（RS256，公钥验签），用于"网关注入 + 内核二次校验"；
//   - jwt_sign.go：内核自行签发 token（HS256，对称密钥），用于用户中心登录/注册后下发 token。
//
// 选择 HS256（而非 RS256）的理由：
//   - 单密钥即可签发+验签，部署简单（无需管理 RSA 密钥对）；
//   - 用户中心是内核自有能力，密钥不外发，对称密钥足够安全；
//   - 网关场景用 RS256 是因为签发方（网关）与验签方（内核）分离，需非对称密钥。
//
// token payload 包含：sub（用户 ID）、username、roles、permissions、tenant_id、exp、iat。
package authctx

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims 内核签发 JWT 的自定义 claims。
// 与 jwt/v5 的 jwt.RegisteredClaims 互补，承载用户中心业务字段。
type JWTClaims struct {
	UserID      string    // sub：用户 ID
	Username    string    // username：用户名
	Roles       []string  // roles：角色 ID 列表
	Permissions []string  // permissions：权限字符串列表（展开后的最终权限）
	TenantID    string    // tenant_id：租户 ID（多租户隔离键）
	JTI         string    // jti：JWT 唯一 ID（用于吊销/blacklist）
	ExpiresAt   time.Time // exp：过期时间
}

// jwtSigner 内部 claims 结构，嵌入 jwt.RegisteredClaims 以填充 sub/exp/iat 等标准字段。
type jwtSigner struct {
	jwt.RegisteredClaims
	Username    string   `json:"username"`    // 用户名
	Roles       []string `json:"roles"`       // 角色 ID 列表
	Permissions []string `json:"permissions"` // 权限字符串列表
	TenantID    string   `json:"tenant_id"`   // 租户 ID
}

// SignJWT 签发 JWT token（HS256）。
//
// 参数：
//   - claims：业务 claims（用户 ID/用户名/角色/权限/租户/过期时间）；
//   - secret：HS256 对称密钥（至少 32 字节建议）；空密钥返回 error。
//
// 返回：签发后的 token 字符串（紧凑格式，Header.Payload.Signature）。
//
// 安全注意：
//   - secret 为空返回 error（防配置遗漏导致任何人可伪造 token）；
//   - ExpiresAt 为零值时默认 24h 后过期（避免永不过期的 token 泄露后无法失效）；
//   - iat 自动填充当前时间。
func SignJWT(claims JWTClaims, secret []byte) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("authctx: JWT 签发密钥为空（未配置）")
	}
	if claims.ExpiresAt.IsZero() {
		claims.ExpiresAt = time.Now().Add(24 * time.Hour)
	}
	// JWT 吊销：为每个 token 生成唯一 jti（JWT ID），登出时加入 blacklist。
	// 调用方未填 JTI 时用 crypto/rand 生成 16 字节 hex（32 字符，碰撞概率可忽略）。
	if claims.JTI == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("authctx: 生成 JWT jti 失败: %w", err)
		}
		claims.JTI = hex.EncodeToString(b)
	}
	now := time.Now()
	signer := jwtSigner{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        claims.JTI, // 标准 JWT jti 字段，用于吊销
			Subject:   claims.UserID,
			ExpiresAt: jwt.NewNumericDate(claims.ExpiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
		Username:    claims.Username,
		Roles:       claims.Roles,
		Permissions: claims.Permissions,
		TenantID:    claims.TenantID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, signer)
	signed, err := token.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("authctx: JWT 签发失败: %w", err)
	}
	return signed, nil
}

// ParseHSJWT 用 HS256 对称密钥验签并解析 token，返回业务 claims。
//
// 用于内核自签发自验签场景（用户中心登录后下发 token，后续请求携带 token 鉴权）。
// 与 authctx.go 的 FromJWT（RS256 网关公钥验签）区分：
//   - FromJWT：验签网关签发的 token（RS256，非对称）；
//   - ParseHSJWT：验签内核签发的 token（HS256，对称）。
//
// 验签失败、过期、密钥不匹配 → 返回 error。
func ParseHSJWT(tokenStr string, secret []byte) (JWTClaims, error) {
	if len(secret) == 0 {
		return JWTClaims{}, errors.New("authctx: JWT 验签密钥为空（未配置）")
	}
	parserOpts := []jwt.ParserOption{jwt.WithValidMethods([]string{"HS256"})}
	token, err := jwt.ParseWithClaims(tokenStr, &jwtSigner{}, func(t *jwt.Token) (interface{}, error) {
		// 双重保险：仅接受 HS256（防 alg=none 降级攻击）。
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("authctx: 非预期签名算法 %v（仅接受 HS256）", t.Header["alg"])
		}
		return secret, nil
	}, parserOpts...)
	if err != nil {
		return JWTClaims{}, fmt.Errorf("authctx: JWT 验签失败: %w", err)
	}
	if !token.Valid {
		return JWTClaims{}, errors.New("authctx: JWT token 无效")
	}
	signer, ok := token.Claims.(*jwtSigner)
	if !ok {
		return JWTClaims{}, errors.New("authctx: JWT claims 类型非 jwtSigner")
	}
	out := JWTClaims{
		UserID:      signer.Subject,
		Username:    signer.Username,
		Roles:       signer.Roles,
		Permissions: signer.Permissions,
		TenantID:    signer.TenantID,
		JTI:         signer.ID, // 标准 JWT jti 字段，用于吊销校验
	}
	if signer.ExpiresAt != nil {
		out.ExpiresAt = signer.ExpiresAt.Time
	}
	return out, nil
}
