package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// jwtClaims is the internal JWT claims structure.
//
// 字段契约与 controlplane internal/authctx.JWTClaims 对齐：
// sub/username/roles/permissions/tenant_id/exp/iat/jti + device_fp（设备指纹绑定）。
// 共享密钥模式下，controlplane 的 ParseHSJWT 可验签 auth-svc 签发的 token（反之亦然），
// 前提是两边使用相同的 JWT secret（通过配置注入）。
type jwtClaims struct {
	jwt.RegisteredClaims
	Username    string   `json:"username"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
	TenantID    string   `json:"tenant_id"`
	DeviceFP    string   `json:"device_fp,omitempty"` // 设备指纹 hash（防 token 跨设备重放）
}

// Claims represents the JWT claims for auth-svc.
type Claims struct {
	UserID      string
	Username    string
	Roles       []string
	Permissions []string
	TenantID    string
	DeviceFP    string // 设备指纹（非空时校验请求设备一致）
	JTI         string
	ExpiresAt   time.Time
}

// Engine handles JWT token issuance and validation.
type Engine struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewEngine creates a new JWT engine.
func NewEngine(secret string, accessTTL, refreshTTL time.Duration) *Engine {
	return &Engine{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

// IssueToken issues a JWT access token for the given user.
//
// 向后兼容封装：deviceFP 为空（不绑定设备）。新调用方应优先用 IssueTokenWithDeviceFP
// 显式传入设备指纹以启用跨设备重放防护。
func (e *Engine) IssueToken(userID, username string, roles, permissions []string) (string, int64, error) {
	return e.IssueTokenWithDeviceFP(userID, username, roles, permissions, "", e.accessTTL)
}

// IssueTokenWithDeviceFP issues a JWT access token bound to a device fingerprint.
//
// deviceFP 非空时写入 claims.device_fp，ValidateToken 返回该值供调用方校验请求设备一致；
// deviceFP 为空时不写入（omitempty），向后兼容旧客户端（不绑定设备）。
func (e *Engine) IssueTokenWithDeviceFP(userID, username string, roles, permissions []string, deviceFP string, ttl time.Duration) (string, int64, error) {
	if len(e.secret) == 0 {
		return "", 0, errors.New("auth: JWT secret is empty")
	}
	if ttl <= 0 {
		return "", 0, errors.New("auth: token TTL must be positive")
	}
	jti := randHex(16)
	expiresAt := time.Now().Add(ttl)
	claims := &jwtClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
		Username:    username,
		Roles:       roles,
		Permissions: permissions,
		TenantID:    "default",
		DeviceFP:    deviceFP,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(e.secret)
	if err != nil {
		return "", 0, fmt.Errorf("auth: failed to sign token: %w", err)
	}
	return signed, int64(ttl.Seconds()), nil
}

// IssueTokenWithTTL issues a JWT access token with a custom TTL.
// 用于改密专用短时效 token（如 mustChangePassword=true 首登场景的 5min token）：
// 与常规 token 同结构同签名（ValidateToken 可校验），仅有效期更短且由调用方
// 通过响应字段（MustChangePassword/ChangePasswordToken）区分用途，客户端不应用它访问受保护 API。
//
// 向后兼容封装：deviceFP 为空。新调用方应优先用 IssueTokenWithDeviceFP。
func (e *Engine) IssueTokenWithTTL(userID, username string, roles, permissions []string, ttl time.Duration) (string, int64, error) {
	return e.IssueTokenWithDeviceFP(userID, username, roles, permissions, "", ttl)
}

// ValidateToken validates a JWT token and returns the claims.
func (e *Engine) ValidateToken(tokenStr string) (*Claims, error) {
	if len(e.secret) == 0 {
		return nil, errors.New("auth: JWT secret is empty")
	}
	parserOpts := []jwt.ParserOption{jwt.WithValidMethods([]string{"HS256"})}
	claims := &jwtClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("auth: unexpected signing method %v", t.Header["alg"])
		}
		return e.secret, nil
	}, parserOpts...)
	if err != nil {
		return nil, fmt.Errorf("auth: token validation failed: %w", err)
	}
	if !token.Valid {
		return nil, errors.New("auth: token is invalid")
	}
	out := &Claims{
		UserID:      claims.Subject,
		Username:    claims.Username,
		Roles:       claims.Roles,
		Permissions: claims.Permissions,
		TenantID:    claims.TenantID,
		DeviceFP:    claims.DeviceFP,
		JTI:         claims.ID,
	}
	if claims.ExpiresAt != nil {
		out.ExpiresAt = claims.ExpiresAt.Time
	}
	return out, nil
}

// IssueRefreshToken generates a random refresh token.
func (e *Engine) IssueRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: failed to generate refresh token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashRefreshToken computes the SHA-256 hash of a refresh token.
func HashRefreshToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// RefreshTokenTTL returns the refresh token TTL.
func (e *Engine) RefreshTokenTTL() time.Duration {
	return e.refreshTTL
}

// AccessTokenTTL returns the access token TTL.
func (e *Engine) AccessTokenTTL() time.Duration {
	return e.accessTTL
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

// User represents a user in the auth system.
type User struct {
	ID                 string
	Username           string
	Email              string
	PasswordHash       string
	Status             string
	RoleIDs            []string
	CreatedAt          time.Time
	MustChangePassword bool
}
