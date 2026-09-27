// config_test.go P0 安全回归：config-svc 加密密钥不得有内置默认值。
//
// 背景：EncryptionKey 曾以公开字面量 "default-encryption-key-change-in-production" 作为
// 默认值。config-svc 用它加密下发的配置密文（secret 轮换、配置版本），任何读过本仓库
// 的人拿到该字面量即可解密任意租户的存量密文。
package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoad_NoInsecureDefaultKey(t *testing.T) {
	_ = os.Unsetenv("CONFIG_SVC_ENCRYPTION_KEY")
	_ = os.Unsetenv("CONFIG_SVC_ALLOW_INSECURE_DEV_SECRET")
	cfg := Load()
	if cfg.EncryptionKey != "" {
		t.Fatalf("Load() 返回了内置默认加密密钥 %q", cfg.EncryptionKey)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("空密钥时 Validate() 应当拒绝启动，但返回了 nil")
	}
}

func TestLegacyDefaultKeyIsNotUsed(t *testing.T) {
	const legacy = "default-encryption-key-change-in-production"
	_ = os.Unsetenv("CONFIG_SVC_ENCRYPTION_KEY")
	if strings.Contains(Load().EncryptionKey, legacy) {
		t.Fatalf("重现了历史漏洞：默认加密密钥 = %q", Load().EncryptionKey)
	}
}

func TestValidate(t *testing.T) {
	const strong = "0123456789abcdef0123456789abcdef" // AES-256 = 32 字节
	cases := []struct {
		name    string
		key     string
		allow   bool
		wantErr bool
	}{
		{"合规密钥", strong, false, false},
		{"密钥为空", "", false, true},
		{"密钥过短", "short-key", false, true},
		{"恰好 31 字节", strings.Repeat("a", 31), false, true},
		{"恰好 32 字节", strings.Repeat("a", 32), false, false},
		{"显式放行 + 空密钥（本地开发）", "", true, false},
		{"显式放行 + 弱密钥（本地开发）", "weak", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &Config{EncryptionKey: c.key, AllowInsecureDevSecret: c.allow}
			err := cfg.Validate()
			if c.wantErr && err == nil {
				t.Fatalf("期望拒绝启动，但 Validate() 返回 nil（key=%q）", c.key)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("期望放行，但被拒绝: %v", err)
			}
		})
	}
}

func TestLoad_ReadsKeyFromEnv(t *testing.T) {
	const want = "env-provided-key-0123456789abcdefghij"
	t.Setenv("CONFIG_SVC_ENCRYPTION_KEY", want)
	if got := Load().EncryptionKey; got != want {
		t.Fatalf("Load().EncryptionKey = %q，期望 %q", got, want)
	}
}

func TestAllowInsecureDevSecretDefaultsOff(t *testing.T) {
	_ = os.Unsetenv("CONFIG_SVC_ALLOW_INSECURE_DEV_SECRET")
	if Load().AllowInsecureDevSecret {
		t.Fatal("AllowInsecureDevSecret 默认为 true —— 逃生舱必须显式开启")
	}
}
