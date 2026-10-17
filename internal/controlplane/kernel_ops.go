// kernel_ops.go 内核运维管理（内核版本/参数/模块/升级/GRUB）。
//
// 设计原则（延续 P0-P5"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/kernel/{deviceID} 读取内核版本与关键参数。
//   - 执行类：POST /api/v1/kernel/... 下发 shell 模板到 agent 执行。
//
// 能力范围：
//   - 内核信息：版本、发行版、架构、运行参数（sysctl 关键项）。
//   - 内核参数：sysctl 查询/设置（临时 + 持久化 /etc/sysctl.d/99-opsmesh.conf）。
//   - 内核模块：lsmod 列表、modprobe 加载、rmmod 卸载、持久化 /etc/modules-load.d/。
//   - 内核升级：yum/dnf/apt 升级安装新内核包，列出可用内核。
//   - GRUB 管理：查看内核条目、设置默认启动内核。
//
// API 端点：
//   - GET  /api/v1/kernel/{deviceID}       — 内核信息与关键参数
//   - POST /api/v1/kernel/param            — sysctl 参数查询/设置 {deviceID, action: query/set, name, value}
//   - POST /api/v1/kernel/module           — 模块管理 {deviceID, action: list/load/unload, name}
//   - POST /api/v1/kernel/upgrade          — 内核升级 {deviceID, action: list/install, version}
//   - POST /api/v1/kernel/grub             — GRUB 管理 {deviceID, action: list/set-default, index}
//
// 安全：action 走白名单；参数名/模块名/版本号走正则校验；值拒绝 shell 元字符；
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

// 内核操作动作白名单（按子域）。
var allowedKernelParamActions = map[string]bool{"query": true, "set": true}
var allowedKernelModuleActions = map[string]bool{"list": true, "load": true, "unload": true}
var allowedKernelUpgradeActions = map[string]bool{"list": true, "install": true}
var allowedKernelGrubActions = map[string]bool{"list": true, "set-default": true}

// 内核参数名正则（sysctl key）。
var kernelParamRe = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)*$`)

// 内核模块名正则。
var kernelModuleRe = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

// 内核版本号正则。
var kernelVersionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9_.\-]+)?$`)

// 内核参数值正则（允许数字、点、冒号、空格分隔的多个值、等号组合）。
var kernelValueRe = regexp.MustCompile(`^[a-zA-Z0-9_.:/ \-,]+$`)

// kernelParamReq 内核参数请求体。
type kernelParamReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // query/set
	Name     string `json:"name"`   // sysctl 键（如 net.ipv4.ip_forward）
	Value    string `json:"value"`  // 值（set 时必填）
}

// kernelModuleReq 内核模块请求体。
type kernelModuleReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // list/load/unload
	Name     string `json:"name"`   // 模块名
}

// kernelUpgradeReq 内核升级请求体。
type kernelUpgradeReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"`  // list/install
	Version  string `json:"version"` // 目标版本（install 时必填）
}

// kernelGrubReq GRUB 管理请求体。
type kernelGrubReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // list/set-default
	Index    int    `json:"index"`  // 条目索引（set-default 时必填）
}

// handleKernel 查询指定设备的内核信息。
func (s *Server) handleKernel(w http.ResponseWriter, r *http.Request, deviceID string) {
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
		"kernel": map[string]string{
			"version": metrics.Kernel,
			"os":      metrics.OS,
			"arch":    metrics.Arch,
		},
	})
}

// handleKernelParam 处理 POST /api/v1/kernel/param：sysctl 参数查询/设置。
func (s *Server) handleKernelParam(w http.ResponseWriter, r *http.Request) {
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
	var req kernelParamReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedKernelParamActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (query/set)"})
		return
	}
	if req.Action == "set" {
		if req.Name != "" && !kernelParamRe.MatchString(req.Name) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid parameter name"})
			return
		}
		if req.Value != "" && !kernelValueRe.MatchString(req.Value) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid parameter value"})
			return
		}
		if req.Name == "" || req.Value == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and value are required for set"})
			return
		}
	} else if req.Name != "" && !kernelParamRe.MatchString(req.Name) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid parameter name"})
		return
	}

	script := buildKernelParamScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "kernel_param_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "kernel.param", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "kernel_param_" + req.Action, Target: req.Name,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s value=%s", req.DeviceID, req.Value)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildKernelParamScript(req kernelParamReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Action {
	case "query":
		if req.Name != "" {
			b.WriteString(fmt.Sprintf("sysctl %s\n", req.Name))
		} else {
			b.WriteString("sysctl -a 2>/dev/null | head -200\n")
		}
	case "set":
		// 临时生效 + 持久化到 /etc/sysctl.d/99-opsmesh.conf
		b.WriteString(fmt.Sprintf("sysctl -w %s=%s\n", req.Name, req.Value))
		b.WriteString("# 持久化（幂等：先删除同名行再追加）\n")
		b.WriteString(fmt.Sprintf("sed -i '/^%s[[:space:]]*=/d' /etc/sysctl.d/99-opsmesh.conf 2>/dev/null || true\n", req.Name))
		b.WriteString(fmt.Sprintf("echo '%s = %s' >> /etc/sysctl.d/99-opsmesh.conf\n", req.Name, req.Value))
		b.WriteString("echo '内核参数已设置并持久化（/etc/sysctl.d/99-opsmesh.conf）'\n")
	}
	return b.String()
}

// handleKernelModule 处理 POST /api/v1/kernel/module：内核模块管理。
func (s *Server) handleKernelModule(w http.ResponseWriter, r *http.Request) {
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
	var req kernelModuleReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedKernelModuleActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (list/load/unload)"})
		return
	}
	if req.Action != "list" && !kernelModuleRe.MatchString(req.Name) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid module name"})
		return
	}

	script := buildKernelModuleScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "kernel_module_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "kernel.module", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "kernel_module_" + req.Action, Target: req.Name,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildKernelModuleScript(req kernelModuleReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Action {
	case "list":
		b.WriteString("lsmod\n")
	case "load":
		// 加载 + 持久化 /etc/modules-load.d/opsmesh.conf
		b.WriteString(fmt.Sprintf("modprobe %s\n", req.Name))
		b.WriteString(fmt.Sprintf("grep -qxF %s /etc/modules-load.d/opsmesh.conf 2>/dev/null || echo %s >> /etc/modules-load.d/opsmesh.conf\n", req.Name, req.Name))
		b.WriteString(fmt.Sprintf("echo '模块已加载并持久化: %s'\n", req.Name))
	case "unload":
		b.WriteString(fmt.Sprintf("modprobe -r %s || echo '卸载失败（可能被占用）'\n", req.Name))
		b.WriteString(fmt.Sprintf("sed -i '/^%s$/d' /etc/modules-load.d/opsmesh.conf 2>/dev/null || true\n", req.Name))
	}
	return b.String()
}

// handleKernelUpgrade 处理 POST /api/v1/kernel/upgrade：内核升级。
func (s *Server) handleKernelUpgrade(w http.ResponseWriter, r *http.Request) {
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
	var req kernelUpgradeReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedKernelUpgradeActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (list/install)"})
		return
	}
	if req.Action == "install" {
		if req.Version != "" && !kernelVersionRe.MatchString(req.Version) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid kernel version"})
			return
		}
		if req.Version == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "version is required for install"})
			return
		}
	}

	script := buildKernelUpgradeScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "kernel_upgrade_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "kernel.upgrade", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "kernel_upgrade_" + req.Action, Target: req.Version,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildKernelUpgradeScript(req kernelUpgradeReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	if req.Action == "list" {
		b.WriteString("# 列出已安装与可用内核\n")
		b.WriteString("echo '=== 当前运行内核 ==='\nuname -r\n")
		b.WriteString("echo '=== 已安装内核 ==='\n")
		b.WriteString("if command -v rpm >/dev/null 2>&1; then rpm -qa 'kernel*' | sort; elif command -v dpkg >/dev/null 2>&1; then dpkg -l 'linux-image*' | grep '^ii' | awk '{print $2, $3}'; fi\n")
		b.WriteString("echo '=== 可用内核（未安装）==='\n")
		b.WriteString("if command -v yum >/dev/null 2>&1; then yum list available 'kernel*' 2>/dev/null | head -20; elif command -v apt-get >/dev/null 2>&1; then apt-cache search '^linux-image' | head -20; fi\n")
	} else {
		b.WriteString("# 内核安装/升级\n")
		b.WriteString("if command -v yum >/dev/null 2>&1; then\n")
		b.WriteString(fmt.Sprintf("  yum install -y kernel-%s kernel-devel-%s || yum update -y kernel\n", req.Version, req.Version))
		b.WriteString("elif command -v dnf >/dev/null 2>&1; then\n")
		b.WriteString(fmt.Sprintf("  dnf install -y kernel-%s || dnf update -y kernel\n", req.Version))
		b.WriteString("elif command -v apt-get >/dev/null 2>&1; then\n")
		b.WriteString(fmt.Sprintf("  apt-get install -y linux-image-%s || apt-get install -y --only-upgrade linux-image-generic\n", req.Version))
		b.WriteString("fi\n")
		b.WriteString("echo '内核已安装，需重启生效（reboot required）'\n")
	}
	return b.String()
}

// handleKernelGrub 处理 POST /api/v1/kernel/grub：GRUB 管理。
func (s *Server) handleKernelGrub(w http.ResponseWriter, r *http.Request) {
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
	var req kernelGrubReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedKernelGrubActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (list/set-default)"})
		return
	}
	if req.Action == "set-default" && req.Index < 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index must be >= 0"})
		return
	}

	script := buildKernelGrubScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "kernel_grub_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "kernel.grub", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "kernel_grub_" + req.Action, Target: fmt.Sprintf("index=%d", req.Index),
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildKernelGrubScript(req kernelGrubReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Action {
	case "list":
		b.WriteString("# 列出 GRUB 内核条目\n")
		b.WriteString("if command -v grubby >/dev/null 2>&1; then\n")
		b.WriteString("  grubby --info=ALL | grep -E '^(index|title|kernel)='\n")
		b.WriteString("elif [ -f /boot/grub2/grub.cfg ]; then\n")
		b.WriteString("  grep -E \"^menuentry|^\\s*linux\" /boot/grub2/grub.cfg | head -40\n")
		b.WriteString("elif [ -f /boot/grub/grub.cfg ]; then\n")
		b.WriteString("  grep -E \"^menuentry|^\\s*linux\" /boot/grub/grub.cfg | head -40\n")
		b.WriteString("else echo '未找到 GRUB 配置'\nfi\n")
	case "set-default":
		b.WriteString("# 设置默认启动内核\n")
		b.WriteString(fmt.Sprintf("if command -v grubby >/dev/null 2>&1; then grubby --set-default-index=%d || echo '设置失败（索引越界？）'; fi\n", req.Index))
		b.WriteString(fmt.Sprintf("if command -v grub2-set-default >/dev/null 2>&1; then grub2-set-default %d || true; fi\n", req.Index))
		b.WriteString(fmt.Sprintf("if command -v sed >/dev/null 2>&1 && [ -f /etc/default/grub ]; then sed -i 's/^GRUB_DEFAULT=.*/GRUB_DEFAULT=%d/' /etc/default/grub || true; fi\n", req.Index))
		b.WriteString("echo '默认内核已设置，重启后生效'\n")
	}
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerKernelRoutes 注册内核运维路由。
func (s *Server) registerKernelRoutes(mux *http.ServeMux) {
	// 固定路径优先
	mux.HandleFunc("/api/v1/kernel/param", s.handleKernelParam)
	mux.HandleFunc("/api/v1/kernel/module", s.handleKernelModule)
	mux.HandleFunc("/api/v1/kernel/upgrade", s.handleKernelUpgrade)
	mux.HandleFunc("/api/v1/kernel/grub", s.handleKernelGrub)
	// 泛路由：GET /api/v1/kernel/{deviceID}
	mux.HandleFunc("/api/v1/kernel/", s.handleKernelRouting)
}

func (s *Server) handleKernelRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/kernel/")
	segments := strings.Split(path, "/")
	if len(segments) == 1 && segments[0] != "" {
		s.handleKernel(w, r, segments[0])
		return
	}
	paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}
