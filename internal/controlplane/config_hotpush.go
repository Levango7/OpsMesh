package controlplane

// config_hotpush.go 实现 Phase 2 配置热推送 HTTP handler。
//
// API 端点：
//   - POST /api/v1/config/hotpush   热推送配置到指定设备
//   - POST /api/v1/config/canary    灰度配置发布
//   - GET  /api/v1/config/versions  查询配置版本历史
//
// 设计要点：
//   - 热推送通过 TaskStore.CreateTask 下发 file 类型任务（写配置文件到目标路径）；
//   - 配置版本通过 ConfigStore.SetConfig/ConfigHistory 管理；
//   - 鉴权：需 config:read/config:write 权限（复用 cmdb 领域权限）。

import (
	"opsmesh/internal/controlplane/paginate"
	"net/http"
	"strconv"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// handleConfigHotpush 处理 POST /api/v1/config/hotpush：热推送配置到指定设备。
// 请求体：{agentID, key, value, path, format, description}
// 行为：先 SetConfig 保存配置版本，再 CreateTask 下发 file 类型任务写配置文件。
func (s *Server) handleConfigHotpush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "cmdb:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	var body struct {
		AgentID     string `json:"agentID"`
		Key         string `json:"key"`
		Value       string `json:"value"`
		Path        string `json:"path"`
		Format      string `json:"format"`
		Description string `json:"description"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.AgentID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentID is required"})
		return
	}
	if body.Key == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
		return
	}
	if body.Path == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	// 1. 保存配置版本到 ConfigStore
	item := &store.ConfigItem{
		Key:         body.Key,
		Value:       body.Value,
		Format:      body.Format,
		Description: body.Description,
		TenantID:    actx.TenantID,
		UpdatedBy:   caller.ID,
	}
	if item.Format == "" {
		item.Format = "text"
	}
	saved := s.store.SetConfig(item)

	// 2. 下发 file 类型任务（写配置文件到目标路径）
	task := &proto.Task{
		AgentID:  body.AgentID,
		TenantID: actx.TenantID,
		Type:     proto.TaskTypeFile,
		Content:  body.Value,
		Path:     body.Path,
	}
	created := s.store.CreateTask(task)

	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "config_hotpush", Target: body.Key, Detail: sanitizeAuditDetail("agent=" + body.AgentID + " path=" + body.Path),
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"configKey":     body.Key,
		"configVersion": saved.Version,
		"taskID":        created.TaskID,
		"agentID":       body.AgentID,
		"status":        "pushed",
	})
}

// handleConfigCanary 处理 POST /api/v1/config/canary：灰度配置发布。
// 请求体：{agentIDs: [], key, value, path, format, percentage}
// 行为：保存配置版本，向指定设备批量下发 file 类型任务。
func (s *Server) handleConfigCanary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "cmdb:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	var body struct {
		AgentIDs   []string `json:"agentIDs"`
		Key        string   `json:"key"`
		Value      string   `json:"value"`
		Path       string   `json:"path"`
		Format     string   `json:"format"`
		Percentage int      `json:"percentage"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if len(body.AgentIDs) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "agentIDs is required (non-empty)"})
		return
	}
	if body.Key == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
		return
	}
	if body.Path == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	if body.Percentage < 0 || body.Percentage > 100 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "percentage must be between 0 and 100"})
		return
	}
	// 1. 保存配置版本
	item := &store.ConfigItem{
		Key:       body.Key,
		Value:     body.Value,
		Format:    body.Format,
		TenantID:  actx.TenantID,
		UpdatedBy: caller.ID,
	}
	if item.Format == "" {
		item.Format = "text"
	}
	saved := s.store.SetConfig(item)

	// 2. 批量下发 file 类型任务
	tasks := make([]map[string]string, 0, len(body.AgentIDs))
	for _, agentID := range body.AgentIDs {
		task := &proto.Task{
			AgentID:  agentID,
			TenantID: actx.TenantID,
			Type:     proto.TaskTypeFile,
			Content:  body.Value,
			Path:     body.Path,
		}
		created := s.store.CreateTask(task)
		tasks = append(tasks, map[string]string{
			"agentID": agentID,
			"taskID":  created.TaskID,
		})
	}

	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "config_canary", Target: body.Key, Detail: sanitizeAuditDetail("agents=" + strconv.Itoa(len(body.AgentIDs)) + " pct=" + strconv.Itoa(body.Percentage)),
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"configKey":     body.Key,
		"configVersion": saved.Version,
		"percentage":    body.Percentage,
		"tasks":         tasks,
		"status":        "canary_pushed",
	})
}

// handleConfigVersions 处理 GET /api/v1/config/versions：查询配置版本历史。
// 查询参数：?key=xxx（必填）
func (s *Server) handleConfigVersions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "cmdb:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "key query parameter is required"})
		return
	}
	history := s.store.ConfigHistory(actx.TenantID, key)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"key":      key,
		"versions": history,
	})
}
