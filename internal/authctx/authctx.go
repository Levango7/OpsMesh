// Package authctx 定义"网关注入的身份上下文"的提取与校验。
//
// 设计原则（等保三级 + 复用底座，非自研登录）：
//   - 内核（控制面）【不】自行实现登录 / 鉴权 / 用户表 / 密码哈希；
//   - 登录、SSO、MFA、RBAC 由前置网关（APISIX / 蓝鲸 IAM）完成；
//   - 网关校验 JWT/OIDC 后，把身份注入到请求头 / gRPC metadata：
//     X-Tenant-ID  / x-tenant-id   —— 租户（行级隔离键）
//     X-User-Id   / x-user-id     —— 用户（审计留痕）
//     X-User-Roles/ x-user-roles  —— 角色（逗号分隔，垂直越权防护辅助）
//   - 内核只消费这些头，并据此做"行级租户隔离"与"审计留痕"。
//
// 缺失头时（开发 / 单机模式，无网关）视为单一隐式租户，放行全部——
// 这是 MVP 单租户（多团队逻辑隔离）的合理降级，不是越权。
//
// JWT 验签（可选启用）：当配置了网关 RSA 公钥时，FromRequest 强制从
// Authorization: Bearer <token> 提取并 RS256 验签，从 claims 取 tenant_id/
// user_id/user_roles，作为"网关注入 + 内核二次校验"的纵深防御。
// 启用 JWT 验签时必须携带有效 token，未携带或验签失败均返回错误（401），
// 不再回退到头注入模式以防越权。未配置公钥时才回退到头注入模式（向后兼容）。
package authctx

import (
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/metadata"
)

// Context 是网关注入的身份上下文。
type Context struct {
	TenantID string
	UserID   string
	Roles    []string
}

const (
	hdrTenant = "x-tenant-id"
	hdrUser   = "x-user-id"
	hdrRoles  = "x-user-roles"
)

// FromHTTPHeader 从 HTTP 头提取身份上下文（前置网关已校验 JWT 并注入）。
//
// 安全警告：本函数不校验头来源真实性，仅机械读取 X-Tenant-ID 等头。
// 生产环境必须在可信网关（APISIX/Envoy/蓝鲸 IAM）后部署，网关负责：
//   - 校验调用方 JWT/OIDC 后剥离客户端自带的 X-Tenant-ID，再重注入经鉴权的真实租户；
//   - 拒绝直连控制面（绕过网关）的请求（网络策略 / mTLS 双向认证）。
//
// 直接暴露控制面将允许任意客户端伪造租户头越权（任意声明属于任何租户），
// 故 --require-auth=true 时控制面会拒绝缺失租户头的请求（见 server.go / grpc.go）。
func FromHTTPHeader(h http.Header) Context {
	c := Context{
		TenantID: h.Get(hdrTenant),
		UserID:   h.Get(hdrUser),
	}
	c.Roles = splitRoles(h.Get(hdrRoles))
	return c
}

// FromGRPCMetadata 从 gRPC metadata 提取身份上下文（网关 / mTLS 终止点注入）。
func FromGRPCMetadata(md metadata.MD) Context {
	get := func(k string) string {
		if v := md.Get(k); len(v) > 0 {
			return v[0]
		}
		return ""
	}
	c := Context{
		TenantID: get(hdrTenant),
		UserID:   get(hdrUser),
	}
	c.Roles = splitRoles(get(hdrRoles))
	return c
}

func splitRoles(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// BelongsTo 判断资源所属租户是否与当前上下文匹配。
// 空 tenantID 表示“无网关 / 开发模式”，放行全部（不强制隔离）。
func (c Context) BelongsTo(resourceTenant string) bool {
	if c.TenantID == "" {
		return true
	}
	return c.TenantID == resourceTenant
}

// HasRole 判断上下文是否含指定角色。
// 垂直越权防护由网关 RBAC 拦截器主力完成，此为内核侧辅助判断。
func (c Context) HasRole(role string) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// ============================================================================
// JWT 验签（网关公钥 RS256）
// ============================================================================

// JWTConfig 是 JWT 验签的可选配置，由调用方（如 server.go）从 config.Config 装配。
//
// 启用语义：Enabled=true 且 PublicKey!=nil 时 FromRequest 走 JWT 验签路径；
// 否则回退到 FromHTTPHeader 头注入模式（MVP 兼容）。
type JWTConfig struct {
	PublicKey *rsa.PublicKey // RS256 验签公钥；nil 表示未配置
	Issuer    string         // 预期 iss claim；非空时校验 iss 必须匹配
	Enabled   bool           // 是否启用 JWT 验签（与 PublicKey!=nil 等价，显式字段便于配置装配）
}

// JWT claim 键约定（与网关 / IAM 签发的 token 对齐）。
const (
	claimTenantID  = "tenant_id"
	claimUserID    = "user_id"
	claimUserRoles = "user_roles" // 字符串数组或逗号分隔字符串，两种均兼容
)

// ErrNoJWTToken 表示请求未携带 Authorization: Bearer <token> 头。
// 当 JWT 验签启用（Enabled && PublicKey!=nil）时，FromRequest 返回此错误，
// 调用方应据此拒绝请求（401），不再回退到头注入模式以防越权。
var ErrNoJWTToken = errors.New("authctx: 未携带 Authorization Bearer token")

// LoadJWTPublicKey 从 PEM 文件加载 RSA 公钥。
// path 为空时返回 (nil, nil) 表示未配置（调用方据此关闭 JWT 验签）。
func LoadJWTPublicKey(path string) (*rsa.PublicKey, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("authctx: 读取 JWT 公钥文件 %q 失败: %w", path, err)
	}
	return ParseJWTPublicKey(data)
}

// ParseJWTPublicKey 从 PEM 字节流解析 RSA 公钥。
// 支持 "PUBLIC KEY"（PKCS#1/SPKI 均可，由 x509 自动识别）。
func ParseJWTPublicKey(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("authctx: JWT 公钥 PEM 解码失败（非有效 PEM 格式）")
	}
	pub, err := jwt.ParseRSAPublicKeyFromPEM(data)
	if err != nil {
		return nil, fmt.Errorf("authctx: 解析 RSA 公钥失败: %w", err)
	}
	return pub, nil
}

// FromJWT 从 HTTP 头提取 Authorization: Bearer <token>，用 RSA 公钥 RS256 验签，
// 校验 issuer（若配置），从 claims 提取 tenant_id/user_id/user_roles 构造 Context。
//
// 安全语义：
//   - 验签失败、过期、issuer 不匹配 → 返回 error，调用方应拒绝请求（不回退头注入）；
//   - 未携带 Authorization 头 → 返回 ErrNoJWTToken，调用方可选择回退头注入模式；
//   - 验签通过但 claims 缺 tenant_id/user_id → 字段为空（与头注入缺失等价，由 BelongsTo 处理降级）。
//
// 注意：本函数不做"网关剥离 + 内核二次校验"以外的越权判断，行级隔离仍由 BelongsTo 完成。
func FromJWT(h http.Header, publicKey *rsa.PublicKey, issuer string) (Context, error) {
	if publicKey == nil {
		return Context{}, errors.New("authctx: JWT 验签公钥为 nil（未配置）")
	}
	tokenStr, err := extractBearerToken(h)
	if err != nil {
		return Context{}, err
	}

	// 解析并验签。jwt/v5 默认校验 exp/nbf/iat（WithExpirationRequired 等需显式开启）。
	// 这里显式校验 exp（过期 token 拒绝），并按配置校验 iss。
	// 用 MapClaims 作为 claims 目标，既能验签又能提取自定义 claim（tenant_id 等）。
	parserOpts := []jwt.ParserOption{jwt.WithValidMethods([]string{"RS256"})}
	if issuer != "" {
		parserOpts = append(parserOpts, jwt.WithIssuer(issuer))
	}
	token, err := jwt.ParseWithClaims(tokenStr, jwt.MapClaims{}, func(t *jwt.Token) (interface{}, error) {
		// 双重保险：仅接受 RS256（WithValidMethods 已限，此处再断言算法类型防降级攻击）。
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("authctx: 非预期签名算法 %v（仅接受 RS256）", t.Header["alg"])
		}
		return publicKey, nil
	}, parserOpts...)
	if err != nil {
		return Context{}, fmt.Errorf("authctx: JWT 验签失败: %w", err)
	}
	if !token.Valid {
		return Context{}, errors.New("authctx: JWT token 无效")
	}

	// 提取自定义 claims（tenant_id/user_id/user_roles）。
	c := Context{}
	mc, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return Context{}, errors.New("authctx: JWT claims 类型非 MapClaims（无法提取身份字段）")
	}
	c.TenantID = claimString(mc, claimTenantID)
	c.UserID = claimString(mc, claimUserID)
	c.Roles = claimRoles(mc)
	return c, nil
}

// FromRequest 按配置选择身份提取路径：JWT 验签启用时强制走 JWT 路径，
// 未启用时回退到头注入模式（MVP 兼容）。
//
// 推荐用法（server.go / grpc.go 入口）：
//
//	jwtCfg := authctx.JWTConfig{PublicKey: pub, Issuer: cfg.JWTIssuer, Enabled: pub != nil}
//	c, err := authctx.FromRequest(r.Header, jwtCfg)
//	if err != nil { /* 401 */ }
//
// 行为矩阵（安全加固：JWT 启用时禁止无 token 回退头注入）：
//   - Enabled && PublicKey!=nil && 携带有效 token   → 返回 JWT 提取的 Context, nil
//   - Enabled && PublicKey!=nil && token 验签失败   → 返回零值, error（调用方应 401）
//   - Enabled && PublicKey!=nil && 未携带 token     → 返回零值, ErrNoJWTToken（调用方应 401，不回退头注入）
//   - Enabled && PublicKey!=nil && Authorization 非 Bearer 格式 → 返回零值, error（调用方应 401）
//   - !Enabled || PublicKey==nil                   → 直接 FromHTTPHeader, nil（MVP 头注入模式）
//
// 安全语义：当 JWT 验签启用时，必须携带有效 Bearer token，攻击者无法通过省略
// Authorization 头并伪造 X-Tenant-ID 头来绕过身份校验。
func FromRequest(h http.Header, cfg JWTConfig) (Context, error) {
	if cfg.Enabled && cfg.PublicKey != nil {
		// JWT 验签启用：强制走 JWT 路径，不回退头注入模式。
		// - 携带有效 token → FromJWT 验签并提取 claims
		// - token 验签失败 / 非 Bearer 格式 / 未携带 token → 返回 error，调用方应 401
		return FromJWT(h, cfg.PublicKey, cfg.Issuer)
	}
	// 未启用 JWT 验签：回退头注入模式（MVP 兼容，需在可信网关后部署）。
	return FromHTTPHeader(h), nil
}

// extractBearerToken 从 Authorization: Bearer <token> 头提取 token 部分。
func extractBearerToken(h http.Header) (string, error) {
	v := h.Get("Authorization")
	if v == "" {
		return "", ErrNoJWTToken
	}
	// 兼容 "Bearer xxx" 与 "bearer xxx"（RFC 7235 区分大小写，但实战中网关常小写）。
	const prefix = "Bearer "
	if len(v) < len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return "", errors.New("authctx: Authorization 头非 Bearer 格式")
	}
	token := strings.TrimSpace(v[len(prefix):])
	if token == "" {
		return "", errors.New("authctx: Authorization Bearer token 为空")
	}
	return token, nil
}

// claimString 从 MapClaims 取字符串值（兼容 string/数字等可 stringify 类型）。
func claimString(mc jwt.MapClaims, key string) string {
	v, ok := mc[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	// 数字 / 其他类型 fallback 到 fmt.Sprint（避免 json.Number 等丢失）。
	return fmt.Sprint(v)
}

// claimRoles 从 MapClaims 取 user_roles，兼容字符串数组与逗号分隔字符串两种签发格式。
func claimRoles(mc jwt.MapClaims) []string {
	v, ok := mc[claimUserRoles]
	if !ok || v == nil {
		return nil
	}
	switch rv := v.(type) {
	case []interface{}:
		out := make([]string, 0, len(rv))
		for _, r := range rv {
			if s, ok := r.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	case string:
		return splitRoles(rv)
	default:
		// 兜底：单值 stringify。
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" {
			return nil
		}
		return []string{s}
	}
}
