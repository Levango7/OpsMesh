// ansible_ops.go Ansible 自动化运维集成。
//
// 背景：OpsMesh 现有执行机制是"控制面生成 shell 模板 → agent 执行"（无状态、单机粒度），
// 缺少 Ansible 的核心能力：inventory（主机清单）、playbook（声明式剧本）、ad-hoc、
// 幂等编排、批量主机管理。本文件补这批能力（在 agent 侧落地 Ansible 运行时）。
//
// 设计原则（延续 P0-P5"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/ansible/{deviceID} 查询 Ansible 安装状态/版本。
//   - 执行类：POST /api/v1/ansible/... 下发 shell 模板到 agent 执行。
//
// 能力范围：
//   - 安装：pip/yum/apt 安装 ansible-core。
//   - inventory：生成/查看主机清单（/etc/ansible/hosts）。
//   - ad-hoc：ansible <pattern> -m <module> -a <args>（module 白名单）。
//   - playbook：将剧本写入 /etc/ansible/playbooks/<name>.yml 后执行（ansible-playbook）。
//     剧本内容存于 agent 本地目录，控制面仅下发内容（不接受路径穿越）。
//   - 语法检查：ansible-playbook --syntax-check。
//
// API 端点：
//   - GET  /api/v1/ansible/{deviceID}       — Ansible 状态
//   - POST /api/v1/ansible/deploy            — 安装 ansible-core
//   - POST /api/v1/ansible/adhoc             — ad-hoc 执行 {deviceID, pattern, module, args}
//   - POST /api/v1/ansible/playbook          — playbook 执行 {deviceID, action: run/check/syntax, name, content}
//   - POST /api/v1/ansible/inventory         — inventory 管理 {deviceID, action: list/sync, hosts}
//
// 安全：module 走白名单；pattern/playbook 名走正则校验；剧本内容拒绝 NUL；
// 5xx 错误走 writeInternalError，不回吐 err.Error()。

package controlplane

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// Ansible ad-hoc module 白名单（常用且安全的模块）。
var allowedAnsibleModules = map[string]bool{
	"ping":       true, // 连通性
	"command":    true, // 命令（不经过 shell）
	"shell":      true, // shell 命令
	"copy":       true, // 文件复制
	"file":       true, // 文件属性
	"service":    true, // 服务管理
	"systemd":    true, // systemd 管理
	"yum":        true, // yum 包管理
	"apt":        true, // apt 包管理
	"setup":      true, // 收集 facts
	"user":       true, // 用户管理
	"group":      true, // 组管理
	"cron":       true, // 计划任务
	"lineinfile": true, // 行编辑
	"template":   true, // 模板渲染
	"get_url":    true, // 下载
	"mount":      true, // 挂载
	"debug":      true, // 调试输出
}

// Ansible 操作动作白名单。
var allowedAnsiblePlaybookActions = map[string]bool{
	"run":    true, // 执行
	"check":  true, // 干跑（--check）
	"syntax": true, // 语法检查
}

// Ansible inventory 动作白名单。
var allowedAnsibleInventoryActions = map[string]bool{
	"list": true,
	"sync": true,
}

// pattern 正则（ansible 主机模式：hosts/group/all，支持通配符与冒号分隔）。
var ansiblePatternRe = regexp.MustCompile(`^[a-zA-Z0-9_.\-\*:\[\]]{1,256}$`)

// playbook 名正则。
var ansiblePlaybookRe = regexp.MustCompile(`^[a-zA-Z0-9_.\-]{1,128}$`)

// validatedAnsibleAdhoc / validatedAnsiblePlaybook / validatedAnsibleInventory 是白名单校验后的形态。
// builder 只接受这几种类型 —— 原始 ansibleAdhocReq / ansiblePlaybookReq / ansibleInventoryReq 无法被传入 builder。
// Pattern 并入 validated 结构（默认值 all 在 validator 内置）。
type validatedAnsibleAdhoc struct {
	Pattern, Module, Args string
}

type validatedAnsiblePlaybook struct {
	Action, Name, Content string
}

type validatedAnsibleInventory struct {
	Action, Hosts string
}

func validateAnsibleAdhoc(req ansibleAdhocReq) (validatedAnsibleAdhoc, error) {
	if !allowedAnsibleModules[req.Module] {
		return validatedAnsibleAdhoc{}, errors.New("unsupported module (whitelist enforced)")
	}
	pattern := req.Pattern
	if pattern == "" {
		pattern = "all"
	}
	if !ansiblePatternRe.MatchString(pattern) {
		return validatedAnsibleAdhoc{}, errors.New("invalid pattern")
	}
	if strings.ContainsAny(req.Args, "\x00") {
		return validatedAnsibleAdhoc{}, errors.New("args must not contain NUL")
	}
	return validatedAnsibleAdhoc{Pattern: pattern, Module: req.Module, Args: req.Args}, nil
}

func validateAnsiblePlaybook(req ansiblePlaybookReq) (validatedAnsiblePlaybook, error) {
	if !allowedAnsiblePlaybookActions[req.Action] {
		return validatedAnsiblePlaybook{}, errors.New("unsupported action (run/check/syntax)")
	}
	if !ansiblePlaybookRe.MatchString(req.Name) {
		return validatedAnsiblePlaybook{}, errors.New("invalid playbook name")
	}
	if strings.ContainsAny(req.Content, "\x00") {
		return validatedAnsiblePlaybook{}, errors.New("content must not contain NUL")
	}
	return validatedAnsiblePlaybook{Action: req.Action, Name: req.Name, Content: req.Content}, nil
}

func validateAnsibleInventory(req ansibleInventoryReq) (validatedAnsibleInventory, error) {
	if !allowedAnsibleInventoryActions[req.Action] {
		return validatedAnsibleInventory{}, errors.New("unsupported action (list/sync)")
	}
	if req.Action == "sync" {
		if strings.TrimSpace(req.Hosts) == "" {
			return validatedAnsibleInventory{}, errors.New("hosts is required for sync")
		}
		if strings.ContainsAny(req.Hosts, "\x00") {
			return validatedAnsibleInventory{}, errors.New("hosts must not contain NUL")
		}
	}
	return validatedAnsibleInventory{Action: req.Action, Hosts: req.Hosts}, nil
}

// ansibleAdhocReq ad-hoc 请求体。
type ansibleAdhocReq struct {
	DeviceID string `json:"deviceID"`
	Pattern  string `json:"pattern"` // 主机模式（all/group/host，默认 all）
	Module   string `json:"module"`  // 模块名（白名单）
	Args     string `json:"args"`    // 模块参数（如 'cmd=uptime'）
}

// ansiblePlaybookReq playbook 请求体。
type ansiblePlaybookReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"`  // run/check/syntax
	Name     string `json:"name"`    // playbook 名（不含扩展名）
	Content  string `json:"content"` // 剧本内容（可选：提供时先写入）
}

// ansibleInventoryReq inventory 请求体。
type ansibleInventoryReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // list/sync
	Hosts    string `json:"hosts"`  // INI 格式主机清单（sync 时必填）
}

// handleAnsible 查询设备 Ansible 状态。
func (s *Server) handleAnsible(w http.ResponseWriter, r *http.Request, deviceID string) {
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
	// Ansible 是 CLI 工具（非服务），探测 sys 属性/服务列表仅供参考。
	status := "unknown"
	if metrics.Services != nil {
		for _, svc := range metrics.Services {
			if strings.EqualFold(svc.Name, "ansible") {
				status = svc.Status
				break
			}
		}
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"deviceID": deviceID,
		"ansible":  map[string]string{"status": status},
	})
}

// handleAnsibleDeploy 处理 POST /api/v1/ansible/deploy：安装 ansible-core。
func (s *Server) handleAnsibleDeploy(w http.ResponseWriter, r *http.Request) {
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
	var req ansibleAdhocReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}

	script := `set -e
if command -v ansible >/dev/null 2>&1; then echo 'Ansible 已安装'; ansible --version | head -1; exit 0; fi
if command -v yum >/dev/null 2>&1; then yum install -y ansible || yum install -y ansible-core
elif command -v apt-get >/dev/null 2>&1; then apt-get install -y ansible || apt-get install -y ansible-core
else pip3 install ansible-core || pip install ansible-core
fi
mkdir -p /etc/ansible/playbooks
ansible --version | head -1
echo 'Ansible 安装完成'`

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "ansible_deploy")
	if err != nil {
		writeInternalError(r.Context(), w, "ansible.deploy", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "ansible_deploy", Target: "ansible",
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// handleAnsibleAdhoc 处理 POST /api/v1/ansible/adhoc：ad-hoc 执行。
func (s *Server) handleAnsibleAdhoc(w http.ResponseWriter, r *http.Request) {
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
	var req ansibleAdhocReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	vreq, verr := validateAnsibleAdhoc(req)
	if verr != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
		return
	}

	script := buildAnsibleAdhocScript(vreq)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "ansible_adhoc")
	if err != nil {
		writeInternalError(r.Context(), w, "ansible.adhoc", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "ansible_adhoc", Target: vreq.Pattern + ":" + vreq.Module,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildAnsibleAdhocScript(vreq validatedAnsibleAdhoc) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	if vreq.Args != "" {
		fmt.Fprintf(&b, "ansible %s -m %s -a %s\n", vreq.Pattern, vreq.Module, shellQuote(vreq.Args))
	} else {
		fmt.Fprintf(&b, "ansible %s -m %s\n", vreq.Pattern, vreq.Module)
	}
	b.WriteString("echo 'Ansible ad-hoc 执行完成'\n")
	return b.String()
}

// handleAnsiblePlaybook 处理 POST /api/v1/ansible/playbook：playbook 执行。
func (s *Server) handleAnsiblePlaybook(w http.ResponseWriter, r *http.Request) {
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
	var req ansiblePlaybookReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	vreq, verr := validateAnsiblePlaybook(req)
	if verr != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
		return
	}

	script := buildAnsiblePlaybookScript(vreq)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "ansible_playbook_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "ansible.playbook", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "ansible_playbook_" + vreq.Action, Target: req.Name,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildAnsiblePlaybookScript(vreq validatedAnsiblePlaybook) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	pbPath := "/etc/ansible/playbooks/" + vreq.Name + ".yml"
	if vreq.Content != "" {
		b.WriteString("# 写入剧本（heredoc 防止 shell 解释）\n")
		fmt.Fprintf(&b, "cat > %s <<'ANSIBLE_PLAYBOOK_EOF'\n", pbPath)
		b.WriteString(vreq.Content)
		if !strings.HasSuffix(vreq.Content, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("ANSIBLE_PLAYBOOK_EOF\n")
	}
	switch vreq.Action {
	case "syntax":
		fmt.Fprintf(&b, "ansible-playbook --syntax-check %s\n", pbPath)
	case "check":
		fmt.Fprintf(&b, "ansible-playbook --check %s\n", pbPath)
	case "run":
		fmt.Fprintf(&b, "ansible-playbook %s\n", pbPath)
	}
	fmt.Fprintf(&b, "echo 'Ansible playbook %s 执行完成\n", vreq.Action)
	return b.String()
}

// handleAnsibleInventory 处理 POST /api/v1/ansible/inventory：inventory 管理。
func (s *Server) handleAnsibleInventory(w http.ResponseWriter, r *http.Request) {
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
	var req ansibleInventoryReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	vreq, verr := validateAnsibleInventory(req)
	if verr != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
		return
	}

	script := buildAnsibleInventoryScript(vreq)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "ansible_inventory_"+vreq.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "ansible.inventory", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "ansible_inventory_" + vreq.Action, Target: "inventory",
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildAnsibleInventoryScript(vreq validatedAnsibleInventory) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch vreq.Action {
	case "list":
		b.WriteString("# 查看主机清单\n")
		b.WriteString("if [ -f /etc/ansible/hosts ]; then cat /etc/ansible/hosts; else echo '（inventory 为空）'; fi\n")
		b.WriteString("echo '=== ansible-inventory 解析结果 ==='\n")
		b.WriteString("ansible-inventory --list 2>/dev/null | head -50 || true\n")
	case "sync":
		b.WriteString("# 同步主机清单（写入 /etc/ansible/hosts，保留原文件备份）\n")
		b.WriteString("mkdir -p /etc/ansible\n")
		b.WriteString("[ -f /etc/ansible/hosts ] && cp /etc/ansible/hosts /etc/ansible/hosts.bak.$(date +%s) || true\n")
		b.WriteString("cat > /etc/ansible/hosts <<'ANSIBLE_INVENTORY_EOF'\n")
		b.WriteString(vreq.Hosts)
		if !strings.HasSuffix(vreq.Hosts, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("ANSIBLE_INVENTORY_EOF\n")
		b.WriteString("echo '=== 连通性测试（ansible all -m ping）==='\n")
		b.WriteString("ansible all -m ping --timeout 5 2>&1 | head -30 || true\n")
	}
	b.WriteString("echo 'Ansible inventory 操作完成'\n")
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerAnsibleRoutes 注册 Ansible 运维路由。
func (s *Server) registerAnsibleRoutes(mux *http.ServeMux) {
	// 固定路径优先
	mux.HandleFunc("/api/v1/ansible/deploy", s.handleAnsibleDeploy)
	mux.HandleFunc("/api/v1/ansible/adhoc", s.handleAnsibleAdhoc)
	mux.HandleFunc("/api/v1/ansible/playbook", s.handleAnsiblePlaybook)
	mux.HandleFunc("/api/v1/ansible/inventory", s.handleAnsibleInventory)
	// 泛路由：GET /api/v1/ansible/{deviceID}
	mux.HandleFunc("/api/v1/ansible/", s.handleAnsibleRouting)
}

func (s *Server) handleAnsibleRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/ansible/")
	segments := strings.Split(path, "/")
	if len(segments) == 1 && segments[0] != "" {
		s.handleAnsible(w, r, segments[0])
		return
	}
	paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}
