// security_p02_test.go — P0-2+3 安全加固 config 测试：
//   - TrustGatewayHeaders 默认 false + Production 强制 false
//   - AgentShellWhitelist 默认填充（agent shell 白名单默认开启）
package config

import (
	"strings"
	"testing"
)

// =============================================================================
// 修复1：TrustGatewayHeaders 默认 false + Production 强制 false
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
// 修复3：AgentShellWhitelist 默认填充（agent shell 白名单默认开启）
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
