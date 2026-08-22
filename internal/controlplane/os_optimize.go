// Package controlplane: os_optimize.go 实现 OS 基础环境优化预置模板与 API。
//
// 提供运维场景化的 shell 脚本模板（内核/网络/安全/时间同步/SSH/磁盘/系统/用户），
// 通过 GET /api/v1/os-templates 列表、GET /api/v1/os-templates/{id} 详情、
// POST /api/v1/os-templates/{id}/execute 在指定 agent 上执行（复用 task 下发通道）。
//
// 设计要点（模板从内存常量改为 store 持久化，支持在线 CRUD）：
//   - 预置模板仍以内存常量 osTemplates 维护（版本随代码升级），启动时 seedPresetOSTemplates
//     将其幂等写入 store（按 ID 去重，已存在不覆盖，保留用户在线修改）。
//   - API 从 store 读取模板列表/详情；store 为空时回退到内存常量（向后兼容）。
//   - 新增 CRUD：POST /api/v1/os-templates 创建、PUT /api/v1/os-templates/{id} 更新、
//     DELETE /api/v1/os-templates/{id} 删除。
//   - execute 将 params 通过 `set --` 注入脚本位置参数，agent 侧 `sh -c command` 执行时 $1/$2 即可拿到。
//   - 租户隔离与审计复用 handleCreateTask 同款逻辑（requireAuth + authctx + Audit + 事件总线 + SSE）。
package controlplane

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"opsmesh/internal/events"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// OSTemplate 预置 OS 优化任务模板。
// Commands 为一段 shell 脚本（在目标 Linux 主机以 `sh -c` 执行）；
// 需要参数的模板在脚本内通过 $1/$2/... 引用（旧模式）或 {name}/{port}/... 占位符引用（新模式），
// execute 时由控制面注入位置参数或做占位符替换。
type OSTemplate struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Category    string    `json:"category"` // kernel/network/security/time/ssh/disk/system/user
	Description string    `json:"description"`
	Commands    string    `json:"commands"`         // shell 脚本（可用 #!/bin/bash 开头）
	Risk        string    `json:"risk"`             // low/medium/high
	Tags        []string  `json:"tags"`             // 标签
	OS          string    `json:"os"`               // 适用操作系统：centos/ubuntu/all
	Params      []OSParam `json:"params,omitempty"` // 参数定义（新模式占位符替换 + 验证）
}

// OSParam OS 优化模板参数定义。
type OSParam struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     string `json:"default"`
	Required    bool   `json:"required"`
	Type        string `json:"type"` // string/int
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

	// ---------------- Phase 1/2 扩展模板 ----------------
	// swap-setup (kernel, low) — 配置 swap 空间
	{
		ID:          "swap-setup",
		Name:        "配置 Swap 空间",
		Category:    "kernel",
		Description: "创建并启用 swap 文件，写入 /etc/fstab 持久化；参数 size 指定大小（如 2G/4G）",
		Commands: `#!/bin/bash
set -e
SIZE="{size}"
if swapon --show 2>/dev/null | grep -q "/swapfile"; then
  echo "swap already active, skip"
  exit 0
fi
fallocate -l "$SIZE" /swapfile
chmod 600 /swapfile
mkswap /swapfile
swapon /swapfile
grep -q "/swapfile" /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
echo "swap-setup done: size=$SIZE"
`,
		Risk:   "low",
		Tags:   []string{"kernel", "swap", "memory"},
		OS:     "all",
		Params: []OSParam{{Name: "size", Description: "swap 文件大小（如 2G/4G）", Default: "2G", Required: true, Type: "string"}},
	},
	// limits-config (kernel, low) — 配置 /etc/security/limits.conf
	{
		ID:          "limits-config",
		Name:        "配置文件描述符限制",
		Category:    "kernel",
		Description: "在 /etc/security/limits.conf 设置 nofile 上限；参数 nofile 指定值",
		Commands: `#!/bin/bash
set -e
NOFILE="{nofile}"
grep -q "opsmesh-limits" /etc/security/limits.conf 2>/dev/null || cat >> /etc/security/limits.conf <<EOF
# opsmesh-limits
* soft nofile $NOFILE
* hard nofile $NOFILE
EOF
echo "limits-config done: nofile=$NOFILE"
`,
		Risk:   "low",
		Tags:   []string{"kernel", "limits", "fd"},
		OS:     "all",
		Params: []OSParam{{Name: "nofile", Description: "文件描述符上限", Default: "65536", Required: true, Type: "int"}},
	},
	// net-security (security, medium) — 网络安全参数
	{
		ID:          "net-security",
		Name:        "网络安全参数加固",
		Category:    "security",
		Description: "禁用 ICMP 重定向与广播响应，加固网络协议栈",
		Commands: `#!/bin/bash
set -e
sysctl -w net.ipv4.conf.all.accept_redirects=0
sysctl -w net.ipv4.conf.all.send_redirects=0
sysctl -w net.ipv4.conf.default.accept_redirects=0
sysctl -w net.ipv4.conf.default.send_redirects=0
sysctl -w net.ipv4.icmp_echo_ignore_broadcasts=1
sysctl -w net.ipv4.icmp_ignore_bogus_error_responses=1
cat > /etc/sysctl.d/99-opsmesh-netsec.conf <<'EOF'
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.all.send_redirects = 0
net.ipv4.conf.default.accept_redirects = 0
net.ipv4.conf.default.send_redirects = 0
net.ipv4.icmp_echo_ignore_broadcasts = 1
net.ipv4.icmp_ignore_bogus_error_responses = 1
EOF
sysctl --system
echo "net-security done"
`,
		Risk: "medium",
		Tags: []string{"security", "network", "sysctl"},
		OS:   "all",
	},
	// ntp-setup (time, low) — NTP 时间同步
	{
		ID:          "ntp-setup",
		Name:        "NTP 时间同步",
		Category:    "time",
		Description: "安装 ntp 并启动 ntpd 服务；参数 ntpserver 指定上游 NTP 服务器",
		Commands: `#!/bin/bash
set -e
NTPSERVER="{ntpserver}"
if command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
  yum install -y ntp || dnf install -y ntp
elif command -v apt >/dev/null 2>&1; then
  apt update -y && apt install -y ntp
else
  echo "unsupported package manager" >&2
  exit 2
fi
sed -i '/^server /d' /etc/ntp.conf
echo "server $NTPSERVER iburst" >> /etc/ntp.conf
systemctl enable ntpd 2>/dev/null || systemctl enable ntp
systemctl restart ntpd 2>/dev/null || systemctl restart ntp
ntpq -p 2>/dev/null || true
echo "ntp-setup done: server=$NTPSERVER"
`,
		Risk:   "low",
		Tags:   []string{"time", "ntp", "sync"},
		OS:     "all",
		Params: []OSParam{{Name: "ntpserver", Description: "上游 NTP 服务器", Default: "pool.ntp.org", Required: true, Type: "string"}},
	},
	// dns-config (network, low) — DNS 配置
	{
		ID:          "dns-config",
		Name:        "DNS 配置",
		Category:    "network",
		Description: "配置 /etc/resolv.conf DNS 服务器；参数 dns1/dns2 指定主备 DNS",
		Commands: `#!/bin/bash
set -e
DNS1="{dns1}"
DNS2="{dns2}"
cat > /etc/resolv.conf <<EOF
nameserver $DNS1
nameserver $DNS2
EOF
echo "dns-config done: dns1=$DNS1 dns2=$DNS2"
`,
		Risk:   "low",
		Tags:   []string{"network", "dns", "resolv"},
		OS:     "all",
		Params: []OSParam{{Name: "dns1", Description: "主 DNS 服务器", Default: "8.8.8.8", Required: true, Type: "string"}, {Name: "dns2", Description: "备 DNS 服务器", Default: "114.114.114.114", Required: true, Type: "string"}},
	},
	// tcp-tune (network, low) — TCP 连接优化
	{
		ID:          "tcp-tune",
		Name:        "TCP 连接优化",
		Category:    "network",
		Description: "优化 TCP 连接复用与超时参数，提升短连接场景性能",
		Commands: `#!/bin/bash
set -e
sysctl -w net.ipv4.tcp_tw_reuse=1
sysctl -w net.ipv4.tcp_fin_timeout=30
sysctl -w net.ipv4.tcp_keepalive_time=600
cat > /etc/sysctl.d/99-opsmesh-tcp.conf <<'EOF'
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 30
net.ipv4.tcp_keepalive_time = 600
EOF
sysctl --system
echo "tcp-tune done"
`,
		Risk: "low",
		Tags: []string{"network", "tcp", "sysctl"},
		OS:   "all",
	},
	// memory-tune (kernel, low) — 内存参数优化
	{
		ID:          "memory-tune",
		Name:        "内存参数优化",
		Category:    "kernel",
		Description: "调整 swappiness 与 dirty_ratio，优化内存回收与写回策略",
		Commands: `#!/bin/bash
set -e
sysctl -w vm.swappiness=10
sysctl -w vm.dirty_ratio=10
sysctl -w vm.dirty_background_ratio=5
cat > /etc/sysctl.d/99-opsmesh-mem.conf <<'EOF'
vm.swappiness = 10
vm.dirty_ratio = 10
vm.dirty_background_ratio = 5
EOF
sysctl --system
echo "memory-tune done"
`,
		Risk: "low",
		Tags: []string{"kernel", "memory", "sysctl"},
		OS:   "all",
	},
	// disk-io-tune (disk, medium) — 磁盘 IO 调优
	{
		ID:          "disk-io-tune",
		Name:        "磁盘 IO 调度器调优",
		Category:    "disk",
		Description: "设置磁盘 IO 调度器为 deadline；参数 device 指定磁盘（如 sda/vda）",
		Commands: `#!/bin/bash
set -e
DEVICE="{device}"
SCHED_PATH="/sys/block/$DEVICE/queue/scheduler"
if [ ! -f "$SCHED_PATH" ]; then
  echo "device scheduler not found: $SCHED_PATH" >&2
  exit 3
fi
echo deadline > "$SCHED_PATH"
cat "$SCHED_PATH"
echo "disk-io-tune done: device=$DEVICE scheduler=deadline"
`,
		Risk:   "medium",
		Tags:   []string{"disk", "io", "scheduler"},
		OS:     "all",
		Params: []OSParam{{Name: "device", Description: "磁盘设备名（如 sda/vda）", Default: "sda", Required: true, Type: "string"}},
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

// handleListOSTemplates 处理 /api/v1/os-templates：
//   - GET：列出所有模板（从 store 读取，store 为空回退预置；可选 category/risk/os 过滤）
//   - POST：创建新模板（CRUD，需 os:write 权限）
func (s *Server) handleListOSTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListOSTemplatesGet(w, r)
	case http.MethodPost:
		s.handleCreateOSTemplate(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListOSTemplatesGet 处理 GET /api/v1/os-templates：列出所有模板（从 store 读取，含回退）。
// 可选查询参数 category 过滤；可选 risk 过滤；可选 os 过滤。
func (s *Server) handleListOSTemplatesGet(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "os:read"); !ok {
		return
	}
	q := r.URL.Query()
	category := q.Get("category")
	risk := q.Get("risk")
	osFilter := q.Get("os")
	all := s.listOSTemplatesFromStore(actx.TenantID)
	out := make([]OSTemplate, 0, len(all))
	for _, t := range all {
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

// handleCreateOSTemplate 处理 POST /api/v1/os-templates：创建新 OS 模板（CRUD）。
// 请求体即 OSTemplate JSON；ID 为空时由 store 分配随机 ID。
// 需 os:write 权限；创建后审计 + 事件总线 + SSE 通知。
func (s *Server) handleCreateOSTemplate(w http.ResponseWriter, r *http.Request) {
	if err := s.verifyFederationRequest(r); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	caller, ok := s.requireProd(w, r, "os:write")
	if !ok {
		return
	}
	var tpl OSTemplate
	if err := decodeJSONBody(w, r, &tpl); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if tpl.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if tpl.Commands == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "commands is required"})
		return
	}
	// 基本字段校验：risk 必须为 low/medium/high（空则归一为 low）。
	tpl.Risk = normalizeRisk(tpl.Risk)
	st := osTemplateToStore(&tpl, actx.TenantID)
	if err := s.store.SaveOSTemplate(st); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "save template failed: " + err.Error()})
		return
	}
	// 回读以获取 store 分配的 ID/时间戳。
	saved := osTemplateFromStore(s.store.GetOSTemplate(st.ID))
	userID := ""
	if caller != nil {
		userID = caller.ID
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: userID, Action: "os_template_create", Target: st.ID, Detail: sanitizeAuditDetail("name=" + tpl.Name),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: userID,
			Action: "os_template_create", Target: st.ID, Detail: sanitizeAuditDetail("name=" + tpl.Name), Level: events.LevelInfo,
		})
	}
	s.publishEvent(r.Context(), "os_template_changed", actx.TenantID, map[string]string{"id": st.ID, "op": "create"})
	writeJSON(w, http.StatusCreated, saved)
}

// handleUpdateOSTemplate 处理 PUT /api/v1/os-templates/{id}：更新 OS 模板（CRUD）。
// 请求体为 OSTemplate JSON；ID 路径参数与 body.ID 不一致时以路径为准。
// 需 os:write 权限；不存在返回 404。
func (s *Server) handleUpdateOSTemplate(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.verifyFederationRequest(r); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	caller, ok := s.requireProd(w, r, "os:write")
	if !ok {
		return
	}
	var tpl OSTemplate
	if err := decodeJSONBody(w, r, &tpl); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if tpl.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if tpl.Commands == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "commands is required"})
		return
	}
	// 检查存在性（含回退预置模板：预置模板在 store 中已 seed，此处能查到）。
	existing := s.store.GetOSTemplate(id)
	if existing == nil {
		// 回退检查：若为预置模板 ID 且尚未 seed，允许"upsert"（首次写入 store）。
		if preset := osTemplateByID(id); preset == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
	}
	tpl.ID = id
	tpl.Risk = normalizeRisk(tpl.Risk)
	st := osTemplateToStore(&tpl, actx.TenantID)
	if err := s.store.SaveOSTemplate(st); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "save template failed: " + err.Error()})
		return
	}
	saved := osTemplateFromStore(s.store.GetOSTemplate(id))
	userID := ""
	if caller != nil {
		userID = caller.ID
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: userID, Action: "os_template_update", Target: id, Detail: sanitizeAuditDetail("name=" + tpl.Name),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: userID,
			Action: "os_template_update", Target: id, Detail: sanitizeAuditDetail("name=" + tpl.Name), Level: events.LevelInfo,
		})
	}
	s.publishEvent(r.Context(), "os_template_changed", actx.TenantID, map[string]string{"id": id, "op": "update"})
	writeJSON(w, http.StatusOK, saved)
}

// handleDeleteOSTemplate 处理 DELETE /api/v1/os-templates/{id}：删除 OS 模板（CRUD）。
// 需 os:write 权限；不存在返回 404；删除成功返回 204。
func (s *Server) handleDeleteOSTemplate(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	caller, ok := s.requireProd(w, r, "os:write")
	if !ok {
		return
	}
	existing := s.store.GetOSTemplate(id)
	if existing == nil {
		// 回退检查：预置模板 ID 但未 seed → 视为不存在。
		if osTemplateByID(id) == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		// 预置模板未 seed，直接返回 204（内存中无法删除，但 store 中本就不存在）。
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.store.DeleteOSTemplate(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	userID := ""
	if caller != nil {
		userID = caller.ID
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: userID, Action: "os_template_delete", Target: id, Detail: sanitizeAuditDetail("name=" + existing.Name),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: userID,
			Action: "os_template_delete", Target: id, Detail: sanitizeAuditDetail("name=" + existing.Name), Level: events.LevelInfo,
		})
	}
	s.publishEvent(r.Context(), "os_template_changed", actx.TenantID, map[string]string{"id": id, "op": "delete"})
	w.WriteHeader(http.StatusNoContent)
}

// handleOSTemplateByID 处理 GET /api/v1/os-templates/{id}：返回模板详情（从 store 读取，含回退）。
func (s *Server) handleOSTemplateByID(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	_, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "os:read"); !ok {
		return
	}
	t := s.getOSTemplateByID(id)
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
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if err := s.verifyFederationRequest(r); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "os:execute"); !ok {
		return
	}
	tpl := s.getOSTemplateByID(id)
	if tpl == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	var body struct {
		AgentID   string            `json:"agentID"`
		Params    []string          `json:"params"`
		ParamsMap map[string]string `json:"paramsMap"`
		TenantID  string            `json:"tenantID"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.AgentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}
	// 参数验证与 command 拼接：
	// - 新模式（模板有 Params 定义）：用 paramsMap 做占位符替换 + 验证；
	// - 旧模式（无 Params 定义）：用 params []string 通过 `set --` 注入位置参数。
	var command string
	if len(tpl.Params) > 0 {
		paramsMap := body.ParamsMap
		if paramsMap == nil {
			paramsMap = map[string]string{}
		}
		// 填充默认值 + 校验必填。
		for _, p := range tpl.Params {
			val, ok := paramsMap[p.Name]
			if !ok || val == "" {
				if p.Default != "" {
					paramsMap[p.Name] = p.Default
					continue
				}
				if p.Required {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "param required: " + p.Name})
					return
				}
			}
			_ = val
		}
		// 类型与语义验证。
		if err := validateOSParams(tpl.Params, paramsMap); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// shell 元字符校验：占位符替换前拒绝含元字符的值，防命令注入。
		if err := validateShellSafeValues(paramsMap); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		command = renderOSScript(tpl.Commands, paramsMap)
	} else {
		command = buildOSExecuteCommand(tpl.Commands, body.Params)
	}
	// 认证防御：强制使用头中的租户 ID，忽略 body 中的 tenantID，防 body 覆盖头租户越权。
	targetTenant := actx.TenantID
	agent := s.lookupAgent(body.AgentID)
	if agent == nil || (targetTenant != "" && agent.TenantID != targetTenant) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "agent not found or tenant mismatch"})
		return
	}
	// 拼接最终 command：通过 `set --` 注入位置参数，使脚本内 $1/$2/... 可用。

	task := s.store.CreateTask(&proto.Task{
		AgentID:    body.AgentID,
		TenantID:   targetTenant,
		Type:       proto.TaskTypeShell,
		Command:    command,
		MaxRetries: s.cfg.TaskMaxRetries,
	})
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: targetTenant,
		UserID:   actx.UserID,
		Action:   "execute_os_template",
		Target:   task.TaskID,
		Detail:   sanitizeAuditDetail("template=" + id + " agent=" + body.AgentID),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: targetTenant, UserID: actx.UserID,
			Action: "execute_os_template", Target: task.TaskID,
			Detail: sanitizeAuditDetail("template=" + id + " agent=" + body.AgentID), Level: events.LevelInfo,
		})
	}
	if s.metrics != nil {
		s.metrics.SetQueueDepth(s.store.PendingDepth())
	}
	// SSE：通知前端 OS 模板执行任务已创建。
	// 租户隔离：携带 targetTenant，仅同租户订阅者收到。
	// 携带 ctx 的 trace_id，使 SSE 事件与链路追踪关联。
	s.publishEvent(r.Context(), "task_status", targetTenant, map[string]string{
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

// renderOSScript 将脚本中的 {name}/{size}/... 占位符替换为 params 实际值。
// 占位符语法：{key}，未提供 key 时保留原占位符（便于排查）。
func renderOSScript(script string, params map[string]string) string {
	out := script
	for k, v := range params {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}

// validateOSParams 校验 OS 模板参数的类型与语义。
// - int 类型：必须为整数；若参数名为 port 则校验端口范围 1-65535。
// - string 类型：非空校验。
func validateOSParams(params []OSParam, values map[string]string) error {
	for _, p := range params {
		val, ok := values[p.Name]
		if !ok || val == "" {
			continue // 必填已在调用前处理
		}
		switch p.Type {
		case "int":
			n, err := strconv.Atoi(val)
			if err != nil {
				return fmt.Errorf("param %s must be integer, got %s", p.Name, val)
			}
			if p.Name == "port" {
				if err := validatePort(n); err != nil {
					return err
				}
			}
		case "string":
			if err := validateNonEmpty(p.Name, val); err != nil {
				return err
			}
		}
	}
	return nil
}

// validatePort 校验端口范围 1-65535。
func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", port)
	}
	return nil
}

// validateNonEmpty 校验字符串非空（trim 后）。
func validateNonEmpty(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	return nil
}

// validatePath 校验路径以 / 开头。
func validatePath(path string) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must start with /, got %s", path)
	}
	return nil
}

// shellUnsafeChars 是禁止出现在模板参数值中的 shell 元字符（命令注入防护）。
// 模板参数经 renderMiddlewareScript/renderOSScript 原样替换进 shell 脚本，由 agent 以 sh -c 执行；
// 值中若含以下字符即可截断/拼接命令造成目标机 RCE，故一律拒绝（含空格，防参数歧义）。
const shellUnsafeChars = " ;&|$`\n\r\t<>(){}\"'\\*?[]!#~"

// validateShellSafeValues 校验全部模板参数值不含 shell 元字符（命令注入防护）。
// 对 values 中每个键值做检查，任一值含元字符即返回错误。
// 调用点：middleware deploy/uninstall 与 OS 模板 execute 在渲染脚本前统一调用。
func validateShellSafeValues(values map[string]string) error {
	for name, val := range values {
		if strings.ContainsAny(val, shellUnsafeChars) {
			return fmt.Errorf("param %s contains shell metacharacters and is rejected for safety", name)
		}
	}
	return nil
}

// handleOSTemplateRouting 统一分派 /api/v1/os-templates/{id}... 子路径：
//   - GET    /api/v1/os-templates/{id}：模板详情
//   - PUT    /api/v1/os-templates/{id}：更新模板（CRUD）
//   - DELETE /api/v1/os-templates/{id}：删除模板（CRUD）
//   - POST   /api/v1/os-templates/{id}/execute：在指定 agent 上执行模板
//
// 注意：/api/v1/os-templates（无尾斜杠）由 handleListOSTemplates 处理；
// /api/v1/os-templates/（带尾斜杠但无 id）此处返回 400。
func (s *Server) handleOSTemplateRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/os-templates/")
	if idAndRest == "" {
		// 兜底：/api/v1/os-templates/（带尾斜杠）转给 list handler 处理 GET/POST。
		s.handleListOSTemplates(w, r)
		return
	}
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template id required"})
		return
	}
	switch {
	case len(parts) == 1:
		// /api/v1/os-templates/{id}
		switch r.Method {
		case http.MethodGet:
			s.handleOSTemplateByID(w, r, id)
		case http.MethodPut:
			s.handleUpdateOSTemplate(w, r, id)
		case http.MethodDelete:
			s.handleDeleteOSTemplate(w, r, id)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	case len(parts) == 2 && parts[1] == "execute":
		// POST /api/v1/os-templates/{id}/execute
		s.handleExecuteOSTemplate(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// ============================================================================
// ：OS 模板 store 持久化适配（转换 + seed + 查询回退）
// ============================================================================

// osTemplateToStore 将 controlplane.OSTemplate 转换为 store.OSTemplate。
// 整个 OSTemplate 序列化为 JSON 存入 Config 字段；store.OSTemplate 的 Name/OS 冗余存储便于 SQL 过滤。
func osTemplateToStore(t *OSTemplate, tenantID string) *store.OSTemplate {
	if t == nil {
		return nil
	}
	cfg, _ := json.Marshal(t)
	return &store.OSTemplate{
		ID:       t.ID,
		TenantID: tenantID,
		Name:     t.Name,
		OS:       t.OS,
		Config:   string(cfg),
	}
}

// osTemplateFromStore 将 store.OSTemplate 反转换为 controlplane.OSTemplate（从 Config 反序列化）。
// Config 为空或反序列化失败时，用 store 行的 ID/Name/OS 构造最小 OSTemplate（向后兼容）。
func osTemplateFromStore(st *store.OSTemplate) *OSTemplate {
	if st == nil {
		return nil
	}
	if st.Config == "" {
		return &OSTemplate{ID: st.ID, Name: st.Name, OS: st.OS}
	}
	var t OSTemplate
	if err := json.Unmarshal([]byte(st.Config), &t); err != nil {
		return &OSTemplate{ID: st.ID, Name: st.Name, OS: st.OS}
	}
	// 以 store 行的 ID/Name/OS 为准（防 Config 中过期值）。
	if st.ID != "" {
		t.ID = st.ID
	}
	if st.Name != "" {
		t.Name = st.Name
	}
	if st.OS != "" {
		t.OS = st.OS
	}
	return &t
}

// seedPresetOSTemplates 启动时将预置 OS 模板幂等写入 store（按 ID 去重，已存在不覆盖）。
// 保持向后兼容：store 为空时 API 回退到内存常量 osTemplates。
// 预置模板归入 "default" 租户，对所有租户可见。
func (s *Server) seedPresetOSTemplates() {
	for i := range osTemplates {
		tpl := &osTemplates[i]
		if existing := s.store.GetOSTemplate(tpl.ID); existing != nil {
			continue // 已存在（用户可能已在线修改），不覆盖
		}
		st := osTemplateToStore(tpl, "default")
		if err := s.store.SaveOSTemplate(st); err != nil {
			log.Printf("[controlplane] seed 预置 OS 模板 %s 失败: %v", tpl.ID, err)
		}
	}
}

// listOSTemplatesFromStore 从 store 读取 OS 模板列表（含回退）。
// 合并当前租户的模板与 default 租户的预置模板（按 ID 去重）；
// store 完全为空时回退到内存常量 osTemplates（向后兼容）。
func (s *Server) listOSTemplatesFromStore(tenantID string) []OSTemplate {
	// 取当前租户模板 + default 租户预置模板（合并去重）。
	stored := s.store.ListOSTemplates(tenantID)
	if tenantID != "" && tenantID != "default" {
		stored = append(stored, s.store.ListOSTemplates("default")...)
	}
	if len(stored) == 0 {
		// 回退到内存常量（store 未初始化或为空）。
		out := make([]OSTemplate, len(osTemplates))
		copy(out, osTemplates)
		return out
	}
	seen := make(map[string]bool, len(stored))
	out := make([]OSTemplate, 0, len(stored))
	for _, st := range stored {
		if seen[st.ID] {
			continue
		}
		seen[st.ID] = true
		if t := osTemplateFromStore(st); t != nil {
			out = append(out, *t)
		}
	}
	return out
}

// getOSTemplateByID 从 store 读取单个 OS 模板（含回退）。
// store 中不存在时回退到内存常量 osTemplateByID（向后兼容）。
func (s *Server) getOSTemplateByID(id string) *OSTemplate {
	if st := s.store.GetOSTemplate(id); st != nil {
		return osTemplateFromStore(st)
	}
	// 回退到预置模板（store 未 seed 或为空）。
	return osTemplateByID(id)
}

// normalizeRisk 将 risk 归一为合法值（low/medium/high），空或非法值归一为 low。
func normalizeRisk(risk string) string {
	switch risk {
	case "low", "medium", "high":
		return risk
	default:
		return "low"
	}
}
