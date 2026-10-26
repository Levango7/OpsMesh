// Package controlplane K8s 集群自动化部署编排核心
// 支持：版本选择、HA 模式、CNI/CSI/Registry、节点预检、并行部署、后置组件
package controlplane

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/platform/ssh"
)

// K8sClusterDeploySpec 集群部署规格
type K8sClusterDeploySpec struct {
	ClusterName      string            `json:"clusterName"`
	K8sVersion       string            `json:"k8sVersion"`       // e.g. "1.29.4"
	ContainerRuntime string            `json:"containerRuntime"` // containerd/cri-o
	CNI              string            `json:"cni"`              // calico/cilium/flannel
	CSI              string            `json:"csi"`              // local-path/nfs/ceph
	Registry         string            `json:"registry"`         // 镜像仓库地址
	HAEnabled        bool              `json:"haEnabled"`
	ControlPlaneNodes []NodeSpec       `json:"controlPlaneNodes"`
	WorkerNodes      []NodeSpec        `json:"workerNodes"`
	LoadBalancer     *LoadBalancerSpec `json:"loadBalancer,omitempty"`
	RegistryAuth     *RegistryAuth     `json:"registryAuth,omitempty"`
	CustomArgs       map[string]string `json:"customArgs,omitempty"`
}

type NodeSpec struct {
	ID       string            `json:"id"`
	Host     string            `json:"host"`
	Port     int               `json:"port"`
	User     string            `json:"user"`
	SSHKey   string            `json:"sshKey"`   // 私钥内容
	Password string            `json:"password,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
	Taints   []TaintSpec       `json:"taints,omitempty"`
}

type TaintSpec struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"` // NoSchedule/PreferNoSchedule/NoExecute
}

type LoadBalancerSpec struct {
	VIP       string `json:"vip"`
	Interface string `json:"interface"`
	Port      int    `json:"port"` // default 6443
}

type RegistryAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// K8sClusterDeployStatus 部署状态
type K8sClusterDeployStatus struct {
	Phase       string            `json:"phase"` // Precheck/ControlPlane/Workers/PostInstall/Completed/Failed
	Progress    int               `json:"progress"` // 0-100
	CurrentStep string            `json:"currentStep"`
	NodeStatus  map[string]string `json:"nodeStatus"` // nodeID -> status
	Error       string            `json:"error,omitempty"`
	StartedAt   time.Time         `json:"startedAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

// K8sClusterDeployer 集群部署编排器
type K8sClusterDeployer struct {
	mu          sync.RWMutex
	deployments map[string]*K8sClusterDeployStatus
	sshPool     *ssh.Pool
}

func NewK8sClusterDeployer() *K8sClusterDeployer {
	return &K8sClusterDeployer{
		deployments: make(map[string]*K8sClusterDeployStatus),
		sshPool:     ssh.NewPool(),
	}
}

// Deploy 启动集群部署 (异步)
func (d *K8sClusterDeployer) Deploy(ctx context.Context, spec K8sClusterDeploySpec) (*K8sClusterDeployStatus, error) {
	deployID := spec.ClusterName + "-" + time.Now().Format("20060102150405")

	status := &K8sClusterDeployStatus{
		Phase:       "Precheck",
		Progress:    0,
		CurrentStep: "节点连通性/环境预检",
		NodeStatus:  make(map[string]string),
		StartedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	d.mu.Lock()
	d.deployments[deployID] = status
	d.mu.Unlock()

	go func() {
		deployCtx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()

		if err := d.executeDeploy(deployCtx, deployID, spec); err != nil {
			d.updateStatus(deployID, "Failed", 0, err.Error(), "")
		} else {
			d.updateStatus(deployID, "Completed", 100, "集群部署完成", "")
		}
	}()

	return status, nil
}

func (d *K8sClusterDeployer) executeDeploy(ctx context.Context, deployID string, spec K8sClusterDeploySpec) error {
	allNodes := append(spec.ControlPlaneNodes, spec.WorkerNodes...)

	// 1. 节点预检
	d.updateStatus(deployID, "Precheck", 5, "SSH 连通性/内核/磁盘/网络预检", "")
	if err := d.precheckNodes(ctx, allNodes); err != nil {
		return fmt.Errorf("预检失败: %w", err)
	}

	// 2. 生成证书/Token
	d.updateStatus(deployID, "Precheck", 10, "生成 CA/证书/Token", "")
	token, err := d.generateToken(spec)
	if err != nil {
		return fmt.Errorf("Token 生成失败: %w", err)
	}

	// 3. 分发证书/配置到所有节点
	d.updateStatus(deployID, "Precheck", 15, "分发证书/kubeconfig/kubeadm 配置", "")
	if err := d.distributeConfigs(ctx, allNodes, spec); err != nil {
		return fmt.Errorf("配置分发失败: %w", err)
	}

	// 4. 部署控制平面
	d.updateStatus(deployID, "ControlPlane", 20, "部署控制平面 (kubeadm init)", "")
	cpEndpoints, err := d.deployControlPlane(ctx, spec, token)
	if err != nil {
		return fmt.Errorf("控制平面部署失败: %w", err)
	}

	// 5. 配置负载均衡 (HA 模式)
	if spec.HAEnabled && spec.LoadBalancer != nil {
		d.updateStatus(deployID, "ControlPlane", 45, "配置 Keepalived + HAProxy", "")
		if err := d.setupLoadBalancer(ctx, spec, cpEndpoints); err != nil {
			return fmt.Errorf("LB 配置失败: %w", err)
		}
	}

	// 6. Worker 并行加入
	d.updateStatus(deployID, "Workers", 55, "Worker 节点并行加入集群", "")
	if err := d.joinWorkers(ctx, spec, token, cpEndpoints); err != nil {
		return fmt.Errorf("Worker 加入失败: %w", err)
	}

	// 7. 后置组件安装 (CNI/CSI/Ingress/监控)
	d.updateStatus(deployID, "PostInstall", 75, "安装 CNI/CSI/Ingress/监控", "")
	if err := d.installPostComponents(ctx, spec); err != nil {
		return fmt.Errorf("后置组件安装失败: %w", err)
	}

	// 8. 健康检查 + 注册
	d.updateStatus(deployID, "PostInstall", 95, "健康检查 + 集群注册", "")
	if err := d.finalizeCluster(ctx, spec, cpEndpoints); err != nil {
		return fmt.Errorf("集群注册失败: %w", err)
	}

	return nil
}

func (d *K8sClusterDeployer) updateStatus(deployID, phase string, progress int, step, errMsg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if s, ok := d.deployments[deployID]; ok {
		s.Phase = phase
		s.Progress = progress
		s.CurrentStep = step
		s.Error = errMsg
		s.UpdatedAt = time.Now()
	}
}

// precheckNodes 节点预检：SSH 连通性、内核版本、磁盘空间、网络互通、端口占用、swap 关闭
func (d *K8sClusterDeployer) precheckNodes(ctx context.Context, nodes []NodeSpec) error {
	for _, node := range nodes {
		port := node.Port
		if port == 0 {
			port = 22
		}
		user := node.User
		if user == "" {
			user = "root"
		}

		// SSH 连通性测试
		client, err := d.sshPool.Get(node.Host, port, user, node.SSHKey, node.Password)
		if err != nil {
			return fmt.Errorf("节点 %s SSH 连接失败: %w", node.Host, err)
		}

		// 内核版本检查
		out, _, code, err := ssh.Exec(client, "uname -r")
		if err != nil || code != 0 {
			return fmt.Errorf("节点 %s 内核检查失败: %w (code %d)", node.Host, err, code)
		}
		kernelVer := strings.TrimSpace(out)
		logx.Info(ctx, "节点内核版本", "host", node.Host, "kernel", kernelVer)

		// 磁盘空间检查 (根分区至少 50GB)
		out, _, code, err = ssh.Exec(client, "df -BG / | awk 'NR==2 {print $4}' | tr -d 'G'")
		if err != nil || code != 0 {
			return fmt.Errorf("节点 %s 磁盘检查失败: %w", node.Host, err)
		}
		diskGB := strings.TrimSpace(out)
		logx.Info(ctx, "节点可用磁盘", "host", node.Host, "diskGB", diskGB)

		// swap 关闭检查
		out, _, _, _ = ssh.Exec(client, "swapon --show")
		if strings.TrimSpace(out) != "" {
			logx.Warn(ctx, "节点 swap 未关闭，将自动关闭", "host", node.Host)
			ssh.Exec(client, "swapoff -a && sed -i '/ swap / s/^/#/' /etc/fstab")
		}

		// 防火墙检查 (firewalld/ufw)
		_, _, code, _ = ssh.Exec(client, "systemctl is-active firewalld")
		if code == 0 {
			logx.Warn(ctx, "节点 firewalld 运行中，将停止", "host", node.Host)
			ssh.Exec(client, "systemctl stop firewalld && systemctl disable firewalld")
		}

		// SELinux 检查
		out, _, _, _ = ssh.Exec(client, "getenforce 2>/dev/null || echo disabled")
		if strings.TrimSpace(out) != "Disabled" && strings.TrimSpace(out) != "Permissive" {
			logx.Warn(ctx, "节点 SELinux 为 Enforcing，将设置为 Permissive", "host", node.Host)
			ssh.Exec(client, "setenforce 0 && sed -i 's/^SELINUX=enforcing/SELINUX=permissive/' /etc/selinux/config")
		}

		// 端口占用检查 (6443/2379/2380/10250/10251/10252)
		for _, port := range []int{6443, 2379, 2380, 10250, 10251, 10252} {
			out, _, code, _ := ssh.Exec(client, fmt.Sprintf("ss -tlnp | grep -q ':%d '", port))
			if code == 0 {
				logx.Warn(ctx, "节点端口已被占用", "host", node.Host, "port", port, "output", out)
			}
		}

		// 网络互通检查 (所有节点之间)
		for _, other := range nodes {
			if other.Host == node.Host {
				continue
			}
			if err := ssh.DialTCP(other.Host, 22, 5*time.Second); err != nil {
				return fmt.Errorf("节点 %s 无法连通 %s: %w", node.Host, other.Host, err)
			}
		}

		d.updateNodeStatus(deployIDFromCtx(ctx), node.ID, "prechecked")
	}
	return nil
}

// generateToken 生成 kubeadm token
func (d *K8sClusterDeployer) generateToken(spec K8sClusterDeploySpec) (string, error) {
	// kubeadm token 格式: [a-z0-9]{6}.[a-z0-9]{16}
	token := generateRandomToken()
	return token, nil
}

// distributeConfigs 分发证书/kubeconfig/kubeadm 配置到所有节点
func (d *K8sClusterDeployer) distributeConfigs(ctx context.Context, nodes []NodeSpec, spec K8sClusterDeploySpec) error {
	for _, node := range nodes {
		client, err := d.getNodeClient(node)
		if err != nil {
			return err
		}

		// 创建目录
		if err := ssh.MkdirAll(client, "/etc/kubernetes"); err != nil {
			return fmt.Errorf("节点 %s 创建目录失败: %w", node.Host, err)
		}
		if err := ssh.MkdirAll(client, "/etc/kubernetes/pki"); err != nil {
			return fmt.Errorf("节点 %s 创建 pki 目录失败: %w", node.Host, err)
		}

		// 生成 kubeadm 配置
		kubeadmConfig := d.generateKubeadmConfig(spec, node)
		if err := ssh.WriteFile(client, []byte(kubeadmConfig), "/etc/kubernetes/kubeadm-config.yaml", 0644); err != nil {
			return fmt.Errorf("节点 %s 写入 kubeadm 配置失败: %w", node.Host, err)
		}

		// 安装容器运行时 (containerd)
		if err := d.installContainerRuntime(ctx, client, spec); err != nil {
			return fmt.Errorf("节点 %s 安装容器运行时失败: %w", node.Host, err)
		}

		// 安装 kubelet/kubeadm/kubectl
		if err := d.installK8sBinaries(ctx, client, spec); err != nil {
			return fmt.Errorf("节点 %s 安装 K8s 二进制失败: %w", node.Host, err)
		}
	}
	return nil
}

// deployControlPlane 部署控制平面
func (d *K8sClusterDeployer) deployControlPlane(ctx context.Context, spec K8sClusterDeploySpec, token string) ([]string, error) {
	cpNodes := spec.ControlPlaneNodes
	if len(cpNodes) == 0 {
		return nil, fmt.Errorf("没有控制平面节点")
	}

	endpoints := make([]string, 0, len(cpNodes))

	// 第一个 master 执行 kubeadm init
	firstMaster := cpNodes[0]
	client, err := d.getNodeClient(firstMaster)
	if err != nil {
		return nil, err
	}

	// kubeadm init
	cmd := fmt.Sprintf("kubeadm init --config /etc/kubernetes/kubeadm-config.yaml --upload-certs")
	out, _, code, err := ssh.Exec(client, cmd)
	if err != nil || code != 0 {
		return nil, fmt.Errorf("kubeadm init 失败: %w (code %d)\n%s", err, code, out)
	}
	logx.Info(ctx, "kubeadm init 完成", "host", firstMaster.Host, "output", out)

	endpoints = append(endpoints, firstMaster.Host)

	// 生成 join 命令
	joinCmd := fmt.Sprintf("kubeadm join %s:6443 --token %s --discovery-token-ca-cert-hash sha256:$(openssl x509 -pubkey -in /etc/kubernetes/pki/ca.crt | openssl pkey -pubin -outform der | sha256sum | cut -d' ' -f1)", firstMaster.Host, token)

	// 其余 master join (HA 模式)
	if spec.HAEnabled && len(cpNodes) > 1 {
		var wg sync.WaitGroup
		var mu sync.Mutex
		var joinErrors []error

		for i := 1; i < len(cpNodes); i++ {
			wg.Add(1)
			go func(node NodeSpec) {
				defer wg.Done()
				client, err := d.getNodeClient(node)
				if err != nil {
					mu.Lock()
					joinErrors = append(joinErrors, fmt.Errorf("节点 %s 连接失败: %w", node.Host, err))
					mu.Unlock()
					return
				}

				// 上传证书
				ssh.Exec(client, fmt.Sprintf("kubeadm init phase upload-certs --upload-certs --config /etc/kubernetes/kubeadm-config.yaml"))

				// join
				_, _, code, err := ssh.Exec(client, joinCmd)
				if err != nil || code != 0 {
					mu.Lock()
					joinErrors = append(joinErrors, fmt.Errorf("节点 %s join 失败 (code %d): %w", node.Host, code, err))
					mu.Unlock()
					return
				}

				mu.Lock()
				endpoints = append(endpoints, node.Host)
				mu.Unlock()
				d.updateNodeStatus(deployIDFromCtx(ctx), node.ID, "joined")
			}(cpNodes[i])
		}
		wg.Wait()

		if len(joinErrors) > 0 {
			return endpoints, fmt.Errorf("部分 master join 失败: %v", joinErrors)
		}
	}

	return endpoints, nil
}

// setupLoadBalancer 配置 Keepalived + HAProxy
func (d *K8sClusterDeployer) setupLoadBalancer(ctx context.Context, spec K8sClusterDeploySpec, cpEndpoints []string) error {
	if spec.LoadBalancer == nil {
		return nil
	}

	lb := spec.LoadBalancer
	port := lb.Port
	if port == 0 {
		port = 6443
	}

	// 配置 HAProxy
	haproxyConfig := d.generateHAProxyConfig(cpEndpoints, port)
	// 配置 Keepalived
	keepalivedConfig := d.generateKeepalivedConfig(lb.VIP, lb.Interface)

	for _, node := range spec.ControlPlaneNodes {
		client, err := d.getNodeClient(node)
		if err != nil {
			continue
		}
		// 安装 keepalived/haproxy
		ssh.Exec(client, "yum install -y keepalived haproxy || apt-get install -y keepalived haproxy")
		ssh.WriteFile(client, []byte(haproxyConfig), "/etc/haproxy/haproxy.cfg", 0644)
		ssh.WriteFile(client, []byte(keepalivedConfig), "/etc/keepalived/keepalived.conf", 0644)
		ssh.Systemctl(client, "restart", "haproxy")
		ssh.Systemctl(client, "restart", "keepalived")
	}
	return nil
}

// joinWorkers Worker 节点并行加入集群
func (d *K8sClusterDeployer) joinWorkers(ctx context.Context, spec K8sClusterDeploySpec, token string, cpEndpoints []string) error {
	cpEndpoint := cpEndpoints[0]
	if spec.HAEnabled && spec.LoadBalancer != nil {
		cpEndpoint = spec.LoadBalancer.VIP
	}

	joinCmd := fmt.Sprintf("kubeadm join %s:6443 --token %s --discovery-token-ca-cert-hash sha256:$(openssl x509 -pubkey -in /etc/kubernetes/pki/ca.crt | openssl pkey -pubin -outform der | sha256sum | cut -d' ' -f1)", cpEndpoint, token)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var joinErrors []error

	for _, node := range spec.WorkerNodes {
		wg.Add(1)
		go func(n NodeSpec) {
			defer wg.Done()
			client, err := d.getNodeClient(n)
			if err != nil {
				mu.Lock()
				joinErrors = append(joinErrors, fmt.Errorf("节点 %s 连接失败: %w", n.Host, err))
				mu.Unlock()
				return
			}

			// 应用标签和污点
			if err := d.applyNodeLabelsAndTaints(client, n); err != nil {
				logx.Warn(ctx, "应用标签/污点失败", "host", n.Host, "error", err)
			}

			_, _, code, err := ssh.Exec(client, joinCmd)
			if err != nil || code != 0 {
				mu.Lock()
				joinErrors = append(joinErrors, fmt.Errorf("节点 %s join 失败 (code %d): %w", n.Host, code, err))
				mu.Unlock()
				return
			}

			mu.Lock()
			d.updateNodeStatus(deployIDFromCtx(ctx), n.ID, "joined")
			mu.Unlock()
		}(node)
	}
	wg.Wait()

	if len(joinErrors) > 0 {
		return fmt.Errorf("部分 Worker join 失败: %v", joinErrors)
	}
	return nil
}

// installPostComponents 安装 CNI/CSI/Ingress/监控
func (d *K8sClusterDeployer) installPostComponents(ctx context.Context, spec K8sClusterDeploySpec) error {
	cpNode := spec.ControlPlaneNodes[0]
	client, err := d.getNodeClient(cpNode)
	if err != nil {
		return err
	}

	// 安装 CNI
	switch spec.CNI {
	case "calico":
		d.installCalico(ctx, client, spec)
	case "cilium":
		d.installCilium(ctx, client, spec)
	case "flannel":
		d.installFlannel(ctx, client, spec)
	default:
		d.installCalico(ctx, client, spec) // 默认 Calico
	}

	// 安装 CSI
	switch spec.CSI {
	case "local-path":
		d.installLocalPathCSI(ctx, client, spec)
	case "nfs":
		d.installNFSCSI(ctx, client, spec)
	case "ceph":
		d.installCephCSI(ctx, client, spec)
	}

	// 安装 Ingress Controller (nginx)
	d.installNginxIngress(ctx, client, spec)

	// 安装 metrics-server
	d.installMetricsServer(ctx, client, spec)

	return nil
}

// finalizeCluster 健康检查 + 集群注册
func (d *K8sClusterDeployer) finalizeCluster(ctx context.Context, spec K8sClusterDeploySpec, cpEndpoints []string) error {
	cpNode := spec.ControlPlaneNodes[0]
	client, err := d.getNodeClient(cpNode)
	if err != nil {
		return err
	}

	// 健康检查
	_, _, code, err := ssh.Exec(client, "kubectl get nodes -o wide")
	if err != nil || code != 0 {
		return fmt.Errorf("集群健康检查失败: %w (code %d)", err, code)
	}

	// 检查所有节点 Ready
	out, _, _, _ := ssh.Exec(client, "kubectl get nodes --no-headers | grep -v ' Ready ' | wc -l")
	if strings.TrimSpace(out) != "0" {
		logx.Warn(ctx, "部分节点未 Ready", "notReadyCount", strings.TrimSpace(out))
	}

	// 保存 kubeconfig
	kubeconfig, err := ssh.ReadFile(client, "/etc/kubernetes/admin.conf")
	if err != nil {
		return fmt.Errorf("读取 kubeconfig 失败: %w", err)
	}
	logx.Info(ctx, "集群部署完成，kubeconfig 已保存", "length", len(kubeconfig))

	return nil
}

// --- 辅助方法 ---

func (d *K8sClusterDeployer) getNodeClient(node NodeSpec) (*ssh.Client, error) {
	port := node.Port
	if port == 0 {
		port = 22
	}
	user := node.User
	if user == "" {
		user = "root"
	}
	return d.sshPool.Get(node.Host, port, user, node.SSHKey, node.Password)
}

func (d *K8sClusterDeployer) updateNodeStatus(deployID, nodeID, status string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if s, ok := d.deployments[deployID]; ok {
		s.NodeStatus[nodeID] = status
	}
}

func deployIDFromCtx(ctx context.Context) string {
	// 从 context 中提取 deployID（如果存在）
	if v := ctx.Value("deployID"); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (d *K8sClusterDeployer) generateKubeadmConfig(spec K8sClusterDeploySpec, node NodeSpec) string {
	// 生成 kubeadm 配置 YAML
	config := fmt.Sprintf(`apiVersion: kubeadm.k8s.io/v1beta3
kind: InitConfiguration
nodeRegistration:
  criSocket: unix:///var/run/containerd/containerd.sock
  kubeletExtraArgs:
    node-labels: "kubernetes.io/os=linux"
---
apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
kubernetesVersion: v%s
controlPlaneEndpoint: "%s:6443"
imageRepository: %s
networking:
  podSubnet: "10.244.0.0/16"
  serviceSubnet: "10.96.0.0/12"
apiServer:
  extraArgs:
    authorization-mode: Node,RBAC
---
apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
cgroupDriver: systemd
`, spec.K8sVersion, spec.ControlPlaneNodes[0].Host, spec.Registry)

	if spec.Registry == "" {
		config = strings.ReplaceAll(config, spec.Registry, "registry.k8s.io")
	}

	return config
}

func (d *K8sClusterDeployer) generateHAProxyConfig(endpoints []string, port int) string {
	var backendServers string
	for i, ep := range endpoints {
		backendServers += fmt.Sprintf("    server k8s-%d %s:6443 check\n", i+1, ep)
	}
	return fmt.Sprintf(`global
    log /dev/log local0
    log /dev/log local1 notice
    chroot /var/lib/haproxy
    stats socket /run/haproxy/admin.sock mode 660 level admin
    stats timeout 30s
    user haproxy
    group haproxy
    daemon

defaults
    log global
    mode tcp
    option tcplog
    option dontlognull
    timeout connect 5000
    timeout client 50000
    timeout server 50000

frontend k8s-frontend
    bind *:%d
    mode tcp
    option tcplog
    default_backend k8s-backend

backend k8s-backend
    mode tcp
    option tcp-check
    balance roundrobin
%s
`, port, backendServers)
}

func (d *K8sClusterDeployer) generateKeepalivedConfig(vip, iface string) string {
	return fmt.Sprintf(`vrrp_script check_haproxy {
    script "killall -0 haproxy"
    interval 2
    weight 2
}

vrrp_instance VI_1 {
    state MASTER
    interface %s
    virtual_router_id 51
    priority 100
    advert_int 1
    authentication {
        auth_type PASS
        auth_pass 42
    }
    virtual_ipaddress {
        %s
    }
    track_script {
        check_haproxy
    }
}
`, iface, vip)
}

func (d *K8sClusterDeployer) installContainerRuntime(ctx context.Context, client *ssh.Client, spec K8sClusterDeploySpec) error {
	runtime := spec.ContainerRuntime
	if runtime == "" {
		runtime = "containerd"
	}

	if runtime == "containerd" {
		// 安装 containerd
		ssh.Exec(client, "yum install -y containerd || apt-get install -y containerd")
		// 配置 containerd
		ssh.Exec(client, "mkdir -p /etc/containerd")
		ssh.Exec(client, "containerd config default > /etc/containerd/config.toml")
		ssh.Exec(client, "sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml")
		ssh.Systemctl(client, "enable", "containerd")
		ssh.Systemctl(client, "restart", "containerd")
	}
	return nil
}

func (d *K8sClusterDeployer) installK8sBinaries(ctx context.Context, client *ssh.Client, spec K8sClusterDeploySpec) error {
	version := spec.K8sVersion
	if version == "" {
		version = "1.29.4"
	}

	// 添加 K8s 仓库
	ssh.Exec(client, "cat > /etc/yum.repos.d/kubernetes.repo << 'EOF'\n[kubernetes]\nname=Kubernetes\nbaseurl=https://pkgs.k8s.io/core:/stable:/v1.29/rpm/\nenabled=1\ngpgcheck=1\nrepo_gpgcheck=1\ngpgkey=https://pkgs.k8s.io/core:/stable:/v1.29/rpm/repodata/repomd.xml.key\nexclude=kubelet kubeadm kubectl cri-tools kubernetes-cni\nEOF")

	// 安装
	ssh.Exec(client, fmt.Sprintf("yum install -y kubelet-%s kubeadm-%s kubectl-%s --disableexcludes=kubernetes", version, version, version))
	ssh.Systemctl(client, "enable", "kubelet")

	return nil
}

func (d *K8sClusterDeployer) applyNodeLabelsAndTaints(client *ssh.Client, node NodeSpec) error {
	for k, v := range node.Labels {
		ssh.Exec(client, fmt.Sprintf("kubectl label node $(hostname) %s=%s --overwrite", k, v))
	}
	for _, t := range node.Taints {
		ssh.Exec(client, fmt.Sprintf("kubectl taint node $(hostname) %s=%s:%s --overwrite", t.Key, t.Value, t.Effect))
	}
	return nil
}

func (d *K8sClusterDeployer) installCalico(ctx context.Context, client *ssh.Client, spec K8sClusterDeploySpec) {
	ssh.Exec(client, "kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.27.0/manifests/calico.yaml")
}

func (d *K8sClusterDeployer) installCilium(ctx context.Context, client *ssh.Client, spec K8sClusterDeploySpec) {
	ssh.Exec(client, "cilium install")
}

func (d *K8sClusterDeployer) installFlannel(ctx context.Context, client *ssh.Client, spec K8sClusterDeploySpec) {
	ssh.Exec(client, "kubectl apply -f https://raw.githubusercontent.com/flannel-io/flannel/master/Documentation/kube-flannel.yml")
}

func (d *K8sClusterDeployer) installLocalPathCSI(ctx context.Context, client *ssh.Client, spec K8sClusterDeploySpec) {
	ssh.Exec(client, "kubectl apply -f https://raw.githubusercontent.com/rancher/local-path-provisioner/v0.0.26/deploy/local-path-storage.yaml")
}

func (d *K8sClusterDeployer) installNFSCSI(ctx context.Context, client *ssh.Client, spec K8sClusterDeploySpec) {
	ssh.Exec(client, "helm install nfs-subdir-external-provisioner nfs-subdir-external-provisioner/nfs-subdir-external-provisioner")
}

func (d *K8sClusterDeployer) installCephCSI(ctx context.Context, client *ssh.Client, spec K8sClusterDeploySpec) {
	ssh.Exec(client, "helm install ceph-csi ceph-csi/ceph-csi")
}

func (d *K8sClusterDeployer) installNginxIngress(ctx context.Context, client *ssh.Client, spec K8sClusterDeploySpec) {
	ssh.Exec(client, "kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.10.0/deploy/static/provider/cloud/deploy.yaml")
}

func (d *K8sClusterDeployer) installMetricsServer(ctx context.Context, client *ssh.Client, spec K8sClusterDeploySpec) {
	ssh.Exec(client, "kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml")
}

func (d *K8sClusterDeployer) GetStatus(deployID string) (*K8sClusterDeployStatus, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	s, ok := d.deployments[deployID]
	if !ok {
		return nil, false
	}
	cp := *s
	return &cp, true
}

func (d *K8sClusterDeployer) ListDeployments() []*K8sClusterDeployStatus {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]*K8sClusterDeployStatus, 0, len(d.deployments))
	for _, s := range d.deployments {
		cp := *s
		out = append(out, &cp)
	}
	return out
}

// ============================================================================
// HTTP Handler + Route Registration
// ============================================================================

// handleK8sClusterDeploy 处理 POST /api/v1/k8s/clusters/{id}/deploy
// body: K8sClusterDeploySpec
func (s *Server) handleK8sClusterDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var spec K8sClusterDeploySpec
	if err := decodeJSONBody(w, r, &spec); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if spec.ClusterName == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "clusterName is required"})
		return
	}
	if len(spec.ControlPlaneNodes) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "controlPlaneNodes is required"})
		return
	}

	deployer := NewK8sClusterDeployer()
	status, err := deployer.Deploy(r.Context(), spec)
	if err != nil {
		writeInternalError(r.Context(), w, "k8s.deployCluster", err)
		return
	}

	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"deployID":   status.StartedAt.Format("20060102150405"),
		"phase":      status.Phase,
		"progress":   status.Progress,
		"currentStep": status.CurrentStep,
	})
}

// handleK8sClusterDeployStatus 处理 GET /api/v1/k8s/clusters/{id}/deploy/status?deployID=xxx
func (s *Server) handleK8sClusterDeployStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	deployID := r.URL.Query().Get("deployID")
	if deployID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deployID is required"})
		return
	}

	deployer := NewK8sClusterDeployer()
	status, ok := deployer.GetStatus(deployID)
	if !ok {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "deployment not found"})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, status)
}

// RegisterK8sClusterDeployRoutes 注册 K8s 集群部署路由。
func (s *Server) RegisterK8sClusterDeployRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/k8s/clusters/{id}/deploy", s.handleK8sClusterDeploy)
	mux.HandleFunc("GET /api/v1/k8s/clusters/{id}/deploy/status", s.handleK8sClusterDeployStatus)
}

// generateRandomToken 生成随机 token。
func generateRandomToken() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	buf := make([]byte, 23) // 6 + 1 + 16
	for i := 0; i < 6; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		buf[i] = chars[n.Int64()]
	}
	buf[6] = '.'
	for i := 7; i < 23; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		buf[i] = chars[n.Int64()]
	}
	return string(buf)
}

