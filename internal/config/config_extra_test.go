// config_extra_test.go 补充 config.go 未覆盖的 Validate 分支与辅助函数。
package config

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Validate 端口范围校验
// =============================================================================

func TestValidate_InvalidHTTPPort(t *testing.T) {
	c := base()
	c.HTTPPort = 0
	if err := c.Validate(); err == nil {
		t.Fatal("http-port=0 应被拒绝")
	}
	c.HTTPPort = 70000
	if err := c.Validate(); err == nil {
		t.Fatal("http-port=70000 应被拒绝")
	}
}

func TestValidate_InvalidGRPCPort(t *testing.T) {
	c := base()
	c.GRPCPort = -1
	if err := c.Validate(); err == nil {
		t.Fatal("grpc-port=-1 应被拒绝")
	}
}

func TestValidate_InvalidMetricsPort(t *testing.T) {
	c := base()
	c.MetricsPort = 0
	if err := c.Validate(); err == nil {
		t.Fatal("metrics-port=0 应被拒绝")
	}
}

// =============================================================================
// Validate agent 模式校验
// =============================================================================

func TestValidate_AgentWorkerConcurrency(t *testing.T) {
	c := base()
	c.Mode = "agent"
	c.WorkerConcurrency = 0
	if err := c.Validate(); err == nil {
		t.Fatal("agent worker-concurrency=0 应被拒绝")
	}
}

func TestValidate_AgentTaskTimeout(t *testing.T) {
	c := base()
	c.Mode = "agent"
	c.TaskTimeout = 0
	if err := c.Validate(); err == nil {
		t.Fatal("agent task-timeout=0 应被拒绝")
	}
}

func TestValidate_AgentOK(t *testing.T) {
	c := base()
	c.Mode = "agent"
	if err := c.Validate(); err != nil {
		t.Fatalf("agent 合法配置应通过: %v", err)
	}
}

// =============================================================================
// Validate task-lease-sec 校验
// =============================================================================

func TestValidate_TaskLeaseSecZero(t *testing.T) {
	c := base()
	c.TaskLeaseSec = 0
	if err := c.Validate(); err == nil {
		t.Fatal("task-lease-sec=0 应被拒绝")
	}
}

// =============================================================================
// Validate discover 校验
// =============================================================================

func TestValidate_DiscoverMissingCIDR(t *testing.T) {
	c := base()
	c.Discover = true
	c.SegmentCIDR = ""
	if err := c.Validate(); err == nil {
		t.Fatal("discover 开启但 CIDR 为空应被拒绝")
	}
}

func TestValidate_DiscoverInvalidCIDR(t *testing.T) {
	c := base()
	c.Discover = true
	c.SegmentCIDR = "not-a-cidr"
	if err := c.Validate(); err == nil {
		t.Fatal("discover 非法 CIDR 应被拒绝")
	}
}

func TestValidate_DiscoverOK(t *testing.T) {
	c := base()
	c.Discover = true
	c.SegmentCIDR = "10.30.0.0/24"
	if err := c.Validate(); err != nil {
		t.Fatalf("discover 合法 CIDR 应通过: %v", err)
	}
}

// =============================================================================
// Validate MultiSchema 校验
// =============================================================================

func TestValidate_MultiSchemaNonMysql(t *testing.T) {
	c := base()
	c.MultiSchema = true
	c.Store = "memory"
	if err := c.Validate(); err == nil {
		t.Fatal("multi-schema + memory 应被拒绝")
	}
}

func TestValidate_MultiSchemaOK(t *testing.T) {
	c := base()
	c.MultiSchema = true
	c.Store = "mysql"
	c.MySQLDSN = "u:p@tcp(db:3306)/ops_device"
	if err := c.Validate(); err != nil {
		t.Fatalf("multi-schema + mysql 应通过: %v", err)
	}
}

// =============================================================================
// Validate FederationPeers 校验
// =============================================================================

func TestValidate_FederationPeersInvalidURL(t *testing.T) {
	c := base()
	c.FederationPeers = []string{"://bad"}
	c.FederationSecret = "secret"
	if err := c.Validate(); err == nil {
		t.Fatal("federation peer 非法 URL 应被拒绝")
	}
}

func TestValidate_FederationPeersMissingScheme(t *testing.T) {
	c := base()
	c.FederationPeers = []string{"peer1:8080"}
	c.FederationSecret = "secret"
	if err := c.Validate(); err == nil {
		t.Fatal("federation peer 缺 scheme 应被拒绝")
	}
}

func TestValidate_FederationPeersOK(t *testing.T) {
	c := base()
	c.FederationPeers = []string{"http://peer1:8080", "http://peer2:8080"}
	c.FederationSecret = "shared-secret"
	if err := c.Validate(); err != nil {
		t.Fatalf("federation peers 合法配置应通过: %v", err)
	}
}

func TestValidate_FederationPeersMissingSecret(t *testing.T) {
	c := base()
	c.FederationPeers = []string{"http://peer1:8080"}
	c.FederationSecret = ""
	if err := c.Validate(); err == nil {
		t.Fatal("federation peers 缺 secret 应被拒绝")
	}
}

// =============================================================================
// Validate MetricsAllowCIDR 校验
// =============================================================================

func TestValidate_MetricsAllowCIDRInvalid(t *testing.T) {
	c := base()
	c.MetricsAllowCIDR = "not-a-cidr"
	if err := c.Validate(); err == nil {
		t.Fatal("非法 metrics-allow-cidr 应被拒绝")
	}
}

func TestValidate_MetricsAllowCIDROK(t *testing.T) {
	c := base()
	c.MetricsAllowCIDR = "10.0.0.0/8, 192.168.0.0/16"
	if err := c.Validate(); err != nil {
		t.Fatalf("合法 metrics-allow-cidr 应通过: %v", err)
	}
}

func TestValidate_MetricsAllowCIDRWithEmptyItem(t *testing.T) {
	c := base()
	c.MetricsAllowCIDR = "10.0.0.0/8,, 192.168.0.0/16"
	if err := c.Validate(); err != nil {
		t.Fatalf("含空项的 metrics-allow-cidr 应通过: %v", err)
	}
}

// =============================================================================
// Validate FederationPort 校验
// =============================================================================

func TestValidate_FederationPortTooHigh(t *testing.T) {
	c := base()
	c.FederationPort = 70000
	if err := c.Validate(); err == nil {
		t.Fatal("federation-port>65535 应被拒绝")
	}
}

func TestValidate_FederationPortMissingCert(t *testing.T) {
	c := base()
	c.FederationPort = 9093
	c.FederationTLSCert = ""
	c.FederationTLSKey = ""
	if err := c.Validate(); err == nil {
		t.Fatal("federation-port>0 但缺 TLS cert/key 应被拒绝")
	}
}

func TestValidate_FederationPortOK(t *testing.T) {
	c := base()
	c.FederationPort = 9093
	c.FederationTLSCert = "cert.pem"
	c.FederationTLSKey = "key.pem"
	if err := c.Validate(); err != nil {
		t.Fatalf("federation-port 合法配置应通过: %v", err)
	}
}

// =============================================================================
// Validate Production + TLS 合法
// =============================================================================

func TestValidate_ProductionWithTLS(t *testing.T) {
	c := base()
	c.Production = true
	c.TLSCert = "tls.crt"
	c.JWTSecret = "0123456789abcdef0123456789abcdef"
	c.EncryptionKey = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=" // 32 字节 base64
	if err := c.Validate(); err != nil {
		t.Fatalf("production + TLS + JWT 应通过: %v", err)
	}
}

// =============================================================================
// parseFederationPeers 纯函数
// =============================================================================

func TestParseFederationPeers_Empty(t *testing.T) {
	if got := parseFederationPeers(""); got != nil {
		t.Fatalf("空串应返回 nil, got %v", got)
	}
}

func TestParseFederationPeers_Single(t *testing.T) {
	got := parseFederationPeers("http://peer1:8080")
	if len(got) != 1 || got[0] != "http://peer1:8080" {
		t.Fatalf("got=%v", got)
	}
}

func TestParseFederationPeers_Multiple(t *testing.T) {
	got := parseFederationPeers("http://peer1:8080, http://peer2:8080 , , ")
	if len(got) != 2 {
		t.Fatalf("got=%v, want 2 peers", got)
	}
	if got[0] != "http://peer1:8080" || got[1] != "http://peer2:8080" {
		t.Fatalf("got=%v", got)
	}
}

func TestParseFederationPeers_AllWhitespace(t *testing.T) {
	if got := parseFederationPeers("  ,  ,  "); got != nil {
		t.Fatalf("全空白应返回 nil, got %v", got)
	}
}

// =============================================================================
// 辅助函数：boolEnv / envInt / envInt64 / durationEnv / getEnv
// =============================================================================

func TestBoolEnv(t *testing.T) {
	os.Setenv("TEST_BOOL_TRUE", "true")
	defer os.Unsetenv("TEST_BOOL_TRUE")
	if !boolEnv("TEST_BOOL_TRUE", false) {
		t.Fatal("true should be true")
	}

	os.Setenv("TEST_BOOL_ONE", "1")
	defer os.Unsetenv("TEST_BOOL_ONE")
	if !boolEnv("TEST_BOOL_ONE", false) {
		t.Fatal("1 should be true")
	}

	os.Setenv("TEST_BOOL_FALSE", "false")
	defer os.Unsetenv("TEST_BOOL_FALSE")
	if boolEnv("TEST_BOOL_FALSE", true) {
		t.Fatal("false should be false")
	}

	if boolEnv("TEST_BOOL_UNSET", false) {
		t.Fatal("unset with default false should return false")
	}
	if !boolEnv("TEST_BOOL_UNSET2", true) {
		t.Fatal("unset with default true should return true")
	}
}

func TestEnvInt(t *testing.T) {
	os.Setenv("TEST_INT_42", "42")
	defer os.Unsetenv("TEST_INT_42")
	if got := envInt("TEST_INT_42", 0); got != 42 {
		t.Fatalf("got=%d, want 42", got)
	}

	os.Setenv("TEST_INT_BAD", "abc")
	defer os.Unsetenv("TEST_INT_BAD")
	if got := envInt("TEST_INT_BAD", 99); got != 99 {
		t.Fatalf("invalid should return default, got=%d", got)
	}

	if got := envInt("TEST_INT_UNSET", 7); got != 7 {
		t.Fatalf("unset should return default, got=%d", got)
	}
}

func TestEnvInt64(t *testing.T) {
	os.Setenv("TEST_INT64_100", "100")
	defer os.Unsetenv("TEST_INT64_100")
	if got := envInt64("TEST_INT64_100", 0); got != 100 {
		t.Fatalf("got=%d, want 100", got)
	}

	os.Setenv("TEST_INT64_BAD", "xyz")
	defer os.Unsetenv("TEST_INT64_BAD")
	if got := envInt64("TEST_INT64_BAD", 1); got != 1 {
		t.Fatalf("invalid should return default, got=%d", got)
	}

	if got := envInt64("TEST_INT64_UNSET", 5); got != 5 {
		t.Fatalf("unset should return default, got=%d", got)
	}
}

func TestDurationEnv(t *testing.T) {
	os.Setenv("TEST_DUR_120S", "120s")
	defer os.Unsetenv("TEST_DUR_120S")
	if got := durationEnv("TEST_DUR_120S", 0); got != 120*time.Second {
		t.Fatalf("got=%v, want 120s", got)
	}

	os.Setenv("TEST_DUR_BAD", "not-a-duration")
	defer os.Unsetenv("TEST_DUR_BAD")
	if got := durationEnv("TEST_DUR_BAD", 30*time.Second); got != 30*time.Second {
		t.Fatalf("invalid should return default, got=%v", got)
	}

	if got := durationEnv("TEST_DUR_UNSET", 60*time.Second); got != 60*time.Second {
		t.Fatalf("unset should return default, got=%v", got)
	}
}

func TestGetEnv(t *testing.T) {
	os.Setenv("TEST_STR_HELLO", "hello")
	defer os.Unsetenv("TEST_STR_HELLO")
	if got := getEnv("TEST_STR_HELLO", "default"); got != "hello" {
		t.Fatalf("got=%q, want hello", got)
	}

	if got := getEnv("TEST_STR_UNSET", "default"); got != "default" {
		t.Fatalf("unset should return default, got=%q", got)
	}
}

// =============================================================================
// 补充 Validate 分支
// =============================================================================

func TestValidate_LogBackendSQL(t *testing.T) {
	c := base()
	c.LogBackend = "sql"
	if err := c.Validate(); err != nil {
		t.Fatalf("log-backend=sql 应通过: %v", err)
	}
}

func TestValidate_DiscoverWithAutoProvision(t *testing.T) {
	c := base()
	c.Discover = true
	c.SegmentCIDR = "10.30.0.0/24"
	c.AutoProvision = true
	if err := c.Validate(); err != nil {
		t.Fatalf("discover + auto-provision 应通过: %v", err)
	}
}

func TestValidate_ProductionFullConfig(t *testing.T) {
	c := base()
	c.Production = true
	c.TLSCert = "tls.crt"
	c.JWTSecret = "0123456789abcdef0123456789abcdef"
	c.EncryptionKey = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=" // 32 字节 base64
	c.Store = "mysql"
	c.MySQLDSN = "u:p@tcp(db:3306)/ops_device"
	c.Replicas = 3
	// H2/H3 配套：P1-P6 全部 15 个领域已实现 MySQL 持久化，stubStoreDomains 为空，
	// 生产 + mysql 后端不再拒绝启动（无桩领域则无须放行门槛）。AllowStubStores=true 仍兼容放行。
	// 本用例验证"完整 production 配置"通过。
	c.AllowStubStores = true
	if err := c.Validate(); err != nil {
		t.Fatalf("production full config 应通过: %v", err)
	}
}

func TestValidate_AllPortsBoundary(t *testing.T) {
	c := base()
	c.HTTPPort = 1
	c.GRPCPort = 65535
	c.MetricsPort = 1
	if err := c.Validate(); err != nil {
		t.Fatalf("boundary ports 应通过: %v", err)
	}
}

// =============================================================================
// 辅助函数：float64Env / parseLogPushFiles
// =============================================================================

func TestFloat64Env(t *testing.T) {
	os.Setenv("TEST_FLOAT_42", "42.5")
	defer os.Unsetenv("TEST_FLOAT_42")
	if got := float64Env("TEST_FLOAT_42", 0); got != 42.5 {
		t.Fatalf("got=%v, want 42.5", got)
	}

	os.Setenv("TEST_FLOAT_BAD", "abc")
	defer os.Unsetenv("TEST_FLOAT_BAD")
	if got := float64Env("TEST_FLOAT_BAD", 1.5); got != 1.5 {
		t.Fatalf("invalid should return default, got=%v", got)
	}

	if got := float64Env("TEST_FLOAT_UNSET", 2.5); got != 2.5 {
		t.Fatalf("unset should return default, got=%v", got)
	}
}

func TestParseLogPushFiles_Empty(t *testing.T) {
	if got := parseLogPushFiles(""); got != nil {
		t.Fatalf("空串应返回 nil, got %v", got)
	}
}

func TestParseLogPushFiles_Single(t *testing.T) {
	got := parseLogPushFiles("/var/log/syslog")
	if len(got) != 1 || got[0] != "/var/log/syslog" {
		t.Fatalf("got=%v", got)
	}
}

func TestParseLogPushFiles_Multiple(t *testing.T) {
	got := parseLogPushFiles("/var/log/syslog, /var/log/app.log , , ")
	if len(got) != 2 {
		t.Fatalf("got=%v, want 2 files", got)
	}
	if got[0] != "/var/log/syslog" || got[1] != "/var/log/app.log" {
		t.Fatalf("got=%v", got)
	}
}

func TestParseLogPushFiles_AllWhitespace(t *testing.T) {
	if got := parseLogPushFiles("  ,  ,  "); got != nil {
		t.Fatalf("全空白应返回 nil, got %v", got)
	}
}

// =============================================================================
// Validate NotifyChannels 校验
// =============================================================================

func TestValidate_NotifyChannelsDingtalkMissingWebhook(t *testing.T) {
	c := base()
	c.NotifyChannels = []NotifyChannelConfig{{Type: "dingtalk", WebhookURL: ""}}
	if err := c.Validate(); err == nil {
		t.Fatal("dingtalk 缺 webhook_url 应被拒绝")
	}
}

func TestValidate_NotifyChannelsSlackMissingWebhook(t *testing.T) {
	c := base()
	c.NotifyChannels = []NotifyChannelConfig{{Type: "slack", WebhookURL: ""}}
	if err := c.Validate(); err == nil {
		t.Fatal("slack 缺 webhook_url 应被拒绝")
	}
}

func TestValidate_NotifyChannelsEmailIncomplete(t *testing.T) {
	// 缺 SMTPHost
	c := base()
	c.NotifyChannels = []NotifyChannelConfig{{Type: "email", SMTPHost: "", SMTPPort: 25, From: "f", To: []string{"t"}}}
	if err := c.Validate(); err == nil {
		t.Fatal("email 缺 smtp_host 应被拒绝")
	}
	// SMTPPort <= 0
	c2 := base()
	c2.NotifyChannels = []NotifyChannelConfig{{Type: "email", SMTPHost: "h", SMTPPort: 0, From: "f", To: []string{"t"}}}
	if err := c2.Validate(); err == nil {
		t.Fatal("email smtp_port<=0 应被拒绝")
	}
	// 缺 From
	c3 := base()
	c3.NotifyChannels = []NotifyChannelConfig{{Type: "email", SMTPHost: "h", SMTPPort: 25, From: "", To: []string{"t"}}}
	if err := c3.Validate(); err == nil {
		t.Fatal("email 缺 from 应被拒绝")
	}
	// 缺 To
	c4 := base()
	c4.NotifyChannels = []NotifyChannelConfig{{Type: "email", SMTPHost: "h", SMTPPort: 25, From: "f", To: nil}}
	if err := c4.Validate(); err == nil {
		t.Fatal("email 缺 to 应被拒绝")
	}
}

func TestValidate_NotifyChannelsInvalidType(t *testing.T) {
	c := base()
	c.NotifyChannels = []NotifyChannelConfig{{Type: "bogus"}}
	if err := c.Validate(); err == nil {
		t.Fatal("非法 channel type 应被拒绝")
	}
}

func TestValidate_NotifyChannelsEmailOK(t *testing.T) {
	c := base()
	c.NotifyChannels = []NotifyChannelConfig{
		{Type: "email", SMTPHost: "smtp.example.com", SMTPPort: 25, From: "ops@example.com", To: []string{"ops1@example.com"}},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("email 合法配置应通过: %v", err)
	}
}

func TestValidate_NotifyChannelsSlackOK(t *testing.T) {
	c := base()
	c.NotifyChannels = []NotifyChannelConfig{
		{Type: "slack", WebhookURL: "https://hooks.slack.com/services/xxx"},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("slack 合法配置应通过: %v", err)
	}
}

func TestValidate_NotifyChannelsFeishuOK(t *testing.T) {
	c := base()
	c.NotifyChannels = []NotifyChannelConfig{
		{Type: "feishu", WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/xxx", Secret: "s"},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("feishu 合法配置应通过: %v", err)
	}
}

func TestValidate_NotifyChannelsWechatOK(t *testing.T) {
	c := base()
	c.NotifyChannels = []NotifyChannelConfig{
		{Type: "wechat", WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("wechat 合法配置应通过: %v", err)
	}
}

// =============================================================================
// Validate ProvisionCIDRWhitelist 校验
// =============================================================================

func TestValidate_ProvisionCIDRWhitelistInvalid(t *testing.T) {
	c := base()
	c.ProvisionCIDRWhitelist = "not-a-cidr"
	if err := c.Validate(); err == nil {
		t.Fatal("非法 provision-cidr-whitelist 应被拒绝")
	}
}

func TestValidate_ProvisionCIDRWhitelistOK(t *testing.T) {
	c := base()
	c.ProvisionCIDRWhitelist = "10.30.0.0/24, 10.31.0.0/24"
	if err := c.Validate(); err != nil {
		t.Fatalf("合法 provision-cidr-whitelist 应通过: %v", err)
	}
}

func TestValidate_ProvisionCIDRWhitelistEmptyItem(t *testing.T) {
	c := base()
	c.ProvisionCIDRWhitelist = "10.30.0.0/24,, 10.31.0.0/24"
	if err := c.Validate(); err != nil {
		t.Fatalf("含空项的 provision-cidr-whitelist 应通过: %v", err)
	}
}

// =============================================================================
// Validate LogPush 校验
// =============================================================================

func TestValidate_LogPushEnabledMissingFiles(t *testing.T) {
	c := base()
	c.LogPushEnabled = true
	c.LogPushFiles = nil
	c.LogPushEndpoint = "http://loki:3100"
	c.LogPushBackend = "loki"
	if err := c.Validate(); err == nil {
		t.Fatal("log-push-enabled 但 files 为空应被拒绝")
	}
}

func TestValidate_LogPushEnabledMissingEndpoint(t *testing.T) {
	c := base()
	c.LogPushEnabled = true
	c.LogPushFiles = []string{"/var/log/syslog"}
	c.LogPushEndpoint = ""
	c.LogPushBackend = "loki"
	if err := c.Validate(); err == nil {
		t.Fatal("log-push-enabled 但 endpoint 为空应被拒绝")
	}
}

func TestValidate_LogPushEnabledInvalidBackend(t *testing.T) {
	c := base()
	c.LogPushEnabled = true
	c.LogPushFiles = []string{"/var/log/syslog"}
	c.LogPushEndpoint = "http://loki:3100"
	c.LogPushBackend = "bogus"
	if err := c.Validate(); err == nil {
		t.Fatal("log-push-enabled 但 backend 非法应被拒绝")
	}
}

func TestValidate_LogPushOK(t *testing.T) {
	c := base()
	c.LogPushEnabled = true
	c.LogPushFiles = []string{"/var/log/syslog"}
	c.LogPushEndpoint = "http://loki:3100"
	c.LogPushBackend = "loki"
	if err := c.Validate(); err != nil {
		t.Fatalf("log-push 合法配置应通过: %v", err)
	}
	// es 后端也应通过
	c2 := base()
	c2.LogPushEnabled = true
	c2.LogPushFiles = []string{"/var/log/syslog"}
	c2.LogPushEndpoint = "http://es:9200"
	c2.LogPushBackend = "es"
	if err := c2.Validate(); err != nil {
		t.Fatalf("log-push es 后端应通过: %v", err)
	}
}

func TestValidate_LogPushDisabled(t *testing.T) {
	c := base()
	c.LogPushEnabled = false
	// 即使其他字段非法，disabled 时也应通过
	c.LogPushBackend = "bogus"
	if err := c.Validate(); err != nil {
		t.Fatalf("log-push disabled 应通过: %v", err)
	}
}

// =============================================================================
// Load 函数测试：通过重置 flag.CommandLine 和 os.Args 来测试 Load
// =============================================================================

// loadForTest 在隔离的 flag 环境中执行 Load，返回配置。
// 调用方负责在调用前设置需要的环境变量。
func loadForTest(args ...string) *Config {
	oldArgs := os.Args
	oldFlag := flag.CommandLine
	os.Args = append([]string{"opsmesh"}, args...)
	flag.CommandLine = flag.NewFlagSet("opsmesh", flag.ContinueOnError)
	defer func() {
		os.Args = oldArgs
		flag.CommandLine = oldFlag
	}()
	return Load()
}

// clearOpsmeshEnv 清除所有 OPSMESH_ 前缀的环境变量，返回恢复函数。
func clearOpsmeshEnv() func() {
	saved := map[string]string{}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "OPSMESH_") {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) == 2 {
				saved[parts[0]] = parts[1]
				os.Unsetenv(parts[0])
			}
		}
	}
	return func() {
		for k, v := range saved {
			os.Setenv(k, v)
		}
	}
}

func TestLoad_Defaults(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest()
	if cfg.Mode != "controlplane" {
		t.Fatalf("Mode = %q, want controlplane", cfg.Mode)
	}
	if cfg.HTTPPort != 8080 || cfg.GRPCPort != 9090 || cfg.MetricsPort != 9091 {
		t.Fatalf("ports = %d/%d/%d, want 8080/9090/9091", cfg.HTTPPort, cfg.GRPCPort, cfg.MetricsPort)
	}
	if cfg.Store != "memory" {
		t.Fatalf("Store = %q, want memory", cfg.Store)
	}
	if cfg.TaskTimeout != 120*time.Second {
		t.Fatalf("TaskTimeout = %v, want 120s", cfg.TaskTimeout)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Fatalf("ShutdownTimeout = %v, want 15s", cfg.ShutdownTimeout)
	}
	if cfg.WorkerConcurrency != 4 {
		t.Fatalf("WorkerConcurrency = %d, want 4", cfg.WorkerConcurrency)
	}
	if cfg.TaskLeaseSec != 300 || cfg.Replicas != 1 || cfg.TaskMaxRetries != 3 {
		t.Fatalf("TaskLeaseSec/Replicas/TaskMaxRetries = %d/%d/%d", cfg.TaskLeaseSec, cfg.Replicas, cfg.TaskMaxRetries)
	}
	if cfg.LogBackend != "memory" || cfg.LogStore != "memory" {
		t.Fatalf("LogBackend/LogStore = %q/%q", cfg.LogBackend, cfg.LogStore)
	}
	if cfg.LBStrategy != "failover" {
		t.Fatalf("LBStrategy = %q, want failover", cfg.LBStrategy)
	}
	if cfg.EventBus != "noop" {
		t.Fatalf("EventBus = %q, want noop", cfg.EventBus)
	}
	if cfg.DataDir != "./data" {
		t.Fatalf("DataDir = %q, want ./data", cfg.DataDir)
	}
	if cfg.LeaderTTLSec != 15 || cfg.LeaderTickSec != 5 || cfg.ArchiveAgeMin != 1440 {
		t.Fatalf("leader/archive = %d/%d/%d", cfg.LeaderTTLSec, cfg.LeaderTickSec, cfg.ArchiveAgeMin)
	}
	if cfg.NotifyDedupTTLMin != 5 || cfg.NotifyRetryMaxAttempts != 3 {
		t.Fatalf("notify dedup/retry = %d/%d", cfg.NotifyDedupTTLMin, cfg.NotifyRetryMaxAttempts)
	}
	if cfg.NotifyRetryInterval != 5*time.Second || cfg.NotifyRetryBackoff != 2.0 {
		t.Fatalf("notify interval/backoff = %v/%v", cfg.NotifyRetryInterval, cfg.NotifyRetryBackoff)
	}
	if cfg.CBFailureThreshold != 5 || cfg.CBHalfOpenMaxCalls != 1 {
		t.Fatalf("CB thresholds = %d/%d", cfg.CBFailureThreshold, cfg.CBHalfOpenMaxCalls)
	}
	if cfg.CBRecoveryTimeout != 30*time.Second {
		t.Fatalf("CBRecoveryTimeout = %v, want 30s", cfg.CBRecoveryTimeout)
	}
	if cfg.AnomalyWindowSize != 100 || cfg.AnomalyThreshold != 3.0 {
		t.Fatalf("anomaly = %d/%v", cfg.AnomalyWindowSize, cfg.AnomalyThreshold)
	}
	if cfg.LogPushBackend != "loki" {
		t.Fatalf("LogPushBackend = %q, want loki", cfg.LogPushBackend)
	}
	if cfg.ProvisionSSHUser != "root" {
		t.Fatalf("ProvisionSSHUser = %q, want root", cfg.ProvisionSSHUser)
	}
	if cfg.VaultMount != "secret" {
		t.Fatalf("VaultMount = %q, want secret", cfg.VaultMount)
	}
	if cfg.SchemaPrefix != "opsmesh_tenant_" {
		t.Fatalf("SchemaPrefix = %q, want opsmesh_tenant_", cfg.SchemaPrefix)
	}
	if !cfg.PublicRegister {
		t.Fatal("PublicRegister default should be true")
	}
	if cfg.AllowPublicRegister {
		t.Fatal("AllowPublicRegister default should be false")
	}
	if cfg.MaxProcs != 256 || cfg.MaxFiles != 4096 {
		t.Fatalf("MaxProcs/MaxFiles = %d/%d", cfg.MaxProcs, cfg.MaxFiles)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	os.Setenv("OPSMESH_MODE", "agent")
	os.Setenv("OPSMESH_HTTP_PORT", "9000")
	os.Setenv("OPSMESH_GRPC_PORT", "9091")
	os.Setenv("OPSMESH_METRICS_PORT", "9092")
	os.Setenv("OPSMESH_STORE", "mysql")
	os.Setenv("OPSMESH_MYSQL_DSN", "u:p@tcp(db:3306)/db")
	os.Setenv("OPSMESH_REDIS_ADDR", "redis:6379")
	os.Setenv("OPSMESH_TASK_TIMEOUT", "60s")
	os.Setenv("OPSMESH_SHUTDOWN_TIMEOUT", "10s")
	os.Setenv("OPSMESH_WORKER_CONCURRENCY", "8")
	os.Setenv("OPSMESH_REPLICAS", "3")
	os.Setenv("OPSMESH_PRODUCTION", "true")
	os.Setenv("OPSMESH_TLS_CERT", "cert.pem")
	os.Setenv("OPSMESH_TLS_KEY", "key.pem")
	os.Setenv("OPSMESH_JWT_SECRET", "0123456789abcdef0123456789abcdef")
	os.Setenv("OPSMESH_ENCRYPTION_KEY", "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=")
	os.Setenv("OPSMESH_LOG_BACKEND", "loki")
	os.Setenv("OPSMESH_LOKI_ENDPOINT", "http://loki:3100")
	os.Setenv("OPSMESH_LB_STRATEGY", "round-robin")
	os.Setenv("OPSMESH_CONTROLPLANE_ENDPOINTS", "http://cp1:8080,http://cp2:8080")
	os.Setenv("OPSMESH_FEDERATION_PEERS", "http://peer1:8080,http://peer2:8080")
	os.Setenv("OPSMESH_FEDERATION_SECRET", "shared-secret")
	os.Setenv("OPSMESH_METRICS_ALLOW_CIDR", "10.0.0.0/8")
	os.Setenv("OPSMESH_SESSION_STORE", "redis://localhost:6379")
	os.Setenv("OPSMESH_MAX_MEMORY_MB", "512")
	os.Setenv("OPSMESH_NOTIFY_RETRY_BACKOFF", "1.5")
	os.Setenv("OPSMESH_ANOMALY_THRESHOLD", "2.5")
	os.Setenv("OPSMESH_DEVICE_FP_DEADLINE", "2026-09-01T00:00:00Z")
	os.Setenv("OPSMESH_LOG_PUSH_FILES", "/var/log/syslog,/var/log/app.log")
	os.Setenv("OPSMESH_DEMO", "false")

	cfg := loadForTest()
	if cfg.Mode != "agent" {
		t.Fatalf("Mode = %q, want agent", cfg.Mode)
	}
	if cfg.HTTPPort != 9000 || cfg.GRPCPort != 9091 || cfg.MetricsPort != 9092 {
		t.Fatalf("ports = %d/%d/%d", cfg.HTTPPort, cfg.GRPCPort, cfg.MetricsPort)
	}
	if cfg.Store != "mysql" || cfg.MySQLDSN != "u:p@tcp(db:3306)/db" {
		t.Fatalf("Store/DSN = %q/%q", cfg.Store, cfg.MySQLDSN)
	}
	if cfg.RedisAddr != "redis:6379" {
		t.Fatalf("RedisAddr = %q", cfg.RedisAddr)
	}
	if cfg.TaskTimeout != 60*time.Second || cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("timeouts = %v/%v", cfg.TaskTimeout, cfg.ShutdownTimeout)
	}
	if cfg.WorkerConcurrency != 8 || cfg.Replicas != 3 {
		t.Fatalf("worker/replicas = %d/%d", cfg.WorkerConcurrency, cfg.Replicas)
	}
	if !cfg.Production || cfg.TLSCert != "cert.pem" {
		t.Fatalf("Production/TLSCert = %v/%q", cfg.Production, cfg.TLSCert)
	}
	if cfg.LogBackend != "loki" || cfg.LokiEndpoint != "http://loki:3100" {
		t.Fatalf("LogBackend/Loki = %q/%q", cfg.LogBackend, cfg.LokiEndpoint)
	}
	if cfg.LBStrategy != "round-robin" {
		t.Fatalf("LBStrategy = %q", cfg.LBStrategy)
	}
	if cfg.ControlplaneEndpoints != "http://cp1:8080,http://cp2:8080" {
		t.Fatalf("ControlplaneEndpoints = %q", cfg.ControlplaneEndpoints)
	}
	if cfg.ControlAddrs != "http://cp1:8080,http://cp2:8080" {
		t.Fatalf("ControlAddrs = %q", cfg.ControlAddrs)
	}
	if len(cfg.FederationPeers) != 2 || cfg.FederationSecret != "shared-secret" {
		t.Fatalf("FederationPeers/Secret = %v/%q", cfg.FederationPeers, cfg.FederationSecret)
	}
	if cfg.MetricsAllowCIDR != "10.0.0.0/8" {
		t.Fatalf("MetricsAllowCIDR = %q", cfg.MetricsAllowCIDR)
	}
	if cfg.SessionStore != "redis://localhost:6379" {
		t.Fatalf("SessionStore = %q", cfg.SessionStore)
	}
	if cfg.MaxMemoryMB != 512 {
		t.Fatalf("MaxMemoryMB = %d, want 512", cfg.MaxMemoryMB)
	}
	if cfg.NotifyRetryBackoff != 1.5 || cfg.AnomalyThreshold != 2.5 {
		t.Fatalf("backoff/threshold = %v/%v", cfg.NotifyRetryBackoff, cfg.AnomalyThreshold)
	}
	if cfg.DeviceFPDeadline.IsZero() {
		t.Fatal("DeviceFPDeadline should be set")
	}
	if len(cfg.LogPushFiles) != 2 {
		t.Fatalf("LogPushFiles len = %d, want 2", len(cfg.LogPushFiles))
	}
	// Production 模式应自动开启 require-auth / grpc-require-signature / cookie-secure
	if !cfg.RequireAuth || !cfg.GRPCRequireSignature || !cfg.CookieSecure {
		t.Fatalf("Production auto-enables: RequireAuth=%v GRPCRequireSignature=%v CookieSecure=%v", cfg.RequireAuth, cfg.GRPCRequireSignature, cfg.CookieSecure)
	}
}

func TestLoad_ExplicitFlagPriority(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	os.Setenv("OPSMESH_MODE", "agent")
	os.Setenv("OPSMESH_HTTP_PORT", "9000")
	cfg := loadForTest("--mode=controlplane", "--http-port=8080")
	if cfg.Mode != "controlplane" {
		t.Fatalf("Mode = %q, want controlplane (explicit flag)", cfg.Mode)
	}
	if cfg.HTTPPort != 8080 {
		t.Fatalf("HTTPPort = %d, want 8080 (explicit flag)", cfg.HTTPPort)
	}
}

func TestLoad_LogStoreAlias(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--log-store=loki", "--loki-endpoint=http://loki:3100")
	if cfg.LogBackend != "loki" {
		t.Fatalf("LogBackend = %q, want loki (log-store alias)", cfg.LogBackend)
	}
	if cfg.LogStore != "loki" {
		t.Fatalf("LogStore = %q, want loki", cfg.LogStore)
	}
}

func TestLoad_ControlplaneEndpointsAlias(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--controlplane-endpoints=http://cp1:8080,http://cp2:8080")
	if cfg.ControlplaneEndpoints != "http://cp1:8080,http://cp2:8080" {
		t.Fatalf("ControlplaneEndpoints = %q", cfg.ControlplaneEndpoints)
	}
	if cfg.ControlAddrs != "http://cp1:8080,http://cp2:8080" {
		t.Fatalf("ControlAddrs = %q (should be synced)", cfg.ControlAddrs)
	}
}

func TestLoad_LBStrategyInvalid(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--lb-strategy=bogus")
	if cfg.LBStrategy != "failover" {
		t.Fatalf("LBStrategy = %q, want failover (fallback)", cfg.LBStrategy)
	}
	cfg2 := loadForTest("--lb-strategy=round-robin")
	if cfg2.LBStrategy != "round-robin" {
		t.Fatalf("LBStrategy = %q, want round-robin", cfg2.LBStrategy)
	}
}

func TestLoad_DemoDefaults(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--demo")
	if !cfg.Demo {
		t.Fatal("Demo should be true")
	}
	if !cfg.PublicRegister {
		t.Fatal("Demo should auto-enable public-register")
	}
	if cfg.GRPCRequireSignature {
		t.Fatal("Demo should disable grpc-require-signature")
	}
}

func TestLoad_ProductionPublicRegisterFalse(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--production", "--tls-cert=tls.crt",
		"--jwt-secret=0123456789abcdef0123456789abcdef",
		"--encryption-key=AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=",
		"--store=mysql", "--mysql-dsn=u:p@tcp(db:3306)/db")
	if cfg.PublicRegister {
		t.Fatal("Production should default public-register=false")
	}
}

func TestLoad_ProductionExplicitPublicRegister(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	// 显式 --public-register=true 时生产模式应尊重用户设置
	cfg := loadForTest("--production", "--tls-cert=tls.crt",
		"--jwt-secret=0123456789abcdef0123456789abcdef",
		"--encryption-key=AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=",
		"--store=mysql", "--mysql-dsn=u:p@tcp(db:3306)/db",
		"--public-register=true")
	if !cfg.PublicRegister {
		t.Fatal("Explicit --public-register=true should be respected")
	}
}

func TestLoad_DemoExplicitPublicRegisterFalse(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	// demo 模式下显式 --public-register=false 应被尊重
	cfg := loadForTest("--demo", "--public-register=false")
	if cfg.PublicRegister {
		t.Fatal("Explicit --public-register=false should be respected in demo")
	}
}

func TestLoad_ProductionExplicitRequireAuthFalse(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--production", "--tls-cert=tls.crt",
		"--jwt-secret=0123456789abcdef0123456789abcdef",
		"--encryption-key=AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=",
		"--store=mysql", "--mysql-dsn=u:p@tcp(db:3306)/db",
		"--require-auth=false")
	if cfg.RequireAuth {
		t.Fatal("Explicit --require-auth=false should be respected in production")
	}
}

func TestLoad_ProductionExplicitGrpcSignatureFalse(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--production", "--tls-cert=tls.crt",
		"--jwt-secret=0123456789abcdef0123456789abcdef",
		"--encryption-key=AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=",
		"--store=mysql", "--mysql-dsn=u:p@tcp(db:3306)/db",
		"--grpc-require-signature=false")
	if cfg.GRPCRequireSignature {
		t.Fatal("Explicit --grpc-require-signature=false should be respected in production")
	}
}

func TestLoad_ProductionExplicitCookieSecureFalse(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--production", "--tls-cert=tls.crt",
		"--jwt-secret=0123456789abcdef0123456789abcdef",
		"--encryption-key=AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=",
		"--store=mysql", "--mysql-dsn=u:p@tcp(db:3306)/db",
		"--cookie-secure=false")
	if cfg.CookieSecure {
		t.Fatal("Explicit --cookie-secure=false should be respected in production")
	}
}

func TestLoad_AllowPublicRegisterWarning(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	cfg := loadForTest("--allow-public-register=true")
	if !cfg.AllowPublicRegister {
		t.Fatal("AllowPublicRegister should be true")
	}
}

func TestLoad_NotifyChannelsConfig(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	path := filepath.Join(t.TempDir(), "channels.json")
	content := `{"channels":[{"type":"dingtalk","webhook_url":"http://x"}]}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	cfg := loadForTest("--notify-channels-config=" + path)
	if len(cfg.NotifyChannels) != 1 {
		t.Fatalf("NotifyChannels len = %d, want 1", len(cfg.NotifyChannels))
	}
	if cfg.NotifyChannels[0].Type != "dingtalk" {
		t.Fatalf("NotifyChannels[0].Type = %q", cfg.NotifyChannels[0].Type)
	}
}

func TestLoad_NotifyChannelsConfigBadFile(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	// 不存在的文件：Load 不 fail-fast，仅打印告警，NotifyChannels 为空
	cfg := loadForTest("--notify-channels-config=/nonexistent/path/channels.json")
	if len(cfg.NotifyChannels) != 0 {
		t.Fatalf("NotifyChannels len = %d, want 0 (bad file)", len(cfg.NotifyChannels))
	}
}

func TestLoad_ProductionMemoryStoreWarning(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()
	// 生产模式 + memory store：Load 不 fail-fast（Validate 才 fail-fast），仅告警
	cfg := loadForTest("--production", "--tls-cert=tls.crt",
		"--jwt-secret=0123456789abcdef0123456789abcdef",
		"--encryption-key=AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=")
	if !cfg.Production {
		t.Fatal("Production should be true")
	}
	if cfg.Store != "memory" {
		t.Fatalf("Store = %q, want memory", cfg.Store)
	}
}

// =============================================================================
// H2/H3 配套：AllowStubStores 边界矩阵（生产 SQL 桩存储须显式放行）
// =============================================================================

// prodReadyDefaults 为 c 补齐生产模式前置硬性要求（TLS / JWT / EncryptionKey），
// 使 Validate 能穿透前序校验到达目标分支（如 AllowStubStores 校验）。
// 仅对传入对象就地修改；调用方按需覆盖字段。
func prodReadyDefaults(c *Config) {
	c.Production = true
	c.TLSCert = "tls.crt"
	c.JWTSecret = "0123456789abcdef0123456789abcdef"                 // 32 字节
	c.EncryptionKey = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=" // 32 字节 base64
}

// TestValidate_AllowStubStoresMatrix 覆盖 Production × Store × AllowStubStores 组合边界：
// 现状：P1-P6 全部 15 个领域已实现 MySQL 持久化，stubStoreDomains 收敛为空字符串，
// Validate 中的拒绝启动分支被跳过（无桩领域则无须放行门槛），故生产 + mysql 后端
// 即使 AllowStubStores=false 也通过；非生产与 memory 后端不受影响。
// 保留矩阵结构向后兼容：未来若新增桩领域（stubStoreDomains 非空），拒绝逻辑自动恢复。
func TestValidate_AllowStubStoresMatrix(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(c *Config)
		wantErr     bool
		errContains string // 非空时断言错误消息包含该子串（strings.Contains 保持健壮）
	}{
		{
			name: "prod_mysql_without_allow_stub_stores_passes",
			mutate: func(c *Config) {
				prodReadyDefaults(c)
				c.Store = "mysql"
				c.MySQLDSN = "u:p@tcp(db:3306)/ops_device"
				c.AllowStubStores = false
			},
			wantErr: false, // P1-P6 全部持久化，无桩领域，无须放行门槛
		},
		{
			name: "prod_mysql_with_allow_stub_stores_passes",
			mutate: func(c *Config) {
				prodReadyDefaults(c)
				c.Store = "mysql"
				c.MySQLDSN = "u:p@tcp(db:3306)/ops_device"
				c.AllowStubStores = true
			},
			wantErr: false,
		},
		{
			name: "dev_mysql_without_allow_stub_stores_passes",
			mutate: func(c *Config) {
				c.Production = false
				c.Store = "mysql"
				c.MySQLDSN = "u:p@tcp(db:3306)/ops_device"
			},
			wantErr: false,
		},
		{
			name: "prod_memory_ignores_allow_stub_stores",
			mutate: func(c *Config) {
				prodReadyDefaults(c)
				c.Store = "memory"
				c.AllowStubStores = false
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base()
			tt.mutate(c)
			err := c.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("应被拒绝但通过了: %+v", c)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("应通过却被拒绝: %v", err)
			}
			if tt.wantErr && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Fatalf("错误消息应包含 %q, got %q", tt.errContains, err.Error())
			}
		})
	}
}

// =============================================================================
// 生产模式 TLS 强制（等保三级）：错误消息与 agent 模式适用性
// =============================================================================

// TestValidate_ProductionTLSRequiredMessage 验证 Production=true 且 TLSCert=""
// 时 Validate 直接拒绝（不再是告警），并断言错误消息指向 --tls-cert；
// agent 模式同样适用；非生产模式保持开发降级放行。
func TestValidate_ProductionTLSRequiredMessage(t *testing.T) {
	// 控制面：production + 无 TLS → 拒绝且消息含 --tls-cert
	c := base()
	c.Production = true
	err := c.Validate()
	if err == nil {
		t.Fatal("production + 无 TLS 应被拒绝")
	}
	if !strings.Contains(err.Error(), "--tls-cert") {
		t.Fatalf("错误消息应提示 --tls-cert, got %q", err.Error())
	}

	// agent 模式：production 同样强制持证
	a := base()
	a.Mode = "agent"
	a.Production = true
	a.TLSCert = ""
	if err := a.Validate(); err == nil {
		t.Fatal("agent 模式 production + 无 TLS 应被拒绝")
	} else if !strings.Contains(err.Error(), "--tls-cert") {
		t.Fatalf("agent 错误消息应提示 --tls-cert, got %q", err.Error())
	}

	// 非 production + 无 TLS → 开发降级放行
	d := base()
	d.Production = false
	d.TLSCert = ""
	if err := d.Validate(); err != nil {
		t.Fatalf("非 production + 无 TLS 应通过: %v", err)
	}
}

// TestValidate_ProductionRequiresEncryptionKey 覆盖生产模式 kubeconfig 加密密钥缺失分支：
// Production=true 且 EncryptionKey="" 必须拒绝并提示 --encryption-key。
func TestValidate_ProductionRequiresEncryptionKey(t *testing.T) {
	c := base()
	prodReadyDefaults(c)
	c.EncryptionKey = "" // 聚焦本分支
	err := c.Validate()
	if err == nil {
		t.Fatal("production + 空 encryption-key 应被拒绝")
	}
	if !strings.Contains(err.Error(), "--encryption-key") {
		t.Fatalf("错误消息应提示 --encryption-key, got %q", err.Error())
	}

	// 对照：非生产 + 空 encryption-key 放行（明文存储仅限开发/demo）
	d := base()
	d.Production = false
	d.EncryptionKey = ""
	if err := d.Validate(); err != nil {
		t.Fatalf("非 production + 空 encryption-key 应通过: %v", err)
	}
}

// =============================================================================
// Validate flag 组合矩阵：Production × TLS × MultiSchema × Federation × RateLimit × AllowStubStores
// =============================================================================

// TestValidate_FlagCombinationMatrix 以表驱动方式覆盖关键 flag 交叉组合。
// 每个用例从 base() 出发按 mutate 定制；wantErr 用例以 errContains 断言错误消息，
// 确保拒绝发生在预期的校验分支而非更早的其他分支。
func TestValidate_FlagCombinationMatrix(t *testing.T) {
	const (
		validJWT = "0123456789abcdef0123456789abcdef"             // 32 字节 HS256 密钥
		validEnc = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=" // 32 字节 AES-256 base64
		validDSN = "u:p@tcp(db:3306)/ops_device"
	)
	tests := []struct {
		name        string
		mutate      func(c *Config)
		wantErr     bool
		errContains string
	}{
		{
			name:    "dev_all_flags_off_passes",
			mutate:  func(c *Config) {},
			wantErr: false,
		},
		{
			// 大满贯：生产全量合规组合一次性通过（MultiSchema/Federation/RateLimit/Stub 全开）。
			name: "prod_full_stack_all_enabled_passes",
			mutate: func(c *Config) {
				prodReadyDefaults(c)
				c.Store = "mysql"
				c.MySQLDSN = validDSN
				c.AllowStubStores = true
				c.MultiSchema = true
				c.SchemaPrefix = "opsmesh_tenant_"
				c.FederationPeers = []string{"http://peer1:8080", "http://peer2:8080"}
				c.FederationSecret = "shared-secret"
				c.CBRateLimitPerSec = 100
			},
			wantErr: false,
		},
		{
			// Production×TLS 交叉：缺证书在 TLS 强制分支被拒（早于 multi-schema/stub）。
			name: "prod_without_tls_rejected_at_tls_gate",
			mutate: func(c *Config) {
				prodReadyDefaults(c)
				c.TLSCert = ""
				c.MultiSchema = true // 即使 multi-schema 也不该到达该校验
			},
			wantErr:     true,
			errContains: "--tls-cert",
		},
		{
			// MultiSchema×Store 交叉：multi-schema 仅支持 mysql，memory 后端直接拒绝。
			name: "multischema_with_memory_store_rejected",
			mutate: func(c *Config) {
				c.MultiSchema = true
				c.Store = "memory"
			},
			wantErr:     true,
			errContains: "--multi-schema",
		},
		{
			// MultiSchema×Production×AllowStubStores 三重交叉：
			// 现状：P1-P6 全部持久化，stubStoreDomains 为空，桩存储拒绝分支被跳过，
			// multi-schema + 生产 + mysql 即使 AllowStubStores=false 也通过。
			// 未来若新增桩领域（stubStoreDomains 非空），拒绝逻辑自动恢复。
			name: "prod_multischema_mysql_without_stubs_passes",
			mutate: func(c *Config) {
				prodReadyDefaults(c)
				c.Store = "mysql"
				c.MySQLDSN = validDSN
				c.MultiSchema = true
				c.AllowStubStores = false
			},
			wantErr: false,
		},
		{
			// Federation×Production 交叉：生产 + 联邦 peer + 共享密钥合法放行。
			name: "prod_federation_with_secret_passes",
			mutate: func(c *Config) {
				prodReadyDefaults(c)
				c.FederationPeers = []string{"http://peer1:8080"}
				c.FederationSecret = "shared-secret"
			},
			wantErr: false,
		},
		{
			// Federation 密钥缺失：无论是否生产均 fail-fast（防伪造租户身份头）。
			name: "federation_peers_missing_secret_rejected",
			mutate: func(c *Config) {
				c.FederationPeers = []string{"http://peer1:8080"}
				c.FederationSecret = ""
			},
			wantErr:     true,
			errContains: "federation-secret",
		},
		{
			// RateLimit 单开：CBRateLimitPerSec>0 不影响 Validate 通过性（运行期才生效）。
			name: "rate_limit_only_passes",
			mutate: func(c *Config) {
				c.CBRateLimitPerSec = 200
			},
			wantErr: false,
		},
		{
			// RateLimit×Federation×MultiSchema 组合（非生产）：全部叠加仍放行。
			name: "dev_ratelimit_federation_multischema_passes",
			mutate: func(c *Config) {
				c.CBRateLimitPerSec = 50
				c.Store = "mysql"
				c.MySQLDSN = validDSN
				c.MultiSchema = true
				c.FederationPeers = []string{"http://peer1:8080"}
				c.FederationSecret = "s"
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base()
			tt.mutate(c)
			err := c.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("应被拒绝但通过了: %+v", c)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("错误消息应包含 %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("应通过却被拒绝: %v", err)
			}
		})
	}
}

// =============================================================================
// DefaultAgentShellWhitelist 导出函数
// =============================================================================

// TestDefaultAgentShellWhitelistExported 覆盖导出包装函数：
// 返回值必须与内部常量一致，且仅含只读诊断命令（不含可写系统状态的危险命令）。
func TestDefaultAgentShellWhitelistExported(t *testing.T) {
	got := DefaultAgentShellWhitelist()
	if got != defaultAgentShellWhitelist {
		t.Fatalf("DefaultAgentShellWhitelist() = %q, want %q", got, defaultAgentShellWhitelist)
	}
	for _, dangerous := range []string{"sh", "bash", "rm", "mv", "curl", "wget", "nc", "python"} {
		for _, item := range strings.Split(got, ",") {
			if strings.TrimSpace(item) == dangerous {
				t.Fatalf("白名单不应包含危险命令 %q: %q", dangerous, got)
			}
		}
	}
}

// =============================================================================
// Load 显式 flag 优先级：valInt64 / valDur / valFloat64 的 explicit 分支
// =============================================================================

// TestLoad_ExplicitFlagOverridesEnv 验证显式 flag 严格优先于环境变量，
// 覆盖 valInt64（--max-memory-mb）、valDur（--task-timeout）、valFloat64（--anomaly-threshold）
// 三个取值闭包的 explicit 分支；并以 env-only 反向对照确认 env 兜底仍然生效。
func TestLoad_ExplicitFlagOverridesEnv(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()

	// env 设为与 flag 相反的值：flag 显式设置时应完全压过 env。
	os.Setenv("OPSMESH_MAX_MEMORY_MB", "512")
	os.Setenv("OPSMESH_TASK_TIMEOUT", "60s")
	os.Setenv("OPSMESH_ANOMALY_THRESHOLD", "3.5")

	cfg := loadForTest(
		"--max-memory-mb=1024",
		"--task-timeout=90s",
		"--anomaly-threshold=2.5",
	)
	if cfg.MaxMemoryMB != 1024 {
		t.Fatalf("MaxMemoryMB = %d, want 1024 (explicit flag overrides env)", cfg.MaxMemoryMB)
	}
	if cfg.TaskTimeout != 90*time.Second {
		t.Fatalf("TaskTimeout = %v, want 90s (explicit flag overrides env)", cfg.TaskTimeout)
	}
	if cfg.AnomalyThreshold != 2.5 {
		t.Fatalf("AnomalyThreshold = %v, want 2.5 (explicit flag overrides env)", cfg.AnomalyThreshold)
	}

	// 反向对照：不传 flag 时 env 兜底生效。
	cfg2 := loadForTest()
	if cfg2.MaxMemoryMB != 512 || cfg2.TaskTimeout != 60*time.Second || cfg2.AnomalyThreshold != 3.5 {
		t.Fatalf("env fallback: MaxMemoryMB=%d TaskTimeout=%v AnomalyThreshold=%v, want 512/60s/3.5",
			cfg2.MaxMemoryMB, cfg2.TaskTimeout, cfg2.AnomalyThreshold)
	}
}
