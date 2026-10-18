// security_ops.go 安全运维（CVE 漏洞扫描/基线检查与修复/组件更新/回滚）。
//
// 设计原则（延续 P0-P5"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/security/{deviceID} 读取安全态势（OS/内核版本、可更新包）。
//   - 执行类：POST /api/v1/security/... 下发 shell 模板到 agent 执行。
//
// 能力范围：
//   - 漏洞扫描：OS 包级安全更新检查（yum updateinfo --security / apt 可升级包），
//     支持按 CVE ID 检索受影响包（yum updateinfo list cve.<CVE>）。
//   - 基线检查：复用 compliance 引擎的 CIS 规则（CheckScript 下发执行）。
//   - 基线修复：内置"规则 ID → 修复脚本"映射（预置命令，不接受任意 shell），
//     支持按规则批量下发修复（含 sshd/sysctl 持久化）。
//   - 组件更新：按包名升级（log4j-core/httpd/openssl 等 CVE 高发组件），
//     支持查找当前版本与可用版本。
//   - 更新回滚：yum history undo（按事务 ID）/ apt 降级提示。
//
// API 端点：
//   - GET  /api/v1/security/{deviceID}     — 安全态势
//   - POST /api/v1/security/scan           — 漏洞扫描 {deviceID, cve}
//   - POST /api/v1/security/baseline       — 基线检查 {deviceID} 或修复 {deviceID, fix: true, ruleIDs}
//   - POST /api/v1/security/patch          — 组件更新 {deviceID, packages, action: check/update, securityOnly}
//   - POST /api/v1/security/rollback       — 更新回滚 {deviceID, transactionID}
//
// 安全：action 走白名单（不含任意 shell）；包名/CVE/规则 ID 走正则校验；
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

// CVE ID 正则（如 CVE-2021-44228）。
var cveIDRe = regexp.MustCompile(`^CVE-[0-9]{4}-[0-9]{4,7}$`)

// 包名正则（含 -_.+ 与版本分隔）。
var securityPackageRe = regexp.MustCompile(`^[a-zA-Z0-9_.+:~\-]{1,128}$`)

// 基线规则 ID 正则。
var baselineRuleRe = regexp.MustCompile(`^[a-z0-9\-]{1,64}$`)

// 事务 ID 正则（yum history 数字 ID）。
var yumTxnRe = regexp.MustCompile(`^[0-9]{1,10}$`)

// securityScanReq 漏洞扫描请求体。
type securityScanReq struct {
	DeviceID string `json:"deviceID"`
	CVE      string `json:"cve"` // 可选：按 CVE 检索受影响包
}

// securityBaselineReq 基线检查/修复请求体。
type securityBaselineReq struct {
	DeviceID string   `json:"deviceID"`
	Fix      bool     `json:"fix"`     // false=仅检查，true=执行修复
	RuleIDs  []string `json:"ruleIDs"` // 修复时指定规则（空=全部可修复规则）
}

// securityPatchReq 组件更新请求体。
type securityPatchReq struct {
	DeviceID     string   `json:"deviceID"`
	Action       string   `json:"action"`       // check/update
	Packages     []string `json:"packages"`     // 指定包（空+securityOnly=全部安全更新）
	SecurityOnly bool     `json:"securityOnly"` // 仅安全更新
}

// securityRollbackReq 回滚请求体。
type securityRollbackReq struct {
	DeviceID      string `json:"deviceID"`
	TransactionID string `json:"transactionID"` // yum history 事务 ID
}

// handleSecurity 查询设备安全态势。
func (s *Server) handleSecurity(w http.ResponseWriter, r *http.Request, deviceID string) {
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
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"deviceID": deviceID,
		"security": map[string]string{
			"os":          metrics.OS,
			"osVersion":   metrics.OSVersion,
			"kernel":      metrics.Kernel,
			"hostname":    metrics.Hostname,
			"collectedAt": metrics.CollectedAt.Format("2006-01-02T15:04:05Z07:00"),
		},
	})
}

// handleSecurityScan 处理 POST /api/v1/security/scan：漏洞扫描。
func (s *Server) handleSecurityScan(w http.ResponseWriter, r *http.Request) {
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
	var req securityScanReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.CVE != "" && !cveIDRe.MatchString(req.CVE) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid CVE id (format: CVE-YYYY-NNNN)"})
		return
	}

	script := buildSecurityScanScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "security_scan")
	if err != nil {
		writeInternalError(r.Context(), w, "security.scan", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "security_scan", Target: req.CVE,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildSecurityScanScript(req securityScanReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	if req.CVE != "" {
		fmt.Fprintf(&b, "# 按 CVE 检索受影响包: %s\n", req.CVE)
		b.WriteString("if command -v yum >/dev/null 2>&1; then\n")
		fmt.Fprintf(&b, "  yum updateinfo list cve.%s 2>/dev/null || yum updateinfo list security\n", strings.ToLower(req.CVE))
		b.WriteString("elif command -v dnf >/dev/null 2>&1; then\n")
		fmt.Fprintf(&b, "  dnf updateinfo list cve.%s 2>/dev/null || dnf updateinfo list security\n", strings.ToLower(req.CVE))
		b.WriteString("elif command -v apt-get >/dev/null 2>&1; then\n")
		b.WriteString("  echo 'Debian/Ubuntu 按 CVE 检索需 ubuntu-security-status 或 debsecan 工具'\n")
		b.WriteString("  apt list --upgradable 2>/dev/null | head -50\n")
		b.WriteString("fi\n")
	} else {
		b.WriteString("# 全量安全更新扫描\n")
		b.WriteString("if command -v yum >/dev/null 2>&1; then\n")
		b.WriteString("  echo '=== 安全更新（CVE）==='\n")
		b.WriteString("  yum updateinfo list security 2>/dev/null | head -100\n")
		b.WriteString("  echo '=== 全部可更新包 ==='\n")
		b.WriteString("  yum check-update 2>/dev/null | tail -n +2 | head -100 || true\n")
		b.WriteString("elif command -v dnf >/dev/null 2>&1; then\n")
		b.WriteString("  dnf updateinfo list security 2>/dev/null | head -100\n")
		b.WriteString("elif command -v apt-get >/dev/null 2>&1; then\n")
		b.WriteString("  apt-get update -qq 2>/dev/null || true\n")
		b.WriteString("  apt list --upgradable 2>/dev/null | head -100\n")
		b.WriteString("fi\n")
	}
	b.WriteString("echo '漏洞扫描完成'\n")
	return b.String()
}

// baselineFixScripts 基线修复映射表：规则 ID → 预置修复脚本（固定命令，不接受任意输入）。
//
// 与 internal/compliance/engine.go 的规则 ID 一一对应；修复策略：
//   - sshd 类：sed 替换 + reload（保留连接不中断）
//   - sysctl 类：临时生效 + /etc/sysctl.d 持久化
//   - 服务类：systemctl enable --now
//   - 文件权限类：chmod 至基线值
var baselineFixScripts = map[string]string{ //nolint:gosec // 修复脚本模板含 sshd 配置项名（PasswordAuthentication 等），非硬编码凭据。
	"cis-ssh-01": `sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config && systemctl reload sshd || systemctl reload ssh
echo 'SSH PermitRootLogin=no 已设置'`,
	"cis-ssh-02": `sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config && systemctl reload sshd || systemctl reload ssh
echo 'SSH PasswordAuthentication=no 已设置'`,
	"cis-firewall-01": `systemctl enable --now firewalld 2>/dev/null || systemctl enable --now ufw 2>/dev/null || echo '无 firewalld/ufw'
echo '防火墙已启用'`,
	"cis-file-01": `chmod 644 /etc/passwd
echo '/etc/passwd 权限已修复为 0644'`,
	"cis-file-02": `chmod 640 /etc/shadow
echo '/etc/shadow 权限已修复为 0640'`,
	"cis-password-01": `if [ -f /etc/security/pwquality.conf ]; then
  sed -i 's/^#\?minlen.*/minlen = 12/' /etc/security/pwquality.conf || echo 'minlen = 12' >> /etc/security/pwquality.conf
  echo 'pwquality minlen=12 已设置'
else echo '未找到 pwquality.conf'; fi`,
	"cis-audit-01": `systemctl enable --now auditd 2>/dev/null || echo 'auditd 不可用'
echo 'auditd 已启用'`,
	"cis-audit-02": `systemctl enable --now rsyslog 2>/dev/null || echo 'rsyslog 不可用'
echo 'rsyslog 已启用'`,
	"cis-kernel-01": `sysctl -w net.ipv4.ip_forward=0
sed -i '/^net.ipv4.ip_forward/d' /etc/sysctl.d/99-opsmesh.conf 2>/dev/null || true
echo 'net.ipv4.ip_forward = 0' >> /etc/sysctl.d/99-opsmesh.conf
echo 'IP 转发已禁用并持久化'`,
	"cis-kernel-02": `sysctl -w net.ipv4.tcp_syncookies=1
sed -i '/^net.ipv4.tcp_syncookies/d' /etc/sysctl.d/99-opsmesh.conf 2>/dev/null || true
echo 'net.ipv4.tcp_syncookies = 1' >> /etc/sysctl.d/99-opsmesh.conf
echo 'SYN Cookies 已启用并持久化'`,
	"cis-service-01": `systemctl disable --now telnet.socket 2>/dev/null || true
echo 'telnet 服务已禁用'`,
	"cis-selinux-01": `sed -i 's/^SELINUX=.*/SELINUX=enforcing/' /etc/selinux/config 2>/dev/null || echo '未找到 selinux config'
echo 'SELinux=enforcing 已设置（需重启生效）'`,
	"cis-time-01": `systemctl enable --now chronyd 2>/dev/null || systemctl enable --now ntpd 2>/dev/null || echo '无 chronyd/ntpd'
echo '时间同步已启用'`,
	"cis-sudo-01": `if [ -d /etc/audit/rules.d ]; then
  echo '-a always,exit -F arch=b64 -S execve -F path=/usr/bin/sudo -k sudo' > /etc/audit/rules.d/sudo.rules
  augenrules --load 2>/dev/null || true
  echo 'sudo 审计规则已添加'
else echo '未找到 audit rules.d'; fi`,
}

// handleSecurityBaseline 处理 POST /api/v1/security/baseline：基线检查/修复。
func (s *Server) handleSecurityBaseline(w http.ResponseWriter, r *http.Request) {
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
	var req securityBaselineReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	// 校验规则 ID 白名单（仅允许内置修复映射中存在的 ID）。
	for _, id := range req.RuleIDs {
		if !baselineRuleRe.MatchString(id) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid rule id: " + id})
			return
		}
		if req.Fix {
			if _, exists := baselineFixScripts[id]; !exists {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "no fix script for rule: " + id})
				return
			}
		}
	}

	script := buildSecurityBaselineScript(req)
	auditAction := "security_baseline_check"
	if req.Fix {
		auditAction = "security_baseline_fix"
	}
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, auditAction)
	if err != nil {
		writeInternalError(r.Context(), w, "security.baseline", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: auditAction, Target: strings.Join(req.RuleIDs, ","),
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildSecurityBaselineScript(req securityBaselineReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	// 规则检查脚本（与 compliance 引擎一致的精简检查集）。
	checks := map[string]string{ //nolint:gosec // 检查脚本含 sshd 配置项名（PasswordAuthentication 等），非硬编码凭据。
		"cis-ssh-01":      `grep -qE "^PermitRootLogin\s+no" /etc/ssh/sshd_config && echo "PASS cis-ssh-01" || echo "FAIL cis-ssh-01"`,
		"cis-ssh-02":      `grep -qE "^PasswordAuthentication\s+no" /etc/ssh/sshd_config && echo "PASS cis-ssh-02" || echo "FAIL cis-ssh-02"`,
		"cis-firewall-01": `(systemctl is-active firewalld >/dev/null 2>&1 || systemctl is-active ufw >/dev/null 2>&1) && echo "PASS cis-firewall-01" || echo "FAIL cis-firewall-01"`,
		"cis-file-01":     `[ "$(stat -c %a /etc/passwd)" = "644" ] && echo "PASS cis-file-01" || echo "FAIL cis-file-01"`,
		"cis-file-02":     `[ "$(stat -c %a /etc/shadow)" = "640" ] && echo "PASS cis-file-02" || echo "FAIL cis-file-02"`,
		"cis-password-01": `grep -qE "minlen\s*=\s*1[2-9]|[2-9][0-9]" /etc/security/pwquality.conf 2>/dev/null && echo "PASS cis-password-01" || echo "FAIL cis-password-01"`,
		"cis-audit-01":    `systemctl is-active auditd >/dev/null 2>&1 && echo "PASS cis-audit-01" || echo "FAIL cis-audit-01"`,
		"cis-audit-02":    `systemctl is-active rsyslog >/dev/null 2>&1 && echo "PASS cis-audit-02" || echo "FAIL cis-audit-02"`,
		"cis-kernel-01":   `[ "$(sysctl -n net.ipv4.ip_forward)" = "0" ] && echo "PASS cis-kernel-01" || echo "FAIL cis-kernel-01"`,
		"cis-kernel-02":   `[ "$(sysctl -n net.ipv4.tcp_syncookies)" = "1" ] && echo "PASS cis-kernel-02" || echo "FAIL cis-kernel-02"`,
		"cis-service-01":  `(! systemctl is-enabled telnet.socket >/dev/null 2>&1 && ! systemctl is-active telnet.socket >/dev/null 2>&1) && echo "PASS cis-service-01" || echo "FAIL cis-service-01"`,
		"cis-selinux-01":  `[ "$(getenforce 2>/dev/null)" = "Enforcing" ] && echo "PASS cis-selinux-01" || echo "FAIL cis-selinux-01"`,
		"cis-time-01":     `(systemctl is-active chronyd >/dev/null 2>&1 || systemctl is-active ntpd >/dev/null 2>&1) && echo "PASS cis-time-01" || echo "FAIL cis-time-01"`,
		"cis-sudo-01":     `(grep -q "/usr/bin/sudo" /etc/audit/audit.rules 2>/dev/null || grep -rq "sudo" /etc/audit/rules.d/ 2>/dev/null) && echo "PASS cis-sudo-01" || echo "FAIL cis-sudo-01"`,
	}
	// 规则顺序固定（遍历 map 不稳定，用切片固定顺序）。
	ruleOrder := []string{
		"cis-ssh-01", "cis-ssh-02", "cis-firewall-01", "cis-file-01", "cis-file-02",
		"cis-password-01", "cis-audit-01", "cis-audit-02", "cis-kernel-01", "cis-kernel-02",
		"cis-service-01", "cis-selinux-01", "cis-time-01", "cis-sudo-01",
	}
	selected := req.RuleIDs
	if len(selected) == 0 {
		selected = ruleOrder
	}
	b.WriteString("# 基线检查\n")
	for _, id := range selected {
		if c, ok := checks[id]; ok {
			b.WriteString(c + "\n")
		}
	}
	if req.Fix {
		b.WriteString("# 基线修复\n")
		for _, id := range selected {
			if fix, ok := baselineFixScripts[id]; ok {
				fmt.Fprintf(&b, "# --- 修复 %s ---\n", id)
				b.WriteString(fix + "\n")
			}
		}
		b.WriteString("echo '基线修复完成'\n")
	} else {
		b.WriteString("echo '基线检查完成（未修复）'\n")
	}
	return b.String()
}

// handleSecurityPatch 处理 POST /api/v1/security/patch：组件更新。
func (s *Server) handleSecurityPatch(w http.ResponseWriter, r *http.Request) {
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
	var req securityPatchReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Action != "check" && req.Action != "update" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (check/update)"})
		return
	}
	for _, pkg := range req.Packages {
		if !securityPackageRe.MatchString(pkg) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid package name: " + pkg})
			return
		}
	}
	if req.Action == "update" && len(req.Packages) == 0 && !req.SecurityOnly {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "update requires packages or securityOnly=true"})
		return
	}

	script := buildSecurityPatchScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "security_patch_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "security.patch", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "security_patch_" + req.Action, Target: strings.Join(req.Packages, ","),
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s securityOnly=%v", req.DeviceID, req.SecurityOnly)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildSecurityPatchScript(req securityPatchReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	pkgList := strings.Join(req.Packages, " ")
	if req.Action == "check" {
		b.WriteString("# 查询组件当前版本与可用版本\n")
		if len(req.Packages) > 0 {
			for _, pkg := range req.Packages {
				fmt.Fprintf(&b, "echo '=== %s ==='\n", pkg)
				fmt.Fprintf(&b, "if command -v rpm >/dev/null 2>&1; then rpm -q %s 2>/dev/null || echo '未安装'; yum list available %s 2>/dev/null | tail -3 || true; elif command -v dpkg >/dev/null 2>&1; then dpkg -l %s 2>/dev/null | tail -2 || echo '未安装'; apt list --upgradable 2>/dev/null | grep '^%s/' || true; fi\n", pkg, pkg, pkg, pkg)
			}
		} else {
			b.WriteString("if command -v yum >/dev/null 2>&1; then yum check-update 2>/dev/null | tail -n +2 | head -100 || true; elif command -v apt-get >/dev/null 2>&1; then apt list --upgradable 2>/dev/null | head -100; fi\n")
		}
	} else {
		b.WriteString("# 执行组件更新\n")
		if len(req.Packages) > 0 {
			if req.SecurityOnly {
				fmt.Fprintf(&b, "if command -v yum >/dev/null 2>&1; then yum update -y --security %s || yum update -y %s; elif command -v apt-get >/dev/null 2>&1; then apt-get install -y --only-upgrade %s; fi\n", pkgList, pkgList, pkgList)
			} else {
				fmt.Fprintf(&b, "if command -v yum >/dev/null 2>&1; then yum update -y %s; elif command -v dnf >/dev/null 2>&1; then dnf update -y %s; elif command -v apt-get >/dev/null 2>&1; then apt-get install -y --only-upgrade %s; fi\n", pkgList, pkgList, pkgList)
			}
		} else {
			b.WriteString("if command -v yum >/dev/null 2>&1; then yum update -y --security || yum update -y --bugfix; elif command -v apt-get >/dev/null 2>&1; then apt-get upgrade -y; fi\n")
		}
		b.WriteString("echo '组件更新完成（部分组件需重启服务/主机生效）'\n")
	}
	return b.String()
}

// handleSecurityRollback 处理 POST /api/v1/security/rollback：更新回滚。
func (s *Server) handleSecurityRollback(w http.ResponseWriter, r *http.Request) {
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
	var req securityRollbackReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !yumTxnRe.MatchString(req.TransactionID) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid transaction id"})
		return
	}

	script := buildSecurityRollbackScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "security_rollback")
	if err != nil {
		writeInternalError(r.Context(), w, "security.rollback", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "security_rollback", Target: req.TransactionID,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildSecurityRollbackScript(req securityRollbackReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# 回滚最近的更新事务\n")
	b.WriteString("if command -v yum >/dev/null 2>&1; then\n")
	fmt.Fprintf(&b, "  yum history undo %s -y || echo '回滚失败（事务不存在或不可逆）'\n", req.TransactionID)
	b.WriteString("elif command -v dnf >/dev/null 2>&1; then\n")
	fmt.Fprintf(&b, "  dnf history undo %s -y || echo '回滚失败'\n", req.TransactionID)
	b.WriteString("elif command -v apt-get >/dev/null 2>&1; then\n")
	b.WriteString("  echo 'Debian/Ubuntu 回滚需手动降级：apt-get install <pkg>=<old-version>'\n")
	b.WriteString(fiProbe())
	b.WriteString("echo '更新回滚操作完成'\n")
	return b.String()
}

// fiProbe 返回一个安全的 fi 片段（保持 shell 语法完整）。
func fiProbe() string {
	return "fi\n"
}

// ============================================================================
// 路由注册
// ============================================================================

// registerSecurityRoutes 注册安全运维路由。
func (s *Server) registerSecurityRoutes(mux *http.ServeMux) {
	// 固定路径优先
	mux.HandleFunc("/api/v1/security/scan", s.handleSecurityScan)
	mux.HandleFunc("/api/v1/security/baseline", s.handleSecurityBaseline)
	mux.HandleFunc("/api/v1/security/patch", s.handleSecurityPatch)
	mux.HandleFunc("/api/v1/security/rollback", s.handleSecurityRollback)
	// 泛路由：GET /api/v1/security/{deviceID}
	mux.HandleFunc("/api/v1/security/", s.handleSecurityRouting)
}

func (s *Server) handleSecurityRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/security/")
	segments := strings.Split(path, "/")
	if len(segments) == 1 && segments[0] != "" {
		s.handleSecurity(w, r, segments[0])
		return
	}
	paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}
