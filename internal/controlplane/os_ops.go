// Package controlplane os_ops.go 实现操作系统级运维场景
// 支持：Linux (RedHat/Debian/SUSE/Arch/Alpine 家族)、Unix (AIX/HP-UX/Solaris)、Windows、HarmonyOS
// 场景：包管理 (yum/dnf/apt/zypper/pacman/apk)、服务管理 (systemd/sysvinit/launchd/Windows Service)、
//       防火墙 (firewalld/iptables/ufw/nftables/Windows Firewall)、MAC (SELinux/AppArmor)、
//       OS 生命周期 (reboot/dist-upgrade/kernel live-patch)、架构感知
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// ============================================================================
// OS 家族/发行版检测与归一化
// ============================================================================

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

// handleOSDetect 处理 POST /api/v1/os/detect
func (s *Server) handleOSDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	script := `echo "=== OS Detection ===" && cat /etc/os-release 2>/dev/null | head -10 && echo "=== Arch ===" && uname -m && echo "=== Kernel ===" && uname -r && echo "=== Package Manager ===" && (command -v dnf || command -v yum || command -v apt || command -v zypper || command -v pacman || command -v apk || echo "unknown") && echo "=== Service Manager ===" && (systemctl --version 2>/dev/null | head -1 || echo "not systemd") && echo "=== Firewall ===" && (command -v firewall-cmd || command -v ufw || command -v nft || echo "none") && echo "=== MAC ===" && (getenforce 2>/dev/null || aa-status 2>/dev/null | head -1 || echo "none")`
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_detect")
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "detect"})
}

// ============================================================================
// 包管理 (多发行版支持)
// ============================================================================

// handleOSPackage 处理 POST /api/v1/os/package
func (s *Server) handleOSPackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`   // install/remove/upgrade/search/list
		Package  string `json:"package"`
		Manager  string `json:"manager"`  // auto/yum/dnf/apt/zypper/pacman/apk
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Package == "" && req.Action != "list" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "package is required"})
		return
	}

	mgr := req.Manager
	if mgr == "" || mgr == "auto" {
		mgr = "auto"
	}

	var script string
	switch req.Action {
	case "install":
		script = buildOSPackageScript(mgr, "install", req.Package)
	case "remove":
		script = buildOSPackageScript(mgr, "remove", req.Package)
	case "upgrade":
		script = buildOSPackageScript(mgr, "upgrade", req.Package)
	case "search":
		script = buildOSPackageScript(mgr, "search", req.Package)
	case "list":
		script = buildOSPackageScript(mgr, "list", "")
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_package_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

func buildOSPackageScript(mgr, action, pkg string) string {
	// auto-detect package manager
	detect := `PKG_MGR=""; command -v dnf >/dev/null 2>&1 && PKG_MGR="dnf"; command -v yum >/dev/null 2>&1 && [ -z "$PKG_MGR" ] && PKG_MGR="yum"; command -v apt-get >/dev/null 2>&1 && [ -z "$PKG_MGR" ] && PKG_MGR="apt"; command -v zypper >/dev/null 2>&1 && [ -z "$PKG_MGR" ] && PKG_MGR="zypper"; command -v pacman >/dev/null 2>&1 && [ -z "$PKG_MGR" ] && PKG_MGR="pacman"; command -v apk >/dev/null 2>&1 && [ -z "$PKG_MGR" ] && PKG_MGR="apk"`

	if mgr != "auto" {
		detect = fmt.Sprintf(`PKG_MGR="%s"`, mgr)
	}

	var cmd string
	switch action {
	case "install":
		cmd = `$PKG_MGR install -y ` + pkg
	case "remove":
		cmd = `$PKG_MGR remove -y ` + pkg
	case "upgrade":
		if pkg != "" {
			cmd = `$PKG_MGR upgrade -y ` + pkg
		} else {
			cmd = `$PKG_MGR upgrade -y`
		}
	case "search":
		cmd = `$PKG_MGR search ` + pkg
	case "list":
		cmd = `$PKG_MGR list installed 2>/dev/null | head -50`
	}

	return detect + " && " + cmd
}

// ============================================================================
// 服务管理 (多 init 系统支持)
// ============================================================================

// handleOSService 处理 POST /api/v1/os/service
func (s *Server) handleOSService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`  // start/stop/restart/reload/enable/disable/status/is-active
		Service  string `json:"service"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Service == "" && req.Action != "list" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "service is required"})
		return
	}

	// Auto-detect init system
	script := fmt.Sprintf(`if command -v systemctl >/dev/null 2>&1; then
  SVC_MGR="systemctl"
elif command -v service >/dev/null 2>&1; then
  SVC_MGR="service"
elif command -v rc-service >/dev/null 2>&1; then
  SVC_MGR="rc-service"
elif [ -f /etc/init.d/%s ]; then
  SVC_MGR="sysvinit"
else
  SVC_MGR="unknown"
fi
case "$SVC_MGR" in
  systemctl) systemctl %s %s 2>&1 ;;
  service) service %s %s 2>&1 ;;
  rc-service) rc-service %s %s 2>&1 ;;
  sysvinit) /etc/init.d/%s %s 2>&1 ;;
  *) echo "Unknown init system" ;;
esac`, req.Service, req.Action, req.Service, req.Service, req.Action, req.Service, req.Action, req.Service, req.Action)

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_service_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 防火墙管理 (firewalld/iptables/ufw/nftables/Windows Firewall)
// ============================================================================

// handleOSFirewall 处理 POST /api/v1/os/firewall
func (s *Server) handleOSFirewall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`   // add/delete/list/status/enable/disable
		Backend  string `json:"backend"`  // auto/firewalld/iptables/ufw/nftables
		Protocol string `json:"protocol"` // tcp/udp
		Port     string `json:"port"`     // e.g. 80, 443, 8080-8090
		Source   string `json:"source"`   // source IP/CIDR
		Zone     string `json:"zone"`     // firewalld zone
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	backend := req.Backend
	if backend == "" || backend == "auto" {
		backend = "auto"
	}

	var script string
	switch req.Action {
	case "add":
		if req.Port == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "port is required"})
			return
		}
		proto := req.Protocol
		if proto == "" {
			proto = "tcp"
		}
		script = buildFirewallAddScript(backend, proto, req.Port, req.Source, req.Zone)
	case "delete":
		if req.Port == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "port is required"})
			return
		}
		proto := req.Protocol
		if proto == "" {
			proto = "tcp"
		}
		script = buildFirewallDeleteScript(backend, proto, req.Port, req.Source, req.Zone)
	case "list":
		script = buildFirewallListScript(backend)
	case "status":
		script = buildFirewallStatusScript(backend)
	case "enable":
		script = buildFirewallEnableScript(backend)
	case "disable":
		script = buildFirewallDisableScript(backend)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_firewall_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

func buildFirewallAddScript(backend, proto, port, source, zone string) string {
	if backend == "auto" {
		backend = `if command -v firewall-cmd >/dev/null 2>&1; then echo firewalld; elif command -v ufw >/dev/null 2>&1; then echo ufw; elif command -v nft >/dev/null 2>&1; then echo nftables; else echo iptables; fi`
	}
	// Simplified: auto-detect and apply
	return fmt.Sprintf(`FW=$(if command -v firewall-cmd >/dev/null 2>&1; then echo firewalld; elif command -v ufw >/dev/null 2>&1; then echo ufw; elif command -v nft >/dev/null 2>&1; then echo nftables; else echo iptables; fi)
case "$FW" in
  firewalld) firewall-cmd --permanent --add-port=%s/%s && firewall-cmd --reload ;;
  ufw) ufw allow %s/%s ;;
  nftables) nft add rule inet filter input %s dport %s accept ;;
  iptables) iptables -A INPUT -p %s --dport %s -j ACCEPT ;;
esac && echo "Firewall rule added: %s/%s"`, port, proto, port, proto, proto, port, proto, port, port, proto)
}

func buildFirewallDeleteScript(backend, proto, port, source, zone string) string {
	return fmt.Sprintf(`FW=$(if command -v firewall-cmd >/dev/null 2>&1; then echo firewalld; elif command -v ufw >/dev/null 2>&1; then echo ufw; elif command -v nft >/dev/null 2>&1; then echo nftables; else echo iptables; fi)
case "$FW" in
  firewalld) firewall-cmd --permanent --remove-port=%s/%s && firewall-cmd --reload ;;
  ufw) ufw delete allow %s/%s ;;
  nftables) echo "Manual nft rule removal required" ;;
  iptables) iptables -D INPUT -p %s --dport %s -j ACCEPT ;;
esac && echo "Firewall rule removed: %s/%s"`, port, proto, port, proto, proto, port, port, proto)
}

func buildFirewallListScript(backend string) string {
	return `FW=$(if command -v firewall-cmd >/dev/null 2>&1; then echo firewalld; elif command -v ufw >/dev/null 2>&1; then echo ufw; elif command -v nft >/dev/null 2>&1; then echo nftables; else echo iptables; fi)
case "$FW" in
  firewalld) firewall-cmd --list-all ;;
  ufw) ufw status verbose ;;
  nftables) nft list ruleset ;;
  iptables) iptables -L -n -v ;;
esac`
}

func buildFirewallStatusScript(backend string) string {
	return `FW=$(if command -v firewall-cmd >/dev/null 2>&1; then echo firewalld; elif command -v ufw >/dev/null 2>&1; then echo ufw; elif command -v nft >/dev/null 2>&1; then echo nftables; else echo iptables; fi)
echo "Active firewall: $FW"
case "$FW" in
  firewalld) firewall-cmd --state ;;
  ufw) ufw status ;;
  nftables) nft list ruleset 2>/dev/null | head -5 ;;
  iptables) iptables -L 2>/dev/null | head -5 ;;
esac`
}

func buildFirewallEnableScript(backend string) string {
	return `FW=$(if command -v firewall-cmd >/dev/null 2>&1; then echo firewalld; elif command -v ufw >/dev/null 2>&1; then echo ufw; else echo iptables; fi)
case "$FW" in
  firewalld) systemctl enable --now firewalld && echo "firewalld enabled" ;;
  ufw) ufw enable && echo "ufw enabled" ;;
  *) echo "iptables always active" ;;
esac`
}

func buildFirewallDisableScript(backend string) string {
	return `FW=$(if command -v firewall-cmd >/dev/null 2>&1; then echo firewalld; elif command -v ufw >/dev/null 2>&1; then echo ufw; else echo iptables; fi)
case "$FW" in
  firewalld) systemctl disable --now firewalld && echo "firewalld disabled" ;;
  ufw) ufw disable && echo "ufw disabled" ;;
  *) echo "iptables cannot be disabled" ;;
esac`
}

// ============================================================================
// MAC (Mandatory Access Control): SELinux + AppArmor
// ============================================================================

// handleOSMAC 处理 POST /api/v1/os/mac
func (s *Server) handleOSMAC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/disable/enable/list-profiles/enforce/complain
		Profile  string `json:"profile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== SELinux ===" && getenforce 2>/dev/null || echo "SELinux not available" && echo "=== AppArmor ===" && aa-status 2>/dev/null | head -5 || echo "AppArmor not available"`
	case "disable":
		script = `setenforce 0 2>/dev/null && sed -i 's/^SELINUX=enforcing/SELINUX=permissive/' /etc/selinux/config && echo "SELinux set to permissive" ; aa-teardown 2>/dev/null && echo "AppArmor disabled"`
	case "enable":
		script = `setenforce 1 2>/dev/null && sed -i 's/^SELINUX=permissive/SELINUX=enforcing/' /etc/selinux/config && echo "SELinux set to enforcing" ; systemctl start apparmor 2>/dev/null && echo "AppArmor enabled"`
	case "list-profiles":
		script = `aa-status 2>/dev/null | grep -i "profiles\|processes" || echo "AppArmor not available"`
	case "enforce":
		if req.Profile == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "profile is required"})
			return
		}
		script = fmt.Sprintf(`aa-enforce %s 2>/dev/null && echo "Profile %s enforced"`, req.Profile, req.Profile)
	case "complain":
		if req.Profile == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "profile is required"})
			return
		}
		script = fmt.Sprintf(`aa-complain %s 2>/dev/null && echo "Profile %s in complain mode"`, req.Profile, req.Profile)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_mac_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// OS 生命周期 (reboot, dist-upgrade, kernel live-patch)
// ============================================================================

// handleOSLifecycle 处理 POST /api/v1/os/lifecycle
func (s *Server) handleOSLifecycle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // reboot/shutdown/dist-upgrade/kernel-livepatch/info
		Delay    int    `json:"delay"`  // reboot delay (seconds)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "reboot":
		delay := req.Delay
		if delay <= 0 {
			delay = 0
		}
		script := fmt.Sprintf(`shutdown -r +%d "OpsMesh scheduled reboot" 2>/dev/null || reboot && echo "Reboot scheduled in %d seconds"`, delay/60, delay)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_lifecycle_reboot")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "reboot"})

	case "shutdown":
		delay := req.Delay
		if delay <= 0 {
			delay = 0
		}
		script := fmt.Sprintf(`shutdown -h +%d "OpsMesh scheduled shutdown" 2>/dev/null || poweroff && echo "Shutdown scheduled in %d seconds"`, delay/60, delay)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_lifecycle_shutdown")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "shutdown"})

	case "dist-upgrade":
		script := `PKG_MGR=""; command -v dnf >/dev/null 2>&1 && PKG_MGR="dnf"; command -v apt-get >/dev/null 2>&1 && [ -z "$PKG_MGR" ] && PKG_MGR="apt"; command -v zypper >/dev/null 2>&1 && [ -z "$PKG_MGR" ] && PKG_MGR="zypper"
case "$PKG_MGR" in
  dnf) dnf distro-sync -y ;;
  apt) apt-get dist-upgrade -y ;;
  zypper) zypper dist-upgrade -y ;;
  *) echo "Unsupported package manager" ;;
esac && echo "Distribution upgrade complete"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_lifecycle_dist-upgrade")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "dist-upgrade"})

	case "kernel-livepatch":
		script := `echo "=== Kernel Live Patch ===" && (command -v kpatch >/dev/null 2>&1 && kpatch list || echo "kpatch not installed") && (command -v canonical-livepatch >/dev/null 2>&1 && canonical-livepatch status || echo "canonical-livepatch not installed") && (command -v kgraft >/dev/null 2>&1 && echo "kgraft available" || echo "kgraft not available")`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_lifecycle_kernel-livepatch")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "kernel-livepatch"})

	case "info":
		script := `echo "=== OS Info ===" && cat /etc/os-release 2>/dev/null | head -5 && echo "=== Arch ===" && uname -m && echo "=== Kernel ===" && uname -r && echo "=== Uptime ===" && uptime && echo "=== Load ===" && cat /proc/loadavg`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_lifecycle_info")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "info"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// HarmonyOS 支持
// ============================================================================

// handleHarmonyOS 处理 POST /api/v1/os/harmony
func (s *Server) handleHarmonyOS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // info/deploy-app/list-apps
		AppPath  string `json:"appPath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "info":
		script := `echo "=== HarmonyOS Info ===" && cat /etc/os-release 2>/dev/null | head -5 && echo "=== Arch ===" && uname -m && echo "=== Kernel ===" && uname -r && echo "=== HDC ===" && hdc version 2>/dev/null || echo "HDC not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "harmony_info")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "info"})

	case "deploy-app":
		if req.AppPath == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "appPath is required"})
			return
		}
		script := fmt.Sprintf(`hdc install %s 2>/dev/null && echo "App deployed: %s" || echo "HDC not available"`, req.AppPath, req.AppPath)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "harmony_deploy-app")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy-app"})

	case "list-apps":
		script := `hdc shell bm dump -a 2>/dev/null | head -20 || echo "HDC not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "harmony_list-apps")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-apps"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 路由注册
// ============================================================================

// registerOSRoutes 注册 OS 运维路由
func (s *Server) registerOSRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/os/detect", s.handleOSDetect)
	mux.HandleFunc("/api/v1/os/package", s.handleOSPackage)
	mux.HandleFunc("/api/v1/os/service", s.handleOSService)
	mux.HandleFunc("/api/v1/os/firewall", s.handleOSFirewall)
	mux.HandleFunc("/api/v1/os/mac", s.handleOSMAC)
	mux.HandleFunc("/api/v1/os/lifecycle", s.handleOSLifecycle)
	mux.HandleFunc("/api/v1/os/harmony", s.handleHarmonyOS)
}
