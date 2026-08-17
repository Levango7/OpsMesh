// secrets_extra_test.go — 补全 internal/secrets 包测试覆盖率（task 325）。
//
// 重点覆盖 factory.go 中 FromConfig/buildChain/buildSingle/buildVault 四个工厂函数
// （原覆盖率为 0%），并补充 provider.go 与 vault.go 的边界分支。
//
// 仅新增测试，不修改任何源代码。

package secrets

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"opsmesh/internal/config"
)

// ============================================================================
// FromConfig 测试（factory.go）
// ============================================================================

// TestFromConfig_NilCfg cfg 为 nil 时返回 nil, nil（不启用密钥外置）。
func TestFromConfig_NilCfg(t *testing.T) {
	p, err := FromConfig(nil)
	if err != nil {
		t.Fatalf("nil cfg 应返回 nil 错误，实际: %v", err)
	}
	if p != nil {
		t.Errorf("nil cfg 应返回 nil provider，实际: %v", p)
	}
}

// TestFromConfig_EmptySpec SecretProvider 为空时返回 nil, nil。
func TestFromConfig_EmptySpec(t *testing.T) {
	cfg := &config.Config{SecretProvider: ""}
	p, err := FromConfig(cfg)
	if err != nil {
		t.Fatalf("空 spec 应返回 nil 错误，实际: %v", err)
	}
	if p != nil {
		t.Errorf("空 spec 应返回 nil provider，实际: %v", p)
	}
}

// TestFromConfig_Env spec="env" 返回 EnvProvider。
func TestFromConfig_Env(t *testing.T) {
	cfg := &config.Config{SecretProvider: "env"}
	p, err := FromConfig(cfg)
	if err != nil {
		t.Fatalf("FromConfig 失败: %v", err)
	}
	if p == nil || p.Name() != "env" {
		t.Errorf("应返回 EnvProvider，实际: %v", p)
	}
}

// TestFromConfig_File_EmptySecretFile spec="file" 但 SecretFile 为空 → 错误。
func TestFromConfig_File_EmptySecretFile(t *testing.T) {
	cfg := &config.Config{SecretProvider: "file"}
	_, err := FromConfig(cfg)
	if err == nil {
		t.Errorf("file provider 但 SecretFile 为空应返回错误")
	}
	if !strings.Contains(err.Error(), "secret-file") {
		t.Errorf("错误信息应提及 secret-file，实际: %v", err)
	}
}

// TestFromConfig_File_Success spec="file" 且 SecretFile 指向有效 JSON 文件 → FileProvider。
func TestFromConfig_File_Success(t *testing.T) {
	path := writeTempJSON(t, map[string]interface{}{"k": "v"})
	cfg := &config.Config{SecretProvider: "file", SecretFile: path}
	p, err := FromConfig(cfg)
	if err != nil {
		t.Fatalf("FromConfig 失败: %v", err)
	}
	if p == nil || p.Name() != "file" {
		t.Errorf("应返回 FileProvider，实际: %v", p)
	}
}

// TestFromConfig_Vault_EmptyAddr spec="vault" 但 VaultAddr 为空 → 错误。
func TestFromConfig_Vault_EmptyAddr(t *testing.T) {
	cfg := &config.Config{SecretProvider: "vault", VaultToken: "tok"}
	_, err := FromConfig(cfg)
	if err == nil {
		t.Errorf("vault provider 但 VaultAddr 为空应返回错误")
	}
	if !strings.Contains(err.Error(), "vault-addr") {
		t.Errorf("错误信息应提及 vault-addr，实际: %v", err)
	}
}

// TestFromConfig_Vault_Success spec="vault" 且配置完整 → VaultProvider。
func TestFromConfig_Vault_Success(t *testing.T) {
	// 用 httptest 启动一个最小 Vault 兼容服务器（仅用于构造 client，不实际调用 Get）。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer ts.Close()

	cfg := &config.Config{
		SecretProvider: "vault",
		VaultAddr:      ts.URL,
		VaultToken:     "test-token",
		VaultMount:     "secret",
	}
	p, err := FromConfig(cfg)
	if err != nil {
		t.Fatalf("FromConfig 失败: %v", err)
	}
	if p == nil || p.Name() != "vault" {
		t.Errorf("应返回 VaultProvider，实际: %v", p)
	}
}

// TestFromConfig_UnknownSpec 未知 spec → 错误。
func TestFromConfig_UnknownSpec(t *testing.T) {
	cfg := &config.Config{SecretProvider: "unknown"}
	_, err := FromConfig(cfg)
	if err == nil {
		t.Errorf("未知 spec 应返回错误")
	}
	if !strings.Contains(err.Error(), "非法") {
		t.Errorf("错误信息应提示非法，实际: %v", err)
	}
}

// TestFromConfig_Chain_EnvFile spec="chain:env,file" → ChainProvider。
func TestFromConfig_Chain_EnvFile(t *testing.T) {
	path := writeTempJSON(t, map[string]interface{}{"k": "v"})
	cfg := &config.Config{SecretProvider: "chain:env,file", SecretFile: path}
	p, err := FromConfig(cfg)
	if err != nil {
		t.Fatalf("FromConfig 失败: %v", err)
	}
	if p == nil || p.Name() != "chain" {
		t.Errorf("应返回 ChainProvider，实际: %v", p)
	}
}

// TestFromConfig_Chain_UnknownChild chain 中含未知子 provider → 错误。
func TestFromConfig_Chain_UnknownChild(t *testing.T) {
	cfg := &config.Config{SecretProvider: "chain:env,unknown"}
	_, err := FromConfig(cfg)
	if err == nil {
		t.Errorf("chain 中含未知子 provider 应返回错误")
	}
	if !strings.Contains(err.Error(), "构造失败") {
		t.Errorf("错误信息应提示构造失败，实际: %v", err)
	}
}

// TestFromConfig_Chain_EmptyAfterParse chain: 后全为空白 → 错误。
func TestFromConfig_Chain_EmptyAfterParse(t *testing.T) {
	cfg := &config.Config{SecretProvider: "chain: , "}
	_, err := FromConfig(cfg)
	if err == nil {
		t.Errorf("chain: 解析后无有效 provider 应返回错误")
	}
	if !strings.Contains(err.Error(), "无有效 provider") {
		t.Errorf("错误信息应提示无有效 provider，实际: %v", err)
	}
}

// TestFromConfig_Chain_VaultChild chain:env,vault 子 provider 为 vault。
func TestFromConfig_Chain_VaultChild(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer ts.Close()

	cfg := &config.Config{
		SecretProvider: "chain:env,vault",
		VaultAddr:      ts.URL,
		VaultToken:     "tok",
	}
	p, err := FromConfig(cfg)
	if err != nil {
		t.Fatalf("FromConfig 失败: %v", err)
	}
	if p == nil || p.Name() != "chain" {
		t.Errorf("应返回 ChainProvider，实际: %v", p)
	}
}

// ============================================================================
// buildVault 测试（factory.go）
// ============================================================================

// TestBuildVault_TokenFromEnv VaultToken 为空时回退环境变量 OPSMESH_VAULT_TOKEN。
func TestBuildVault_TokenFromEnv(t *testing.T) {
	t.Setenv("OPSMESH_VAULT_TOKEN", "env-token")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer ts.Close()

	cfg := &config.Config{VaultAddr: ts.URL} // VaultToken 留空
	p, err := buildVault(cfg)
	if err != nil {
		t.Fatalf("buildVault 失败: %v", err)
	}
	if p == nil || p.Name() != "vault" {
		t.Errorf("应返回 VaultProvider，实际: %v", p)
	}
}

// TestBuildVault_NoToken 既无 VaultToken 也无环境变量 → 错误。
func TestBuildVault_NoToken(t *testing.T) {
	os.Unsetenv("OPSMESH_VAULT_TOKEN")

	cfg := &config.Config{VaultAddr: "http://localhost:8200"}
	_, err := buildVault(cfg)
	if err == nil {
		t.Errorf("无 token 应返回错误")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("错误信息应提及 token，实际: %v", err)
	}
}

// TestBuildVault_EmptyAddr VaultAddr 为空 → 错误。
func TestBuildVault_EmptyAddr(t *testing.T) {
	cfg := &config.Config{VaultToken: "tok"}
	_, err := buildVault(cfg)
	if err == nil {
		t.Errorf("空 VaultAddr 应返回错误")
	}
	if !strings.Contains(err.Error(), "vault-addr") {
		t.Errorf("错误信息应提及 vault-addr，实际: %v", err)
	}
}

// TestBuildVault_DefaultMount mount 为空时默认 "secret"（通过 Get 行为间接验证）。
func TestBuildVault_DefaultMount(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 默认 mount 应为 "secret"，请求路径含 /v1/secret/data/...
		if !strings.HasPrefix(r.URL.Path, "/v1/secret/data/") {
			t.Errorf("默认 mount 应为 secret，请求路径: %q", r.URL.Path)
		}
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{"field": "value"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := &config.Config{VaultAddr: ts.URL, VaultToken: "tok"} // VaultMount 留空
	p, err := buildVault(cfg)
	if err != nil {
		t.Fatalf("buildVault 失败: %v", err)
	}
	got, err := p.Get("path/to#field")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got != "value" {
		t.Errorf("Get 返回 %q，期望 %q", got, "value")
	}
}

// ============================================================================
// buildSingle 测试（factory.go）
// ============================================================================

// TestBuildSingle_Env 构造 env provider。
func TestBuildSingle_Env(t *testing.T) {
	p, err := buildSingle("env", &config.Config{})
	if err != nil {
		t.Fatalf("buildSingle 失败: %v", err)
	}
	if p.Name() != "env" {
		t.Errorf("应返回 env provider，实际: %v", p)
	}
}

// TestBuildSingle_File_EmptyPath file 但 SecretFile 为空 → 错误。
func TestBuildSingle_File_EmptyPath(t *testing.T) {
	_, err := buildSingle("file", &config.Config{})
	if err == nil {
		t.Errorf("file 但 SecretFile 为空应返回错误")
	}
}

// TestBuildSingle_File_Success file 且 SecretFile 有效 → FileProvider。
func TestBuildSingle_File_Success(t *testing.T) {
	path := writeTempJSON(t, map[string]interface{}{"k": "v"})
	p, err := buildSingle("file", &config.Config{SecretFile: path})
	if err != nil {
		t.Fatalf("buildSingle 失败: %v", err)
	}
	if p.Name() != "file" {
		t.Errorf("应返回 file provider，实际: %v", p)
	}
}

// TestBuildSingle_Unknown 未知名称 → 错误。
func TestBuildSingle_Unknown(t *testing.T) {
	_, err := buildSingle("unknown", &config.Config{})
	if err == nil {
		t.Errorf("未知名称应返回错误")
	}
	if !strings.Contains(err.Error(), "未知 provider") {
		t.Errorf("错误信息应提示未知 provider，实际: %v", err)
	}
}

// ============================================================================
// buildChain 测试（factory.go）
// ============================================================================

// TestBuildChain_SingleName 仅一个子 provider。
func TestBuildChain_SingleName(t *testing.T) {
	p, err := buildChain("env", &config.Config{})
	if err != nil {
		t.Fatalf("buildChain 失败: %v", err)
	}
	if p.Name() != "chain" {
		t.Errorf("应返回 chain provider，实际: %v", p)
	}
}

// TestBuildChain_WithSpaces 含空白的子 provider 名称应被 TrimSpace。
func TestBuildChain_WithSpaces(t *testing.T) {
	p, err := buildChain(" env , env ", &config.Config{})
	if err != nil {
		t.Fatalf("buildChain 失败: %v", err)
	}
	if p.Name() != "chain" {
		t.Errorf("应返回 chain provider，实际: %v", p)
	}
}

// TestBuildChain_VaultChildError 子 provider 为 vault 但配置缺失 → 错误。
func TestBuildChain_VaultChildError(t *testing.T) {
	_, err := buildChain("vault", &config.Config{}) // VaultAddr 为空
	if err == nil {
		t.Errorf("vault 子 provider 配置缺失应返回错误")
	}
	if !strings.Contains(err.Error(), "构造失败") {
		t.Errorf("错误信息应提示构造失败，实际: %v", err)
	}
}

// ============================================================================
// provider.go 边界补充测试
// ============================================================================

// TestFileProvider_InvalidJSON 文件存在但 JSON 无效 → 错误。
func TestFileProvider_InvalidJSON(t *testing.T) {
	// 写入一个非法 JSON 临时文件。
	f, err := os.CreateTemp("", "opsmesh-invalid-*.json")
	if err != nil {
		t.Fatalf("CreateTemp 失败: %v", err)
	}
	if _, err := f.WriteString("{invalid json"); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })

	_, err = NewFileProvider(f.Name())
	if err == nil {
		t.Errorf("非法 JSON 应返回错误")
	}
	if !strings.Contains(err.Error(), "解析") {
		t.Errorf("错误信息应提示解析失败，实际: %v", err)
	}
}

// TestFileProvider_NestedValueNonString 中间段为 string 而非 map → ErrSecretNotFound。
func TestFileProvider_NestedValueNonString(t *testing.T) {
	// 顶层 "a" 是 string，请求 "a/b" 时下钻到非 map。
	path := writeTempJSON(t, map[string]interface{}{"a": "string-value"})
	p, err := NewFileProvider(path)
	if err != nil {
		t.Fatalf("NewFileProvider 失败: %v", err)
	}
	_, err = p.Get("a/b")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("下钻到非 map 应返回 ErrSecretNotFound，实际: %v", err)
	}
}

// TestFileProvider_LeafNonString 叶子节点非 string → ErrSecretNotFound。
func TestFileProvider_LeafNonString(t *testing.T) {
	// 顶层 "n" 是数字，请求 "n" 时叶子非 string。
	path := writeTempJSON(t, map[string]interface{}{"n": 123})
	p, err := NewFileProvider(path)
	if err != nil {
		t.Fatalf("NewFileProvider 失败: %v", err)
	}
	_, err = p.Get("n")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("叶子非 string 应返回 ErrSecretNotFound，实际: %v", err)
	}
}

// TestChainProvider_NonNotFoundError 第一个 provider 返回非 NotFound 错误 → 立即返回该错误。
func TestChainProvider_NonNotFoundError(t *testing.T) {
	// 用一个自定义 provider 模拟非 NotFound 错误。
	customErr := errors.New("custom provider failure")
	p1 := &errorProvider{err: customErr}
	p2 := NewEnvProvider("")

	chain := NewChainProvider(p1, p2)
	_, err := chain.Get("any-key")
	if !errors.Is(err, customErr) {
		t.Errorf("非 NotFound 错误应立即返回，实际: %v", err)
	}
}

// TestChainProvider_Empty 空 providers 列表 → Get 返回 ErrSecretNotFound。
func TestChainProvider_Empty(t *testing.T) {
	chain := NewChainProvider()
	_, err := chain.Get("any-key")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("空 chain 应返回 ErrSecretNotFound，实际: %v", err)
	}
}

// errorProvider 测试用 provider：始终返回预设错误。
type errorProvider struct {
	err error
}

func (e *errorProvider) Get(key string) (string, error) { return "", e.err }
func (e *errorProvider) Name() string                   { return "error" }

// ============================================================================
// vault.go 边界补充测试
// ============================================================================

// TestVaultProvider_NonStringField 字段值为数字 → fmt.Sprintf("%v") 转 string。
func TestVaultProvider_NonStringField(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{"count": 42},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	v, err := NewVaultProvider(ts.URL, "tok", "secret")
	if err != nil {
		t.Fatalf("NewVaultProvider 失败: %v", err)
	}
	got, err := v.Get("path#count")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got != "42" {
		t.Errorf("非 string 字段应转为 string，期望 %q，实际 %q", "42", got)
	}
}

// TestVaultProvider_ServerError Vault 返回 500 → 包装错误（非 ErrSecretNotFound）。
func TestVaultProvider_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []string{"internal server error"},
		})
	}))
	defer ts.Close()

	v, err := NewVaultProvider(ts.URL, "tok", "secret")
	if err != nil {
		t.Fatalf("NewVaultProvider 失败: %v", err)
	}
	_, err = v.Get("path#field")
	if err == nil {
		t.Errorf("500 错误应返回错误")
	}
	if errors.Is(err, ErrSecretNotFound) {
		t.Errorf("500 错误不应识别为 ErrSecretNotFound，实际: %v", err)
	}
}

// TestVaultProvider_DefaultMount mount 为空时默认 "secret"。
func TestVaultProvider_DefaultMount(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/secret/data/") {
			t.Errorf("默认 mount 应为 secret，请求路径: %q", r.URL.Path)
		}
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{"field": "value"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	v, err := NewVaultProvider(ts.URL, "tok", "") // mount 留空
	if err != nil {
		t.Fatalf("NewVaultProvider 失败: %v", err)
	}
	got, err := v.Get("path#field")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got != "value" {
		t.Errorf("Get 返回 %q，期望 %q", got, "value")
	}
}
