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
	"net/http"
	"net/http/httptest"
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
