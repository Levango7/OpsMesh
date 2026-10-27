// Package controlplane os_ops.go 实现操作系统级运维场景
// 支持：Linux (RedHat/Debian/SUSE/Arch/Alpine 家族)、Unix (AIX/HP-UX/Solaris)、Windows、HarmonyOS
// 场景：包管理 (yum/dnf/apt/zypper/pacman/apk)、服务管理 (systemd/sysvinit/launchd/Windows Service)、
// 防火墙 (firewalld/iptables/ufw/nftables/Windows Firewall)、MAC (SELinux/AppArmor)、
// OS 生命周期 (reboot/dist-upgrade/kernel live-patch)、架构感知
package controlplane

// OSDistroInfo OS 发行版信息
type OSDistroInfo struct {
	Family     string `json:"family"`     // redhat/debian/suse/arch/alpine/windows/macos/harmony
	Distro     string `json:"distro"`     // centos/rhel/rocky/alma/ubuntu/debian/suse/opensuse/arch/alpine/windows/harmonyos
	Version    string `json:"version"`    // e.g. 9.3, 22.04, 1.3
	Arch       string `json:"arch"`       // amd64/arm64/arm/386/riscv64
	KernelVer  string `json:"kernelVer"`  // e.g. 5.14.0-362
	PackageMgr string `json:"packageMgr"` // yum/dnf/apt/zypper/pacman/apk/winget/brew
	ServiceMgr string `json:"serviceMgr"` // systemd/sysvinit/launchd/windows-service
	Firewall   string `json:"firewall"`   // firewalld/iptables/ufw/nftables/windows-firewall
	MAC        string `json:"mac"`        // selinux/apparmor/none
}
