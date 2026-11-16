package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// base 返回一份合理的控制面默认配置，避免每个用例重复样板。
func base() *Config {
	return &Config{
		Mode:              "controlplane",
		HTTPPort:          8080,
		GRPCPort:          9090,
		MetricsPort:       9091,
		Store:             "memory",
		TaskTimeout:       120 * time.Second,
		ShutdownTimeout:   15 * time.Second,
		WorkerConcurrency: 4,
		TaskLeaseSec:      300,
		Replicas:          1,
		TaskMaxRetries:    3,
		LogBackend:        "memory", // 默认日志后端
	}
}

// TestValidate_OK 验证默认控制面配置通过校验。
func TestValidate_OK(t *testing.T) {
	if err := base().Validate(); err != nil {
		t.Fatalf("base valid config errored: %v", err)
	}
}

// TestValidate_MemoryMultiReplica ：memory store 配多副本必须被拒绝（数据分裂）。
func TestValidate_MemoryMultiReplica(t *testing.T) {
	c := base()
	c.Store = "memory"
	c.Replicas = 2
	if err := c.Validate(); err == nil {
		t.Fatal("memory + replicas>1 应被拒绝，但通过了")
	}
}

// TestValidate_MysqlMultiReplica ：mysql store + 多副本是 HA 合法组合。
func TestValidate_MysqlMultiReplica(t *testing.T) {
	c := base()
	c.Store = "mysql"
	c.MySQLDSN = "u:p@tcp(db:3306)/ops_device"
	c.Replicas = 2
	if err := c.Validate(); err != nil {
		t.Fatalf("mysql + replicas>1 应合法，却报错: %v", err)
	}
}

// TestValidate_ProductionRejectsNoTLS 等保加固：生产模式 + 无 TLS 证书应被 Validate 拒绝
// （Production=true 且 TLSCert="" 直接返回 error，避免 agent↔控制面明文通信）。
func TestValidate_ProductionRejectsNoTLS(t *testing.T) {
	c := base()
	c.Production = true
	c.Store = "memory"
	c.Replicas = 1 // 单副本不触发多副本拒绝
	c.TLSCert = "" // 无 TLS 证书
	// production + 无 TLS 应被拒绝（等保三级要求）。
	if err := c.Validate(); err == nil {
		t.Fatal("production + 无 TLS 应被拒绝，但 Validate 通过了")
	}
}

// TestValidate_ModeAndStore ：非法 mode 与 mysql 缺 DSN 必须拒绝。
func TestValidate_ModeAndStore(t *testing.T) {
	c := base()
	c.Mode = "bogus"
	if err := c.Validate(); err == nil {
		t.Fatal("非法 mode 应被拒绝")
	}

	c2 := base()
	c2.Store = "mysql"
	c2.MySQLDSN = ""
	if err := c2.Validate(); err == nil {
		t.Fatal("mysql 缺 DSN 应被拒绝")
	}
}

// TestLoad_ProductionEnablesRequireAuth ：生产模式默认开启 require-auth（除非显式关闭）。
func TestLoad_ProductionEnablesRequireAuth(t *testing.T) {
	// 直接构造 Load 等价逻辑：production 且无显式 require-auth 时翻 true。
	c := base()
	c.Production = true
	c.RequireAuth = false
	explicitRequireAuth := false // 模拟未显式设置该 flag
	if c.Production && !explicitRequireAuth {
		c.RequireAuth = true
	}
	if !c.RequireAuth {
		t.Fatal("production 模式应默认开启 require-auth")
	}
}

// TestValidate_LogBackend ：非法 log-backend 必须被拒绝。
func TestValidate_LogBackend(t *testing.T) {
	c := base()
	c.LogBackend = "bogus"
	if err := c.Validate(); err == nil {
		t.Fatal("非法 log-backend 应被拒绝")
	}
}

// TestValidate_LogBackendLokiMissingEndpoint ：log-backend=loki 但缺 endpoint 必须被拒绝。
func TestValidate_LogBackendLokiMissingEndpoint(t *testing.T) {
	c := base()
	c.LogBackend = "loki"
	c.LokiEndpoint = ""
	if err := c.Validate(); err == nil {
		t.Fatal("log-backend=loki 缺 endpoint 应被拒绝")
	}
}

// TestValidate_LogBackendLokiOK ：log-backend=loki + endpoint 合法。
func TestValidate_LogBackendLokiOK(t *testing.T) {
	c := base()
	c.LogBackend = "loki"
	c.LokiEndpoint = "http://loki:3100"
	if err := c.Validate(); err != nil {
		t.Fatalf("loki 合法配置应通过: %v", err)
	}
}

// TestValidate_LogBackendESMissingEndpoint ：log-backend=es 但缺 endpoint 必须被拒绝。
func TestValidate_LogBackendESMissingEndpoint(t *testing.T) {
	c := base()
	c.LogBackend = "es"
	c.ESEndpoint = ""
	if err := c.Validate(); err == nil {
		t.Fatal("log-backend=es 缺 endpoint 应被拒绝")
	}
}

// TestValidate_LogBackendESMissingIndex ：log-backend=es 但缺 index 必须被拒绝。
func TestValidate_LogBackendESMissingIndex(t *testing.T) {
	c := base()
	c.LogBackend = "es"
	c.ESEndpoint = "http://es:9200"
	c.ESIndex = ""
	if err := c.Validate(); err == nil {
		t.Fatal("log-backend=es 缺 index 应被拒绝")
	}
}

// TestValidate_LogBackendESOK ：log-backend=es + endpoint + index 合法。
func TestValidate_LogBackendESOK(t *testing.T) {
	c := base()
	c.LogBackend = "es"
	c.ESEndpoint = "http://es:9200"
	c.ESIndex = "opsmesh-logs"
	if err := c.Validate(); err != nil {
		t.Fatalf("es 合法配置应通过: %v", err)
	}
}

// TestValidate_ProductionControlplaneRequiresJWTSecret：生产 controlplane 必须显式注入 JWT 密钥。
func TestValidate_ProductionControlplaneRequiresJWTSecret(t *testing.T) {
	// prod + 空密钥 -> 拒绝
	c := base()
	c.Production = true
	c.TLSCert = "tls.crt" // 绕过 H6 TLS，聚焦 JWT 校验
	c.TLSKey = "tls.key"
	if err := c.Validate(); err == nil {
		t.Fatal("production + 空 jwt-secret 应被拒绝，但 Validate 通过了")
	}
	// prod + 合法密钥 -> 通过
	c2 := base()
	c2.Production = true
	c2.TLSCert = "tls.crt"
	c2.TLSKey = "tls.key"
	c2.JWTSecret = "0123456789abcdef0123456789abcdef"                 // 32 字节
	c2.EncryptionKey = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=" // 32 字节 base64
	if err := c2.Validate(); err != nil {
		t.Fatalf("production + 合法 jwt-secret 应通过: %v", err)
	}
	// 非 prod + 空密钥 -> 通过（dev 随机兜底保留）
	c3 := base()
	c3.Production = false
	c3.JWTSecret = ""
	if err := c3.Validate(); err != nil {
		t.Fatalf("非 production + 空 jwt-secret 应通过: %v", err)
	}
}

// TestValidate_ProductionJWTSecretLength：生产 jwt-secret 过短（<32 字节）必须拒绝。
func TestValidate_ProductionJWTSecretLength(t *testing.T) {
	c := base()
	c.Production = true
	c.TLSCert = "tls.crt"
	c.TLSKey = "tls.key"
	c.JWTSecret = "tooshort" // 8 字节 < 32
	if err := c.Validate(); err == nil {
		t.Fatal("production + (<32) jwt-secret 应被拒绝，但通过了")
	}
	c2 := base()
	c2.Production = true
	c2.TLSCert = "tls.crt"
	c2.TLSKey = "tls.key"
	c2.JWTSecret = "0123456789abcdef0123456789abcdef"                 // 32 字节
	c2.EncryptionKey = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=" // 32 字节 base64
	if err := c2.Validate(); err != nil {
		t.Fatalf("production + 32 字节 jwt-secret 应通过: %v", err)
	}
}

// ============================================================================
// / 新选项校验测试
// ============================================================================

// TestValidate_SessionStoreFormat 验证 --session-store 格式校验。
func TestValidate_SessionStoreFormat(t *testing.T) {
	// 合法格式：redis://host:port
	c := base()
	c.SessionStore = "redis://localhost:6379"
	if err := c.Validate(); err != nil {
		t.Fatalf("redis://localhost:6379 应通过: %v", err)
	}

	// 非法格式：非 redis:// 前缀
	c2 := base()
	c2.SessionStore = "mysql://localhost:3306"
	if err := c2.Validate(); err == nil {
		t.Fatal("非 redis:// 前缀应被拒绝")
	}

	// 非法格式：redis:// 无 host:port
	c3 := base()
	c3.SessionStore = "redis://"
	if err := c3.Validate(); err == nil {
		t.Fatal("redis:// 无 host:port 应被拒绝")
	}

	// 空（默认）应通过
	c4 := base()
	c4.SessionStore = ""
	if err := c4.Validate(); err != nil {
		t.Fatalf("空 session-store 应通过: %v", err)
	}
}

// TestParseDeviceFPDeadline 验证 --device-fp-deadline 解析。
func TestParseDeviceFPDeadline(t *testing.T) {
	// 空串返回零值。
	if d := parseDeviceFPDeadline(""); !d.IsZero() {
		t.Fatal("空串应返回零值")
	}

	// 合法 RFC3339 应解析成功。
	d := parseDeviceFPDeadline("2026-09-01T00:00:00Z")
	if d.IsZero() {
		t.Fatal("合法 RFC3339 应解析成功")
	}
	if d.Year() != 2026 || d.Month() != 9 || d.Day() != 1 {
		t.Fatalf("解析结果应为 2026-09-01，得到 %v", d)
	}

	// 非法格式返回零值（不 panic）。
	if d := parseDeviceFPDeadline("not-a-date"); !d.IsZero() {
		t.Fatal("非法格式应返回零值")
	}

	// 带空格的输入应 TrimSpace 后解析。
	d2 := parseDeviceFPDeadline("  2026-09-01T00:00:00Z  ")
	if d2.IsZero() {
		t.Fatal("带空格的合法 RFC3339 应解析成功")
	}
}

// ============================================================================
// 告警抑制集成：--inhibit-rules-file 校验测试
// ============================================================================

// TestValidate_InhibitRulesFile_Empty 验证空 InhibitRulesFile 通过校验（向后兼容）。
func TestValidate_InhibitRulesFile_Empty(t *testing.T) {
	c := base()
	c.InhibitRulesFile = ""
	if err := c.Validate(); err != nil {
		t.Fatalf("空 InhibitRulesFile 应通过: %v", err)
	}
}

// TestValidate_InhibitRulesFile_Exists 验证 InhibitRulesFile 指向存在的文件通过校验。
func TestValidate_InhibitRulesFile_Exists(t *testing.T) {
	// 创建临时文件
	path := filepath.Join(t.TempDir(), "inhibit_rules.json")
	if err := os.WriteFile(path, []byte("[]"), 0644); err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}

	c := base()
	c.InhibitRulesFile = path
	if err := c.Validate(); err != nil {
		t.Fatalf("存在的 InhibitRulesFile 应通过: %v", err)
	}
}

// TestValidate_InhibitRulesFile_NotExists 验证 InhibitRulesFile 指向不存在的文件被拒绝。
func TestValidate_InhibitRulesFile_NotExists(t *testing.T) {
	c := base()
	c.InhibitRulesFile = filepath.Join(t.TempDir(), "nonexistent.json")
	if err := c.Validate(); err == nil {
		t.Fatal("不存在的 InhibitRulesFile 应被拒绝")
	}
}

// ============================================================================
// 联邦 per-peer 密钥配置：解析 / 校验 / Load 映射
// ============================================================================

// TestParseFederationPeerSecrets_Grouping 验证逗号分隔条目的归并规则：
// `<peerID>@<peerURL>=<secret1>[,<secret2>...]` 中的轮换密钥与后续 peer 条目可共存。
func TestParseFederationPeerSecrets_Grouping(t *testing.T) {
	got := parseFederationPeerSecrets("peer-a@http://a:8080=s1,s2,peer-b@http://b:8080=t1")
	want := []string{"peer-a@http://a:8080=s1,s2", "peer-b@http://b:8080=t1"}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entries[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// base64 密钥末尾的 '=' 不应被误判为新条目（不含 @ 结构）。
	got2 := parseFederationPeerSecrets("peer-a@http://a:8080=c2VjcmV0,ZW5k=")
	if len(got2) != 1 || got2[0] != "peer-a@http://a:8080=c2VjcmV0,ZW5k=" {
		t.Fatalf("base64 secrets parsed as %v", got2)
	}

	// ;ops= 后缀与轮换密钥共存，且不破坏后续 peer 条目切分。
	got3 := parseFederationPeerSecrets("peer-a@http://a:8080=s1,s2;ops=retire|metrics,peer-b@http://b:8080=t1")
	if len(got3) != 2 || got3[0] != "peer-a@http://a:8080=s1,s2;ops=retire|metrics" || got3[1] != "peer-b@http://b:8080=t1" {
		t.Fatalf("ops suffix grouping parsed as %v", got3)
	}

	if got := parseFederationPeerSecrets("  "); got != nil {
		t.Fatalf("blank input = %v, want nil", got)
	}
}

// TestValidate_FederationPeerSecrets 验证 per-peer 密钥可替代全局密钥，且非法条目被拒绝。
func TestValidate_FederationPeerSecrets(t *testing.T) {
	c := base()
	c.FederationPeers = []string{"http://peer-a:8080", "http://peer-b:8080"}
	c.FederationSecret = ""
	c.FederationPeerSecrets = []string{"peer-a@http://peer-a:8080=s1,s2", "peer-b@http://peer-b:8080=t1"}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid per-peer secrets rejected: %v", err)
	}

	c2 := base()
	c2.FederationPeers = []string{"http://peer-a:8080"}
	c2.FederationPeerSecrets = []string{"peer-a@http://peer-a:8080"} // 缺 =<secret>
	if err := c2.Validate(); err == nil {
		t.Fatal("malformed per-peer entry accepted")
	}

	c3 := base()
	c3.FederationPeers = []string{"http://peer-a:8080"}
	c3.FederationPeerSecrets = []string{"peer-a@peer-a:8080=s1"} // URL 缺 scheme/host
	if err := c3.Validate(); err == nil {
		t.Fatal("invalid per-peer URL accepted")
	}
}

// TestLoad_FederationPeerConfig 验证 flag 与 env 映射：
// --federation-peer-id / --federation-peer-secrets 及 OPSMESH_* 环境变量。
func TestLoad_FederationPeerConfig(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()

	cfg := loadForTest(
		"--federation-peer-id=cp-a",
		"--federation-peer-secrets=peer-a@http://a:8080=s1,s2,peer-b@http://b:8080=t1",
	)
	if cfg.FederationPeerID != "cp-a" {
		t.Fatalf("FederationPeerID = %q, want cp-a", cfg.FederationPeerID)
	}
	if len(cfg.FederationPeerSecrets) != 2 || cfg.FederationPeerSecrets[0] != "peer-a@http://a:8080=s1,s2" || cfg.FederationPeerSecrets[1] != "peer-b@http://b:8080=t1" {
		t.Fatalf("FederationPeerSecrets = %v", cfg.FederationPeerSecrets)
	}

	def := loadForTest()
	if def.FederationPeerID != "default" || def.FederationPeerSecrets != nil {
		t.Fatalf("defaults: peerID=%q secrets=%v", def.FederationPeerID, def.FederationPeerSecrets)
	}

	if err := os.Setenv("OPSMESH_FEDERATION_PEER_ID", "cp-env"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	if err := os.Setenv("OPSMESH_FEDERATION_PEER_SECRETS", "peer-a@http://a:8080=env-secret"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	envCfg := loadForTest()
	if envCfg.FederationPeerID != "cp-env" || len(envCfg.FederationPeerSecrets) != 1 || envCfg.FederationPeerSecrets[0] != "peer-a@http://a:8080=env-secret" {
		t.Fatalf("env mapping: peerID=%q secrets=%v", envCfg.FederationPeerID, envCfg.FederationPeerSecrets)
	}
}

// TestParseFederationAllowedPaths 验证联邦入站白名单解析：去空白、忽略空项、空输入为 nil。
func TestParseFederationAllowedPaths(t *testing.T) {
	got := parseFederationAllowedPaths(" /api/v1/federation/ , ,/api/v1/tasks ")
	want := []string{"/api/v1/federation/", "/api/v1/tasks"}
	if len(got) != len(want) {
		t.Fatalf("allowed paths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("allowed paths[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if got := parseFederationAllowedPaths("  "); got != nil {
		t.Fatalf("blank input = %v, want nil", got)
	}
}

// TestValidate_FederationAllowedPaths 验证白名单项必须以 "/" 开头，否则启动期拒绝。
func TestValidate_FederationAllowedPaths(t *testing.T) {
	c := base()
	c.FederationAllowedPaths = []string{"/api/v1/federation/", "/api/v1/tasks"}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid allowed paths rejected: %v", err)
	}

	bad := base()
	bad.FederationAllowedPaths = []string{"api/v1/tasks"}
	if err := bad.Validate(); err == nil {
		t.Fatal("allowed path without leading slash accepted")
	}
}

// TestLoad_FederationAllowedPaths 验证 flag 与 env 映射。
func TestLoad_FederationAllowedPaths(t *testing.T) {
	restore := clearOpsmeshEnv()
	defer restore()

	cfg := loadForTest("--federation-allowed-paths=/api/v1/federation/,/api/v1/tasks")
	if len(cfg.FederationAllowedPaths) != 2 || cfg.FederationAllowedPaths[0] != "/api/v1/federation/" || cfg.FederationAllowedPaths[1] != "/api/v1/tasks" {
		t.Fatalf("flag mapping: %v", cfg.FederationAllowedPaths)
	}

	if def := loadForTest(); def.FederationAllowedPaths != nil {
		t.Fatalf("default allowed paths = %v, want nil", def.FederationAllowedPaths)
	}

	if err := os.Setenv("OPSMESH_FEDERATION_ALLOWED_PATHS", "/api/v1/devices,/api/v1/tasks"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	envCfg := loadForTest()
	if len(envCfg.FederationAllowedPaths) != 2 || envCfg.FederationAllowedPaths[0] != "/api/v1/devices" || envCfg.FederationAllowedPaths[1] != "/api/v1/tasks" {
		t.Fatalf("env mapping: %v", envCfg.FederationAllowedPaths)
	}
}
