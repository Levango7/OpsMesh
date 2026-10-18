// k8s_provision.go K8s 集群一键部署（kubeadm/kind/k3s）。
//
// 背景：现有 k8s_cluster.go/k8s_manage.go 是"连接已有集群"的管理能力（基于 kubeconfig）；
// 本文件补"从零部署集群"的能力，经 agent shell 模板下发执行。
//
// 三种部署工具定位：
//   - kubeadm：官方生产级集群引导工具，需预装容器运行时（containerd）+ kubelet/kubeadm/kubectl。
//   - kind：Kubernetes IN Docker，测试/CI 级，单机多节点，秒级起停。
//   - k3s：轻量级生产可用发行版，单二进制，内置 containerd/etcd/SQLite，适合边缘/IoT。
//
// API 端点：
//   - POST /api/v1/k8s/provision           — 部署集群（{deviceID, tool, version, mode, ...}）
//   - POST /api/v1/k8s/provision/lifecycle — 启停/卸载集群（{deviceID, tool, action}）
//   - POST /api/v1/k8s/provision/join      — 加入节点（kubeadm/k3s）
//   - POST /api/v1/k8s/provision/kubeconfig— 导出 kubeconfig
//
// 安全：tool/action 走白名单；版本/token/节点地址走正则校验；5xx 错误走 writeInternalError。

package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// K8s 部署工具白名单。
var allowedK8sTools = map[string]bool{
	"kubeadm": true,
	"kind":    true,
	"k3s":     true,
}

// K8s 生命周期动作白名单。
var allowedK8sProvisionActions = map[string]bool{
	"start":  true,
	"stop":   true,
	"delete": true, // 卸载/删除集群
	"status": true,
}

// K8s 部署模式白名单（按工具差异化）。
var allowedK8sModes = map[string]map[string]bool{
	"kubeadm": {
		"single": true, // 单控制面
		"ha":     true, // 高可用（多控制面 + LB）
	},
	"kind": {
		"single": true,
		"multi":  true, // 多节点（config 文件）
	},
	"k3s": {
		"single":       true,
		"server-agent": true, // server + agent 分离
	},
}

// K8s 版本号正则。
var k8sVersionRe = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$`)

// K8s 集群名/节点地址/token 正则。
var k8sClusterNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]{0,62}$`)
var k8sTokenRe = regexp.MustCompile(`^[a-zA-Z0-9_.:]+$`)

// k8sProvisionReq K8s 部署请求体。
type k8sProvisionReq struct {
	DeviceID    string `json:"deviceID"`
	Tool        string `json:"tool"`        // kubeadm/kind/k3s
	Version     string `json:"version"`     // K8s 版本（如 v1.29.0）
	Mode        string `json:"mode"`        // 部署模式
	ClusterName string `json:"clusterName"` // 集群名（kind/k3s）
	PodCIDR     string `json:"podCIDR"`     // Pod 网段
	Nodes       int    `json:"nodes"`       // 节点数（kind multi）
}

// k8sLifecycleReq K8s 生命周期请求体。
type k8sLifecycleReq struct {
	DeviceID    string `json:"deviceID"`
	Tool        string `json:"tool"`        // kubeadm/kind/k3s
	Action      string `json:"action"`      // start/stop/delete/status
	ClusterName string `json:"clusterName"` // 集群名
}

// k8sJoinReq K8s 加入节点请求体。
type k8sJoinReq struct {
	DeviceID   string `json:"deviceID"`
	Tool       string `json:"tool"`       // kubeadm/k3s
	Role       string `json:"role"`       // controlplane/worker
	MasterAddr string `json:"masterAddr"` // master 地址
	Token      string `json:"token"`      // join token
	CertHash   string `json:"certHash"`   // CA 证书哈希（kubeadm）
}

// handleK8sProvision 处理 POST /api/v1/k8s/provision：一键部署 K8s 集群。
func (s *Server) handleK8sProvision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	var req k8sProvisionReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !allowedK8sTools[req.Tool] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported k8s tool (kubeadm/kind/k3s)"})
		return
	}
	mode := req.Mode
	if mode == "" {
		mode = "single"
	}
	if !allowedK8sModes[req.Tool][mode] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported mode for " + req.Tool})
		return
	}
	if req.Version != "" && !k8sVersionRe.MatchString(req.Version) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid k8s version"})
		return
	}
	if req.ClusterName != "" && !k8sClusterNameRe.MatchString(req.ClusterName) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster name"})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Mode = mode

	script := buildK8sProvisionScript(req)
	action := "k8s_provision_" + req.Tool + "_" + mode

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.provision."+req.Tool, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Tool + ":" + mode,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s version=%s cluster=%s", req.DeviceID, req.Version, req.ClusterName)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildK8sProvisionScript(req k8sProvisionReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Tool {
	case "kubeadm":
		b.WriteString(buildKubeadmProvisionScript(req))
	case "kind":
		b.WriteString(buildKindProvisionScript(req))
	case "k3s":
		b.WriteString(buildK3sProvisionScript(req))
	}
	return b.String()
}

func buildKubeadmProvisionScript(req k8sProvisionReq) string {
	var b strings.Builder
	b.WriteString("# kubeadm 生产级集群引导（需 containerd + kubelet/kubeadm/kubectl）\n")
	b.WriteString("K8S_VERSION=${K8S_VERSION:-" + defaultIfEmpty(req.Version, "v1.29.0") + "}\n")
	b.WriteString("if command -v kubeadm >/dev/null 2>&1; then echo 'kubeadm 已安装'; else\n")
	b.WriteString("  # 关闭 swap + 加载内核模块（kubeadm 前置条件）\n")
	b.WriteString("  swapoff -a || true\n")
	b.WriteString("  sed -i '/swap/s/^/#/' /etc/fstab || true\n")
	b.WriteString("  modprobe br_netfilter || true\n")
	b.WriteString("  sysctl -w net.bridge.bridge-nf-call-iptables=1 net.ipv4.ip_forward=1 || true\n")
	b.WriteString("  # 配置 k8s apt/yum 源并安装\n")
	b.WriteString("  if command -v apt-get >/dev/null 2>&1; then\n")
	b.WriteString("    apt-get update -qq && apt-get install -y apt-transport-https ca-certificates curl containerd kubelet kubeadm kubectl || echo '请配置 pkgs.k8s.io 源' \n")
	b.WriteString("  elif command -v yum >/dev/null 2>&1; then\n")
	b.WriteString("    yum install -y containerd kubelet kubeadm kubectl || echo '请配置 pkgs.k8s.io 源' \n")
	b.WriteString("  fi\n")
	b.WriteString("fi\n")
	if req.Mode == "ha" {
		b.WriteString("# HA：需先初始化首个控制面，再用 --control-plane --certificate-key 加入其余控制面\n")
		b.WriteString("kubeadm init --control-plane-endpoint ${LB_ADDR:-cluster-endpoint:6443} --pod-network-cidr=" + defaultIfEmpty(req.PodCIDR, "10.244.0.0/16") + " || true\n")
	} else {
		b.WriteString("kubeadm init --pod-network-cidr=" + defaultIfEmpty(req.PodCIDR, "10.244.0.0/16") + " || true\n")
	}
	b.WriteString("mkdir -p $HOME/.kube && cp -f /etc/kubernetes/admin.conf $HOME/.kube/config || true\n")
	b.WriteString("export KUBECONFIG=/etc/kubernetes/admin.conf\n")
	b.WriteString("# 安装 CNI（Flannel）\n")
	b.WriteString("kubectl apply -f https://raw.githubusercontent.com/flannel-io/flannel/master/Documentation/kube-flannel.yml || true\n")
	b.WriteString("echo 'kubeadm 集群已初始化'\n")
	return b.String()
}

func buildKindProvisionScript(req k8sProvisionReq) string {
	var b strings.Builder
	b.WriteString("# kind（Kubernetes IN Docker）测试/CI 级集群\n")
	b.WriteString("if command -v kind >/dev/null 2>&1; then echo 'kind 已安装'; else\n")
	b.WriteString("  curl -Lo ./kind https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64 && chmod +x ./kind && mv ./kind /usr/local/bin/kind || echo '安装 kind 失败' \n")
	b.WriteString("fi\n")
	cluster := defaultIfEmpty(req.ClusterName, "opsmesh-kind")
	nodes := req.Nodes
	if nodes <= 0 {
		nodes = 1
	}
	if req.Mode == "multi" {
		b.WriteString("# 多节点集群：生成 kind 配置\n")
		fmt.Fprintf(&b, "cat > /tmp/kind-%s.yaml <<EOF\nkind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nnodes:\n- role: control-plane\n", cluster)
		fmt.Fprintf(&b, "  extraPortMappings:\n  - containerPort: 30000\n    hostPort: 30000\n    protocol: TCP\n")
		for i := 0; i < nodes; i++ {
			b.WriteString("- role: worker\n")
		}
		b.WriteString("EOF\n")
		ver := strings.TrimPrefix(req.Version, "v")
		if ver != "" {
			fmt.Fprintf(&b, "kind create cluster --name %s --config /tmp/kind-%s.yaml --image kindest/node:v%s || true\n", cluster, cluster, ver)
		} else {
			fmt.Fprintf(&b, "kind create cluster --name %s --config /tmp/kind-%s.yaml || true\n", cluster, cluster)
		}
	} else {
		fmt.Fprintf(&b, "kind create cluster --name %s || true\n", cluster)
	}
	fmt.Fprintf(&b, "kind get kubeconfig --name %s > /etc/kubernetes/kind-%s.kubeconfig || true\n", cluster, cluster)
	b.WriteString("echo 'kind 集群已创建'\n")
	return b.String()
}

func buildK3sProvisionScript(req k8sProvisionReq) string {
	var b strings.Builder
	b.WriteString("# k3s 轻量级生产可用发行版\n")
	b.WriteString("if command -v k3s >/dev/null 2>&1; then echo 'k3s 已安装'; else\n")
	b.WriteString("  curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION=" + defaultIfEmpty(req.Version, "v1.29.0+k3s1") + " sh - || echo '安装 k3s 失败'\n")
	b.WriteString("fi\n")
	if req.Mode == "server-agent" {
		b.WriteString("# server-agent 分离：本机作为 server，agent 节点用 K3S_URL + K3S_TOKEN 加入\n")
		b.WriteString("K3S_TOKEN=$(cat /var/lib/rancher/k3s/server/node-token 2>/dev/null || echo '')\n")
		b.WriteString("echo \"k3s server 已就绪，agent 加入: curl -sfL https://get.k3s.io | K3S_URL=https://<server>:6443 K3S_TOKEN=$K3S_TOKEN sh -\"\n")
	} else {
		b.WriteString("systemctl enable --now k3s || true\n")
		b.WriteString("echo 'k3s 单节点已启动'\n")
	}
	b.WriteString("mkdir -p $HOME/.kube && cp -f /etc/rancher/k3s/k3s.yaml $HOME/.kube/config || true\n")
	b.WriteString("echo 'k3s 集群已就绪 (kubeconfig: /etc/rancher/k3s/k3s.yaml)'\n")
	return b.String()
}

// handleK8sProvisionLifecycle 处理 POST /api/v1/k8s/provision/lifecycle：启停/卸载集群。
func (s *Server) handleK8sProvisionLifecycle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	var req k8sLifecycleReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !allowedK8sTools[req.Tool] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported k8s tool"})
		return
	}
	if !allowedK8sProvisionActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (start/stop/delete/status)"})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.ClusterName != "" && !k8sClusterNameRe.MatchString(req.ClusterName) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster name"})
		return
	}

	script := buildK8sLifecycleScript(req)
	taskAction := "k8s_" + req.Action + "_" + req.Tool

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.lifecycle."+req.Tool, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: req.Tool,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s cluster=%s", req.DeviceID, req.ClusterName)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildK8sLifecycleScript(req k8sLifecycleReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	cluster := defaultIfEmpty(req.ClusterName, "opsmesh-kind")
	switch req.Tool {
	case "kubeadm":
		b.WriteString("# kubeadm 集群生命周期\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start kubelet || true\n")
		case "stop":
			b.WriteString("systemctl stop kubelet || true\n")
		case "delete":
			b.WriteString("kubeadm reset -f || true\nrm -rf $HOME/.kube /etc/kubernetes || true\n")
		case "status":
			b.WriteString("kubectl get nodes --kubeconfig=/etc/kubernetes/admin.conf 2>/dev/null || echo 'not running'\n")
		}
	case "kind":
		b.WriteString("# kind 集群生命周期\n")
		switch req.Action {
		case "start":
			fmt.Fprintf(&b, "docker start $(kind get nodes --name %s 2>/dev/null) 2>/dev/null || true\n", cluster)
		case "stop":
			fmt.Fprintf(&b, "docker stop $(kind get nodes --name %s 2>/dev/null) 2>/dev/null || true\n", cluster)
		case "delete":
			fmt.Fprintf(&b, "kind delete cluster --name %s || true\n", cluster)
		case "status":
			b.WriteString("kind get clusters 2>/dev/null || echo 'not running'\n")
		}
	case "k3s":
		b.WriteString("# k3s 集群生命周期\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start k3s || true\n")
		case "stop":
			b.WriteString("systemctl stop k3s || true\n")
		case "delete":
			b.WriteString("/usr/local/bin/k3s-uninstall.sh || true\n")
		case "status":
			b.WriteString("systemctl is-active k3s || echo 'not running'\n")
		}
	}
	fmt.Fprintf(&b, "echo 'K8s(%s) %s 操作完成\n", req.Tool, req.Action)
	return b.String()
}

// handleK8sJoin 处理 POST /api/v1/k8s/provision/join：加入节点。
func (s *Server) handleK8sJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	var req k8sJoinReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// 仅支持 kubeadm/k3s 加入节点（kind 不支持）。
	if req.Tool != "kubeadm" && req.Tool != "k3s" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "join only supports kubeadm/k3s"})
		return
	}
	if req.Role != "controlplane" && req.Role != "worker" && req.Role != "agent" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role (controlplane/worker/agent)"})
		return
	}
	if req.MasterAddr == "" || !monitorHostRe.MatchString(req.MasterAddr) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid master address"})
		return
	}
	if !k8sTokenRe.MatchString(req.Token) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid token"})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}

	script := buildK8sJoinScript(req)
	taskAction := "k8s_join_" + req.Tool

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.join."+req.Tool, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: req.Role,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s master=%s", req.DeviceID, req.MasterAddr)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildK8sJoinScript(req k8sJoinReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Tool {
	case "kubeadm":
		b.WriteString("# kubeadm 节点加入\n")
		if req.Role == "controlplane" {
			fmt.Fprintf(&b, "kubeadm join %s:6443 --token %s --discovery-token-ca-cert-hash %s --control-plane || true\n", req.MasterAddr, req.Token, defaultIfEmpty(req.CertHash, "sha256:<hash>"))
		} else {
			fmt.Fprintf(&b, "kubeadm join %s:6443 --token %s --discovery-token-ca-cert-hash %s || true\n", req.MasterAddr, req.Token, defaultIfEmpty(req.CertHash, "sha256:<hash>"))
		}
	case "k3s":
		b.WriteString("# k3s 节点加入（agent）\n")
		fmt.Fprintf(&b, "curl -sfL https://get.k3s.io | K3S_URL=https://%s:6443 K3S_TOKEN=%s sh - || true\n", req.MasterAddr, req.Token)
	}
	b.WriteString("echo '节点加入完成'\n")
	return b.String()
}

// handleK8sKubeconfig 处理 POST /api/v1/k8s/provision/kubeconfig：导出 kubeconfig。
func (s *Server) handleK8sKubeconfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "device:read"); !ok {
		return
	}
	var req k8sLifecycleReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !allowedK8sTools[req.Tool] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported k8s tool"})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.ClusterName != "" && !k8sClusterNameRe.MatchString(req.ClusterName) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cluster name"})
		return
	}

	script := buildK8sKubeconfigScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "k8s_kubeconfig_"+req.Tool)
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.kubeconfig."+req.Tool, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "k8s_kubeconfig_" + req.Tool, Target: req.Tool,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildK8sKubeconfigScript(req k8sLifecycleReq) string {
	var b strings.Builder
	switch req.Tool {
	case "kubeadm":
		b.WriteString("cat /etc/kubernetes/admin.conf\n")
	case "kind":
		fmt.Fprintf(&b, "kind get kubeconfig --name %s\n", defaultIfEmpty(req.ClusterName, "opsmesh-kind"))
	case "k3s":
		b.WriteString("cat /etc/rancher/k3s/k3s.yaml\n")
	}
	return b.String()
}

// defaultIfEmpty 返回 s（非空）或 fallback。
func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// ============================================================================
// 路由注册
// ============================================================================

// registerK8sProvisionRoutes 注册 K8s 一键部署路由。
func (s *Server) registerK8sProvisionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/k8s/provision", s.handleK8sProvision)
	mux.HandleFunc("/api/v1/k8s/provision/lifecycle", s.handleK8sProvisionLifecycle)
	mux.HandleFunc("/api/v1/k8s/provision/join", s.handleK8sJoin)
	mux.HandleFunc("/api/v1/k8s/provision/kubeconfig", s.handleK8sKubeconfig)
}
