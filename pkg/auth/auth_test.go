package auth

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

const testSecret = "test-secret-key-at-least-32-bytes-long!"

// ============================================================================
// JWT 生成与校验
// ============================================================================

func TestGenerateServiceToken_HappyPath(t *testing.T) {
	claims := ServiceClaims{
		ServiceID:   "svc-001",
		ServiceName: "auth-svc",
		TenantID:    "tenant-abc",
		Permissions: []string{"read", "write"},
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	tok, err := GenerateServiceToken(claims, testSecret)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if tok == "" {
		t.Fatal("token 不应为空")
	}
}

func TestGenerateServiceToken_DefaultExpiry(t *testing.T) {
	claims := ServiceClaims{
		ServiceID: "svc-001",
	}
	tok, err := GenerateServiceToken(claims, testSecret)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	parsed, err := ValidateServiceToken(tok, testSecret)
	if err != nil {
		t.Fatalf("验签失败: %v", err)
	}
	if parsed.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt 不应为零值")
	}
	// 默认过期时间约 1 小时后。
	if time.Until(parsed.ExpiresAt) < 50*time.Minute {
		t.Fatalf("默认过期时间应约 1 小时，got %v", time.Until(parsed.ExpiresAt))
	}
}

func TestGenerateServiceToken_EmptySecret(t *testing.T) {
	_, err := GenerateServiceToken(ServiceClaims{}, "")
	if err == nil {
		t.Fatal("空密钥应报错")
	}
}

func TestValidateServiceToken_HappyPath(t *testing.T) {
	claims := ServiceClaims{
		ServiceID:   "svc-002",
		ServiceName: "device-svc",
		TenantID:    "tenant-xyz",
		Permissions: []string{"read", "admin"},
		ExpiresAt:   time.Now().Add(2 * time.Hour),
	}
	tok, err := GenerateServiceToken(claims, testSecret)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	parsed, err := ValidateServiceToken(tok, testSecret)
	if err != nil {
		t.Fatalf("验签失败: %v", err)
	}
	if parsed.ServiceID != claims.ServiceID {
		t.Fatalf("ServiceID = %q, want %q", parsed.ServiceID, claims.ServiceID)
	}
	if parsed.ServiceName != claims.ServiceName {
		t.Fatalf("ServiceName = %q, want %q", parsed.ServiceName, claims.ServiceName)
	}
	if parsed.TenantID != claims.TenantID {
		t.Fatalf("TenantID = %q, want %q", parsed.TenantID, claims.TenantID)
	}
	if len(parsed.Permissions) != 2 || parsed.Permissions[0] != "read" || parsed.Permissions[1] != "admin" {
		t.Fatalf("Permissions = %v, want [read admin]", parsed.Permissions)
	}
}

func TestValidateServiceToken_WrongSecret(t *testing.T) {
	tok, err := GenerateServiceToken(ServiceClaims{ServiceID: "svc-001"}, testSecret)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	_, err = ValidateServiceToken(tok, "wrong-secret-key-at-least-32-bytes!")
	if err == nil {
		t.Fatal("错误密钥应验签失败")
	}
}

func TestValidateServiceToken_Expired(t *testing.T) {
	claims := ServiceClaims{
		ServiceID: "svc-001",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	tok, err := GenerateServiceToken(claims, testSecret)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	_, err = ValidateServiceToken(tok, testSecret)
	if err == nil {
		t.Fatal("过期 token 应被拒绝")
	}
}

// ============================================================================
// Context 传播
// ============================================================================

func TestPropagateContext_RoundTrip(t *testing.T) {
	const token = "my-jwt-token"
	ctx := PropagateContext(context.Background(), token)
	got, ok := ExtractTokenFromContext(ctx)
	if !ok {
		t.Fatal("应能从 context 提取 token")
	}
	if got != token {
		t.Fatalf("token = %q, want %q", got, token)
	}
}

func TestExtractFromContext_Empty(t *testing.T) {
	_, err := ExtractFromContext(context.Background())
	if err == nil {
		t.Fatal("空 context 应报错")
	}
}

// ============================================================================
// HTTP 中间件
// ============================================================================

func TestAuthMiddleware_ValidToken(t *testing.T) {
	tok, _ := GenerateServiceToken(ServiceClaims{
		ServiceID:   "svc-001",
		Permissions: []string{"read"},
	}, testSecret)

	h := AuthMiddleware(testSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromRequest(r)
		if !ok || claims.ServiceID != "svc-001" {
			t.Fatalf("中间件未正确注入 claims")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	h := AuthMiddleware(testSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("不应到达 handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d, want 401", rec.Code)
	}
}

func TestRequirePermission_Granted(t *testing.T) {
	tok, _ := GenerateServiceToken(ServiceClaims{
		ServiceID:   "svc-001",
		Permissions: []string{"read", "write"},
	}, testSecret)

	h := AuthMiddleware(testSecret)(RequirePermission("write")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", rec.Code)
	}
}

func TestRequirePermission_Denied(t *testing.T) {
	tok, _ := GenerateServiceToken(ServiceClaims{
		ServiceID:   "svc-001",
		Permissions: []string{"read"},
	}, testSecret)

	h := AuthMiddleware(testSecret)(RequirePermission("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("不应到达 handler")
	})))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("状态码 = %d, want 403", rec.Code)
	}
}

func TestServiceAuthMiddleware_RequiresServiceID(t *testing.T) {
	// 无 service_id 的 token 应被拒绝。
	tok, _ := GenerateServiceToken(ServiceClaims{
		ServiceName: "no-id-svc",
	}, testSecret)

	h := ServiceAuthMiddleware(testSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("不应到达 handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d, want 401", rec.Code)
	}
}

// ============================================================================
// mTLS
// ============================================================================

func TestGenerateSelfCert_HappyPath(t *testing.T) {
	certPEM, keyPEM, err := GenerateSelfCert("localhost")
	if err != nil {
		t.Fatalf("生成自签证书失败: %v", err)
	}
	if len(certPEM) == 0 {
		t.Fatal("证书 PEM 为空")
	}
	if len(keyPEM) == 0 {
		t.Fatal("私钥 PEM 为空")
	}
	// 验证 PEM 可解析为有效 TLS 证书。
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("解析 PEM 证书失败: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("证书链为空")
	}
}

// ============================================================================
// gRPC 拦截器（验证 context 注入）
// ============================================================================

func TestUnaryServerInterceptor_ValidToken(t *testing.T) {
	tok, _ := GenerateServiceToken(ServiceClaims{
		ServiceID: "grpc-svc",
	}, testSecret)

	interceptor := UnaryServerInterceptor(testSecret)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(grpcTokenKey, tok))

	_, err := interceptor(ctx, nil, nil, func(ctx context.Context, req interface{}) (interface{}, error) {
		claims, ok := ClaimsFromContext(ctx)
		if !ok || claims.ServiceID != "grpc-svc" {
			t.Fatalf("拦截器未正确注入 claims")
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("拦截器不应报错: %v", err)
	}
}

func TestUnaryServerInterceptor_MissingMetadata(t *testing.T) {
	interceptor := UnaryServerInterceptor(testSecret)
	_, err := interceptor(context.Background(), nil, nil, func(ctx context.Context, req interface{}) (interface{}, error) {
		t.Fatal("不应到达 handler")
		return nil, nil
	})
	if err == nil {
		t.Fatal("缺失 metadata 应报错")
	}
}
