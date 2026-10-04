package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the autoscaler-svc.
type Config struct {
	HTTPPort        int           `json:"httpPort"`
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`

	// Prometheus settings.
	PrometheusURL string `json:"prometheusUrl"`

	// K8s settings.
	KubeConfig    string `json:"kubeConfig"`
	KubeNamespace string `json:"kubeNamespace"`

	// K8sExecutor 选择扩缩容执行器，决定"扩容"这个动作有没有真实副作用：
	//   - "simulated"（默认）：进程内 map，不动任何集群，重启即丢；
	//   - "kubeconfig"：用 KubeConfig 指向的 kubeconfig 连远端 APIServer；
	//   - "in-cluster"：以 Pod 的 ServiceAccount 身份连本集群。
	// 此前没有这个开关——main.go 无条件装配内存 map，而 API 对外报"已扩容"。
	K8sExecutor string `json:"k8sExecutor"`

	// Production 对应 OPSMESH_PRODUCTION。真实执行器配不上时必须硬失败，
	// 不允许静默退回模拟（否则生产上表现为"扩了但没动"，且没有任何信号）。
	Production bool `json:"production"`

	// Cooldown settings.
	CooldownUp   time.Duration `json:"cooldownUp"`
	CooldownDown time.Duration `json:"cooldownDown"`
}

// Load returns a Config populated from environment variables with defaults.
func Load() *Config {
	return &Config{
		HTTPPort:        getEnvInt("AUTOSCALER_SVC_HTTP_PORT", 8080),
		ShutdownTimeout: getEnvDuration("AUTOSCALER_SVC_SHUTDOWN_TIMEOUT", 10*time.Second),
		PrometheusURL:   getEnv("PROMETHEUS_URL", "http://localhost:9090"),
		// AUTOSCALER_KUBECONFIG 优先于通用的 KUBECONFIG：容器里 KUBECONFIG 可能被
		// 别的挂载占位，而本服务需要显式指向"要扩容的那个集群"。
		KubeConfig:    getEnv("AUTOSCALER_KUBECONFIG", getEnv("KUBECONFIG", "")),
		KubeNamespace: getEnv("KUBE_NAMESPACE", "default"),
		K8sExecutor:   getEnv("AUTOSCALER_K8S_EXECUTOR", "simulated"),
		Production:    getEnvBool("OPSMESH_PRODUCTION", false),
		CooldownUp:    getEnvDuration("COOLDOWN_UP", 60*time.Second),
		CooldownDown:  getEnvDuration("COOLDOWN_DOWN", 300*time.Second),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// getEnvBool 解析布尔环境变量，接受 true/1/yes/on（大小写不敏感）。
//
// 值存在但解析不出来时**返回默认值**而不是报错：本服务没有错误上报通道，
// 把 "OPSMESH_PRODUCTION=ture" 这种笔误变成启动失败，会让人以为服务坏了。
func getEnvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "t", "true", "yes", "y", "on":
		return true
	case "0", "f", "false", "no", "n", "off":
		return false
	}
	return def
}
