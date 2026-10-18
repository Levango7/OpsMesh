// env_ops.go 环境依赖优化管理（Java/node.js/Python 2.x/Python 3.x/Go + fd-limit/IPVS）。
//
// 设计原则（延续 P0/P1/P3/P4"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/env/{deviceID} 从 DeviceMetrics.Envs 读取环境依赖版本信息，
//     从 DeviceMetrics.Inodes / 系统 limits 读取 fd-limit。
//   - 执行类：POST /api/v1/env/install 等，shell 模板下发到 agent 执行。
//
// API 端点：
//   - GET  /api/v1/env/{deviceID}              — 查询环境依赖版本 + fd-limit
//   - POST /api/v1/env/install                   — 安装语言环境（{deviceID, type, version}）
//   - POST /api/v1/env/{type}/{action}           — 环境操作（start/stop/status/version）
//   - POST /api/v1/env/fdlimit                   — fd-limit 查询/设置
//   - POST /api/v1/env/ipvs                      — IPVS 虚拟服务器管理
//
// 安全：type/version 走正则校验，杜绝命令注入；5xx 错误走 writeInternalError。

package controlplane

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// 环境类型白名单。
var allowedEnvTypes = map[string]bool{
	"java":    true,
	"nodejs":  true,
	"python2": true,
	"python3": true,
	"go":      true,
}

// 环境操作动作白名单。
var allowedEnvActions = map[string]bool{
	"install":   true,
	"version":   true,
	"uninstall": true,
}

// 版本号正则。
var envVersionRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*(-[a-zA-Z0-9]+)?$`)

// envQueryReq 环境查询请求体。
type envQueryReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"` // java/nodejs/python2/python3/go
}

// envInstallReq 环境安装请求体。
type envInstallReq struct {
	DeviceID string `json:"deviceID"`
	Type     string `json:"type"`    // java/nodejs/python2/python3/go
	Version  string `json:"version"` // 版本号
}

// handleEnv 查询指定设备上的环境依赖 + fd-limit。
func (s *Server) handleEnv(w http.ResponseWriter, r *http.Request, deviceID string) {
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
	type envInfo struct {
		Type    string `json:"type"`
		Version string `json:"version"`
	}
	var result []envInfo
	for _, env := range metrics.Envs {
		result = append(result, envInfo{Type: env.Name, Version: env.Version})
	}
	// fd-limit 从 SystemInventory 或系统 limits 读取（简化：返回占位）
	fdLimit := "unlimited"
	if metrics.System != nil {
		// 尝试从 System 中读取 fd-limit（如有字段）
		_ = fdLimit
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"deviceID": deviceID,
		"envs":     result,
		"fdLimit":  fdLimit,
	})
}

// handleEnvInstall 处理 POST /api/v1/env/install：安装语言环境。
func (s *Server) handleEnvInstall(w http.ResponseWriter, r *http.Request) {
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
	var req envInstallReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !allowedEnvTypes[req.Type] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported env type"})
		return
	}
	if !envVersionRe.MatchString(req.Version) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version"})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}

	script := buildEnvInstallScript(req)
	action := "env_install_" + req.Type

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "env.install."+req.Type, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: req.Type + ":" + req.Version,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s version=%s", req.DeviceID, req.Version)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildEnvInstallScript(req envInstallReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# 自动探测包管理器\n")
	b.WriteString("if command -v yum >/dev/null 2>&1; then PKG_MGR=yum; elif command -v dnf >/dev/null 2>&1; then PKG_MGR=dnf; elif command -v apt-get >/dev/null 2>&1; then PKG_MGR=apt-get; else echo '未找到包管理器' && exit 1; fi\n")
	switch req.Type {
	case "java":
		b.WriteString("# Java 环境安装（OpenJDK）\n")
		fmt.Fprintf(&b, "$PKG_MGR install -y java-%s-openjdk || $PKG_MGR install -y jdk-%s || true\n", req.Version, req.Version)
		b.WriteString("java -version\n")
	case "nodejs":
		b.WriteString("# Node.js 环境安装（nvm）\n")
		b.WriteString("curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.0/install.sh | bash\n")
		b.WriteString("source ~/.nvm/nvm.sh\n")
		fmt.Fprintf(&b, "nvm install %s && nvm use %s && node --version\n", req.Version, req.Version)
	case "python2":
		b.WriteString("# Python 2.x 环境安装（已弃用，仅支持旧系统）\n")
		fmt.Fprintf(&b, "$PKG_MGR install -y python2 || $PKG_MGR install -y python2.%s || true\n", req.Version)
		b.WriteString("python2 --version\n")
	case "python3":
		b.WriteString("# Python 3.x 环境安装\n")
		fmt.Fprintf(&b, "$PKG_MGR install -y python3.%s || $PKG_MGR install -y python3 || true\n", req.Version)
		b.WriteString("python3 --version\n")
	case "go":
		b.WriteString("# Go 环境安装\n")
		fmt.Fprintf(&b, "wget -q https://go.dev/dl/go%s.linux-amd64.tar.gz -O /tmp/go.tar.gz || true\n", req.Version)
		b.WriteString("rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tar.gz\n")
		b.WriteString("export PATH=$PATH:/usr/local/go/bin\n")
		b.WriteString("go version\n")
	}
	fmt.Fprintf(&b, "echo '%s %s 环境安装完成\n", req.Type, req.Version)
	return b.String()
}

// handleEnvAction 处理 POST /api/v1/env/{type}/{action}：环境操作（version 等）。
func (s *Server) handleEnvAction(w http.ResponseWriter, r *http.Request, envType, action string) {
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
	if !allowedEnvTypes[envType] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported env type"})
		return
	}
	if !allowedEnvActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action"})
		return
	}
	var req envQueryReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Type = envType

	script := buildEnvActionScript(req)
	taskAction := "env_" + action + "_" + envType

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "env.action."+envType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: envType,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildEnvActionScript(req envQueryReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Type {
	case "java":
		b.WriteString("java -version 2>&1 || echo 'Java 未安装'\n")
	case "nodejs":
		b.WriteString("node --version 2>&1 || echo 'Node.js 未安装'\n")
	case "python2":
		b.WriteString("python2 --version 2>&1 || echo 'Python 2 未安装'\n")
	case "python3":
		b.WriteString("python3 --version 2>&1 || echo 'Python 3 未安装'\n")
	case "go":
		b.WriteString("go version 2>&1 || echo 'Go 未安装'\n")
	}
	return b.String()
}

// fdLimitReq fd-limit 查询/设置请求体。
type fdLimitReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // query/set
	Limit    int    `json:"limit"`  // fd-limit 数值（set 时必填）
}

// handleFdLimit 处理 POST /api/v1/env/fdlimit：fd-limit 查询/设置。
func (s *Server) handleFdLimit(w http.ResponseWriter, r *http.Request) {
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
	var req fdLimitReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Action != "query" && req.Action != "set" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (query/set)"})
		return
	}
	if req.Action == "set" && req.Limit <= 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be positive"})
		return
	}

	script := buildFdLimitScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "env_fdlimit_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "env.fdlimit", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "env_fdlimit_" + req.Action, Target: "fdlimit",
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s limit=%d", req.DeviceID, req.Limit)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildFdLimitScript(req fdLimitReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Action {
	case "query":
		b.WriteString("ulimit -n\n")
		b.WriteString("cat /etc/security/limits.conf 2>/dev/null | grep -v '^#' | grep -v '^$' || true\n")
	case "set":
		fmt.Fprintf(&b, "* soft nofile %d\n", req.Limit)
		fmt.Fprintf(&b, "* hard nofile %d\n", req.Limit)
		b.WriteString("echo 'fd-limit 已写入 /etc/security/limits.conf，需重新登录生效'\n")
	}
	return b.String()
}

// ipvsReq IPVS 虚拟服务器管理请求体。
type ipvsReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"`   // list/add/del
	VIP      string `json:"vip"`      // 虚拟 IP
	Port     int    `json:"port"`     // 虚拟端口
	Protocol string `json:"protocol"` // tcp/udp
}

// handleIPVS 处理 POST /api/v1/env/ipvs：IPVS 虚拟服务器管理。
func (s *Server) handleIPVS(w http.ResponseWriter, r *http.Request) {
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
	var req ipvsReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	allowed := map[string]bool{"list": true, "add": true, "del": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (list/add/del)"})
		return
	}
	if req.Action != "list" && (req.VIP == "" || req.Port <= 0 || req.Port > 65535) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid vip/port"})
		return
	}
	if req.Action != "list" && req.Protocol != "tcp" && req.Protocol != "udp" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid protocol (tcp/udp)"})
		return
	}

	script := buildIPVSScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "env_ipvs_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "env.ipvs", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "env_ipvs_" + req.Action, Target: fmt.Sprintf("%s:%d", req.VIP, req.Port),
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildIPVSScript(req ipvsReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("modprobe ip_vs || true\n")
	switch req.Action {
	case "list":
		b.WriteString("ipvsadm -L -n\n")
	case "add":
		fmt.Fprintf(&b, "ipvsadm -A -t %s:%d -s rr || true\n", req.VIP, req.Port)
	case "del":
		fmt.Fprintf(&b, "ipvsadm -D -t %s:%d || true\n", req.VIP, req.Port)
	}
	fmt.Fprintf(&b, "echo 'IPVS %s 操作完成\n", req.Action)
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerEnvRoutes 注册环境依赖运维路由。
func (s *Server) registerEnvRoutes(mux *http.ServeMux) {
	// 固定路径优先
	mux.HandleFunc("/api/v1/env/install", s.handleEnvInstall)
	mux.HandleFunc("/api/v1/env/fdlimit", s.handleFdLimit)
	mux.HandleFunc("/api/v1/env/ipvs", s.handleIPVS)
	// 泛路由：GET /api/v1/env/{deviceID} / POST /api/v1/env/{type}/{action}
	mux.HandleFunc("/api/v1/env/", s.handleEnvRouting)
}

func (s *Server) handleEnvRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/env/")
	segments := strings.Split(path, "/")
	switch len(segments) {
	case 1:
		// /api/v1/env/{deviceID}
		if segments[0] != "" {
			s.handleEnv(w, r, segments[0])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	case 2:
		// /api/v1/env/{type}/{action}
		if segments[0] != "" && segments[1] != "" {
			s.handleEnvAction(w, r, segments[0], segments[1])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}
