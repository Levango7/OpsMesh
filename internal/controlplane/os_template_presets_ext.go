// Package controlplane: os_template_presets_ext.go 定义 OS 优化预置模板（扩展子领域）。
//
// 从 os_optimize.go 拆分而来。osTemplatesExt 包含磁盘/系统/用户类模板及 Phase1/2 扩展模板，
// 与 os_template_presets.go 中 osTemplatesCore 共同组成 osTemplates（见 os_optimize.go）。
package controlplane

// osTemplatesExt 预置 OS 优化模板（扩展：磁盘/系统/用户 + Phase1/2）。
var osTemplatesExt = []OSTemplate{
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
