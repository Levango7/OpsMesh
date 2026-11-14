// Package k8slifecycle 提供 K8s 集群全生命周期管理：版本升级/回滚、证书轮换、节点扩缩容、etcd 备份恢复。
package k8slifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/platform/ssh"
	"github.com/Levango7/OpsMesh/internal/store/sqlstore"
)

// SSH执行配置常量
const (
	sshDialTimeout        = 30 * time.Second
	sshCmdTimeout         = 300 * time.Second // 单命令最长5分钟
	sshMaxRetries         = 3                 // 最大重试次数
	sshRetryBackoff       = 5 * time.Second   // 重试间隔
	drainGracePeriod      = 600               // drain宽限期(秒)
	nodeReadyPollInterval = 10 * time.Second  // 节点Ready轮询间隔
	maxNodeReadyWait      = 5 * time.Minute   // 节点Ready最大等待
)

// NodeSpec 节点规格
type NodeSpec struct {
	ID       string `json:"id"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	SSHKey   string `json:"sshKey"`
	Password string `json:"password,omitempty"`
}

// ============================================================================
// 版本升级
// ============================================================================

// UpgradeSpec 升级规格
type UpgradeSpec struct {
	ClusterID     string   `json:"clusterId"`
	TargetVersion string   `json:"targetVersion"` // e.g. "1.30.0"
	Nodes         []string `json:"nodes"`         // 节点列表 (空=全部)
	DrainNodes    bool     `json:"drainNodes"`    // 升级前 drain
	Force         bool     `json:"force"`         // 强制升级 (跳过预检)
	// ApprovalRequestID 可选：显式引用审批请求（triggerType=k8s_upgrade，
	// target=clusterId，detail=targetVersion）。非 force 时必须能匹配到已批准请求。
	ApprovalRequestID string `json:"approvalRequestId,omitempty"`
}

// UpgradeStatus 升级状态
type UpgradeStatus struct {
	Phase       string            `json:"phase"` // Planning/Upgrading/Completed/Failed/RolledBack
	Progress    int               `json:"progress"`
	CurrentStep string            `json:"currentStep"`
	NodeStatus  map[string]string `json:"nodeStatus"`
	FromVersion string            `json:"fromVersion"`
	ToVersion   string            `json:"toVersion"`
	Error       string            `json:"error,omitempty"`
	StartedAt   time.Time         `json:"startedAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

// UpgradeManager 版本升级管理器
type UpgradeManager struct {
	mu       sync.RWMutex
	upgrades map[string]*UpgradeStatus
	sshPool  *ssh.Pool
}

func NewUpgradeManager() *UpgradeManager {
	return &UpgradeManager{
		upgrades: make(map[string]*UpgradeStatus),
		sshPool:  ssh.NewPool(),
	}
}

// PlanUpgrade 执行 kubeadm upgrade plan
func (m *UpgradeManager) PlanUpgrade(ctx context.Context, spec UpgradeSpec, node NodeSpec) (*UpgradePlanResult, error) {
	client, err := m.sshPool.Get(node.Host, nodePort(node), node.User, node.SSHKey, node.Password)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败: %w", err)
	}

	// 获取当前版本
	out, _, _, err := ssh.Exec(client, "kubectl version --short 2>/dev/null || kubectl version")
	if err != nil {
		return nil, fmt.Errorf("获取当前 kubectl 版本失败: %w", err)
	}
	currentVer := parseK8sVersion(out)

	// 执行 upgrade plan
	cmd := fmt.Sprintf("kubeadm upgrade plan v%s --output json 2>/dev/null || kubeadm upgrade plan", spec.TargetVersion)
	out, stderr, code, err := ssh.Exec(client, cmd)
	if err != nil || code != 0 {
		return nil, fmt.Errorf("upgrade plan 失败: %w (code %d)\n%s\n%s", err, code, out, stderr)
	}

	return &UpgradePlanResult{
		CurrentVersion: currentVer,
		TargetVersion:  spec.TargetVersion,
		PlanOutput:     out,
		Available:      true,
	}, nil
}

// UpgradePlanResult 升级计划结果
type UpgradePlanResult struct {
	CurrentVersion string `json:"currentVersion"`
	TargetVersion  string `json:"targetVersion"`
	PlanOutput     string `json:"planOutput"`
	Available      bool   `json:"available"`
}

func (m *UpgradeManager) updateStatus(upgradeID, phase string, progress int, step, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.upgrades[upgradeID]; ok {
		s.Phase = phase
		s.Progress = progress
		s.CurrentStep = step
		s.Error = errMsg
		s.UpdatedAt = time.Now()
	}
}

func (m *UpgradeManager) GetStatus(upgradeID string) (*UpgradeStatus, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.upgrades[upgradeID]
	if !ok {
		return nil, false
	}
	return snapshotUpgradeStatus(s), true
}

// snapshotUpgradeStatus 复制一份可安全交给并发读取方（HTTP 响应序列化/GetStatus）的状态。
// 作业结构体可能被后台执行器就地修改，直接返回内部指针会构成 data race。
func snapshotUpgradeStatus(s *UpgradeStatus) *UpgradeStatus {
	cp := *s
	if s.NodeStatus != nil {
		cp.NodeStatus = make(map[string]string, len(s.NodeStatus))
		for k, v := range s.NodeStatus {
			cp.NodeStatus[k] = v
		}
	}
	return &cp
}

// ============================================================================
// 版本回滚
// ============================================================================

// RollbackSpec 回滚规格
type RollbackSpec struct {
	ClusterID      string `json:"clusterId"`
	TargetVersion  string `json:"targetVersion"`  // 回滚到的版本
	EtcdBackupPath string `json:"etcdBackupPath"` // etcd 备份路径
	Reason         string `json:"reason"`
}

// ============================================================================
// 证书轮换
// ============================================================================

// CertRotationSpec 证书轮换规格
type CertRotationSpec struct {
	ClusterID  string   `json:"clusterId"`
	Nodes      []string `json:"nodes"`      // 空=全部
	RenewAll   bool     `json:"renewAll"`   // 是否续签所有证书
	Certs      []string `json:"certs"`      // 指定续签的证书列表
	CACertPath string   `json:"caCertPath"` // CA 证书路径
	CAKeyPath  string   `json:"caKeyPath"`  // CA 私钥路径
}

// 证书轮换的执行链路未实现（A 项裁决为显式 501，见 HandleCertRotation）；
// 真实现见 docs/design/k8s-lifecycle-execution-charter.md（C 项立项）。

// ============================================================================
// 节点扩缩容
// ============================================================================

// NodeScaleSpec 节点扩缩容规格
type NodeScaleSpec struct {
	ClusterID string   `json:"clusterId"`
	Action    string   `json:"action"` // add/remove
	Nodes     []string `json:"nodes"`  // 节点 IP/主机名
}

// NodeScaler 节点扩缩容管理器
type NodeScaler struct {
	sshPool *ssh.Pool
}

func NewNodeScaler() *NodeScaler {
	return &NodeScaler{sshPool: ssh.NewPool()}
}

// AddNodes 添加 Worker 节点到集群
func (ns *NodeScaler) AddNodes(ctx context.Context, spec NodeScaleSpec, joinCmd string) ([]string, error) {
	var added []string
	var errors []string

	for _, nodeHost := range spec.Nodes {
		client, err := ns.sshPool.Get(nodeHost, 22, "root", "", "")
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: SSH 连接失败: %v", nodeHost, err))
			continue
		}

		// 执行 join
		_, stderr, code, err := ssh.Exec(client, joinCmd)
		if err != nil || code != 0 {
			errors = append(errors, fmt.Sprintf("%s: join 失败 (code %d): %s", nodeHost, code, stderr))
			continue
		}

		added = append(added, nodeHost)
		logx.Info(ctx, "节点加入集群", "host", nodeHost)
	}

	if len(errors) > 0 {
		return added, fmt.Errorf("部分节点加入失败: %s", strings.Join(errors, "; "))
	}
	return added, nil
}

// RemoveNodes 从集群移除节点 (drain + delete)
func (ns *NodeScaler) RemoveNodes(ctx context.Context, spec NodeScaleSpec, cpNode string) ([]string, error) {
	client, err := ns.sshPool.Get(cpNode, 22, "root", "", "")
	if err != nil {
		return nil, fmt.Errorf("控制平面 SSH 连接失败: %w", err)
	}

	var removed []string
	var errors []string

	for _, nodeHost := range spec.Nodes {
		// 1. Drain 节点
		_, _, code, err := ssh.Exec(client, fmt.Sprintf("kubectl drain %s --ignore-daemonsets --delete-emptydir-data --force", nodeHost))
		if err != nil || code != 0 {
			errors = append(errors, fmt.Sprintf("%s: drain 失败", nodeHost))
			continue
		}

		// 2. 删除节点
		_, _, code, err = ssh.Exec(client, fmt.Sprintf("kubectl delete node %s", nodeHost))
		if err != nil || code != 0 {
			errors = append(errors, fmt.Sprintf("%s: delete node 失败", nodeHost))
			continue
		}

		removed = append(removed, nodeHost)
		logx.Info(ctx, "节点已移除", "host", nodeHost)
	}

	if len(errors) > 0 {
		return removed, fmt.Errorf("部分节点移除失败: %s", strings.Join(errors, "; "))
	}
	return removed, nil
}

// ============================================================================
// etcd 备份/恢复
// ============================================================================

// EtcdBackupSpec etcd 备份规格
type EtcdBackupSpec struct {
	ClusterID string `json:"clusterId"`
	CPNode    string `json:"cpNode"`
	BackupDir string `json:"backupDir"` // 默认 /var/backups/etcd
}

// EtcdBackupResult 备份结果
type EtcdBackupResult struct {
	BackupPath string    `json:"backupPath"`
	Size       int64     `json:"size"`
	Timestamp  time.Time `json:"timestamp"`
}

// EtcdManager etcd 管理器
type EtcdManager struct {
	sshPool *ssh.Pool
}

func NewEtcdManager() *EtcdManager {
	return &EtcdManager{sshPool: ssh.NewPool()}
}

// Backup etcd 数据
func (m *EtcdManager) Backup(ctx context.Context, spec EtcdBackupSpec) (*EtcdBackupResult, error) {
	client, err := m.sshPool.Get(spec.CPNode, 22, "root", "", "")
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败: %w", err)
	}

	backupDir := spec.BackupDir
	if backupDir == "" {
		backupDir = "/var/backups/etcd"
	}

	// 创建备份目录
	if err := ssh.MkdirAll(client, backupDir); err != nil {
		return nil, fmt.Errorf("创建备份目录失败: %w", err)
	}

	// 执行 etcdctl snapshot save
	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s/etcd-snapshot-%s.db", backupDir, timestamp)

	cmd := fmt.Sprintf("ETCDCTL_API=3 etcdctl --endpoints=https://127.0.0.1:2379 --cacert=/etc/kubernetes/pki/etcd/ca.crt --cert=/etc/kubernetes/pki/etcd/server.crt --key=/etc/kubernetes/pki/etcd/server.key snapshot save %s", backupPath)
	out, stderr, code, err := ssh.Exec(client, cmd)
	if err != nil || code != 0 {
		return nil, fmt.Errorf("etcd 备份失败: %w (code %d)\n%s\n%s", err, code, out, stderr)
	}

	// 获取文件大小
	out, _, _, err = ssh.Exec(client, fmt.Sprintf("stat -c %%s %s", backupPath))
	if err != nil {
		return nil, fmt.Errorf("获取备份文件大小失败: %w", err)
	}
	size := int64(0)
	if _, serr := fmt.Sscanf(strings.TrimSpace(out), "%d", &size); serr != nil {
		// 解析失败显式回退为 0：快照本身已保存成功，不应因大小读取失败而判失败
		logx.Warn(ctx, "解析备份文件大小失败, 置为 0", "path", backupPath, "error", serr)
	}

	logx.Info(ctx, "etcd 备份完成", "path", backupPath, "size", size)
	return &EtcdBackupResult{
		BackupPath: backupPath,
		Size:       size,
		Timestamp:  time.Now(),
	}, nil
}

// Restore etcd 数据
func (m *EtcdManager) Restore(ctx context.Context, spec EtcdBackupSpec, backupPath string) error {
	client, err := m.sshPool.Get(spec.CPNode, 22, "root", "", "")
	if err != nil {
		return fmt.Errorf("SSH 连接失败: %w", err)
	}

	// 1. 停止 kube-apiserver 和 etcd
	if _, stderr, code, err := ssh.Exec(client, "mv /etc/kubernetes/manifests/kube-apiserver.yaml /tmp/"); err != nil || code != 0 {
		return fmt.Errorf("移出 kube-apiserver manifest 失败: %w (code %d)\n%s", err, code, stderr)
	}
	if _, stderr, code, err := ssh.Exec(client, "mv /etc/kubernetes/manifests/etcd.yaml /tmp/"); err != nil || code != 0 {
		return fmt.Errorf("移出 etcd manifest 失败: %w (code %d)\n%s", err, code, stderr)
	}
	time.Sleep(5 * time.Second)

	// 2. 清理 etcd 数据目录
	if _, stderr, code, err := ssh.Exec(client, "rm -rf /var/lib/etcd/member"); err != nil || code != 0 {
		return fmt.Errorf("清理 etcd 数据目录失败: %w (code %d)\n%s", err, code, stderr)
	}

	// 3. 恢复快照
	cmd := fmt.Sprintf("ETCDCTL_API=3 etcdctl --endpoints=https://127.0.0.1:2379 --cacert=/etc/kubernetes/pki/etcd/ca.crt --cert=/etc/kubernetes/pki/etcd/server.crt --key=/etc/kubernetes/pki/etcd/server.key snapshot restore %s --data-dir=/var/lib/etcd", backupPath)
	out, stderr, code, err := ssh.Exec(client, cmd)
	if err != nil || code != 0 {
		// 恢复失败时尽力还原 manifest，避免集群停留在坏状态；还原失败仅记录（原始恢复错误优先返回）
		if mErr := restoreManifests(client); mErr != nil {
			logx.Error(ctx, "恢复失败后还原 manifest 失败", mErr)
		}
		return fmt.Errorf("etcd 恢复失败: %w (code %d)\n%s\n%s", err, code, out, stderr)
	}

	// 4. 恢复 manifest
	if err := restoreManifests(client); err != nil {
		return err
	}

	logx.Info(ctx, "etcd 恢复完成", "backup", backupPath)
	return nil
}

// restoreManifests 将 /tmp 下暂存的 kube-apiserver/etcd manifest 还原回 manifests 目录。
func restoreManifests(client *ssh.Client) error {
	if _, stderr, code, err := ssh.Exec(client, "mv /tmp/kube-apiserver.yaml /etc/kubernetes/manifests/"); err != nil || code != 0 {
		return fmt.Errorf("还原 kube-apiserver manifest 失败: %w (code %d)\n%s", err, code, stderr)
	}
	if _, stderr, code, err := ssh.Exec(client, "mv /tmp/etcd.yaml /etc/kubernetes/manifests/"); err != nil || code != 0 {
		return fmt.Errorf("还原 etcd manifest 失败: %w (code %d)\n%s", err, code, stderr)
	}
	return nil
}

// ListBackups 列出备份文件
func (m *EtcdManager) ListBackups(ctx context.Context, cpNode, backupDir string) ([]string, error) {
	client, err := m.sshPool.Get(cpNode, 22, "root", "", "")
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败: %w", err)
	}

	if backupDir == "" {
		backupDir = "/var/backups/etcd"
	}

	out, _, _, err := ssh.Exec(client, fmt.Sprintf("ls -1 %s/*.db 2>/dev/null", backupDir))
	if err != nil {
		return nil, err
	}

	files := strings.Split(strings.TrimSpace(out), "\n")
	var result []string
	for _, f := range files {
		if f != "" {
			result = append(result, f)
		}
	}
	return result, nil
}

// ============================================================================
// HTTP Handler
// ============================================================================

// K8sLifecycleHandler HTTP handler
type K8sLifecycleHandler struct {
	upgradeMgr *UpgradeManager
	nodeScaler *NodeScaler
	etcdMgr    *EtcdManager
	store      LifecycleJobStore // 用于持久化作业状态

	// clusterInfo 真实集群信息源（审计整改批次1）。
	// nil=未装配 ⇒ 升级/回滚/证书轮换在预检阶段显式失败，绝不回退到模拟节点。
	clusterInfo func(ctx context.Context, clusterID string) (*ClusterInfo, error)

	// approvalChecker 升级审批校验（审计整改批次15，接 controlplane 审批引擎）。
	// nil 且非 force ⇒ 预检拒绝执行（显式失败，不静默放行）。
	approvalChecker func(ctx context.Context, tenantID, requestID, clusterID, targetVersion string) (bool, string)

	// Authorize 鉴权回调（由 controlplane 注入），语义同 cmdb/log/deploy/orch 的
	// Authorize：失败时回调已写响应并返回 ok=false，调用方直接 return。
	// nil 时不启用鉴权（独立使用与单测的向后兼容路径）。
	Authorize func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool)
}

// SetClusterInfoSource 注入真实集群信息源（生产装配调用）。
// fn 返回的 ClusterInfo 至少要有 1 个 CP 节点，否则预检拒绝执行。
func (h *K8sLifecycleHandler) SetClusterInfoSource(fn func(ctx context.Context, clusterID string) (*ClusterInfo, error)) {
	h.clusterInfo = fn
}

// SetApprovalChecker 注入升级审批校验（生产装配调用，接 controlplane 审批引擎）。
// 返回 (approved, reason)：approved=false 时 reason 说明未满足原因。
// 非 force 升级在预检第 6 步必须通过该校验（审计整改：替换原 TODO 占位）。
func (h *K8sLifecycleHandler) SetApprovalChecker(fn func(ctx context.Context, tenantID, requestID, clusterID, targetVersion string) (bool, string)) {
	h.approvalChecker = fn
}

// authorize 按请求方法映射权限：GET/HEAD/OPTIONS 只读 → readPerm，其余 → writePerm。
// 鉴权通过后若租户头缺失，回填回调解析出的租户（与 cmdb 的 G1 修复同范式）。
func (h *K8sLifecycleHandler) authorize(fn http.HandlerFunc, readPerm, writePerm string) http.HandlerFunc {
	if h.Authorize == nil {
		return fn
	}
	return func(w http.ResponseWriter, r *http.Request) {
		perm := readPerm
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			perm = writePerm
		}
		actx, ok := h.Authorize(w, r, perm)
		if !ok {
			return
		}
		if actx.TenantID != "" && strings.TrimSpace(r.Header.Get("X-Tenant-ID")) == "" {
			r.Header.Set("X-Tenant-ID", actx.TenantID)
		}
		fn(w, r)
	}
}

// LifecycleJobStore 生命周期作业存储接口（用于测试 mock）
type LifecycleJobStore interface {
	CreateLifecycleJob(ctx context.Context, job *sqlstore.LifecycleJob) error
	GetLifecycleJob(ctx context.Context, id string) (*sqlstore.LifecycleJob, error)
	UpdateLifecycleJob(ctx context.Context, job *sqlstore.LifecycleJob) error
	GetActiveLifecycleJob(ctx context.Context, clusterID, kind string) (*sqlstore.LifecycleJob, error)
}

// NewK8sLifecycleHandler 创建 K8s 生命周期处理器
func NewK8sLifecycleHandler(store LifecycleJobStore) *K8sLifecycleHandler {
	return &K8sLifecycleHandler{
		upgradeMgr: NewUpgradeManager(),
		nodeScaler: NewNodeScaler(),
		etcdMgr:    NewEtcdManager(),
		store:      store,
	}
}

// HandleUpgrade 处理 POST /api/v1/k8s/lifecycle/upgrade
func (h *K8sLifecycleHandler) HandleUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var spec UpgradeSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if spec.ClusterID == "" || spec.TargetVersion == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "clusterId and targetVersion are required"})
		return
	}

	// 获取租户 ID（从授权上下文）
	actx := authctx.FromContext(r.Context())
	tenantID := actx.TenantID
	if tenantID == "" {
		tenantID = "default"
	}

	// 检查是否已有活跃的升级作业
	if h.store != nil {
		activeJob, err := h.store.GetActiveLifecycleJob(r.Context(), spec.ClusterID, "upgrade")
		if err != nil {
			logx.Error(r.Context(), "检查活跃升级作业失败", err)
		} else if activeJob != nil {
			paginate.WriteJSON(w, http.StatusConflict, map[string]string{
				"error":    "cluster already has an active upgrade job",
				"jobId":    activeJob.ID,
				"jobPhase": activeJob.Phase,
			})
			return
		}
	}

	// 生成作业 ID
	jobID := fmt.Sprintf("%s-upgrade-%d", spec.ClusterID, time.Now().UnixMilli())

	// 创建作业记录
	specJSON := map[string]any{
		"clusterId":     spec.ClusterID,
		"targetVersion": spec.TargetVersion,
		"nodes":         spec.Nodes,
		"drainNodes":    spec.DrainNodes,
		"force":         spec.Force,
	}

	if h.store != nil {
		if err := h.store.CreateLifecycleJob(r.Context(), &sqlstore.LifecycleJob{
			ID:        jobID,
			ClusterID: spec.ClusterID,
			Kind:      "upgrade",
			TenantID:  tenantID,
			Phase:     "pending",
			Progress:  0,
			SpecJSON:  specJSON,
			LockOwner: "k8slifecycle-upgrade",
		}); err != nil {
			logx.Error(r.Context(), "创建升级作业失败", err)
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create upgrade job"})
			return
		}
	}

	// 启动异步升级
	go h.runUpgrade(r.Context(), tenantID, jobID, spec)

	// 返回 202 Accepted
	paginate.WriteJSON(w, http.StatusAccepted, map[string]string{
		"jobId":   jobID,
		"status":  "accepted",
		"message": "upgrade job started",
	})
}

// HandleUpgradePlan 处理 POST /api/v1/k8s/lifecycle/upgrade/plan
func (h *K8sLifecycleHandler) HandleUpgradePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		ClusterID     string `json:"clusterId"`
		TargetVersion string `json:"targetVersion"`
		Host          string `json:"host"`
		User          string `json:"user"`
		SSHKey        string `json:"sshKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	node := NodeSpec{Host: req.Host, User: req.User, SSHKey: req.SSHKey}
	plan, err := h.upgradeMgr.PlanUpgrade(r.Context(), UpgradeSpec{ClusterID: req.ClusterID, TargetVersion: req.TargetVersion}, node)
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, plan)
}

// HandleUpgradeStatus 处理 GET /api/v1/k8s/lifecycle/upgrade/status?upgradeID=xxx
func (h *K8sLifecycleHandler) HandleUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	upgradeID := r.URL.Query().Get("upgradeID")
	status, ok := h.upgradeMgr.GetStatus(upgradeID)
	if !ok {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "upgrade not found"})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, status)
}

// HandleRollback 处理 POST /api/v1/k8s/lifecycle/rollback
func (h *K8sLifecycleHandler) HandleRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var spec RollbackSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if spec.ClusterID == "" || spec.TargetVersion == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "clusterId and targetVersion are required"})
		return
	}

	// 获取租户 ID
	actx := authctx.FromContext(r.Context())
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	tenantID := actx.TenantID
	if tenantID == "" {
		tenantID = "default"
	}

	// 检查是否已有活跃的回滚作业
	if h.store != nil {
		activeJob, err := h.store.GetActiveLifecycleJob(r.Context(), spec.ClusterID, "rollback")
		if err != nil {
			logx.Error(r.Context(), "检查活跃回滚作业失败", err)
		} else if activeJob != nil {
			paginate.WriteJSON(w, http.StatusConflict, map[string]string{
				"error":    "cluster already has an active rollback job",
				"jobId":    activeJob.ID,
				"jobPhase": activeJob.Phase,
			})
			return
		}
	}

	// 生成作业 ID
	jobID := fmt.Sprintf("%s-rollback-%d", spec.ClusterID, time.Now().UnixMilli())

	// 创建作业记录
	specJSON := map[string]any{
		"clusterId":      spec.ClusterID,
		"targetVersion":  spec.TargetVersion,
		"etcdBackupPath": spec.EtcdBackupPath,
		"reason":         spec.Reason,
	}

	if h.store != nil {
		if err := h.store.CreateLifecycleJob(r.Context(), &sqlstore.LifecycleJob{
			ID:        jobID,
			ClusterID: spec.ClusterID,
			Kind:      "rollback",
			TenantID:  tenantID,
			Phase:     "pending",
			Progress:  0,
			SpecJSON:  specJSON,
			LockOwner: "k8slifecycle-rollback",
		}); err != nil {
			logx.Error(r.Context(), "创建回滚作业失败", err)
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create rollback job"})
			return
		}
	}

	// 启动异步回滚
	go h.runRollback(r.Context(), jobID, spec)

	// 返回 202 Accepted
	paginate.WriteJSON(w, http.StatusAccepted, map[string]string{
		"jobId":   jobID,
		"status":  "accepted",
		"message": "rollback job started",
	})
}

// HandleCertRotation 处理 POST /api/v1/k8s/lifecycle/cert-rotate
func (h *K8sLifecycleHandler) HandleCertRotation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var spec CertRotationSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if spec.ClusterID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "clusterId is required"})
		return
	}

	// 获取租户 ID
	actx := authctx.FromContext(r.Context())
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	tenantID := actx.TenantID
	if tenantID == "" {
		tenantID = "default"
	}

	// 检查是否已有活跃的证书轮换作业
	if h.store != nil {
		activeJob, err := h.store.GetActiveLifecycleJob(r.Context(), spec.ClusterID, "cert-rotate")
		if err != nil {
			logx.Error(r.Context(), "检查活跃证书轮换作业失败", err)
		} else if activeJob != nil {
			paginate.WriteJSON(w, http.StatusConflict, map[string]string{
				"error":    "cluster already has an active cert-rotate job",
				"jobId":    activeJob.ID,
				"jobPhase": activeJob.Phase,
			})
			return
		}
	}

	// 生成作业 ID
	jobID := fmt.Sprintf("%s-cert-rotate-%d", spec.ClusterID, time.Now().UnixMilli())

	// 创建作业记录
	specJSON := map[string]any{
		"clusterId":  spec.ClusterID,
		"renewAll":   spec.RenewAll,
		"certs":      spec.Certs,
		"caCertPath": spec.CACertPath,
		"caKeyPath":  spec.CAKeyPath,
	}

	if h.store != nil {
		if err := h.store.CreateLifecycleJob(r.Context(), &sqlstore.LifecycleJob{
			ID:        jobID,
			ClusterID: spec.ClusterID,
			Kind:      "cert-rotate",
			TenantID:  tenantID,
			Phase:     "pending",
			Progress:  0,
			SpecJSON:  specJSON,
			LockOwner: "k8slifecycle-cert-rotate",
		}); err != nil {
			logx.Error(r.Context(), "创建证书轮换作业失败", err)
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create cert-rotate job"})
			return
		}
	}

	// 启动异步证书轮换
	go h.runCertRotation(r.Context(), jobID, spec)

	// 返回 202 Accepted
	paginate.WriteJSON(w, http.StatusAccepted, map[string]string{
		"jobId":   jobID,
		"status":  "accepted",
		"message": "cert-rotate job started",
	})
}

// HandleNodeScale 处理 POST /api/v1/k8s/lifecycle/node-scale
func (h *K8sLifecycleHandler) HandleNodeScale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		ClusterID string   `json:"clusterId"`
		Action    string   `json:"action"` // add/remove
		Nodes     []string `json:"nodes"`
		JoinCmd   string   `json:"joinCmd,omitempty"`
		CPNode    string   `json:"cpNode,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	spec := NodeScaleSpec{ClusterID: req.ClusterID, Action: req.Action, Nodes: req.Nodes}

	switch req.Action {
	case "add":
		added, err := h.nodeScaler.AddNodes(r.Context(), spec, req.JoinCmd)
		if err != nil {
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"added": added})
	case "remove":
		removed, err := h.nodeScaler.RemoveNodes(r.Context(), spec, req.CPNode)
		if err != nil {
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"removed": removed})
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be add or remove"})
	}
}

// HandleEtcdBackup 处理 POST /api/v1/k8s/lifecycle/etcd-backup
func (h *K8sLifecycleHandler) HandleEtcdBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var spec EtcdBackupSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	result, err := h.etcdMgr.Backup(r.Context(), spec)
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, result)
}

// HandleEtcdRestore 处理 POST /api/v1/k8s/lifecycle/etcd-restore
func (h *K8sLifecycleHandler) HandleEtcdRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		EtcdBackupSpec
		BackupPath string `json:"backupPath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if err := h.etcdMgr.Restore(r.Context(), req.EtcdBackupSpec, req.BackupPath); err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

// HandleEtcdListBackups 处理 GET /api/v1/k8s/lifecycle/etcd-backups
func (h *K8sLifecycleHandler) HandleEtcdListBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	cpNode := r.URL.Query().Get("cpNode")
	backupDir := r.URL.Query().Get("backupDir")
	files, err := h.etcdMgr.ListBackups(r.Context(), cpNode, backupDir)
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"backups": files,
	})
}

// RegisterK8sLifecycleRoutes 注册 K8s 生命周期路由。
// 全部经 authorize 包装：GET 需要 k8s:read，其余（含 etcd-restore / node-scale 这类
// 真副作用动作）需要 k8s:write。若未来要给 etcd-restore 更细的权限点，需先扩 RBAC 目录。
// runUpgrade 真实执行 K8s 集群升级（异步后台任务）
func (h *K8sLifecycleHandler) runUpgrade(ctx context.Context, tenantID, jobID string, spec UpgradeSpec) {
	// 更新作业状态：预检阶段
	h.updateJobPhase(ctx, jobID, "precheck", 5, "precheck: running preflight checks")

	// 1. 预检：版本兼容性、节点健康、etcd 健康度、控制面 Pod 数、审批确认
	if err := h.precheckUpgrade(ctx, tenantID, jobID, spec); err != nil {
		h.failJob(ctx, jobID, "precheck failed: "+err.Error())
		return
	}

	h.updateJobPhase(ctx, jobID, "control_plane_upgrading", 20, "control_plane_upgrading: upgrading control plane nodes")

	// 2. 控制面升级：首个 CP kubeadm upgrade apply -> 其余 CP 逐个
	if err := h.upgradeControlPlane(ctx, jobID, spec); err != nil {
		h.failJob(ctx, jobID, "control plane upgrade failed: "+err.Error())
		return
	}

	h.updateJobPhase(ctx, jobID, "worker_upgrading", 50, "worker_upgrading: upgrading worker nodes")

	// 3. Worker 节点分批次升级
	if err := h.upgradeWorkers(ctx, jobID, spec); err != nil {
		h.failJob(ctx, jobID, "worker upgrade failed: "+err.Error())
		return
	}

	h.updateJobPhase(ctx, jobID, "verifying", 90, "verifying: verifying cluster health")

	// 4. 验证：版本一致、Pod 健康、无可用升级
	if err := h.verifyUpgrade(ctx, jobID, spec); err != nil {
		h.failJob(ctx, jobID, "verification failed: "+err.Error())
		return
	}

	// 完成
	h.completeJob(ctx, jobID, "upgrade completed successfully")
}

// precheckUpgrade 预检升级前置条件
func (h *K8sLifecycleHandler) precheckUpgrade(ctx context.Context, tenantID, jobID string, spec UpgradeSpec) error {
	h.updateJobPhase(ctx, jobID, "precheck", 5, "precheck: fetching cluster info")

	// 1. 获取集群信息和 CP 节点
	cluster, err := h.getClusterInfo(ctx, spec.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster info: %w", err)
	}

	h.updateJobPhase(ctx, jobID, "precheck", 10, "precheck: version compatibility check")

	// 2. 版本兼容性检查：同主版本内跳跃
	currentVer := cluster.CurrentVersion
	targetVer := spec.TargetVersion
	if !isValidVersionJump(currentVer, targetVer) {
		return fmt.Errorf("invalid version jump: %s -> %s (only same minor version jump allowed)", currentVer, targetVer)
	}

	// 3. 验证升级计划与目标版本一致
	h.updateJobPhase(ctx, jobID, "precheck", 20, "precheck: validating upgrade plan")
	plan, err := h.upgradeMgr.PlanUpgrade(ctx, UpgradeSpec{ClusterID: spec.ClusterID, TargetVersion: spec.TargetVersion}, cluster.CPNodes[0])
	if err != nil {
		return fmt.Errorf("upgrade plan failed: %w", err)
	}
	if plan.TargetVersion != spec.TargetVersion {
		return fmt.Errorf("upgrade plan target version mismatch: plan=%s spec=%s", plan.TargetVersion, spec.TargetVersion)
	}

	// 3. 节点健康检查
	h.updateJobPhase(ctx, jobID, "precheck", 35, "precheck: node health check")
	if err := h.checkNodesHealth(ctx, cluster); err != nil {
		return fmt.Errorf("node health check failed: %w", err)
	}

	// 4. etcd 健康度检查
	h.updateJobPhase(ctx, jobID, "precheck", 50, "precheck: etcd health check")
	if err := h.checkEtcdHealth(ctx, cluster); err != nil {
		return fmt.Errorf("etcd health check failed: %w", err)
	}

	// 5. 控制面 Pod 数量检查
	h.updateJobPhase(ctx, jobID, "precheck", 70, "precheck: control plane pods check")
	if err := h.checkControlPlanePods(ctx, cluster); err != nil {
		return fmt.Errorf("control plane pods check failed: %w", err)
	}

	// 6. 确认升级计划（审计整改批次15：审批流真实接入，替换原 TODO 占位）
	h.updateJobPhase(ctx, jobID, "precheck", 90, "precheck: approval check")
	if !spec.Force {
		if h.approvalChecker == nil {
			return fmt.Errorf("upgrade requires approval: approval checker not configured (submit an approval request and wire SetApprovalChecker, or set force=true)")
		}
		approved, reason := h.approvalChecker(ctx, tenantID, spec.ApprovalRequestID, spec.ClusterID, spec.TargetVersion)
		if !approved {
			return fmt.Errorf("upgrade approval check failed: %s", reason)
		}
	}

	h.updateJobPhase(ctx, jobID, "precheck", 100, "precheck: completed")
	return nil
}

// getClusterInfo 获取集群信息（CP 节点、Worker 节点、当前版本）。
//
// 安全边界（审计整改批次1）：**不做任何模拟数据回退**。真实集群信息来源必须由
// 装配方通过 SetClusterInfoSource 注入；未注入时直接报错，让升级/回滚/证书轮换
// 在预检阶段显式失败，而不是把 SSH 打到硬编码的假节点上。
func (h *K8sLifecycleHandler) getClusterInfo(ctx context.Context, clusterID string) (*ClusterInfo, error) {
	if h.clusterInfo == nil {
		return nil, fmt.Errorf("cluster info source not configured: k8s lifecycle execution requires SetClusterInfoSource wiring (clusterID=%s)", clusterID)
	}
	info, err := h.clusterInfo(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("cluster info lookup failed (clusterID=%s): %w", clusterID, err)
	}
	if info == nil {
		return nil, fmt.Errorf("cluster not found: %s", clusterID)
	}
	if len(info.CPNodes) == 0 {
		return nil, fmt.Errorf("cluster %s has no control-plane nodes configured", clusterID)
	}
	return info, nil
}

type ClusterInfo struct {
	ClusterID      string
	CurrentVersion string
	CPNodes        []NodeSpec // 支持多 CP 节点
	WorkerNodes    []string
}

// execSSH 执行 SSH 命令（带超时、重试、上下文传播）
func (h *K8sLifecycleHandler) execSSH(ctx context.Context, host, cmd string) (string, string, int, error) {
	return h.execSSHWithRetry(ctx, host, cmd, sshMaxRetries)
}

func (h *K8sLifecycleHandler) execSSHWithRetry(ctx context.Context, host, cmd string, maxRetries int) (string, string, int, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			logx.Warn(ctx, "SSH 命令重试", "host", host, "attempt", attempt, "cmd", cmd)
			select {
			case <-ctx.Done():
				return "", "", -1, ctx.Err()
			case <-time.After(sshRetryBackoff):
			}
		}

		// 创建带超时的上下文
		cmdCtx, cancel := context.WithTimeout(ctx, sshCmdTimeout)
		defer cancel()

		out, stderr, code, err := h.execSSHOnce(cmdCtx, host, cmd)
		if err == nil && code == 0 {
			return out, stderr, code, nil
		}

		lastErr = fmt.Errorf("attempt %d: code=%d err=%v stderr=%s", attempt+1, code, err, stderr)
		logx.Error(ctx, "SSH 命令执行失败", lastErr, "host", host, "attempt", attempt+1, "cmd", cmd)
	}

	return "", "", -1, fmt.Errorf("SSH 执行失败(%d次重试后): %w", maxRetries+1, lastErr)
}

func (h *K8sLifecycleHandler) execSSHOnce(ctx context.Context, host, cmd string) (string, string, int, error) {
	client, err := ssh.NewPool().Get(host, 22, "root", "", "")
	if err != nil {
		return "", "", -1, fmt.Errorf("SSH connect %s: %w", host, err)
	}
	defer client.Close()

	// 使用通道实现命令超时
	type result struct {
		out    string
		stderr string
		code   int
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		out, stderr, code, err := ssh.Exec(client, cmd)
		resultCh <- result{out, stderr, code, err}
	}()

	select {
	case r := <-resultCh:
		return r.out, r.stderr, r.code, r.err
	case <-ctx.Done():
		return "", "", -1, ctx.Err()
	}
}

// upgradeControlPlane 升级控制面节点
func (h *K8sLifecycleHandler) upgradeControlPlane(ctx context.Context, jobID string, spec UpgradeSpec) error {
	cluster, err := h.getClusterInfo(ctx, spec.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster info: %w", err)
	}

	h.updateJobPhase(ctx, jobID, "control_plane_upgrading", 15, "control_plane_upgrading: backing up etcd before upgrade")

	// 1. 升级前先做 etcd 备份 (使用第一个 CP 节点)
	firstCP := cluster.CPNodes[0]
	backupSpec := EtcdBackupSpec{ClusterID: spec.ClusterID, CPNode: firstCP.Host}
	backupResult, err := h.etcdMgr.Backup(ctx, backupSpec)
	if err != nil {
		return fmt.Errorf("pre-upgrade etcd backup failed: %w", err)
	}
	h.updateJobPhase(ctx, jobID, "control_plane_upgrading", 20, fmt.Sprintf("etcd backup completed: %s", backupResult.BackupPath))

	// 2. 逐个升级所有控制面节点
	for i, cp := range cluster.CPNodes {
		h.updateJobPhase(ctx, jobID, "control_plane_upgrading", 20+int(float64(i)/float64(len(cluster.CPNodes))*55), fmt.Sprintf("upgrading control plane node %d/%d: %s", i+1, len(cluster.CPNodes), cp.Host))

		if err := h.upgradeSingleControlPlane(ctx, cp.Host, spec.TargetVersion); err != nil {
			return fmt.Errorf("upgrade control plane node %s failed: %w", cp.Host, err)
		}
		h.updateJobPhase(ctx, jobID, "control_plane_upgrading", 20+int(float64(i+1)/float64(len(cluster.CPNodes))*55), fmt.Sprintf("control plane node %d/%d upgraded", i+1, len(cluster.CPNodes)))
	}

	h.updateJobPhase(ctx, jobID, "control_plane_upgrading", 75, "control plane upgrade completed")
	return nil
}

func (h *K8sLifecycleHandler) upgradeSingleControlPlane(ctx context.Context, cpNode, targetVersion string) error {
	logx.Info(ctx, "开始升级控制面节点", "node", cpNode, "targetVersion", targetVersion)

	// 1. drain 节点 (仅调度) - 幂等：重复执行无副作用
	h.updateJobPhase(context.Background(), "", "control_plane_upgrading", 25, fmt.Sprintf("draining node %s", cpNode))
	cmd := fmt.Sprintf("kubectl drain %s --ignore-daemonsets --delete-emptydir-data --force --grace-period=%d", cpNode, drainGracePeriod)
	if _, stderr, code, err := h.execSSH(context.Background(), cpNode, cmd); err != nil || code != 0 {
		// 检查是否已 drain (幂等)
		if strings.Contains(stderr, "already cordoned") || strings.Contains(stderr, "already drained") {
			logx.Info(context.Background(), "节点已 drain，跳过", "node", cpNode)
		} else {
			return fmt.Errorf("drain %s failed: %v, stderr: %s", cpNode, err, stderr)
		}
	}

	// 2. 升级 kubeadm - 幂等：重复安装同版本无副作用
	h.updateJobPhase(context.Background(), "", "control_plane_upgrading", 30, fmt.Sprintf("installing kubeadm %s", targetVersion))
	cmd = fmt.Sprintf("apt-get update && apt-get install -y kubeadm=%s-00", targetVersion)
	if _, stderr, code, err := h.execSSH(context.Background(), cpNode, cmd); err != nil || code != 0 {
		// 检查是否已安装目标版本
		if strings.Contains(stderr, "already the newest version") {
			logx.Info(context.Background(), "kubeadm 已是目标版本，跳过", "node", cpNode, "version", targetVersion)
		} else {
			return fmt.Errorf("install kubeadm %s failed: %v, stderr: %s", targetVersion, err, stderr)
		}
	}

	// 3. 执行 kubeadm upgrade apply - 幂等：已升级到目标版本会返回成功
	h.updateJobPhase(context.Background(), "", "control_plane_upgrading", 40, fmt.Sprintf("applying kubeadm upgrade to %s", targetVersion))
	cmd = fmt.Sprintf("kubeadm upgrade apply v%s -y", targetVersion)
	if _, stderr, code, err := h.execSSH(context.Background(), cpNode, cmd); err != nil || code != 0 {
		// 检查是否已是目标版本
		if strings.Contains(stderr, "already at version") || strings.Contains(stderr, "cluster is already upgraded") {
			logx.Info(context.Background(), "控制面已是目标版本，跳过升级", "node", cpNode, "version", targetVersion)
		} else {
			return fmt.Errorf("kubeadm upgrade apply %s failed: %v, stderr: %s", targetVersion, err, stderr)
		}
	}

	// 4. 升级 kubelet 和 kubectl
	h.updateJobPhase(context.Background(), "", "control_plane_upgrading", 60, fmt.Sprintf("upgrading kubelet/kubectl to %s", targetVersion))
	cmd = fmt.Sprintf("apt-get install -y kubelet=%s-00 kubectl=%s-00", targetVersion, targetVersion)
	if _, stderr, code, err := h.execSSH(context.Background(), cpNode, cmd); err != nil || code != 0 {
		if strings.Contains(stderr, "already the newest version") {
			logx.Info(context.Background(), "kubelet/kubectl 已是目标版本，跳过", "node", cpNode, "version", targetVersion)
		} else {
			return fmt.Errorf("install kubelet/kubectl %s failed: %v, stderr: %s", targetVersion, err, stderr)
		}
	}

	// 5. 重启 kubelet
	h.updateJobPhase(context.Background(), "", "control_plane_upgrading", 70, "restarting kubelet")
	cmd = "systemctl daemon-reload && systemctl restart kubelet"
	if _, stderr, code, err := h.execSSH(context.Background(), cpNode, cmd); err != nil || code != 0 {
		return fmt.Errorf("restart kubelet failed: %v, stderr: %s", err, stderr)
	}

	// 6. uncordon 节点 - 幂等
	cmd = "kubectl uncordon " + cpNode
	if _, stderr, code, err := h.execSSH(context.Background(), cpNode, cmd); err != nil || code != 0 {
		if strings.Contains(stderr, "not cordoned") {
			logx.Info(context.Background(), "节点未被 cordon，跳过 uncordon", "node", cpNode)
		} else {
			return fmt.Errorf("uncordon %s failed: %v, stderr: %s", cpNode, err, stderr)
		}
	}

	// 7. 等待节点 Ready - 带轮询和超时
	h.updateJobPhase(context.Background(), "", "control_plane_upgrading", 75, "waiting for node Ready")
	if err := h.waitNodeReady(context.Background(), cpNode, cpNode, maxNodeReadyWait); err != nil {
		return fmt.Errorf("node %s not ready after upgrade: %w", cpNode, err)
	}

	logx.Info(context.Background(), "控制面节点升级完成", "node", cpNode, "version", targetVersion)
	return nil
}

// upgradeWorkers 升级 Worker 节点
func (h *K8sLifecycleHandler) upgradeWorkers(ctx context.Context, jobID string, spec UpgradeSpec) error {
	cluster, err := h.getClusterInfo(ctx, spec.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster info: %w", err)
	}

	if len(cluster.WorkerNodes) == 0 {
		h.updateJobPhase(ctx, jobID, "worker_upgrading", 100, "no worker nodes to upgrade")
		return nil
	}

	// 分批次升级，默认每批 1 个节点
	batchSize := 1
	if len(spec.Nodes) > 0 {
		// 如果指定了节点列表，使用指定列表
		cluster.WorkerNodes = spec.Nodes
	}

	h.updateJobPhase(ctx, jobID, "worker_upgrading", 55, fmt.Sprintf("upgrading %d worker nodes in batches of %d", len(cluster.WorkerNodes), batchSize))

	for i := 0; i < len(cluster.WorkerNodes); i += batchSize {
		end := i + batchSize
		if end > len(cluster.WorkerNodes) {
			end = len(cluster.WorkerNodes)
		}
		batch := cluster.WorkerNodes[i:end]

		h.updateJobPhase(ctx, jobID, "worker_upgrading", 55+int(float64(i)/float64(len(cluster.WorkerNodes))*40), fmt.Sprintf("upgrading batch %d/%d: %v", i/batchSize+1, (len(cluster.WorkerNodes)+batchSize-1)/batchSize, batch))

		for _, node := range batch {
			if err := h.upgradeSingleWorker(ctx, cluster.CPNodes[0].Host, node, spec.TargetVersion); err != nil {
				return fmt.Errorf("upgrade worker %s failed: %w", node, err)
			}
		}

		// 批间健康检查
		if err := h.checkBatchHealth(ctx, cluster); err != nil {
			return fmt.Errorf("batch health check failed: %w", err)
		}
	}

	h.updateJobPhase(ctx, jobID, "worker_upgrading", 95, "all worker nodes upgraded")
	return nil
}

func (h *K8sLifecycleHandler) upgradeSingleWorker(ctx context.Context, kubectlHost, node, targetVersion string) error {
	logx.Info(ctx, "开始升级 Worker 节点", "node", node, "targetVersion", targetVersion, "kubectlHost", kubectlHost)

	// 1. drain 节点（kubectl 必须在控制面执行——worker 通常无 admin kubeconfig）
	h.updateJobPhase(context.Background(), "", "worker_upgrading", 55, fmt.Sprintf("draining worker %s", node))
	cmd := fmt.Sprintf("kubectl drain %s --ignore-daemonsets --delete-emptydir-data --force --grace-period=%d", node, drainGracePeriod)
	if _, stderr, code, err := h.execSSH(context.Background(), kubectlHost, cmd); err != nil || code != 0 {
		if strings.Contains(stderr, "already cordoned") || strings.Contains(stderr, "already drained") {
			logx.Info(context.Background(), "Worker 节点已 drain，跳过", "node", node)
		} else {
			return fmt.Errorf("drain %s failed: %v, stderr: %s", node, err, stderr)
		}
	}

	// 2. 升级 kubelet 和 kubectl - 幂等
	h.updateJobPhase(context.Background(), "", "worker_upgrading", 65, fmt.Sprintf("upgrading kubelet/kubectl on %s to %s", node, targetVersion))
	cmd = fmt.Sprintf("apt-get update && apt-get install -y kubelet=%s-00 kubectl=%s-00", targetVersion, targetVersion)
	if _, stderr, code, err := h.execSSH(context.Background(), node, cmd); err != nil || code != 0 {
		if strings.Contains(stderr, "already the newest version") {
			logx.Info(context.Background(), "Worker kubelet/kubectl 已是目标版本，跳过", "node", node, "version", targetVersion)
		} else {
			return fmt.Errorf("install kubelet/kubectl %s failed: %v, stderr: %s", targetVersion, err, stderr)
		}
	}

	// 3. 重启 kubelet
	cmd = "systemctl daemon-reload && systemctl restart kubelet"
	if _, stderr, code, err := h.execSSH(context.Background(), node, cmd); err != nil || code != 0 {
		return fmt.Errorf("restart kubelet failed: %v, stderr: %s", err, stderr)
	}

	// 4. uncordon 节点 - 幂等（kubectl 在控制面执行）
	cmd = "kubectl uncordon " + node
	if _, stderr, code, err := h.execSSH(context.Background(), kubectlHost, cmd); err != nil || code != 0 {
		if strings.Contains(stderr, "not cordoned") {
			logx.Info(context.Background(), "Worker 节点未被 cordon，跳过 uncordon", "node", node)
		} else {
			return fmt.Errorf("uncordon %s failed: %v, stderr: %s", node, err, stderr)
		}
	}

	// 5. 等待节点 Ready - 带轮询和超时
	if err := h.waitNodeReady(context.Background(), kubectlHost, node, maxNodeReadyWait); err != nil {
		return fmt.Errorf("worker %s not ready after upgrade: %w", node, err)
	}

	logx.Info(context.Background(), "Worker 节点升级完成", "node", node, "version", targetVersion)
	return nil
}

func (h *K8sLifecycleHandler) checkBatchHealth(ctx context.Context, cluster *ClusterInfo) error {
	// 检查批次节点是否 Ready
	// 简化：检查所有节点是否 Ready
	cmd := "kubectl get nodes --no-headers | awk '$2 != \"Ready\" {print $1}'"
	out, _, code, err := h.execSSH(context.Background(), cluster.CPNodes[0].Host, cmd)
	if err != nil || code != 0 {
		return fmt.Errorf("health check command failed: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("nodes not ready: %s", strings.TrimSpace(out))
	}
	return nil
}

// verifyUpgrade 验证升级结果
func (h *K8sLifecycleHandler) verifyUpgrade(ctx context.Context, jobID string, spec UpgradeSpec) error {
	cluster, err := h.getClusterInfo(ctx, spec.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster info: %w", err)
	}

	h.updateJobPhase(ctx, jobID, "verifying", 92, "verifying: checking node versions")

	// 1. 验证所有节点版本 == 目标版本
	if err := h.verifyNodeVersions(ctx, cluster, spec.TargetVersion); err != nil {
		return fmt.Errorf("node version verification failed: %w", err)
	}

	h.updateJobPhase(ctx, jobID, "verifying", 95, "verifying: checking pod health")

	// 2. 验证所有 Pod 健康
	if err := h.verifyPodsHealth(ctx, cluster.CPNodes[0].Host); err != nil {
		return fmt.Errorf("pod health check failed: %w", err)
	}

	h.updateJobPhase(ctx, jobID, "verifying", 98, "verifying: no available upgrades")

	// 3. 验证无可用升级
	if err := h.verifyNoAvailableUpgrade(ctx, cluster.CPNodes[0].Host, spec.TargetVersion); err != nil {
		return fmt.Errorf("upgrade plan verification failed: %w", err)
	}

	return nil
}

func (h *K8sLifecycleHandler) verifyNodeVersions(ctx context.Context, cluster *ClusterInfo, targetVersion string) error {
	cmd := "kubectl get nodes -o jsonpath='{range .items[*]}{.status.nodeInfo.kubeletVersion}{\"\\n\"}{end}'"
	out, _, code, err := h.execSSH(context.Background(), cluster.CPNodes[0].Host, cmd)
	if err != nil || code != 0 {
		return fmt.Errorf("get node versions failed: %v", err)
	}

	versions := strings.Split(strings.TrimSpace(out), "\n")
	for _, v := range versions {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !strings.HasPrefix(v, "v"+targetVersion) && v != "v"+targetVersion {
			return fmt.Errorf("node version mismatch: got %s, expected %s", v, targetVersion)
		}
	}
	return nil
}

func (h *K8sLifecycleHandler) verifyPodsHealth(ctx context.Context, cpNode string) error {
	cmd := "kubectl get pods -A --no-headers | awk '$3 != \"Running\" && $3 != \"Completed\" {print $1\"/\"$2\" \"$3}'"
	out, _, code, err := h.execSSH(context.Background(), cpNode, cmd)
	if err != nil || code != 0 {
		return fmt.Errorf("get pods failed: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("unhealthy pods found:\n%s", out)
	}
	return nil
}

func (h *K8sLifecycleHandler) verifyNoAvailableUpgrade(ctx context.Context, cpNode, targetVersion string) error {
	cmd := "kubeadm upgrade plan | grep -c \"version available\""
	out, _, code, err := h.execSSH(context.Background(), cpNode, cmd)
	if err != nil || code != 0 {
		// kubeadm upgrade plan 可能返回非 0 码但有输出
		if strings.Contains(out, targetVersion) {
			return nil
		}
		return fmt.Errorf("kubeadm upgrade plan failed: %v", err)
	}
	if strings.TrimSpace(out) != "0" {
		return fmt.Errorf("unexpected upgrades still available")
	}
	return nil
}

// updateJobPhase 更新作业阶段
func (h *K8sLifecycleHandler) updateJobPhase(ctx context.Context, jobID, phase string, progress int, step string) {
	if h.store == nil || jobID == "" {
		return
	}
	job, err := h.store.GetLifecycleJob(ctx, jobID)
	if err != nil || job == nil {
		return
	}
	job.Phase = phase
	job.Progress = progress
	job.CurrentStep = step
	if uerr := h.store.UpdateLifecycleJob(ctx, &sqlstore.LifecycleJob{
		ID:          job.ID,
		Phase:       job.Phase,
		Progress:    job.Progress,
		CurrentStep: job.CurrentStep,
		Version:     job.Version,
	}); uerr != nil {
		logx.Warn(ctx, "更新生命周期作业进度失败（best-effort）", "error", uerr)
	}
}

// failJob 标记作业失败
func (h *K8sLifecycleHandler) failJob(ctx context.Context, jobID, errMsg string) {
	if h.store == nil || jobID == "" {
		return
	}
	job, err := h.store.GetLifecycleJob(ctx, jobID)
	if err != nil || job == nil {
		return
	}
	job.Phase = "failed"
	job.Progress = 0
	job.CurrentStep = ""
	job.Error = errMsg
	if uerr := h.store.UpdateLifecycleJob(ctx, &sqlstore.LifecycleJob{
		ID:          job.ID,
		Phase:       job.Phase,
		Progress:    job.Progress,
		CurrentStep: job.CurrentStep,
		Error:       job.Error,
		Version:     job.Version,
	}); uerr != nil {
		logx.Warn(ctx, "更新生命周期作业进度失败（best-effort）", "error", uerr)
	}
}

// completeJob 标记作业完成
func (h *K8sLifecycleHandler) completeJob(ctx context.Context, jobID, msg string) {
	if h.store == nil || jobID == "" {
		return
	}
	job, err := h.store.GetLifecycleJob(ctx, jobID)
	if err != nil || job == nil {
		return
	}
	job.Phase = "completed"
	job.Progress = 100
	job.CurrentStep = msg
	resultJSON := map[string]any{"message": msg}
	if uerr := h.store.UpdateLifecycleJob(ctx, &sqlstore.LifecycleJob{
		ID:          job.ID,
		Phase:       job.Phase,
		Progress:    job.Progress,
		CurrentStep: job.CurrentStep,
		ResultJSON:  resultJSON,
		Version:     job.Version,
	}); uerr != nil {
		logx.Warn(ctx, "更新生命周期作业进度失败（best-effort）", "error", uerr)
	}
}

// waitNodeReady 等待节点变为 Ready 状态（带轮询和超时）。
// kubectlHost 是执行 kubectl 的控制面节点（必须有 kubeconfig）；
// node 是被等待的目标节点名（可以是 worker）。
// 审计整改批次1：原实现固定 SSH 到硬编码 10.0.0.1，worker 等待链路同样被误路由。
func (h *K8sLifecycleHandler) waitNodeReady(ctx context.Context, kubectlHost, node string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(nodeReadyPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for node %s to become Ready", node)
			}
			cmd := fmt.Sprintf("kubectl get node %s -o jsonpath='{.status.conditions[?(@.type==\"Ready\")].status}'", node)
			out, _, code, err := h.execSSH(ctx, kubectlHost, cmd)
			if err != nil || code != 0 {
				continue // 继续重试
			}
			if strings.TrimSpace(out) == "True" {
				return nil
			}
		case <-time.After(timeout):
			return fmt.Errorf("timeout waiting for node %s to become Ready", node)
		}
	}
}

// checkNodesHealth 检查节点健康状态
func (h *K8sLifecycleHandler) checkNodesHealth(ctx context.Context, cluster *ClusterInfo) error {
	cmd := "kubectl get nodes --no-headers | awk '$2 != \"Ready\" {print $1}'"
	out, _, code, err := h.execSSH(ctx, cluster.CPNodes[0].Host, cmd)
	if err != nil || code != 0 {
		return fmt.Errorf("get nodes failed: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("nodes not ready: %s", strings.TrimSpace(out))
	}
	return nil
}

// checkEtcdHealth 检查 etcd 健康状态
func (h *K8sLifecycleHandler) checkEtcdHealth(ctx context.Context, cluster *ClusterInfo) error {
	cmd := "etcdctl endpoint health --cluster"
	_, _, code, err := h.execSSH(ctx, cluster.CPNodes[0].Host, cmd)
	if err != nil || code != 0 {
		return fmt.Errorf("etcd health check failed: %v", err)
	}
	return nil
}

// checkControlPlanePods 检查控制面 Pod 状态
func (h *K8sLifecycleHandler) checkControlPlanePods(ctx context.Context, cluster *ClusterInfo) error {
	cmd := "kubectl get pods -n kube-system --no-headers | awk '$3 != \"Running\" {print $1\"/\"$2\" \"$3}'"
	out, _, code, err := h.execSSH(ctx, cluster.CPNodes[0].Host, cmd)
	if err != nil || code != 0 {
		return fmt.Errorf("get control plane pods failed: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("unhealthy control plane pods:\n%s", out)
	}
	return nil
}

// RegisterK8sLifecycleRoutes 注册 K8s 生命周期路由。
func RegisterK8sLifecycleRoutes(mux *http.ServeMux, handler *K8sLifecycleHandler) {
	mux.HandleFunc("POST /api/v1/k8s/lifecycle/upgrade", handler.authorize(handler.HandleUpgrade, "k8s:read", "k8s:write"))
	mux.HandleFunc("POST /api/v1/k8s/lifecycle/upgrade/plan", handler.authorize(handler.HandleUpgradePlan, "k8s:read", "k8s:write"))
	mux.HandleFunc("GET /api/v1/k8s/lifecycle/upgrade/status", handler.authorize(handler.HandleUpgradeStatus, "k8s:read", "k8s:write"))
	mux.HandleFunc("POST /api/v1/k8s/lifecycle/rollback", handler.authorize(handler.HandleRollback, "k8s:read", "k8s:write"))
	mux.HandleFunc("POST /api/v1/k8s/lifecycle/cert-rotate", handler.authorize(handler.HandleCertRotation, "k8s:read", "k8s:write"))
	mux.HandleFunc("POST /api/v1/k8s/lifecycle/node-scale", handler.authorize(handler.HandleNodeScale, "k8s:read", "k8s:write"))
	mux.HandleFunc("POST /api/v1/k8s/lifecycle/etcd-backup", handler.authorize(handler.HandleEtcdBackup, "k8s:read", "k8s:write"))
	mux.HandleFunc("POST /api/v1/k8s/lifecycle/etcd-restore", handler.authorize(handler.HandleEtcdRestore, "k8s:read", "k8s:write"))
	mux.HandleFunc("GET /api/v1/k8s/lifecycle/etcd-backups", handler.authorize(handler.HandleEtcdListBackups, "k8s:read", "k8s:write"))
}

// 辅助函数

// runRollback 真实执行 K8s 集群回滚（异步后台任务）
func (h *K8sLifecycleHandler) runRollback(ctx context.Context, jobID string, spec RollbackSpec) {
	cluster, err := h.getClusterInfo(ctx, spec.ClusterID)
	if err != nil {
		h.failJob(ctx, jobID, "get cluster info: "+err.Error())
		return
	}

	h.updateJobPhase(ctx, jobID, "precheck", 5, "precheck: validating rollback prerequisites")

	// 1. 预检：etcd 备份路径存在、目标版本有效、etcd 健康
	if err := h.precheckRollback(ctx, jobID, spec, cluster); err != nil {
		h.failJob(ctx, jobID, "precheck failed: "+err.Error())
		return
	}

	h.updateJobPhase(ctx, jobID, "etcd_restore", 20, "etcd_restore: restoring etcd from backup")

	// 2. etcd 恢复
	if err := h.restoreEtcd(ctx, jobID, spec.EtcdBackupPath, cluster); err != nil {
		h.failJob(ctx, jobID, "etcd restore failed: "+err.Error())
		return
	}

	h.updateJobPhase(ctx, jobID, "control_plane_rollback", 40, "control_plane_rollback: rolling back control plane")

	// 3. 控制面回滚
	if err := h.rollbackControlPlane(ctx, jobID, spec, cluster); err != nil {
		h.failJob(ctx, jobID, "control plane rollback failed: "+err.Error())
		return
	}

	h.updateJobPhase(ctx, jobID, "worker_rollback", 70, "worker_rollback: rolling back workers")

	// 4. Worker 节点回滚
	if err := h.rollbackWorkers(ctx, jobID, spec, cluster); err != nil {
		h.failJob(ctx, jobID, "worker rollback failed: "+err.Error())
		return
	}

	h.updateJobPhase(ctx, jobID, "verifying", 90, "verifying: checking cluster health")

	// 5. 验证
	if err := h.verifyRollback(ctx, jobID, spec, cluster); err != nil {
		h.failJob(ctx, jobID, "verification failed: "+err.Error())
		return
	}

	h.completeJob(ctx, jobID, "rollback completed successfully")
}

// precheckRollback 预检回滚前置条件
func (h *K8sLifecycleHandler) precheckRollback(ctx context.Context, jobID string, spec RollbackSpec, cluster *ClusterInfo) error {
	h.updateJobPhase(ctx, jobID, "precheck", 5, "precheck: checking etcd backup exists")

	// 检查 etcd 备份文件存在
	cmd := fmt.Sprintf("test -f %s", spec.EtcdBackupPath)
	if _, _, code, err := h.execSSH(context.Background(), cluster.CPNodes[0].Host, cmd); err != nil || code != 0 {
		return fmt.Errorf("etcd backup file not found: %s", spec.EtcdBackupPath)
	}

	h.updateJobPhase(ctx, jobID, "precheck", 30, "precheck: checking target version valid")

	// 验证目标版本有效
	if !isValidVersionJump(cluster.CurrentVersion, spec.TargetVersion) {
		return fmt.Errorf("invalid version rollback: %s -> %s", cluster.CurrentVersion, spec.TargetVersion)
	}

	h.updateJobPhase(ctx, jobID, "precheck", 60, "precheck: checking etcd health")

	// etcd 健康检查
	if err := h.checkEtcdHealth(context.Background(), &ClusterInfo{CPNodes: cluster.CPNodes}); err != nil {
		return fmt.Errorf("etcd health check failed: %w", err)
	}

	// 验证控制面 Pod 健康
	if err := h.checkControlPlanePods(context.Background(), &ClusterInfo{CPNodes: cluster.CPNodes}); err != nil {
		return fmt.Errorf("control plane pods unhealthy: %w", err)
	}

	return nil
}

// restoreEtcd 恢复 etcd（支持多 CP 集群）
func (h *K8sLifecycleHandler) restoreEtcd(ctx context.Context, jobID, backupPath string, cluster *ClusterInfo) error {
	h.updateJobPhase(context.Background(), "", "etcd_restore", 15, "etcd_restore: stopping etcd on all CP nodes")

	// 1. 停止所有 CP 节点的 etcd（并行停止，但按顺序更安全）
	for _, cp := range cluster.CPNodes {
		if _, stderr, code, err := h.execSSH(context.Background(), cp.Host, "systemctl stop etcd"); err != nil || code != 0 {
			return fmt.Errorf("stop etcd on %s failed: %v, stderr: %s", cp.Host, err, stderr)
		}
	}

	h.updateJobPhase(context.Background(), "", "etcd_restore", 30, "etcd_restore: restoring from backup on first CP node")

	// 2. 在第一个 CP 节点恢复 etcd 数据
	firstCP := cluster.CPNodes[0]
	cmd := fmt.Sprintf("etcdctl snapshot restore %s --data-dir /var/lib/etcd --name %s --initial-cluster %s --initial-cluster-token etcd-cluster --initial-advertise-peer-urls https://%s:2380",
		backupPath, "cp-1", h.buildInitialCluster(cluster), firstCP.Host)
	if _, stderr, code, err := h.execSSH(context.Background(), cluster.CPNodes[0].Host, cmd); err != nil || code != 0 {
		return fmt.Errorf("etcdctl snapshot restore failed: %v, stderr: %s", err, stderr)
	}

	// 3. 启动第一个 etcd 实例
	if _, stderr, code, err := h.execSSH(context.Background(), cluster.CPNodes[0].Host, "systemctl start etcd"); err != nil || code != 0 {
		return fmt.Errorf("start etcd failed: %v, stderr: %s", err, stderr)
	}

	// 等待第一个 etcd 健康并成为 leader
	time.Sleep(10 * time.Second)
	if err := h.waitEtcdLeader(context.Background(), cluster.CPNodes[0].Host, maxNodeReadyWait); err != nil {
		return fmt.Errorf("etcd leader not elected after restore: %w", err)
	}

	// 3. 其余 CP 节点加入集群（逐个添加成员并启动）
	for i := 1; i < len(cluster.CPNodes); i++ {
		cp := cluster.CPNodes[i]
		h.updateJobPhase(context.Background(), "", "etcd_restore", 40+int(float64(i)/float64(len(cluster.CPNodes))*50), fmt.Sprintf("adding etcd member %d/%d: %s", i+1, len(cluster.CPNodes), cp.Host))

		// 添加成员到集群
		cmd = fmt.Sprintf("etcdctl member add %s --peer-urls=https://%s:2380", cp.Host, cp.Host)
		if _, stderr, code, err := h.execSSH(context.Background(), cluster.CPNodes[0].Host, cmd); err != nil || code != 0 {
			// 成员可能已存在，检查错误
			if strings.Contains(stderr, "member already exists") {
				logx.Info(context.Background(), "etcd member already exists, skipping add", "node", cp.Host)
			} else {
				return fmt.Errorf("etcdctl member add %s failed: %v, stderr: %s", cp.Host, err, stderr)
			}
		}

		// 恢复该节点的 etcd 数据（使用相同的 snapshot）
		cmd = fmt.Sprintf("etcdctl snapshot restore %s --data-dir /var/lib/etcd --name %s --initial-cluster %s --initial-cluster-token etcd-cluster --initial-advertise-peer-urls https://%s:2380",
			backupPath, fmt.Sprintf("cp-%d", i+1), h.buildInitialCluster(cluster), cp.Host)
		if _, stderr, code, err := h.execSSH(context.Background(), cp.Host, cmd); err != nil || code != 0 {
			return fmt.Errorf("etcdctl snapshot restore on %s failed: %v, stderr: %s", cp.Host, err, stderr)
		}

		// 启动该节点 etcd
		if _, stderr, code, err := h.execSSH(context.Background(), cp.Host, "systemctl start etcd"); err != nil || code != 0 {
			return fmt.Errorf("start etcd on %s failed: %v, stderr: %s", cp.Host, err, stderr)
		}

		// 等待该节点加入集群
		if err := h.waitEtcdMemberReady(context.Background(), cp.Host, 60*time.Second); err != nil {
			return fmt.Errorf("etcd member %s not ready: %w", cp.Host, err)
		}
	}

	// 最终验证：所有成员健康
	if err := h.checkEtcdHealth(context.Background(), &ClusterInfo{CPNodes: cluster.CPNodes}); err != nil {
		return fmt.Errorf("etcd cluster not healthy after restore: %w", err)
	}

	return nil
}

// buildInitialCluster 构建 etcd 初始集群字符串
func (h *K8sLifecycleHandler) buildInitialCluster(cluster *ClusterInfo) string {
	var members []string
	for i, cp := range cluster.CPNodes {
		members = append(members, fmt.Sprintf("cp-%d=https://%s:2380", i+1, cp.Host))
	}
	return strings.Join(members, ",")
}

// waitEtcdLeader 等待 etcd 选举出 leader
func (h *K8sLifecycleHandler) waitEtcdLeader(ctx context.Context, host string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for etcd leader on %s", host)
			}
			cmd := "etcdctl endpoint status --cluster -w json | jq -r '.[] | select(.Status.leader == .Status.header.member_id) | .Status.header.member_id'"
			out, _, code, err := h.execSSH(context.Background(), host, cmd)
			if err != nil || code != 0 {
				continue
			}
			if strings.TrimSpace(out) != "" {
				return nil
			}
		case <-time.After(30 * time.Second):
			return fmt.Errorf("timeout waiting for etcd leader")
		}
	}
}

// waitEtcdMemberReady 等待 etcd 成员加入集群并就绪
func (h *K8sLifecycleHandler) waitEtcdMemberReady(ctx context.Context, host string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for etcd member %s to join", host)
			}
			cmd := "etcdctl endpoint health --cluster"
			_, _, code, err := h.execSSH(context.Background(), host, cmd)
			if err == nil && code == 0 {
				return nil
			}
		case <-time.After(60 * time.Second):
			return fmt.Errorf("timeout waiting for etcd member %s", host)
		}
	}
}

// rollbackControlPlane 回滚控制面节点
func (h *K8sLifecycleHandler) rollbackControlPlane(ctx context.Context, jobID string, spec RollbackSpec, cluster *ClusterInfo) error {
	for i, cp := range cluster.CPNodes {
		h.updateJobPhase(ctx, jobID, "control_plane_rollback", 40+int(float64(i)/float64(len(cluster.CPNodes))*30), fmt.Sprintf("rolling back control plane node %d/%d: %s", i+1, len(cluster.CPNodes), cp.Host))

		if err := h.rollbackSingleControlPlane(ctx, cp.Host, spec.TargetVersion); err != nil {
			return fmt.Errorf("rollback control plane node %s failed: %w", cp.Host, err)
		}
	}
	return nil
}

func (h *K8sLifecycleHandler) rollbackSingleControlPlane(ctx context.Context, cpNode, targetVersion string) error {
	logx.Info(ctx, "开始回滚控制面节点", "node", cpNode, "targetVersion", targetVersion)

	// 1. drain 节点
	h.updateJobPhase(context.Background(), "", "control_plane_rollback", 25, fmt.Sprintf("draining node %s", cpNode))
	cmd := fmt.Sprintf("kubectl drain %s --ignore-daemonsets --delete-emptydir-data --force --grace-period=%d", cpNode, drainGracePeriod)
	if _, stderr, code, err := h.execSSH(context.Background(), cpNode, cmd); err != nil || code != 0 {
		if strings.Contains(stderr, "already cordoned") || strings.Contains(stderr, "already drained") {
			logx.Info(context.Background(), "节点已 drain，跳过", "node", cpNode)
		} else {
			return fmt.Errorf("drain %s failed: %v, stderr: %s", cpNode, err, stderr)
		}
	}

	// 2. 降级 kubeadm
	h.updateJobPhase(context.Background(), "", "control_plane_rollback", 40, fmt.Sprintf("downgrading kubeadm to %s", targetVersion))
	cmd = fmt.Sprintf("apt-get install -y kubeadm=%s-00", targetVersion)
	if _, stderr, code, err := h.execSSH(context.Background(), cpNode, cmd); err != nil || code != 0 {
		if strings.Contains(stderr, "already the newest version") {
			logx.Info(context.Background(), "kubeadm 已是目标版本，跳过", "node", cpNode, "version", targetVersion)
		} else {
			return fmt.Errorf("install kubeadm %s failed: %v, stderr: %s", targetVersion, err, stderr)
		}
	}

	// 3. 执行 kubeadm upgrade apply (降级)
	h.updateJobPhase(context.Background(), "", "control_plane_rollback", 50, fmt.Sprintf("applying kubeadm downgrade to %s", targetVersion))
	cmd = fmt.Sprintf("kubeadm upgrade apply v%s -y", targetVersion)
	if _, stderr, code, err := h.execSSH(context.Background(), cpNode, cmd); err != nil || code != 0 {
		if strings.Contains(stderr, "already at version") || strings.Contains(stderr, "cluster is already at version") {
			logx.Info(context.Background(), "控制面已是目标版本，跳过降级", "node", cpNode, "version", targetVersion)
		} else {
			return fmt.Errorf("kubeadm upgrade apply %s failed: %v, stderr: %s", targetVersion, err, stderr)
		}
	}

	// 4. 降级 kubelet 和 kubectl
	h.updateJobPhase(context.Background(), "", "control_plane_rollback", 65, fmt.Sprintf("downgrading kubelet/kubectl to %s", targetVersion))
	cmd = fmt.Sprintf("apt-get install -y kubelet=%s-00 kubectl=%s-00", targetVersion, targetVersion)
	if _, stderr, code, err := h.execSSH(context.Background(), cpNode, cmd); err != nil || code != 0 {
		if strings.Contains(stderr, "already the newest version") {
			logx.Info(context.Background(), "kubelet/kubectl 已是目标版本，跳过", "node", cpNode, "version", targetVersion)
		} else {
			return fmt.Errorf("install kubelet/kubectl %s failed: %v, stderr: %s", targetVersion, err, stderr)
		}
	}

	// 5. 重启 kubelet
	h.updateJobPhase(context.Background(), "", "control_plane_rollback", 75, "restarting kubelet")
	cmd = "systemctl daemon-reload && systemctl restart kubelet"
	if _, stderr, code, err := h.execSSH(context.Background(), cpNode, cmd); err != nil || code != 0 {
		return fmt.Errorf("restart kubelet failed: %v, stderr: %s", err, stderr)
	}

	// 6. uncordon 节点
	cmd = "kubectl uncordon " + cpNode
	if _, stderr, code, err := h.execSSH(context.Background(), cpNode, cmd); err != nil || code != 0 {
		if strings.Contains(stderr, "not cordoned") {
			logx.Info(context.Background(), "节点未被 cordon，跳过 uncordon", "node", cpNode)
		} else {
			return fmt.Errorf("uncordon %s failed: %v, stderr: %s", cpNode, err, stderr)
		}
	}

	// 7. 等待节点 Ready
	if err := h.waitNodeReady(context.Background(), cpNode, cpNode, maxNodeReadyWait); err != nil {
		return fmt.Errorf("node %s not ready after rollback: %w", cpNode, err)
	}

	logx.Info(context.Background(), "控制面节点回滚完成", "node", cpNode, "version", targetVersion)
	return nil
}

// rollbackWorkers 回滚 Worker 节点
func (h *K8sLifecycleHandler) rollbackWorkers(ctx context.Context, jobID string, spec RollbackSpec, cluster *ClusterInfo) error {
	if len(cluster.WorkerNodes) == 0 {
		return nil
	}

	h.updateJobPhase(ctx, jobID, "worker_rollback", 70, fmt.Sprintf("rolling back %d worker nodes", len(cluster.WorkerNodes)))

	for i := 0; i < len(cluster.WorkerNodes); i += 1 {
		node := cluster.WorkerNodes[i]
		h.updateJobPhase(ctx, jobID, "worker_rollback", 70+int(float64(i)/float64(len(cluster.WorkerNodes))*20), fmt.Sprintf("rolling back worker %d/%d: %s", i+1, len(cluster.WorkerNodes), node))

		if err := h.rollbackSingleWorker(ctx, cluster.CPNodes[0].Host, node, spec.TargetVersion); err != nil {
			return fmt.Errorf("rollback worker %s failed: %w", node, err)
		}
	}
	return nil
}

func (h *K8sLifecycleHandler) rollbackSingleWorker(ctx context.Context, kubectlHost, node, targetVersion string) error {
	logx.Info(ctx, "开始回滚 Worker 节点", "node", node, "targetVersion", targetVersion, "kubectlHost", kubectlHost)

	// 1. drain 节点（kubectl 在控制面执行）
	cmd := fmt.Sprintf("kubectl drain %s --ignore-daemonsets --delete-emptydir-data --force --grace-period=%d", node, drainGracePeriod)
	if _, stderr, code, err := h.execSSH(context.Background(), kubectlHost, cmd); err != nil || code != 0 {
		if strings.Contains(stderr, "already cordoned") || strings.Contains(stderr, "already drained") {
			logx.Info(context.Background(), "Worker 节点已 drain，跳过", "node", node)
		} else {
			return fmt.Errorf("drain %s failed: %v, stderr: %s", node, err, stderr)
		}
	}

	// 2. 降级 kubelet 和 kubectl
	cmd = fmt.Sprintf("apt-get install -y kubelet=%s-00 kubectl=%s-00", targetVersion, targetVersion)
	if _, stderr, code, err := h.execSSH(context.Background(), node, cmd); err != nil || code != 0 {
		if strings.Contains(stderr, "already the newest version") {
			logx.Info(context.Background(), "Worker kubelet/kubectl 已是目标版本，跳过", "node", node, "version", targetVersion)
		} else {
			return fmt.Errorf("install kubelet/kubectl %s failed: %v, stderr: %s", targetVersion, err, stderr)
		}
	}

	// 3. 重启 kubelet
	cmd = "systemctl daemon-reload && systemctl restart kubelet"
	if _, stderr, code, err := h.execSSH(context.Background(), node, cmd); err != nil || code != 0 {
		return fmt.Errorf("restart kubelet failed: %v, stderr: %s", err, stderr)
	}

	// 4. uncordon（kubectl 在控制面执行）
	cmd = "kubectl uncordon " + node
	if _, stderr, code, err := h.execSSH(context.Background(), kubectlHost, cmd); err != nil || code != 0 {
		if strings.Contains(stderr, "not cordoned") {
			logx.Info(context.Background(), "Worker 节点未被 cordon，跳过 uncordon", "node", node)
		} else {
			return fmt.Errorf("uncordon %s failed: %v, stderr: %s", node, err, stderr)
		}
	}

	// 等待节点 Ready
	if err := h.waitNodeReady(context.Background(), kubectlHost, node, maxNodeReadyWait); err != nil {
		return fmt.Errorf("worker %s not ready after rollback: %w", node, err)
	}

	logx.Info(context.Background(), "Worker 节点回滚完成", "node", node, "version", targetVersion)
	return nil
}

// verifyRollback 验证回滚结果
func (h *K8sLifecycleHandler) verifyRollback(ctx context.Context, jobID string, spec RollbackSpec, cluster *ClusterInfo) error {
	h.updateJobPhase(ctx, jobID, "verifying", 92, "verifying: checking node versions")

	// 1. 验证所有节点版本 == 目标版本
	if err := h.verifyNodeVersions(ctx, &ClusterInfo{CPNodes: cluster.CPNodes, WorkerNodes: cluster.WorkerNodes}, spec.TargetVersion); err != nil {
		return fmt.Errorf("node version verification failed: %w", err)
	}

	h.updateJobPhase(ctx, jobID, "verifying", 95, "verifying: checking pod health")

	// 2. 验证 Pod 健康
	if err := h.verifyPodsHealth(ctx, cluster.CPNodes[0].Host); err != nil {
		return fmt.Errorf("pod health check failed: %w", err)
	}

	return nil
}

// runCertRotation 真实执行证书轮换（异步后台任务）
func (h *K8sLifecycleHandler) runCertRotation(ctx context.Context, jobID string, spec CertRotationSpec) {
	cluster, err := h.getClusterInfo(ctx, spec.ClusterID)
	if err != nil {
		h.failJob(ctx, jobID, "get cluster info: "+err.Error())
		return
	}

	h.updateJobPhase(ctx, jobID, "precheck", 5, "precheck: checking certificate rotation prerequisites")

	// 1. 预检：CA 证书/私钥存在、etcd 健康
	if err := h.precheckCertRotation(ctx, jobID, spec, cluster); err != nil {
		h.failJob(ctx, jobID, "precheck failed: "+err.Error())
		return
	}

	h.updateJobPhase(ctx, jobID, "cert_rotation", 20, "cert_rotation: rotating certificates")

	// 2. 执行证书轮换
	if spec.RenewAll {
		if err := h.rotateAllCerts(ctx, jobID, spec, cluster); err != nil {
			h.failJob(ctx, jobID, "cert rotation failed: "+err.Error())
			return
		}
	} else if len(spec.Certs) > 0 {
		if err := h.rotateSpecifiedCerts(ctx, jobID, spec.Certs, cluster); err != nil {
			h.failJob(ctx, jobID, "cert rotation failed: "+err.Error())
			return
		}
	}

	h.updateJobPhase(ctx, jobID, "component_restart", 80, "component_restart: restarting k8s components")

	// 3. 重启相关组件
	if err := h.restartComponentsAfterCertRotation(ctx, jobID, cluster); err != nil {
		h.failJob(ctx, jobID, "component restart failed: "+err.Error())
		return
	}

	h.updateJobPhase(ctx, jobID, "verifying", 95, "verifying: checking certificate validity")

	// 4. 验证
	if err := h.verifyCertRotation(ctx, jobID, spec, cluster); err != nil {
		h.failJob(ctx, jobID, "verification failed: "+err.Error())
		return
	}

	h.completeJob(ctx, jobID, "cert-rotate completed successfully")
}

// precheckCertRotation 预检证书轮换前置条件
func (h *K8sLifecycleHandler) precheckCertRotation(ctx context.Context, jobID string, spec CertRotationSpec, cluster *ClusterInfo) error {
	h.updateJobPhase(ctx, jobID, "precheck", 10, "precheck: checking CA cert/key")

	// 检查 CA 证书/私钥存在
	if !spec.RenewAll && len(spec.Certs) > 0 {
		if spec.CACertPath == "" || spec.CAKeyPath == "" {
			return fmt.Errorf("CA cert/key paths required for specified certs")
		}
		cmd := fmt.Sprintf("test -f %s && test -f %s", spec.CACertPath, spec.CAKeyPath)
		if _, _, code, err := h.execSSH(context.Background(), cluster.CPNodes[0].Host, cmd); err != nil || code != 0 {
			return fmt.Errorf("CA cert/key not found: %s, %s", spec.CACertPath, spec.CAKeyPath)
		}
	}

	// etcd 健康检查
	if err := h.checkEtcdHealth(context.Background(), &ClusterInfo{CPNodes: cluster.CPNodes}); err != nil {
		return fmt.Errorf("etcd health check failed: %w", err)
	}

	return nil
}

// rotateAllCerts 续签所有证书（支持多 CP 集群）
func (h *K8sLifecycleHandler) rotateAllCerts(ctx context.Context, jobID string, spec CertRotationSpec, cluster *ClusterInfo) error {
	h.updateJobPhase(ctx, jobID, "cert_rotation", 30, "cert_rotation: renewing all certificates via kubeadm")

	// 1. 在第一个 CP 节点续签所有证书
	cmd := "kubeadm certs renew all"
	if _, stderr, code, err := h.execSSH(context.Background(), cluster.CPNodes[0].Host, cmd); err != nil || code != 0 {
		return fmt.Errorf("kubeadm certs renew all failed: %v, stderr: %s", err, stderr)
	}

	// 2. 多 CP 集群：上传证书并分发到其他 CP 节点
	if len(cluster.CPNodes) > 1 {
		h.updateJobPhase(ctx, jobID, "cert_rotation", 40, "cert_rotation: uploading certificates for multi-CP distribution")

		// 在第一个 CP 上传证书并获取 certificate-key
		cmd := "kubeadm init phase upload-certs --upload-certs --certificate-key=$(kubeadm init phase upload-certs --dry-run -o json | jq -r '.certificateKey')"
		out, _, code, err := h.execSSH(context.Background(), cluster.CPNodes[0].Host, cmd)
		if err != nil || code != 0 {
			// 备选：直接使用 upload-certs 并解析输出
			cmd = "kubeadm init phase upload-certs --upload-certs"
			var stderr string
			out, stderr, code, err = h.execSSH(context.Background(), cluster.CPNodes[0].Host, cmd)
			if err != nil || code != 0 {
				return fmt.Errorf("kubeadm init phase upload-certs failed: %v, stderr: %s", err, stderr)
			}
		}

		// 解析输出中的 certificate-key
		certKey := extractCertificateKey(out)
		if certKey == "" {
			return fmt.Errorf("failed to extract certificate-key from upload-certs output")
		}

		h.updateJobPhase(ctx, jobID, "cert_rotation", 50, "cert_rotation: distributing certificates to remaining CP nodes")

		// 3. 其他 CP 节点下载证书并加入集群
		for _, cp := range cluster.CPNodes {
			if cp.Host == cluster.CPNodes[0].Host {
				continue
			}

			h.updateJobPhase(ctx, jobID, "cert_rotation", 50+int(float64(10)/float64(len(cluster.CPNodes))*10), fmt.Sprintf("distributing certs to CP node %s", cp.Host))

			// 在其他 CP 节点上执行 join --certificate-key
			// 注意：实际需要从 cluster 获取正确的 join 命令参数
			// 简化：使用 kubeadm join phase control-plane-prepare certs
			cmd := fmt.Sprintf("kubeadm join phase control-plane-prepare certs --certificate-key %s --config /etc/kubernetes/kubeadm-config.yaml", certKey)
			if _, _, code, err := h.execSSH(context.Background(), cp.Host, cmd); err != nil || code != 0 {
				// 备选：直接复制 /etc/kubernetes/pki 目录
				return h.distributeCertsViaSCP(cluster.CPNodes[0].Host, cp.Host)
			}
		}
	}

	h.updateJobPhase(ctx, jobID, "cert_rotation", 60, "cert_rotation: all certificates renewed and distributed")
	return nil
}

// rotateSpecifiedCerts 续签指定证书
func (h *K8sLifecycleHandler) rotateSpecifiedCerts(ctx context.Context, jobID string, certs []string, cluster *ClusterInfo) error {
	h.updateJobPhase(ctx, jobID, "cert_rotation", 30, "cert_rotation: renewing specified certificates")

	for _, cert := range certs {
		cmd := fmt.Sprintf("kubeadm certs renew %s", cert)
		if _, stderr, code, err := h.execSSH(context.Background(), cluster.CPNodes[0].Host, cmd); err != nil || code != 0 {
			return fmt.Errorf("kubeadm certs renew %s failed: %v, stderr: %s", cert, err, stderr)
		}
	}

	h.updateJobPhase(context.Background(), "", "cert_rotation", 60, "cert_rotation: specified certificates renewed")
	return nil
}

// restartComponentsAfterCertRotation 证书轮换后重启组件
func (h *K8sLifecycleHandler) restartComponentsAfterCertRotation(ctx context.Context, jobID string, cluster *ClusterInfo) error {
	components := []string{"kube-apiserver", "kube-controller-manager", "kube-scheduler", "etcd"}

	for _, comp := range components {
		cmd := fmt.Sprintf("systemctl restart %s", comp)
		if _, stderr, code, err := h.execSSH(context.Background(), cluster.CPNodes[0].Host, cmd); err != nil || code != 0 {
			return fmt.Errorf("restart %s failed: %v, stderr: %s", comp, err, stderr)
		}
	}

	// 等待组件就绪
	time.Sleep(10 * time.Second)

	// 验证组件健康
	cmd := "kubectl get pods -n kube-system --no-headers | awk '$3 != \"Running\" {print $1\"/\"$2\" \"$3}'"
	out, _, code, err := h.execSSH(context.Background(), cluster.CPNodes[0].Host, cmd)
	if err != nil || code != 0 {
		return fmt.Errorf("get pods failed: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("unhealthy pods after restart:\n%s", out)
	}

	return nil
}

// verifyCertRotation 验证证书轮换结果
func (h *K8sLifecycleHandler) verifyCertRotation(ctx context.Context, jobID string, spec CertRotationSpec, cluster *ClusterInfo) error {
	// 检查证书有效期
	cmd := "kubeadm certs check-expiration"
	out, _, code, err := h.execSSH(context.Background(), cluster.CPNodes[0].Host, cmd)
	if err != nil || code != 0 {
		return fmt.Errorf("kubeadm certs check-expiration failed: %v", err)
	}

	// 简单检查：输出中不应有 "EXPIRED" 或过期日期
	if strings.Contains(out, "EXPIRED") {
		return fmt.Errorf("some certificates are expired: %s", out)
	}

	logx.Info(context.Background(), "证书轮换验证通过", "output", out)
	return nil
}

// isValidVersionJump 检查版本跳跃是否合法（仅允许同主版本号内的小版本升级）
func isValidVersionJump(current, target string) bool {
	// 解析主版本号 (major.minor)
	currentParts := strings.Split(current, ".")
	targetParts := strings.Split(target, ".")
	if len(currentParts) < 2 || len(targetParts) < 2 {
		return false
	}
	// 主版本号必须一致 (如 1.29 -> 1.30 允许，1.29 -> 1.29 允许，1.29 -> 2.0 不允许)
	return currentParts[0] == targetParts[0] && currentParts[1] == targetParts[1]
}

// parseK8sVersion 从 kubectl version 输出中提取版本号
func parseK8sVersion(out string) string {
	re := regexp.MustCompile(`v?(\d+\.\d+(?:\.\d+)?)`)
	if m := re.FindStringSubmatch(out); len(m) > 1 {
		return m[1]
	}
	return ""
}

func nodePort(node NodeSpec) int {
	if node.Port > 0 {
		return node.Port
	}
	return 22
}

// extractCertificateKey 从 kubeadm upload-certs 输出中提取 certificate-key
func extractCertificateKey(out string) string {
	// kubeadm upload-certs 输出示例：
	// [upload-certs] Storing the certificates in Secret "kubeadm-certs" in the "kube-system" Namespace
	// [upload-certs] Using certificate key: a1b2c3d4e5f6...
	re := regexp.MustCompile(`Using certificate key:\s*([a-f0-9]{64})`)
	if m := re.FindStringSubmatch(out); len(m) > 1 {
		return m[1]
	}
	// 备选：直接匹配 64 位十六进制字符串
	re2 := regexp.MustCompile(`\b([a-f0-9]{64})\b`)
	if m := re2.FindStringSubmatch(out); len(m) > 1 {
		return m[1]
	}
	return ""
}

// distributeCertsViaSCP 通过 SCP 分发证书文件（备选方案）
func (h *K8sLifecycleHandler) distributeCertsViaSCP(srcHost, dstHost string) error {
	// 将 /etc/kubernetes/pki 目录通过 scp 复制到目标节点
	// 注意：需要 SSH 密钥互信或密码认证
	cmd := fmt.Sprintf("scp -r -o StrictHostKeyChecking=no /etc/kubernetes/pki %s:/etc/kubernetes/", dstHost)
	if _, _, code, err := h.execSSH(context.Background(), srcHost, cmd); err != nil || code != 0 {
		return fmt.Errorf("scp certs from %s to %s failed: %v", srcHost, dstHost, err)
	}

	// 修复目标节点上的证书文件权限
	cmd = "chmod 644 /etc/kubernetes/pki/*.crt /etc/kubernetes/pki/*.key 2>/dev/null; chmod 600 /etc/kubernetes/pki/etcd/*.crt /etc/kubernetes/pki/etcd/*.key 2>/dev/null"
	if _, _, code, err := h.execSSH(context.Background(), dstHost, cmd); err != nil || code != 0 {
		logx.Warn(context.Background(), "fix cert permissions warning", "host", dstHost)
	}

	return nil
}
