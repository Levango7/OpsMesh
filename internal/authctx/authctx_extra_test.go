package authctx

// 本文件为补全 internal/authctx 包测试覆盖率而新增的额外测试。
// 重点覆盖 jwt_sign.go（SignJWT/ParseHSJWT）以及 authctx.go 中
// LoadJWTPublicKey、FromGRPCMetadata、FromJWT、extractBearerToken、
// claimString、claimRoles 等函数的未覆盖分支。
//
// 设计原则：
//   - 只创建新测试文件，不修改现有源代码；
//   - 测试使用简体中文注释；
//   - 通过真实签发 → 验签的闭环验证 HS256 JWT 流程；
//   - 通过临时文件验证 LoadJWTPublicKey 文件读取路径。

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/metadata"
)

// ============================================================================
// jwt_sign.go: SignJWT
// ============================================================================

// TestSignJWT_EmptySecret 空密钥应返回 error（防配置遗漏导致任何人可伪造 token）。
func TestSignJWT_EmptySecret(t *testing.T) {
	_, err := SignJWT(JWTClaims{UserID: "u1"}, nil)
	if err == nil {
		t.Fatal("空密钥应报错")
	}
	// 错误消息应包含可识别关键字，便于调用方日志检索。
	if !contains(err.Error(), "密钥为空") {
		t.Fatalf("错误消息应提示密钥为空，got %v", err)
	}
}

// TestSignJWT_HappyPath 正常签发流程：默认过期 24h、自动生成 jti。
func TestSignJWT_HappyPath(t *testing.T) {
	secret := []byte("test-secret-at-least-32-bytes-long")
	claims := JWTClaims{
		UserID:      "u1",
		Username:    "alice",
		Roles:       []string{"admin", "ops"},
		Permissions: []string{"read", "write"},
		TenantID:    "t1",
	}
	tok, err := SignJWT(claims, secret)
	if err != nil {
		t.Fatalf("签发应成功: %v", err)
	}
	if tok == "" {
		t.Fatal("token 不应为空")
	}
	// 验证 token 是三段式紧凑格式（header.payload.signature）。
	if countByte(tok, '.') != 2 {
		t.Fatalf("token 应为三段式，got %q", tok)
	}
}

// TestSignJWT_DefaultExpiry ExpiresAt 为零值时默认 24h 后过期。
func TestSignJWT_DefaultExpiry(t *testing.T) {
	secret := []byte("test-secret-at-least-32-bytes-long")
	claims := JWTClaims{UserID: "u1"}
	// ExpiresAt 为零值。
	tok, err := SignJWT(claims, secret)
	if err != nil {
		t.Fatalf("签发应成功: %v", err)
	}
	// 解析回来验证 exp 大约在 24h 后。
	parsed, err := ParseHSJWT(tok, secret)
	if err != nil {
		t.Fatalf("验签应成功: %v", err)
	}
	// 允许 ±2 分钟误差（签发与解析之间有耗时）。
	approx24h := time.Until(parsed.ExpiresAt)
	if approx24h < 23*time.Hour || approx24h > 25*time.Hour {
		t.Fatalf("默认过期应约 24h，got %v", approx24h)
	}
}

// TestSignJWT_ExplicitJTI 调用方显式指定 JTI 时应保留（不覆盖）。
func TestSignJWT_ExplicitJTI(t *testing.T) {
	secret := []byte("test-secret-at-least-32-bytes-long")
	claims := JWTClaims{
		UserID:    "u1",
		JTI:       "my-custom-jti-123",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	tok, err := SignJWT(claims, secret)
	if err != nil {
		t.Fatalf("签发应成功: %v", err)
	}
	parsed, err := ParseHSJWT(tok, secret)
	if err != nil {
		t.Fatalf("验签应成功: %v", err)
	}
	if parsed.JTI != "my-custom-jti-123" {
		t.Fatalf("JTI 应保留原值，got %q", parsed.JTI)
	}
}

// TestSignJWT_AutoJTIUniqueness 多次签发应生成不同 jti（碰撞概率可忽略）。
func TestSignJWT_AutoJTIUniqueness(t *testing.T) {
	secret := []byte("test-secret-at-least-32-bytes-long")
	seen := make(map[string]bool, 10)
	for i := 0; i < 10; i++ {
		tok, err := SignJWT(JWTClaims{UserID: "u1", ExpiresAt: time.Now().Add(1 * time.Hour)}, secret)
		if err != nil {
			t.Fatalf("签发 %d 失败: %v", i, err)
		}
		parsed, err := ParseHSJWT(tok, secret)
		if err != nil {
			t.Fatalf("验签 %d 失败: %v", i, err)
		}
		if parsed.JTI == "" {
			t.Fatalf("第 %d 个 token 的 JTI 不应为空", i)
		}
		if seen[parsed.JTI] {
			t.Fatalf("第 %d 个 token 的 JTI %q 与之前重复", i, parsed.JTI)
		}
		seen[parsed.JTI] = true
	}
}

// ============================================================================
// jwt_sign.go: ParseHSJWT
// ============================================================================

// TestParseHSJWT_EmptySecret 空密钥应返回 error。
func TestParseHSJWT_EmptySecret(t *testing.T) {
	_, err := ParseHSJWT("dummy.token.here", nil)
	if err == nil {
		t.Fatal("空密钥应报错")
	}
	if !contains(err.Error(), "密钥为空") {
		t.Fatalf("错误消息应提示密钥为空，got %v", err)
	}
}

// TestParseHSJWT_WrongSecret 密钥不匹配应验签失败。
func TestParseHSJWT_WrongSecret(t *testing.T) {
	signSecret := []byte("signing-secret-32-bytes-long-xxx")
	verifySecret := []byte("different-secret-32-bytes-long-yyy")
	claims := JWTClaims{
		UserID:    "u1",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	tok, err := SignJWT(claims, signSecret)
	if err != nil {
		t.Fatalf("签发应成功: %v", err)
	}
	if _, err := ParseHSJWT(tok, verifySecret); err == nil {
		t.Fatal("密钥不匹配应验签失败")
	}
}

// TestParseHSJWT_MalformedToken 非法 token 字符串应报错。
func TestParseHSJWT_MalformedToken(t *testing.T) {
	secret := []byte("test-secret-at-least-32-bytes-long")
	if _, err := ParseHSJWT("not-a-jwt", secret); err == nil {
		t.Fatal("非法 token 应报错")
	}
	if _, err := ParseHSJWT("a.b.c", secret); err == nil {
		t.Fatal("非法 token 应报错")
	}
}

// TestParseHSJWT_AlgNoneAttack alg=none 降级攻击应被拒绝（仅接受 HS256）。
func TestParseHSJWT_AlgNoneAttack(t *testing.T) {
	secret := []byte("test-secret-at-least-32-bytes-long")
	// 手工构造 alg=none 的 token（无签名）。
	// header: {"alg":"none","typ":"JWT"}；payload: {"sub":"u1"}
	header := `{"alg":"none","typ":"JWT"}`
	payload := `{"sub":"u1"}`
	tok := base64URLEncode([]byte(header)) + "." + base64URLEncode([]byte(payload)) + "."
	if _, err := ParseHSJWT(tok, secret); err == nil {
		t.Fatal("alg=none 攻击应被拒绝")
	}
}

// TestParseHSJWT_ExpiredToken 过期 token 应被拒绝。
func TestParseHSJWT_ExpiredToken(t *testing.T) {
	secret := []byte("test-secret-at-least-32-bytes-long")
	// 直接用 jwt 库签发一个已过期的 token（绕过 SignJWT 的默认 24h 兜底）。
	signer := jwtSigner{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "u1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, signer)
	tok, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("签发过期 token 失败: %v", err)
	}
	if _, err := ParseHSJWT(tok, secret); err == nil {
		t.Fatal("过期 token 应被拒绝")
	}
}

// TestParseHSJWT_FullRoundTrip 签发 → 验签完整闭环，所有字段应一致。
func TestParseHSJWT_FullRoundTrip(t *testing.T) {
	secret := []byte("test-secret-at-least-32-bytes-long")
	original := JWTClaims{
		UserID:      "u-full",
		Username:    "bob",
		Roles:       []string{"admin", "ops", "viewer"},
		Permissions: []string{"read", "write", "delete"},
		TenantID:    "t-full",
		JTI:         "jti-full-rt",
		ExpiresAt:   time.Now().Add(2 * time.Hour).Truncate(time.Second),
	}
	tok, err := SignJWT(original, secret)
	if err != nil {
		t.Fatalf("签发应成功: %v", err)
	}
	parsed, err := ParseHSJWT(tok, secret)
	if err != nil {
		t.Fatalf("验签应成功: %v", err)
	}
	if parsed.UserID != original.UserID {
		t.Errorf("UserID = %q, want %q", parsed.UserID, original.UserID)
	}
	if parsed.Username != original.Username {
		t.Errorf("Username = %q, want %q", parsed.Username, original.Username)
	}
	if parsed.TenantID != original.TenantID {
		t.Errorf("TenantID = %q, want %q", parsed.TenantID, original.TenantID)
	}
	if parsed.JTI != original.JTI {
		t.Errorf("JTI = %q, want %q", parsed.JTI, original.JTI)
	}
	if !sliceEqual(parsed.Roles, original.Roles) {
		t.Errorf("Roles = %v, want %v", parsed.Roles, original.Roles)
	}
	if !sliceEqual(parsed.Permissions, original.Permissions) {
		t.Errorf("Permissions = %v, want %v", parsed.Permissions, original.Permissions)
	}
}

// TestParseHSJWT_NilExpiresAt claims 中无 exp 字段时 ExpiresAt 应保持零值。
func TestParseHSJWT_NilExpiresAt(t *testing.T) {
	secret := []byte("test-secret-at-least-32-bytes-long")
	// 手工签发一个无 exp 的 token（jwt 库默认不强制要求 exp）。
	signer := jwtSigner{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:  "u1",
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, signer)
	tok, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("签发无 exp token 失败: %v", err)
	}
	parsed, err := ParseHSJWT(tok, secret)
	if err != nil {
		t.Fatalf("无 exp token 验签应通过: %v", err)
	}
	if !parsed.ExpiresAt.IsZero() {
		t.Fatalf("无 exp 时 ExpiresAt 应为零值，got %v", parsed.ExpiresAt)
	}
}

// ============================================================================
// authctx.go: LoadJWTPublicKey（文件读取路径）
// ============================================================================

// TestLoadJWTPublicKey_FromFile 从临时 PEM 文件加载 RSA 公钥应成功。
func TestLoadJWTPublicKey_FromFile(t *testing.T) {
	_, pub := testRSAKey(t)
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("编码公钥 DER 失败: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

	// 写入临时文件。
	dir := t.TempDir()
	path := filepath.Join(dir, "pub.pem")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("写入临时 PEM 文件失败: %v", err)
	}

	loaded, err := LoadJWTPublicKey(path)
	if err != nil {
		t.Fatalf("从文件加载公钥应成功: %v", err)
	}
	if loaded == nil {
		t.Fatal("加载的公钥不应为 nil")
	}
	if loaded.N.Cmp(pub.N) != 0 {
		t.Fatal("加载的公钥模数与原公钥不一致")
	}
}

// TestLoadJWTPublicKey_FileNotExist 文件不存在应返回 error。
func TestLoadJWTPublicKey_FileNotExist(t *testing.T) {
	_, err := LoadJWTPublicKey(filepath.Join(t.TempDir(), "nonexistent.pem"))
	if err == nil {
		t.Fatal("文件不存在应报错")
	}
	if !contains(err.Error(), "读取 JWT 公钥文件") {
		t.Fatalf("错误消息应提示读取失败，got %v", err)
	}
}

// TestLoadJWTPublicKey_InvalidPEMContent 文件存在但内容非法应返回 error。
func TestLoadJWTPublicKey_InvalidPEMContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.pem")
	if err := os.WriteFile(path, []byte("not a pem content"), 0o600); err != nil {
		t.Fatalf("写入临时文件失败: %v", err)
	}
	if _, err := LoadJWTPublicKey(path); err == nil {
		t.Fatal("非法 PEM 内容应报错")
	}
}

// ============================================================================
// authctx.go: FromGRPCMetadata（空 metadata 分支）
// ============================================================================

// TestFromGRPCMetadata_Empty 空 metadata 应返回零值 Context。
func TestFromGRPCMetadata_Empty(t *testing.T) {
	c := FromGRPCMetadata(metadata.MD{})
	if c.TenantID != "" || c.UserID != "" {
		t.Fatalf("空 metadata 应返回零值，got %+v", c)
	}
	if c.Roles != nil {
		t.Fatalf("空 metadata Roles 应为 nil，got %v", c.Roles)
	}
}

// TestFromGRPCMetadata_PartialMissing 仅设置部分头，其余字段应为空。
func TestFromGRPCMetadata_PartialMissing(t *testing.T) {
	md := metadata.Pairs("x-tenant-id", "t-only")
	c := FromGRPCMetadata(md)
	if c.TenantID != "t-only" {
		t.Fatalf("TenantID = %q, want t-only", c.TenantID)
	}
	if c.UserID != "" {
		t.Fatalf("UserID 应为空，got %q", c.UserID)
	}
	if c.Roles != nil {
		t.Fatalf("Roles 应为 nil，got %v", c.Roles)
	}
}

// TestFromGRPCMetadata_RolesWithSpaces 角色字符串带空格应正确 trim。
func TestFromGRPCMetadata_RolesWithSpaces(t *testing.T) {
	md := metadata.Pairs("x-user-roles", "  admin ,  ops  , viewer  ")
	c := FromGRPCMetadata(md)
	want := []string{"admin", "ops", "viewer"}
	if !sliceEqual(c.Roles, want) {
		t.Fatalf("Roles = %v, want %v", c.Roles, want)
	}
}

// ============================================================================
// authctx.go: extractBearerToken（小写 bearer、空 token 分支）
// ============================================================================

// TestExtractBearerToken_LowercaseBearer 小写 "bearer" 前缀应被接受（实战兼容）。
func TestExtractBearerToken_LowercaseBearer(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "bearer abc.def.ghi")
	tok, err := extractBearerToken(h)
	if err != nil {
		t.Fatalf("小写 bearer 应被接受: %v", err)
	}
	if tok != "abc.def.ghi" {
		t.Fatalf("token = %q, want abc.def.ghi", tok)
	}
}

// TestExtractBearerToken_MixedCaseBearer 大小写混合应被接受。
func TestExtractBearerToken_MixedCaseBearer(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "BeArEr token123")
	tok, err := extractBearerToken(h)
	if err != nil {
		t.Fatalf("混合大小写 bearer 应被接受: %v", err)
	}
	if tok != "token123" {
		t.Fatalf("token = %q, want token123", tok)
	}
}

// TestExtractBearerToken_EmptyToken Bearer 后为空应报错。
func TestExtractBearerToken_EmptyToken(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer ")
	if _, err := extractBearerToken(h); err == nil {
		t.Fatal("Bearer 后为空应报错")
	}
}

// TestExtractBearerToken_OnlySpaces Bearer 后仅空格应报错。
func TestExtractBearerToken_OnlySpaces(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer    ")
	if _, err := extractBearerToken(h); err == nil {
		t.Fatal("Bearer 后仅空格应报错")
	}
}

// TestExtractBearerToken_TooShort Authorization 值过短（短于 "Bearer " 长度）应报错。
func TestExtractBearerToken_TooShort(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bear")
	if _, err := extractBearerToken(h); err == nil {
		t.Fatal("过短的 Authorization 值应报错")
	}
}

// TestExtractBearerToken_NoAuthorizationHeader 未设置 Authorization 头应返回 ErrNoJWTToken。
func TestExtractBearerToken_NoAuthorizationHeader(t *testing.T) {
	h := http.Header{}
	_, err := extractBearerToken(h)
	if !errors.Is(err, ErrNoJWTToken) {
		t.Fatalf("未设置 Authorization 头应返回 ErrNoJWTToken，got %v", err)
	}
}

// ============================================================================
// authctx.go: FromJWT（token 无效、claims 类型断言失败分支）
// ============================================================================

// TestFromJWT_InvalidTokenString 非法 token 字符串应验签失败。
func TestFromJWT_InvalidTokenString(t *testing.T) {
	_, pub := testRSAKey(t)
	h := http.Header{}
	setBearer(h, "not.a.valid.jwt")
	if _, err := FromJWT(h, pub, ""); err == nil {
		t.Fatal("非法 token 字符串应验签失败")
	}
}

// TestFromJWT_EmptyTokenInBearer Bearer 后为空应报错（经 extractBearerToken 拒绝）。
func TestFromJWT_EmptyTokenInBearer(t *testing.T) {
	_, pub := testRSAKey(t)
	h := http.Header{}
	h.Set("Authorization", "Bearer ")
	if _, err := FromJWT(h, pub, ""); err == nil {
		t.Fatal("Bearer 后为空应报错")
	}
}

// TestFromJWT_NumericClaims claim 值为数字类型时应通过 fmt.Sprint 兜底 stringify。
func TestFromJWT_NumericClaims(t *testing.T) {
	priv, pub := testRSAKey(t)
	// 用数字类型签发 tenant_id/user_id（覆盖 claimString 的非字符串分支）。
	claims := jwt.MapClaims{
		"iss":         "opsmesh-gw",
		"exp":         time.Now().Add(1 * time.Hour).Unix(),
		"iat":         time.Now().Unix(),
		"nbf":         time.Now().Unix(),
		claimTenantID: float64(12345), // JSON 数字解析为 float64
		claimUserID:   float64(67890),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("签发 JWT 失败: %v", err)
	}
	h := http.Header{}
	setBearer(h, tok)
	c, err := FromJWT(h, pub, "opsmesh-gw")
	if err != nil {
		t.Fatalf("数字 claims 验签应通过: %v", err)
	}
	// float64 → fmt.Sprint → "12345"。
	if c.TenantID != "12345" {
		t.Fatalf("TenantID = %q, want 12345", c.TenantID)
	}
	if c.UserID != "67890" {
		t.Fatalf("UserID = %q, want 67890", c.UserID)
	}
}

// TestFromJWT_RolesAsNumber user_roles 为数字类型时应走 claimRoles 的 default 兜底分支。
func TestFromJWT_RolesAsNumber(t *testing.T) {
	priv, pub := testRSAKey(t)
	claims := jwt.MapClaims{
		"iss":          "opsmesh-gw",
		"exp":          time.Now().Add(1 * time.Hour).Unix(),
		"iat":          time.Now().Unix(),
		"nbf":          time.Now().Unix(),
		claimTenantID:  "t1",
		claimUserID:    "u1",
		claimUserRoles: float64(42), // 非数组、非字符串 → default 兜底
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("签发 JWT 失败: %v", err)
	}
	h := http.Header{}
	setBearer(h, tok)
	c, err := FromJWT(h, pub, "opsmesh-gw")
	if err != nil {
		t.Fatalf("验签应通过: %v", err)
	}
	// float64(42) → fmt.Sprint → "42"。
	if len(c.Roles) != 1 || c.Roles[0] != "42" {
		t.Fatalf("Roles = %v, want [42]", c.Roles)
	}
}

// TestFromJWT_RolesAsArrayWithNonStringElements 数组中含非字符串元素应跳过。
func TestFromJWT_RolesAsArrayWithNonStringElements(t *testing.T) {
	priv, pub := testRSAKey(t)
	claims := jwt.MapClaims{
		"iss":         "opsmesh-gw",
		"exp":         time.Now().Add(1 * time.Hour).Unix(),
		"iat":         time.Now().Unix(),
		"nbf":         time.Now().Unix(),
		claimTenantID: "t1",
		claimUserID:   "u1",
		// 数组中混合字符串与数字，数字元素应被跳过。
		claimUserRoles: []interface{}{"admin", float64(123), "ops", ""},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("签发 JWT 失败: %v", err)
	}
	h := http.Header{}
	setBearer(h, tok)
	c, err := FromJWT(h, pub, "opsmesh-gw")
	if err != nil {
		t.Fatalf("验签应通过: %v", err)
	}
	want := []string{"admin", "ops"}
	if !sliceEqual(c.Roles, want) {
		t.Fatalf("Roles = %v, want %v", c.Roles, want)
	}
}

// TestFromJWT_MissingClaims claims 缺失 tenant_id/user_id/user_roles 时字段应为空。
func TestFromJWT_MissingClaims(t *testing.T) {
	priv, pub := testRSAKey(t)
	// 仅签发标准 claims，不含自定义字段。
	claims := jwt.MapClaims{
		"iss": "opsmesh-gw",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
		"nbf": time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("签发 JWT 失败: %v", err)
	}
	h := http.Header{}
	setBearer(h, tok)
	c, err := FromJWT(h, pub, "opsmesh-gw")
	if err != nil {
		t.Fatalf("验签应通过: %v", err)
	}
	if c.TenantID != "" || c.UserID != "" {
		t.Fatalf("缺失 claims 时字段应为空，got %+v", c)
	}
	if c.Roles != nil {
		t.Fatalf("缺失 user_roles 时 Roles 应为 nil，got %v", c.Roles)
	}
}

// ============================================================================
// authctx.go: FromRequest（补充未覆盖的回退分支）
// ============================================================================

// TestFromRequest_JWTEnabledButMalformedAuth 启用 JWT 但 Authorization 头格式非法
// （非 Bearer）时，FromRequest 应返回 error（安全加固）。
// 修复前：回退到头注入模式，攻击者可用非 Bearer 格式绕过 JWT 验签。
// 修复后：返回 error，调用方应 401。
func TestFromRequest_JWTEnabledButMalformedAuth(t *testing.T) {
	_, pub := testRSAKey(t)
	cfg := JWTConfig{PublicKey: pub, Issuer: "opsmesh-gw", Enabled: true}
	h := http.Header{}
	h.Set("Authorization", "Basic abc") // 非 Bearer 格式
	h.Set("X-Tenant-ID", "t-evil")
	h.Set("X-User-Id", "u-evil")
	c, err := FromRequest(h, cfg)
	if err == nil {
		t.Fatal("非 Bearer 格式应返回 error，不应回退头注入（越权风险）")
	}
	// 返回的 Context 应为零值，不应泄露头注入的身份。
	if c.TenantID != "" || c.UserID != "" {
		t.Fatalf("非 Bearer 格式时 Context 应为零值，不应泄露头注入身份，got %+v", c)
	}
}

// ============================================================================
// authctx.go: FromRequest 行为矩阵（安全加固：JWT 启用时禁止无 token 回退）
// ============================================================================
//
// 本组测试系统性验证修复后的 FromRequest 行为矩阵：
//   - Enabled && PublicKey!=nil && 携带有效 token   → JWT 提取的 Context, nil
//   - Enabled && PublicKey!=nil && token 验签失败   → 零值, error
//   - Enabled && PublicKey!=nil && 未携带 token     → 零值, ErrNoJWTToken
//   - Enabled && PublicKey!=nil && 非 Bearer 格式   → 零值, error
//   - !Enabled || PublicKey==nil                   → FromHTTPHeader, nil

// TestFromRequest_Matrix_JWTEnabled_ValidToken 启用 JWT 且携带有效 token → 返回 JWT 提取的 Context。
func TestFromRequest_Matrix_JWTEnabled_ValidToken(t *testing.T) {
	priv, pub := testRSAKey(t)
	tok := issueJWT(t, priv, "opsmesh-gw", time.Now().Add(1*time.Hour), "t-jwt", "u-jwt", []string{"admin", "ops"})
	cfg := JWTConfig{PublicKey: pub, Issuer: "opsmesh-gw", Enabled: true}

	h := http.Header{}
	setBearer(h, tok)
	// 同时设置头注入（应被 JWT 路径覆盖，证明走了 JWT 而非头注入）。
	h.Set("X-Tenant-ID", "t-header")
	h.Set("X-User-Id", "u-header")

	c, err := FromRequest(h, cfg)
	if err != nil {
		t.Fatalf("有效 token 应验签通过: %v", err)
	}
	if c.TenantID != "t-jwt" || c.UserID != "u-jwt" {
		t.Fatalf("应走 JWT 路径提取身份，got %+v", c)
	}
	if !sliceEqual(c.Roles, []string{"admin", "ops"}) {
		t.Fatalf("Roles = %v, want [admin ops]", c.Roles)
	}
}

// TestFromRequest_Matrix_JWTEnabled_InvalidToken 启用 JWT 且 token 验签失败 → 返回错误。
func TestFromRequest_Matrix_JWTEnabled_InvalidToken(t *testing.T) {
	priv1, _ := testRSAKey(t)
	_, pub2 := testRSAKey(t) // 不同密钥对，验签必失败
	tok := issueJWT(t, priv1, "opsmesh-gw", time.Now().Add(1*time.Hour), "t1", "u1", nil)
	cfg := JWTConfig{PublicKey: pub2, Issuer: "opsmesh-gw", Enabled: true}

	h := http.Header{}
	setBearer(h, tok)
	h.Set("X-Tenant-ID", "t-evil") // 即使有头注入也不应回退

	c, err := FromRequest(h, cfg)
	if err == nil {
		t.Fatal("token 验签失败应返回 error，不应回退头注入")
	}
	if c.TenantID != "" || c.UserID != "" {
		t.Fatalf("验签失败时 Context 应为零值，不应泄露头注入身份，got %+v", c)
	}
}

// TestFromRequest_Matrix_JWTEnabled_NoToken 启用 JWT 且未携带 token → 返回 ErrNoJWTToken。
// 这是本次安全加固的核心：修复前会回退头注入，攻击者可伪造任意租户身份。
func TestFromRequest_Matrix_JWTEnabled_NoToken(t *testing.T) {
	_, pub := testRSAKey(t)
	cfg := JWTConfig{PublicKey: pub, Issuer: "opsmesh-gw", Enabled: true}

	h := http.Header{}
	// 攻击者伪造头注入但省略 Authorization 头。
	h.Set("X-Tenant-ID", "t-evil")
	h.Set("X-User-Id", "u-evil")
	h.Set("X-User-Roles", "admin")

	c, err := FromRequest(h, cfg)
	if err == nil {
		t.Fatal("未携带 token 必须返回 error，不应回退头注入（越权风险）")
	}
	if !errors.Is(err, ErrNoJWTToken) {
		t.Fatalf("应返回 ErrNoJWTToken，got %v", err)
	}
	if c.TenantID != "" || c.UserID != "" || c.Roles != nil {
		t.Fatalf("Context 应为零值，不应泄露头注入身份，got %+v", c)
	}
}

// TestFromRequest_Matrix_JWTEnabled_NonBearerFormat 启用 JWT 且 Authorization 非 Bearer 格式 → 返回错误。
func TestFromRequest_Matrix_JWTEnabled_NonBearerFormat(t *testing.T) {
	_, pub := testRSAKey(t)
	cfg := JWTConfig{PublicKey: pub, Issuer: "opsmesh-gw", Enabled: true}

	h := http.Header{}
	h.Set("Authorization", "Basic abc")
	h.Set("X-Tenant-ID", "t-evil")

	c, err := FromRequest(h, cfg)
	if err == nil {
		t.Fatal("非 Bearer 格式应返回 error")
	}
	if c.TenantID != "" {
		t.Fatalf("Context 应为零值，got %+v", c)
	}
}

// TestFromRequest_Matrix_JWTDisabled_FallbackHeader 未启用 JWT → 回退头注入模式（保持兼容）。
func TestFromRequest_Matrix_JWTDisabled_FallbackHeader(t *testing.T) {
	_, pub := testRSAKey(t)
	cfg := JWTConfig{PublicKey: pub, Issuer: "opsmesh-gw", Enabled: false}

	h := http.Header{}
	h.Set("X-Tenant-ID", "t-header")
	h.Set("X-User-Id", "u-header")
	h.Set("X-User-Roles", "admin,ops")

	c, err := FromRequest(h, cfg)
	if err != nil {
		t.Fatalf("未启用 JWT 不应报错: %v", err)
	}
	if c.TenantID != "t-header" || c.UserID != "u-header" {
		t.Fatalf("应走头注入，got %+v", c)
	}
	if !sliceEqual(c.Roles, []string{"admin", "ops"}) {
		t.Fatalf("Roles = %v, want [admin ops]", c.Roles)
	}
}

// TestFromRequest_Matrix_NilPublicKey_FallbackHeader PublicKey==nil → 回退头注入模式。
// 防御性测试：配置装配错误（Enabled=true 但 PublicKey=nil）不应导致 panic 或拒绝服务。
func TestFromRequest_Matrix_NilPublicKey_FallbackHeader(t *testing.T) {
	cfg := JWTConfig{PublicKey: nil, Issuer: "", Enabled: true}
	h := http.Header{}
	h.Set("X-Tenant-ID", "t1")
	h.Set("X-User-Id", "u1")

	c, err := FromRequest(h, cfg)
	if err != nil {
		t.Fatalf("nil 公钥应安全回退头注入: %v", err)
	}
	if c.TenantID != "t1" || c.UserID != "u1" {
		t.Fatalf("回退头注入失败: %+v", c)
	}
}

// TestFromRequest_SecurityBypassAttack 模拟真实攻击场景：
// 攻击者知道目标租户 ID，尝试不带 token 仅通过伪造 X-Tenant-ID 头越权访问。
// 修复前：FromRequest 回退头注入，攻击者以 t-victim 身份成功访问。
// 修复后：FromRequest 返回 ErrNoJWTToken，调用方应 401 拒绝。
func TestFromRequest_SecurityBypassAttack(t *testing.T) {
	_, pub := testRSAKey(t)
	cfg := JWTConfig{PublicKey: pub, Issuer: "opsmesh-gw", Enabled: true}

	// 攻击者构造的请求：无 Authorization 头，伪造受害租户的头注入。
	attackHeader := http.Header{}
	attackHeader.Set("X-Tenant-ID", "t-victim")
	attackHeader.Set("X-User-Id", "u-attacker")
	attackHeader.Set("X-User-Roles", "admin")

	c, err := FromRequest(attackHeader, cfg)
	if err == nil {
		t.Fatal("攻击场景：未携带 token 但伪造头注入必须被拒绝（返回 error）")
	}
	if !errors.Is(err, ErrNoJWTToken) {
		t.Fatalf("应返回 ErrNoJWTToken，got %v", err)
	}
	// 关键断言：返回的 Context 不得包含攻击者伪造的身份。
	if c.TenantID == "t-victim" {
		t.Fatal("严重越权：返回的 Context 包含攻击者伪造的 t-victim 租户身份")
	}
	if c.UserID == "u-attacker" {
		t.Fatal("严重越权：返回的 Context 包含攻击者伪造的 u-attacker 用户身份")
	}
	if c.HasRole("admin") {
		t.Fatal("严重越权：返回的 Context 包含攻击者伪造的 admin 角色")
	}
}

// ============================================================================
// 辅助函数
// ============================================================================

// contains 判断 s 是否包含 substr（避免引入 strings 包仅为此一处使用）。
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (indexOf(s, substr) >= 0)
}

// indexOf 返回 substr 在 s 中首次出现的起始下标，未找到返回 -1。
func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// countByte 统计字符串中某字节的出现次数。
func countByte(s string, b byte) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			n++
		}
	}
	return n
}

// sliceEqual 比较两个字符串切片是否相等（元素顺序敏感）。
func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// base64URLEncode 将字节切片做 base64url 无填充编码（用于手工构造 JWT token）。
func base64URLEncode(src []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var out []byte
	for i := 0; i < len(src); i += 3 {
		b0 := uint(src[i])
		var b1, b2 uint
		n := 1
		if i+1 < len(src) {
			b1 = uint(src[i+1])
			n++
		}
		if i+2 < len(src) {
			b2 = uint(src[i+2])
			n++
		}
		out = append(out, alphabet[(b0>>2)&0x3F])
		out = append(out, alphabet[((b0<<4)|(b1>>4))&0x3F])
		if n >= 2 {
			out = append(out, alphabet[((b1<<2)|(b2>>6))&0x3F])
		}
		if n >= 3 {
			out = append(out, alphabet[b2&0x3F])
		}
	}
	return string(out)
}

// 防止编译器未使用导入的告警（保留 rand/rsa 供未来扩展使用）。
var _ = rand.Reader
var _ = rsa.GenerateKey
