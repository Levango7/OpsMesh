// system_services.go 实现 systemctl 服务管理与 journal 日志查询。
//
// 设计（延续 system_ops.go 的"查询走 API、执行走 shell"原则）：
//   - 服务状态查询：已由 agent collectServices 采集为结构化数据（DeviceMetrics.Services），
//     前端可直接读取；本文件补服务操作（start/stop/restart/reload/enable/disable）。
//   - 服务操作：POST /api/v1/system/services → shell `systemctl <action> <service>` 下发到 agent；
//     action 走白名单（杜绝任意命令），service 名走 safeNameRe 正则校验。
//   - journal 日志查询：journalctl 输出为文本且可能很大，采用"下发查询任务 + 轮询结果"模式
//     （与网络诊断一致）：POST /api/v1/system/journal 下发，GET /api/v1/system/journal/{taskId} 查询结果。
//
// API 端点：
//   - POST /api/v1/system/services          {agentID, service, action} → Task（start/stop/restart/reload/enable/disable）
//   - POST /api/v1/system/journal           {agentID, unit, lines, level, since} → Task（journalctl 查询）
//   - GET  /api/v1/system/journal/{taskId}  查询 journal 任务结果（复用 /api/v1/tasks/{id}/result）
package controlplane

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// 服务操作白名单（systemctl 子命令）。仅允许这些动作，杜绝任意命令。
var allowedServiceActions = map[string]bool{
	"start":     true,
	"stop":      true,
	"restart":   true,
	"reload":    true,
	"enable":    true,
	"disable":   true,
	"status":    true,
	"is-active": true,
}

// handleSystemServices 处理 POST /api/v1/system/services：系统服务操作。
// Body: {agentID, service, action}
// 安全：service 名走 safeNameRe 校验；action 走白名单。
func (s *Server) handleSystemServices(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		AgentID string `json:"agentID"`
		Service string `json:"service"`
		Action  string `json:"action"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.AgentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}
	if !safeNameRe.MatchString(req.Service) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid service name (allowed: a-zA-Z0-9_.-)"})
		return
	}
	if !allowedServiceActions[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid action (allowed: start/stop/restart/reload/enable/disable/status)"})
		return
	}

	// 构造脚本：systemctl <action> <service>；status/is-active 用 --no-pager 输出供轮询查看。
	var script string
	if req.Action == "status" || req.Action == "is-active" {
		script = fmt.Sprintf("systemctl --no-pager %s %q\n", req.Action, req.Service)
	} else {
		script = fmt.Sprintf("systemctl %s %q && systemctl is-active %q\n", req.Action, req.Service, req.Service)
	}

	task, err := s.createSystemTask(actx.TenantID, req.AgentID, script, "service_"+req.Action)
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "service_" + req.Action, Target: req.Service,
		Detail: sanitizeAuditDetail("agentID=" + req.AgentID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

// handleSystemJournal 处理 journal 日志查询：
//   - POST /api/v1/system/journal：下发 journalctl 查询任务
//   - GET  /api/v1/system/journal/{taskId}：返回任务查询引导（结果经 /api/v1/tasks/{id}/result）
//
// POST Body: {agentID, unit, lines, level, since}
//   - unit：systemd 单元名（可选，空= 全部系统日志）
//   - lines：返回行数（默认 100，上限 2000）
//   - level：日志级别过滤（err/warning/info，可选）
//   - since：起始时间（如 "1 hour ago"/"2024-01-01"，可选，走正则校验）
func (s *Server) handleSystemJournal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:read"); !ok {
		return
	}
	var req struct {
		AgentID string `json:"agentID"`
		Unit    string `json:"unit"`
		Lines   int    `json:"lines"`
		Level   string `json:"level"`
		Since   string `json:"since"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.AgentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}
	// 行数 clamp。
	lines := req.Lines
	if lines <= 0 {
		lines = 100
	}
	if lines > 2000 {
		lines = 2000
	}
	// unit 校验（可选）。
	if req.Unit != "" && !safeNameRe.MatchString(req.Unit) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid unit name"})
		return
	}
	// level 白名单。
	if req.Level != "" && !safeJournalLevels[req.Level] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid level (allowed: err/warning/info/debug)"})
		return
	}
	// since 校验：仅允许字母数字空格冒号横杠点（如 "1 hour ago"/"2024-01-01 10:00:00"）。
	if req.Since != "" && !safeJournalSinceRe.MatchString(req.Since) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid since format"})
		return
	}

	args := []string{"--no-pager", "-n", fmt.Sprintf("%d", lines), "-o", "short-iso"}
	if req.Unit != "" {
		args = append(args, "-u", req.Unit)
	}
	if req.Level != "" {
		args = append(args, "-p", req.Level)
	}
	if req.Since != "" {
		args = append(args, "--since", fmt.Sprintf("%q", req.Since))
	}
	script := "journalctl " + strings.Join(args, " ") + "\n"

	task, err := s.createSystemTask(actx.TenantID, req.AgentID, script, "journal_query")
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "journal_query", Target: req.Unit,
		Detail: sanitizeAuditDetail(fmt.Sprintf("agentID=%s lines=%d level=%s", req.AgentID, lines, req.Level)),
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"taskID":    task.TaskID,
		"status":    "dispatched",
		"resultURL": "/api/v1/tasks/" + task.TaskID + "/result",
	})
}

// safeJournalLevels journal 日志级别白名单（journalctl -p 值）。
var safeJournalLevels = map[string]bool{
	"emerg": true, "alert": true, "crit": true, "err": true,
	"warning": true, "notice": true, "info": true, "debug": true,
}
