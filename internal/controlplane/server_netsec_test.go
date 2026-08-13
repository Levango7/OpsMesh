package controlplane

import (
	"net"
	"testing"
)

// ============================================================================
// task 248 SSRF 防护测试：ValidateWebhookURL + ValidateCIDR
// ============================================================================
//
// 测试策略：
//   - 使用 IP 字面量避免 DNS 解析依赖（测试环境可能无网络/DNS 受限）
//   - 公网 IP 用 8.8.8.8（Google DNS，公网公认地址）做 Happy 路径
//   - 私网/loopback/元数据地址覆盖所有 isPrivateIP 分支
//   - ValidateCIDR 测试白名单内/外/边界/空白名单

// TestValidateWebhookURL_Happy 公网 URL 通过校验。
// 使用 IP 字面量避免 DNS 解析依赖（测试环境可能无网络）。
func TestValidateWebhookURL_Happy(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"公网 IP http", "http://8.8.8.8/webhook"},
		{"公网 IP https", "https://8.8.8.8/webhook"},
		{"公网 IP 带端口", "https://8.8.8.8:8443/webhook"},
		{"公网 IP 203.0.113.1", "http://203.0.113.1/hook"}, // RFC5737 文档示例公网段
		{"公网 IP 198.51.100.1", "https://198.51.100.1/hook"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateWebhookURL(c.url, false); err != nil {
				t.Errorf("ValidateWebhookURL(%q, false) = %v, want nil", c.url, err)
			}
		})
	}
}

// TestValidateWebhookURL_Private 私网 URL 被拒（10/172.16/192.168）。
func TestValidateWebhookURL_Private(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"私网 A 10.x", "http://10.0.0.1/webhook"},
		{"私网 B 172.16.x", "http://172.16.0.1/webhook"},
		{"私网 B 172.31.x", "http://172.31.255.1/webhook"},
		{"私网 C 192.168.x", "http://192.168.1.1/webhook"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateWebhookURL(c.url, false); err == nil {
				t.Errorf("ValidateWebhookURL(%q, false) = nil, want error (private IP rejected)", c.url)
			}
		})
	}
}

// TestValidateWebhookURL_Loopback loopback 地址被拒（127.0.0.0/8）。
func TestValidateWebhookURL_Loopback(t *testing.T) {
	cases := []string{
		"http://127.0.0.1/webhook",
		"http://127.1.2.3/webhook",
		"https://127.0.0.1:8080/hook",
	}
	for _, url := range cases {
		if err := ValidateWebhookURL(url, false); err == nil {
			t.Errorf("ValidateWebhookURL(%q, false) = nil, want error (loopback rejected)", url)
		}
	}
}

// TestValidateWebhookURL_Metadata 云元数据地址被拒（169.254.169.254）。
func TestValidateWebhookURL_Metadata(t *testing.T) {
	cases := []string{
		"http://169.254.169.254/latest/meta-data/",   // AWS 元数据
		"http://169.254.169.254/computeMetadata/v1/", // GCP 元数据
		"http://169.254.1.1/hook",                    // 链路本地其他地址
	}
	for _, url := range cases {
		if err := ValidateWebhookURL(url, false); err == nil {
			t.Errorf("ValidateWebhookURL(%q, false) = nil, want error (metadata/link-local rejected)", url)
		}
	}
}

// TestValidateWebhookURL_BadProtocol 非 http/https 协议被拒。
func TestValidateWebhookURL_BadProtocol(t *testing.T) {
	cases := []string{
		"file:///etc/passwd",
		"gopher://8.8.8.8/x",
		"dict://8.8.8.8:6379/x",
		"ftp://8.8.8.8/file",
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
	}
	for _, url := range cases {
		if err := ValidateWebhookURL(url, false); err == nil {
			t.Errorf("ValidateWebhookURL(%q, false) = nil, want error (bad protocol rejected)", url)
		}
	}
}

// TestValidateWebhookURL_AllowPrivate allowPrivate=true 时私网 URL 通过（内网部署场景）。
func TestValidateWebhookURL_AllowPrivate(t *testing.T) {
	cases := []string{
		"http://10.0.0.1/webhook",
		"http://192.168.1.1/webhook",
		"http://127.0.0.1/webhook",
		"http://169.254.169.254/latest/meta-data/",
	}
	for _, url := range cases {
		if err := ValidateWebhookURL(url, true); err != nil {
			t.Errorf("ValidateWebhookURL(%q, true) = %v, want nil (allowPrivate should bypass)", url, err)
		}
	}
}

// TestValidateWebhookURL_ZeroNetwork 0.0.0.0/8 本网地址被拒（task 248 增强）。
func TestValidateWebhookURL_ZeroNetwork(t *testing.T) {
	cases := []string{
		"http://0.0.0.0/webhook",
		"http://0.1.2.3/webhook", // 0.0.0.0/8 网段
		"http://0.255.255.255/webhook",
	}
	for _, url := range cases {
		if err := ValidateWebhookURL(url, false); err == nil {
			t.Errorf("ValidateWebhookURL(%q, false) = nil, want error (0.0.0.0/8 rejected)", url)
		}
	}
}

// TestValidateWebhookURL_EmptyHost 空主机名被拒。
func TestValidateWebhookURL_EmptyHost(t *testing.T) {
	cases := []string{
		"http://",
		"http:///path",
		"https://",
	}
	for _, url := range cases {
		if err := ValidateWebhookURL(url, false); err == nil {
			t.Errorf("ValidateWebhookURL(%q, false) = nil, want error (empty host rejected)", url)
		}
	}
}

// TestValidateWebhookURL_InvalidURL 非法 URL 格式被拒。
func TestValidateWebhookURL_InvalidURL(t *testing.T) {
	// url.Parse 对控制字符会报错
	url := "http://exa mple.com/hook" // 空格在 host 中非法
	if err := ValidateWebhookURL(url, false); err == nil {
		t.Errorf("ValidateWebhookURL(%q, false) = nil, want error (invalid URL)", url)
	}
}

// TestValidateCIDR_Happy 白名单内 CIDR 通过校验。
func TestValidateCIDR_Happy(t *testing.T) {
	allowed := []string{"10.30.0.0/16", "192.168.0.0/16"}
	cases := []struct {
		name string
		cidr string
	}{
		{"完全包含 10.30.0.0/24", "10.30.0.0/24"},
		{"完全包含 10.30.1.0/24", "10.30.1.0/24"},
		{"完全包含 192.168.1.0/24", "192.168.1.0/24"},
		{"等于白名单 10.30.0.0/16", "10.30.0.0/16"},
		{"等于白名单 192.168.0.0/16", "192.168.0.0/16"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateCIDR(c.cidr, allowed); err != nil {
				t.Errorf("ValidateCIDR(%q, %v) = %v, want nil", c.cidr, allowed, err)
			}
		})
	}
}

// TestValidateCIDR_Rejected 白名单外 CIDR 被拒。
func TestValidateCIDR_Rejected(t *testing.T) {
	allowed := []string{"10.30.0.0/16", "192.168.0.0/16"}
	cases := []struct {
		name string
		cidr string
	}{
		{"完全在外 172.16.0.0/24", "172.16.0.0/24"},
		{"部分在外 10.0.0.0/8（超出 10.30.0.0/16）", "10.0.0.0/8"},
		{"部分在外 10.30.0.0/12（超出 10.30.0.0/16）", "10.30.0.0/12"},
		{"元数据网段 169.254.169.254/32", "169.254.169.254/32"},
		{"loopback 127.0.0.0/8", "127.0.0.0/8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateCIDR(c.cidr, allowed); err == nil {
				t.Errorf("ValidateCIDR(%q, %v) = nil, want error (CIDR outside whitelist)", c.cidr, allowed)
			}
		})
	}
}

// TestValidateCIDR_EmptyWhitelist 白名单为空时不校验（向后兼容）。
func TestValidateCIDR_EmptyWhitelist(t *testing.T) {
	cases := []string{
		"10.0.0.0/8",
		"169.254.169.254/32",
		"127.0.0.0/8",
		"0.0.0.0/0",
	}
	for _, cidr := range cases {
		if err := ValidateCIDR(cidr, nil); err != nil {
			t.Errorf("ValidateCIDR(%q, nil) = %v, want nil (empty whitelist = no validation)", cidr, err)
		}
		if err := ValidateCIDR(cidr, []string{}); err != nil {
			t.Errorf("ValidateCIDR(%q, []) = %v, want nil (empty whitelist = no validation)", cidr, err)
		}
	}
}

// TestValidateCIDR_InvalidTarget 非法目标 CIDR 被拒。
func TestValidateCIDR_InvalidTarget(t *testing.T) {
	allowed := []string{"10.0.0.0/8"}
	cases := []string{
		"not-a-cidr",
		"10.0.0.0", // 缺少掩码
		"999.999.999.999/24",
		"10.0.0.0/99",
	}
	for _, cidr := range cases {
		if err := ValidateCIDR(cidr, allowed); err == nil {
			t.Errorf("ValidateCIDR(%q, %v) = nil, want error (invalid target CIDR)", cidr, allowed)
		}
	}
}

// TestValidateCIDR_InvalidAllowed 非法白名单 CIDR 被拒。
func TestValidateCIDR_InvalidAllowed(t *testing.T) {
	allowed := []string{"not-a-cidr"}
	if err := ValidateCIDR("10.0.0.0/24", allowed); err == nil {
		t.Errorf("ValidateCIDR with invalid allowed CIDR should return error")
	}
}

// TestValidateCIDR_AllowlistWithSpaces 白名单带空格仍能正确解析（TrimSpace 兼容）。
func TestValidateCIDR_AllowlistWithSpaces(t *testing.T) {
	allowed := []string{" 10.30.0.0/16 ", " 192.168.0.0/16"}
	if err := ValidateCIDR("10.30.0.0/24", allowed); err != nil {
		t.Errorf("ValidateCIDR with spaces in allowed list should still work: %v", err)
	}
}

// TestIsPrivateIP 增强后的 isPrivateIP 覆盖测试（task 248：0.0.0.0/8 增强）。
func TestIsPrivateIP(t *testing.T) {
	private := []string{
		"10.0.0.1",
		"172.16.0.1",
		"172.31.255.1",
		"192.168.1.1",
		"127.0.0.1",
		"169.254.169.254",
		"169.254.1.1",
		"0.0.0.0",
		"0.1.2.3", // 0.0.0.0/8 增强
		"0.255.255.255",
		"::1", // IPv6 loopback
	}
	for _, ip := range private {
		parsed := parseIPMust(t, ip)
		if !isPrivateIP(parsed) {
			t.Errorf("isPrivateIP(%s) = false, want true", ip)
		}
	}
	public := []string{
		"8.8.8.8",
		"1.1.1.1",
		"203.0.113.1",
		"198.51.100.1",
		"172.32.0.1", // 172.32 不在 172.16-31 私网范围
		"11.0.0.1",   // 11.x 不是 10.x 私网
	}
	for _, ip := range public {
		parsed := parseIPMust(t, ip)
		if isPrivateIP(parsed) {
			t.Errorf("isPrivateIP(%s) = true, want false", ip)
		}
	}
}

// parseIPMust 解析 IP 字符串，失败时 t.Fatal。
func parseIPMust(t *testing.T, s string) net.IP {
	t.Helper()
	parsed := net.ParseIP(s)
	if parsed == nil {
		t.Fatalf("net.ParseIP(%q) = nil", s)
	}
	return parsed
}
