// config_extra_test.go 补充 config.go 未覆盖的 Validate 分支与辅助函数。
package config

import (
	"os"
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
	c.Store = "mysql"
	c.MySQLDSN = "u:p@tcp(db:3306)/ops_device"
	c.Replicas = 3
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
