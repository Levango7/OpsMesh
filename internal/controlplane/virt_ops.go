// virt_ops.go 虚拟化运维衔接（KVM/vSphere/OpenStack/Proxmox）。
//
// 设计原则（延续 P0-P5"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/virt/{deviceID} 查询虚拟化平台状态（宿主机角色/服务）。
//   - 执行类：POST /api/v1/virt/... 下发 shell 模板到 agent 执行。
//
// 虚拟化平台定位：
//   - KVM：Linux 原生虚拟化（libvirt/virsh），开源私有云底座。
//   - vSphere：VMware 企业虚拟化（vim-cmd/govc），传统数据中心存量最大。
//   - OpenStack：开源私有云平台（openstack CLI），IaaS 全套。
//   - Proxmox：开源虚拟化平台（qm/pct），中小规模私有云常用。
//
// API 端点：
//   - GET  /api/v1/virt/{deviceID}         — 虚拟化平台状态
//   - POST /api/v1/virt/deploy              — 部署虚拟化宿主（{deviceID, type, mode}）
//   - POST /api/v1/virt/{type}/{action}     — 启停重启/状态（libvirtd 等）
//   - POST /api/v1/virt/{type}/vm           — VM 管理（list/start/stop/restart/delete）
//   - POST /api/v1/virt/{type}/snapshot     — 快照管理（list/create/revert/delete）
//
// 安全：type/action 走白名单；VM 名/快照名走正则校验；凭证经环境变量注入；
// 5xx 错误走 writeInternalError，不回吐 err.Error()。

package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// 虚拟化类型白名单。
var allowedVirtTypes = map[string]bool{
	"kvm":       true,
	"vsphere":   true,
	"openstack": true,
	"proxmox":   true,
}

// 虚拟化部署模式白名单。
var allowedVirtModes = map[string]map[string]bool{
	"kvm": {
		"host": true, // KVM 宿主机
	},
	"vsphere": {
		"client": true, // 通过 API/CLI 连接 vCenter
	},
	"openstack": {
		"client": true, // 通过 CLI 连接 OpenStack
	},
	"proxmox": {
		"host": true, // Proxmox 宿主
	},
}

// 虚拟化操作动作白名单。
var allowedVirtActions = map[string]bool{
	"deploy":   true,
	"start":    true,
	"stop":     true,
	"restart":  true,
	"status":   true,
	"undeploy": true,
}

// VM 操作动作白名单。
var allowedVirtVMActions = map[string]bool{
	"list":    true,
	"start":   true,
	"stop":    true,
	"restart": true,
	"delete":  true,
}

// 快照操作动作白名单。
var allowedVirtSnapshotActions = map[string]bool{
	"list":   true,
	"create": true,
	"revert": true,
	"delete": true,
}

// VM 名/快照名正则。
var virtNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.\-]{1,128}$`)

// virtDeployReq 虚拟化部署请求体。
type virtDeployReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"` // kvm/vsphere/openstack/proxmox
	Mode     string `json:"mode"` // host/client
}

// virtOpsReq 通用虚拟化操作请求体。
type virtOpsReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`
	Action   string `json:"action"`
}

// virtVMReq VM 管理请求体。
type virtVMReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`   // kvm/vsphere/openstack/proxmox
	Action   string `json:"action"` // list/start/stop/restart/delete
	Name     string `json:"name"`   // VM 名称
}

// virtSnapshotReq 快照管理请求体。
type virtSnapshotReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`     // kvm/proxmox
	Action   string `json:"action"`   // list/create/revert/delete
	Name     string `json:"name"`     // VM 名称
	Snapshot string `json:"snapshot"` // 快照名称
}

// handleVirt 查询设备虚拟化平台状态。
func (s *Server) handleVirt(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodGet {
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
	dev := s.store.Device(deviceID)
	if dev == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	if actx.TenantID != "" && dev.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	metrics := s.store.DeviceMetrics(deviceID)
	if metrics == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "no metrics yet"})
		return
	}
	type virtInfo struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	var result []virtInfo
	for _, vType := range []string{"kvm", "vsphere", "openstack", "proxmox"} {
		info := virtInfo{Type: vType, Status: "not-installed"}
		if metrics.Services != nil {
			for _, svc := range metrics.Services {
				// KVM: libvirtd；Proxmox: pveproxy/pvedaemon；vSphere/OpenStack 为客户端连接
				if strings.EqualFold(svc.Name, vType) ||
					strings.EqualFold(svc.Name, vType+"-server") ||
					(vType == "kvm" && strings.EqualFold(svc.Name, "libvirtd")) ||
					(vType == "proxmox" && strings.EqualFold(svc.Name, "pveproxy")) {
					info.Status = svc.Status
					break
				}
			}
		}
		if info.Status == "not-installed" {
			continue
		}
		result = append(result, info)
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"deviceID": deviceID, "virt": result})
}

// handleVirtDeploy 处理 POST /api/v1/virt/deploy：部署虚拟化宿主/配置客户端。
func (s *Server) handleVirtDeploy(w http.ResponseWriter, r *http.Request) {
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
	var req virtDeployReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !allowedVirtTypes[req.Type] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported virt type (kvm/vsphere/openstack/proxmox)"})
		return
	}
	mode := req.Mode
	if mode == "" {
		if req.Type == "kvm" || req.Type == "proxmox" {
			mode = "host"
		} else {
			mode = "client"
		}
	}
	if !allowedVirtModes[req.Type][mode] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported mode for " + req.Type})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Mode = mode

	script := buildVirtDeployScript(req)
	action := "virt_deploy_" + req.Type + "_" + mode

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "virt.deploy."+req.Type, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Type + ":" + mode,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVirtDeployScript(req virtDeployReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("if command -v yum >/dev/null 2>&1; then PKG_MGR=yum; elif command -v dnf >/dev/null 2>&1; then PKG_MGR=dnf; elif command -v apt-get >/dev/null 2>&1; then PKG_MGR=apt-get; else echo '未找到包管理器' && exit 1; fi\n")
	switch req.Type {
	case "kvm":
		b.WriteString("# KVM 宿主机部署（libvirt + qemu-kvm）\n")
		b.WriteString("# 检查 CPU 虚拟化支持\n")
		b.WriteString("grep -qE 'vmx|svm' /proc/cpuinfo || echo '警告: CPU 未检测到虚拟化扩展'\n")
		b.WriteString("if command -v virsh >/dev/null 2>&1; then echo 'KVM/libvirt 已安装'; else\n")
		b.WriteString("  $PKG_MGR install -y qemu-kvm libvirt libvirt-daemon-system virtinst bridge-utils || $PKG_MGR install -y qemu-kvm libvirt virt-install || echo '请检查 KVM 包名'\n")
		b.WriteString("fi\n")
		b.WriteString("systemctl enable --now libvirtd || true\n")
		b.WriteString("echo 'KVM 宿主机已就绪 (libvirtd)'\n")
	case "vsphere":
		b.WriteString("# vSphere 客户端（govc/vim-cmd）\n")
		b.WriteString("if command -v govc >/dev/null 2>&1; then echo 'govc 已安装'; else\n")
		b.WriteString("  curl -fsSL https://github.com/vmware/govmomi/releases/latest/download/govc_Linux_x86_64.tar.gz | tar xz -C /usr/local/bin govc || echo '安装 govc 失败'\n")
		b.WriteString("fi\n")
		b.WriteString("echo 'vSphere 客户端就绪：export GOVC_URL=https://vcenter GOVC_USERNAME=... GOVC_PASSWORD=...'\n")
	case "openstack":
		b.WriteString("# OpenStack 客户端\n")
		b.WriteString("if command -v openstack >/dev/null 2>&1; then echo 'openstack CLI 已安装'; else\n")
		b.WriteString("  $PKG_MGR install -y python3-openstackclient || pip3 install python-openstackclient || echo '安装失败'\n")
		b.WriteString("fi\n")
		b.WriteString("echo 'OpenStack 客户端就绪：需 source openrc.sh（OS_AUTH_URL/OS_USERNAME/OS_PASSWORD）'\n")
	case "proxmox":
		b.WriteString("# Proxmox 宿主（qm/pct 工具检查）\n")
		b.WriteString("if command -v qm >/dev/null 2>&1; then echo 'Proxmox 工具已安装'; else echo 'Proxmox 需官方 ISO 安装（基于 Debian），本机非 PVE 宿主' && exit 1; fi\n")
		b.WriteString("systemctl enable --now pveproxy pvedaemon || true\n")
		b.WriteString("echo 'Proxmox 宿主已就绪 (Web:8006)'\n")
	}
	return b.String()
}

// handleVirtAction 处理 POST /api/v1/virt/{type}/{action}：平台服务启停。
func (s *Server) handleVirtAction(w http.ResponseWriter, r *http.Request, vType, action string) {
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
	if !allowedVirtTypes[vType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported virt type"})
		return
	}
	if !allowedVirtActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action"})
		return
	}
	var req virtOpsReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Type = vType
	req.Action = action

	script := buildVirtActionScript(req)
	taskAction := "virt_" + action + "_" + vType

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "virt.action."+vType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: vType,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVirtActionScript(req virtOpsReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "kvm":
		b.WriteString("# KVM/libvirtd 启停\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start libvirtd || true\n")
		case "stop":
			b.WriteString("systemctl stop libvirtd || true\n")
		case "restart":
			b.WriteString("systemctl restart libvirtd || true\n")
		case "status":
			b.WriteString("systemctl is-active libvirtd || echo 'not running'\nvirsh list --all 2>/dev/null | head -20 || true\n")
		}
	case "proxmox":
		b.WriteString("# Proxmox 服务启停\n")
		switch req.Action {
		case "start":
			b.WriteString("systemctl start pveproxy pvedaemon pve-cluster || true\n")
		case "stop":
			b.WriteString("systemctl stop pveproxy pvedaemon || true\n")
		case "restart":
			b.WriteString("systemctl restart pveproxy pvedaemon || true\n")
		case "status":
			b.WriteString("qm list 2>/dev/null || echo 'not running'\n")
		}
	case "vsphere":
		b.WriteString("# vSphere 连接测试（需 GOVC_URL/GOVC_USERNAME/GOVC_PASSWORD）\n")
		switch req.Action {
		case "status":
			b.WriteString("govc about 2>/dev/null || echo 'vSphere 未连接（需配置 GOVC_* 环境变量）'\n")
		default:
			b.WriteString("echo 'vSphere 是外部平台，启停由 vCenter 控制'\n")
		}
	case "openstack":
		b.WriteString("# OpenStack 连接测试（需 OS_AUTH_URL/OS_USERNAME/OS_PASSWORD）\n")
		switch req.Action {
		case "status":
			b.WriteString("openstack token issue 2>/dev/null | head -5 || echo 'OpenStack 未连接（需 source openrc.sh）'\n")
		default:
			b.WriteString("echo 'OpenStack 是外部平台，启停由控制节点管理'\n")
		}
	}
	b.WriteString(fmt.Sprintf("echo '虚拟化 %s %s 操作完成\n", req.Type, req.Action))
	return b.String()
}

// handleVirtVM 处理 POST /api/v1/virt/{type}/vm：VM 管理。
func (s *Server) handleVirtVM(w http.ResponseWriter, r *http.Request, vType string) {
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
	if !allowedVirtTypes[vType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported virt type"})
		return
	}
	var req virtVMReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedVirtVMActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (list/start/stop/restart/delete)"})
		return
	}
	if req.Action != "list" && !virtNameRe.MatchString(req.Name) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid VM name"})
		return
	}
	req.Type = vType

	script := buildVirtVMScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "virt_vm_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "virt.vm."+vType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "virt_vm_" + req.Action, Target: req.Name,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVirtVMScript(req virtVMReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "kvm":
		b.WriteString("# KVM VM 管理（virsh）\n")
		switch req.Action {
		case "list":
			b.WriteString("virsh list --all\n")
		case "start":
			b.WriteString(fmt.Sprintf("virsh start %s\n", req.Name))
		case "stop":
			b.WriteString(fmt.Sprintf("virsh shutdown %s || virsh destroy %s\n", req.Name, req.Name))
		case "restart":
			b.WriteString(fmt.Sprintf("virsh reboot %s || (virsh destroy %s && virsh start %s)\n", req.Name, req.Name, req.Name))
		case "delete":
			b.WriteString(fmt.Sprintf("virsh destroy %s 2>/dev/null || true\nvirsh undefine %s --remove-all-storage || echo '删除失败（VM 不存在？）'\n", req.Name, req.Name))
		}
	case "proxmox":
		b.WriteString("# Proxmox VM 管理（qm，VMID 为数字）\n")
		switch req.Action {
		case "list":
			b.WriteString("qm list\n")
		case "start":
			b.WriteString(fmt.Sprintf("qm start %s\n", req.Name))
		case "stop":
			b.WriteString(fmt.Sprintf("qm shutdown %s || qm stop %s\n", req.Name, req.Name))
		case "restart":
			b.WriteString(fmt.Sprintf("qm reboot %s || (qm stop %s && qm start %s)\n", req.Name, req.Name, req.Name))
		case "delete":
			b.WriteString(fmt.Sprintf("qm destroy %s --purge || echo '删除失败'\n", req.Name))
		}
	case "vsphere":
		b.WriteString("# vSphere VM 管理（govc）\n")
		switch req.Action {
		case "list":
			b.WriteString("govc ls /DC/vm 2>/dev/null || govc find . -type m 2>/dev/null || echo '未连接 vSphere'\n")
		case "start":
			b.WriteString(fmt.Sprintf("govc vm.power -on %s\n", req.Name))
		case "stop":
			b.WriteString(fmt.Sprintf("govc vm.power -off %s\n", req.Name))
		case "restart":
			b.WriteString(fmt.Sprintf("govc vm.power -reset %s\n", req.Name))
		case "delete":
			b.WriteString(fmt.Sprintf("govc vm.destroy %s || echo '删除失败'\n", req.Name))
		}
	case "openstack":
		b.WriteString("# OpenStack VM 管理（openstack CLI）\n")
		switch req.Action {
		case "list":
			b.WriteString("openstack server list\n")
		case "start":
			b.WriteString(fmt.Sprintf("openstack server start %s\n", req.Name))
		case "stop":
			b.WriteString(fmt.Sprintf("openstack server stop %s\n", req.Name))
		case "restart":
			b.WriteString(fmt.Sprintf("openstack server reboot %s\n", req.Name))
		case "delete":
			b.WriteString(fmt.Sprintf("openstack server delete %s || echo '删除失败'\n", req.Name))
		}
	}
	b.WriteString(fmt.Sprintf("echo '虚拟化 %s VM %s 操作完成\n", req.Type, req.Action))
	return b.String()
}

// handleVirtSnapshot 处理 POST /api/v1/virt/{type}/snapshot：快照管理。
func (s *Server) handleVirtSnapshot(w http.ResponseWriter, r *http.Request, vType string) {
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
	if vType != "kvm" && vType != "proxmox" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "snapshot only supports kvm/proxmox"})
		return
	}
	var req virtSnapshotReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedVirtSnapshotActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (list/create/revert/delete)"})
		return
	}
	if !virtNameRe.MatchString(req.Name) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid VM name"})
		return
	}
	if req.Action != "list" && !virtNameRe.MatchString(req.Snapshot) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid snapshot name"})
		return
	}
	req.Type = vType

	script := buildVirtSnapshotScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "virt_snapshot_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "virt.snapshot."+vType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "virt_snapshot_" + req.Action, Target: req.Name + "@" + req.Snapshot,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildVirtSnapshotScript(req virtSnapshotReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "kvm":
		b.WriteString("# KVM 快照（virsh）\n")
		switch req.Action {
		case "list":
			b.WriteString(fmt.Sprintf("virsh snapshot-list %s\n", req.Name))
		case "create":
			b.WriteString(fmt.Sprintf("virsh snapshot-create-as %s %s --disk-only --atomic || virsh snapshot-create-as %s %s\n", req.Name, req.Snapshot, req.Name, req.Snapshot))
		case "revert":
			b.WriteString(fmt.Sprintf("virsh snapshot-revert %s %s\n", req.Name, req.Snapshot))
		case "delete":
			b.WriteString(fmt.Sprintf("virsh snapshot-delete %s %s\n", req.Name, req.Snapshot))
		}
	case "proxmox":
		b.WriteString("# Proxmox 快照（qm）\n")
		switch req.Action {
		case "list":
			b.WriteString(fmt.Sprintf("qm listsnapshot %s\n", req.Name))
		case "create":
			b.WriteString(fmt.Sprintf("qm snapshot %s %s\n", req.Name, req.Snapshot))
		case "revert":
			b.WriteString(fmt.Sprintf("qm rollback %s %s\n", req.Name, req.Snapshot))
		case "delete":
			b.WriteString(fmt.Sprintf("qm delsnapshot %s %s\n", req.Name, req.Snapshot))
		}
	}
	b.WriteString(fmt.Sprintf("echo '虚拟化 %s 快照 %s 操作完成\n", req.Type, req.Action))
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerVirtRoutes 注册虚拟化运维路由。
func (s *Server) registerVirtRoutes(mux *http.ServeMux) {
	// 固定路径优先：/api/v1/virt/{type}/{vm|snapshot}
	for _, t := range []string{"kvm", "vsphere", "openstack", "proxmox"} {
		tt := t
		mux.HandleFunc("/api/v1/virt/"+tt+"/vm", func(w http.ResponseWriter, r *http.Request) { s.handleVirtVM(w, r, tt) })
	}
	mux.HandleFunc("/api/v1/virt/kvm/snapshot", s.handleVirtSnapshotKVM)
	mux.HandleFunc("/api/v1/virt/proxmox/snapshot", s.handleVirtSnapshotProxmox)
	// 泛路由
	mux.HandleFunc("/api/v1/virt/deploy", s.handleVirtDeploy)
	mux.HandleFunc("/api/v1/virt/", s.handleVirtRouting)
}

func (s *Server) handleVirtSnapshotKVM(w http.ResponseWriter, r *http.Request) {
	s.handleVirtSnapshot(w, r, "kvm")
}
func (s *Server) handleVirtSnapshotProxmox(w http.ResponseWriter, r *http.Request) {
	s.handleVirtSnapshot(w, r, "proxmox")
}

func (s *Server) handleVirtRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/virt/")
	segments := strings.Split(path, "/")
	switch len(segments) {
	case 1:
		if segments[0] != "" {
			s.handleVirt(w, r, segments[0])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	case 2:
		if segments[0] != "" && segments[1] != "" {
			s.handleVirtAction(w, r, segments[0], segments[1])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}
