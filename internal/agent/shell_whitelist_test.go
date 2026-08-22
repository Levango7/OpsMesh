// shell_whitelist_test.go — 安全加固 agent 测试：
//   - 默认白名单（defaultAgentShellWhitelist）放行只读诊断命令、拒绝危险命令
//   - checkShellWhitelist 与默认白名单的集成行为
package agent

import (
	"strings"
	"testing"

	"opsmesh/internal/config"
)

// TestCheckShellWhitelist_DefaultWhitelistAllowsReadOnly 验证 defaultAgentShellWhitelist
// 中的只读诊断命令被放行（白名单默认开启后的预期行为）。
func TestCheckShellWhitelist_DefaultWhitelistAllowsReadOnly(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentShellWhitelist: config.DefaultAgentShellWhitelist()}}
	// 这些命令应在默认白名单中，被放行。
	allowed := []string{
		"ls -la",
		"cat /etc/hostname",
		"echo hello",
		"date",
		"whoami",
		"hostname",
		"pwd",
		"ps aux",
		"df -h",
		"uptime",
	}
	for _, cmd := range allowed {
		if err := a.checkShellWhitelist(cmd); err != nil {
			t.Errorf("默认白名单应放行 %q: %v", cmd, err)
		}
	}
}

// TestCheckShellWhitelist_DefaultWhitelistRejectsDangerous 验证 defaultAgentShellWhitelist
// 不含危险命令，白名单默认开启后这些命令被拒绝。
func TestCheckShellWhitelist_DefaultWhitelistRejectsDangerous(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentShellWhitelist: config.DefaultAgentShellWhitelist()}}
	// 这些命令不应在默认白名单中，被拒绝。
	dangerous := []string{
		"rm -rf /",
		"sh -c evil",
		"bash -c evil",
		"mv /etc/passwd /tmp",
		"python -c 'import os; os.system(\"evil\")'",
		"perl -e 'system(\"evil\")'",
		"chmod 777 /etc",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda",
	}
	for _, cmd := range dangerous {
		if err := a.checkShellWhitelist(cmd); err == nil {
			t.Errorf("默认白名单应拒绝危险命令 %q，但放行了", cmd)
		}
	}
}

// TestCheckShellWhitelist_DefaultWhitelistNetworkDiagnoseStillAllowed 验证默认白名单启用后
// 网络诊断命令（ping/curl 等）仍被内置白名单放行（M6 集成不破坏）。
func TestCheckShellWhitelist_DefaultWhitelistNetworkDiagnoseStillAllowed(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentShellWhitelist: config.DefaultAgentShellWhitelist()}}
	// 网络诊断命令由 isNetworkDiagnoseCommand 内置白名单放行，不受 --agent-shell-whitelist 影响。
	diag := []string{"ping", "ping6", "traceroute", "tracert", "nslookup", "dig", "host", "curl", "wget", "nc", "netcat", "powershell"}
	for _, c := range diag {
		if err := a.checkShellWhitelist(c + " 127.0.0.1"); err != nil {
			t.Errorf("网络诊断命令 %q 应被内置白名单放行（不受默认白名单影响）: %v", c, err)
		}
	}
}

// TestCheckShellWhitelist_DefaultWhitelistCrossPlatform 验证默认白名单含跨平台命令
// （Linux: ss/netstat/free；Windows: ipconfig/systeminfo）。
func TestCheckShellWhitelist_DefaultWhitelistCrossPlatform(t *testing.T) {
	wl := config.DefaultAgentShellWhitelist()
	for _, cmd := range []string{"ipconfig", "systeminfo", "netstat", "ss", "free"} {
		if !strings.Contains(wl, cmd) {
			t.Errorf("默认白名单应含跨平台命令 %q，得到 %q", cmd, wl)
		}
	}
}
