package controlplane

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/store"
)

// 本文件补全 server_secrets.go 的单元测试。
// 覆盖：handleSecretsStatus、handleSecretsTest、isVaultNotFound、handleSecretsKeys、
// enumerateSecretKeys、keysFromSingleProvider、enumerateEnvKeys、enumerateFileKeys、
// flattenJSONKeys、dedupSecretKeys。

// newSecretsTestServer 构造测试 Server。
func newSecretsTestServer() *Server {
	st := store.NewMemoryStore()
	return &Server{
		store: st,
		cfg:   &config.Config{TaskMaxRetries: 3, Demo: true},
	}
}

// =============================================================================
// handleSecretsStatus
// =============================================================================

func TestHandleSecretsStatus(t *testing.T) {
	s := newSecretsTestServer()
	s.cfg.SecretProvider = "vault"
	s.cfg.VaultAddr = "http://vault:8200"
	s.cfg.VaultMount = "secret"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/status", nil)
	rec := httptest.NewRecorder()
	s.handleSecretsStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleSecretsStatus_MethodNotAllowed(t *testing.T) {
	s := newSecretsTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/status", nil)
	rec := httptest.NewRecorder()
	s.handleSecretsStatus(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post = %d, want 405", rec.Code)
	}
}

func TestHandleSecretsStatus_Disabled(t *testing.T) {
	s := newSecretsTestServer()
	// SecretProvider 空 → Enabled=false
	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/status", nil)
	rec := httptest.NewRecorder()
	s.handleSecretsStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// =============================================================================
// handleSecretsTest
// =============================================================================

func TestHandleSecretsTest_MethodNotAllowed(t *testing.T) {
	s := newSecretsTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/test", nil)
	rec := httptest.NewRecorder()
	s.handleSecretsTest(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("get = %d, want 405", rec.Code)
	}
}

func TestHandleSecretsTest_BadJSON(t *testing.T) {
	s := newSecretsTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	s.handleSecretsTest(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rec.Code)
	}
}

func TestHandleSecretsTest_MissingAddr(t *testing.T) {
	s := newSecretsTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.handleSecretsTest(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing addr = %d, want 400", rec.Code)
	}
}

func TestHandleSecretsTest_SSRFBlocked(t *testing.T) {
	s := newSecretsTestServer()
	// 私网地址 → SSRF 拒绝（返回 200 + ok=false）
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test", strings.NewReader(`{"addr":"http://10.0.0.1:8200"}`))
	rec := httptest.NewRecorder()
	s.handleSecretsTest(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ssrf = %d, want 200", rec.Code)
	}
}

func TestHandleSecretsTest_EmptyToken(t *testing.T) {
	s := newSecretsTestServer()
	// 公网地址但 token 空 → ok=false
	os.Unsetenv("OPSMESH_VAULT_TOKEN")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test", strings.NewReader(`{"addr":"https://vault.example.com:8200"}`))
	rec := httptest.NewRecorder()
	s.handleSecretsTest(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty token = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleSecretsTest_WithToken(t *testing.T) {
	s := newSecretsTestServer()
	os.Unsetenv("OPSMESH_VAULT_TOKEN")
	// 公网地址 + token → 构造 provider 并探测（会失败但覆盖代码）
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test", strings.NewReader(`{"addr":"https://vault.example.com:8200","token":"test-token"}`))
	rec := httptest.NewRecorder()
	s.handleSecretsTest(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("with token = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// isVaultNotFound
// =============================================================================

func TestIsVaultNotFound(t *testing.T) {
	if !isVaultNotFound("error 404 not found") {
		t.Fatal("404 should be true")
	}
	if !isVaultNotFound("secret not found") {
		t.Fatal("not found should be true")
	}
	if isVaultNotFound("connection refused") {
		t.Fatal("other error should be false")
	}
}

// =============================================================================
// handleSecretsKeys
// =============================================================================

func TestHandleSecretsKeys_MethodNotAllowed(t *testing.T) {
	s := newSecretsTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/keys", nil)
	rec := httptest.NewRecorder()
	s.handleSecretsKeys(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post = %d, want 405", rec.Code)
	}
}

func TestHandleSecretsKeys_Disabled(t *testing.T) {
	s := newSecretsTestServer()
	// SecretProvider 空 → 返回空列表
	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/keys", nil)
	rec := httptest.NewRecorder()
	s.handleSecretsKeys(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disabled = %d, want 200", rec.Code)
	}
}

func TestHandleSecretsKeys_EnvProvider(t *testing.T) {
	s := newSecretsTestServer()
	s.cfg.SecretProvider = "env"
	os.Setenv("OPSMESH_TEST_KEY_1", "v1")
	defer os.Unsetenv("OPSMESH_TEST_KEY_1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/keys", nil)
	rec := httptest.NewRecorder()
	s.handleSecretsKeys(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("env = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleSecretsKeys_VaultProvider(t *testing.T) {
	s := newSecretsTestServer()
	s.cfg.SecretProvider = "vault"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/keys", nil)
	rec := httptest.NewRecorder()
	s.handleSecretsKeys(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("vault = %d, want 200", rec.Code)
	}
}

func TestHandleSecretsKeys_ChainProvider(t *testing.T) {
	s := newSecretsTestServer()
	s.cfg.SecretProvider = "chain:env,vault"
	os.Setenv("OPSMESH_CHAIN_KEY", "v")
	defer os.Unsetenv("OPSMESH_CHAIN_KEY")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/keys", nil)
	rec := httptest.NewRecorder()
	s.handleSecretsKeys(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("chain = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// enumerateEnvKeys / flattenJSONKeys / dedupSecretKeys
// =============================================================================

func TestEnumerateEnvKeys(t *testing.T) {
	os.Setenv("OPSMESH_ENUM_TEST", "x")
	defer os.Unsetenv("OPSMESH_ENUM_TEST")
	keys := enumerateEnvKeys("OPSMESH_")
	found := false
	for _, k := range keys {
		if k.Key == "ENUM_TEST" && k.Provider == "env" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ENUM_TEST not found in %v", keys)
	}
}

func TestFlattenJSONKeys(t *testing.T) {
	data := map[string]interface{}{
		"a": map[string]interface{}{
			"b": "v1",
		},
		"c": "v2",
		"d": 123,
	}
	var out []secretsKeyEntry
	flattenJSONKeys(data, "", &out, "file")
	if len(out) != 3 {
		t.Fatalf("out = %v, want 3 entries", out)
	}
}

func TestDedupSecretKeys(t *testing.T) {
	entries := []secretsKeyEntry{
		{Key: "a", Provider: "env"},
		{Key: "b", Provider: "file"},
		{Key: "a", Provider: "vault"}, // 重复
	}
	out := dedupSecretKeys(entries)
	if len(out) != 2 {
		t.Fatalf("dedup = %v, want 2", out)
	}
	if out[0].Provider != "env" {
		t.Fatalf("first a should be from env, got %s", out[0].Provider)
	}
}

func TestEnumerateFileKeys_NoFile(t *testing.T) {
	s := newSecretsTestServer()
	s.cfg.SecretFile = ""
	if got := s.enumerateFileKeys(); got != nil {
		t.Fatalf("empty path = %v, want nil", got)
	}
}

func TestEnumerateFileKeys_NonexistentFile(t *testing.T) {
	s := newSecretsTestServer()
	s.cfg.SecretFile = "/nonexistent/path/secret.json"
	if got := s.enumerateFileKeys(); got != nil {
		t.Fatalf("nonexistent file = %v, want nil", got)
	}
}
