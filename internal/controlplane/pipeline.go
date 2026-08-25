package controlplane

// pipeline.go 实现 Phase 2 CI/CD 流水线 HTTP handler。
//
// API 端点：
//   - GET    /api/v1/pipeline/templates           列出模板
//   - POST   /api/v1/pipeline/templates           创建模板
//   - GET    /api/v1/pipeline/templates/{id}      获取模板详情
//   - PUT    /api/v1/pipeline/templates/{id}      更新模板
//   - DELETE /api/v1/pipeline/templates/{id}      删除模板
//   - POST   /api/v1/pipeline/templates/{id}/run  触发运行
//   - GET    /api/v1/pipeline/runs                列出运行记录
//   - GET    /api/v1/pipeline/runs/{id}           获取运行详情
//
// 设计要点（与 traffic.go 风格一致）：
//   - 用 s.k8sTenantFromRequest(r) 提取租户；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 用 decodeJSONBody 解析请求体；
//   - 鉴权：需 pipeline:read/pipeline:write 权限。

import (
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// handlePipelineTemplates 统一处理 /api/v1/pipeline/templates：
//   - GET：列出模板
//   - POST：创建模板
func (s *Server) handlePipelineTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListPipelineTemplates(w, r)
	case http.MethodPost:
		s.handleCreatePipelineTemplate(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListPipelineTemplates 处理 GET /api/v1/pipeline/templates：列出模板。
func (s *Server) handleListPipelineTemplates(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "pipeline:read"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	templates := s.store.ListTemplates(tenant)
	writeJSON(w, http.StatusOK, map[string]interface{}{"templates": templates})
}

// handleCreatePipelineTemplate 处理 POST /api/v1/pipeline/templates：创建模板。
func (s *Server) handleCreatePipelineTemplate(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "pipeline:write")
	if !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	var body store.PipelineTemplate
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	created := s.store.CreateTemplate(tenant, &body)
	if created == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create template failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "pipeline_template_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	writeJSON(w, http.StatusCreated, created)
}

// handlePipelineTemplate 分派 /api/v1/pipeline/templates/{id} 子路径：
//   - GET    /api/v1/pipeline/templates/{id}        获取模板详情
//   - PUT    /api/v1/pipeline/templates/{id}        更新模板
//   - DELETE /api/v1/pipeline/templates/{id}        删除模板
//   - POST   /api/v1/pipeline/templates/{id}/run    触发运行
func (s *Server) handlePipelineTemplate(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/pipeline/templates/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template id required"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetPipelineTemplate(w, r, id)
		case http.MethodPut:
			s.handleUpdatePipelineTemplate(w, r, id)
		case http.MethodDelete:
			s.handleDeletePipelineTemplate(w, r, id)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "run":
		s.handleRunPipelineTemplate(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetPipelineTemplate 处理 GET /api/v1/pipeline/templates/{id}：获取详情。
func (s *Server) handleGetPipelineTemplate(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "pipeline:read"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	t, ok := s.store.GetTemplate(tenant, id)
	if !ok || t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleUpdatePipelineTemplate 处理 PUT /api/v1/pipeline/templates/{id}：更新模板。
// PipelineStore 无 UpdateTemplate 方法，用 Delete + Create 模拟（保留原 ID）。
func (s *Server) handleUpdatePipelineTemplate(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "pipeline:write")
	if !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	existing, ok := s.store.GetTemplate(tenant, id)
	if !ok || existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	var body store.PipelineTemplate
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	body.ID = id
	body.TenantID = tenant
	body.CreatedAt = existing.CreatedAt
	if !s.store.DeleteTemplate(tenant, id) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "update template failed (delete step)"})
		return
	}
	updated := s.store.CreateTemplate(tenant, &body)
	if updated == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "update template failed (create step)"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "pipeline_template_update", Target: id, Detail: sanitizeAuditDetail("name=" + updated.Name),
	})
	writeJSON(w, http.StatusOK, updated)
}

// handleDeletePipelineTemplate 处理 DELETE /api/v1/pipeline/templates/{id}：删除模板。
func (s *Server) handleDeletePipelineTemplate(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "pipeline:write")
	if !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	if !s.store.DeleteTemplate(tenant, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "pipeline_template_delete", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleRunPipelineTemplate 处理 POST /api/v1/pipeline/templates/{id}/run：触发运行。
func (s *Server) handleRunPipelineTemplate(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "pipeline:write")
	if !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	tpl, ok := s.store.GetTemplate(tenant, id)
	if !ok || tpl == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}
	var body struct {
		Parameters map[string]string `json:"parameters"`
	}
	// 请求体可选（可无参数触发）
	_ = decodeJSONBody(w, r, &body)
	now := time.Now()
	run := &store.PipelineRun{
		TemplateID:   tpl.ID,
		TemplateName: tpl.Name,
		Status:       "pending",
		Parameters:   body.Parameters,
		StartedAt:    &now,
	}
	created := s.store.CreateRun(tenant, run)
	if created == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create run failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "pipeline_run", Target: created.ID, Detail: sanitizeAuditDetail("template=" + tpl.Name),
	})
	writeJSON(w, http.StatusCreated, created)
}

// handlePipelineRuns 统一处理 /api/v1/pipeline/runs：
//   - GET：列出运行记录（支持 ?templateID= 过滤）
func (s *Server) handlePipelineRuns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListPipelineRuns(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListPipelineRuns 处理 GET /api/v1/pipeline/runs：列出运行记录。
func (s *Server) handleListPipelineRuns(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "pipeline:read"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	templateID := r.URL.Query().Get("templateID")
	runs := s.store.ListRuns(tenant, templateID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"runs": runs})
}

// handlePipelineRun 处理 /api/v1/pipeline/runs/{id}：获取运行详情。
func (s *Server) handlePipelineRun(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/pipeline/runs/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "run id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "run id required"})
		return
	}
	if len(parts) > 1 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + parts[1]})
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "pipeline:read"); !ok {
		return
	}
	tenant := s.k8sTenantFromRequest(r)
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	run, ok := s.store.GetRun(tenant, id)
	if !ok || run == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
		return
	}
	writeJSON(w, http.StatusOK, run)
}
