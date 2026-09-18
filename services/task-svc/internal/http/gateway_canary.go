// gateway_canary.go 实现 task-svc 灰度发布 HTTP API（对齐 controlplane server_batch.go L269-653）：
//   - POST /api/v1/tasks/canary          灰度发布创建
//   - GET  /api/v1/tasks/canary/{id}     灰度发布状态查询
//   - POST /api/v1/tasks/canary/{id}/advance  推进到下一阶段
//
// 设计原则：
//   - 纯增量：新增 HTTP handler，不改 gateway.go（路由注册由 team leader 统一完成）
//   - 复用现有 writeJSON/writeError/writeAuthError/extractAuth 工具函数
//   - JSON 契约：camelCase 字段名与 controlplane 对齐
//   - 租户隔离：从 extractAuth 提取租户 ID，防 body 覆盖越权
package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/services/task-svc/internal/service"
)

// RegisterCanaryRoutes 注册灰度发布路由到给定 mux。
// 路由注册由 team leader 统一完成，此函数供 main 调用。
func (g *Gateway) RegisterCanaryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/tasks/canary", g.handleCanaryCreate)
	mux.HandleFunc("/api/v1/tasks/canary/", g.handleCanaryRouting)
}

// ============================================================================
// 灰度发布 API
// ============================================================================

// handleCanaryCreate 处理 POST /api/v1/tasks/canary：灰度发布。
// 请求体: { deviceIDs: [], taskType, command, strategy: "percentage|group|label", percentage?, groups?, labels? }
// 返回: { canaryID, phases: [{phase, deviceIDs, status}] }
func (g *Gateway) handleCanaryCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	var body struct {
		DeviceIDs  []string          `json:"deviceIDs"`
		TaskType   string            `json:"taskType"`
		Command    string            `json:"command"`
		Content    string            `json:"content"`
		Path       string            `json:"path"`
		Strategy   string            `json:"strategy"`
		Percentage int               `json:"percentage"`
		Groups     []string          `json:"groups"`
		Labels     map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	// 强制使用认证租户，防 body 覆盖越权（对齐 handleCreateTask 的防御语义）。
	canary, err := g.svc.CreateCanary(r.Context(), &service.CanaryCreateRequest{
		DeviceIDs:  body.DeviceIDs,
		TaskType:   body.TaskType,
		Command:    body.Command,
		Content:    body.Content,
		Path:       body.Path,
		Strategy:   body.Strategy,
		Percentage: body.Percentage,
		Groups:     body.Groups,
		Labels:     body.Labels,
		TenantID:   actx.TenantID,
		UserID:     actx.UserID,
	})
	if err != nil {
		writeCanaryError(w, err)
		return
	}
	// 返回阶段摘要（不含每任务详情，前端按需查询）。
	phaseSummary := make([]map[string]interface{}, len(canary.Phases))
	for i, p := range canary.Phases {
		phaseSummary[i] = map[string]interface{}{
			"phase":     p.Phase,
			"deviceIDs": p.DeviceIDs,
			"status":    p.Status,
		}
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"canaryID": canary.CanaryID,
		"phases":   phaseSummary,
	})
}

// handleCanaryRouting 路由分派 /api/v1/tasks/canary/{id}[/advance]。
//
// GET  /api/v1/tasks/canary/{id}         → handleCanaryStatus
// POST /api/v1/tasks/canary/{id}/advance → handleCanaryAdvance
func (g *Gateway) handleCanaryRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/canary/")
	if idAndRest == "" {
		writeError(w, http.StatusNotFound, "canary id required")
		return
	}
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if len(parts) == 1 {
		g.handleCanaryStatus(w, r, id)
		return
	}
	switch parts[1] {
	case "advance":
		g.handleCanaryAdvance(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// handleCanaryStatus 处理 GET /api/v1/tasks/canary/{id}：灰度发布状态。
// 返回灰度发布状态 + 各阶段详情（含每设备任务实时状态）。
func (g *Gateway) handleCanaryStatus(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	canary, err := g.svc.GetCanaryStatus(r.Context(), id, actx.TenantID)
	if err != nil {
		writeCanaryError(w, err)
		return
	}
	// 构建阶段详情响应（含每设备任务状态，对齐 controlplane handleCanaryStatus）。
	phases := make([]map[string]interface{}, len(canary.Phases))
	for i, p := range canary.Phases {
		phases[i] = map[string]interface{}{
			"phase":      p.Phase,
			"deviceIDs":  p.DeviceIDs,
			"status":     p.Status,
			"tasks":      p.Tasks,
			"startedAt":  p.StartedAt,
			"finishedAt": p.FinishedAt,
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"canaryID":  canary.CanaryID,
		"strategy":  canary.Strategy,
		"taskType":  canary.TaskType,
		"command":   canary.Command,
		"createdAt": canary.CreatedAt,
		"createdBy": canary.CreatedBy,
		"phases":    phases,
	})
}

// handleCanaryAdvance 处理 POST /api/v1/tasks/canary/{id}/advance：推进到下一阶段。
// 返回: { canaryID, phase, status }
func (g *Gateway) handleCanaryAdvance(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, err := g.extractAuth(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	canary, err := g.svc.AdvanceCanary(r.Context(), id, actx.TenantID, actx.UserID)
	if err != nil {
		writeCanaryError(w, err)
		return
	}
	// 找到刚执行的阶段（最后一个 running 阶段，因为阶段顺序执行）。
	phase := 0
	status := ""
	for i := range canary.Phases {
		if canary.Phases[i].Status == "running" {
			phase = canary.Phases[i].Phase
			status = canary.Phases[i].Status
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"canaryID": canary.CanaryID,
		"phase":    phase,
		"status":   status,
	})
}

// ============================================================================
// 错误映射
// ============================================================================

// writeCanaryError 将 canary service 错误映射到 HTTP 状态码。
func writeCanaryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrCanaryStoreNotAvailable):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, service.ErrCanaryNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrCanaryNoPendingPhase):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrTaskInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
