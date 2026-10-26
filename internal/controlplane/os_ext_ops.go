// Package controlplane os_ext_ops.go 实现 OS 扩展运维（进程/日志/服务/Agent 分发/HarmonyOS 扩展）
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleOSProcess 处理 POST /api/v1/os/process
func (s *Server) handleOSProcess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		PID      int    `json:"pid"`
		Signal   string `json:"signal"`
		Nice     int    `json:"nice"`
		Filter   string `json:"filter"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	var script string
	switch req.Action {
	case "list":
		if req.Filter != "" {
			script = fmt.Sprintf(`ps aux | grep -i %s | grep -v grep | head -50`, req.Filter)
		} else {
			script = `ps aux | head -50`
		}
	case "top":
		script = `top -b -n 1 | head -30`
	case "kill":
		sig := req.Signal
		if sig == "" {
			sig = "TERM"
		}
		script = fmt.Sprintf(`kill -%s %d 2>&1 && echo "Killed %d"`, sig, req.PID, req.PID)
	case "nice":
		script = fmt.Sprintf(`renice %d -p %d 2>&1 && echo "Reniced %d"`, req.Nice, req.PID, req.PID)
	case "cgroups":
		script = `cat /proc/cgroups 2>/dev/null; echo "---"; ls /sys/fs/cgroup 2>/dev/null`
	case "info":
		script = fmt.Sprintf(`ps -p %d -o pid,ppid,user,%%cpu,%%mem,vsz,rss,stat,start,time,cmd 2>/dev/null || echo "PID %d not found"`, req.PID, req.PID)
	default:
		script = `ps aux | head -30`
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_process_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleOSLog 处理 POST /api/v1/os/log
func (s *Server) handleOSLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		Unit     string `json:"unit"`
		Since    string `json:"since"`
		Lines    int    `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.Lines <= 0 {
		req.Lines = 50
	}
	var script string
	switch req.Action {
	case "journal-query":
		unit := req.Unit
		if unit == "" {
			unit = "--system"
		} else {
			unit = "-u " + unit
		}
		since := req.Since
		if since == "" {
			since = "1h ago"
		}
		script = fmt.Sprintf(`journalctl %s --since "%s" -n %d --no-pager 2>/dev/null || echo "journalctl unavailable"`, unit, since, req.Lines)
	case "journal-vacuum":
		script = `journalctl --vacuum-time=7d 2>/dev/null && echo "Vacuum complete"`
	case "logrotate":
		script = `logrotate -f /etc/logrotate.conf 2>&1 && echo "Logrotate complete"`
	case "rsyslog-config":
		script = `cat /etc/rsyslog.conf 2>/dev/null | head -40`
	case "status":
		script = `systemctl status rsyslog 2>/dev/null || service rsyslog status 2>/dev/null || echo "rsyslog status unknown"`
	default:
		script = `journalctl -n 50 --no-pager 2>/dev/null || echo "journalctl unavailable"`
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_log_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleWindowsService 处理 POST /api/v1/os/windows-service
func (s *Server) handleWindowsService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		Service  string `json:"service"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	var script string
	switch req.Action {
	case "list":
		script = `powershell -Command "Get-Service | Select-Object Name, Status, StartType | Format-Table -AutoSize" 2>/dev/null`
	case "start":
		script = fmt.Sprintf(`powershell -Command "Start-Service -Name '%s'" 2>/dev/null && echo "Service %s started"`, req.Service, req.Service)
	case "stop":
		script = fmt.Sprintf(`powershell -Command "Stop-Service -Name '%s' -Force" 2>/dev/null && echo "Service %s stopped"`, req.Service, req.Service)
	case "restart":
		script = fmt.Sprintf(`powershell -Command "Restart-Service -Name '%s' -Force" 2>/dev/null && echo "Service %s restarted"`, req.Service, req.Service)
	case "create":
		script = fmt.Sprintf(`powershell -Command "New-Service -Name '%s' -BinaryPathName '%s'" 2>/dev/null && echo "Service %s created"`, req.Service, req.Service, req.Service)
	case "delete":
		script = fmt.Sprintf(`powershell -Command "sc.exe delete %s" 2>/dev/null && echo "Service %s deleted"`, req.Service, req.Service)
	case "query":
		script = fmt.Sprintf(`powershell -Command "Get-Service -Name '%s' | Select-Object Name, Status, StartType, DisplayName" 2>/dev/null`, req.Service)
	default:
		script = `powershell -Command "Get-Service | Select-Object Name, Status | Format-Table -AutoSize" 2>/dev/null`
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "windows_service_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleMacOSService 处理 POST /api/v1/os/macos-service
func (s *Server) handleMacOSService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		Service  string `json:"service"`
		Plist    string `json:"plist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	var script string
	switch req.Action {
	case "list":
		script = `launchctl list 2>/dev/null | head -40`
	case "load":
		script = fmt.Sprintf(`launchctl load %s 2>&1 && echo "Loaded %s"`, req.Plist, req.Plist)
	case "unload":
		script = fmt.Sprintf(`launchctl unload %s 2>&1 && echo "Unloaded %s"`, req.Plist, req.Plist)
	case "start":
		script = fmt.Sprintf(`launchctl start %s 2>&1 && echo "Started %s"`, req.Service, req.Service)
	case "stop":
		script = fmt.Sprintf(`launchctl stop %s 2>&1 && echo "Stopped %s"`, req.Service, req.Service)
	case "status":
		script = fmt.Sprintf(`launchctl list %s 2>/dev/null || echo "Service %s not found"`, req.Service, req.Service)
	default:
		script = `launchctl list 2>/dev/null | head -40`
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "macos_service_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleAgentDistribute 处理 POST /api/v1/os/agent-distribute
func (s *Server) handleAgentDistribute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		Arch     string `json:"arch"`
		OS       string `json:"os"`
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.Arch == "" {
		req.Arch = "amd64"
	}
	if req.OS == "" {
		req.OS = "linux"
	}
	if req.Version == "" {
		req.Version = "latest"
	}
	var script string
	switch req.Action {
	case "detect":
		script = `uname -s && uname -m && cat /etc/os-release 2>/dev/null | head -5`
	case "download":
		script = fmt.Sprintf(`echo "Downloading agent %s for %s/%s"`, req.Version, req.OS, req.Arch)
	case "install":
		script = fmt.Sprintf(`echo "Installing agent %s for %s/%s"`, req.Version, req.OS, req.Arch)
	default:
		script = `uname -s && uname -m`
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "agent_distribute_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleHarmonyOSExt 处理 POST /api/v1/os/harmony-ext
func (s *Server) handleHarmonyOSExt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		DeviceID   string `json:"deviceID"`
		Action     string `json:"action"`
		AppPath    string `json:"appPath"`
		AppName    string `json:"appName"`
		LocalPath  string `json:"localPath"`
		RemotePath string `json:"remotePath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	var script string
	switch req.Action {
	case "info":
		script = `echo "HarmonyOS device info"; hdc shell "cat /proc/version" 2>/dev/null || echo "hdc unavailable"`
	case "install-app":
		script = fmt.Sprintf(`hdc install %s 2>&1 && echo "Installed %s"`, req.AppPath, req.AppPath)
	case "uninstall-app":
		script = fmt.Sprintf(`hdc uninstall %s 2>&1 && echo "Uninstalled %s"`, req.AppName, req.AppName)
	case "list-apps":
		script = `hdc shell "bm dump -a" 2>/dev/null | head -40 || echo "hdc unavailable"`
	case "transfer-file":
		script = fmt.Sprintf(`hdc file send %s %s 2>&1 && echo "Transferred %s -> %s"`, req.LocalPath, req.RemotePath, req.LocalPath, req.RemotePath)
	default:
		script = `echo "HarmonyOS ext: unknown action"`
	}
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "harmony_ext_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerOSExtRoutes 注册 OS 扩展路由
func (s *Server) registerOSExtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/os/process", s.handleOSProcess)
	mux.HandleFunc("/api/v1/os/log", s.handleOSLog)
	mux.HandleFunc("/api/v1/os/windows-service", s.handleWindowsService)
	mux.HandleFunc("/api/v1/os/macos-service", s.handleMacOSService)
	mux.HandleFunc("/api/v1/os/agent-distribute", s.handleAgentDistribute)
	mux.HandleFunc("/api/v1/os/harmony-ext", s.handleHarmonyOSExt)
}
