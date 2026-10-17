// ops_validation_test.go P3-P7 运维能力的校验/白名单/脚本构造测试。
//
// 覆盖重点（安全相关性最高的面）：
//   - SSH 互信公钥校验（注入防护入口：换行/heredoc 终止符/格式拒绝）
//   - NTP/DNS/文件共享的参数校验（时区/CIDR/记录值/绝对路径/挂载参数白名单）
//   - 安全运维的 CVE/包名/基线映射（基线修复映射与 compliance 规则 ID 完整对齐）
//   - Kernel/中间件/向量等白名单与正则 spot check
//   - 若干 build*Script 构造的注入面（恶意值必须在校验层被拒；合法值产出无裸拼接）
package controlplane

import (
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/compliance"
)

// ---------------------------------------------------------------------------
// SSH 互信：公钥校验（注入防护核心）
// ---------------------------------------------------------------------------

func TestValidSSHPublicKey(t *testing.T) {
	valid := []string{
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIN8x opsmesh@host",
		"ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQ== root@node1",
		"ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTY= user@dev",
		"sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lZDI1NTE5QG9wZW5zc2guY29tAAAAI=",
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIN8x", // 无注释也合法
	}
	for _, v := range valid {
		if !validSSHPublicKey(v) {
			t.Errorf("validSSHPublicKey should accept %q", v)
		}
	}

	invalid := []string{
		"",                               // 空
		"ssh-ed25519 AAAA\nssh-rsa BBBB", // 换行（双写注入）
		"ssh-ed25519 AAAA\r\n",           // CRLF
		"ssh-ed25519 AAAA\x00",           // NUL
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 " + sshTrustHeredocEOF, // heredoc 终止符
		"ssh-ed25519 AAAA; rm -rf /",                             // 元字符
		"ssh-ed25519 $(id) comment",                              // 命令替换
		"unknown-algo AAAAC3NzaC1lZDI1NTE5",                      // 算法不在白名单
		"ssh-ed25519",                                            // 缺 base64
		"cmd=uptime",                                             // 非公钥
	}
	for _, v := range invalid {
		if validSSHPublicKey(v) {
			t.Errorf("validSSHPublicKey should reject %q", v)
		}
	}
}

// TestBuildSSHTrustAuthorizeScript 公钥以 printf %s + 幂等 grep -qF 写入（无裸拼接）。
func TestBuildSSHTrustAuthorizeScript(t *testing.T) {
	pk := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIN8x opsmesh@host"
	script := buildSSHTrustScript(sshTrustReq{
		Action: "authorize", User: "root", PublicKey: pk,
	})
	// 幂等：先 grep 再条件追加。
	if !strings.Contains(script, "grep -qF") {
		t.Error("authorize script should check existence with grep -qF (idempotent)")
	}
	// 公钥经引号包裹写入（shellQuote → 双引号），不裸拼。
	if !strings.Contains(script, `printf '%s\n' "`+pk+`"`) {
		t.Errorf("authorize script should write pubkey via quoted printf, got:\n%s", script)
	}
	// 显式 chmod 600。
	if !strings.Contains(script, "chmod 600") {
		t.Error("authorize script should chmod 600 authorized_keys")
	}
}

// TestBuildSSHTrustTestScript BatchMode 防交互挂起 + 接受新主机指纹。
func TestBuildSSHTrustTestScript(t *testing.T) {
	script := buildSSHTrustScript(sshTrustReq{
		Action: "test", User: "root", Host: "10.0.0.2", Port: 2222,
	})
	for _, want := range []string{"BatchMode=yes", "StrictHostKeyChecking=accept-new", "ConnectTimeout=5", "-p 2222", "root@10.0.0.2"} {
		if !strings.Contains(script, want) {
			t.Errorf("test script missing %q, got:\n%s", want, script)
		}
	}
}

// ---------------------------------------------------------------------------
// NTP：时区/CIDR/时间源校验
// ---------------------------------------------------------------------------

func TestNTPValidation(t *testing.T) {
	validTZ := []string{"UTC", "Asia/Shanghai", "America/Argentina/Buenos_Aires", "Etc/GMT+8", "Asia"}
	for _, v := range validTZ {
		if !timezoneRe.MatchString(v) {
			t.Errorf("timezoneRe should accept %q", v)
		}
	}
	// 注：形状合法即放行（如 "Asia"），真实有效性由 timedatectl 兜底（脚本 set -e 可见失败）。
	invalidTZ := []string{"", "/Shanghai", "Asia/", "a b", "Asia/Shanghai;rm", "$(id)", "Asia//Shanghai", "1Asia/X"}
	for _, v := range invalidTZ {
		if timezoneRe.MatchString(v) {
			t.Errorf("timezoneRe should reject %q", v)
		}
	}

	validCIDR := []string{"10.0.0.0/8", "192.168.1.0/24", "0.0.0.0/0"}
	for _, v := range validCIDR {
		if !cidrRe.MatchString(v) {
			t.Errorf("cidrRe should accept %q", v)
		}
	}
	for _, v := range []string{"", "10.0.0.0", "abc/8", "10.0.0.0/999", "10.0.0.0/8;rm"} {
		if cidrRe.MatchString(v) {
			t.Errorf("cidrRe should reject %q", v)
		}
	}

	// 时间源主机（host 或 host:port）沿用 monitorHostRe。
	validHosts := []string{"ntp.aliyun.com", "10.0.0.1", "ntp1.internal.local"}
	for _, v := range validHosts {
		if !monitorHostRe.MatchString(v) {
			t.Errorf("monitorHostRe should accept ntp server %q", v)
		}
	}
}

// TestBuildNTPConfigScript 时间源写入 drop-in 并重启 chronyd。
func TestBuildNTPConfigScript(t *testing.T) {
	script := buildNTPScript(ntpReq{
		Action: "config", Servers: []string{"ntp.aliyun.com", "10.0.0.1"},
	})
	for _, want := range []string{"server ntp.aliyun.com iburst", "server 10.0.0.1 iburst", "restart chronyd", "OPSMESH_NTP_EOF"} {
		if !strings.Contains(script, want) {
			t.Errorf("ntp config script missing %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// DNS：记录值/路径/名称校验
// ---------------------------------------------------------------------------

func TestValidDNSRecordValue(t *testing.T) {
	cases := []struct {
		recordType, value string
		want              bool
	}{
		{"A", "10.0.0.5", true},
		{"A", "999.0.0.5", true}, // 粗校验（数值范围由 DNS 服务端兜底）
		{"A", "10.0.0", false},
		{"A", "10.0.0.5;rm", false},
		{"AAAA", "::1", true},
		{"AAAA", "2001:db8::1", true},
		{"AAAA", "10.0.0.5", false}, // 无冒号
		{"CNAME", "target.example.com", true},
		{"CNAME", "bad name", false},
		{"MX", "mail.example.com", true},
		{"MX", "10 mail.example.com", true},
		{"MX", "abc mail.example.com", false},
		{"TXT", "v=spf1 include:_spf.example.com ~all", true},
		{"TXT", "line\nbreak", false},
		{"A", "", false},
	}
	for _, c := range cases {
		if got := validDNSRecordValue(c.recordType, c.value); got != c.want {
			t.Errorf("validDNSRecordValue(%q,%q)=%v want %v", c.recordType, c.value, got, c.want)
		}
	}
}

func TestDNSNameAndZoneRe(t *testing.T) {
	for _, v := range []string{"db.internal.local", "*.example.com", "host1", "_service.example.com"} {
		if !dnsNameRe.MatchString(v) {
			t.Errorf("dnsNameRe should accept %q", v)
		}
	}
	for _, v := range []string{"", "bad name", "host;rm", "$(id).com"} {
		if dnsNameRe.MatchString(v) {
			t.Errorf("dnsNameRe should reject %q", v)
		}
	}
	for _, v := range []string{"opsmesh.internal", "a.b.c.example.com"} {
		if !dnsZoneRe.MatchString(v) {
			t.Errorf("dnsZoneRe should accept %q", v)
		}
	}
	for _, v := range []string{"", "single", "*.wild.com", "bad zone"} {
		if dnsZoneRe.MatchString(v) {
			t.Errorf("dnsZoneRe should reject %q", v)
		}
	}
}

// ---------------------------------------------------------------------------
// 文件共享：路径 && 挂载参数 && NFS 选项白名单
// ---------------------------------------------------------------------------

func TestValidFSPath(t *testing.T) {
	for _, v := range []string{"/data/share", "/mnt/nfs-1/x_y", "/"} {
		if !validFSPath(v) {
			t.Errorf("validFSPath should accept %q", v)
		}
	}
	for _, v := range []string{
		"", "relative/path", "/data/../etc", "/data share", "/data/x;rm",
		"/data/$(id)", "/data/x`id`", "/data/x'y", "/data/x\"y",
	} {
		if validFSPath(v) {
			t.Errorf("validFSPath should reject %q", v)
		}
	}
}

func TestValidMountOpts(t *testing.T) {
	for _, v := range []string{"", "rw", "rw,hard,intr,noatime", "nfsvers=4,rsize=65536"} {
		if !validMountOpts(v) {
			t.Errorf("validMountOpts should accept %q", v)
		}
	}
	for _, v := range []string{"rw;rm", "rw,auto", "nosuid", "rw,intr,$(id)", "rw,hard,intr;reboot"} {
		if validMountOpts(v) {
			t.Errorf("validMountOpts should reject %q", v)
		}
	}
}

func TestAllowedNFSOption(t *testing.T) {
	for _, v := range []string{"rw", "ro", "sync", "no_root_squash", "root_squash", "no_subtree_check", "anonuid=1000", "fsid=0"} {
		if !isAllowedNFSOption(v) {
			t.Errorf("isAllowedNFSOption should accept %q", v)
		}
	}
	for _, v := range []string{"", "evil", "rw;rm", "no_all_squash", "$(id)", "foo=bar"} {
		if isAllowedNFSOption(v) {
			t.Errorf("isAllowedNFSOption should reject %q", v)
		}
	}
}

func TestValidFSOptions(t *testing.T) {
	if !validFSOptions("nfs", "rw,sync,no_root_squash") {
		t.Error("nfs options should accept standard set")
	}
	if validFSOptions("nfs", "rw,evil_option") {
		t.Error("nfs options should reject unknown option")
	}
	if !validFSOptions("samba", "browseable = yes") {
		t.Error("samba options should accept key=value")
	}
	if validFSOptions("samba", "browseable = yes; rm -rf /") {
		t.Error("samba options should reject metachars")
	}
}

// ---------------------------------------------------------------------------
// 安全运维：CVE/包名 && 基线修复映射完整性
// ---------------------------------------------------------------------------

func TestSecurityValidation(t *testing.T) {
	for _, v := range []string{"CVE-2021-44228", "CVE-2014-0160", "CVE-2024-1234567"} {
		if !cveIDRe.MatchString(v) {
			t.Errorf("cveIDRe should accept %q", v)
		}
	}
	for _, v := range []string{"", "CVE-21-44228", "cve-2021-44228", "CVE-2021-44", "CVE-2021-44228;rm"} {
		if cveIDRe.MatchString(v) {
			t.Errorf("cveIDRe should reject %q", v)
		}
	}

	for _, v := range []string{"log4j-core", "httpd", "openssl", "pkg+dfsg-1", "pkg:amd64", "nginx-1.20~rc1"} {
		if !securityPackageRe.MatchString(v) {
			t.Errorf("securityPackageRe should accept %q", v)
		}
	}
	for _, v := range []string{"", "pkg;rm", "pkg name", "$(id)", "pkg|x"} {
		if securityPackageRe.MatchString(v) {
			t.Errorf("securityPackageRe should reject %q", v)
		}
	}
}

// TestBaselineFixMappingCoversAllComplianceRules 基线修复映射覆盖 compliance 引擎的全部规则 ID。
//
// 防漂移：compliance 增规则而 security_ops 未补修复映射时判红（修复动作会 400「no fix script」）。
func TestBaselineFixMappingCoversAllComplianceRules(t *testing.T) {
	engine := compliance.NewEngine()
	rules := engine.ListRules()
	if len(rules) == 0 {
		t.Fatal("compliance engine returned no rules")
	}
	for _, r := range rules {
		if _, ok := baselineFixScripts[r.ID]; !ok {
			t.Errorf("baselineFixScripts missing fix script for compliance rule %s (%s)", r.ID, r.Name)
		}
	}
	// 反向：映射中的 ID 必须都是真实规则（防拼写漂移）。
	ruleSet := make(map[string]bool, len(rules))
	for _, r := range rules {
		ruleSet[r.ID] = true
	}
	for id := range baselineFixScripts {
		if !ruleSet[id] {
			t.Errorf("baselineFixScripts has stale rule id %s not present in compliance engine", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Kernel：参数/模块/版本/值 正则
// ---------------------------------------------------------------------------

func TestKernelValidation(t *testing.T) {
	for _, v := range []string{"net.ipv4.ip_forward", "vm.swappiness", "kernel.pid_max"} {
		if !kernelParamRe.MatchString(v) {
			t.Errorf("kernelParamRe should accept %q", v)
		}
	}
	for _, v := range []string{"", "net ipv4", "net;rm", "$(id)", "Net.Ipv4"} {
		if kernelParamRe.MatchString(v) {
			t.Errorf("kernelParamRe should reject %q", v)
		}
	}

	for _, v := range []string{"br_netfilter", "ip_vs", "kvm-intel"} {
		if !kernelModuleRe.MatchString(v) {
			t.Errorf("kernelModuleRe should accept %q", v)
		}
	}
	for _, v := range []string{"", "mod;rm", "mod name", "$(id)"} {
		if kernelModuleRe.MatchString(v) {
			t.Errorf("kernelModuleRe should reject %q", v)
		}
	}

	for _, v := range []string{"5.15.0", "6.1.0-13-generic", "5.10.0-28.1.el7"} {
		if !kernelVersionRe.MatchString(v) {
			t.Errorf("kernelVersionRe should accept %q", v)
		}
	}
	for _, v := range []string{"", "5.15", "5.15.0;rm", "$(uname)"} {
		if kernelVersionRe.MatchString(v) {
			t.Errorf("kernelVersionRe should reject %q", v)
		}
	}

	// 典型 sysctl 值：数字/布尔/IPv6（冒号）/多值（空格）/路径。
	for _, v := range []string{"1", "65535", "10.0.0.5", "1024 2048", "::1", "/proc/x", "1,2"} {
		if !kernelValueRe.MatchString(v) {
			t.Errorf("kernelValueRe should accept %q", v)
		}
	}
	// 注：值为收紧白名单（不含 =、$、引号等），超出形状由 400 拒绝。
	for _, v := range []string{"", "val;rm", "$(id)", "val|cat", "val`id`", "a=b"} {
		if kernelValueRe.MatchString(v) {
			t.Errorf("kernelValueRe should reject %q", v)
		}
	}
}

// ---------------------------------------------------------------------------
// 中间件/向量/OLAP 等白名单 spot check
// ---------------------------------------------------------------------------

func TestMiddlewareCatalogs(t *testing.T) {
	// 每类型的 mode 白名单非空，且 redis 覆盖四种部署模式。
	for typ, modes := range allowedMiddlewareModes {
		if len(modes) == 0 {
			t.Errorf("allowedMiddlewareModes[%s] empty", typ)
		}
	}
	for _, m := range []string{"standalone", "master-slave", "sentinel", "cluster"} {
		if !allowedMiddlewareModes["redis"][m] {
			t.Errorf("redis mode %q should be allowed", m)
		}
	}
	// 动作白名单覆盖生命周期集。
	for _, a := range []string{"start", "stop", "restart", "status"} {
		if !allowedMiddlewareActions[a] {
			t.Errorf("middleware action %q should be allowed", a)
		}
	}
	if !kafkaTopicRe.MatchString("order-events.v2") {
		t.Error("kafkaTopicRe should accept dotted topic")
	}
	if kafkaTopicRe.MatchString("topic;rm") {
		t.Error("kafkaTopicRe should reject metachars")
	}
}

func TestFloatSliceToJSON(t *testing.T) {
	if got := floatSliceToJSON([]float64{0.1, 1, -2.5}); got != "[0.1,1,-2.5]" {
		t.Errorf("floatSliceToJSON = %q", got)
	}
	if got := floatSliceToJSON(nil); got != "[]" {
		t.Errorf("floatSliceToJSON(nil) = %q", got)
	}
}

func TestMiscNameRes(t *testing.T) {
	cases := []struct {
		name string
		re   interface{ MatchString(string) bool }
		ok   []string
		bad  []string
	}{
		{"vectorCollectionRe", vectorCollectionRe, []string{"docs_v2", "A1"}, []string{"", "a-b", "a b", "a;b"}},
		{"olapNameRe", olapNameRe, []string{"analytics", "dws_1"}, []string{"", "a-b", "a.b", "a;b"}},
		{"tsdbDBRe", tsdbDBRe, []string{"metrics", "db-1"}, []string{"", "db.1", "db;1"}},
		{"graphDatabaseRe", graphDatabaseRe, []string{"knowgraph", "g1"}, []string{"", "g-1", "g.1"}},
		{"mongoNameRe", mongoNameRe, []string{"app", "app.prod", "app-1"}, []string{"", "app;1", "app x"}},
		{"harborProjectRe", harborProjectRe, []string{"library", "team-a", "app.v2"}, []string{"", "a b", "a;b"}},
		{"ansiblePlaybookRe", ansiblePlaybookRe, []string{"deploy-web", "site.v2"}, []string{"", "a b", "a/b"}},
		{"fsShareNameRe", fsShareNameRe, []string{"share1", "team-data"}, []string{"", "share 1", "share/1"}},
	}
	for _, c := range cases {
		for _, v := range c.ok {
			if !c.re.MatchString(v) {
				t.Errorf("%s should accept %q", c.name, v)
			}
		}
		for _, v := range c.bad {
			if c.re.MatchString(v) {
				t.Errorf("%s should reject %q", c.name, v)
			}
		}
	}
}

// TestAnsibleModuleWhitelist 常用模块在册、危险面不收（如 raw 需要显式评估）。
func TestAnsibleModuleWhitelist(t *testing.T) {
	for _, m := range []string{"ping", "command", "shell", "copy", "file", "service", "systemd", "yum", "apt", "setup"} {
		if !allowedAnsibleModules[m] {
			t.Errorf("ansible module %q should be allowed", m)
		}
	}
	if allowedAnsibleModules["raw"] || allowedAnsibleModules["script"] {
		t.Error("raw/script modules should not be in default whitelist")
	}
}
