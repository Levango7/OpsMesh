package authctx

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testRSAKey 生成测试用 RSA 密钥对（2048 位，测试用足够）。
// 返回 (私钥用于签发, 公钥用于验签)。
func testRSAKey(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成 RSA 密钥失败: %v", err)
	}
	return priv, &priv.PublicKey
}

// issueJWT 用私钥 RS256 签发一个带自定义 claims 的 JWT。
func issueJWT(t *testing.T, priv *rsa.PrivateKey, issuer string, expiresAt time.Time, tenantID, userID string, roles []string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":         issuer,
		"exp":         expiresAt.Unix(),
		"iat":         time.Now().Unix(),
		"nbf":         time.Now().Unix(),
		claimTenantID: tenantID,
		claimUserID:   userID,
	}
	if roles != nil {
		// 用字符串数组签发（生产网关常见格式）。
		claims[claimUserRoles] = roles
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("签发 JWT 失败: %v", err)
	}
	return signed
}

// issueJWTStringRoles 签发 user_roles 为逗号分隔字符串格式的 JWT（兼容性测试）。
func issueJWTStringRoles(t *testing.T, priv *rsa.PrivateKey, issuer string, expiresAt time.Time, tenantID, userID, roles string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":          issuer,
		"exp":          expiresAt.Unix(),
		"iat":          time.Now().Unix(),
		"nbf":          time.Now().Unix(),
		claimTenantID:  tenantID,
		claimUserID:    userID,
		claimUserRoles: roles,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("签发 JWT 失败: %v", err)
	}
	return signed
}

func setBearer(h http.Header, token string) {
	h.Set("Authorization", "Bearer "+token)
}

// ----------------------------------------------------------------------------
// Happy path
// ----------------------------------------------------------------------------

// TestFromJWT_HappyPath 验签通过：生成 RSA 密钥 → 签发 JWT → 验签提取 tenant/user/roles。
func TestFromJWT_HappyPath(t *testing.T) {
	priv, pub := testRSAKey(t)
	tok := issueJWT(t, priv, "opsmesh-gw", time.Now().Add(1*time.Hour), "t1", "u1", []string{"admin", "ops"})

	h := http.Header{}
	setBearer(h, tok)

	c, err := FromJWT(h, pub, "opsmesh-gw")
	if err != nil {
		t.Fatalf("验签应通过，但报错: %v", err)
	}
	if c.TenantID != "t1" {
		t.Fatalf("TenantID = %q, want t1", c.TenantID)
	}
	if c.UserID != "u1" {
		t.Fatalf("UserID = %q, want u1", c.UserID)
	}
	if len(c.Roles) != 2 || c.Roles[0] != "admin" || c.Roles[1] != "ops" {
		t.Fatalf("Roles = %v, want [admin ops]", c.Roles)
	}
}

// TestFromJWT_RolesAsString 兼容 user_roles 为逗号分隔字符串的签发格式。
func TestFromJWT_RolesAsString(t *testing.T) {
	priv, pub := testRSAKey(t)
	tok := issueJWTStringRoles(t, priv, "opsmesh-gw", time.Now().Add(1*time.Hour), "t1", "u1", "admin,ops,viewer")

	h := http.Header{}
	setBearer(h, tok)

	c, err := FromJWT(h, pub, "opsmesh-gw")
	if err != nil {
		t.Fatalf("验签应通过: %v", err)
	}
	want := []string{"admin", "ops", "viewer"}
	if len(c.Roles) != len(want) {
		t.Fatalf("Roles = %v, want %v", c.Roles, want)
	}
	for i := range want {
		if c.Roles[i] != want[i] {
			t.Fatalf("Roles[%d] = %q, want %q", i, c.Roles[i], want[i])
		}
	}
}

// TestFromJWT_NoIssuerCheck issuer 留空时不校验 iss（任何 issuer 均通过）。
func TestFromJWT_NoIssuerCheck(t *testing.T) {
	priv, pub := testRSAKey(t)
	tok := issueJWT(t, priv, "any-issuer", time.Now().Add(1*time.Hour), "t1", "u1", nil)

	h := http.Header{}
	setBearer(h, tok)

	c, err := FromJWT(h, pub, "")
	if err != nil {
		t.Fatalf("未配置 issuer 校验时应通过: %v", err)
	}
	if c.TenantID != "t1" || c.UserID != "u1" {
		t.Fatalf("got %+v", c)
	}
}

// ----------------------------------------------------------------------------
// 错误 case
// ----------------------------------------------------------------------------

// TestFromJWT_Expired 过期 token 应被拒绝。
func TestFromJWT_Expired(t *testing.T) {
	priv, pub := testRSAKey(t)
	// 签发一个已过期 1 小时的 token。
	tok := issueJWT(t, priv, "opsmesh-gw", time.Now().Add(-1*time.Hour), "t1", "u1", nil)

	h := http.Header{}
	setBearer(h, tok)

	if _, err := FromJWT(h, pub, "opsmesh-gw"); err == nil {
		t.Fatal("过期 token 应被拒绝，但验签通过")
	}
}

// TestFromJWT_WrongIssuer issuer 不匹配应被拒绝。
func TestFromJWT_WrongIssuer(t *testing.T) {
	priv, pub := testRSAKey(t)
	tok := issueJWT(t, priv, "evil-issuer", time.Now().Add(1*time.Hour), "t1", "u1", nil)

	h := http.Header{}
	setBearer(h, tok)

	if _, err := FromJWT(h, pub, "opsmesh-gw"); err == nil {
		t.Fatal("错误 issuer 应被拒绝，但验签通过")
	}
}

// TestFromJWT_InvalidSignature 用不同密钥签发应被拒绝（防伪造）。
func TestFromJWT_InvalidSignature(t *testing.T) {
	priv1, _ := testRSAKey(t)
	_, pub2 := testRSAKey(t) // 不同的密钥对
	tok := issueJWT(t, priv1, "opsmesh-gw", time.Now().Add(1*time.Hour), "t1", "u1", nil)

	h := http.Header{}
	setBearer(h, tok)

	if _, err := FromJWT(h, pub2, "opsmesh-gw"); err == nil {
		t.Fatal("错误签名应被拒绝，但验签通过")
	}
}

// TestFromJWT_NoToken 未携带 Authorization 头应返回 ErrNoJWTToken。
func TestFromJWT_NoToken(t *testing.T) {
	_, pub := testRSAKey(t)
	h := http.Header{}
	_, err := FromJWT(h, pub, "opsmesh-gw")
	if err == nil {
		t.Fatal("未携带 token 应报错")
	}
	if err != ErrNoJWTToken {
		t.Fatalf("应返回 ErrNoJWTToken，got %v", err)
	}
}

// TestFromJWT_MalformedAuthorization 非法 Authorization 头格式应被拒绝。
func TestFromJWT_MalformedAuthorization(t *testing.T) {
	_, pub := testRSAKey(t)
	h := http.Header{}
	h.Set("Authorization", "Basic abc") // 非 Bearer
	if _, err := FromJWT(h, pub, "opsmesh-gw"); err == nil {
		t.Fatal("非 Bearer 格式应被拒绝")
	}
}

// TestFromJWT_NilPublicKey 公钥为 nil 应报错（防配置装配遗漏）。
func TestFromJWT_NilPublicKey(t *testing.T) {
	h := http.Header{}
	setBearer(h, "dummy")
	if _, err := FromJWT(h, nil, ""); err == nil {
		t.Fatal("公钥 nil 应报错")
	}
}

// ----------------------------------------------------------------------------
// FromRequest 回退逻辑
// ----------------------------------------------------------------------------

// TestFromRequest_NoTokenFallbackHeader 启用 JWT 但未携带 token 时回退头注入模式。
func TestFromRequest_NoTokenFallbackHeader(t *testing.T) {
	_, pub := testRSAKey(t)
	cfg := JWTConfig{PublicKey: pub, Issuer: "opsmesh-gw", Enabled: true}

	h := http.Header{}
	h.Set("X-Tenant-ID", "t-fallback")
	h.Set("X-User-Id", "u-fallback")
	h.Set("X-User-Roles", "admin,ops")

	c, err := FromRequest(h, cfg)
	if err != nil {
		t.Fatalf("回退头注入不应报错: %v", err)
	}
	if c.TenantID != "t-fallback" || c.UserID != "u-fallback" {
		t.Fatalf("回退头注入失败: %+v", c)
	}
	if len(c.Roles) != 2 || c.Roles[0] != "admin" || c.Roles[1] != "ops" {
		t.Fatalf("Roles = %v", c.Roles)
	}
}

// TestFromRequest_JWTEnabledAndTokenProvided 启用 JWT 且携带 token 时走 JWT 验签路径。
func TestFromRequest_JWTEnabledAndTokenProvided(t *testing.T) {
	priv, pub := testRSAKey(t)
	tok := issueJWT(t, priv, "opsmesh-gw", time.Now().Add(1*time.Hour), "t-jwt", "u-jwt", []string{"admin"})
	cfg := JWTConfig{PublicKey: pub, Issuer: "opsmesh-gw", Enabled: true}

	h := http.Header{}
	setBearer(h, tok)
	// 同时设置头注入（应被 JWT 路径覆盖，证明走了 JWT 而非头注入）。
	h.Set("X-Tenant-ID", "t-header")
	h.Set("X-User-Id", "u-header")

	c, err := FromRequest(h, cfg)
	if err != nil {
		t.Fatalf("JWT 验签应通过: %v", err)
	}
	if c.TenantID != "t-jwt" || c.UserID != "u-jwt" {
		t.Fatalf("应走 JWT 路径，got %+v", c)
	}
}

// TestFromRequest_JWTEnabledButTokenInvalid 启用 JWT 且 token 验签失败应返回 error（不回退头注入）。
func TestFromRequest_JWTEnabledButTokenInvalid(t *testing.T) {
	priv1, _ := testRSAKey(t)
	_, pub2 := testRSAKey(t) // 不同密钥
	tok := issueJWT(t, priv1, "opsmesh-gw", time.Now().Add(1*time.Hour), "t1", "u1", nil)
	cfg := JWTConfig{PublicKey: pub2, Issuer: "opsmesh-gw", Enabled: true}

	h := http.Header{}
	setBearer(h, tok)
	h.Set("X-Tenant-ID", "t-header") // 即使有头注入也不应回退

	_, err := FromRequest(h, cfg)
	if err == nil {
		t.Fatal("token 验签失败应返回 error，不应回退头注入")
	}
}

// TestFromRequest_JWTDisabled 未启用 JWT 时直接走头注入模式。
func TestFromRequest_JWTDisabled(t *testing.T) {
	_, pub := testRSAKey(t)
	cfg := JWTConfig{PublicKey: pub, Issuer: "opsmesh-gw", Enabled: false}

	h := http.Header{}
	// 即使携带 token，未启用也应走头注入。
	setBearer(h, "dummy.invalid.token")
	h.Set("X-Tenant-ID", "t-header")
	h.Set("X-User-Id", "u-header")

	c, err := FromRequest(h, cfg)
	if err != nil {
		t.Fatalf("未启用 JWT 不应报错: %v", err)
	}
	if c.TenantID != "t-header" || c.UserID != "u-header" {
		t.Fatalf("应走头注入，got %+v", c)
	}
}

// TestFromRequest_NilPublicKeyWithEnabled PublicKey 为 nil 但 Enabled=true 时安全回退头注入。
// 防御性测试：配置装配错误不应导致 panic。
func TestFromRequest_NilPublicKeyWithEnabled(t *testing.T) {
	cfg := JWTConfig{PublicKey: nil, Issuer: "", Enabled: true}
	h := http.Header{}
	h.Set("X-Tenant-ID", "t1")

	c, err := FromRequest(h, cfg)
	if err != nil {
		t.Fatalf("nil 公钥应安全回退头注入: %v", err)
	}
	if c.TenantID != "t1" {
		t.Fatalf("回退头注入失败: %+v", c)
	}
}

// ----------------------------------------------------------------------------
// LoadJWTPublicKey / ParseJWTPublicKey
// ----------------------------------------------------------------------------

// TestLoadJWTPublicKey_EmptyPath 空路径返回 (nil, nil) 表示未配置。
func TestLoadJWTPublicKey_EmptyPath(t *testing.T) {
	pub, err := LoadJWTPublicKey("")
	if err != nil {
		t.Fatalf("空路径不应报错: %v", err)
	}
	if pub != nil {
		t.Fatalf("空路径应返回 nil 公钥，got %v", pub)
	}
}

// TestParseJWTPublicKey_FromPEM 从 PEM 字节流解析 RSA 公钥。
func TestParseJWTPublicKey_FromPEM(t *testing.T) {
	_, pub := testRSAKey(t)
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("编码公钥 DER 失败: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

	parsed, err := ParseJWTPublicKey(pemBytes)
	if err != nil {
		t.Fatalf("解析公钥应成功: %v", err)
	}
	if parsed == nil {
		t.Fatal("解析公钥不应为 nil")
	}
	// 验证解析出的公钥与原公钥一致（模数比对）。
	if parsed.N.Cmp(pub.N) != 0 {
		t.Fatal("解析出的公钥模数与原公钥不一致")
	}
}

// TestParseJWTPublicKey_InvalidPEM 非法 PEM 应报错。
func TestParseJWTPublicKey_InvalidPEM(t *testing.T) {
	if _, err := ParseJWTPublicKey([]byte("not a pem")); err == nil {
		t.Fatal("非法 PEM 应报错")
	}
}

// TestParseJWTPublicKey_NonRSAPublicKey 非 RSA 公钥（如 ECDSA）应报错。
func TestParseJWTPublicKey_NonRSAPublicKey(t *testing.T) {
	// 构造一个 PEM 块但内容非 RSA 公钥。
	block := &pem.Block{Type: "PUBLIC KEY", Bytes: []byte("not a valid der")}
	if _, err := ParseJWTPublicKey(pem.EncodeToMemory(block)); err == nil {
		t.Fatal("非 RSA 公钥应报错")
	}
}
