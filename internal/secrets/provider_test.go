package secrets

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// ============================================================================
// EnvProvider 测试
// ============================================================================

func TestEnvProvider(t *testing.T) {
	t.Setenv("OPSMESH_TEST_KEY", "test-value")

	p := NewEnvProvider("")
	got, err := p.Get("OPSMESH_TEST_KEY")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got != "test-value" {
		t.Errorf("Get 返回 %q，期望 %q", got, "test-value")
	}
	if p.Name() != "env" {
		t.Errorf("Name 返回 %q，期望 %q", p.Name(), "env")
	}
}

func TestEnvProvider_NotFound(t *testing.T) {
	// 确保环境变量未设置。
	os.Unsetenv("OPSMESH_NOT_SET_KEY_265")

	p := NewEnvProvider("")
	_, err := p.Get("OPSMESH_NOT_SET_KEY_265")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("未设置的环境变量应返回 ErrSecretNotFound，实际: %v", err)
	}
}

func TestEnvProvider_Prefix(t *testing.T) {
	t.Setenv("OPSMESH_MY_KEY", "prefixed-value")

	p := NewEnvProvider("OPSMESH_")
	got, err := p.Get("MY_KEY")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got != "prefixed-value" {
		t.Errorf("Get 返回 %q，期望 %q", got, "prefixed-value")
	}
}

// ============================================================================
// FileProvider 测试
// ============================================================================

func TestFileProvider(t *testing.T) {
	// 构造嵌套 JSON：扁平 + 嵌套 key。
	data := map[string]interface{}{
		"flat_key": "flat-value",
		"notify": map[string]interface{}{
			"dingtalk": map[string]interface{}{
				"webhook_url": "https://oapi.dingtalk.com/robot/send?access_token=xxx",
				"secret":      "SECxxx",
			},
		},
	}
	path := writeTempJSON(t, data)

	p, err := NewFileProvider(path)
	if err != nil {
		t.Fatalf("NewFileProvider 失败: %v", err)
	}
	if p.Name() != "file" {
		t.Errorf("Name 返回 %q，期望 %q", p.Name(), "file")
	}

	// 扁平 key。
	got, err := p.Get("flat_key")
	if err != nil {
		t.Fatalf("Get flat_key 失败: %v", err)
	}
	if got != "flat-value" {
		t.Errorf("Get flat_key 返回 %q，期望 %q", got, "flat-value")
	}

	// 嵌套 key。
	got, err = p.Get("notify/dingtalk/webhook_url")
	if err != nil {
		t.Fatalf("Get notify/dingtalk/webhook_url 失败: %v", err)
	}
	want := "https://oapi.dingtalk.com/robot/send?access_token=xxx"
	if got != want {
		t.Errorf("Get notify/dingtalk/webhook_url 返回 %q，期望 %q", got, want)
	}

	got, err = p.Get("notify/dingtalk/secret")
	if err != nil {
		t.Fatalf("Get notify/dingtalk/secret 失败: %v", err)
	}
	if got != "SECxxx" {
		t.Errorf("Get notify/dingtalk/secret 返回 %q，期望 %q", got, "SECxxx")
	}
}

func TestFileProvider_NotFound(t *testing.T) {
	data := map[string]interface{}{"existing": "value"}
	path := writeTempJSON(t, data)

	p, err := NewFileProvider(path)
	if err != nil {
		t.Fatalf("NewFileProvider 失败: %v", err)
	}

	// 不存在的 key。
	_, err = p.Get("nonexistent")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("不存在的 key 应返回 ErrSecretNotFound，实际: %v", err)
	}

	// 部分匹配但下钻到非 map。
	_, err = p.Get("existing/sub")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("下钻到非 map 应返回 ErrSecretNotFound，实际: %v", err)
	}

	// 嵌套不存在的 key。
	_, err = p.Get("notify/dingtalk/webhook_url")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("嵌套不存在的 key 应返回 ErrSecretNotFound，实际: %v", err)
	}
}

func TestFileProvider_FileNotExist(t *testing.T) {
	_, err := NewFileProvider(filepath.Join(os.TempDir(), "opsmesh-nonexistent-265.json"))
	if err == nil {
		t.Errorf("不存在的文件应返回错误")
	}
}

// ============================================================================
// ChainProvider 测试
// ============================================================================

func TestChainProvider(t *testing.T) {
	t.Setenv("OPSMESH_CHAIN_KEY", "from-env")

	envP := NewEnvProvider("")
	// file provider 没有该 key（构造一个空 JSON 文件）。
	path := writeTempJSON(t, map[string]interface{}{})
	fileP, err := NewFileProvider(path)
	if err != nil {
		t.Fatalf("NewFileProvider 失败: %v", err)
	}

	chain := NewChainProvider(envP, fileP)
	got, err := chain.Get("OPSMESH_CHAIN_KEY")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got != "from-env" {
		t.Errorf("Get 返回 %q，期望 %q", got, "from-env")
	}
	if chain.Name() != "chain" {
		t.Errorf("Name 返回 %q，期望 %q", chain.Name(), "chain")
	}
}

func TestChainProvider_Fallback(t *testing.T) {
	// env 没有 key，file 有，验证 fallback 到 file。
	path := writeTempJSON(t, map[string]interface{}{"fallback_key": "from-file"})
	fileP, err := NewFileProvider(path)
	if err != nil {
		t.Fatalf("NewFileProvider 失败: %v", err)
	}

	os.Unsetenv("fallback_key")
	envP := NewEnvProvider("")

	chain := NewChainProvider(envP, fileP)
	got, err := chain.Get("fallback_key")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got != "from-file" {
		t.Errorf("Get 返回 %q，期望 %q", got, "from-file")
	}
}

func TestChainProvider_AllNotFound(t *testing.T) {
	os.Unsetenv("OPSMESH_CHAIN_NOT_EXIST_265")
	envP := NewEnvProvider("")

	path := writeTempJSON(t, map[string]interface{}{})
	fileP, err := NewFileProvider(path)
	if err != nil {
		t.Fatalf("NewFileProvider 失败: %v", err)
	}

	chain := NewChainProvider(envP, fileP)
	_, err = chain.Get("OPSMESH_CHAIN_NOT_EXIST_265")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("全部 NotFound 时应返回 ErrSecretNotFound，实际: %v", err)
	}
}

// ============================================================================
// ResolveSecret 测试
// ============================================================================

func TestResolveSecret_PlainValue(t *testing.T) {
	// 明文值直接返回。
	got, err := ResolveSecret("plain-secret", NewEnvProvider(""))
	if err != nil {
		t.Fatalf("ResolveSecret 失败: %v", err)
	}
	if got != "plain-secret" {
		t.Errorf("ResolveSecret 返回 %q，期望 %q", got, "plain-secret")
	}

	// 不完整的引用格式（无后缀 }）应视为明文。
	got, err = ResolveSecret("${incomplete", nil)
	if err != nil {
		t.Fatalf("ResolveSecret 失败: %v", err)
	}
	if got != "${incomplete" {
		t.Errorf("不完整引用应视为明文，返回 %q", got)
	}
}

func TestResolveSecret_Reference(t *testing.T) {
	t.Setenv("OPSMESH_REF_KEY", "resolved-value")
	p := NewEnvProvider("OPSMESH_")

	// ${key} 形式：用传入的 provider 解析。
	got, err := ResolveSecret("${REF_KEY}", p)
	if err != nil {
		t.Fatalf("ResolveSecret 失败: %v", err)
	}
	if got != "resolved-value" {
		t.Errorf("ResolveSecret 返回 %q，期望 %q", got, "resolved-value")
	}
}

func TestResolveSecret_ReferenceWithProvider(t *testing.T) {
	t.Setenv("OPSMESH_REF_KEY2", "resolved-value2")
	p := NewEnvProvider("OPSMESH_")

	// ${env:REF_KEY2} 形式：含 provider 名前缀，仍由传入 provider 解析。
	got, err := ResolveSecret("${env:REF_KEY2}", p)
	if err != nil {
		t.Fatalf("ResolveSecret 失败: %v", err)
	}
	if got != "resolved-value2" {
		t.Errorf("ResolveSecret 返回 %q，期望 %q", got, "resolved-value2")
	}
}

func TestResolveSecret_NilProvider(t *testing.T) {
	// provider 为 nil 时引用格式保守返回原文（避免误把引用当密钥）。
	got, err := ResolveSecret("${SOME_KEY}", nil)
	if err != nil {
		t.Fatalf("ResolveSecret 失败: %v", err)
	}
	if got != "${SOME_KEY}" {
		t.Errorf("nil provider 时应返回原文，实际 %q", got)
	}
}

func TestResolveSecret_NotFound(t *testing.T) {
	os.Unsetenv("OPSMESH_NOT_EXIST_REF_265")
	p := NewEnvProvider("OPSMESH_")

	// 引用解析失败应返回错误。
	_, err := ResolveSecret("${NOT_EXIST_REF_265}", p)
	if err == nil {
		t.Errorf("引用不存在的 key 应返回错误")
	}
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("错误应 wrap ErrSecretNotFound，实际: %v", err)
	}
}

// ============================================================================
// VaultProvider 测试（用 httptest 模拟 Vault API）
// ============================================================================

func TestVaultProvider(t *testing.T) {
	// 模拟 Vault KV v2 API 响应。
	// KV v2 GET 路径：/v1/<mount>/data/<secretPath>
	// 响应体：{"data":{"data":{"webhook_url":"...","secret":"..."},"metadata":{...}}}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 校验请求路径。
		wantPath := "/v1/secret/data/notify/dingtalk"
		if r.URL.Path != wantPath {
			t.Errorf("请求路径 %q，期望 %q", r.URL.Path, wantPath)
			http.NotFound(w, r)
			return
		}
		// 校验 token 头。
		if r.Header.Get("X-Vault-Token") != "test-token" {
			t.Errorf("X-Vault-Token 头 %q，期望 %q", r.Header.Get("X-Vault-Token"), "test-token")
		}

		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"webhook_url": "https://oapi.dingtalk.com/robot/send?access_token=xxx",
					"secret":      "SECxxx",
				},
				"metadata": map[string]interface{}{
					"version": 1,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	v, err := NewVaultProvider(ts.URL, "test-token", "secret")
	if err != nil {
		t.Fatalf("NewVaultProvider 失败: %v", err)
	}
	if v.Name() != "vault" {
		t.Errorf("Name 返回 %q，期望 %q", v.Name(), "vault")
	}

	// 读取 webhook_url 字段。
	got, err := v.Get("notify/dingtalk#webhook_url")
	if err != nil {
		t.Fatalf("Get notify/dingtalk#webhook_url 失败: %v", err)
	}
	want := "https://oapi.dingtalk.com/robot/send?access_token=xxx"
	if got != want {
		t.Errorf("Get 返回 %q，期望 %q", got, want)
	}

	// 读取 secret 字段。
	got, err = v.Get("notify/dingtalk#secret")
	if err != nil {
		t.Fatalf("Get notify/dingtalk#secret 失败: %v", err)
	}
	if got != "SECxxx" {
		t.Errorf("Get 返回 %q，期望 %q", got, "SECxxx")
	}
}

func TestVaultProvider_NotFound(t *testing.T) {
	// 模拟 Vault 返回 404（Vault API 错误响应格式：{"errors":[...]}）。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []string{"Not Found"},
		})
	}))
	defer ts.Close()

	v, err := NewVaultProvider(ts.URL, "test-token", "secret")
	if err != nil {
		t.Fatalf("NewVaultProvider 失败: %v", err)
	}

	_, err = v.Get("notify/dingtalk#webhook_url")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("404 应返回 ErrSecretNotFound，实际: %v", err)
	}
}

func TestVaultProvider_FieldNotFound(t *testing.T) {
	// secret 存在但字段不存在。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"webhook_url": "https://oapi.dingtalk.com/robot/send?access_token=xxx",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	v, err := NewVaultProvider(ts.URL, "test-token", "secret")
	if err != nil {
		t.Fatalf("NewVaultProvider 失败: %v", err)
	}

	_, err = v.Get("notify/dingtalk#nonexistent_field")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("字段不存在应返回 ErrSecretNotFound，实际: %v", err)
	}
}

func TestVaultProvider_InvalidKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	v, err := NewVaultProvider(ts.URL, "test-token", "secret")
	if err != nil {
		t.Fatalf("NewVaultProvider 失败: %v", err)
	}

	// 缺少 #field 后缀。
	_, err = v.Get("notify/dingtalk")
	if err == nil {
		t.Errorf("缺少 #field 后缀应返回错误")
	}

	// 空字段。
	_, err = v.Get("notify/dingtalk#")
	if err == nil {
		t.Errorf("空字段应返回错误")
	}

	// 空 secretPath。
	_, err = v.Get("#field")
	if err == nil {
		t.Errorf("空 secretPath 应返回错误")
	}
}

func TestVaultProvider_EmptyArgs(t *testing.T) {
	// addr 为空。
	_, err := NewVaultProvider("", "token", "secret")
	if err == nil {
		t.Errorf("空 addr 应返回错误")
	}

	// token 为空。
	_, err = NewVaultProvider("http://localhost:8200", "", "secret")
	if err == nil {
		t.Errorf("空 token 应返回错误")
	}
}

// ============================================================================
// 辅助函数
// ============================================================================

// writeTempJSON 将 data 序列化为 JSON 写入临时文件，返回文件路径。
// t.Cleanup 自动清理。
func writeTempJSON(t *testing.T, data interface{}) string {
	t.Helper()
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent 失败: %v", err)
	}
	f, err := os.CreateTemp("", "opsmesh-secrets-test-*.json")
	if err != nil {
		t.Fatalf("CreateTemp 失败: %v", err)
	}
	if _, err := f.Write(raw); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}
