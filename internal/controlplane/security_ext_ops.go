// Package controlplane security_ext_ops.go 实现网络安全与漏洞扫描运维
// 覆盖：WAF (ModSecurity/Nginx)、DDoS 防护 (fail2ban/iptables rate limit)、
// SQL 注入/XSS 检测、漏洞扫描 (Trivy/Semgrep/gosec)、安全加固 (CIS/基线)
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// ============================================================================
// WAF 管理 (ModSecurity / Nginx WAF)
// ============================================================================

// handleWAF 处理 POST /api/v1/security/waf
func (s *Server) handleWAF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/enable/disable/rules/list-rules/add-rule/delete-rule
		Rule     string `json:"rule"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== WAF Status ===" && (nginx -T 2>/dev/null | grep -i "modsecurity\|waf" | head -5 || echo "WAF not configured") && echo "---" && (apachectl -M 2>/dev/null | grep -i security || echo "ModSecurity not loaded")`
	case "enable":
		script = `echo "Enabling WAF..." && (sed -i 's/SecRuleEngine DetectionOnly/SecRuleEngine On/' /etc/modsecurity/modsecurity.conf 2>/dev/null && echo "ModSecurity enabled" || echo "ModSecurity config not found") && (nginx -t 2>/dev/null && systemctl reload nginx && echo "Nginx reloaded" || echo "Nginx reload failed")`
	case "disable":
		script = `echo "Disabling WAF..." && (sed -i 's/SecRuleEngine On/SecRuleEngine DetectionOnly/' /etc/modsecurity/modsecurity.conf 2>/dev/null && echo "ModSecurity disabled" || echo "ModSecurity config not found") && (nginx -t 2>/dev/null && systemctl reload nginx && echo "Nginx reloaded" || echo "Nginx reload failed")`
	case "list-rules":
		script = `echo "=== WAF Rules ===" && ls /etc/modsecurity/rules/ 2>/dev/null || ls /etc/nginx/modsec/ 2>/dev/null || echo "No WAF rules found"`
	case "add-rule":
		if req.Rule == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "rule is required"})
			return
		}
		script = fmt.Sprintf(`echo '%s' >> /etc/modsecurity/rules/custom.conf 2>/dev/null && echo "Rule added" || echo "Failed to add rule"`, req.Rule)
	case "delete-rule":
		if req.Rule == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "rule is required"})
			return
		}
		script = fmt.Sprintf(`sed -i '/%s/d' /etc/modsecurity/rules/custom.conf 2>/dev/null && echo "Rule deleted" || echo "Failed to delete rule"`, req.Rule)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "waf_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// DDoS 防护
// ============================================================================

// handleDDoS 处理 POST /api/v1/security/ddos
func (s *Server) handleDDoS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/enable-fail2ban/enable-iptables/enable-cloudflare/status
		Threshold int   `json:"threshold"` // 连接数阈值
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Fail2ban ===" && (fail2ban-client status 2>/dev/null || echo "fail2ban not running") && echo "=== iptables rate limit ===" && (iptables -L -n 2>/dev/null | grep -i "limit\|drop" | head -10 || echo "No rate limits") && echo "=== Connection count ===" && (ss -s 2>/dev/null || netstat -s 2>/dev/null | head -5)`
	case "enable-fail2ban":
		script = `apt-get install -y fail2ban 2>/dev/null || yum install -y fail2ban 2>/dev/null && systemctl enable --now fail2ban && echo "fail2ban enabled"`
	case "enable-iptables":
		threshold := req.Threshold
		if threshold <= 0 {
			threshold = 100
		}
		script = fmt.Sprintf(`iptables -A INPUT -p tcp --syn -m connlimit --connlimit-above %d -j DROP 2>/dev/null && iptables -A INPUT -p tcp --syn -m limit --limit 100/s --limit-burst 200 -j ACCEPT 2>/dev/null && echo "iptables rate limit enabled (threshold=%d)"`, threshold, threshold)
	case "enable-cloudflare":
		script = `echo "Cloudflare DDoS protection: configure via Cloudflare dashboard or API" && echo "Enable 'Under Attack Mode' in Cloudflare settings"`
	case "status-all":
		script = `echo "=== DDoS Protection Status ===" && echo "--- Fail2ban ---" && (fail2ban-client status 2>/dev/null || echo "Not running") && echo "--- iptables ---" && (iptables -L INPUT -n 2>/dev/null | grep -i "limit\|drop" | head -5 || echo "No rules") && echo "--- Connections ---" && (ss -s 2>/dev/null || echo "N/A")`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "ddos_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// SQL 注入 / XSS 检测
// ============================================================================

// handleInjectionDetect 处理 POST /api/v1/security/injection
func (s *Server) handleInjectionDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // scan-sqli/scan-xss/scan-headers/scan-all
		Target   string `json:"target"` // URL or file path
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "scan-sqli":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (URL) is required"})
			return
		}
		script = fmt.Sprintf(`echo "=== SQL Injection Scan ===" && (sqlmap -u "%s" --batch --level=1 --risk=1 2>/dev/null || echo "sqlmap not available")`, req.Target)
	case "scan-xss":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (URL) is required"})
			return
		}
		script = fmt.Sprintf(`echo "=== XSS Scan ===" && (xsser -u "%s" 2>/dev/null || echo "xsser not available")`, req.Target)
	case "scan-headers":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (URL) is required"})
			return
		}
		script = fmt.Sprintf(`echo "=== Security Headers Check ===" && curl -sI "%s" 2>/dev/null | grep -iE "x-frame-options|x-content-type-options|strict-transport-security|content-security-policy|x-xss-protection" || echo "No security headers found"`, req.Target)
	case "scan-all":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (URL) is required"})
			return
		}
		script = fmt.Sprintf(`echo "=== Full Security Scan ===" && echo "--- Headers ---" && curl -sI "%s" 2>/dev/null | grep -iE "x-frame-options|x-content-type-options|strict-transport-security|content-security-policy" && echo "--- SQLi ---" && (sqlmap -u "%s" --batch --level=1 2>/dev/null | tail -5 || echo "sqlmap not available") && echo "--- XSS ---" && (xsser -u "%s" 2>/dev/null | tail -5 || echo "xsser not available")`, req.Target, req.Target, req.Target)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "injection_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 漏洞扫描 (Trivy/Semgrep/gosec)
// ============================================================================

// handleVulnScan 处理 POST /api/v1/security/vuln-scan
func (s *Server) handleVulnScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Scanner  string `json:"scanner"` // trivy/semgrep/gosec/bandit
		Action   string `json:"action"`  // scan-image/scan-fs/scan-repo/scan-code
		Target   string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	scanner := req.Scanner
	if scanner == "" {
		scanner = "trivy"
	}

	var script string
	switch req.Action {
	case "scan-image":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (image name) is required"})
			return
		}
		script = fmt.Sprintf(`trivy image %s 2>/dev/null || echo "Trivy not available"`, req.Target)
	case "scan-fs":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (path) is required"})
			return
		}
		script = fmt.Sprintf(`trivy fs %s 2>/dev/null || echo "Trivy not available"`, req.Target)
	case "scan-repo":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (repo URL) is required"})
			return
		}
		script = fmt.Sprintf(`trivy repo %s 2>/dev/null || echo "Trivy not available"`, req.Target)
	case "scan-code":
		if req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "target (path) is required"})
			return
		}
		switch scanner {
		case "semgrep":
			script = fmt.Sprintf(`semgrep scan --config auto %s 2>/dev/null || echo "Semgrep not available"`, req.Target)
		case "gosec":
			script = fmt.Sprintf(`gosec %s 2>/dev/null || echo "Gosec not available"`, req.Target)
		case "bandit":
			script = fmt.Sprintf(`bandit -r %s 2>/dev/null || echo "Bandit not available"`, req.Target)
		default:
			script = fmt.Sprintf(`trivy fs %s 2>/dev/null || echo "Trivy not available"`, req.Target)
		}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vuln_scan_"+scanner+"_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action, "scanner": scanner})
}

// ============================================================================
// 安全加固 (CIS/DISA STIG 基线)
// ============================================================================

// handleSecurityHardening 处理 POST /api/v1/security/hardening
func (s *Server) handleSecurityHardening(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // cis-baseline/stig-baseline/ssh-harden/audit-enable
		Level    string `json:"level"`  // level1/level2
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "cis-baseline":
		level := req.Level
		if level == "" {
			level = "level1"
		}
		script = fmt.Sprintf(`echo "=== CIS Baseline (%s) ===" && echo "1. Ensure /tmp is separate partition" && echo "2. Ensure nodev/nosuid/noexec on /tmp" && echo "3. Ensure SELinux/AppArmor enabled" && echo "4. Ensure firewall configured" && echo "5. Ensure SSH root login disabled" && echo "6. Ensure password complexity" && echo "7. Ensure auditd enabled" && echo "8. Ensure time sync configured"`, level)
	case "stig-baseline":
		script = `echo "=== DISA STIG Baseline ===" && echo "1. Account lockout policy" && echo "2. Password complexity" && echo "3. Audit logging" && echo "4. SELinux enforcing" && echo "5. Firewall rules" && echo "6. SSH hardening" && echo "7. Kernel parameters" && echo "8. File permissions"`
	case "ssh-harden":
		script = `cp /etc/ssh/sshd_config /etc/ssh/sshd_config.bak && sed -i 's/^#PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config && sed -i 's/^#PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config && sed -i 's/^#PubkeyAuthentication.*/PubkeyAuthentication yes/' /etc/ssh/sshd_config && systemctl restart sshd && echo "SSH hardened"`
	case "audit-enable":
		script = `systemctl enable --now auditd && echo "Auditd enabled" && echo "Audit rules:" && auditctl -l 2>/dev/null | head -10`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "security_hardening_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// SSL/TLS 证书管理
// ============================================================================

// handleCertManagement 处理 POST /api/v1/security/cert
func (s *Server) handleCertManagement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/check/generate/renew/revoke
		CertPath string `json:"certPath"`
		Domain   string `json:"domain"`
		Days     int    `json:"days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `find /etc/ssl/certs /etc/pki/tls/certs /etc/letsencrypt/live -name "*.crt" -o -name "*.pem" 2>/dev/null | head -20`
	case "check":
		if req.CertPath == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "certPath is required"})
			return
		}
		script = fmt.Sprintf(`openssl x509 -in %s -noout -subject -issuer -dates -serial 2>/dev/null || echo "Certificate not found"`, req.CertPath)
	case "generate":
		days := req.Days
		if days <= 0 {
			days = 365
		}
		domain := req.Domain
		if domain == "" {
			domain = "localhost"
		}
		script = fmt.Sprintf(`openssl req -x509 -nodes -days %d -newkey rsa:2048 -keyout /tmp/%s.key -out /tmp/%s.crt -subj "/CN=%s" 2>/dev/null && echo "Certificate generated: /tmp/%s.crt (valid %d days)"`, days, domain, domain, domain, domain, days)
	case "renew":
		if req.Domain == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "domain is required"})
			return
		}
		script = fmt.Sprintf(`certbot renew --cert-name %s 2>/dev/null && echo "Certificate renewed for %s" || echo "Certbot not available"`, req.Domain, req.Domain)
	case "revoke":
		if req.CertPath == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "certPath is required"})
			return
		}
		script = fmt.Sprintf(`openssl revoke -in %s 2>/dev/null && echo "Certificate revoked" || echo "Revocation failed"`, req.CertPath)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "cert_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 路由注册
// ============================================================================

// registerSecurityExtRoutes 注册安全扩展路由
func (s *Server) registerSecurityExtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/security/waf", s.handleWAF)
	mux.HandleFunc("/api/v1/security/ddos", s.handleDDoS)
	mux.HandleFunc("/api/v1/security/injection", s.handleInjectionDetect)
	mux.HandleFunc("/api/v1/security/vuln-scan", s.handleVulnScan)
	mux.HandleFunc("/api/v1/security/hardening", s.handleSecurityHardening)
	mux.HandleFunc("/api/v1/security/cert", s.handleCertManagement)
}
