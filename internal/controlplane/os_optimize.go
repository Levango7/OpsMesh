// Package controlplane: os_optimize.go 实现 OS 基础环境优化预置模板与 API。
//
// 提供运维场景化的 shell 脚本模板（内核/网络/安全/时间同步/SSH/磁盘/系统/用户），
// 通过 GET /api/v1/os-templates 列表、GET /api/v1/os-templates/{id} 详情、
// POST /api/v1/os-templates/{id}/execute 在指定 agent 上执行（复用 task 下发通道）。
//
// 设计要点：
//   - 模板为内存常量，不落库（预置最佳实践，版本随代码升级）。
//   - execute 将 params 通过 `set --` 注入脚本位置参数，agent 侧 `sh -c command` 执行时 $1/$2 即可拿到。
//   - 租户隔离与审计复用 handleCreateTask 同款逻辑（requireAuth + authctx + Audit + 事件总线 + SSE）。
package controlplane

import (
	"net/http"
	"strings"

	"opsmesh/internal/authctx"
	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

// OSTemplate 预置 OS 优化任务模板。
// Commands 为一段 shell 脚本（在目标 Linux 主机以 `sh -c` 执行）；
// 需要参数的模板在脚本内通过 $1/$2/... 引用，execute 时由控制面注入位置参数。
type OSTemplate struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"` // kernel/network/security/time/ssh/disk/system/user
	Description string   `json:"description"`
	Commands    string   `json:"commands"` // shell 脚本（可用 #!/bin/bash 开头）
	Risk        string   `json:"risk"`     // low/medium/high
	Tags        []string `json:"tags"`     // 标签
	OS          string   `json:"os"`       // 适用操作系统：centos/ubuntu/all
}

// osTemplates 预置 OS 优化模板集合。
// 每个模板对应一类常见运维场景，脚本遵循"幂等 + 失败即退出"原则。
var osTemplates = []OSTemplate{
	// ---------------- 内核调优 (kernel) ----------------
	{
		ID:          "kernel-tune",
		Name:        "内核参数调优",
		Category:    "kernel",
		Description: "调整内核网络/内存/文件句柄参数，提升高并发与连接复用能力",
		Commands: `#!/bin/bash
set -e
# 内核参数调优（幂等：sysctl -w 重复执行无副作用）
sysctl -w net.core.somaxconn=65535
sysctl -w net.ipv4.tcp_max_syn_backlog=65535
sysctl -w vm.swappiness=10
sysctl -w fs.file-max=2097152
sysctl -w net.ipv4.tcp_tw_reuse=1
sysctl -w "net.ipv4.ip_local_port_range=1024 65535"
sysctl -w net.ipv4.tcp_fin_timeout=30
sysctl -w net.ipv4.tcp_keepalive_time=1200
sysctl -w net.ipv4.tcp_syncookies=1
# 持久化到 /etc/sysctl.d/99-opsmesh.conf
cat > /etc/sysctl.d/99-opsmesh.conf <<'EOF'
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
vm.swappiness = 10
fs.file-max = 2097152
net.ipv4.tcp_tw_reuse = 1
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_fin_timeout = 30
net.ipv4.tcp_keepalive_time = 1200
net.ipv4.tcp_syncookies = 1
EOF
sysctl --system
echo "kernel-tune done"
`,
		Risk: "medium",
		Tags: []string{"kernel", "sysctl", "concurrency"},
		OS:   "all",
	},
	{
		ID:          "fd-limit",
		Name:        "文件描述符限制",
		Category:    "kernel",
		Description: "提升全局与 pam 限制下的文件描述符上限到 65535",
		Commands: `#!/bin/bash
set -e
# /etc/security/limits.conf：pam 会话级限制
grep -q "opsmesh-fd" /etc/security/limits.conf 2>/dev/null || cat >> /etc/security/limits.conf <<'EOF'
# opsmesh-fd
* soft nofile 65535
* hard nofile 65535
root soft nofile 65535
root hard nofile 65535
EOF
# systemd 服务级默认（适用于 systemd 管理的服务）
mkdir -p /etc/systemd/system.conf.d
cat > /etc/systemd/system.conf.d/99-opsmesh-fd.conf <<'EOF'
[Manager]
DefaultLimitNOFILE=65535
EOF
mkdir -p /etc/systemd/user.conf.d
cat > /etc/systemd/user.conf.d/99-opsmesh-fd.conf <<'EOF'
[Manager]
DefaultLimitNOFILE=65535
EOF
systemctl daemon-reexec 2>/dev/null || true
echo "fd-limit done"
`,
		Risk: "low",
		Tags: []string{"kernel", "fd", "limits"},
		OS:   "all",
	},

	// ---------------- 网络 (network) ----------------
	{
		ID:          "network-tune",
		Name:        "网络参数调优",
		Category:    "network",
		Description: "调整 TCP keepalive 与网卡队列 backlog，优化长连接与高吞吐",
		Commands: `#!/bin/bash
set -e
sysctl -w net.ipv4.tcp_keepalive_time=600
sysctl -w net.ipv4.tcp_keepalive_intvl=30
sysctl -w net.ipv4.tcp_keepalive_probes=3
sysctl -w net.core.netdev_max_backlog=5000
sysctl -w net.ipv4.tcp_mtu_probing=1
sysctl -w net.ipv4.tcp_rmem="4096 87380 6291456"
sysctl -w net.ipv4.tcp_wmem="4096 65536 4194304"
cat > /etc/sysctl.d/99-opsmesh-network.conf <<'EOF'
net.ipv4.tcp_keepalive_time = 600
net.ipv4.tcp_keepalive_intvl = 30
net.ipv4.tcp_keepalive_probes = 3
net.core.netdev_max_backlog = 5000
net.ipv4.tcp_mtu_probing = 1
net.ipv4.tcp_rmem = 4096 87380 6291456
net.ipv4.tcp_wmem = 4096 65536 4194304
EOF
sysctl --system
echo "network-tune done"
`,
		Risk: "low",
		Tags: []string{"network", "tcp", "sysctl"},
		OS:   "all",
	},
	{
		ID:          "hostname-set",
		Name:        "设置主机名",
		Category:    "network",
		Description: "通过 hostnamectl 设置静态主机名；参数 $1=新主机名",
		Commands: `#!/bin/bash
set -e
if [ -z "$1" ]; then
  echo "usage: hostname-set <new-hostname>" >&2
  exit 2
fi
hostnamectl set-hostname "$1"
# 同步 /etc/hosts：移除旧 127.0.1.1 行后追加新行（幂等）
sed -i '/^127\.0\.1\.1[[:space:]]/d' /etc/hosts
echo "127.0.1.1 $1" >> /etc/hosts
echo "hostname-set done: $1"
`,
		Risk: "low",
		Tags: []string{"network", "hostname"},
		OS:   "all",
	},

	// ---------------- 安全 (security) ----------------
	{
		ID:          "selinux-disable",
		Name:        "关闭 SELinux",
		Category:    "security",
		Description: "临时关闭 SELinux 并修改配置文件为 disabled（重启后生效）",
		Commands: `#!/bin/bash
set -e
if command -v setenforce >/dev/null 2>&1; then
  setenforce 0 || true
fi
if [ -f /etc/selinux/config ]; then
  sed -i 's/^SELINUX=.*/SELINUX=disabled/' /etc/selinux/config
fi
echo "selinux-disable done"
`,
		Risk: "medium",
		Tags: []string{"security", "selinux"},
		OS:   "centos",
	},
	{
		ID:          "firewall-config",
		Name:        "防火墙配置",
		Category:    "security",
		Description: "检查 firewalld 状态并放行常用端口（22/80/443/8080/9090）",
		Commands: `#!/bin/bash
set -e
if ! command -v firewall-cmd >/dev/null 2>&1; then
  echo "firewalld not installed, skip"
  exit 0
fi
if ! systemctl is-active --quiet firewalld; then
  systemctl start firewalld
  systemctl enable firewalld
fi
for port in 22 80 443 8080 9090; do
  firewall-cmd --permanent --add-port=${port}/tcp
done
firewall-cmd --reload
echo "firewall-config done"
`,
		Risk: "medium",
		Tags: []string{"security", "firewall", "firewalld"},
		OS:   "centos",
	},

	// ---------------- 时间同步 (time) ----------------
	{
		ID:          "chrony-setup",
		Name:        "时间同步（chrony）",
		Category:    "time",
		Description: "安装 chrony 并配置 NTP 服务器（阿里云/腾讯云），启动并设为开机自启",
		Commands: `#!/bin/bash
set -e
if command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
  yum install -y chrony || dnf install -y chrony
elif command -v apt >/dev/null 2>&1; then
  apt update -y && apt install -y chrony
else
  echo "unsupported package manager" >&2
  exit 2
fi
cat > /etc/chrony.conf <<'EOF'
server ntp.aliyun.com iburst
server ntp.tencent.com iburst
pool cn.pool.ntp.org iburst
driftfile /var/lib/chrony/drift
makestep 1.0 3
rtcsync
logdir /var/log/chrony
EOF
systemctl enable --now chronyd 2>/dev/null || systemctl enable --now chrony
chronyc sources -v
echo "chrony-setup done"
`,
		Risk: "low",
		Tags: []string{"time", "chrony", "ntp"},
		OS:   "all",
	},

	// ---------------- SSH 加固 (ssh) ----------------
	{
		ID:          "ssh-harden",
		Name:        "SSH 安全加固",
		Category:    "ssh",
		Description: "禁用 root 密码登录与空密码，限制重试与登录宽限时间，重启 sshd",
		Commands: `#!/bin/bash
set -e
SSHD_CONFIG="/etc/ssh/sshd_config"
cp -n "$SSHD_CONFIG" "${SSHD_CONFIG}.opsmesh.bak" 2>/dev/null || true
# 幂等：先删旧行再加新行
update_or_add() {
  local key="$1" val="$2"
  sed -i "/^[#[:space:]]*${key}[[:space:]]/d" "$SSHD_CONFIG"
  echo "${key} ${val}" >> "$SSHD_CONFIG"
}
update_or_add PermitRootLogin no
update_or_add PermitEmptyPasswords no
update_or_add MaxAuthTries 3
update_or_add LoginGraceTime 30
update_or_add X11Forwarding no
update_or_add UseDNS no
# 校验配置语法
sshd -t
systemctl restart sshd
echo "ssh-harden done"
`,
		Risk: "high",
		Tags: []string{"ssh", "harden", "security"},
		OS:   "all",
	},

	// ---------------- 磁盘 (disk) ----------------
	{
		ID:          "disk-info",
		Name:        "磁盘信息收集",
		Category:    "disk",
		Description: "收集 df/lsblk/fdisk/mount 等磁盘信息，用于巡检与容量规划",
		Commands: `#!/bin/bash
set +e
echo "===== df -h ====="
df -h
echo "===== lsblk ====="
lsblk
echo "===== fdisk -l ====="
fdisk -l 2>/dev/null || parted -l 2>/dev/null
echo "===== mount ====="
mount | sort
echo "===== lvs/vgs/pvs ====="
vgs 2>/dev/null; pvs 2>/dev/null; lvs 2>/dev/null
echo "===== iostat ====="
iostat -x 1 2 2>/dev/null || true
echo "disk-info done"
`,
		Risk: "low",
		Tags: []string{"disk", "info", "inspect"},
		OS:   "all",
	},
	{
		ID:          "disk-lvm",
		Name:        "LVM 配置",
		Category:    "disk",
		Description: "在指定磁盘创建 PV/VG/LV 并格式化挂载；参数：$1=磁盘(如/dev/sdb) $2=VG名 $3=LV名 $4=大小(如10G)",
		Commands: `#!/bin/bash
set -e
DEV="$1"; VG="$2"; LV="$3"; SIZE="$4"
if [ -z "$DEV" ] || [ -z "$VG" ] || [ -z "$LV" ] || [ -z "$SIZE" ]; then
  echo "usage: disk-lvm <device> <vg> <lv> <size>" >&2
  echo "  e.g. disk-lvm /dev/sdb datavg datalv 10G" >&2
  exit 2
fi
if [ ! -b "$DEV" ]; then
  echo "device not found: $DEV" >&2
  exit 3
fi
pvcreate -y "$DEV"
vgcreate "$VG" "$DEV"
lvcreate -y -L "$SIZE" -n "$LV" "$VG"
mkfs.xfs "/dev/${VG}/${LV}"
MNT="/mnt/${LV}"
mkdir -p "$MNT"
grep -q "/dev/${VG}/${LV}" /etc/fstab || echo "/dev/${VG}/${LV} ${MNT} xfs defaults 0 0" >> /etc/fstab
mount -a
echo "disk-lvm done: /dev/${VG}/${LV} -> ${MNT}"
`,
		Risk: "high",
		Tags: []string{"disk", "lvm", "format"},
		OS:   "all",
	},

	// ---------------- 系统 (system) ----------------
	{
		ID:          "system-cleanup",
		Name:        "系统清理",
		Category:    "system",
		Description: "清理临时目录与 yum/apt 缓存及旧日志，释放磁盘空间",
		Commands: `#!/bin/bash
set +e
# 清理临时目录（保留 7 天内文件）
find /tmp -type f -mtime +7 -delete 2>/dev/null
find /var/tmp -type f -mtime +7 -delete 2>/dev/null
# 包管理器缓存
if command -v yum >/dev/null 2>&1; then
  yum clean all
  rm -rf /var/cache/yum
elif command -v dnf >/dev/null 2>&1; then
  dnf clean all
  rm -rf /var/cache/dnf
fi
if command -v apt >/dev/null 2>&1; then
  apt clean
  apt autoremove -y
  rm -rf /var/cache/apt/archives/*.deb
fi
# 旧日志（journald 保留 7 天）
journalctl --vacuum-time=7d 2>/dev/null || true
# 旧轮转日志
find /var/log -type f -name "*.gz" -mtime +30 -delete 2>/dev/null
echo "system-cleanup done"
`,
		Risk: "low",
		Tags: []string{"system", "cleanup", "cache"},
		OS:   "all",
	},
	{
		ID:          "system-info",
		Name:        "系统信息收集",
		Category:    "system",
		Description: "收集 CPU/内存/磁盘/网络/OS 版本/内核版本/运行时间等系统信息",
		Commands: `#!/bin/bash
set +e
echo "===== OS Release ====="
cat /etc/os-release 2>/dev/null
echo "===== Kernel ====="
uname -a
echo "===== CPU ====="
lscpu 2>/dev/null | head -20
echo "===== Memory ====="
free -h
echo "===== Disk ====="
df -h
echo "===== Network ====="
ip -o addr 2>/dev/null || ifconfig -a 2>/dev/null
echo "===== Uptime ====="
uptime
echo "===== Load ====="
cat /proc/loadavg
echo "===== Top 5 Procs ====="
ps aux --sort=-%mem 2>/dev/null | head -6 || ps aux 2>/dev/null | head -6
echo "system-info done"
`,
		Risk: "low",
		Tags: []string{"system", "info", "inspect"},
		OS:   "all",
	},
	{
		ID:          "system-update",
		Name:        "系统更新",
		Category:    "system",
		Description: "执行系统包更新（yum/dnf/apt 自动识别），同步安全补丁",
		Commands: `#!/bin/bash
set -e
if command -v dnf >/dev/null 2>&1; then
  dnf update -y
elif command -v yum >/dev/null 2>&1; then
  yum update -y
elif command -v apt >/dev/null 2>&1; then
  apt update -y
  apt upgrade -y
  apt autoremove -y
else
  echo "unsupported package manager" >&2
  exit 2
fi
echo "system-update done"
`,
		Risk: "medium",
		Tags: []string{"system", "update", "patch"},
		OS:   "all",
	},

	// ---------------- 用户管理 (user) ----------------
	{
		ID:          "user-create",
		Name:        "创建运维用户",
		Category:    "user",
		Description: "创建运维用户并可选配置 sudo 免密；参数：$1=用户名 $2=是否加 sudo（yes/no）",
		Commands: `#!/bin/bash
set -e
USER="$1"; SUDO="$2"
if [ -z "$USER" ]; then
  echo "usage: user-create <username> [yes|no]" >&2
  exit 2
fi
if id "$USER" >/dev/null 2>&1; then
  echo "user already exists: $USER"
else
  useradd -m -s /bin/bash "$USER"
  echo "user created: $USER"
fi
# 强制首次登录改密
chage -d 0 "$USER"
if [ "$SUDO" = "yes" ]; then
  echo "$USER ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/"$USER"
  chmod 0440 /etc/sudoers.d/"$USER"
  visudo -cf
  echo "sudo granted: $USER"
fi
echo "user-create done: $USER"
`,
		Risk: "medium",
		Tags: []string{"user", "sudo", "ops"},
		OS:   "all",
	},
}

// osTemplateByID 按 ID 查找预置模板，未找到返回 nil。
func osTemplateByID(id string) *OSTemplate {
	for i := range osTemplates {
		if osTemplates[i].ID == id {
			return &osTemplates[i]
		}
	}
	return nil
}

// handleListOSTemplates 处理 GET /api/v1/os-templates：列出所有预置模板。
// 可选查询参数 category 过滤；可选 risk 过滤；可选 os 过滤。
func (s *Server) handleListOSTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	q := r.URL.Query()
	category := q.Get("category")
	risk := q.Get("risk")
	osFilter := q.Get("os")
	out := make([]OSTemplate, 0, len(osTemplates))
	for _, t := range osTemplates {
		if category != "" && t.Category != category {
			continue
		}
		if risk != "" && t.Risk != risk {
			continue
		}
		if osFilter != "" && t.OS != osFilter && t.OS != "all" {
			continue
		}
		out = append(out, t)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleOSTemplateByID 处理 GET /api/v1/os-templates/{id}：返回模板详情。
func (s *Server) handleOSTemplateByID(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	t := osTemplateByID(id)
	if t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleExecuteOSTemplate 处理 POST /api/v1/os-templates/{id}/execute：在指定 agent 上执行模板。
// 请求体: { "agentID": "...", "params": ["arg1", "arg2"] }
// 实现：将模板 Commands 通过 `set --` 注入位置参数后作为 shell task 下发，复用 store.CreateTask。
func (s *Server) handleExecuteOSTemplate(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.verifyFederationRequest(r); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx := authctx.FromHTTPHeader(r.Header)
	if s.requireAuth && actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing identity context (gateway auth required)"})
		return
	}
	tpl := osTemplateByID(id)
	if tpl == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	var body struct {
		AgentID  string   `json:"agentID"`
		Params   []string `json:"params"`
		TenantID string   `json:"tenantID"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.AgentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}
	targetTenant := body.TenantID
	if targetTenant == "" {
		targetTenant = actx.TenantID
	}
	agent := s.lookupAgent(body.AgentID)
	if agent == nil || (targetTenant != "" && agent.TenantID != targetTenant) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "agent not found or tenant mismatch"})
		return
	}
	// 拼接最终 command：通过 `set --` 注入位置参数，使脚本内 $1/$2/... 可用。
	command := buildOSExecuteCommand(tpl.Commands, body.Params)
	task := s.store.CreateTask(&proto.Task{
		AgentID:    body.AgentID,
		TenantID:   targetTenant,
		Type:       proto.TaskTypeShell,
		Command:    command,
		MaxRetries: s.cfg.TaskMaxRetries,
	})
	s.store.Audit(&proto.AuditEvent{
		TenantID: targetTenant,
		UserID:   actx.UserID,
		Action:   "execute_os_template",
		Target:   task.TaskID,
		Detail:   "template=" + id + " agent=" + body.AgentID,
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: targetTenant, UserID: actx.UserID,
			Action: "execute_os_template", Target: task.TaskID,
			Detail: "template=" + id + " agent=" + body.AgentID, Level: events.LevelInfo,
		})
	}
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.store.PendingDepth())
	}
	s.publishEvent("task_status", map[string]string{
		"taskID":  task.TaskID,
		"status":  task.Status,
		"agentID": body.AgentID,
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"task":         task,
		"templateID":   id,
		"templateName": tpl.Name,
	})
}

// buildOSExecuteCommand 将模板脚本与 params 拼接为最终 shell 命令。
// 通过 `set -- 'p1' 'p2' ...` 注入位置参数（单引号转义），脚本内 $1/$2 即可引用。
// 无 params 时直接返回原脚本。
func buildOSExecuteCommand(commands string, params []string) string {
	if len(params) == 0 {
		return commands
	}
	var b strings.Builder
	b.WriteString("set --")
	for _, p := range params {
		b.WriteString(" '")
		// 单引号转义：' -> '\''
		b.WriteString(strings.ReplaceAll(p, "'", `'\''`))
		b.WriteString("'")
	}
	b.WriteString("\n")
	b.WriteString(commands)
	return b.String()
}

// handleOSTemplateRouting 统一分派 /api/v1/os-templates/{id}... 子路径：
//   - GET  /api/v1/os-templates/{id}：模板详情
//   - POST /api/v1/os-templates/{id}/execute：在指定 agent 上执行模板
//
// 注意：/api/v1/os-templates（无尾斜杠）由 handleListOSTemplates 处理；
// /api/v1/os-templates/（带尾斜杠但无 id）此处返回 400。
func (s *Server) handleOSTemplateRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/os-templates/")
	if idAndRest == "" {
		// 兜底：/api/v1/os-templates/（带尾斜杠）转给 list handler 处理 GET。
		s.handleListOSTemplates(w, r)
		return
	}
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		http.Error(w, "template id required", http.StatusBadRequest)
		return
	}
	switch {
	case len(parts) == 1:
		// GET /api/v1/os-templates/{id}
		s.handleOSTemplateByID(w, r, id)
	case len(parts) == 2 && parts[1] == "execute":
		// POST /api/v1/os-templates/{id}/execute
		s.handleExecuteOSTemplate(w, r, id)
	default:
		http.NotFound(w, r)
	}
}
