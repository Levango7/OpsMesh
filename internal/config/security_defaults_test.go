// security_defaults_test.go — 安全加固 config 测试：
//   - TrustGatewayHeaders 默认 false + Production 强制 false
//   - AgentShellWhitelist 默认填充（agent shell 白名单默认开启）
package config

import (
	"strings"
	"testing"
)

// =============================================================================
// TrustGatewayHeaders 默认 false + Production 强制 false
// =============================================================================

// TestLoad_TrustGatewayHeadersDefaultFalse 验证 TrustGatewayHeaders 默认 false（安全基线）。
func TestLoad_TrustGatewayHeadersDefaultFalse(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest()
	if cfg.TrustGatewayHeaders {
		t.Fatal("TrustGatewayHeaders 默认应为 false（安全基线，防客户端伪造 X-User-Roles 越权）")
	}
}

// TestLoad_TrustGatewayHeadersExplicitTrue 验证非生产模式下显式 --trust-gateway-headers=true 被尊重。
func TestLoad_TrustGatewayHeadersExplicitTrue(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--trust-gateway-headers=true")
	if !cfg.TrustGatewayHeaders {
		t.Fatal("非生产模式显式 --trust-gateway-headers=true 应被尊重")
	}
}

// TestLoad_TrustGatewayHeadersProductionForcesFalse 验证生产模式强制 false（即使显式 true 也覆盖）。
func TestLoad_TrustGatewayHeadersProductionForcesFalse(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest(
		"--production=true",
		"--tls-cert=cert.pem",
		"--tls-key=key.pem",
		"--jwt-secret=0123456789abcdef0123456789abcdef",
		"--encryption-key=AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=",
		"--trust-gateway-headers=true", // 显式 true，应被生产模式覆盖为 false
	)
	if cfg.TrustGatewayHeaders {
		t.Fatal("生产模式应强制 TrustGatewayHeaders=false（即使显式 true 也覆盖），杜绝信任客户端可伪造的头")
	}
}

// TestValidate_TrustGatewayHeadersNoConstraint 验证非生产模式下 TrustGatewayHeaders=true 通过 Validate。
// Validate 不应拒绝非生产模式下的显式开启（用于内网部署有可信网关前置的场景）。
func TestValidate_TrustGatewayHeadersNoConstraint(t *testing.T) {
	c := base()
	c.TrustGatewayHeaders = true
	c.Production = false
	if err := c.Validate(); err != nil {
		t.Fatalf("非生产模式 TrustGatewayHeaders=true 应通过 Validate: %v", err)
	}
}

// =============================================================================
// AgentShellWhitelist 默认填充（agent shell 白名单默认开启）
// =============================================================================

// TestLoad_AgentShellWhitelistDefaultFilled 验证未显式设置 --agent-shell-whitelist 时
// 自动填充 defaultAgentShellWhitelist（只读诊断命令白名单）。
func TestLoad_AgentShellWhitelistDefaultFilled(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest()
	if cfg.AgentShellWhitelist == "" {
		t.Fatal("未显式设置 --agent-shell-whitelist 时应自动填充 defaultAgentShellWhitelist（非空）")
	}
	if cfg.AgentShellWhitelist != defaultAgentShellWhitelist {
		t.Fatalf("默认填充应为 defaultAgentShellWhitelist=%q，得到 %q", defaultAgentShellWhitelist, cfg.AgentShellWhitelist)
	}
	// 验证默认白名单含只读诊断命令，不含危险命令。
	for _, cmd := range []string{"ls", "cat", "echo", "date", "ps"} {
		if !strings.Contains(cfg.AgentShellWhitelist, cmd) {
			t.Fatalf("默认白名单应含 %q，得到 %q", cmd, cfg.AgentShellWhitelist)
		}
	}
	for _, dangerous := range []string{"rm", "sh", "bash", "curl", "nc", "python"} {
		if strings.Contains(cfg.AgentShellWhitelist, dangerous) {
			t.Fatalf("默认白名单不应含危险命令 %q，得到 %q", dangerous, cfg.AgentShellWhitelist)
		}
	}
}

// TestLoad_AgentShellWhitelistExplicitOverridesDefault 验证显式设置 --agent-shell-whitelist 时
// 覆盖默认填充（用户自定义优先）。
func TestLoad_AgentShellWhitelistExplicitOverridesDefault(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--agent-shell-whitelist=ls,cat,echo")
	if cfg.AgentShellWhitelist != "ls,cat,echo" {
		t.Fatalf("显式 --agent-shell-whitelist 应覆盖默认，得到 %q", cfg.AgentShellWhitelist)
	}
}

// TestLoad_AgentShellWhitelistExplicitEmptyRespected 验证显式设置 --agent-shell-whitelist="" 时
// 尊重用户意图（不限制），不填充默认。
func TestLoad_AgentShellWhitelistExplicitEmptyRespected(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--agent-shell-whitelist=")
	if cfg.AgentShellWhitelist != "" {
		t.Fatalf("显式 --agent-shell-whitelist= 应被尊重（不限制），得到 %q", cfg.AgentShellWhitelist)
	}
}

// TestLoad_AgentShellWhitelistDefaultFalseKeepsEmpty 验证 --agent-shell-whitelist-default=false 时
// 未显式设置 --agent-shell-whitelist 保持空（向后兼容）。
func TestLoad_AgentShellWhitelistDefaultFalseKeepsEmpty(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--agent-shell-whitelist-default=false")
	if cfg.AgentShellWhitelist != "" {
		t.Fatalf("--agent-shell-whitelist-default=false 时应保持空（向后兼容），得到 %q", cfg.AgentShellWhitelist)
	}
}

// TestLoad_AgentShellWhitelistDefaultFlagEnvOverride 验证 env OPSMESH_AGENT_SHELL_WHITELIST_DEFAULT
// 可关闭默认填充。
func TestLoad_AgentShellWhitelistDefaultFlagEnvOverride(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	t.Setenv("OPSMESH_AGENT_SHELL_WHITELIST_DEFAULT", "false")
	cfg := loadForTest()
	if cfg.AgentShellWhitelist != "" {
		t.Fatalf("env OPSMESH_AGENT_SHELL_WHITELIST_DEFAULT=false 时应保持空，得到 %q", cfg.AgentShellWhitelist)
	}
}

// TestDefaultAgentShellWhitelistContainsExpected 验证 defaultAgentShellWhitelist 常量含预期命令。
func TestDefaultAgentShellWhitelistContainsExpected(t *testing.T) {
	expected := []string{
		"ls", "cat", "echo", "date", "whoami", "hostname", "pwd",
		"free", "df", "uptime", "top", "ps", "netstat", "ss",
		"ipconfig", "systeminfo",
	}
	for _, cmd := range expected {
		if !strings.Contains(defaultAgentShellWhitelist, cmd) {
			t.Fatalf("defaultAgentShellWhitelist 应含 %q，得到 %q", cmd, defaultAgentShellWhitelist)
		}
	}
}

// =============================================================================
// 初始 admin 口令交付通道（P0-1：生产不得静默锁死管理员）
// =============================================================================

// TestLoad_AdminPasswordDefaults 验证三个口令相关字段默认均为空/false（不隐式注入口令）。
func TestLoad_AdminPasswordDefaults(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest()
	if cfg.AdminPassword != "" || cfg.AdminPasswordFile != "" {
		t.Fatalf("默认不应有口令交付通道，得到 password=%q file=%q", cfg.AdminPassword, cfg.AdminPasswordFile)
	}
	if cfg.AdminPasswordForceReset {
		t.Fatal("AdminPasswordForceReset 默认应为 false（避免重启静默回滚界面改密）")
	}
}

// TestLoad_AdminPasswordFromFlagAndEnv 验证 flag 与 env 两种注入方式均生效。
func TestLoad_AdminPasswordFromFlagAndEnv(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--admin-password=FlagPw123", "--admin-password-file=/tmp/pw")
	if cfg.AdminPassword != "FlagPw123" || cfg.AdminPasswordFile != "/tmp/pw" {
		t.Fatalf("flag 注入失败: password=%q file=%q", cfg.AdminPassword, cfg.AdminPasswordFile)
	}
	restore2 := clearOpsmeshEnv()
	defer restore2()
	t.Setenv("OPSMESH_ADMIN_PASSWORD", "EnvPw123")
	t.Setenv("OPSMESH_ADMIN_PASSWORD_FILE", "/tmp/envpw")
	cfg = loadForTest()
	if cfg.AdminPassword != "EnvPw123" || cfg.AdminPasswordFile != "/tmp/envpw" {
		t.Fatalf("env 注入失败: password=%q file=%q", cfg.AdminPassword, cfg.AdminPasswordFile)
	}
}

// TestValidate_AdminPasswordForceResetRequiresPassword 验证恢复开关必须与显式口令配合。
func TestValidate_AdminPasswordForceResetRequiresPassword(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--admin-password-force-reset=true")
	if err := cfg.Validate(); err == nil {
		t.Fatal("--admin-password-force-reset=true 而未提供 --admin-password 时应拒绝启动")
	}
	cfg = loadForTest("--admin-password-force-reset=true", "--admin-password=Str0ngPw123")
	if err := cfg.Validate(); err != nil {
		t.Fatalf("提供 --admin-password 后应通过校验: %v", err)
	}
}

// =============================================================================
// Production 与 Demo 互斥（demo 是全局安全降级开关，不可与生产同时开启）
// =============================================================================

// TestValidate_ProductionAndDemoMutuallyExclusive 验证 --production 与 --demo 同时为真被拒绝启动。
//
// demo 不是"演示配色"，而是一组安全降级开关的集合：
//   - server_bootstrap.go: Demo 让 bootstrap token 校验直接 return true
//     → /install.sh 与 /bin/opsmesh-agent 对全网开放（后者分发二进制本体）；
//   - server_middleware.go: Demo 跳过 CSRF Origin 校验；
//   - auth.go: Demo 无身份即放行（RBAC 兜底关闭）；
//   - config.go: Demo 关闭 gRPC agent 身份签名。
//
// 一次误配即同时关掉这四道防线，故 Validate 必须 fail-fast。
func TestValidate_ProductionAndDemoMutuallyExclusive(t *testing.T) {
	c := base()
	c.Production = true
	c.Demo = true
	if err := c.Validate(); err == nil {
		t.Fatal("--production=true 与 --demo=true 同时设置应被拒绝启动")
	}
	// 单独开启任一个都不应因互斥而失败（生产侧还须自备 TLS/密钥，
	// 这里的断言只关心"不再收到互斥错误"）。
	only := base()
	only.Production = true
	only.Demo = false
	if err := only.Validate(); err != nil && strings.Contains(err.Error(), "互斥") {
		t.Fatalf("仅 --production 不应触发互斥错误: %v", err)
	}
	onlyDemo := base()
	onlyDemo.Production = false
	onlyDemo.Demo = true
	if err := onlyDemo.Validate(); err != nil {
		t.Fatalf("仅 --demo 应通过校验: %v", err)
	}
}

// TestLoad_DemoRespectsExplicitGRPCRequireSignature 验证 demo 模式尊重显式的
// --grpc-require-signature=true。
//
// 回归背景：原实现是 `if cfg.Demo { cfg.GRPCRequireSignature = false }`——无条件覆盖，
// 连用户显式 `--grpc-require-signature=true` 都压不过，等于 demo 成了一个可以把
// 安全开关关掉的全局后门。现改为仅在未显式设置时才默认关闭。
func TestLoad_DemoRespectsExplicitGRPCRequireSignature(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--demo=true", "--grpc-require-signature=true")
	if !cfg.GRPCRequireSignature {
		t.Fatal("demo 模式下显式 --grpc-require-signature=true 应被尊重")
	}
}

// TestLoad_DemoDefaultsGRPCRequireSignatureOff 验证未显式设置时 demo 仍默认关闭签名
// （保持向后兼容：本地一键体验不需要 agent 签名）。
func TestLoad_DemoDefaultsGRPCRequireSignatureOff(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--demo=true")
	if cfg.GRPCRequireSignature {
		t.Fatal("demo 模式未显式设置时应默认关闭 gRPC 签名（向后兼容）")
	}
}
