// harbor_ops.go Harbor 镜像仓库运维管理。
//
// 设计原则（延续 P0/P1/P3/P4"查询走 API、执行走 shell"架构）：
//   - 查询类：GET /api/v1/harbor/{deviceID} 查询 Harbor 服务状态。
//   - 执行类：POST /api/v1/harbor/project/repository/artifact 等，shell 模板下发到 agent 执行，
//     使用 curl 调用 Harbor REST API。
//
// 安全：Harbor 管理员凭证不通过 shell 参数明文传递，改由 agent 侧环境变量
//（HARBOR_ADMIN_USER / HARBOR_ADMIN_PASSWORD）读取；project/repository/artifact 名走正则校验；
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

// Harbor 操作动作白名单。
var allowedHarborActions = map[string]bool{
	"deploy":   true,
	"start":    true,
	"stop":     true,
	"restart":  true,
	"status":   true,
	"undeploy": true,
}

// Harbor 资源操作动作白名单。
var allowedHarborResourceActions = map[string]bool{
	"create": true,
	"delete": true,
	"list":   true,
}

// 项目名正则。
var harborProjectRe = regexp.MustCompile(`^[a-zA-Z0-9_.\-]+$`)

// 仓库名正则。
var harborRepositoryRe = regexp.MustCompile(`^[a-zA-Z0-9_.\-/]+$`)

// Artifact 引用正则（仓库@引用或 digest）。
var harborArtifactRe = regexp.MustCompile(`^[a-zA-Z0-9_.\-/:@]+$`)

// Harbor 部署请求体。
type harborDeployRequest struct {
	DeviceID string `json:"deviceID"`
	Mode     string `json:"mode"`    // single/ha
	Version  string `json:"version"` // Harbor 版本
	Config   string `json:"config"`  // 部署配置 JSON
}

// harborOpsRequest 通用 Harbor 操作请求体。
type harborOpsRequest struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // start/stop/restart/status/undeploy
}

// harborProjectReq Harbor 项目操作请求体。
type harborProjectReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"`  // create/delete/list
	Project  string `json:"project"` // 项目名
}

// harborRepositoryReq Harbor 仓库操作请求体。
type harborRepositoryReq struct {
	DeviceID   string `json:"deviceID"`
	Action     string `json:"action"`     // list/delete
	Project    string `json:"project"`    // 项目名
	Repository string `json:"repository"` // 仓库名
}

// harborArtifactReq Harbor artifact 操作请求体。
type harborArtifactReq struct {
	DeviceID   string `json:"deviceID"`
	Action     string `json:"action"`     // list/delete
	Project    string `json:"project"`    // 项目名
	Repository string `json:"repository"` // 仓库名
	Reference  string `json:"reference"`  // artifact 引用（tag 或 digest）
}

// handleHarbor 查询指定设备上的 Harbor 状态。
func (s *Server) handleHarbor(w http.ResponseWriter, r *http.Request, deviceID string) {
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
	info := struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}{Type: "harbor", Status: "not-installed"}
	if metrics.Services != nil {
		for _, svc := range metrics.Services {
			if strings.EqualFold(svc.Name, "harbor") || strings.EqualFold(svc.Name, "harbor-server") {
				info.Status = svc.Status
				break
			}
		}
	}
	if info.Status == "not-installed" {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "harbor not installed on device"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"deviceID": deviceID, "harbor": info})
}

// handleHarborDeploy 处理 POST /api/v1/harbor/deploy：部署 Harbor。
func (s *Server) handleHarborDeploy(w http.ResponseWriter, r *http.Request) {
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
	var req harborDeployRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}

	script := buildHarborDeployScript(req)
	action := "harbor_deploy"

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, action)
	if err != nil {
		writeInternalError(r.Context(), w, "harbor.deploy", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: action, Target: "harbor",
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s mode=%s version=%s", req.DeviceID, req.Mode, req.Version)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildHarborDeployScript(req harborDeployRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# Harbor 部署（需 docker + docker-compose）\n")
	b.WriteString("if command -v docker >/dev/null 2>&1 && command -v docker-compose >/dev/null 2>&1; then echo 'docker 环境已就绪'; else echo '请先安装 docker 和 docker-compose' && exit 1; fi\n")
	b.WriteString("HARBOR_VERSION=${HARBOR_VERSION:-v2.8.0}\n")
	b.WriteString("HARBOR_HOST=${HARBOR_HOST:-localhost}\n")
	b.WriteString("mkdir -p /data/harbor\n")
	b.WriteString("cd /data/harbor || exit 1\n")
	b.WriteString("curl -fsSL https://github.com/goharbor/harbor/releases/download/$HARBOR_VERSION/harbor-offline-installer-$HARBOR_VERSION.tgz | tar xz\n")
	b.WriteString("cd harbor || exit 1\n")
	b.WriteString("sed -i 's/hostname = reg.mydomain.com/hostname = $HARBOR_HOST/' harbor.yml\n")
	b.WriteString("./install.sh\n")
	b.WriteString("echo 'Harbor 已部署 (https://$HARBOR_HOST)'\n")
	return b.String()
}

// handleHarborAction 处理 POST /api/v1/harbor/{action}：启停重启/状态。
func (s *Server) handleHarborAction(w http.ResponseWriter, r *http.Request, action string) {
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
	if !allowedHarborActions[action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action"})
		return
	}
	var req harborOpsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Action = action

	script := buildHarborActionScript(req)
	taskAction := "harbor_" + action

	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, taskAction)
	if err != nil {
		writeInternalError(r.Context(), w, "harbor.action", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: taskAction, Target: "harbor",
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildHarborActionScript(req harborOpsRequest) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# Harbor 启停（docker-compose）\n")
	b.WriteString("cd /data/harbor/harbor 2>/dev/null || true\n")
	switch req.Action {
	case "start":
		b.WriteString("docker-compose up -d || true\n")
	case "stop":
		b.WriteString("docker-compose down || true\n")
	case "restart":
		b.WriteString("docker-compose restart || true\n")
	case "status":
		b.WriteString("docker-compose ps || echo 'not running'\n")
	}
	b.WriteString(fmt.Sprintf("echo 'Harbor %s 操作完成\n", req.Action))
	return b.String()
}

// handleHarborProject 处理 POST /api/v1/harbor/project：项目管理。
func (s *Server) handleHarborProject(w http.ResponseWriter, r *http.Request) {
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
	var req harborProjectReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedHarborResourceActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	if req.Action != "list" && !harborProjectRe.MatchString(req.Project) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid project name"})
		return
	}

	script := buildHarborProjectScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "harbor_project_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "harbor.project", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "harbor_project_" + req.Action, Target: req.Project,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildHarborProjectScript(req harborProjectReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# Harbor 项目管理（Harbor REST API，凭证从环境变量 HARBOR_ADMIN_USER / HARBOR_ADMIN_PASSWORD 读取）\n")
	b.WriteString("HARBOR_URL=${HARBOR_URL:-https://localhost}\n")
	b.WriteString("AUTH=\"$HARBOR_ADMIN_USER:$HARBOR_ADMIN_PASSWORD\"\n")
	switch req.Action {
	case "create":
		b.WriteString(fmt.Sprintf("curl -s -u \"$AUTH\" -X POST \"$HARBOR_URL/api/v2.0/projects\" -H 'Content-Type: application/json' -d '{\"project_name\": \"%s\", \"public\": false}' || echo '创建失败: %s'\n", req.Project, req.Project))
	case "delete":
		b.WriteString(fmt.Sprintf("curl -s -u \"$AUTH\" -X DELETE \"$HARBOR_URL/api/v2.0/projects/%s\" || echo '删除失败: %s'\n", req.Project, req.Project))
	case "list":
		b.WriteString("curl -s -u \"$AUTH\" \"$HARBOR_URL/api/v2.0/projects\"\n")
	}
	b.WriteString(fmt.Sprintf("echo 'Harbor 项目 %s 操作完成\n", req.Action))
	return b.String()
}

// handleHarborRepository 处理 POST /api/v1/harbor/repository：仓库管理。
func (s *Server) handleHarborRepository(w http.ResponseWriter, r *http.Request) {
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
	var req harborRepositoryReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedHarborResourceActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (list/delete)"})
		return
	}
	if req.Project == "" || !harborProjectRe.MatchString(req.Project) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid project name"})
		return
	}
	if req.Action == "delete" && (req.Repository == "" || !harborRepositoryRe.MatchString(req.Repository)) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid repository name"})
		return
	}

	script := buildHarborRepositoryScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "harbor_repository_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "harbor.repository", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "harbor_repository_" + req.Action, Target: req.Project + "/" + req.Repository,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildHarborRepositoryScript(req harborRepositoryReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# Harbor 仓库管理（Harbor REST API）\n")
	b.WriteString("HARBOR_URL=${HARBOR_URL:-https://localhost}\n")
	b.WriteString("AUTH=\"$HARBOR_ADMIN_USER:$HARBOR_ADMIN_PASSWORD\"\n")
	switch req.Action {
	case "list":
		b.WriteString(fmt.Sprintf("curl -s -u \"$AUTH\" \"$HARBOR_URL/api/v2.0/projects/%s/repositories\"\n", req.Project))
	case "delete":
		b.WriteString(fmt.Sprintf("curl -s -u \"$AUTH\" -X DELETE \"$HARBOR_URL/api/v2.0/projects/%s/repositories/%s\" || echo '删除失败'\n", req.Project, req.Repository))
	}
	b.WriteString(fmt.Sprintf("echo 'Harbor 仓库 %s 操作完成\n", req.Action))
	return b.String()
}

// handleHarborArtifact 处理 POST /api/v1/harbor/artifact：镜像 artifact 管理。
func (s *Server) handleHarborArtifact(w http.ResponseWriter, r *http.Request) {
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
	var req harborArtifactReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !allowedHarborResourceActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (list/delete)"})
		return
	}
	if req.Project == "" || !harborProjectRe.MatchString(req.Project) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid project name"})
		return
	}
	if req.Repository == "" || !harborRepositoryRe.MatchString(req.Repository) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid repository name"})
		return
	}
	if req.Action == "delete" && (req.Reference == "" || !harborArtifactRe.MatchString(req.Reference)) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid artifact reference"})
		return
	}

	script := buildHarborArtifactScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "harbor_artifact_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "harbor.artifact", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "harbor_artifact_" + req.Action, Target: req.Project + "/" + req.Repository + "@" + req.Reference,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildHarborArtifactScript(req harborArtifactReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# Harbor artifact 管理（Harbor REST API）\n")
	b.WriteString("HARBOR_URL=${HARBOR_URL:-https://localhost}\n")
	b.WriteString("AUTH=\"$HARBOR_ADMIN_USER:$HARBOR_ADMIN_PASSWORD\"\n")
	switch req.Action {
	case "list":
		b.WriteString(fmt.Sprintf("curl -s -u \"$AUTH\" \"$HARBOR_URL/api/v2.0/projects/%s/repositories/%s/artifacts\"\n", req.Project, req.Repository))
	case "delete":
		b.WriteString(fmt.Sprintf("curl -s -u \"$AUTH\" -X DELETE \"$HARBOR_URL/api/v2.0/projects/%s/repositories/%s/artifacts/%s\" || echo '删除失败'\n", req.Project, req.Repository, req.Reference))
	}
	b.WriteString(fmt.Sprintf("echo 'Harbor artifact %s 操作完成\n", req.Action))
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerHarborRoutes 注册 Harbor 运维路由。
func (s *Server) registerHarborRoutes(mux *http.ServeMux) {
	// 固定路径优先
	mux.HandleFunc("/api/v1/harbor/project", s.handleHarborProject)
	mux.HandleFunc("/api/v1/harbor/repository", s.handleHarborRepository)
	mux.HandleFunc("/api/v1/harbor/artifact", s.handleHarborArtifact)
	// 泛路由：GET /api/v1/harbor/{deviceID} / POST /api/v1/harbor/{action} / POST /api/v1/harbor/deploy
	mux.HandleFunc("/api/v1/harbor/deploy", s.handleHarborDeploy)
	mux.HandleFunc("/api/v1/harbor/", s.handleHarborRouting)
}

func (s *Server) handleHarborRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/harbor/")
	segments := strings.Split(path, "/")
	switch len(segments) {
	case 1:
		if segments[0] == "" {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		// POST /api/v1/harbor/{action}：启停/状态（start/stop/restart/status/undeploy）。
		if r.Method == http.MethodPost && allowedHarborActions[segments[0]] {
			s.handleHarborAction(w, r, segments[0])
			return
		}
		// GET /api/v1/harbor/{deviceID}
		s.handleHarbor(w, r, segments[0])
	case 2:
		// 兼容带尾斜杠的 /api/v1/harbor/{action}/ 形式。
		if segments[0] != "" && segments[1] == "" && allowedHarborActions[segments[0]] {
			s.handleHarborAction(w, r, segments[0])
		} else {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}
