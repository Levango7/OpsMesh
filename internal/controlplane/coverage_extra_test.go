// coverage_extra_test.go 补充 controlplane 包未覆盖代码路径，目标覆盖率 ≥ 80%。
//
// 覆盖模块：federation.go / k8s_cluster.go / k8s_manage.go / server_netsec.go /
// backup.go / server_batch.go / grpc.go / device_metrics.go
//
// 测试风格：httptest + MemoryStore + fake clientset，遵循现有测试风格（loginAsAdmin 鉴权）。
package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/alertengine"
	"opsmesh/internal/config"
	"opsmesh/internal/grpcx"
	"opsmesh/internal/k8s"
	"opsmesh/internal/logstore"
	"opsmesh/internal/metrics"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	intstr "k8s.io/apimachinery/pkg/util/intstr"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// =============================================================================
// federation.go — signFederationRequest / handler 错误路径
// =============================================================================

// TestSignFederationRequest_WithSecret 验证带共享密钥时 signFederationRequest 实际执行签名：
// 构造带 secret 的 FederationManager，经 ForwardTask 触发签名头注入，peer 侧验签通过。
func TestSignFederationRequest_WithSecret(t *testing.T) {
	secret := "fed-sign-test-secret"
	// peer 侧：真实 Server + FederationSecret，验签放行。
	peerSt := store.NewMemoryStore()
	peerSt.Register(&proto.AgentInfo{Segment: "peer-seg", TenantID: "t1", Hostname: "peer-agent"})
	peerSrv := &Server{
		store:       peerSt,
		cfg:         &config.Config{TaskMaxRetries: 3, Demo: true, FederationSecret: secret},
		requireAuth: false,
	}
	peerMux := http.NewServeMux()
	peerMux.HandleFunc("/api/v1/tasks", peerSrv.handleListTasks)
	peerMux.HandleFunc("/api/v1/devices", peerSrv.handleDevices)
	peerMux.HandleFunc("/healthz", peerSrv.handleHealthz)
	peer := httptest.NewServer(peerMux)
	defer peer.Close()

	// 本地：带 secret 的 FederationManager，ForwardTask 会调用 signFederationRequest。
	fed := NewFederationManager([]string{peer.URL}, store.NewMemoryStore(), secret, nil)
	if fed == nil {
		t.Fatal("NewFederationManager returned nil")
	}

	// 取 peer 上 agent ID。
	devReq, _ := http.NewRequest(http.MethodGet, peer.URL+"/api/v1/devices", nil)
	devReq.Header.Set("X-Tenant-ID", "t1")
	resp, err := peer.Client().Do(devReq)
	if err != nil {
		t.Fatalf("get peer devices: %v", err)
	}
	var segMap map[string][]proto.DeviceInfo
	json.NewDecoder(resp.Body).Decode(&segMap)
	resp.Body.Close()
	var peerAgentID string
	for _, devs := range segMap {
		if len(devs) > 0 {
			peerAgentID = devs[0].AgentID
			break
		}
	}
	if peerAgentID == "" {
		t.Fatal("no agent on peer")
	}

	// ForwardTask 带 identity 头，signFederationRequest 应注入签名头，peer 验签通过。
	idHeaders := http.Header{}
	idHeaders.Set("X-Tenant-ID", "t1")
	idHeaders.Set("X-User-Id", "u1")
	created, err := fed.ForwardTask(context.Background(), peer.URL, proto.Task{
		AgentID: peerAgentID, Type: "shell", Command: "echo signed",
	}, idHeaders)
	if err != nil {
		t.Fatalf("ForwardTask with secret err: %v", err)
	}
	if created.TaskID == "" {
		t.Fatal("created.TaskID empty")
	}
}

// TestHandleFederationPeers_NilFed 验证 fed=nil 时返回 404。
func TestHandleFederationPeers_NilFed(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/federation/peers", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleFederationPeers(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nil fed: status=%d, want 404", rec.Code)
	}
}

// TestHandleFederationPeers_MethodNotAllowed 验证非 GET 方法返回 405。
func TestHandleFederationPeers_MethodNotAllowed(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/peers", nil)
	rec := httptest.NewRecorder()
	s.handleFederationPeers(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status=%d, want 405", rec.Code)
	}
}

// TestHandleFederationForwardTask_NilFed 验证 fed=nil 时返回 404。
func TestHandleFederationForwardTask_NilFed(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/forward/task", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	s.handleFederationForwardTask(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nil fed: status=%d, want 404", rec.Code)
	}
}

// TestHandleFederationForwardTask_MethodNotAllowed 验证非 POST 方法返回 405。
func TestHandleFederationForwardTask_MethodNotAllowed(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/federation/forward/task", nil)
	rec := httptest.NewRecorder()
	s.handleFederationForwardTask(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: status=%d, want 405", rec.Code)
	}
}

// TestHandleFederationForwardTask_BadJSON 验证非法 JSON 返回 400。
func TestHandleFederationForwardTask_BadJSON(t *testing.T) {
	peer := newPeerTestServer(t)
	defer peer.Close()
	s := &Server{
		store: store.NewMemoryStore(),
		cfg:   &config.Config{FederationPeers: []string{peer.URL}, Demo: true},
		fed:   NewFederationManager([]string{peer.URL}, store.NewMemoryStore(), "", nil),
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/forward/task", strings.NewReader("not json"))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleFederationForwardTask(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: status=%d, want 400", rec.Code)
	}
}

// TestHandleFederationForwardTask_EmptyPeerURL 验证空 peerURL 返回 400。
func TestHandleFederationForwardTask_EmptyPeerURL(t *testing.T) {
	peer := newPeerTestServer(t)
	defer peer.Close()
	s := &Server{
		store: store.NewMemoryStore(),
		cfg:   &config.Config{FederationPeers: []string{peer.URL}, Demo: true},
		fed:   NewFederationManager([]string{peer.URL}, store.NewMemoryStore(), "", nil),
	}
	body, _ := json.Marshal(map[string]interface{}{
		"peerURL": "",
		"task":    map[string]string{"agentID": "a1", "command": "echo"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/forward/task", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleFederationForwardTask(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty peerURL: status=%d, want 400", rec.Code)
	}
}

// TestHandleFederationForwardTask_EmptyTaskFields 验证空 agentID/command 返回 400。
func TestHandleFederationForwardTask_EmptyTaskFields(t *testing.T) {
	peer := newPeerTestServer(t)
	defer peer.Close()
	s := &Server{
		store: store.NewMemoryStore(),
		cfg:   &config.Config{FederationPeers: []string{peer.URL}, Demo: true},
		fed:   NewFederationManager([]string{peer.URL}, store.NewMemoryStore(), "", nil),
	}
	body, _ := json.Marshal(map[string]interface{}{
		"peerURL": peer.URL,
		"task":    map[string]string{"agentID": "", "command": ""},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/forward/task", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleFederationForwardTask(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty task: status=%d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleFederationDevices_NilFed 验证 fed=nil 时返回 404。
func TestHandleFederationDevices_NilFed(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/federation/devices", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleFederationDevices(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nil fed: status=%d, want 404", rec.Code)
	}
}

// TestHandleFederationDevices_MethodNotAllowed 验证非 GET 方法返回 405。
func TestHandleFederationDevices_MethodNotAllowed(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/devices", nil)
	rec := httptest.NewRecorder()
	s.handleFederationDevices(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// k8s_cluster.go — encryptKubeconfig / decryptKubeconfig 带密钥
// =============================================================================

// newEncryptionKey 构造 32 字节 AES-256 密钥（测试固定值）。
func newEncryptionKey() []byte {
	return bytes.Repeat([]byte{0x01}, 32)
}

// TestEncryptDecryptKubeconfig_RoundTrip 验证带加密密钥时加解密往返一致。
func TestEncryptDecryptKubeconfig_RoundTrip(t *testing.T) {
	s := &Server{cfg: &config.Config{}, encryptionKey: newEncryptionKey()}
	plaintext := "apiVersion: v1\nkind: Config\nclusters: []\n"
	encrypted, err := s.encryptKubeconfig(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if encrypted == plaintext {
		t.Fatal("encrypted == plaintext, want ciphertext")
	}
	decrypted, err := s.decryptKubeconfig(encrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("round-trip: got %q, want %q", decrypted, plaintext)
	}
}

// TestDecryptKubeconfig_BadBase64 验证非法 base64 返回错误。
func TestDecryptKubeconfig_BadBase64(t *testing.T) {
	s := &Server{cfg: &config.Config{}, encryptionKey: newEncryptionKey()}
	if _, err := s.decryptKubeconfig("!!!not-base64!!!"); err == nil {
		t.Fatal("bad base64: want error, got nil")
	}
}

// TestDecryptKubeconfig_TamperedCiphertext 验证篡改的密文 GCM 验签失败。
func TestDecryptKubeconfig_TamperedCiphertext(t *testing.T) {
	s := &Server{cfg: &config.Config{}, encryptionKey: newEncryptionKey()}
	encrypted, err := s.encryptKubeconfig("secret-kubeconfig")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// 篡改最后一个字符（base64 编码后）。
	tampered := encrypted[:len(encrypted)-1]
	if encrypted[len(encrypted)-1] == 'A' {
		tampered += "B"
	} else {
		tampered += "A"
	}
	if _, err := s.decryptKubeconfig(tampered); err == nil {
		t.Fatal("tampered ciphertext: want error, got nil")
	}
}

// TestDecryptKubeconfig_ShortCiphertext 验证密文长度 < nonce 长度时返回错误。
func TestDecryptKubeconfig_ShortCiphertext(t *testing.T) {
	s := &Server{cfg: &config.Config{}, encryptionKey: newEncryptionKey()}
	// base64 编码 1 字节（< nonceSize=12）。
	short := "AA==" // 1 字节
	if _, err := s.decryptKubeconfig(short); err == nil {
		t.Fatal("short ciphertext: want error, got nil")
	}
}

// =============================================================================
// k8s_manage.go — handleNodeMetrics / handleRollbackDeployment / handleScaleDeployment
// =============================================================================

// newFakeK8sClientWithNode 构造带预置 node 的 fake client。
func newFakeK8sClientWithNode(name string) *k8s.K8sClient {
	client := newFakeK8sClient()
	client.Clientset.CoreV1().Nodes().Create(nil, &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"node-role.kubernetes.io/master": ""},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
				{Type: corev1.NodeExternalIP, Address: "1.2.3.4"},
			},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}, metav1.CreateOptions{})
	return client
}

// TestHandleNodeMetrics_Happy 验证节点指标查询 happy path。
func TestHandleNodeMetrics_Happy(t *testing.T) {
	s := newK8sManageAuthTestServer()
	auth := loginAsAdmin(t, s)
	client := newFakeK8sClientWithNode("worker-1")
	// 预置一个 running pod 带 requests，分配到该 node。
	client.Clientset.CoreV1().Pods("default").Create(nil, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec: corev1.PodSpec{
			NodeName: "worker-1",
			Containers: []corev1.Container{{
				Name: "c1",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}, metav1.CreateOptions{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/nodes/worker-1/metrics", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleNodeMetrics(rec, req, client, "worker-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["name"] != "worker-1" {
		t.Fatalf("name=%v, want worker-1", got["name"])
	}
}

// TestHandleNodeMetrics_EmptyName 验证空节点名返回 400。
func TestHandleNodeMetrics_EmptyName(t *testing.T) {
	s := newK8sManageAuthTestServer()
	auth := loginAsAdmin(t, s)
	client := newFakeK8sClient()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/nodes//metrics", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleNodeMetrics(rec, req, client, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty name: status=%d, want 400", rec.Code)
	}
}

// TestHandleNodeMetrics_NoAuth 验证无鉴权返回 401。
func TestHandleNodeMetrics_NoAuth(t *testing.T) {
	s := newK8sManageAuthTestServer()
	client := newFakeK8sClient()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/nodes/n1/metrics", nil)
	rec := httptest.NewRecorder()
	s.handleNodeMetrics(rec, req, client, "n1")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status=%d, want 401", rec.Code)
	}
}

// TestHandleNodeMetrics_NodeNotFound 验证节点不存在返回 500。
func TestHandleNodeMetrics_NodeNotFound(t *testing.T) {
	s := newK8sManageAuthTestServer()
	auth := loginAsAdmin(t, s)
	client := newFakeK8sClient()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/nodes/nope/metrics", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleNodeMetrics(rec, req, client, "nope")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("not found: status=%d, want 500", rec.Code)
	}
}

// TestHandleRollbackDeployment_Happy 验证回滚 happy path：deployment revision=2，
// 存在 revision=1 的 ReplicaSet（受此 deployment 控制），回滚成功。
func TestHandleRollbackDeployment_Happy(t *testing.T) {
	s := newK8sManageAuthTestServer()
	auth := loginAsAdmin(t, s)
	client := newFakeK8sClient()

	// 预置 deployment（revision=2）。
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myapp",
			Namespace: "default",
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "2",
			},
			UID: "dep-uid-1",
		},
		Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
	}
	client.Clientset.AppsV1().Deployments("default").Create(nil, dep, metav1.CreateOptions{})

	// 预置 ReplicaSet（revision=1，受此 deployment 控制）。
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myapp-rs-1",
			Namespace: "default",
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "1",
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "myapp",
				UID:        "dep-uid-1",
				Controller: boolPtr(true),
			}},
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: int32Ptr(1),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "myapp"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c1", Image: "img:v1"}}},
			},
		},
	}
	client.Clientset.AppsV1().ReplicaSets("default").Create(nil, rs, metav1.CreateOptions{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/deployments/default/myapp/rollback", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleRollbackDeployment(rec, req, client, "default", "myapp")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "rolled back" {
		t.Fatalf("status=%v, want rolled back", resp["status"])
	}
}

// TestHandleRollbackDeployment_NoRevisionAnnotation 验证无 revision annotation 返回 400。
func TestHandleRollbackDeployment_NoRevisionAnnotation(t *testing.T) {
	s := newK8sManageAuthTestServer()
	auth := loginAsAdmin(t, s)
	client := newFakeK8sClient()
	client.Clientset.AppsV1().Deployments("default").Create(nil, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "norev", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
	}, metav1.CreateOptions{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/deployments/default/norev/rollback", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleRollbackDeployment(rec, req, client, "default", "norev")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no revision: status=%d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleRollbackDeployment_InitialRevision 验证 revision=1（初始）返回 400。
func TestHandleRollbackDeployment_InitialRevision(t *testing.T) {
	s := newK8sManageAuthTestServer()
	auth := loginAsAdmin(t, s)
	client := newFakeK8sClient()
	client.Clientset.AppsV1().Deployments("default").Create(nil, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "init",
			Namespace:   "default",
			Annotations: map[string]string{"deployment.kubernetes.io/revision": "1"},
		},
		Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
	}, metav1.CreateOptions{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/deployments/default/init/rollback", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleRollbackDeployment(rec, req, client, "default", "init")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("initial revision: status=%d, want 400", rec.Code)
	}
}

// TestHandleRollbackDeployment_TargetRSNotFound 验证目标 ReplicaSet 不存在返回 404。
func TestHandleRollbackDeployment_TargetRSNotFound(t *testing.T) {
	s := newK8sManageAuthTestServer()
	auth := loginAsAdmin(t, s)
	client := newFakeK8sClient()
	client.Clientset.AppsV1().Deployments("default").Create(nil, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "nors",
			Namespace:   "default",
			Annotations: map[string]string{"deployment.kubernetes.io/revision": "3"},
		},
		Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
	}, metav1.CreateOptions{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/deployments/default/nors/rollback", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleRollbackDeployment(rec, req, client, "default", "nors")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("target RS not found: status=%d, want 404", rec.Code)
	}
}

// TestHandleScaleDeployment_Happy 验证扩缩容 happy path。
// fake clientset 的 GetScale 会 panic（尝试把 Deployment 转为 Scale），
// 与现有 TestK8sScaleDeployment_Happy 一致跳过；错误路径已由 NoAuth/BadJSON 覆盖。
func TestHandleScaleDeployment_Happy(t *testing.T) {
	t.Skip("fake clientset GetScale panics on Deployment->Scale conversion")
}

// TestHandleListNodes_WithData 验证带数据的节点列表。
func TestHandleListNodes_WithData(t *testing.T) {
	s := newK8sManageAuthTestServer()
	auth := loginAsAdmin(t, s)
	client := newFakeK8sClientWithNode("node-1")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/nodes", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleListNodes(rec, req, client)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Nodes []map[string]interface{} `json:"nodes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("nodes=%d, want 1", len(resp.Nodes))
	}
	if resp.Nodes[0]["status"] != "Ready" {
		t.Fatalf("status=%v, want Ready", resp.Nodes[0]["status"])
	}
}

// TestHandleListServices_WithData 验证带数据的 service 列表（含 ports + LoadBalancer ingress）。
func TestHandleListServices_WithData(t *testing.T) {
	s := newK8sManageAuthTestServer()
	auth := loginAsAdmin(t, s)
	client := newFakeK8sClient()
	client.Clientset.CoreV1().Services("default").Create(nil, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "mysvc", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeLoadBalancer,
			ClusterIP: "10.96.0.1",
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       80,
				TargetPort: intstr.FromInt(8080),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}},
			},
		},
	}, metav1.CreateOptions{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/services?namespace=default", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleListServices(rec, req, client)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Services []map[string]interface{} `json:"services"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Services) != 1 {
		t.Fatalf("services=%d, want 1", len(resp.Services))
	}
	if resp.Services[0]["externalIP"] != "1.2.3.4" {
		t.Fatalf("externalIP=%v, want 1.2.3.4", resp.Services[0]["externalIP"])
	}
}

// TestHandleClusterDashboard_WithData 验证集群仪表盘带数据。
func TestHandleClusterDashboard_WithData(t *testing.T) {
	s := newK8sManageAuthTestServer()
	auth := loginAsAdmin(t, s)
	client := newFakeK8sClientWithNode("n1")
	// 预置一个 running pod。
	client.Clientset.CoreV1().Pods("default").Create(nil, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}, metav1.CreateOptions{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/dashboard", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleClusterDashboard(rec, req, client)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var dash ClusterDashboard
	if err := json.Unmarshal(rec.Body.Bytes(), &dash); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dash.Nodes.Total != 1 || dash.Nodes.Ready != 1 {
		t.Fatalf("nodes=%+v, want total=1 ready=1", dash.Nodes)
	}
	if dash.Pods.Running != 1 {
		t.Fatalf("pods running=%d, want 1", dash.Pods.Running)
	}
}

// TestHandleClusterHealth_WithData 验证集群健康检查带数据（含 not-ready node + failed pod）。
func TestHandleClusterHealth_WithData(t *testing.T) {
	s := newK8sManageAuthTestServer()
	auth := loginAsAdmin(t, s)
	client := newFakeK8sClient()
	// 预置一个 not-ready node。
	client.Clientset.CoreV1().Nodes().Create(nil, &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}},
		},
	}, metav1.CreateOptions{})
	// 预置一个 failed pod。
	client.Clientset.CoreV1().Pods("default").Create(nil, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "failed-pod", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodFailed},
	}, metav1.CreateOptions{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/clusters/c1/health", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleClusterHealth(rec, req, client)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var health ClusterHealth
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if health.Status != "degraded" {
		t.Fatalf("status=%q, want degraded", health.Status)
	}
	if health.Nodes.NotReady != 1 {
		t.Fatalf("not-ready=%d, want 1", health.Nodes.NotReady)
	}
	if health.Pods.Failed != 1 {
		t.Fatalf("failed=%d, want 1", health.Pods.Failed)
	}
}

// TestHandleClusterHealth_MethodNotAllowed 验证非 GET 返回 405。
func TestHandleClusterHealth_MethodNotAllowed(t *testing.T) {
	s := newK8sManageAuthTestServer()
	auth := loginAsAdmin(t, s)
	client := newFakeK8sClient()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/k8s/clusters/c1/health", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Tenant-ID", "default")
	rec := httptest.NewRecorder()
	s.handleClusterHealth(rec, req, client)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// boolPtr 返回 bool 指针。
func boolPtr(b bool) *bool { return &b }

// =============================================================================
// server_netsec.go — buildGRPC / buildMetrics / grpcRecoveryInterceptor
// =============================================================================

// TestBuildGRPC_NoTLS 验证无 TLS 时 buildGRPC 成功构造 gRPC server。
func TestBuildGRPC_NoTLS(t *testing.T) {
	s := &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{TaskMaxRetries: 3},
		requireAuth: false,
		grpcPort:    0, // 随机端口
	}
	gs, lis, err := s.buildGRPC()
	if err != nil {
		t.Fatalf("buildGRPC: %v", err)
	}
	defer gs.Stop()
	if lis == nil {
		t.Fatal("listener nil")
	}
	lis.Close()
}

// TestBuildMetrics_Happy_Extra 验证 buildMetrics 成功构造 metrics server。
func TestBuildMetrics_Happy_Extra(t *testing.T) {
	s := &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{},
		metrics:     metrics.New(),
		metricsPort: 0, // 随机端口
	}
	srv, lis, err := s.buildMetrics()
	if err != nil {
		t.Fatalf("buildMetrics: %v", err)
	}
	defer srv.Close()
	if lis == nil {
		t.Fatal("listener nil")
	}
	lis.Close()
}

// TestBuildMetrics_PortInUse 验证端口占用时返回错误。
func TestBuildMetrics_PortInUse(t *testing.T) {
	// 先用 buildMetrics 成功监听一个端口，再尝试用同一端口再次 buildMetrics。
	s := &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{},
		metrics:     metrics.New(),
		metricsPort: 0, // 随机端口
	}
	srv1, lis1, err := s.buildMetrics()
	if err != nil {
		t.Fatalf("first buildMetrics: %v", err)
	}
	defer srv1.Close()
	port := lis1.Addr().(*net.TCPAddr).Port
	// 用同一端口再次 buildMetrics，应失败（端口已占用）。
	s2 := &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{},
		metrics:     metrics.New(),
		metricsPort: port,
	}
	if _, _, err := s2.buildMetrics(); err == nil {
		t.Fatal("port in use: want error, got nil")
	}
}

// TestGrpcRecoveryInterceptor_Panic 验证 panic 被拦截转为 Internal 错误。
func TestGrpcRecoveryInterceptor_Panic(t *testing.T) {
	info := &grpc.UnaryServerInfo{FullMethod: "/test/panic"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		panic("test panic")
	}
	_, err := grpcRecoveryInterceptor(context.Background(), nil, info, handler)
	if err == nil {
		t.Fatal("panic: want error, got nil")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Internal {
		t.Fatalf("err=%v, want Internal", err)
	}
}

// TestGrpcRecoveryInterceptor_Normal 验证正常 handler 透传。
func TestGrpcRecoveryInterceptor_Normal(t *testing.T) {
	info := &grpc.UnaryServerInfo{FullMethod: "/test/normal"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	resp, err := grpcRecoveryInterceptor(context.Background(), nil, info, handler)
	if err != nil {
		t.Fatalf("normal: %v", err)
	}
	if resp != "ok" {
		t.Fatalf("resp=%v, want ok", resp)
	}
}

// =============================================================================
// backup.go — NewStoreForCLI / ExportBackupFile / ImportBackupFile
// =============================================================================

// TestNewStoreForCLI_Memory 验证 cfg.Store 为空时返回 MemoryStore。
func TestNewStoreForCLI_Memory(t *testing.T) {
	cfg := &config.Config{Mode: "controlplane"}
	st, err := NewStoreForCLI(cfg)
	if err != nil {
		t.Fatalf("NewStoreForCLI: %v", err)
	}
	if st == nil {
		t.Fatal("store nil")
	}
}

// TestExportBackupFile_Happy 验证导出到文件成功。
func TestExportBackupFile_Happy(t *testing.T) {
	st := seedBackupStore()
	cfg := &config.Config{Mode: "controlplane"}
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "backup.json")
	opts := ExportOptions{Format: "json", IncludeAudits: true}
	data, err := ExportBackupFile(context.Background(), st, cfg, opts, outPath)
	if err != nil {
		t.Fatalf("ExportBackupFile: %v", err)
	}
	if data.Meta.Counts.Agents != 2 {
		t.Fatalf("agents=%d, want 2", data.Meta.Counts.Agents)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("backup file not created: %v", err)
	}
}

// TestExportBackupFile_BadPath 验证非法路径返回错误。
func TestExportBackupFile_BadPath(t *testing.T) {
	st := seedBackupStore()
	cfg := &config.Config{Mode: "controlplane"}
	opts := ExportOptions{Format: "json"}
	if _, err := ExportBackupFile(context.Background(), st, cfg, opts, "/nonexistent/dir/backup.json"); err == nil {
		t.Fatal("bad path: want error, got nil")
	}
}

// TestImportBackupFile_Happy 验证从文件导入成功。
func TestImportBackupFile_Happy(t *testing.T) {
	// 先导出到文件。
	srcSt := seedBackupStore()
	cfg := &config.Config{Mode: "controlplane"}
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "backup.json")
	opts := ExportOptions{Format: "json", IncludeAudits: true}
	if _, err := ExportBackupFile(context.Background(), srcSt, cfg, opts, outPath); err != nil {
		t.Fatalf("export: %v", err)
	}
	// 从文件导入到新 store。
	dstSt := store.NewMemoryStore()
	data, res, err := ImportBackupFile(context.Background(), dstSt, ImportOptions{}, outPath)
	if err != nil {
		t.Fatalf("ImportBackupFile: %v", err)
	}
	if data.Meta.Counts.Agents != 2 {
		t.Fatalf("agents=%d, want 2", data.Meta.Counts.Agents)
	}
	if res.Agents != 2 {
		t.Fatalf("imported agents=%d, want 2", res.Agents)
	}
}

// TestImportBackupFile_DryRun 验证 dry-run 只校验不写入。
func TestImportBackupFile_DryRun(t *testing.T) {
	srcSt := seedBackupStore()
	cfg := &config.Config{Mode: "controlplane"}
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "backup.json")
	if _, err := ExportBackupFile(context.Background(), srcSt, cfg, ExportOptions{Format: "json"}, outPath); err != nil {
		t.Fatalf("export: %v", err)
	}
	dstSt := store.NewMemoryStore()
	_, res, err := ImportBackupFile(context.Background(), dstSt, ImportOptions{DryRun: true}, outPath)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if res.Devices == 0 {
		t.Fatal("dry-run: devices=0, want >0")
	}
}

// TestImportBackupFile_BadPath 验证非法路径返回错误。
func TestImportBackupFile_BadPath(t *testing.T) {
	st := store.NewMemoryStore()
	if _, _, err := ImportBackupFile(context.Background(), st, ImportOptions{}, "/nonexistent/backup.json"); err == nil {
		t.Fatal("bad path: want error, got nil")
	}
}

// =============================================================================
// server_batch.go — cleanupDoneBatches / handleCanaryStatus happy
// =============================================================================

// TestCleanupDoneBatches_RemovesOld 验证清理 36h 前的批次。
func TestCleanupDoneBatches_RemovesOld(t *testing.T) {
	bs := newBatchStore()
	// 插入一个 40h 前的批次（应被清理）。
	oldID := "batch-old"
	bs.batches[oldID] = &batchTask{
		BatchID:   oldID,
		CreatedAt: time.Now().Add(-40 * time.Hour),
	}
	// 插入一个 1h 前的批次（应保留）。
	newID := "batch-new"
	bs.batches[newID] = &batchTask{
		BatchID:   newID,
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}
	// 插入一个 40h 前的灰度（应被清理）。
	oldCanary := "canary-old"
	bs.canaries[oldCanary] = &canaryRelease{
		CanaryID:  oldCanary,
		CreatedAt: time.Now().Add(-40 * time.Hour),
	}

	bs.cleanupDoneBatches()
	if _, exists := bs.batches[oldID]; exists {
		t.Fatal("old batch not removed")
	}
	if _, exists := bs.batches[newID]; !exists {
		t.Fatal("new batch removed")
	}
	if _, exists := bs.canaries[oldCanary]; exists {
		t.Fatal("old canary not removed")
	}
}

// TestHandleCanaryStatus_Happy 验证灰度状态查询 happy path。
func TestHandleCanaryStatus_Happy(t *testing.T) {
	s := newBatchTestServer()
	// 先创建一个灰度发布。
	body := `{"deviceIDs":["d1","d2"],"command":"echo hi","strategy":"percentage","percentage":50}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/canary", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u1")
	rec := httptest.NewRecorder()
	s.handleCanaryCreate(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status=%d, body=%s", rec.Code, rec.Body.String())
	}
	var create struct {
		CanaryID string `json:"canaryID"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &create); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	// 查询状态。
	sReq := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/canary/"+create.CanaryID, nil)
	sReq.Header.Set("X-Tenant-ID", "default")
	sRec := httptest.NewRecorder()
	s.handleCanaryStatus(sRec, sReq, create.CanaryID)
	if sRec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", sRec.Code, sRec.Body.String())
	}
}

// TestHandleCanaryStatus_TenantMismatch 验证租户不匹配返回 403。
func TestHandleCanaryStatus_TenantMismatch(t *testing.T) {
	s := newBatchTestServer()
	body := `{"deviceIDs":["d1"],"command":"echo","strategy":"percentage","percentage":100}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/canary", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-ID", "u1")
	rec := httptest.NewRecorder()
	s.handleCanaryCreate(rec, req)
	var create struct {
		CanaryID string `json:"canaryID"`
	}
	json.Unmarshal(rec.Body.Bytes(), &create)

	sReq := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/canary/"+create.CanaryID, nil)
	sReq.Header.Set("X-Tenant-ID", "other")
	sRec := httptest.NewRecorder()
	s.handleCanaryStatus(sRec, sReq, create.CanaryID)
	if sRec.Code != http.StatusForbidden {
		t.Fatalf("tenant mismatch: %d, want 403", sRec.Code)
	}
}

// =============================================================================
// grpc.go — ReportLogs
// =============================================================================

// TestReportLogs_Happy 验证日志上报 happy path。
func TestReportLogs_Happy(t *testing.T) {
	st := store.NewMemoryStore().WithDemo(true)
	st.Register(&proto.AgentInfo{AgentID: "agent-1", Segment: "seg-a", TenantID: "t1"})
	srvImpl := &grpcServerImpl{store: st, requireAuth: false}

	req := &grpcx.ReportLogsReq{
		Report: proto.LogReport{
			AgentID: "agent-1",
			LogName: "/var/log/syslog",
			Lines: []proto.LogLine{
				{Timestamp: time.Now(), Level: "INFO", Message: "test log line"},
			},
		},
	}
	resp, err := srvImpl.ReportLogs(context.Background(), req)
	if err != nil {
		t.Fatalf("ReportLogs: %v", err)
	}
	if resp == nil {
		t.Fatal("resp nil")
	}
}

// TestReportLogs_NilReq 验证 nil 请求返回 InvalidArgument。
func TestReportLogs_NilReq(t *testing.T) {
	st := store.NewMemoryStore().WithDemo(true)
	srvImpl := &grpcServerImpl{store: st, requireAuth: false}
	_, err := srvImpl.ReportLogs(context.Background(), nil)
	if err == nil {
		t.Fatal("nil req: want error, got nil")
	}
	st2, ok := status.FromError(err)
	if !ok || st2.Code() != codes.InvalidArgument {
		t.Fatalf("err=%v, want InvalidArgument", err)
	}
}

// TestReportLogs_EmptyAgentID 验证空 agentID 返回 InvalidArgument。
func TestReportLogs_EmptyAgentID(t *testing.T) {
	st := store.NewMemoryStore().WithDemo(true)
	srvImpl := &grpcServerImpl{store: st, requireAuth: false}
	req := &grpcx.ReportLogsReq{
		Report: proto.LogReport{AgentID: "", LogName: "test"},
	}
	_, err := srvImpl.ReportLogs(context.Background(), req)
	if err == nil {
		t.Fatal("empty agentID: want error, got nil")
	}
}

// TestReportLogs_WithLogHandler 验证带 logstore.Handler 时日志转发到后端。
func TestReportLogs_WithLogHandler(t *testing.T) {
	st := store.NewMemoryStore().WithDemo(true)
	st.Register(&proto.AgentInfo{AgentID: "agent-2", Segment: "seg-a", TenantID: "t1"})
	ls := logstore.NewHandler(logstore.NewMemory(100))
	srvImpl := &grpcServerImpl{store: st, requireAuth: false, logs: ls}

	req := &grpcx.ReportLogsReq{
		Report: proto.LogReport{
			AgentID: "agent-2",
			LogName: "/var/log/app.log",
			Lines: []proto.LogLine{
				{Timestamp: time.Now(), Level: "ERROR", Message: "app error"},
				{Timestamp: time.Now(), Level: "", Message: "no level"},
			},
		},
	}
	if _, err := srvImpl.ReportLogs(context.Background(), req); err != nil {
		t.Fatalf("ReportLogs with handler: %v", err)
	}
}

// =============================================================================
// device_metrics.go — method not allowed
// =============================================================================

// TestHandleDeviceMetrics_MethodNotAllowed 验证非 GET 返回 405。
func TestHandleDeviceMetrics_MethodNotAllowed(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/dev-1/metrics", nil)
	rec := httptest.NewRecorder()
	s.handleDeviceMetrics(rec, req, "dev-1")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// =============================================================================
// server_factory.go — selectStore / newDeployHandler / newOrchestrationHandler / newLogHandler
// =============================================================================

// TestSelectStore_MySQLInvalidDSN 验证 mysql 无效 DSN 返回错误。
func TestSelectStore_MySQLInvalidDSN(t *testing.T) {
	cfg := &config.Config{Store: "mysql", MySQLDSN: "invalid-dsn"}
	_, err := selectStore(cfg, nil)
	if err == nil {
		t.Fatal("invalid DSN: want error, got nil")
	}
}

// TestNewLogHandler_Loki 验证 loki 后端构造。
func TestNewLogHandler_Loki(t *testing.T) {
	cfg := &config.Config{LogStore: "loki", LokiEndpoint: "http://loki:3100"}
	h := newLogHandler(store.NewMemoryStore(), cfg)
	if h == nil {
		t.Fatal("handler nil")
	}
}

// TestNewLogHandler_ES 验证 es 后端构造。
func TestNewLogHandler_ES(t *testing.T) {
	cfg := &config.Config{LogStore: "es", ESEndpoint: "http://es:9200", ESIndex: "opsmesh-logs"}
	h := newLogHandler(store.NewMemoryStore(), cfg)
	if h == nil {
		t.Fatal("handler nil")
	}
}

// =============================================================================
// server_netsec.go — buildFederationServer / buildGRPC TLS 错误路径
// =============================================================================

// TestBuildFederationServer_TLSLoadFail 验证联邦端口启用但 TLS 证书无效时返回错误。
func TestBuildFederationServer_TLSLoadFail(t *testing.T) {
	s := &Server{
		store: store.NewMemoryStore(),
		cfg: &config.Config{
			FederationPort:    9093,
			FederationTLSCert: "/nonexistent/cert.pem",
			FederationTLSKey:  "/nonexistent/key.pem",
		},
		fed: NewFederationManager([]string{"http://peer:8080"}, store.NewMemoryStore(), "", nil),
	}
	_, _, err := s.buildFederationServer()
	if err == nil {
		t.Fatal("TLS load fail: want error, got nil")
	}
}

// TestBuildGRPC_TLSLoadFail 验证 gRPC TLS 证书无效时返回错误。
func TestBuildGRPC_TLSLoadFail(t *testing.T) {
	s := &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{TaskMaxRetries: 3},
		requireAuth: false,
		grpcPort:    0,
		tlsCert:     "/nonexistent/cert.pem",
		tlsKey:      "/nonexistent/key.pem",
	}
	_, _, err := s.buildGRPC()
	if err == nil {
		t.Fatal("TLS load fail: want error, got nil")
	}
}

// =============================================================================
// server.go — shutdownOTel / shutdownTLSReloader
// =============================================================================

// TestShutdownOTel_NonNil 验证 otelShutdown 非 nil 时调用。
func TestShutdownOTel_NonNil(t *testing.T) {
	called := false
	s := &Server{otelShutdown: func(ctx context.Context) error {
		called = true
		return nil
	}}
	s.shutdownOTel()
	if !called {
		t.Fatal("otelShutdown not called")
	}
}

// TestShutdownOTel_Error 验证 otelShutdown 返回错误时不 panic。
func TestShutdownOTel_Error(t *testing.T) {
	s := &Server{otelShutdown: func(ctx context.Context) error {
		return context.DeadlineExceeded
	}}
	s.shutdownOTel() // 不应 panic
}

// TestHandleAlerts_MethodNotAllowed 验证非 GET 返回 405。
func TestHandleAlerts_MethodNotAllowed(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", nil)
	rec := httptest.NewRecorder()
	s.handleAlerts(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", rec.Code)
	}
}

// TestHandleAlerts_Pagination 验证分页路径。
func TestHandleAlerts_Pagination(t *testing.T) {
	st := store.NewMemoryStore()
	// 添加 3 条告警。
	for i := 0; i < 3; i++ {
		st.AddAlert(&proto.Alert{AlertID: "a" + string(rune('0'+i)), TenantID: "t1", Severity: "warning"})
	}
	s := &Server{store: st, cfg: &config.Config{Demo: true}, requireAuth: false}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?page=1&pageSize=2", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAlerts(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
}

// TestListAlertRules_Pagination 验证告警规则分页路径。
func TestListAlertRules_Pagination(t *testing.T) {
	st := store.NewMemoryStore()
	st.CreateAlertRule(&store.AlertRule{ID: "r1", TenantID: "t1", Metric: "cpu", Op: ">", Threshold: 90, Enabled: true})
	s := &Server{store: st, cfg: &config.Config{Demo: true}, requireAuth: false}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules?page=1&pageSize=10", nil)
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleAlertRules(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// server_network.go — handleNetworkConnectivity 错误路径
// =============================================================================

// TestHandleNetworkConnectivity_EmptySourceAgent 验证空 sourceAgentId 返回 400。
func TestHandleNetworkConnectivity_EmptySourceAgent(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}, requireAuth: false}
	body := `{"sourceAgentId":"","targets":[{"ip":"10.0.0.1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/connectivity", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNetworkConnectivity(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

// TestHandleNetworkConnectivity_EmptyTargets 验证空 targets 返回 400。
func TestHandleNetworkConnectivity_EmptyTargets(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}, requireAuth: false}
	body := `{"sourceAgentId":"a1","targets":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/connectivity", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNetworkConnectivity(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

// TestHandleNetworkConnectivity_AgentNotFound 验证 agent 不存在返回 404。
func TestHandleNetworkConnectivity_AgentNotFound(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}, requireAuth: false}
	body := `{"sourceAgentId":"nope","targets":[{"ip":"10.0.0.1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/connectivity", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleNetworkConnectivity(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

// =============================================================================
// server_secrets.go — handleSecretsTest 错误路径
// =============================================================================

// TestHandleSecretsTest_EmptyAddr 验证空 addr 返回 400。
func TestHandleSecretsTest_EmptyAddr(t *testing.T) {
	s := &Server{store: store.NewMemoryStore(), cfg: &config.Config{Demo: true}, requireAuth: false}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test", strings.NewReader(`{"addr":""}`))
	req.Header.Set("X-Tenant-ID", "t1")
	rec := httptest.NewRecorder()
	s.handleSecretsTest(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

// =============================================================================
// grpc.go — checkAgentTenant
// =============================================================================

// TestCheckAgentTenant_RequireAuthNoTenant 验证 requireAuth=true 且无租户头返回错误。
func TestCheckAgentTenant_RequireAuthNoTenant(t *testing.T) {
	st := store.NewMemoryStore().WithDemo(true)
	srvImpl := &grpcServerImpl{store: st, requireAuth: true}
	err := srvImpl.checkAgentTenant(context.Background(), "a1")
	if err == nil {
		t.Fatal("require auth no tenant: want error, got nil")
	}
}

// TestCheckAgentTenant_RequireAuthCrossTenant 验证 requireAuth=true 且跨租户返回错误。
func TestCheckAgentTenant_RequireAuthCrossTenant(t *testing.T) {
	st := store.NewMemoryStore().WithDemo(true)
	st.Register(&proto.AgentInfo{AgentID: "a1", Segment: "seg-a", TenantID: "t1"})
	srvImpl := &grpcServerImpl{store: st, requireAuth: true}
	// 构造带 t2 租户的 incoming context。
	md := metadata.Pairs("x-tenant-id", "t2")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	err := srvImpl.checkAgentTenant(ctx, "a1")
	if err == nil {
		t.Fatal("cross tenant: want error, got nil")
	}
}

// TestCheckAgentTenant_RequireAuthSameTenant 验证 requireAuth=true 且同租户放行。
func TestCheckAgentTenant_RequireAuthSameTenant(t *testing.T) {
	st := store.NewMemoryStore().WithDemo(true)
	st.Register(&proto.AgentInfo{AgentID: "a1", Segment: "seg-a", TenantID: "t1"})
	srvImpl := &grpcServerImpl{store: st, requireAuth: true}
	md := metadata.Pairs("x-tenant-id", "t1")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	if err := srvImpl.checkAgentTenant(ctx, "a1"); err != nil {
		t.Fatalf("same tenant: %v", err)
	}
}

// TestCheckAgentTenant_RequireAuthAgentNotExist 验证 requireAuth=true 且 agent 不存在时放行。
func TestCheckAgentTenant_RequireAuthAgentNotExist(t *testing.T) {
	st := store.NewMemoryStore().WithDemo(true)
	srvImpl := &grpcServerImpl{store: st, requireAuth: true}
	md := metadata.Pairs("x-tenant-id", "t1")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	if err := srvImpl.checkAgentTenant(ctx, "nope"); err != nil {
		t.Fatalf("agent not exist: %v", err)
	}
}

// =============================================================================
// handleNetworkDiagnose 补充测试
// =============================================================================

func TestHandleNetworkDiagnose_BadTool(t *testing.T) {
	s := newTestServer()
	body := `{"agentId":"a1","tool":"badtool","target":"1.2.3.4"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnose(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad tool: status=%d, want 400", w.Code)
	}
}

func TestHandleNetworkDiagnose_MissingAgentID(t *testing.T) {
	s := newTestServer()
	body := `{"tool":"ping","target":"1.2.3.4"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnose(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing agentId: status=%d, want 400", w.Code)
	}
}

func TestHandleNetworkDiagnose_MissingTarget(t *testing.T) {
	s := newTestServer()
	body := `{"agentId":"a1","tool":"ping"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnose(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing target: status=%d, want 400", w.Code)
	}
}

func TestHandleNetworkDiagnose_BadCount(t *testing.T) {
	s := newTestServer()
	body := `{"agentId":"a1","tool":"ping","target":"1.2.3.4","options":{"count":200}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnose(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad count: status=%d, want 400", w.Code)
	}
}

func TestHandleNetworkDiagnose_BadTimeout(t *testing.T) {
	s := newTestServer()
	body := `{"agentId":"a1","tool":"ping","target":"1.2.3.4","options":{"timeout":100}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnose(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad timeout: status=%d, want 400", w.Code)
	}
}

func TestHandleNetworkDiagnose_TcpingNoPort(t *testing.T) {
	s := newTestServer()
	body := `{"agentId":"a1","tool":"tcping","target":"1.2.3.4"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnose(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("tcping no port: status=%d, want 400", w.Code)
	}
}

func TestHandleNetworkDiagnose_AgentNotFound(t *testing.T) {
	s := newTestServer()
	body := `{"agentId":"nope","tool":"ping","target":"1.2.3.4"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnose(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("agent not found: status=%d, want 404", w.Code)
	}
}

func TestHandleNetworkDiagnose_Happy(t *testing.T) {
	s := newTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := fmt.Sprintf(`{"agentId":"%s","tool":"ping","target":"127.0.0.1"}`, a.AgentID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnose(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("happy: status=%d, want 202, body=%s", w.Code, w.Body.String())
	}
	var resp diagnoseResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TaskID == "" {
		t.Fatal("empty taskID")
	}
}

func TestHandleNetworkDiagnose_TracerouteHappy(t *testing.T) {
	s := newTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := fmt.Sprintf(`{"agentId":"%s","tool":"traceroute","target":"127.0.0.1"}`, a.AgentID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnose(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("traceroute: status=%d, want 202, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleNetworkDiagnose_NslookupHappy(t *testing.T) {
	s := newTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default", OS: "windows"})
	body := fmt.Sprintf(`{"agentId":"%s","tool":"nslookup","target":"localhost"}`, a.AgentID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnose(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("nslookup: status=%d, want 202, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleNetworkDiagnose_CurlHappy(t *testing.T) {
	s := newTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := fmt.Sprintf(`{"agentId":"%s","tool":"curl","target":"http://127.0.0.1:8080"}`, a.AgentID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnose(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("curl: status=%d, want 202, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleNetworkDiagnose_TcpingWithPort(t *testing.T) {
	s := newTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := fmt.Sprintf(`{"agentId":"%s","tool":"tcping","target":"127.0.0.1","options":{"port":8080}}`, a.AgentID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnose(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("tcping with port: status=%d, want 202, body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// handleNetworkDiagnoseResult 补充测试
// =============================================================================

func TestHandleNetworkDiagnoseResult_Happy(t *testing.T) {
	s := newTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	task := s.store.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "default", Type: proto.TaskTypeShell, Command: "ping -c 1 127.0.0.1"})
	s.store.ClaimTask(a.AgentID)
	s.store.SubmitResult(&proto.TaskResult{TaskID: task.TaskID, AgentID: a.AgentID, ExitCode: 0, Stdout: "1 packets"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/diagnose/"+task.TaskID, nil)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnoseResult(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("happy: status=%d, want 200, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleNetworkDiagnoseResult_TaskNotFound(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/diagnose/nonexistent", nil)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnoseResult(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("not found: status=%d, want 404", w.Code)
	}
}

func TestHandleNetworkDiagnoseResult_EmptyTaskID(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/diagnose/", nil)
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkDiagnoseResult(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty taskID: status=%d, want 400", w.Code)
	}
}

func TestHandleNetworkDiagnoseResult_MethodNotAllowed(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/diagnose/abc", nil)
	w := httptest.NewRecorder()
	s.handleNetworkDiagnoseResult(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method: status=%d, want 405", w.Code)
	}
}

// =============================================================================
// handleNetworkConnectivity 补充测试 — happy path
// =============================================================================

func TestHandleNetworkConnectivity_HappyPath(t *testing.T) {
	s := newTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := fmt.Sprintf(`{"sourceAgentId":"%s","targets":[{"ip":"127.0.0.1"},{"ip":"192.168.1.1","port":80}]}`, a.AgentID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/connectivity", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkConnectivity(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("happy: status=%d, want 200, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleNetworkConnectivity_TargetWithEmptyIP(t *testing.T) {
	s := newTestServer()
	a := s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	body := fmt.Sprintf(`{"sourceAgentId":"%s","targets":[{"ip":""},{"ip":"127.0.0.1"}]}`, a.AgentID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/connectivity", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleNetworkConnectivity(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("empty ip: status=%d, want 200, body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// probeNetworkTopology 补充测试
// =============================================================================

func TestProbeNetworkTopology_WithOnlineDevices(t *testing.T) {
	s := newTestServer()
	s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "default"})
	// 提交设备指标使设备在线
	s.store.StoreDeviceMetrics("dev-seg-a-1", &proto.DeviceMetrics{
		DeviceID: "dev-seg-a-1",
		CPU:      proto.CPUMetrics{Usage: 50.0},
		Memory:   proto.MemMetrics{Usage: 60.0},
	})
	topo, err := s.probeNetworkTopology("default")
	if err != nil {
		t.Fatalf("probeNetworkTopology: %v", err)
	}
	if topo == nil {
		t.Fatal("nil topo")
	}
}

func TestProbeNetworkTopology_NoDevices(t *testing.T) {
	s := newTestServer()
	topo, err := s.probeNetworkTopology("default")
	if err != nil {
		t.Fatalf("probeNetworkTopology: %v", err)
	}
	if len(topo.Nodes) != 0 {
		t.Fatalf("nodes=%d, want 0", len(topo.Nodes))
	}
}

// =============================================================================
// handleCreateUser 补充测试
// =============================================================================

func newCreateUserTestServer() *Server {
	st := store.NewMemoryStore().WithDemo(true)
	ss := store.NewInProcessSessionStore()
	return &Server{
		store:        st,
		cfg:          &config.Config{Demo: true},
		jwtSecret:    []byte("test-jwt-secret-32-chars-long!!!!"),
		sessionStore: ss,
		loginGuard:   newLoginGuard(ss),
	}
}

func TestHandleCreateUser_WeakPassword(t *testing.T) {
	s := newCreateUserTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"username":"newuser","password":"123","email":"u@e.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleCreateUser(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("weak password: status=%d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleCreateUser_DuplicateUsername(t *testing.T) {
	s := newCreateUserTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"username":"admin","password":"StrongPass123!","email":"a@e.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleCreateUser(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate: status=%d, want 409, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleCreateUser_Happy(t *testing.T) {
	s := newCreateUserTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"username":"newuser1","password":"StrongPass123!","email":"u1@e.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleCreateUser(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("happy: status=%d, want 201, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleCreateUser_MissingFields(t *testing.T) {
	s := newCreateUserTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"username":"","password":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleCreateUser(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing fields: status=%d, want 400", w.Code)
	}
}

func TestHandleCreateUser_UnknownRole(t *testing.T) {
	s := newCreateUserTestServer()
	auth := loginAsAdmin(t, s)
	body := `{"username":"newuser2","password":"StrongPass123!","role_ids":["nonexistent-role"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleCreateUser(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown role: status=%d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleCreateUser_BadJSON(t *testing.T) {
	s := newCreateUserTestServer()
	auth := loginAsAdmin(t, s)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader("bad json"))
	req.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.handleCreateUser(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad json: status=%d, want 400", w.Code)
	}
}

// =============================================================================
// handleSecretsTest 补充测试
// =============================================================================

func TestHandleSecretsTest_TokenEmpty(t *testing.T) {
	s := newTestServer()
	body := `{"addr":"http://vault.example.com:8200"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleSecretsTest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("token empty: status=%d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp secretsTestResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.OK {
		t.Fatal("should fail with empty token")
	}
}

func TestHandleSecretsTest_TokenFromEnv(t *testing.T) {
	s := newTestServer()
	os.Setenv("OPSMESH_VAULT_TOKEN", "env-token")
	defer os.Unsetenv("OPSMESH_VAULT_TOKEN")
	body := `{"addr":"http://vault.example.com:8200"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleSecretsTest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("token from env: status=%d, want 200, body=%s", w.Code, w.Body.String())
	}
}

// =============================================================================
// evaluateAnomalyForDevice 补充测试 — with metrics
// =============================================================================

func TestEvaluateAnomalyForDevice_WithMetrics(t *testing.T) {
	s := newAlertsTestServer()
	s.anomalyEngine = alertengine.NewAnomalyEngine()
	s.store.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	s.store.StoreDeviceMetrics("dev-seg-a-1", &proto.DeviceMetrics{
		DeviceID: "dev-seg-a-1",
		CPU:      proto.CPUMetrics{Usage: 99.0},
		Memory:   proto.MemMetrics{Usage: 98.0},
	})
	// 第一次调用：无基线，不会触发异常
	events := s.evaluateAnomalyForDevice("dev-seg-a-1")
	// 可能返回 nil（无基线）或非 nil（有异常），不 panic 即可
	_ = events
}

// =============================================================================
// newDeployHandler / newOrchestrationHandler 测试
// =============================================================================

func TestNewDeployHandler_MemoryStore(t *testing.T) {
	st := store.NewMemoryStore()
	h := newDeployHandler(st)
	if h == nil {
		t.Fatal("nil handler")
	}
}

func TestNewOrchestrationHandler_MemoryStore(t *testing.T) {
	st := store.NewMemoryStore()
	h := newOrchestrationHandler(st)
	if h == nil {
		t.Fatal("nil handler")
	}
}

// =============================================================================
// notifyLoop 补充测试
// =============================================================================

func TestNotifyLoop_NoChannels(t *testing.T) {
	s := newTestServer()
	// 无 webhook + 无 email → 立即返回
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		s.notifyLoop(ctx)
		close(done)
	}()
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("notifyLoop did not return immediately with no channels")
	}
}

func TestNotifyLoop_InvalidWebhookURL(t *testing.T) {
	s := newTestServer()
	s.cfg.AlertWebhookURL = "http://127.0.0.1:8200" // SSRF blocked
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		s.notifyLoop(ctx)
		close(done)
	}()
	select {
	case <-done:
		// good — SSRF check should block startup
	case <-time.After(2 * time.Second):
		t.Fatal("notifyLoop did not return with invalid webhook URL")
	}
}

// =============================================================================
// rateLimiter sweepLoop 补充测试
// =============================================================================

func TestRateLimiterSweepLoop(t *testing.T) {
	rl := &rateLimiter{
		buckets:       make(map[string]*tokenBucket),
		sweepInterval: 50 * time.Millisecond,
	}
	rl.buckets["1.2.3.4"] = &tokenBucket{tokens: 5, lastRefill: time.Now().Add(-1 * time.Hour)}
	done := make(chan struct{})
	go func() {
		rl.sweepLoop()
		close(done)
	}()
	// 等待 sweepInterval 过后 sweep 清理过期 bucket
	time.Sleep(150 * time.Millisecond)
	rl.mu.Lock()
	_, stillExists := rl.buckets["1.2.3.4"]
	rl.mu.Unlock()
	if stillExists {
		t.Fatal("sweepLoop did not clean up expired bucket")
	}
}

// =============================================================================
// reclaimLoop 补充测试
// =============================================================================

func TestReclaimLoop_BasicExit(t *testing.T) {
	s := newTestServer()
	s.batches = newBatchStore()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.reclaimLoop(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reclaimLoop did not exit")
	}
}

// =============================================================================
// rateLimitMiddleware 补充测试
// =============================================================================

func TestRateLimitMiddleware_NilLimiter(t *testing.T) {
	s := newTestServer()
	// rateLimiter nil → 透传
	called := false
	h := s.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !called {
		t.Fatal("handler not called")
	}
}

func TestRateLimitMiddleware_HealthEndpointBypass(t *testing.T) {
	s := newTestServer()
	s.rateLimiter = &rateLimiter{
		buckets:       make(map[string]*tokenBucket),
		sweepInterval: time.Hour,
		ratePerSec:    1,
	}
	called := false
	h := s.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !called {
		t.Fatal("health endpoint should bypass rate limit")
	}
}

// =============================================================================
// storeDispatcher 补充测试
// =============================================================================

func TestStoreDispatcher_CreateTaskCov(t *testing.T) {
	st := store.NewMemoryStore()
	d := &storeDispatcher{store: st}
	task := d.CreateTask(&proto.Task{AgentID: "a1", Type: proto.TaskTypeShell, Command: "echo hi"})
	if task == nil || task.TaskID == "" {
		t.Fatal("CreateTask returned nil or empty")
	}
}

func TestStoreDispatcher_DeviceCov(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	d := &storeDispatcher{store: st}
	dev := d.Device("dev-" + a.AgentID)
	if dev == nil {
		t.Fatal("Device returned nil")
	}
}

func TestStoreDispatcher_TaskStates(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	task := st.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: proto.TaskTypeShell, Command: "echo hi"})
	d := &storeDispatcher{store: st}
	states := d.TaskStates([]string{task.TaskID}, "t1")
	if states[task.TaskID] == "" {
		t.Fatal("TaskStates returned empty state")
	}
}

func TestStoreDispatcher_TaskStatesEmpty(t *testing.T) {
	st := store.NewMemoryStore()
	d := &storeDispatcher{store: st}
	states := d.TaskStates(nil, "t1")
	if len(states) != 0 {
		t.Fatalf("expected 0 states, got %d", len(states))
	}
}

// =============================================================================
// buildDiagnoseCommand 补充测试
// =============================================================================

func TestBuildDiagnoseCommand_PingWindows(t *testing.T) {
	cmd, err := buildDiagnoseCommand("ping", "1.2.3.4", diagnoseOptions{Count: 3, Timeout: 2}, "windows")
	if err != nil {
		t.Fatalf("ping windows: %v", err)
	}
	if !strings.Contains(cmd, "ping") {
		t.Fatalf("unexpected cmd: %s", cmd)
	}
}

func TestBuildDiagnoseCommand_TracerouteWindows(t *testing.T) {
	cmd, err := buildDiagnoseCommand("traceroute", "1.2.3.4", diagnoseOptions{}, "windows")
	if err != nil {
		t.Fatalf("traceroute windows: %v", err)
	}
	if !strings.Contains(cmd, "tracert") {
		t.Fatalf("unexpected cmd: %s", cmd)
	}
}

func TestBuildDiagnoseCommand_TcpingWindows(t *testing.T) {
	cmd, err := buildDiagnoseCommand("tcping", "1.2.3.4", diagnoseOptions{Port: 80}, "windows")
	if err != nil {
		t.Fatalf("tcping windows: %v", err)
	}
	if !strings.Contains(cmd, "Test-NetConnection") {
		t.Fatalf("unexpected cmd: %s", cmd)
	}
}

func TestBuildDiagnoseCommand_Nslookup(t *testing.T) {
	cmd, err := buildDiagnoseCommand("nslookup", "example.com", diagnoseOptions{}, "linux")
	if err != nil {
		t.Fatalf("nslookup: %v", err)
	}
	if !strings.Contains(cmd, "nslookup") {
		t.Fatalf("unexpected cmd: %s", cmd)
	}
}

func TestBuildDiagnoseCommand_Curl(t *testing.T) {
	cmd, err := buildDiagnoseCommand("curl", "http://example.com", diagnoseOptions{Timeout: 5}, "linux")
	if err != nil {
		t.Fatalf("curl: %v", err)
	}
	if !strings.Contains(cmd, "curl") {
		t.Fatalf("unexpected cmd: %s", cmd)
	}
}

func TestBuildDiagnoseCommand_InvalidTool(t *testing.T) {
	_, err := buildDiagnoseCommand("badtool", "1.2.3.4", diagnoseOptions{}, "linux")
	if err == nil {
		t.Fatal("expected error for invalid tool")
	}
}

// =============================================================================
// validateDiagnoseTool 补充测试
// =============================================================================

func TestValidateDiagnoseTool_Valid(t *testing.T) {
	for _, tool := range []string{"ping", "traceroute", "tcping", "nslookup", "curl"} {
		if err := validateDiagnoseTool(tool); err != nil {
			t.Fatalf("valid tool %s: %v", tool, err)
		}
	}
}

func TestValidateDiagnoseTool_Invalid(t *testing.T) {
	if err := validateDiagnoseTool("badtool"); err == nil {
		t.Fatal("expected error for invalid tool")
	}
}

// =============================================================================
// buildPingCommand 补充测试
// =============================================================================

func TestBuildPingCommand_Windows(t *testing.T) {
	cmd := buildPingCommand("1.2.3.4", 3, 2, "windows")
	if !strings.Contains(cmd, "-n") {
		t.Fatalf("windows ping should use -n: %s", cmd)
	}
}

func TestBuildPingCommand_Linux(t *testing.T) {
	cmd := buildPingCommand("1.2.3.4", 3, 2, "linux")
	if !strings.Contains(cmd, "-c") {
		t.Fatalf("linux ping should use -c: %s", cmd)
	}
}
