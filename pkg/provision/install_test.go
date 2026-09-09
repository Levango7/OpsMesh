package provision

import (
	"strings"
	"testing"
)

// TestInstallScript_DefaultAdvertise 验证 advertise 为空时回退 127.0.0.1:8080。
func TestInstallScript_DefaultAdvertise(t *testing.T) {
	script := InstallScript("", "v1.0.0")
	if !strings.Contains(script, "127.0.0.1:8080") {
		t.Fatalf("advertise 为空应回退 127.0.0.1:8080，脚本: %s", script)
	}
}

// TestInstallScript_DefaultVersion 验证 version 为空时回退 "unknown"。
func TestInstallScript_DefaultVersion(t *testing.T) {
	script := InstallScript("https://opsmesh.example.com", "")
	if !strings.Contains(script, "version=unknown") {
		t.Fatalf("version 为空应回退 unknown，脚本: %s", script)
	}
}

// TestInstallScript_BothDefaults 验证 advertise 与 version 均为空时同时回退。
func TestInstallScript_BothDefaults(t *testing.T) {
	script := InstallScript("", "")
	if !strings.Contains(script, "127.0.0.1:8080") {
		t.Fatalf("advertise 为空应回退 127.0.0.1:8080，脚本: %s", script)
	}
	if !strings.Contains(script, "version=unknown") {
		t.Fatalf("version 为空应回退 unknown，脚本: %s", script)
	}
}

// TestInstallScript_CustomParams 验证自定义 advertise/version 正确写入脚本头部与 ADVERTISE 变量。
func TestInstallScript_CustomParams(t *testing.T) {
	advertise := "https://opsmesh.example.com:8443"
	version := "v2.3.1"
	script := InstallScript(advertise, version)

	// 头部注释应包含 advertise 与 version
	if !strings.Contains(script, "advertise="+advertise) {
		t.Fatalf("脚本头部应包含 advertise=%s", advertise)
	}
	if !strings.Contains(script, "version="+version) {
		t.Fatalf("脚本头部应包含 version=%s", version)
	}
	// ADVERTISE 变量赋值
	if !strings.Contains(script, `ADVERTISE="`+advertise+`"`) {
		t.Fatalf("脚本应包含 ADVERTISE=%q 赋值", advertise)
	}
}

// TestInstallScript_TokenParsing 验证脚本包含 --token 解析逻辑与缺失 token 时报错退出。
func TestInstallScript_TokenParsing(t *testing.T) {
	script := InstallScript("https://opsmesh.example.com", "v1")

	// 应包含 --token= 解析
	if !strings.Contains(script, "--token=*)") {
		t.Fatal("脚本应包含 --token= 解析逻辑")
	}
	// 缺失 token 应报错退出
	if !strings.Contains(script, "--token is required") {
		t.Fatal("脚本应在缺失 token 时报错")
	}
	if !strings.Contains(script, "exit 1") {
		t.Fatal("脚本缺失 token 时应 exit 1")
	}
}

// TestInstallScript_AgentBinaryDownload 验证脚本包含 agent 二进制下载与 chmod。
func TestInstallScript_AgentBinaryDownload(t *testing.T) {
	advertise := "https://opsmesh.example.com:8443"
	script := InstallScript(advertise, "v1")

	// 应从 $ADVERTISE/bin/opsmesh-agent 下载
	if !strings.Contains(script, "$ADVERTISE/bin/opsmesh-agent") {
		t.Fatal("脚本应从 $ADVERTISE/bin/opsmesh-agent 下载 agent 二进制")
	}
	// 应 curl 下载
	if !strings.Contains(script, "curl -fsSL") {
		t.Fatal("脚本应使用 curl -fsSL 下载")
	}
	// 应 chmod +x
	if !strings.Contains(script, "chmod +x") {
		t.Fatal("脚本应 chmod +x agent 二进制")
	}
}

// TestInstallScript_TokenFileHardening 验证：
// token 写入文件（0600）而非通过命令行 --install-token 传递，避免 ps/auditd 泄露。
func TestInstallScript_TokenFileHardening(t *testing.T) {
	script := InstallScript("https://opsmesh.example.com", "v1")

	// token 应写入文件
	if !strings.Contains(script, "install.token") {
		t.Fatal("脚本应将 token 写入 install.token 文件")
	}
	// 文件权限应为 0600
	if !strings.Contains(script, "chmod 600") {
		t.Fatal("脚本应 chmod 600 install.token 文件")
	}
	// 加固：systemd ExecStart 行不应包含 --install-token 参数
	// （注释中提及 --install-token 是允许的，仅检查 ExecStart 赋值行）
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ExecStart=") && strings.Contains(trimmed, "--install-token") {
			t.Fatalf("加固：ExecStart 行不应包含 --install-token 参数（避免 ps 泄露 token）: %s", trimmed)
		}
	}
}

// TestInstallScript_SystemdUnit 验证脚本包含 systemd 服务单元生成逻辑。
func TestInstallScript_SystemdUnit(t *testing.T) {
	script := InstallScript("https://opsmesh.example.com", "v1")

	// 应包含 systemd 单元关键内容
	checks := []string{
		"systemctl daemon-reload",
		"systemctl enable opsmesh-agent",
		"systemctl restart opsmesh-agent",
		"opsmesh-agent.service",
		"Restart=always",
		"WantedBy=multi-user.target",
	}
	for _, c := range checks {
		if !strings.Contains(script, c) {
			t.Fatalf("脚本应包含 systemd 单元关键内容 %q", c)
		}
	}
}

// TestInstallScript_SystemdFallback 验证 systemd 不可用时的后台启动降级路径。
func TestInstallScript_SystemdFallback(t *testing.T) {
	script := InstallScript("https://opsmesh.example.com", "v1")

	if !strings.Contains(script, "systemd 不可用") {
		t.Fatal("脚本应包含 systemd 不可用时的降级提示")
	}
	if !strings.Contains(script, "agent started") {
		t.Fatal("脚本应包含后台启动成功提示")
	}
}

// TestInstallScript_ExecStartContainsControlAddrs 验证 systemd ExecStart 包含 --control-addrs 与 --data-dir。
func TestInstallScript_ExecStartContainsControlAddrs(t *testing.T) {
	advertise := "https://opsmesh.example.com:8443"
	script := InstallScript(advertise, "v1")

	if !strings.Contains(script, "--control-addrs=$ADVERTISE") {
		t.Fatal("ExecStart 应包含 --control-addrs=$ADVERTISE")
	}
	if !strings.Contains(script, "--data-dir=$DATA_DIR") {
		t.Fatal("ExecStart 应包含 --data-dir=$DATA_DIR")
	}
}

// TestInstallScript_BootstrapDone 验证脚本末尾包含 bootstrap 完成提示。
func TestInstallScript_BootstrapDone(t *testing.T) {
	script := InstallScript("https://opsmesh.example.com", "v1")

	if !strings.Contains(script, "bootstrap done") {
		t.Fatal("脚本应包含 bootstrap done 完成提示")
	}
	if !strings.Contains(script, "agent will register with token") {
		t.Fatal("脚本应提示 agent 将携带 token 注册")
	}
}
