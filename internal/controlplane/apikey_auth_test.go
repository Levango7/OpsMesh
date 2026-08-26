// apikey_auth_test.go 测试 H5 API Key 认证接入（requireProd 第 2.5 路 + authorizeByAPIKey）。
//
// 用例矩阵（FIXPLAN §2.3.4 防回归清单）：
//
//	a) 有效 key + 匹配租户 + scope 足够        → 放行（caller=nil）
//	b) 无效 key（不存在/格式错）               → 401 "invalid api key"
//	c) 禁用 key（Enabled=false）               → 401
//	d) 过期 key（ExpiresAt 早于 now）          → 401
//	e) X-Tenant-ID 与 key 归属租户不一致       → 403 tenant mismatch（越权防线）
//	f) scope 不足                              → 403 insufficient scope
//	g) Scopes 为空的 key 放行全部（platform 向后兼容语义；将由 M2 收紧）
//
// 附加覆盖：X-API-Key 头路径、om_ 前缀优先于 JWT 分发、LastUsedAt 内存聚合计数。
package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/config"
	"opsmesh/internal/platform"
	"opsmesh/internal/store"
)

// newAPIKeyAuthTestServer 构造非 demo 模式的 API Key 认证测试 Server。
// demo 必须 false：demo 模式对无身份请求宽松放行，会掩盖 API Key 分支的认证/授权行为。
func newAPIKeyAuthTestServer() *Server {
	st := store.NewMemoryStore()
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{Demo: false},
		sessionStore: ss,
		jwtSecret:    []byte("test-jwt-secret-for-apikey-auth-32bytes!"),
		loginGuard:   newLoginGuard(ss),
		apiKeyMgr:    platform.NewAPIKeyManager(st),
		apiKeyUsage:  make(map[string]int64),
	}
}

// createTestAPIKey 经 platform.GenerateAPIKey 生成明文/hash 并存入 MemoryStore，
// 返回明文 key（仅测试持有）。scopes 传 nil 即空 Scopes（向后兼容=全权限）。
func createTestAPIKey(t *testing.T, s *Server, tenant, name string, scopes []string, enabled bool, expires time.Time) string {
	t.Helper()
	plain, hash, err := platform.GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	created := s.store.CreateAPIKey(tenant, &store.APIKey{
		Name:      name,
		Key:       hash,
		Scopes:    scopes,
		Enabled:   enabled,
		ExpiresAt: expires,
	})
	if created == nil {
		t.Fatal("CreateAPIKey returned nil")
	}
	return plain
}

// callRequireProdWithAPIKey 以给定凭据头调用 requireProd，返回 (caller, ok, recorder)。
func callRequireProdWithAPIKey(s *Server, authHeader, xAPIKey, tenantHeader, required string) (*store.User, bool, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	if xAPIKey != "" {
		req.Header.Set("X-API-Key", xAPIKey)
	}
	if tenantHeader != "" {
		req.Header.Set("X-Tenant-ID", tenantHeader)
	}
	rec := httptest.NewRecorder()
	user, ok := s.requireProd(rec, req, required)
	return user, ok, rec
}

// decodeError 解析统一错误响应 {"error": "..."}。
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return body.Error
}

// TestApiKeyAuth_ValidKeyAllowed 用例 a：有效 key + 匹配租户 + scope 足够 → 放行，
// 且 caller 为 nil（与联邦路径一致，API Key 调用者非用户）。
func TestApiKeyAuth_ValidKeyAllowed(t *testing.T) {
	s := newAPIKeyAuthTestServer()
	plain := createTestAPIKey(t, s, "tenant-a", "ci-key", []string{"device:read", "task:write"}, true, time.Time{})

	user, ok, rec := callRequireProdWithAPIKey(s, "Bearer "+plain, "", "tenant-a", "device:read")
	if !ok {
		t.Fatalf("valid api key should pass, code=%d body=%s", rec.Code, rec.Body.String())
	}
	if user != nil {
		t.Fatalf("caller should be nil (non-user identity), got %+v", user)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("no error response expected, code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestApiKeyAuth_NoTenantHeaderFallsBackToKeyTenant 补充用例 a：
// 不带 X-Tenant-ID 头时取 key 自身租户，同样放行。
func TestApiKeyAuth_NoTenantHeaderFallsBackToKeyTenant(t *testing.T) {
	s := newAPIKeyAuthTestServer()
	plain := createTestAPIKey(t, s, "tenant-a", "ci-key", []string{"device:read"}, true, time.Time{})

	if _, ok, rec := callRequireProdWithAPIKey(s, "Bearer "+plain, "", "", "device:read"); !ok {
		t.Fatalf("missing X-Tenant-ID should fall back to key tenant and pass, code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestApiKeyAuth_XAPIKeyHeaderAllowed 补充用例 a：X-API-Key 头路径同样放行
// （H5 前"有钥匙孔无锁"，现统一收口到 authorizeByAPIKey）。
func TestApiKeyAuth_XAPIKeyHeaderAllowed(t *testing.T) {
	s := newAPIKeyAuthTestServer()
	plain := createTestAPIKey(t, s, "tenant-a", "ci-key", []string{"device:read"}, true, time.Time{})

	user, ok, rec := callRequireProdWithAPIKey(s, "", plain, "tenant-a", "device:read")
	if !ok || user != nil || rec.Code != http.StatusOK {
		t.Fatalf("X-API-Key header path should pass: ok=%v user=%v code=%d body=%s", ok, user, rec.Code, rec.Body.String())
	}
}

// TestApiKeyAuth_InvalidKeyRejected 用例 b：无效 key（hash 不在库存）→ 401。
// 同时验证 om_ 前缀分发优先于 JWT：错误文案为 "invalid api key" 而非 JWT 校验错误，
// 证明请求走了 API Key 分支而非被 ParseHSJWT 拒绝。
func TestApiKeyAuth_InvalidKeyRejected(t *testing.T) {
	s := newAPIKeyAuthTestServer()

	_, ok, rec := callRequireProdWithAPIKey(s, "Bearer om_"+"0000000000000000000000000000dead", "", "tenant-a", "device:read")
	if ok {
		t.Fatal("unknown api key must not pass")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec); got != "invalid api key" {
		t.Fatalf("error=%q, want %q（证明走 API Key 分支而非 JWT 分支）", got, "invalid api key")
	}
}

// TestApiKeyAuth_MalformedKeyRejected 补充用例 b：非 om_ 格式明文经 X-API-Key
// 注入（长度/前缀不符）同样 401。
func TestApiKeyAuth_MalformedKeyRejected(t *testing.T) {
	s := newAPIKeyAuthTestServer()

	for _, bad := range []string{"sk-not-an-apikey", "om_short", strings.Repeat("x", 35)} {
		_, ok, rec := callRequireProdWithAPIKey(s, "", bad, "tenant-a", "device:read")
		if ok || rec.Code != http.StatusUnauthorized {
			t.Fatalf("malformed key %q: ok=%v code=%d, want reject with 401; body=%s", bad, ok, rec.Code, rec.Body.String())
		}
	}
}

// TestApiKeyAuth_DisabledKeyRejected 用例 c：禁用 key → 401
// （ValidateKey 拒绝 + authorizeByAPIKey Enabled 双保险均应拦截）。
func TestApiKeyAuth_DisabledKeyRejected(t *testing.T) {
	s := newAPIKeyAuthTestServer()
	plain := createTestAPIKey(t, s, "tenant-a", "revoked-key", []string{"device:read"}, false, time.Time{})

	_, ok, rec := callRequireProdWithAPIKey(s, "Bearer "+plain, "", "tenant-a", "device:read")
	if ok {
		t.Fatal("disabled api key must not pass")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec); got != "invalid api key" {
		t.Fatalf("error=%q, want %q", got, "invalid api key")
	}
}

// TestApiKeyAuth_ExpiredKeyRejected 用例 d：过期 key → 401
// （ExpiresAt 非零且早于 now；ValidateKey 与双保险两层均校验）。
func TestApiKeyAuth_ExpiredKeyRejected(t *testing.T) {
	s := newAPIKeyAuthTestServer()
	expired := time.Now().Add(-time.Hour)
	plain := createTestAPIKey(t, s, "tenant-a", "stale-key", []string{"device:read"}, true, expired)

	_, ok, rec := callRequireProdWithAPIKey(s, "Bearer "+plain, "", "tenant-a", "device:read")
	if ok {
		t.Fatal("expired api key must not pass")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec); got != "invalid api key" {
		t.Fatalf("error=%q, want %q", got, "invalid api key")
	}
}

// TestApiKeyAuth_TenantMismatchRejected 用例 e：X-Tenant-ID 与 key 归属租户不一致
// → 403 tenant mismatch（关键越权防线：A 租户 key 不得操作 B 租户资源）。
func TestApiKeyAuth_TenantMismatchRejected(t *testing.T) {
	s := newAPIKeyAuthTestServer()
	plain := createTestAPIKey(t, s, "tenant-a", "cross-tenant-key", []string{"device:read"}, true, time.Time{})

	_, ok, rec := callRequireProdWithAPIKey(s, "Bearer "+plain, "", "tenant-b", "device:read")
	if ok {
		t.Fatal("tenant mismatch must not pass")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code=%d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	want := "tenant mismatch between X-Tenant-ID header and api key tenant"
	if got := decodeError(t, rec); got != want {
		t.Fatalf("error=%q, want %q", got, want)
	}
}

// TestApiKeyAuth_InsufficientScopeRejected 用例 f：scope 不足 → 403 insufficient scope。
func TestApiKeyAuth_InsufficientScopeRejected(t *testing.T) {
	s := newAPIKeyAuthTestServer()
	plain := createTestAPIKey(t, s, "tenant-a", "readonly-key", []string{"device:read"}, true, time.Time{})

	_, ok, rec := callRequireProdWithAPIKey(s, "Bearer "+plain, "", "tenant-a", "device:delete")
	if ok {
		t.Fatal("insufficient scope must not pass")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code=%d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec); got != "insufficient scope" {
		t.Fatalf("error=%q, want %q", got, "insufficient scope")
	}
}

// TestApiKeyAuth_EmptyScopesAllowAll 用例 g：Scopes 为空的 key 放行全部权限点——
// platform.HasScope 的向后兼容语义（避免老 key 失效），叠加 M2 的 PUT 清空漏洞即提权通道，
// 将由 M2（PUT 白名单）收紧，此处锁定当前行为防意外漂移。
func TestApiKeyAuth_EmptyScopesAllowAll(t *testing.T) {
	s := newAPIKeyAuthTestServer()
	plain := createTestAPIKey(t, s, "tenant-a", "legacy-key", nil, true, time.Time{})

	for _, required := range []string{"device:read", "device:delete", "task:approve", "apikey:write"} {
		user, ok, rec := callRequireProdWithAPIKey(s, "Bearer "+plain, "", "tenant-a", required)
		if !ok || user != nil || rec.Code != http.StatusOK {
			t.Fatalf("empty scopes key should allow %q: ok=%v user=%v code=%d body=%s",
				required, ok, user, rec.Code, rec.Body.String())
		}
	}
}

// TestApiKeyAuth_JWTPathUnaffected 防回归：普通 Bearer JWT 不被 API Key 分支劫持。
// 非 om_ token 走原 JWT 路径（此处以无效 JWT 验证其错误来自 JWT 校验而非 "invalid api key"）。
func TestApiKeyAuth_JWTPathUnaffected(t *testing.T) {
	s := newAPIKeyAuthTestServer()

	_, ok, rec := callRequireProdWithAPIKey(s, "Bearer not-a-real-jwt-token", "", "default", "device:read")
	if ok {
		t.Fatal("invalid jwt must not pass")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec); got == "invalid api key" {
		t.Fatal("non-om_ bearer token must be handled by JWT path, not api key path")
	}
}

// TestApiKeyAuth_UsageCounted LastUsedAt 内存聚合：成功认证一次计数 +1，
// 多次累计；失败认证（无效 key）不计数。MVP 仅内存累计不落库。
func TestApiKeyAuth_UsageCounted(t *testing.T) {
	s := newAPIKeyAuthTestServer()
	plain := createTestAPIKey(t, s, "tenant-a", "counted-key", []string{"device:read"}, true, time.Time{})
	id := func() string {
		for _, k := range s.store.ListAPIKeys("tenant-a") {
			if k.Name == "counted-key" {
				return k.ID
			}
		}
		t.Fatal("created key not found")
		return ""
	}()

	if got := s.apiKeyUsageCount(id); got != 0 {
		t.Fatalf("initial usage=%d, want 0", got)
	}
	for i := 1; i <= 3; i++ {
		if _, ok, _ := callRequireProdWithAPIKey(s, "Bearer "+plain, "", "tenant-a", "device:read"); !ok {
			t.Fatalf("request #%d should pass", i)
		}
		if got := s.apiKeyUsageCount(id); got != int64(i) {
			t.Fatalf("usage after #%d request=%d, want %d", i, got, i)
		}
	}
	// 失败请求不计数。
	callRequireProdWithAPIKey(s, "Bearer om_"+"ffffffffffffffffffffffffffffdead", "", "tenant-a", "device:read")
	if got := s.apiKeyUsageCount(id); got != 3 {
		t.Fatalf("failed attempts must not count, usage=%d want 3", got)
	}
}
