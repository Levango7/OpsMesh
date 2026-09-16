// Package controlplane: os_template_presets.go 定义 OS 优化预置模板（核心子领域）。
//
// 从 os_optimize.go 拆分而来。osTemplatesCore 包含内核/网络/安全/时间/SSH 类模板，
// 与 os_template_presets_ext.go 中 osTemplatesExt 共同组成 osTemplates（见 os_optimize.go）。
// 每个模板对应一类常见运维场景，脚本遵循"幂等 + 失败即退出"原则。
package controlplane

// osTemplatesCore 预置 OS 优化模板（核心：内核/网络/安全/时间/SSH）。
var osTemplatesCore = []OSTemplate{
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
}
