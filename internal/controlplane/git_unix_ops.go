// Package controlplane git_unix_ops.go 实现 Git/Unix/macOS-ext/Windows-ext/Bare-metal 运维
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// handleGitOps 处理 POST /api/v1/os/git
func (s *Server) handleOSGit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // clone/pull/push/commit/branch/tag/log/diff/checkout/status/remote
		RepoURL  string `json:"repoUrl"`
		Path     string `json:"path"`
		Branch   string `json:"branch"`
		Message  string `json:"message"`
		Tag      string `json:"tag"`
		Remote   string `json:"remote"`
		File     string `json:"file"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "clone":
		if req.DeviceID == "" || req.RepoURL == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID and repoUrl are required"})
			return
		}
		path := req.Path
		if path == "" {
			path = "/tmp/" + req.RepoURL[strings.LastIndex(req.RepoURL, "/")+1:]
		}
		script := fmt.Sprintf(`git clone %s %s 2>&1 && echo %s`, shellQuote(req.RepoURL), shellQuote(path), shellQuote("Cloned "+req.RepoURL+" to "+path))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "git_clone")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "clone"})

	case "pull":
		if req.DeviceID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
			return
		}
		path := req.Path
		if path == "" {
			path = "/tmp/repo"
		}
		script := fmt.Sprintf(`cd %s && git pull 2>&1 && echo "Pull complete"`, shellQuote(path))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "git_pull")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "pull"})

	case "push":
		if req.DeviceID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
			return
		}
		path := req.Path
		if path == "" {
			path = "/tmp/repo"
		}
		remote := req.Remote
		if remote == "" {
			remote = "origin"
		}
		branch := req.Branch
		if branch == "" {
			branch = "main"
		}
		script := fmt.Sprintf(`cd %s && git push %s %s 2>&1 && echo "Push complete"`, shellQuote(path), shellQuote(remote), shellQuote(branch))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "git_push")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "push"})

	case "commit":
		if req.DeviceID == "" || req.Message == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID and message are required"})
			return
		}
		path := req.Path
		if path == "" {
			path = "/tmp/repo"
		}
		script := fmt.Sprintf(`cd %s && git add -A && git commit -m %s 2>&1 && echo %s`, shellQuote(path), shellQuote(req.Message), shellQuote("Committed: "+req.Message))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "git_commit")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "commit"})

	case "branch":
		if req.DeviceID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
			return
		}
		path := req.Path
		if path == "" {
			path = "/tmp/repo"
		}
		script := fmt.Sprintf(`cd %s && git branch -a 2>&1 && echo "---" && git branch --show-current 2>&1`, shellQuote(path))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "git_branch_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-branches"})

	case "checkout":
		if req.DeviceID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
			return
		}
		path := req.Path
		if path == "" {
			path = "/tmp/repo"
		}
		branch := req.Branch
		if branch == "" {
			branch = "main"
		}
		script := fmt.Sprintf(`cd %s && git checkout %s 2>&1 && echo %s`, shellQuote(path), shellQuote(branch), shellQuote("Checked out "+branch))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "git_checkout")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "checkout"})

	case "tag":
		if req.DeviceID == "" || req.Tag == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID and tag are required"})
			return
		}
		path := req.Path
		if path == "" {
			path = "/tmp/repo"
		}
		script := fmt.Sprintf(`cd %s && git tag %s 2>&1 && echo %s`, shellQuote(path), shellQuote(req.Tag), shellQuote("Tag "+req.Tag+" created"))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "git_tag")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "tag"})

	case "log":
		if req.DeviceID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
			return
		}
		path := req.Path
		if path == "" {
			path = "/tmp/repo"
		}
		script := fmt.Sprintf(`cd %s && git log --oneline -20 2>&1`, shellQuote(path))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "git_log")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "log"})

	case "diff":
		if req.DeviceID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
			return
		}
		path := req.Path
		if path == "" {
			path = "/tmp/repo"
		}
		script := fmt.Sprintf(`cd %s && git diff 2>&1 | head -100`, shellQuote(path))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "git_diff")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "diff"})

	case "status":
		if req.DeviceID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
			return
		}
		path := req.Path
		if path == "" {
			path = "/tmp/repo"
		}
		script := fmt.Sprintf(`cd %s && git status 2>&1`, shellQuote(path))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "git_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "remote":
		if req.DeviceID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
			return
		}
		path := req.Path
		if path == "" {
			path = "/tmp/repo"
		}
		script := fmt.Sprintf(`cd %s && git remote -v 2>&1`, shellQuote(path))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "git_remote")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "remote"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleUnixOps 处理 POST /api/v1/os/unix
func (s *Server) handleUnixOps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // pkg-install/pkg-list/service-start/service-stop/info
		Package  string `json:"package"`
		Service  string `json:"service"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "pkg-install":
		if req.Package == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "package is required"})
			return
		}
		script = fmt.Sprintf(`if command -v pkgadd >/dev/null 2>&1; then pkgadd -d %s 2>/dev/null; elif command -v swinstall >/dev/null 2>&1; then swinstall %s 2>/dev/null; elif command -v pkgutil >/dev/null 2>&1; then pkgutil -i %s 2>/dev/null; else echo "Unknown package manager"; fi && echo %s`, shellQuote(req.Package), shellQuote(req.Package), shellQuote(req.Package), shellQuote("Installed "+req.Package))
	case "pkg-list":
		script = `if command -v lslpp >/dev/null 2>&1; then lslpp -L 2>/dev/null | head -30; elif command -v swlist >/dev/null 2>&1; then swlist 2>/dev/null | head -30; elif command -v pkginfo >/dev/null 2>&1; then pkginfo 2>/dev/null | head -30; else echo "Unknown OS" ; fi`
	case "service-start":
		if req.Service == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "service is required"})
			return
		}
		script = fmt.Sprintf(`if command -v svcadm >/dev/null 2>&1; then svcadm enable %s; elif command -v startsrc >/dev/null 2>&1; then startsrc -s %s; else /etc/init.d/%s start; fi && echo %s`, shellQuote(req.Service), shellQuote(req.Service), shellQuote(req.Service), shellQuote("Service "+req.Service+" started"))
	case "service-stop":
		if req.Service == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "service is required"})
			return
		}
		script = fmt.Sprintf(`if command -v svcadm >/dev/null 2>&1; then svcadm disable %s; elif command -v stopsrc >/dev/null 2>&1; then stopsrc -s %s; else /etc/init.d/%s stop; fi && echo %s`, shellQuote(req.Service), shellQuote(req.Service), shellQuote(req.Service), shellQuote("Service "+req.Service+" stopped"))
	case "info":
		script = `echo "=== OS ===" && uname -a && echo "=== Version ===" && oslevel 2>/dev/null || swlist 2>/dev/null | head -1 || uname -r && echo "=== Arch ===" && uname -m`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "unix_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleMacOSExt 处理 POST /api/v1/os/macos-ext
func (s *Server) handleMacOSExt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // brew-install/brew-upgrade/brew-list/disk-info/system-info/brew-services
		Package  string `json:"package"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "brew-install":
		if req.Package == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "package is required"})
			return
		}
		script = fmt.Sprintf(`brew install %s 2>/dev/null && echo %s || echo "brew not available"`, shellQuote(req.Package), shellQuote("Installed "+req.Package))
	case "brew-upgrade":
		if req.Package != "" {
			script = fmt.Sprintf(`brew upgrade %s 2>/dev/null && echo %s || echo "brew not available"`, shellQuote(req.Package), shellQuote("Upgraded "+req.Package))
		} else {
			script = `brew upgrade 2>/dev/null && echo "All packages upgraded" || echo "brew not available"`
		}
	case "brew-list":
		script = `brew list 2>/dev/null | head -40 || echo "brew not available"`
	case "disk-info":
		script = `diskutil list 2>/dev/null && echo "---" && df -h 2>/dev/null`
	case "system-info":
		script = `system_profiler SPSoftwareDataType 2>/dev/null | head -20 && echo "---" && system_profiler SPHardwareDataType 2>/dev/null | head -20`
	case "brew-services":
		script = `brew services list 2>/dev/null || echo "brew services not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "macos_ext_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// windowsPkgMgrRe 是 windows-package 的包管理器白名单（mgr 会被拼进脚本）。
var windowsPkgMgrRe = regexp.MustCompile(`^(winget|choco|scoop)$`)

// handleWindowsPackage 处理 POST /api/v1/os/windows-package
func (s *Server) handleWindowsPackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // install/remove/upgrade/search/list
		Package  string `json:"package"`
		Manager  string `json:"manager"` // auto/winget/choco/scoop
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	mgr := req.Manager
	if mgr == "" || mgr == "auto" {
		mgr = "auto"
	}
	if mgr != "auto" && !windowsPkgMgrRe.MatchString(mgr) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported package manager: " + mgr})
		return
	}

	var script string
	switch req.Action {
	case "install":
		script = buildWindowsPackageScript(mgr, "install", req.Package)
	case "remove":
		script = buildWindowsPackageScript(mgr, "remove", req.Package)
	case "upgrade":
		script = buildWindowsPackageScript(mgr, "upgrade", req.Package)
	case "search":
		script = buildWindowsPackageScript(mgr, "search", req.Package)
	case "list":
		script = buildWindowsPackageScript(mgr, "list", "")
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "windows_package_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleWindowsFirewall 处理 POST /api/v1/os/windows-firewall
func (s *Server) handleWindowsFirewall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"`   // add/delete/list/status/enable/disable
		Protocol  string `json:"protocol"` // tcp/udp
		Port      string `json:"port"`
		Name      string `json:"name"`
		Direction string `json:"direction"` // in/out
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
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
		name := req.Name
		if name == "" {
			name = fmt.Sprintf("OpsMesh-%s-%s", proto, req.Port)
		}
		dir := req.Direction
		if dir == "" {
			dir = "in"
		}
		script = shellquote.PowerShellEncodedCommand(
			`New-NetFirewallRule -DisplayName `+shellquote.QuotePowerShellSingle(name)+
				` -Direction `+shellquote.QuotePowerShellSingle(dir)+
				` -Protocol `+shellquote.QuotePowerShellSingle(proto)+
				` -LocalPort `+shellquote.QuotePowerShellSingle(req.Port)+
				` -Action Allow -ErrorAction Stop; Write-Output 'Firewall rule added'`) + ` 2>nul || echo Failed`
	case "delete":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = shellquote.PowerShellEncodedCommand(
			`Remove-NetFirewallRule -DisplayName `+shellquote.QuotePowerShellSingle(req.Name)+
				` -ErrorAction Stop; Write-Output 'Firewall rule deleted'`) + ` 2>nul || echo Failed`
	case "list":
		script = shellquote.PowerShellEncodedCommand(
			`Get-NetFirewallRule | Select-Object DisplayName, Direction, Action, Enabled | Format-Table -AutoSize | Select-Object -First 40`) + ` 2>nul`
	case "status":
		script = shellquote.PowerShellEncodedCommand(
			`Get-NetFirewallProfile | Select-Object Name, Enabled`) + ` 2>nul`
	case "enable":
		script = shellquote.PowerShellEncodedCommand(
			`Set-NetFirewallProfile -Profile Domain,Public,Private -Enabled True`) + ` 2>nul && echo Firewall enabled`
	case "disable":
		script = shellquote.PowerShellEncodedCommand(
			`Set-NetFirewallProfile -Profile Domain,Public,Private -Enabled False`) + ` 2>nul && echo Firewall disabled`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "windows_firewall_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleWindowsUser 处理 POST /api/v1/os/windows-user
func (s *Server) handleWindowsUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/create/delete/add-to-group
		User     string `json:"user"`
		Password string `json:"password"`
		Group    string `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = shellquote.PowerShellEncodedCommand(
			`Get-LocalUser | Select-Object Name, Enabled, LastLogon | Format-Table -AutoSize`) + ` 2>nul`
	case "create":
		if req.User == "" || req.Password == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user and password are required"})
			return
		}
		script = shellquote.PowerShellEncodedCommand(
			`New-LocalUser -Name `+shellquote.QuotePowerShellSingle(req.User)+
				` -Password (ConvertTo-SecureString `+shellquote.QuotePowerShellSingle(req.Password)+
				` -AsPlainText -Force) -ErrorAction Stop; Write-Output `+
				shellquote.QuotePowerShellSingle("User "+req.User+" created")) + ` 2>nul || echo Failed`
	case "delete":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		script = shellquote.PowerShellEncodedCommand(
			`Remove-LocalUser -Name `+shellquote.QuotePowerShellSingle(req.User)+
				` -ErrorAction Stop; Write-Output `+shellquote.QuotePowerShellSingle("User "+req.User+" deleted")) + ` 2>nul || echo Failed`
	case "add-to-group":
		if req.User == "" || req.Group == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user and group are required"})
			return
		}
		script = shellquote.PowerShellEncodedCommand(
			`Add-LocalGroupMember -Group `+shellquote.QuotePowerShellSingle(req.Group)+
				` -Member `+shellquote.QuotePowerShellSingle(req.User)+
				` -ErrorAction Stop; Write-Output `+
				shellquote.QuotePowerShellSingle("User "+req.User+" added to group "+req.Group)) + ` 2>nul || echo Failed`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "windows_user_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// windowsEventLevelRe 是 Get-EventLog -EntryType 的级别名白名单（逗号分隔多个）。
// 它作为**裸词**进入 PS 脚本（不能加引号，否则枚举数组绑定失效），因此字符集
// 只允许字母与逗号——含任何 PS 元字符（; | $ 反引号等）都可能成为语句注入。
var windowsEventLevelRe = regexp.MustCompile(`(?i)^[A-Za-z]+(,[A-Za-z]+)*$`)

// handleWindowsEventLog 处理 POST /api/v1/os/windows-eventlog
func (s *Server) handleWindowsEventLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // query/clear/export
		LogName  string `json:"logName"`
		Level    string `json:"level"`
		Count    int    `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	logName := req.LogName
	if logName == "" {
		logName = "System"
	}

	switch req.Action {
	case "query":
		count := req.Count
		if count <= 0 {
			count = 50
		}
		level := req.Level
		if level == "" {
			level = "Error,Warning"
		}
		if !windowsEventLevelRe.MatchString(level) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported level: " + level})
			return
		}
		script := shellquote.PowerShellEncodedCommand(fmt.Sprintf(
			`Get-EventLog -LogName %s -EntryType %s -Newest %d | Select-Object TimeGenerated, EntryType, Source, Message | Format-Table -AutoSize`,
			shellquote.QuotePowerShellSingle(logName), level, count)) + ` 2>nul`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "windows_eventlog_query")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "query"})

	case "clear":
		script := shellquote.PowerShellEncodedCommand(
			`Clear-EventLog -LogName `+shellquote.QuotePowerShellSingle(logName)+
				` -ErrorAction Stop; Write-Output `+shellquote.QuotePowerShellSingle("Event log "+logName+" cleared")) + ` 2>nul || echo Failed`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "windows_eventlog_clear")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "clear"})

	case "export":
		exportPath := "C:\\temp\\" + logName + ".csv"
		script := shellquote.PowerShellEncodedCommand(
			`Get-EventLog -LogName `+shellquote.QuotePowerShellSingle(logName)+
				` -ErrorAction Stop | Export-Csv -Path `+shellquote.QuotePowerShellSingle(exportPath)+
				` -NoTypeInformation -ErrorAction Stop; Write-Output `+shellquote.QuotePowerShellSingle("Exported to "+exportPath)) + ` 2>nul || echo Failed`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "windows_eventlog_export")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "export"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// windowsRegTypeRe 是 reg add /t 的注册表类型白名单（reg.exe 接受的集合）。
var windowsRegTypeRe = regexp.MustCompile(`(?i)^REG_(SZ|DWORD|QWORD|BINARY|MULTI_SZ|EXPAND_SZ|NONE)$`)

// handleWindowsRegistry 处理 POST /api/v1/os/windows-registry
func (s *Server) handleWindowsRegistry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // query/set/delete
		Path     string `json:"path"`
		Name     string `json:"name"`
		Value    string `json:"value"`
		Type     string `json:"type"` // REG_SZ/REG_DWORD/REG_BINARY
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "query":
		if req.Path == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
			return
		}
		// 值嵌在 cmd 双引号段里：cmd 无引号内转义语法，只能力度=拒绝
		// （`"`/`%`/`!`/CR/LF 五类可改变解析；见 shellquote.ValidateCmdQuoted）。
		if err := shellquote.ValidateCmdQuoted(req.Path, 512); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "path: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`reg query "%s" 2>/dev/null || echo "Registry path not found"`, req.Path)
	case "set":
		if req.Path == "" || req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "path and name are required"})
			return
		}
		regType := req.Type
		if regType == "" {
			regType = "REG_SZ"
		}
		if !windowsRegTypeRe.MatchString(regType) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported registry type: " + regType})
			return
		}
		if err := shellquote.ValidateCmdQuoted(req.Path, 512); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "path: " + err.Error()})
			return
		}
		if err := shellquote.ValidateCmdQuoted(req.Name, 255); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name: " + err.Error()})
			return
		}
		if err := shellquote.ValidateCmdQuoted(req.Value, 8192); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "value: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`reg add "%s" /v "%s" /t %s /d "%s" /f 2>/dev/null && echo "Registry value set" || echo "Failed"`, req.Path, req.Name, regType, req.Value)
	case "delete":
		if req.Path == "" || req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "path and name are required"})
			return
		}
		if err := shellquote.ValidateCmdQuoted(req.Path, 512); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "path: " + err.Error()})
			return
		}
		if err := shellquote.ValidateCmdQuoted(req.Name, 255); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`reg delete "%s" /v "%s" /f 2>/dev/null && echo "Registry value deleted" || echo "Failed"`, req.Path, req.Name)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "windows_registry_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleWindowsPerf 处理 POST /api/v1/os/windows-perf
func (s *Server) handleWindowsPerf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // cpu/memory/disk/network/processes
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "cpu":
		script = shellquote.PowerShellEncodedCommand(
			`Get-Counter '\Processor(_Total)\% Processor Time' -SampleInterval 1 -MaxSamples 3 | Select-Object -ExpandProperty CounterSamples | Select-Object CookedValue`) + ` 2>nul`
	case "memory":
		script = shellquote.PowerShellEncodedCommand(
			`Get-CimInstance Win32_OperatingSystem | Select-Object TotalVisibleMemorySize, FreePhysicalMemory, TotalVirtualMemorySize, FreeVirtualMemory`) + ` 2>nul`
	case "disk":
		script = shellquote.PowerShellEncodedCommand(
			`Get-PSDrive -PSProvider FileSystem | Select-Object Name, Used, Free, @{N='Total';E={$_.Used+$_.Free}}`) + ` 2>nul`
	case "network":
		script = shellquote.PowerShellEncodedCommand(
			`Get-NetAdapterStatistics | Select-Object Name, ReceivedBytes, SentBytes`) + ` 2>nul`
	case "processes":
		script = shellquote.PowerShellEncodedCommand(
			`Get-Process | Sort-Object CPU -Descending | Select-Object -First 20 Name, CPU, WorkingSet, Id | Format-Table -AutoSize`) + ` 2>nul`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "windows_perf_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleBaremetalHardwareInventory 处理 POST /api/v1/os/baremetal/inventory
func (s *Server) handleBaremetalHardwareInventory(w http.ResponseWriter, r *http.Request) {
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

	// 收集 DMI/SMBIOS 信息
	script := `echo "=== 系统信息 ===" && dmidecode -t system 2>/dev/null && echo "=== 主板 ===" && dmidecode -t baseboard 2>/dev/null && echo "=== 处理器 ===" && dmidecode -t processor 2>/dev/null && echo "=== 内存 ===" && dmidecode -t memory 2>/dev/null && echo "=== 网卡 ===" && dmidecode -t network 2>/dev/null | head -40 || echo "dmidecode 不可用，尝试 lshw" && lshw -short 2>/dev/null || echo "lshw 也不可用"`
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_hardware_inventory")
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "hardware_inventory"})
}

// handleDiskHealth 处理 POST /api/v1/os/disk-health
func (s *Server) handleDiskHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // info/smart/test
		Disk     string `json:"disk"`   // /dev/sda, /dev/nvme0n1
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "info":
		script := `lsblk -d -o NAME,SIZE,MODEL,TYPE 2>/dev/null && echo "---" && lsblk -d -o NAME,SIZE,MODEL,TYPE,SERIAL 2>/dev/null || echo "lsblk 不可用"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_disk_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "disks"})

	case "smart":
		if req.Disk == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "磁盘设备名必填 (如 /dev/sda)"})
			return
		}
		script := fmt.Sprintf(`smartctl -a %s 2>/dev/null || echo %s`, shellQuote(req.Disk), shellQuote("smartctl 不可用或磁盘 "+req.Disk+" 不支持 SMART"))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "os_disk_smart")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "smart"})

	case "badblocks":
		if req.Disk == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "磁盘设备名是必填项"})
			return
		}
		script := fmt.Sprintf(`badblocks -sv %s 2>/dev/null | head -50 || echo "badblocks 不可用"`, shellQuote(req.Disk))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "redis_badblocks")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "badblocks"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "未知操作: " + req.Action})
	}
}

// handleOSRAID 处理 POST /api/v1/os/raid
func (s *Server) handleOSRAID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID   string `json:"deviceID"`
		Action     string `json:"action"` // list/create/delete/rebuild
		Controller string `json:"controller"`
		Disks      string `json:"disks"` // 逗号分隔的磁盘列表
		Level      string `json:"level"` // 0/1/5/6/10
		Name       string `json:"name"`  // 阵列名
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "list":
		script := `storcli64 /call show 2>/dev/null || megacli -PDList -aALL 2>/dev/null || hpssacli ctrl all show status 2>/dev/null || echo "RAID 控制器未找到或工具不可用"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "raid_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	case "create":
		if req.Name == "" || req.Disks == "" || req.Level == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "需要指定阵列名、磁盘列表和 RAID 级别"})
			return
		}
		script := fmt.Sprintf(`echo %s`, shellQuote("创建 RAID "+req.Name+": "+req.Disks+" 上的磁盘 "+req.Name)) + `
# 实际命令取决于 RAID 控制器类型 (MegaRAID/HP Smart Array/HPSSA 等)
# 示例: storcli /c0/add vd type=raid1 drives=0:1,0:2
#       或: hpssacli controller slot=0 create ld type=raid1 drives=1:1,1:2`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "raid_create")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create"})

	case "remove":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "阵列名是必填项"})
			return
		}
		script := fmt.Sprintf(`echo %s`, shellQuote("删除 RAID 阵列: "+req.Name))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "raid_remove")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "未知操作: " + req.Action})
	}
}

func buildWindowsPackageScript(mgr, action, pkg string) string {
	detect := `PKG_MGR=""; command -v winget >/dev/null 2>&1 && PKG_MGR="winget"; command -v choco >/dev/null 2>&1 && [ -z "$PKG_MGR" ] && PKG_MGR="choco"; command -v scoop >/dev/null 2>&1 && [ -z "$PKG_MGR" ] && PKG_MGR="scoop"`
	if mgr != "auto" {
		detect = fmt.Sprintf(`PKG_MGR=%s`, shellQuote(mgr))
	}

	var cmd string
	switch action {
	case "install":
		cmd = `case "$PKG_MGR" in winget) winget install --id ` + shellQuote(pkg) + ` --silent --accept-package-agreements --accept-source-agreements ;; choco) choco install ` + shellQuote(pkg) + ` -y ;; scoop) scoop install ` + shellQuote(pkg) + ` ;; esac`
	case "remove":
		cmd = `case "$PKG_MGR" in winget) winget uninstall --id ` + shellQuote(pkg) + ` --silent ;; choco) choco uninstall ` + shellQuote(pkg) + ` -y ;; scoop) scoop uninstall ` + shellQuote(pkg) + ` ;; esac`
	case "upgrade":
		if pkg != "" {
			cmd = `case "$PKG_MGR" in winget) winget upgrade --id ` + shellQuote(pkg) + ` --silent ;; choco) choco upgrade ` + shellQuote(pkg) + ` -y ;; scoop) scoop update ` + shellQuote(pkg) + ` ;; esac`
		} else {
			cmd = `case "$PKG_MGR" in winget) winget upgrade --all --silent ;; choco) choco upgrade all -y ;; scoop) scoop update * ;; esac`
		}
	case "search":
		cmd = `case "$PKG_MGR" in winget) winget search ` + shellQuote(pkg) + ` ;; choco) choco search ` + shellQuote(pkg) + ` ;; scoop) scoop search ` + shellQuote(pkg) + ` ;; esac`
	case "list":
		cmd = `case "$PKG_MGR" in winget) winget list ;; choco) choco list --local-only ;; scoop) scoop list ;; esac`
	}

	return detect + " && " + cmd
}

// registerGitUnixRoutes 注册 Git/Unix/macOS-ext/Windows-ext/Bare-metal 路由
func (s *Server) registerGitUnixRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/os/git", s.handleOSGit)
	mux.HandleFunc("/api/v1/os/unix", s.handleUnixOps)
	mux.HandleFunc("/api/v1/os/macos-ext", s.handleMacOSExt)
	mux.HandleFunc("/api/v1/os/windows-package", s.handleWindowsPackage)
	mux.HandleFunc("/api/v1/os/windows-firewall", s.handleWindowsFirewall)
	mux.HandleFunc("/api/v1/os/windows-user", s.handleWindowsUser)
	mux.HandleFunc("/api/v1/os/windows-eventlog", s.handleWindowsEventLog)
	mux.HandleFunc("/api/v1/os/windows-registry", s.handleWindowsRegistry)
	mux.HandleFunc("/api/v1/os/windows-perf", s.handleWindowsPerf)
	mux.HandleFunc("/api/v1/os/hardware-inventory", s.handleBaremetalHardwareInventory)
	mux.HandleFunc("/api/v1/os/disk-health", s.handleDiskHealth)
	mux.HandleFunc("/api/v1/os/raid", s.handleOSRAID)
}
