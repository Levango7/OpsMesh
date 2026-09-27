// config_test.go P0 安全回归：device-svc 租户签名密钥不得有内置默认值。
//
// 背景：JWTSecret 曾以公开字面量 "default-jwt-secret-change-in-production" 作为默认值。
// device-svc 用同一密钥挂 tenant.Middleware 做 HTTP 网关鉴权，拿到该字面量的人可自行
// 签发任意 tenant_id 的 token，配合 X-Tenant-ID 头即可跨租户读取数据。
package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoad_NoInsecureDefaultSecret(t *testing.T) {
	_ = os.Unsetenv("DEVICE_SVC_JWT_SECRET")
	_ = os.Unsetenv("DEVICE_SVC_ALLOW_INSECURE_DEV_SECRET")
	cfg := Load()
	if cfg.JWTSecret != "" {
		t.Fatalf("Load() 返回了内置默认密钥 %q", cfg.JWTSecret)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("空密钥时 Validate() 应当拒绝启动，但返回了 nil")
	}
}

func TestLegacyDefaultSecretIsNotUsed(t *testing.T) {
	const legacy = "default-jwt-secret-change-in-production"
	_ = os.Unsetenv("DEVICE_SVC_JWT_SECRET")
	if strings.Contains(Load().JWTSecret, legacy) {
		t.Fatalf("重现了历史漏洞：默认密钥 = %q", Load().JWTSecret)
	}
}

func TestValidate(t *testing.T) {
	const strong = "0123456789abcdef0123456789abcdef"
	cases := []struct {
		name    string
		secret  string
		allow   bool
		wantErr bool
	}{
		{"合规密钥", strong, false, false},
		{"密钥为空", "", false, true},
		{"密钥过短", "short-secret", false, true},
		{"恰好 31 字节", strings.Repeat("a", 31), false, true},
		{"恰好 32 字节", strings.Repeat("a", 32), false, false},
		{"显式放行 + 空密钥（本地开发）", "", true, false},
		{"显式放行 + 弱密钥（本地开发）", "weak", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &Config{JWTSecret: c.secret, AllowInsecureDevSecret: c.allow}
			err := cfg.Validate()
			if c.wantErr && err == nil {
				t.Fatalf("期望拒绝启动，但 Validate() 返回 nil（secret=%q）", c.secret)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("期望放行，但被拒绝: %v", err)
			}
		})
	}
}

func TestLoad_ReadsSecretFromEnv(t *testing.T) {
	const want = "env-provided-secret-0123456789abcdef"
	t.Setenv("DEVICE_SVC_JWT_SECRET", want)
	if got := Load().JWTSecret; got != want {
		t.Fatalf("Load().JWTSecret = %q，期望 %q", got, want)
	}
}

func TestAllowInsecureDevSecretDefaultsOff(t *testing.T) {
	_ = os.Unsetenv("DEVICE_SVC_ALLOW_INSECURE_DEV_SECRET")
	if Load().AllowInsecureDevSecret {
		t.Fatal("AllowInsecureDevSecret 默认为 true —— 逃生舱必须显式开启")
	}
}
