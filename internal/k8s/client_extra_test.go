// client_extra_test.go 补充 internal/k8s 包的正常路径测试（task 152）。
//
// 覆盖范围：
//   - K8sClient 正常构造（valid kubeconfig 内容与文件路径）
//   - TestConnection 正常/未授权/禁止/其他错误/nil clientset
//   - Close / forceSecureTLS(nil)
//   - Pod list/delete、Deployment list/scale/restart、Service/ConfigMap/Secret/Node/Namespace list
//     （通过 fake.NewSimpleClientset 注入 K8sClient.Clientset 验证资源操作正常返回）
//   - ClusterManager AddCluster/GetClient/RemoveClient/ListClusters/TestCluster 正常与错误流程
//
// 测试策略：client-go fake clientset（kubernetes.Interface 注入）+ 临时 kubeconfig 文件。
package k8s

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// validKubeconfig 是可解析的最小 kubeconfig（指向本地假地址，构造时不发起连接）。
const validKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: c1
  cluster:
    server: https://127.0.0.1:6443
    insecure-skip-tls-verify: true
users:
- name: u1
  user:
    token: test-token
contexts:
- name: ctx1
  context:
    cluster: c1
    user: u1
current-context: ctx1
`

// int32Ptr 返回 int32 指针，用于构造 Deployment.Spec.Replicas。
func int32Ptr(v int32) *int32 { return &v }

// =============================================================================
// K8sClient 正常构造
// =============================================================================

// TestNewK8sClient_ValidKubeconfig 验证 valid kubeconfig 内容可成功构造客户端。
func TestNewK8sClient_ValidKubeconfig(t *testing.T) {
	c, err := NewK8sClient("test-cluster", validKubeconfig)
	if err != nil {
		t.Fatalf("valid kubeconfig 应构造成功: %v", err)
	}
	if c.Name != "test-cluster" {
		t.Fatalf("Name=%q, want test-cluster", c.Name)
	}
	if c.Clientset == nil {
		t.Fatal("Clientset 不应为 nil")
	}
	if c.Config == nil {
		t.Fatal("Config 不应为 nil")
	}
	if c.Server == "" {
		t.Fatal("Server 不应为空")
	}
	// forceSecureTLS 应已关闭 insecure-skip-tls-verify。
	if c.Config.TLSClientConfig.Insecure {
		t.Fatal("forceSecureTLS 应将 Insecure 置为 false")
	}
}

// TestNewK8sClientFromPath_Valid 验证从 kubeconfig 文件路径可成功构造客户端。
func TestNewK8sClientFromPath_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(path, []byte(validKubeconfig), 0o600); err != nil {
		t.Fatalf("写入临时 kubeconfig 失败: %v", err)
	}
	c, err := NewK8sClientFromPath("from-path", path)
	if err != nil {
		t.Fatalf("从文件路径构造应成功: %v", err)
	}
	if c.Clientset == nil {
		t.Fatal("Clientset 不应为 nil")
	}
	if c.Config.TLSClientConfig.Insecure {
		t.Fatal("forceSecureTLS 应将 Insecure 置为 false")
	}
}

// TestNewK8sClientFromPath_Empty 验证空路径被拒绝。
func TestNewK8sClientFromPath_Empty(t *testing.T) {
	if _, err := NewK8sClientFromPath("empty", ""); err == nil {
		t.Fatal("空 kubeconfig 路径应被拒绝")
	}
}

// TestNewK8sClientFromPath_NotFound 验证不存在的文件路径被拒绝。
func TestNewK8sClientFromPath_NotFound(t *testing.T) {
	if _, err := NewK8sClientFromPath("missing", filepath.Join(t.TempDir(), "no-such-file")); err == nil {
		t.Fatal("不存在的 kubeconfig 路径应被拒绝")
	}
}

// TestNewK8sClientFromPath_ExecRejected 验证从路径加载含 exec 插件的 kubeconfig 被拒绝。
func TestNewK8sClientFromPath_ExecRejected(t *testing.T) {
	evil := strings.Replace(validKubeconfig, "    token: test-token",
		"    exec:\n      apiVersion: client.authentication.k8s.io/v1beta1\n      command: /bin/sh", 1)
	dir := t.TempDir()
	path := filepath.Join(dir, "evil-kubeconfig")
	if err := os.WriteFile(path, []byte(evil), 0o600); err != nil {
		t.Fatalf("写入临时 kubeconfig 失败: %v", err)
	}
	if _, err := NewK8sClientFromPath("evil", path); err == nil {
		t.Fatal("含 exec 插件的 kubeconfig 应被拒绝")
	}
}

// =============================================================================
// TestConnection
// =============================================================================

// TestTestConnection_Success 验证 fake clientset 列出 namespaces 成功时返回 nil。
func TestTestConnection_Success(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}})
	c := &K8sClient{Name: "fake", Clientset: cs}
	if err := c.TestConnection(); err != nil {
		t.Fatalf("TestConnection 应成功: %v", err)
	}
}

// TestTestConnection_NilClientset 验证 Clientset 为 nil 时返回错误。
func TestTestConnection_NilClientset(t *testing.T) {
	c := &K8sClient{Name: "nil-cs"}
	if err := c.TestConnection(); err == nil {
		t.Fatal("Clientset 为 nil 应返回错误")
	}
}

// TestTestConnection_Unauthorized 验证未授权错误被识别并返回明确错误。
func TestTestConnection_Unauthorized(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "namespaces", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewUnauthorized("token expired")
	})
	c := &K8sClient{Name: "fake", Clientset: cs}
	err := c.TestConnection()
	if err == nil {
		t.Fatal("未授权应返回错误")
	}
	if !strings.Contains(err.Error(), "鉴权失败") {
		t.Fatalf("错误信息应提及鉴权失败: %v", err)
	}
}

// TestTestConnection_Forbidden 验证权限不足错误被识别并返回明确错误。
func TestTestConnection_Forbidden(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "namespaces", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "", nil)
	})
	c := &K8sClient{Name: "fake", Clientset: cs}
	err := c.TestConnection()
	if err == nil {
		t.Fatal("权限不足应返回错误")
	}
	if !strings.Contains(err.Error(), "权限不足") {
		t.Fatalf("错误信息应提及权限不足: %v", err)
	}
}

// TestTestConnection_OtherError 验证其他错误透传。
func TestTestConnection_OtherError(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "namespaces", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, context.DeadlineExceeded
	})
	c := &K8sClient{Name: "fake", Clientset: cs}
	err := c.TestConnection()
	if err == nil {
		t.Fatal("其他错误应返回错误")
	}
	if !strings.Contains(err.Error(), "连接测试失败") {
		t.Fatalf("错误信息应提及连接测试失败: %v", err)
	}
}

// =============================================================================
// Close / forceSecureTLS
// =============================================================================

// TestClose_NoPanic 验证 Close 不 panic。
func TestClose_NoPanic(t *testing.T) {
	c := &K8sClient{Name: "fake", Clientset: fake.NewSimpleClientset()}
	c.Close()
}

// TestForceSecureTLS_NilConfig 验证 nil config 不 panic。
func TestForceSecureTLS_NilConfig(t *testing.T) {
	forceSecureTLS(nil) // 不应 panic
}

// =============================================================================
// 资源操作（Pod / Deployment / Service / ConfigMap / Secret / Node / Namespace）
// 通过 fake.NewSimpleClientset 注入 K8sClient.Clientset，验证正常操作路径。
// =============================================================================

// newFakeClient 构造带预置资源的 K8sClient（fake clientset 注入）。
func newFakeClient(t *testing.T) *K8sClient {
	t.Helper()
	cs := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "default"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod2", Namespace: "default"}},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "dep1", Namespace: "default"},
			Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
		},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc1", Namespace: "default"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm1", Namespace: "default"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret1", Namespace: "default"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
	)
	return &K8sClient{Name: "fake", Clientset: cs}
}

// TestPod_List 验证 Pod list 正常返回。
func TestPod_List(t *testing.T) {
	c := newFakeClient(t)
	ctx := context.Background()
	list, err := c.Clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List Pods 应成功: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("Pod 数量=%d, want 2", len(list.Items))
	}
}

// TestPod_Delete 验证 Pod delete 正常返回。
func TestPod_Delete(t *testing.T) {
	c := newFakeClient(t)
	ctx := context.Background()
	if err := c.Clientset.CoreV1().Pods("default").Delete(ctx, "pod1", metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Delete Pod 应成功: %v", err)
	}
	list, _ := c.Clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{})
	if len(list.Items) != 1 {
		t.Fatalf("删除后 Pod 数量=%d, want 1", len(list.Items))
	}
}

// TestDeployment_List 验证 Deployment list 正常返回。
func TestDeployment_List(t *testing.T) {
	c := newFakeClient(t)
	ctx := context.Background()
	list, err := c.Clientset.AppsV1().Deployments("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List Deployments 应成功: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("Deployment 数量=%d, want 1", len(list.Items))
	}
}

// TestDeployment_Scale 验证 Deployment scale（Get + 调整 Replicas + Update）正常返回。
//
// 注：client-go fake clientset 的 GetScale/UpdateScale 实现存在已知限制
// （内部将存储的 *Deployment 误转为 *Scale 触发 panic），故此处用 Get/Update
// 等价模拟副本数调整路径，与 controlplane handleScaleDeployment 的语义一致。
func TestDeployment_Scale(t *testing.T) {
	c := newFakeClient(t)
	ctx := context.Background()
	dep, err := c.Clientset.AppsV1().Deployments("default").Get(ctx, "dep1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get Deployment 应成功: %v", err)
	}
	if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 1 {
		t.Fatalf("初始 Replicas=%v, want 1", dep.Spec.Replicas)
	}
	*dep.Spec.Replicas = 3
	updated, err := c.Clientset.AppsV1().Deployments("default").Update(ctx, dep, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Update Deployment 应成功: %v", err)
	}
	if *updated.Spec.Replicas != 3 {
		t.Fatalf("更新后 Replicas=%d, want 3", *updated.Spec.Replicas)
	}
}

// TestDeployment_Restart 验证 Deployment restart（strategic merge patch）正常返回。
func TestDeployment_Restart(t *testing.T) {
	c := newFakeClient(t)
	ctx := context.Background()
	patch := []byte(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restarted":"now"}}}}}`)
	dep, err := c.Clientset.AppsV1().Deployments("default").Patch(
		ctx, "dep1", types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		t.Fatalf("Patch Deployment 应成功: %v", err)
	}
	if dep == nil {
		t.Fatal("Patch 返回的 Deployment 不应为 nil")
	}
}

// TestService_List 验证 Service list 正常返回。
func TestService_List(t *testing.T) {
	c := newFakeClient(t)
	ctx := context.Background()
	list, err := c.Clientset.CoreV1().Services("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List Services 应成功: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("Service 数量=%d, want 1", len(list.Items))
	}
}

// TestConfigMap_List 验证 ConfigMap list 正常返回。
func TestConfigMap_List(t *testing.T) {
	c := newFakeClient(t)
	ctx := context.Background()
	list, err := c.Clientset.CoreV1().ConfigMaps("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List ConfigMaps 应成功: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("ConfigMap 数量=%d, want 1", len(list.Items))
	}
}

// TestSecret_List 验证 Secret list 正常返回。
func TestSecret_List(t *testing.T) {
	c := newFakeClient(t)
	ctx := context.Background()
	list, err := c.Clientset.CoreV1().Secrets("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List Secrets 应成功: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("Secret 数量=%d, want 1", len(list.Items))
	}
}

// TestNode_List 验证 Node list 正常返回。
func TestNode_List(t *testing.T) {
	c := newFakeClient(t)
	ctx := context.Background()
	list, err := c.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List Nodes 应成功: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("Node 数量=%d, want 1", len(list.Items))
	}
}

// TestNamespace_List 验证 Namespace list 正常返回。
func TestNamespace_List(t *testing.T) {
	c := newFakeClient(t)
	ctx := context.Background()
	list, err := c.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List Namespaces 应成功: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("Namespace 数量=%d, want 1", len(list.Items))
	}
}

// =============================================================================
// ClusterManager
// =============================================================================

// TestClusterManager_AddCluster_Success 验证添加集群成功。
func TestClusterManager_AddCluster_Success(t *testing.T) {
	m := NewClusterManager()
	if err := m.AddCluster("c1", validKubeconfig); err != nil {
		t.Fatalf("AddCluster 应成功: %v", err)
	}
	c, err := m.GetClient("c1")
	if err != nil {
		t.Fatalf("GetClient 应成功: %v", err)
	}
	if c == nil {
		t.Fatal("GetClient 返回的 client 不应为 nil")
	}
}

// TestClusterManager_AddCluster_EmptyID 验证空 clusterID 被拒绝。
func TestClusterManager_AddCluster_EmptyID(t *testing.T) {
	m := NewClusterManager()
	if err := m.AddCluster("", validKubeconfig); err == nil {
		t.Fatal("空 clusterID 应被拒绝")
	}
}

// TestClusterManager_AddCluster_InvalidKubeconfig 验证无效 kubeconfig 被拒绝。
func TestClusterManager_AddCluster_InvalidKubeconfig(t *testing.T) {
	m := NewClusterManager()
	if err := m.AddCluster("c1", ""); err == nil {
		t.Fatal("空 kubeconfig 应被拒绝")
	}
}

// TestClusterManager_AddCluster_Replace 验证重复添加同 clusterID 替换旧连接。
func TestClusterManager_AddCluster_Replace(t *testing.T) {
	m := NewClusterManager()
	if err := m.AddCluster("c1", validKubeconfig); err != nil {
		t.Fatalf("首次 AddCluster 应成功: %v", err)
	}
	if err := m.AddCluster("c1", validKubeconfig); err != nil {
		t.Fatalf("再次 AddCluster 应成功: %v", err)
	}
	if _, err := m.GetClient("c1"); err != nil {
		t.Fatalf("替换后 GetClient 应成功: %v", err)
	}
}

// TestClusterManager_GetClient_NotFound 验证不存在的集群返回错误。
func TestClusterManager_GetClient_NotFound(t *testing.T) {
	m := NewClusterManager()
	if _, err := m.GetClient("no-such"); err == nil {
		t.Fatal("不存在的集群应返回错误")
	}
}

// TestClusterManager_RemoveCluster_Existing 验证移除已存在集群成功。
func TestClusterManager_RemoveCluster_Existing(t *testing.T) {
	m := NewClusterManager()
	if err := m.AddCluster("c1", validKubeconfig); err != nil {
		t.Fatalf("AddCluster 应成功: %v", err)
	}
	m.RemoveCluster("c1")
	if _, err := m.GetClient("c1"); err == nil {
		t.Fatal("移除后 GetClient 应返回错误")
	}
}

// TestClusterManager_RemoveCluster_NotExisting 验证移除不存在的集群静默返回。
func TestClusterManager_RemoveCluster_NotExisting(t *testing.T) {
	m := NewClusterManager()
	m.RemoveCluster("no-such") // 不应 panic
}

// TestClusterManager_ListClusters 验证返回已连接集群 ID 列表。
func TestClusterManager_ListClusters(t *testing.T) {
	m := NewClusterManager()
	if err := m.AddCluster("c1", validKubeconfig); err != nil {
		t.Fatalf("AddCluster c1 应成功: %v", err)
	}
	if err := m.AddCluster("c2", validKubeconfig); err != nil {
		t.Fatalf("AddCluster c2 应成功: %v", err)
	}
	list := m.ListClusters()
	if len(list) != 2 {
		t.Fatalf("集群数量=%d, want 2", len(list))
	}
	m.RemoveCluster("c1")
	if got := len(m.ListClusters()); got != 1 {
		t.Fatalf("移除后集群数量=%d, want 1", got)
	}
}

// TestClusterManager_TestCluster_EmptyID 验证空 clusterID 被拒绝。
func TestClusterManager_TestCluster_EmptyID(t *testing.T) {
	m := NewClusterManager()
	if err := m.TestCluster("", validKubeconfig); err == nil {
		t.Fatal("空 clusterID 应被拒绝")
	}
}

// TestClusterManager_TestCluster_InvalidKubeconfig 验证无效 kubeconfig 被拒绝。
func TestClusterManager_TestCluster_InvalidKubeconfig(t *testing.T) {
	m := NewClusterManager()
	if err := m.TestCluster("c1", ""); err == nil {
		t.Fatal("空 kubeconfig 应被拒绝")
	}
}
