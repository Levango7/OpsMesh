// kms_test.go — KMSProvider 单元测试。
//
// 使用 httptest.NewServer 模拟 KMS API，覆盖解密成功/404/HTTP 错误/构造校验/
// factory 集成/token 环境变量回退等场景。

package secrets

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
)

// ============================================================================
// KMSProvider 单元测试
// ============================================================================

// TestKMSProvider_Get_Success mock KMS 返回 200 + {"plaintext":"..."} → 解密成功。
// 同时验证请求体格式（key_id/ciphertext）与 Bearer token 头。
func TestKMSProvider_Get_Success(t *testing.T) {
	var gotReq kmsDecryptRequest
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 校验请求路径。
		if r.URL.Path != "/decrypt" {
			t.Errorf("请求路径应为 /decrypt，实际: %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("请求方法应为 POST，实际: %q", r.Method)
		}
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotReq)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"plaintext": "super-secret-value"})
	}))
	defer ts.Close()

	p, err := NewKMSProvider(ts.URL, "cmk-1234", "bearer-token-abc")
	if err != nil {
		t.Fatalf("NewKMSProvider 失败: %v", err)
	}

	got, err := p.Get("Y2lwaGVydGV4dC1iYXNlNjQ=")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got != "super-secret-value" {
		t.Errorf("Get 返回 %q，期望 %q", got, "super-secret-value")
	}
	// 验证请求体。
	if gotReq.KeyID != "cmk-1234" {
		t.Errorf("请求体 key_id 应为 cmk-1234，实际: %q", gotReq.KeyID)
	}
	if gotReq.Ciphertext != "Y2lwaGVydGV4dC1iYXNlNjQ=" {
		t.Errorf("请求体 ciphertext 不匹配，实际: %q", gotReq.Ciphertext)
	}
	// 验证 Bearer token。
	if gotAuth != "Bearer bearer-token-abc" {
		t.Errorf("Authorization 头应为 Bearer bearer-token-abc，实际: %q", gotAuth)
	}
}

// TestKMSProvider_Get_NotFound mock 返回 404 → ErrSecretNotFound。
func TestKMSProvider_Get_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"ciphertext not found"}`))
	}))
	defer ts.Close()

	p, err := NewKMSProvider(ts.URL, "cmk-1", "tok")
	if err != nil {
		t.Fatalf("NewKMSProvider 失败: %v", err)
	}

	_, err = p.Get("nonexistent-ciphertext")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("HTTP 404 应返回 ErrSecretNotFound，实际: %v", err)
	}
}

// TestKMSProvider_Get_HTTPError mock 返回 500 → 包装错误（非 ErrSecretNotFound）。
func TestKMSProvider_Get_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal kms error"}`))
	}))
	defer ts.Close()

	p, err := NewKMSProvider(ts.URL, "cmk-1", "tok")
	if err != nil {
		t.Fatalf("NewKMSProvider 失败: %v", err)
	}

	_, err = p.Get("some-ciphertext")
	if err == nil {
		t.Fatalf("HTTP 500 应返回错误")
	}
	if errors.Is(err, ErrSecretNotFound) {
		t.Errorf("HTTP 500 不应返回 ErrSecretNotFound，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("错误信息应包含状态码 500，实际: %v", err)
	}
}

// TestKMSProvider_Get_EmptyPlaintext mock 返回 200 但 plaintext 为空 → ErrSecretNotFound。
func TestKMSProvider_Get_EmptyPlaintext(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"plaintext": ""})
	}))
	defer ts.Close()

	p, err := NewKMSProvider(ts.URL, "cmk-1", "tok")
	if err != nil {
		t.Fatalf("NewKMSProvider 失败: %v", err)
	}

	_, err = p.Get("some-ciphertext")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("空 plaintext 应返回 ErrSecretNotFound，实际: %v", err)
	}
}

// TestKMSProvider_Get_NoToken token 为空时不设置 Authorization 头（内网无鉴权场景）。
func TestKMSProvider_Get_NoToken(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"plaintext": "plain"})
	}))
	defer ts.Close()

	p, err := NewKMSProvider(ts.URL, "cmk-1", "") // token 留空
	if err != nil {
		t.Fatalf("NewKMSProvider 失败: %v", err)
	}

	got, err := p.Get("ct")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got != "plain" {
		t.Errorf("Get 返回 %q，期望 %q", got, "plain")
	}
	if gotAuth != "" {
		t.Errorf("token 为空时不应设置 Authorization 头，实际: %q", gotAuth)
	}
}

// TestKMSProvider_Name 验证返回 "kms"。
func TestKMSProvider_Name(t *testing.T) {
	p, err := NewKMSProvider("http://localhost:9999", "cmk-1", "tok")
	if err != nil {
		t.Fatalf("NewKMSProvider 失败: %v", err)
	}
	if p.Name() != "kms" {
		t.Errorf("Name 返回 %q，期望 %q", p.Name(), "kms")
	}
}

// TestNewKMSProvider_InvalidArgs endpoint/keyID 为空时返回错误。
func TestNewKMSProvider_InvalidArgs(t *testing.T) {
	_, err := NewKMSProvider("", "cmk-1", "tok")
	if err == nil {
		t.Errorf("空 endpoint 应返回错误")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("错误信息应提及 endpoint，实际: %v", err)
	}

	_, err = NewKMSProvider("http://localhost:9999", "", "tok")
	if err == nil {
		t.Errorf("空 keyID 应返回错误")
	}
	if !strings.Contains(err.Error(), "key_id") {
		t.Errorf("错误信息应提及 key_id，实际: %v", err)
	}
}

// ============================================================================
// buildKMS / FromConfig 集成测试（factory.go）
// ============================================================================

// TestBuildKMS_FromConfig spec="kms" 且配置完整 → KMSProvider。
func TestBuildKMS_FromConfig(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"plaintext": "decrypted"})
	}))
	defer ts.Close()

	cfg := &config.Config{
		SecretProvider: "kms",
		KmsEndpoint:    ts.URL,
		KmsKeyID:       "cmk-test",
		KmsToken:       "token-from-cfg",
	}
	p, err := FromConfig(cfg)
	if err != nil {
		t.Fatalf("FromConfig 失败: %v", err)
	}
	if p == nil || p.Name() != "kms" {
		t.Errorf("应返回 KMSProvider，实际: %v", p)
	}
	// 端到端验证解密可用。
	got, err := p.Get("Y2lwaGVydGV4dA==")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got != "decrypted" {
		t.Errorf("Get 返回 %q，期望 %q", got, "decrypted")
	}
}

// TestBuildKMS_TokenFromEnv KmsToken 为空时回退环境变量 OPSMESH_KMS_TOKEN。
func TestBuildKMS_TokenFromEnv(t *testing.T) {
	t.Setenv("OPSMESH_KMS_TOKEN", "env-kms-token")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证 token 来自环境变量。
		if r.Header.Get("Authorization") != "Bearer env-kms-token" {
			t.Errorf("Authorization 应为 Bearer env-kms-token，实际: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"plaintext": "ok"})
	}))
	defer ts.Close()

	cfg := &config.Config{KmsEndpoint: ts.URL, KmsKeyID: "cmk-1"} // KmsToken 留空
	p, err := buildKMS(cfg)
	if err != nil {
		t.Fatalf("buildKMS 失败: %v", err)
	}
	if p == nil || p.Name() != "kms" {
		t.Errorf("应返回 KMSProvider，实际: %v", p)
	}
	// 触发一次请求以验证 token 注入。
	if _, err := p.Get("ct"); err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
}

// TestBuildKMS_EmptyEndpoint KmsEndpoint 为空 → 错误。
func TestBuildKMS_EmptyEndpoint(t *testing.T) {
	os.Unsetenv("OPSMESH_KMS_TOKEN")
	cfg := &config.Config{KmsKeyID: "cmk-1"}
	_, err := buildKMS(cfg)
	if err == nil {
		t.Errorf("空 KmsEndpoint 应返回错误")
	}
	if !strings.Contains(err.Error(), "kms-endpoint") {
		t.Errorf("错误信息应提及 kms-endpoint，实际: %v", err)
	}
}

// TestBuildKMS_EmptyKeyID KmsKeyID 为空 → 错误。
func TestBuildKMS_EmptyKeyID(t *testing.T) {
	os.Unsetenv("OPSMESH_KMS_TOKEN")
	cfg := &config.Config{KmsEndpoint: "http://localhost:9999"}
	_, err := buildKMS(cfg)
	if err == nil {
		t.Errorf("空 KmsKeyID 应返回错误")
	}
	if !strings.Contains(err.Error(), "kms-key-id") {
		t.Errorf("错误信息应提及 kms-key-id，实际: %v", err)
	}
}

// TestBuildKMS_NoTokenAllowed token 为空且无环境变量 → 不报错（内网无鉴权 KMS 允许）。
func TestBuildKMS_NoTokenAllowed(t *testing.T) {
	os.Unsetenv("OPSMESH_KMS_TOKEN")
	cfg := &config.Config{KmsEndpoint: "http://localhost:9999", KmsKeyID: "cmk-1"}
	p, err := buildKMS(cfg)
	if err != nil {
		t.Fatalf("KMS 允许 token 为空（内网无鉴权），不应报错，实际: %v", err)
	}
	if p == nil || p.Name() != "kms" {
		t.Errorf("应返回 KMSProvider，实际: %v", p)
	}
}

// TestFromConfig_KMS spec="kms" 经 FromConfig 顶层入口构造（与 buildKMS 互补）。
func TestFromConfig_KMS(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"plaintext": "v"})
	}))
	defer ts.Close()

	cfg := &config.Config{
		SecretProvider: "kms",
		KmsEndpoint:    ts.URL,
		KmsKeyID:       "cmk",
		KmsToken:       "tok",
	}
	p, err := FromConfig(cfg)
	if err != nil {
		t.Fatalf("FromConfig 失败: %v", err)
	}
	if p == nil || p.Name() != "kms" {
		t.Errorf("应返回 KMSProvider，实际: %v", p)
	}
}

// TestFromConfig_Chain_KMSChild chain:env,kms 子 provider 为 kms。
func TestFromConfig_Chain_KMSChild(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"plaintext": "v"})
	}))
	defer ts.Close()

	cfg := &config.Config{
		SecretProvider: "chain:env,kms",
		KmsEndpoint:    ts.URL,
		KmsKeyID:       "cmk",
		KmsToken:       "tok",
	}
	p, err := FromConfig(cfg)
	if err != nil {
		t.Fatalf("FromConfig 失败: %v", err)
	}
	if p == nil || p.Name() != "chain" {
		t.Errorf("应返回 ChainProvider，实际: %v", p)
	}
}
