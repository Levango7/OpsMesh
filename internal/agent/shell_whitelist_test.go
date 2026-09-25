// shell_whitelist_test.go — 安全加固 agent 测试：
//   - 默认白名单（defaultAgentShellWhitelist）放行只读诊断命令、拒绝危险命令
//   - checkShellWhitelist 与默认白名单的集成行为
package agent

import (
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
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

// --- P1-1：命令分隔符绕过（&& / || / |）逐段校验 ---

// TestCheckShellWhitelist_SegmentChainRejected 验证 `ls && rm -rf /` 这类「首 token 命中白名单 +
// 分隔符拼接危险命令」的绕过被拒绝（P1-1 回归）。
func TestCheckShellWhitelist_SegmentChainRejected(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentShellWhitelist: "ls,cat,echo,date"}}
	bypass := []string{
		"ls && rm -rf /",                // 条件与：右侧任意命令
		"ls -la && mv /etc/passwd /tmp", // 首段带参数
		"date || sh -c evil",            // 条件或：左侧失败即执行右侧
		"cat /etc/passwd | sh",          // 管道：数据流注入
		"ls | bash -c evil",             // 管道 + 解释器
		"echo ok && sh -c 'id'",         // 嵌套解释器
		"ls && ls && rm -rf /var",       // 多段链，末段非法
	}
	for _, cmd := range bypass {
		if err := a.checkShellWhitelist(cmd); err == nil {
			t.Errorf("分隔符拼接命令 %q 应被拒绝，但放行了", cmd)
		}
	}
}

// TestCheckShellWhitelist_SegmentChainAllowed 验证逐段校验不误伤合法的同白名单组合。
func TestCheckShellWhitelist_SegmentChainAllowed(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentShellWhitelist: "ls,cat,echo,date,uptime"}}
	allowed := []string{
		"ls -la && cat /etc/hostname", // 两段均在白名单
		"date && uptime",
		"echo hi 1>&2",   // fd 重定向不是分隔符，不得切段（切段会得到命令词 "2"）
		"echo hi &>file", // 合并重定向同理
		"echo a && echo b || echo c",
	}
	for _, cmd := range allowed {
		if err := a.checkShellWhitelist(cmd); err != nil {
			t.Errorf("合法命令 %q 应放行: %v", cmd, err)
		}
	}
}

// TestCheckShellWhitelist_EnvAssignmentRejected 验证环境变量赋值前缀不放行（fail-closed）：
// `PATH=/tmp ls` 可让 shell 用攻击者可控 PATH 解析后续命令，按 basename 放行等于放行任意程序。
func TestCheckShellWhitelist_EnvAssignmentRejected(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentShellWhitelist: "ls,echo"}}
	for _, cmd := range []string{"PATH=/tmp ls", "FOO=bar echo hi", "LD_PRELOAD=/tmp/x ls"} {
		if err := a.checkShellWhitelist(cmd); err == nil {
			t.Errorf("带环境变量赋值的命令 %q 应被拒绝", cmd)
		}
	}
}

// TestCheckShellWhitelist_PathScope 验证带路径命令词的作用域：
// 标准可执行目录内按 basename 匹配；非标准目录必须整条路径显式白名单（防「搬到可写目录再执行」）。
func TestCheckShellWhitelist_PathScope(t *testing.T) {
	a := &Agent{cfg: &config.Config{AgentShellWhitelist: "ls,/opt/app/bin/ctl"}}
	if err := a.checkShellWhitelist("/bin/ls -la"); err != nil {
		t.Errorf("标准目录内的白名单命令应放行: %v", err)
	}
	if err := a.checkShellWhitelist("/opt/app/bin/ctl status"); err != nil {
		t.Errorf("显式白名单的整条路径应放行: %v", err)
	}
	for _, cmd := range []string{"/tmp/ls -la", "./ls", "../bin/ls", "/opt/app/bin/other"} {
		if err := a.checkShellWhitelist(cmd); err == nil {
			t.Errorf("非标准目录未显式白名单的命令 %q 应被拒绝", cmd)
		}
	}
}

// TestCheckShellWhitelist_EmptyWhitelistUnchanged 验证白名单为空时行为不变（向后兼容：
// 不限制命令，靠控制面 validateCommand + IAM 兜底）。
func TestCheckShellWhitelist_EmptyWhitelistUnchanged(t *testing.T) {
	a := &Agent{cfg: &config.Config{}}
	if err := a.checkShellWhitelist("ls && rm -rf /"); err != nil {
		t.Errorf("白名单为空时不应拦截（向后兼容语义）: %v", err)
	}
}

// TestSplitShellSegments 验证分隔符切分规则（重定向不得被误切）。
func TestSplitShellSegments(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"echo hi", 1},
		{"echo hi 1>&2", 1},
		{"echo hi &>file", 1},
		{"ls && cat", 2},
		{"ls && cat && date", 3},
		{"date || echo fallback", 2},
		{"cat /etc/passwd | sh", 2},
		{"ls | grep a | wc -l", 3},
	}
	for _, c := range cases {
		if got := len(splitShellSegments(c.in)); got != c.want {
			t.Errorf("splitShellSegments(%q) 段数 = %d, want %d（%v）", c.in, got, c.want, splitShellSegments(c.in))
		}
	}
}
