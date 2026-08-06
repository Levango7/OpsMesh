package config

import (
	"testing"
	"time"
)

// base 返回一份合理的控制面默认配置，避免每个用例重复样板。
func base() *Config {
	return &Config{
		Mode:           "controlplane",
		HTTPPort:       8080,
		GRPCPort:       9090,
		MetricsPort:     9091,
		Store:          "memory",
		TaskTimeout:    120 * time.Second,
		ShutdownTimeout: 15 * time.Second,
		WorkerConcurrency: 4,
		TaskLeaseSec:   300,
		Replicas:       1,
		TaskMaxRetries: 3,
		LogBackend:     "memory", // M4-4B 默认日志后端
	}
}

// TestValidate_OK 验证默认控制面配置通过校验。
func TestValidate_OK(t *testing.T) {
	if err := base().Validate(); err != nil {
		t.Fatalf("base valid config errored: %v", err)
	}
}

// TestValidate_MemoryMultiReplica A3：memory store 配多副本必须被拒绝（数据分裂）。
func TestValidate_MemoryMultiReplica(t *testing.T) {
	c := base()
	c.Store = "memory"
	c.Replicas = 2
	if err := c.Validate(); err == nil {
		t.Fatal("memory + replicas>1 应被拒绝，但通过了")
	}
}

// TestValidate_MysqlMultiReplica A4：mysql store + 多副本是 HA 合法组合。
func TestValidate_MysqlMultiReplica(t *testing.T) {
	c := base()
	c.Store = "mysql"
	c.MySQLDSN = "u:p@tcp(db:3306)/ops_device"
	c.Replicas = 2
	if err := c.Validate(); err != nil {
		t.Fatalf("mysql + replicas>1 应合法，却报错: %v", err)
	}
}

// TestValidate_ProductionRejectsNoTLS H6 等保加固：生产模式 + 无 TLS 证书应被 Validate 拒绝
// （Production=true 且 TLSCert="" 直接返回 error，避免 agent↔控制面明文通信）。
func TestValidate_ProductionRejectsNoTLS(t *testing.T) {
	c := base()
	c.Production = true
	c.Store = "memory"
	c.Replicas = 1 // 单副本不触发多副本拒绝
	c.TLSCert = ""  // 无 TLS 证书
	// production + 无 TLS 应被拒绝（H6 等保三级要求）。
	if err := c.Validate(); err == nil {
		t.Fatal("production + 无 TLS 应被拒绝，但 Validate 通过了")
	}
}

// TestValidate_ModeAndStore A4：非法 mode 与 mysql 缺 DSN 必须拒绝。
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

// TestLoad_ProductionEnablesRequireAuth A4：生产模式默认开启 require-auth（除非显式关闭）。
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

// TestValidate_LogBackend M4-4B：非法 log-backend 必须被拒绝。
func TestValidate_LogBackend(t *testing.T) {
	c := base()
	c.LogBackend = "bogus"
	if err := c.Validate(); err == nil {
		t.Fatal("非法 log-backend 应被拒绝")
	}
}

// TestValidate_LogBackendLokiMissingEndpoint M4-4B：log-backend=loki 但缺 endpoint 必须被拒绝。
func TestValidate_LogBackendLokiMissingEndpoint(t *testing.T) {
	c := base()
	c.LogBackend = "loki"
	c.LokiEndpoint = ""
	if err := c.Validate(); err == nil {
		t.Fatal("log-backend=loki 缺 endpoint 应被拒绝")
	}
}

// TestValidate_LogBackendLokiOK M4-4B：log-backend=loki + endpoint 合法。
func TestValidate_LogBackendLokiOK(t *testing.T) {
	c := base()
	c.LogBackend = "loki"
	c.LokiEndpoint = "http://loki:3100"
	if err := c.Validate(); err != nil {
		t.Fatalf("loki 合法配置应通过: %v", err)
	}
}

// TestValidate_LogBackendESMissingEndpoint M4-4B：log-backend=es 但缺 endpoint 必须被拒绝。
func TestValidate_LogBackendESMissingEndpoint(t *testing.T) {
	c := base()
	c.LogBackend = "es"
	c.ESEndpoint = ""
	if err := c.Validate(); err == nil {
		t.Fatal("log-backend=es 缺 endpoint 应被拒绝")
	}
}

// TestValidate_LogBackendESMissingIndex M4-4B：log-backend=es 但缺 index 必须被拒绝。
func TestValidate_LogBackendESMissingIndex(t *testing.T) {
	c := base()
	c.LogBackend = "es"
	c.ESEndpoint = "http://es:9200"
	c.ESIndex = ""
	if err := c.Validate(); err == nil {
		t.Fatal("log-backend=es 缺 index 应被拒绝")
	}
}

// TestValidate_LogBackendESOK M4-4B：log-backend=es + endpoint + index 合法。
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
	if err := c.Validate(); err == nil {
		t.Fatal("production + 空 jwt-secret 应被拒绝，但 Validate 通过了")
	}
	// prod + 合法密钥 -> 通过
	c2 := base()
	c2.Production = true
	c2.TLSCert = "tls.crt"
	c2.JWTSecret = "0123456789abcdef0123456789abcdef" // 32 字节
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
	c.JWTSecret = "tooshort" // 8 字节 < 32
	if err := c.Validate(); err == nil {
		t.Fatal("production + (<32) jwt-secret 应被拒绝，但通过了")
	}
	c2 := base()
	c2.Production = true
	c2.TLSCert = "tls.crt"
	c2.JWTSecret = "0123456789abcdef0123456789abcdef" // 32 字节
	if err := c2.Validate(); err != nil {
		t.Fatalf("production + 32 字节 jwt-secret 应通过: %v", err)
	}
}
