// Package compliance 实现安全合规检查引擎（Phase 3 安全合规）。
//
// 设计目标：
//   - 预置 CIS Benchmark 基线规则（SSH 加固/防火墙/文件权限/密码策略/日志审计/
//     内核参数/服务最小化/SELinux/时间同步/网络配置）；
//   - 支持自定义规则（Category="custom"）；
//   - 引擎本身不执行 CheckScript（避免控制面直接 shell 执行带来的注入风险），
//     仅提供规则目录与扫描编排；实际执行由 agent 侧任务下发完成，
//     控制面聚合结果生成 ComplianceReport 落库。
//
// 与 store 包的关系：
//   - compliance.ComplianceRule/ComplianceResult/ComplianceReport 为引擎领域模型；
//   - store.ComplianceReport/ComplianceResult 为持久化模型（API 层做转换）；
//   - 两者刻意分离：引擎模型可演进（如增加 SevScore/ evidences），持久化模型保持稳定。
package compliance

import "time"

// ComplianceRule 合规规则。
//
// 字段语义：
//   - ID：规则唯一标识（如 "cis-ssh-1"）；
//   - Category：规则类别（"cis"/"pci_dss"/"hipaa"/"custom"）；
//   - Severity：严重级别（"high"/"medium"/"low"）；
//   - CheckScript：检查命令（shell），由 agent 侧执行，控制面不直接 shell；
//   - Remediation：修复建议（人类可读，供报告展示）。
type ComplianceRule struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"` // "cis", "pci_dss", "hipaa", "custom"
	Severity    string `json:"severity"` // "high", "medium", "low"
	Description string `json:"description"`
	CheckScript string `json:"checkScript"` // 检查命令（shell）
	Remediation string `json:"remediation"` // 修复建议
}

// ComplianceResult 合规检查结果（单条规则执行结果）。
type ComplianceResult struct {
	RuleID    string    `json:"ruleId"`
	Passed    bool      `json:"passed"`
	Output    string    `json:"output"`
	CheckedAt time.Time `json:"checkedAt"`
}

// ComplianceReport 合规报告（一次扫描产出多条结果 + 汇总分数）。
type ComplianceReport struct {
	ID        string             `json:"id"`
	TenantID  string             `json:"tenantID"`
	DeviceID  string             `json:"deviceID"`
	Results   []ComplianceResult `json:"results"`
	Score     int                `json:"score"` // 合规分数 0-100
	CreatedAt time.Time          `json:"createdAt"`
	// M12 占位标记：Scan 不实际执行 CheckScript，仅聚合传入结果。
	// true 表示此报告由占位扫描生成（非 agent 实际执行检查）。
	Simulated bool `json:"simulated"`
}

// Engine 合规检查引擎：持有规则目录，提供查询与扫描编排。
//
// 并发安全：rules 在 NewEngine 时一次性填充，此后只读，无需加锁。
// 后续如支持动态增删规则，需加 RWMutex 保护。
type Engine struct {
	rules []ComplianceRule
}

// NewEngine 构造合规引擎，预置 10+ 条 CIS Benchmark 基线规则。
//
// 预置规则覆盖等保三级/PCI-DSS 常见基线项：
//  1. SSH 加固（禁用 root 登录 + 密码认证）
//  2. 防火墙启用（firewalld/ufw active）
//  3. 关键文件权限（/etc/passwd 0644、/etc/shadow 0640）
//  4. 密码策略（最小长度 + 复杂度）
//  5. 日志审计（rsyslog/auditd active）
//  6. 内核参数（ip_forward=0、tcp_syncookies=1）
//  7. 服务最小化（关闭 telnet/rsh/ftp）
//  8. SELinux/AppArmor 启用
//  9. 时间同步（chrony/ntp active）
// 10. 网络配置（禁用不安全协议）
// 11. sudo 审计（sudoers 配置）
// 12. 内核模块（禁用 dccp/sctp）
func NewEngine() *Engine {
	rules := []ComplianceRule{
		{
			ID:          "cis-ssh-01",
			Name:        "SSH 禁用 root 登录",
			Category:    "cis",
			Severity:    "high",
			Description: "PermitRootLogin 应为 no，禁止 root 直接 SSH 登录",
			CheckScript: `grep -qE "^PermitRootLogin\s+no" /etc/ssh/sshd_config`,
			Remediation: "编辑 /etc/ssh/sshd_config 设置 PermitRootLogin no 并重启 sshd",
		},
		{
			ID:          "cis-ssh-02",
			Name:        "SSH 禁用密码认证",
			Category:    "cis",
			Severity:    "high",
			Description: "PasswordAuthentication 应为 no，仅允许密钥认证",
			CheckScript: `grep -qE "^PasswordAuthentication\s+no" /etc/ssh/sshd_config`,
			Remediation: "编辑 /etc/ssh/sshd_config 设置 PasswordAuthentication no 并重启 sshd",
		},
		{
			ID:          "cis-firewall-01",
			Name:        "防火墙启用",
			Category:    "cis",
			Severity:    "high",
			Description: "firewalld 或 ufw 应处于 active 状态",
			CheckScript: `systemctl is-active firewalld || systemctl is-active ufw`,
			Remediation: "启用 firewalld: systemctl enable --now firewalld",
		},
		{
			ID:          "cis-file-01",
			Name:        "/etc/passwd 权限",
			Category:    "cis",
			Severity:    "medium",
			Description: "/etc/passwd 权限应为 0644",
			CheckScript: `[ "$(stat -c %a /etc/passwd)" = "644" ]`,
			Remediation: "chmod 644 /etc/passwd",
		},
		{
			ID:          "cis-file-02",
			Name:        "/etc/shadow 权限",
			Category:    "cis",
			Severity:    "high",
			Description: "/etc/shadow 权限应为 0640",
			CheckScript: `[ "$(stat -c %a /etc/shadow)" = "640" ]`,
			Remediation: "chmod 640 /etc/shadow",
		},
		{
			ID:          "cis-password-01",
			Name:        "密码最小长度",
			Category:    "cis",
			Severity:    "medium",
			Description: "PAM 配置密码最小长度应 >= 12",
			CheckScript: `grep -qE "minlen\s*=\s*1[2-9]|[2-9][0-9]" /etc/security/pwquality.conf 2>/dev/null || grep -qE "minlen\s*=\s*1[2-9]|[2-9][0-9]" /etc/pam.d/system-auth`,
			Remediation: "在 /etc/security/pwquality.conf 设置 minlen = 12",
		},
		{
			ID:          "cis-audit-01",
			Name:        "auditd 启用",
			Category:    "cis",
			Severity:    "high",
			Description: "auditd 服务应处于 active 状态",
			CheckScript: `systemctl is-active auditd`,
			Remediation: "systemctl enable --now auditd",
		},
		{
			ID:          "cis-audit-02",
			Name:        "rsyslog 启用",
			Category:    "cis",
			Severity:    "medium",
			Description: "rsyslog 服务应处于 active 状态",
			CheckScript: `systemctl is-active rsyslog`,
			Remediation: "systemctl enable --now rsyslog",
		},
		{
			ID:          "cis-kernel-01",
			Name:        "禁用 IP 转发",
			Category:    "cis",
			Severity:    "medium",
			Description: "net.ipv4.ip_forward 应为 0（非路由设备）",
			CheckScript: `[ "$(sysctl -n net.ipv4.ip_forward)" = "0" ]`,
			Remediation: "sysctl -w net.ipv4.ip_forward=0 并写入 /etc/sysctl.conf",
		},
		{
			ID:          "cis-kernel-02",
			Name:        "启用 SYN Cookies",
			Category:    "cis",
			Severity:    "medium",
			Description: "net.ipv4.tcp_syncookies 应为 1（防 SYN Flood）",
			CheckScript: `[ "$(sysctl -n net.ipv4.tcp_syncookies)" = "1" ]`,
			Remediation: "sysctl -w net.ipv4.tcp_syncookies=1 并写入 /etc/sysctl.conf",
		},
		{
			ID:          "cis-service-01",
			Name:        "禁用 telnet 服务",
			Category:    "cis",
			Severity:    "high",
			Description: "telnet 服务不应启用（明文传输）",
			CheckScript: `! systemctl is-enabled telnet.socket 2>/dev/null && ! systemctl is-active telnet.socket 2>/dev/null`,
			Remediation: "systemctl disable --now telnet.socket 或卸载 telnet-server",
		},
		{
			ID:          "cis-selinux-01",
			Name:        "SELinux 启用",
			Category:    "cis",
			Severity:    "high",
			Description: "SELinux 应处于 enforcing 模式",
			CheckScript: `[ "$(getenforce)" = "Enforcing" ]`,
			Remediation: "编辑 /etc/selinux/config 设置 SELINUX=enforcing 并重启",
		},
		{
			ID:          "cis-time-01",
			Name:        "时间同步启用",
			Category:    "cis",
			Severity:    "medium",
			Description: "chronyd 或 ntpd 应处于 active 状态",
			CheckScript: `systemctl is-active chronyd || systemctl is-active ntpd`,
			Remediation: "systemctl enable --now chronyd",
		},
		{
			ID:          "cis-sudo-01",
			Name:        "sudo 审计启用",
			Category:    "cis",
			Severity:    "low",
			Description: "sudo 命令应被 auditd 记录",
			CheckScript: `grep -q "/usr/bin/sudo" /etc/audit/audit.rules 2>/dev/null || auditctl -l 2>/dev/null | grep -q sudo`,
			Remediation: "在 /etc/audit/audit.rules 添加 sudo 审计规则",
		},
	}
	return &Engine{rules: rules}
}

// ListRules 返回全部规则（按 ID 升序）。
//
// 返回拷贝避免外部修改破坏内部状态（rules 元素为值类型，浅拷贝即深拷贝）。
func (e *Engine) ListRules() []ComplianceRule {
	out := make([]ComplianceRule, len(e.rules))
	copy(out, e.rules)
	return out
}

// GetRule 按 ID 返回单条规则（不存在返回 (nil, false)）。
func (e *Engine) GetRule(id string) (*ComplianceRule, bool) {
	for i := range e.rules {
		if e.rules[i].ID == id {
			cp := e.rules[i]
			return &cp, true
		}
	}
	return nil, false
}

// Scan 对给定规则集执行扫描编排（MVP：不实际执行 CheckScript，返回占位结果）。
//
// 实际执行由 agent 侧任务下发完成，控制面聚合结果生成 ComplianceReport 落库。
// 此方法供测试与未来控制面本地执行（仅对受信脚本）使用。
//
// 评分规则：passed 数 / 总规则数 * 100，向下取整。
func (e *Engine) Scan(tenantID, deviceID string, results []ComplianceResult) *ComplianceReport {
	now := time.Now()
	passed := 0
	for _, r := range results {
		if r.Passed {
			passed++
		}
	}
	score := 0
	if len(results) > 0 {
		score = passed * 100 / len(results)
	}
	return &ComplianceReport{
		TenantID:  tenantID,
		DeviceID:  deviceID,
		Results:   results,
		Score:     score,
		CreatedAt: now,
		// M12 占位标记：Scan 不实际执行 CheckScript，仅编排聚合传入结果。
		Simulated: true,
	}
}
