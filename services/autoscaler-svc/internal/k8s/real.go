package k8s

// real.go — 真实扩缩容执行器：通过 APIServer 改 Deployment 的 spec.replicas。
//
// 为什么用 merge patch 而不是 scale 子资源（这是实测出来的，不是风格偏好）：
// client-go v0.32 的 apps/v1 scheme **没有注册 Scale 类型**
// （k8s.io/api@v0.32.0/apps/v1/register.go 的 AddKnownTypes 里只有
// Deployment/StatefulSet/DaemonSet/ReplicaSet/ControllerRevision 及其 List），
// 于是 `AppsV1().Deployments(ns).GetScale/UpdateScale` 这条 typed 路径在 JSON 协商下
// 连响应都解不开——本轮用它写第一版时，假 APIServer 收到的错误原文是：
//
//	no kind "Scale" is registered for version "apps/v1" in scheme "pkg/runtime/scheme.go:100"
//
// kubectl 走的是另一套 `k8s.io/client-go/scale` 客户端（自带注册了 autoscaling/v1 Scale
// 的 scheme），要引它就得再拖一层 scheme/mapper 依赖。改副本数这件事本身的语义就是
// "把 spec.replicas 设成 N"，用 merge patch 只写这一个字段：
//   - 不需要先读再写、不需要携带 resourceVersion，因此不会与别的写入者互相顶掉；
//   - patch 的 body 只含 spec.replicas，不会覆盖 deployment 的其它 spec（这与
//     `Update` 全量对象有本质区别）。
//
// kubeconfig 的加载与安全校验**复用控制面那份实现**（internal/k8s）而不是再写一份：
// 它会拒绝含 exec / auth-provider 凭据插件的 kubeconfig——client-go 在首次请求时会在
// 本地执行这些插件，恶意 kubeconfig 等于在 autoscaler-svc 的主机上拿到 RCE。
// 两处各写一遍的话，autoscaler 侧迟早漏掉这条校验（本仓已多次为此付学费）。

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	ctrlk8s "github.com/Levango7/OpsMesh/internal/k8s"
)

// scaleRPCTimeout 是单次读写的整体超时。
//
// 没有超时的话，APIServer 挂住会让评估循环与手工扩容请求一起卡死——而 autoscaler
// 卡死的表现是"什么都没发生"，比报错更难发现。
const scaleRPCTimeout = 10 * time.Second

// RealScaler 用 client-go 真正改动集群里的 Deployment。
type RealScaler struct {
	cs       kubernetes.Interface
	executor string
	timeout  time.Duration
}

// NewFromKubeconfig 从 kubeconfig 文件路径建立真实执行器。
func NewFromKubeconfig(path string) (*RealScaler, error) {
	if path == "" {
		return nil, fmt.Errorf("k8s: kubeconfig 路径为空（AUTOSCALER_KUBECONFIG / KUBECONFIG 均未设置）")
	}
	cli, err := ctrlk8s.NewK8sClientFromPath("autoscaler-svc", path)
	if err != nil {
		return nil, err
	}
	return &RealScaler{cs: cli.Clientset, executor: ExecutorKubeconfig, timeout: scaleRPCTimeout}, nil
}

// NewInCluster 在 Pod 内以 ServiceAccount 身份建立真实执行器。
func NewInCluster() (*RealScaler, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("k8s: 集群内配置不可用（autoscaler-svc 未运行在 Pod 里，或缺少 ServiceAccount）: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s: 创建 Clientset 失败: %w", err)
	}
	return &RealScaler{cs: cs, executor: ExecutorInCluster, timeout: scaleRPCTimeout}, nil
}

// newWithClientset 注入已有 Clientset（测试用：httptest 假 APIServer + 真实 client-go 编解码）。
func newWithClientset(cs kubernetes.Interface, executor string) *RealScaler {
	return &RealScaler{cs: cs, executor: executor, timeout: scaleRPCTimeout}
}

// Executor 实现 Scaler。
func (r *RealScaler) Executor() string { return r.executor }

// GetReplicas 读取 Deployment 的 spec.replicas。
//
// 未显式设置 replicas 时 APIServer 会给出默认值 1，因此这里返回 0 只可能是
// "对象不存在/读失败"，那已经先被 err 分支挡住了。
func (r *RealScaler) GetReplicas(deployment, namespace string) (int32, error) {
	if deployment == "" {
		return 0, fmt.Errorf("deployment name cannot be empty")
	}
	if namespace == "" {
		namespace = "default"
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	d, err := r.cs.AppsV1().Deployments(namespace).Get(ctx, deployment, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("k8s: 读取 Deployment %s/%s 失败: %w", namespace, deployment, err)
	}
	if d.Spec.Replicas == nil {
		// 老对象或某些清单确实不写 replicas（由 HPA/默认值接管），此时语义是 1。
		return 1, nil
	}
	return *d.Spec.Replicas, nil
}

// SetReplicas 用 merge patch 把 spec.replicas 设为 replicas。
func (r *RealScaler) SetReplicas(deployment, namespace string, replicas int32) error {
	if deployment == "" {
		return fmt.Errorf("deployment name cannot be empty")
	}
	if replicas < 0 {
		return fmt.Errorf("replicas cannot be negative")
	}
	if namespace == "" {
		namespace = "default"
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	// %d 直接拼进 JSON：replicas 已在上面判定为非负整数，不存在注入面。
	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas)
	_, err := r.cs.AppsV1().Deployments(namespace).Patch(ctx, deployment, types.MergePatchType,
		[]byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("k8s: 设置 Deployment %s/%s 副本数为 %d 失败: %w", namespace, deployment, replicas, err)
	}
	return nil
}
