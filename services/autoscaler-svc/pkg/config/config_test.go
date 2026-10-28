package config

import (
	"testing"
	"time"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"AUTOSCALER_SVC_HTTP_PORT", "AUTOSCALER_SVC_SHUTDOWN_TIMEOUT", "PROMETHEUS_URL",
		"AUTOSCALER_KUBECONFIG", "KUBECONFIG", "KUBE_NAMESPACE", "AUTOSCALER_K8S_EXECUTOR",
		"OPSMESH_PRODUCTION", "COOLDOWN_UP", "COOLDOWN_DOWN",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)
	c := Load()
	want := &Config{
		HTTPPort:        8080,
		ShutdownTimeout: 10 * time.Second,
		PrometheusURL:   "http://localhost:9090",
		KubeConfig:      "",
		KubeNamespace:   "default",
		K8sExecutor:     "simulated",
		Production:      false,
		CooldownUp:      60 * time.Second,
		CooldownDown:    300 * time.Second,
	}
	if *c != *want {
		t.Errorf("默认配置不符：\n got=%+v\nwant=%+v", *c, *want)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("AUTOSCALER_SVC_HTTP_PORT", "8500")
	t.Setenv("AUTOSCALER_SVC_SHUTDOWN_TIMEOUT", "12s")
	t.Setenv("PROMETHEUS_URL", "http://prom:9090")
	t.Setenv("AUTOSCALER_KUBECONFIG", "/etc/kube/config")
	t.Setenv("KUBE_NAMESPACE", "ops")
	t.Setenv("AUTOSCALER_K8S_EXECUTOR", "in-cluster")
	t.Setenv("OPSMESH_PRODUCTION", "true")
	t.Setenv("COOLDOWN_UP", "10s")
	t.Setenv("COOLDOWN_DOWN", "20s")

	c := Load()
	if c.HTTPPort != 8500 || c.ShutdownTimeout != 12*time.Second {
		t.Errorf("端口/时长未覆盖：%+v", c)
	}
	if c.PrometheusURL != "http://prom:9090" || c.KubeConfig != "/etc/kube/config" || c.KubeNamespace != "ops" {
		t.Errorf("外部依赖配置未覆盖：%+v", c)
	}
	if c.K8sExecutor != "in-cluster" || !c.Production {
		t.Errorf("执行器/生产开关未覆盖：%+v", c)
	}
	if c.CooldownUp != 10*time.Second || c.CooldownDown != 20*time.Second {
		t.Errorf("冷却配置未覆盖：%+v", c)
	}
}

// TestLoad_KubeConfigPrecedence 钉住 AUTOSCALER_KUBECONFIG 优先于通用 KUBECONFIG：
// 容器里 KUBECONFIG 可能是别的挂载，误用会扩错集群。
func TestLoad_KubeConfigPrecedence(t *testing.T) {
	clearEnv(t)
	t.Setenv("KUBECONFIG", "/generic/kube")
	t.Setenv("AUTOSCALER_KUBECONFIG", "/specific/kube")
	if c := Load(); c.KubeConfig != "/specific/kube" {
		t.Errorf("AUTOSCALER_KUBECONFIG 应优先，得到 %q", c.KubeConfig)
	}

	clearEnv(t)
	t.Setenv("KUBECONFIG", "/generic/kube")
	if c := Load(); c.KubeConfig != "/generic/kube" {
		t.Errorf("未设专用变量时应回落到 KUBECONFIG，得到 %q", c.KubeConfig)
	}
}

// TestGetEnvBool 钉住布尔解析口径：接受 1/t/true/yes/y/on 与 0/f/false/no/n/off（大小写不敏感），
// 解析不出来时回默认值而不是报错（笔误不应让服务起不来）。
func TestGetEnvBool(t *testing.T) {
	cases := []struct {
		value string
		def   bool
		want  bool
	}{
		{"", true, true},
		{"", false, false},
		{"true", false, true},
		{"TRUE", false, true},
		{" yes ", false, true},
		{"1", false, true},
		{"on", false, true},
		{"false", true, false},
		{"OFF", true, false},
		{"0", true, false},
		{"n", true, false},
		{"ture", true, true},   // 笔误 → 默认值
		{"ture", false, false}, // 笔误 → 默认值
	}
	for _, tc := range cases {
		t.Setenv("AUTOSCALER_TEST_BOOL", tc.value)
		if got := getEnvBool("AUTOSCALER_TEST_BOOL", tc.def); got != tc.want {
			t.Errorf("getEnvBool(%q, %v) = %v，期望 %v", tc.value, tc.def, got, tc.want)
		}
	}
}

func TestLoad_InvalidValuesFallBack(t *testing.T) {
	clearEnv(t)
	t.Setenv("AUTOSCALER_SVC_HTTP_PORT", "port")
	t.Setenv("COOLDOWN_UP", "60")

	c := Load()
	if c.HTTPPort != 8080 {
		t.Errorf("非法端口应回落 8080，得到 %d", c.HTTPPort)
	}
	if c.CooldownUp != 60*time.Second {
		t.Errorf("缺单位时长应回落 60s，得到 %v", c.CooldownUp)
	}
}
