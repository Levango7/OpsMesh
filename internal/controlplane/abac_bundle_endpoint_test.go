// abac_bundle_endpoint_test.go 验证 agent 策略包分发端点的契约（M1）：
// 认证、租户隔离、签名可验证、ETag/304、空策略与未配置密钥的降级行为。
package controlplane

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
	"github.com/Levango7/OpsMesh/pkg/abacbundle"
)

// abacBundleTestFixedNow 是测试用的固定时钟（UTC）。
var abacBundleTestFixedNow = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

// newABACBundleTestServer 构造带签名密钥与内存 store 的测试 Server，返回公钥供验签。
func newABACBundleTestServer(t *testing.T) (*Server, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	st := store.NewMemoryStore()
	s := &Server{
		store: st,
		cfg: &config.Config{
			ABACBundleSigningKey: base64.StdEncoding.EncodeToString(priv),
			ABACBundleKeyID:      "k1",
			ABACBundleTTL:        24 * time.Hour,
		},
		abacBundleNow: func() time.Time { return abacBundleTestFixedNow },
	}
	return s, pub
}

// abacBundleTestRegisterAgent 注册一个租户下的 agent 并返回其 ID。
func abacBundleTestRegisterAgent(t *testing.T, s *Server, tenantID string) string {
	t.Helper()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-test", TenantID: tenantID})
	if a == nil || a.AgentID == "" {
		t.Fatal("register agent failed")
	}
	return a.AgentID
}

// abacBundleTestRequest 构造带 per-agent HMAC 三头的 GET 请求。
func abacBundleTestRequest(t *testing.T, s *Server, agentID, ifNoneMatch string) *http.Request {
	t.Helper()
	secret := s.store.AgentSecret(agentID)
	if secret == "" {
		t.Fatalf("agent %q has no secret", agentID)
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(abacBundleSignaturePrefix + "\n" + agentID + "\n" + ts))
	req := httptest.NewRequest(http.MethodGet, abacBundleRoute, nil)
	req.Header.Set(abacBundleAgentIDHeader, agentID)
	req.Header.Set(abacBundleAgentTimestampHeader, ts)
	req.Header.Set(abacBundleAgentSignatureHeader, hex.EncodeToString(mac.Sum(nil)))
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	return req
}

// TestABACBundle_AuthorizedAgentGetsVerifiableBundle 覆盖：授权 agent 200 + 解码 + 验签。
func TestABACBundle_AuthorizedAgentGetsVerifiableBundle(t *testing.T) {
	s, pub := newABACBundleTestServer(t)
	agentID := abacBundleTestRegisterAgent(t, s, "tenant-a")
	if err := s.addABACPolicy(abacPolicy{
		ID: "p-a1", TenantID: "tenant-a", Subject: "alice", Resource: "vm:*", Action: "read", Effect: "allow",
	}); err != nil {
		t.Fatalf("add policy: %v", err)
	}

	rec := httptest.NewRecorder()
	s.handleAgentABACBundle(rec, abacBundleTestRequest(t, s, agentID, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got == "" {
		t.Fatal("missing Content-Type")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control=%q, want no-cache", got)
	}

	signed, err := abacbundle.Decode(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if signed.Bundle.SchemaVersion != abacbundle.SchemaVersion {
		t.Fatalf("schemaVersion=%d", signed.Bundle.SchemaVersion)
	}
	if signed.Bundle.TenantScope != "tenant-a" {
		t.Fatalf("tenantScope=%q, want tenant-a", signed.Bundle.TenantScope)
	}
	if signed.Bundle.KeyID != "k1" {
		t.Fatalf("keyId=%q, want k1", signed.Bundle.KeyID)
	}
	if len(signed.Bundle.Policies) != 1 || signed.Bundle.Policies[0].ID != "p-a1" {
		t.Fatalf("policies=%+v", signed.Bundle.Policies)
	}
	if err := abacbundle.VerifyAt(signed, pub, abacBundleTestFixedNow); err != nil {
		t.Fatalf("signature verify: %v", err)
	}

	digest, err := abacbundle.Digest(signed.Bundle)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if want := `"` + digest + `"`; rec.Header().Get("ETag") != want {
		t.Fatalf("ETag=%q, want %q", rec.Header().Get("ETag"), want)
	}
	if rec.Header().Get("X-OpsMesh-Bundle-Key-Id") != "k1" {
		t.Fatalf("X-OpsMesh-Bundle-Key-Id=%q", rec.Header().Get("X-OpsMesh-Bundle-Key-Id"))
	}
}

// TestABACBundle_ETagStableAndNotModified 覆盖：同策略集合 ETag 稳定 + If-None-Match 304。
func TestABACBundle_ETagStableAndNotModified(t *testing.T) {
	s, _ := newABACBundleTestServer(t)
	agentID := abacBundleTestRegisterAgent(t, s, "tenant-a")
	if err := s.addABACPolicy(abacPolicy{
		ID: "p-a1", TenantID: "tenant-a", Subject: "alice", Resource: "vm:*", Action: "read", Effect: "allow",
	}); err != nil {
		t.Fatalf("add policy: %v", err)
	}

	first := httptest.NewRecorder()
	s.handleAgentABACBundle(first, abacBundleTestRequest(t, s, agentID, ""))
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}

	second := httptest.NewRecorder()
	s.handleAgentABACBundle(second, abacBundleTestRequest(t, s, agentID, ""))
	if second.Code != http.StatusOK {
		t.Fatalf("second status=%d", second.Code)
	}
	if got := second.Header().Get("ETag"); got != etag {
		t.Fatalf("ETag changed for unchanged policy set: %q -> %q", etag, got)
	}

	third := httptest.NewRecorder()
	s.handleAgentABACBundle(third, abacBundleTestRequest(t, s, agentID, etag))
	if third.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match status=%d, want 304 (body=%s)", third.Code, third.Body.String())
	}
	if third.Body.Len() != 0 {
		t.Fatalf("304 must have empty body, got %q", third.Body.String())
	}
	if got := third.Header().Get("ETag"); got != etag {
		t.Fatalf("304 ETag=%q, want %q", got, etag)
	}
	if got := third.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("304 Cache-Control=%q, want no-cache", got)
	}
}

// TestABACBundle_PoliciesAreTenantScoped 覆盖：A 永不看到 B 的策略，平台策略对所有租户可见。
func TestABACBundle_PoliciesAreTenantScoped(t *testing.T) {
	s, _ := newABACBundleTestServer(t)
	agentA := abacBundleTestRegisterAgent(t, s, "tenant-a")
	agentB := abacBundleTestRegisterAgent(t, s, "tenant-b")

	for _, p := range []abacPolicy{
		{ID: "p-a", TenantID: "tenant-a", Subject: "a", Resource: "r", Action: "x", Effect: "allow"},
		{ID: "p-b", TenantID: "tenant-b", Subject: "b", Resource: "r", Action: "x", Effect: "allow"},
		{ID: "p-global", TenantID: "", Subject: "g", Resource: "r", Action: "x", Effect: "allow"},
	} {
		if err := s.addABACPolicy(p); err != nil {
			t.Fatalf("add policy %s: %v", p.ID, err)
		}
	}

	idsFor := func(agentID string) map[string]bool {
		rec := httptest.NewRecorder()
		s.handleAgentABACBundle(rec, abacBundleTestRequest(t, s, agentID, ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("agent %s status=%d body=%s", agentID, rec.Code, rec.Body.String())
		}
		signed, err := abacbundle.Decode(rec.Body.Bytes())
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		out := make(map[string]bool, len(signed.Bundle.Policies))
		for _, p := range signed.Bundle.Policies {
			out[p.ID] = true
		}
		return out
	}

	a := idsFor(agentA)
	if !a["p-a"] || a["p-b"] || !a["p-global"] {
		t.Fatalf("tenant-a policies=%v, want p-a + p-global and no p-b", a)
	}
	b := idsFor(agentB)
	if !b["p-b"] || b["p-a"] || !b["p-global"] {
		t.Fatalf("tenant-b policies=%v, want p-b + p-global and no p-a", b)
	}
}

// TestABACBundle_MissingSigningKey503 覆盖：未配置密钥时端点 503 且给出明确说明。
func TestABACBundle_MissingSigningKey503(t *testing.T) {
	s, _ := newABACBundleTestServer(t)
	s.cfg.ABACBundleSigningKey = ""
	agentID := abacBundleTestRegisterAgent(t, s, "tenant-a")

	rec := httptest.NewRecorder()
	s.handleAgentABACBundle(rec, abacBundleTestRequest(t, s, agentID, ""))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503 (body=%s)", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["code"] != "signing_not_configured" {
		t.Fatalf("code=%q, want signing_not_configured", body["code"])
	}
	if body["error"] == "" {
		t.Fatal("503 must carry a clear error message")
	}
	if got := rec.Header().Get("Retry-After"); got != "30" {
		t.Fatalf("Retry-After=%q, want 30", got)
	}
}

// TestABACBundle_Unauthenticated401 覆盖：无凭证/错签名统一 401（与 agent 端点同形）。
func TestABACBundle_Unauthenticated401(t *testing.T) {
	s, _ := newABACBundleTestServer(t)
	agentID := abacBundleTestRegisterAgent(t, s, "tenant-a")

	// 无任何 agent 身份头。
	rec := httptest.NewRecorder()
	s.handleAgentABACBundle(rec, httptest.NewRequest(http.MethodGet, abacBundleRoute, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no headers status=%d, want 401", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "unauthorized" {
		t.Fatalf("error=%q, want unauthorized", body["error"])
	}

	// 错误签名。
	req := abacBundleTestRequest(t, s, agentID, "")
	req.Header.Set(abacBundleAgentSignatureHeader, "deadbeef")
	rec = httptest.NewRecorder()
	s.handleAgentABACBundle(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature status=%d, want 401", rec.Code)
	}

	// 未知 agent（与签名错同形，不泄露存在性）。
	req = abacBundleTestRequest(t, s, agentID, "")
	req.Header.Set(abacBundleAgentIDHeader, "ghost")
	rec = httptest.NewRecorder()
	s.handleAgentABACBundle(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown agent status=%d, want 401", rec.Code)
	}
}

// TestABACBundle_EmptyPolicySetStillSigned 覆盖：无策略时仍返回 200 + 可验签空 bundle。
func TestABACBundle_EmptyPolicySetStillSigned(t *testing.T) {
	s, pub := newABACBundleTestServer(t)
	agentID := abacBundleTestRegisterAgent(t, s, "tenant-empty")

	rec := httptest.NewRecorder()
	s.handleAgentABACBundle(rec, abacBundleTestRequest(t, s, agentID, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	signed, err := abacbundle.Decode(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(signed.Bundle.Policies) != 0 {
		t.Fatalf("policies=%+v, want empty", signed.Bundle.Policies)
	}
	if signed.Bundle.TenantScope != "tenant-empty" {
		t.Fatalf("tenantScope=%q", signed.Bundle.TenantScope)
	}
	if err := abacbundle.VerifyAt(signed, pub, abacBundleTestFixedNow); err != nil {
		t.Fatalf("signature verify: %v", err)
	}
}

// TestABACBundle_MethodNotAllowed 覆盖：非 GET 方法 405。
func TestABACBundle_MethodNotAllowed(t *testing.T) {
	s, _ := newABACBundleTestServer(t)
	rec := httptest.NewRecorder()
	s.handleAgentABACBundle(rec, httptest.NewRequest(http.MethodPost, abacBundleRoute, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// TestABACBundle_ProductionDefaultDenyReachesAgentHandler 覆盖生产链路：
// --require-auth=true 时默认拒绝中间件不得用 JWT 规则拦掉 agent HMAC 通道，
// 请求必须到达 handler 并由 HMAC 认证决定 200/401。
func TestABACBundle_ProductionDefaultDenyReachesAgentHandler(t *testing.T) {
	s, _ := newABACBundleTestServer(t)
	s.requireAuth = true
	agentID := abacBundleTestRegisterAgent(t, s, "tenant-a")

	mux := http.NewServeMux()
	mux.HandleFunc(abacBundleRoute, s.handleAgentABACBundle)
	handler := s.defaultDenyMiddleware(mux)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, abacBundleTestRequest(t, s, agentID, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("authorized agent through defaultDeny status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, abacBundleRoute, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated through defaultDeny status=%d, want 401", rec.Code)
	}
}
