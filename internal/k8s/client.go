// Package k8s 封装 K8s 集群客户端连接与多集群管理。
//
// Phase 3 后端 K8s 集群管理：
//   - K8sClient：封装单个 K8s 集群的 client-go 连接（Clientset + rest.Config）；
//   - ClusterManager：管理多个集群连接（clusterID -> K8sClient），并发安全；
//   - 支持 kubeconfig 内容（YAML 字符串）与 kubeconfig 文件路径两种构造方式；
//   - TestConnection 通过列出 namespaces 验证集群连通性。
//
// 设计要点：
//   - rest.Config 由 clientcmd.RESTConfigFromKubeConfig / BuildConfigFromFlags 创建；
//   - Clientset 由 kubernetes.NewForConfig 创建；
//   - 错误处理返回有意义的错误信息（含集群名/原始错误），便于排障；
//   - kubeconfig 是敏感内容，调用方负责脱敏（API 层用 *** 替换）。
package k8s

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// K8sClient 封装单个 K8s 集群的客户端连接。
//
// 字段说明：
//   - Name：集群名（仅用于日志/错误信息，不参与连接）；
//   - Server：API Server 地址（从 kubeconfig 解析得到，便于 API 层展示）；
//   - Clientset：k8s.io/client-go/kubernetes 的标准 Clientset，覆盖 CoreV1/AppsV1 等内置资源；
//     字段类型为 kubernetes.Interface 便于测试注入 fake.NewSimpleClientset()；
//   - Config：底层 rest.Config，调用方可据此构造 DynamicClient / DiscoveryClient 等扩展客户端。
type K8sClient struct {
	Name      string
	Server    string
	Clientset kubernetes.Interface
	Config    *rest.Config
}

// NewK8sClient 从 kubeconfig 内容创建客户端。
// kubeconfigData 是 kubeconfig 文件的 YAML 内容（字符串）。
// name 仅用于日志/错误信息标识，不参与连接。
func NewK8sClient(name, kubeconfigData string) (*K8sClient, error) {
	if kubeconfigData == "" {
		return nil, fmt.Errorf("k8s: 集群 %q 的 kubeconfig 内容为空", name)
	}
	// 安全校验：拒绝含 exec/auth-provider 凭据插件的 kubeconfig，
	// client-go 会在首次请求时本地执行此类插件，恶意 kubeconfig 可致控制面 RCE。
	cfg, err := clientcmd.Load([]byte(kubeconfigData))
	if err != nil {
		return nil, fmt.Errorf("k8s: 集群 %q 解析 kubeconfig 结构失败: %w", name, err)
	}
	if err := validateKubeConfigSafety(cfg); err != nil {
		return nil, fmt.Errorf("k8s: 集群 %q %w", name, err)
	}
	// RESTConfigFromKubeConfig 直接从 kubeconfig 字节解析出 rest.Config，
	// 不依赖 KUBECONFIG 环境变量或 ~/.kube/config 默认路径。
	config, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfigData))
	if err != nil {
		return nil, fmt.Errorf("k8s: 集群 %q 解析 kubeconfig 失败: %w", name, err)
	}
	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("k8s: 集群 %q 创建 Clientset 失败: %w", name, err)
	}
	forceSecureTLS(config)
	return &K8sClient{
		Name:      name,
		Server:    config.Host,
		Clientset: cs,
		Config:    config,
	}, nil
}

// NewK8sClientFromPath 从 kubeconfig 文件路径创建客户端。
// kubeconfigPath 是 kubeconfig 文件的绝对或相对路径。
// 适用于控制面本地调试或 kubeconfig 已落盘的部署场景。
func NewK8sClientFromPath(name, kubeconfigPath string) (*K8sClient, error) {
	if kubeconfigPath == "" {
		return nil, fmt.Errorf("k8s: 集群 %q 的 kubeconfig 路径为空", name)
	}
	// 安全校验：同内容版，拒绝含 exec/auth-provider 凭据插件的 kubeconfig。
	cfg, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("k8s: 集群 %q 读取 kubeconfig 文件失败: %w", name, err)
	}
	if err := validateKubeConfigSafety(cfg); err != nil {
		return nil, fmt.Errorf("k8s: 集群 %q %w", name, err)
	}
	// BuildConfigFromFlags 第一个参数为空串时跳过 master URL，强制从 kubeconfig 解析。
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("k8s: 集群 %q 从路径 %q 加载 kubeconfig 失败: %w", name, kubeconfigPath, err)
	}
	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("k8s: 集群 %q 创建 Clientset 失败: %w", name, err)
	}
	forceSecureTLS(config)
	return &K8sClient{
		Name:      name,
		Server:    config.Host,
		Clientset: cs,
		Config:    config,
	}, nil
}

// TestConnection 测试集群连接是否正常（列出 namespaces）。
//
// 判定逻辑：
//   - 列出 namespaces 成功 → 连接正常，返回 nil；
//   - 返回未授权错误 → 凭据无效，返回明确错误；
//   - 返回超时/连接拒绝 → 网络不可达，返回明确错误；
//   - 其他错误 → 透传 client-go 原始错误。
//
// 超时：固定 10s（避免阻塞 API 调用方）。
func (c *K8sClient) TestConnection() error {
	if c == nil || c.Clientset == nil {
		return fmt.Errorf("k8s: 集群 %q 客户端未初始化", c.Name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		// 区分常见错误类型，给出更友好的错误信息。
		if errors.IsUnauthorized(err) {
			return fmt.Errorf("k8s: 集群 %q 鉴权失败（kubeconfig 凭据无效或已过期）: %w", c.Name, err)
		}
		if errors.IsForbidden(err) {
			return fmt.Errorf("k8s: 集群 %q 权限不足（缺少 list namespaces 权限）: %w", c.Name, err)
		}
		return fmt.Errorf("k8s: 集群 %q 连接测试失败: %w", c.Name, err)
	}
	return nil
}

// Close 释放连接资源。
//
// client-go 的 Clientset 基于 HTTP 连接池，由 net/http transport 管理，
// 无显式 Close 方法；此处保留 Close 占位以兼容后续可能引入的显式资源（如 DynamicClient 缓存）。
func (c *K8sClient) Close() {
	// 当前 client-go Clientset 无需显式释放；保留方法以稳定接口。
}

// validateKubeConfigSafety 校验 kubeconfig 不含可在本机执行任意命令的凭据插件（安全加固）。
// client-go 在首次 API 请求时会本地执行 users[].user.exec 凭据插件与 auth-provider 外部命令，
// 恶意 kubeconfig 可借此在控制面主机上执行任意命令（RCE），故一律拒绝，要求使用静态凭据（token/证书）。
func validateKubeConfigSafety(cfg *clientcmdapi.Config) error {
	for userName, authInfo := range cfg.AuthInfos {
		if authInfo == nil {
			continue
		}
		if authInfo.Exec != nil {
			return fmt.Errorf("用户 %q 配置了 exec 凭据插件（command=%q），出于安全已禁用；请改用 token 或客户端证书", userName, authInfo.Exec.Command)
		}
		if authInfo.AuthProvider != nil {
			return fmt.Errorf("用户 %q 配置了 auth-provider 凭据插件（name=%q），出于安全已禁用；请改用 token 或客户端证书", userName, authInfo.AuthProvider.Name)
		}
	}
	return nil
}

// forceSecureTLS 强制关闭 insecure-skip-tls-verify（安全加固）。
// kubeconfig 中 insecure-skip-tls-verify: true 会跳过服务端证书校验，易被中间人攻击；
// 托管集群统一要求有效证书链（自签证书请通过 certificate-authority-data 提供 CA）。
func forceSecureTLS(config *rest.Config) {
	if config == nil {
		return
	}
	config.Insecure = false
}
