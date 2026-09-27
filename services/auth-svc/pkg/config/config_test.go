// config_test.go P0 安全回归：auth-svc 签名密钥不得有内置默认值。
//
// 背景：JWTSecret 曾以公开字面量 "default-jwt-secret-change-in-production" 作为
// getEnv 默认值。auth-svc 用它签发 HS256 access token，而网关的 requirePermission
// 直接信任 token 内的 permissions（不回查数据库），因此任何读过本仓库的人都能
// 伪造携带任意 roles/permissions 的 token。
package config

import (
	"os"
	"strings"
	"testing"
)

// Load 在无环境变量时不得产出任何可用的默认密钥。
func TestLoad_NoInsecureDefaultSecret(t *testing.T) {
	for _, k := range []string{"AUTH_SVC_JWT_SECRET", "AUTH_SVC_ALLOW_INSECURE_DEV_SECRET"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	cfg := Load()
	if cfg.JWTSecret != "" {
		t.Fatalf("Load() 返回了内置默认密钥 %q —— 任何读源码的人都能据此伪造 token", cfg.JWTSecret)
	}
	// 且默认状态下必须拒绝启动。
	if err := cfg.Validate(); err == nil {
		t.Fatal("空密钥时 Validate() 应当拒绝启动，但返回了 nil")
	}
}

// 公开的旧默认值若重新出现必须被测试拦住。
func TestLegacyDefaultSecretIsNotUsed(t *testing.T) {
	const legacy = "default-jwt-secret-change-in-production"
	_ = os.Unsetenv("AUTH_SVC_JWT_SECRET")
	cfg := Load()
	if strings.Contains(cfg.JWTSecret, legacy) {
		t.Fatalf("重现了历史漏洞：默认密钥 = %q", cfg.JWTSecret)
	}
}

func TestValidate(t *testing.T) {
	const strong = "0123456789abcdef0123456789abcdef" // 32 字节

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

// 环境变量到字段的接线：显式注入的密钥必须真正落到配置上。
func TestLoad_ReadsSecretFromEnv(t *testing.T) {
	const want = "env-provided-secret-0123456789abcdef"
	t.Setenv("AUTH_SVC_JWT_SECRET", want)
	if got := Load().JWTSecret; got != want {
		t.Fatalf("Load().JWTSecret = %q，期望 %q", got, want)
	}
}

// 逃生舱必须是显式 opt-in，默认关闭。
func TestAllowInsecureDevSecretDefaultsOff(t *testing.T) {
	_ = os.Unsetenv("AUTH_SVC_ALLOW_INSECURE_DEV_SECRET")
	if Load().AllowInsecureDevSecret {
		t.Fatal("AllowInsecureDevSecret 默认为 true —— 逃生舱必须显式开启")
	}
	t.Setenv("AUTH_SVC_ALLOW_INSECURE_DEV_SECRET", "true")
	if !Load().AllowInsecureDevSecret {
		t.Fatal("显式设置 AUTH_SVC_ALLOW_INSECURE_DEV_SECRET=true 未生效")
	}
}
