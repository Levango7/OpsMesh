// Package k8slifecycle 提供 K8s 集群全生命周期管理：版本升级/回滚、证书轮换、节点扩缩容、etcd 备份恢复。
package k8slifecycle

import (
	"context"
	"encoding/json"
	"errors"
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
)

// ErrNotImplemented 表示该动作的真实执行链路尚未实现。
// HTTP 层必须把它映射为 501：此前 upgrade/rollback/cert-rotate 先返回 202，
// 再由纯内存状态机把 phase 推进到 Completed——前端与运维看到的是"已完成"，
// 集群侧却没有任何动作（假成功）。真实现见
// docs/design/k8s-lifecycle-execution-charter.md（C 项立项）。
var ErrNotImplemented = errors.New("k8s 生命周期动作尚未实现")

func notImplemented(operation, hint string) error {
	if hint == "" {
		return fmt.Errorf("%w: %s", ErrNotImplemented, operation)
	}
	return fmt.Errorf("%w: %s。%s", ErrNotImplemented, operation, hint)
}

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
	ClusterID     string `json:"clusterId"`
	TargetVersion string `json:"targetVersion"` // 回滚到的版本
	Reason        string `json:"reason"`
}

// ============================================================================
// 证书轮换
// ============================================================================

// CertRotationSpec 证书轮换规格
type CertRotationSpec struct {
	ClusterID string   `json:"clusterId"`
	Nodes     []string `json:"nodes"` // 空=全部
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

	// Authorize 鉴权回调（由 controlplane 注入），语义同 cmdb/log/deploy/orch 的
	// Authorize：失败时回调已写响应并返回 ok=false，调用方直接 return。
	// nil 时不启用鉴权（独立使用与单测的向后兼容路径）。
	Authorize func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool)
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

func NewK8sLifecycleHandler() *K8sLifecycleHandler {
	return &K8sLifecycleHandler{
		upgradeMgr: NewUpgradeManager(),
		nodeScaler: NewNodeScaler(),
		etcdMgr:    NewEtcdManager(),
	}
}

// writeNotImplemented 把"执行链路未实现"写成 501。
// 这三条入口此前先返回 202、再由纯内存状态机把 phase 推进到 Completed——
// 调用方看到"升级/回滚/轮换完成"，集群侧却没有任何动作。A 项裁决：能力不存在就必须明说。
func writeNotImplemented(w http.ResponseWriter, operation, hint string) {
	paginate.WriteJSON(w, http.StatusNotImplemented, map[string]string{
		"error": notImplemented(operation, hint).Error(),
		"code":  "ERR_NOT_IMPLEMENTED",
	})
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

	writeNotImplemented(w, "集群版本升级（upgrade）",
		"目前只有 POST /api/v1/k8s/lifecycle/upgrade/plan 会真实执行 kubeadm upgrade plan，升级动作本身尚未接线")
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

	writeNotImplemented(w, "集群版本回滚（rollback）",
		"回滚依赖 etcd 快照和 kubeadm 降级链路，尚未接线")
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

	writeNotImplemented(w, "集群证书轮换（cert-rotate）",
		"kubeadm certs renew + 组件重启链路尚未接线")
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
