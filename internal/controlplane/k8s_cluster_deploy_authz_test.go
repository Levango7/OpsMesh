package controlplane

// k8s_cluster_deploy_authz_test.go — K8s 集群部署路由鉴权守卫。
//
// 背景（docs/opsmesh-authz-audit-2026-11-04.md 附带发现）：这两个路由原先
// **完全无鉴权**。POST /api/v1/k8s/clusters/deploy 的 body 带 NodeSpec
// （Host/Port/User/SSHKey/Password），executeDeploy→precheckNodes 会拿它
// sshPool.Get(...) 连任意主机执行 kubeadm 安装——匿名可调即「控制面被当成
// 任意主机的 SSH 安装代理」。GET status 的 deployID 可枚举且状态体含节点名。
//
// 判据：viewer 无 k8s:write/k8s:read ⇒ 403；operator 同样无（RolePermissions
// 把 k8s 归 operatorReadOnlyGroups，只给 read）；admin 两者皆有 ⇒ 放行。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

func newK8sDeployAuthzServer() *Server {
	return &Server{
		store:       store.NewMemoryStore(),
		cfg:         &config.Config{TaskMaxRetries: 1, Demo: false, TrustGatewayHeaders: true},
		requireAuth: false,
	}
}

func k8sDeployReq(roles, method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-User-Id", "audit-caller")
	if roles != "" {
		req.Header.Set("X-User-Roles", roles)
	}
	return req
}

const k8sDeploySpec = `{"clusterName":"audit-c","k8sVersion":"1.29.4",` +
	`"controlPlaneNodes":[{"id":"cp1","host":"127.0.0.1","port":22,"user":"root"}]}`

func TestK8sClusterDeploy_DeniesReadOnlyAndOperator(t *testing.T) {
	s := newK8sDeployAuthzServer()
	for _, role := range []string{"viewer", "operator"} {
		rec := httptest.NewRecorder()
		s.handleK8sClusterDeploy(rec, k8sDeployReq(role, http.MethodPost,
			"/api/v1/k8s/clusters/deploy", k8sDeploySpec))
		if rec.Code != http.StatusForbidden {
			t.Errorf("角色 %s 调 K8s 集群部署应 403，实得 %d：%s", role, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "k8s:write") {
			t.Errorf("角色 %s 的拒绝应点名 k8s:write，实得：%s", role, rec.Body.String())
		}
	}
}

func TestK8sClusterDeploy_DeniesAnonymous(t *testing.T) {
	s := newK8sDeployAuthzServer()
	rec := httptest.NewRecorder()
	s.handleK8sClusterDeploy(rec, k8sDeployReq("", http.MethodPost,
		"/api/v1/k8s/clusters/deploy", k8sDeploySpec))
	if rec.Code == http.StatusOK || rec.Code == http.StatusAccepted {
		t.Fatalf("匿名竟能启动 K8s 集群部署（%d）——SSH 代理面重开", rec.Code)
	}
}

func TestK8sClusterDeploy_AllowsAdmin(t *testing.T) {
	s := newK8sDeployAuthzServer()
	rec := httptest.NewRecorder()
	s.handleK8sClusterDeploy(rec, k8sDeployReq("admin", http.MethodPost,
		"/api/v1/k8s/clusters/deploy", k8sDeploySpec))
	if rec.Code == http.StatusForbidden {
		t.Fatalf("admin 持有 k8s:write 却被 403：%s", rec.Body.String())
	}
}

func TestK8sClusterDeployStatus_DeniesWithoutRead(t *testing.T) {
	s := newK8sDeployAuthzServer()
	for _, role := range []string{"", "viewer", "operator"} {
		rec := httptest.NewRecorder()
		s.handleK8sClusterDeployStatus(rec, k8sDeployReq(role, http.MethodGet,
			"/api/v1/k8s/clusters/deploy/status?deployID=audit-c-1", ""))
		if rec.Code == http.StatusForbidden && role != "" {
			t.Errorf("角色 %q 应读不到部署状态", role)
		}
		if rec.Code == http.StatusOK {
			t.Errorf("角色 %q 匿名/无权限竟读到部署状态（%d）", role, rec.Code)
		}
	}
}

func TestK8sClusterDeployStatus_AllowsViewerRead(t *testing.T) {
	// viewer 有 k8s:read（k8s 在 operatorReadOnlyGroups 中，viewer 派生全部 read）
	// ⇒ 状态查询放过，404 表示鉴权通过但无此部署记录，属正常业务响应。
	s := newK8sDeployAuthzServer()
	rec := httptest.NewRecorder()
	s.handleK8sClusterDeployStatus(rec, k8sDeployReq("viewer", http.MethodGet,
		"/api/v1/k8s/clusters/deploy/status?deployID=no-such", ""))
	if rec.Code == http.StatusForbidden {
		t.Fatalf("viewer 持有 k8s:read 却被 403：%s", rec.Body.String())
	}
}

// TestK8sClusterDeploy_NoAliasingWithInternalState 结构性守卫：Deploy/GetStatus/
// ListDeployments 交给调用方的必须是**深拷贝**，不能是 map 内部的指针。
//
// 为什么值得单独立例：这不是理论风险。CI 的 -race 实测抓到过 data race ——
// handleK8sClusterDeploy 读 status.Phase 与 updateStatus（后台 goroutine）写同一
// 字段。修法是快照，而「快照是否真的隔离」靠 -race 才能发现；本用例把这件
// 不变量固化成**不依赖竞态检测器**的判定，让没有 -race 的环境（如 Windows
// 开发机无 gcc）也能守住回归。
func TestK8sClusterDeploy_NoAliasingWithInternalState(t *testing.T) {
	d := NewK8sClusterDeployer()

	spec := K8sClusterDeploySpec{
		ClusterName:       "audit-c",
		ControlPlaneNodes: []NodeSpec{{ID: "cp1", Host: "127.0.0.1", Port: 22, User: "root"}},
	}
	got, err := d.Deploy(context.Background(), spec)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	// 改调用方手里的副本，再观察内部状态是否被污染。
	//
	// 铁律：**所有对内部状态的读取都必须在 d.mu 保护下**。后台 goroutine 除了
	// updateStatus 改 Phase/Progress/…，updateNodeStatus 还会写 NodeStatus map
	// （executeDeploy → precheckNodes 一路在调）。任何锁外读 internal.* 都会与
	// 它们构成新的 data race——本仓 -race CI 第一轮正是红在测试的锁外读上，
	// 而非生产代码。故这里把要断言的值一次性在锁内取出，锁外只做比较。
	got.Phase = "MUTATED"
	got.NodeStatus["cp1"] = "poison"

	var (
		foundInternal bool
		aliasedPtr    bool
		sharedMap     bool
		internalPhase string
		internalCP1   string
		hasCP1        bool
	)
	d.mu.RLock()
	for _, s := range d.deployments {
		if s.StartedAt.Equal(got.StartedAt) {
			foundInternal = true
			aliasedPtr = got == s
			sharedMap = sameMapPointer(got.NodeStatus, s.NodeStatus)
			internalPhase = s.Phase
			internalCP1, hasCP1 = s.NodeStatus["cp1"]
			break
		}
	}
	d.mu.RUnlock()

	if !foundInternal {
		t.Fatal("Deploy 未在 deployments map 中登记条目")
	}
	if aliasedPtr {
		t.Fatal("Deploy 返回了内部指针——后台 goroutine 改它时 HTTP 响应正在序列化（data race）")
	}
	if sharedMap {
		t.Fatal("NodeStatus map 与内部共享底层——浅拷贝同样会竞态")
	}
	if internalPhase == "MUTATED" {
		t.Error("改调用方副本影响了内部状态")
	}
	if hasCP1 && internalCP1 == "poison" {
		t.Error("NodeStatus 是共享 map：改副本污染了内部状态")
	}
}

// sameMapPointer 用反射判两个 map 是否同一份底层（hmap 指针相同）。
func sameMapPointer(a, b map[string]string) bool {
	ra := reflect.ValueOf(a)
	rb := reflect.ValueOf(b)
	return ra.Pointer() == rb.Pointer()
}
