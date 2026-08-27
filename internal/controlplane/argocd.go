package controlplane

// argocd.go 实现 Phase 2 ArgoCD 应用管理 HTTP handler。
//
// API 端点：
//   - GET    /api/v1/argocd/apps           列出应用
//   - POST   /api/v1/argocd/apps           创建应用
//   - GET    /api/v1/argocd/apps/{id}      获取应用详情
//   - PUT    /api/v1/argocd/apps/{id}      更新应用
//   - DELETE /api/v1/argocd/apps/{id}      删除应用
//   - POST   /api/v1/argocd/apps/{id}/sync 同步应用
//
// 设计要点（与 traffic.go 风格一致）：
//   - 用 s.requireTenantContext(w, r) 提取租户；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 用 decodeJSONBody 解析请求体；
//   - 鉴权：需 argocd:read/argocd:write 权限。

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// argoCDSyncTimeout 是 argocd app sync 命令的超时时间。
const argoCDSyncTimeout = 60 * time.Second

// handleArgoCDApps 统一处理 /api/v1/argocd/apps：
//   - GET：列出应用
//   - POST：创建应用
func (s *Server) handleArgoCDApps(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListArgoCDApps(w, r)
	case http.MethodPost:
		s.handleCreateArgoCDApp(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListArgoCDApps 处理 GET /api/v1/argocd/apps：列出应用。
func (s *Server) handleListArgoCDApps(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "argocd:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	apps := s.store.ListApps(actx.TenantID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"apps": apps})
}

// handleCreateArgoCDApp 处理 POST /api/v1/argocd/apps：创建应用。
func (s *Server) handleCreateArgoCDApp(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "argocd:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	var body store.ArgoCDApp
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	created := s.store.CreateApp(actx.TenantID, &body)
	if created == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create app failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "argocd_app_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	writeJSON(w, http.StatusCreated, created)
}

// handleArgoCDApp 分派 /api/v1/argocd/apps/{id} 子路径：
//   - GET    /api/v1/argocd/apps/{id}        获取应用详情
//   - PUT    /api/v1/argocd/apps/{id}        更新应用
//   - DELETE /api/v1/argocd/apps/{id}        删除应用
//   - POST   /api/v1/argocd/apps/{id}/sync   同步应用
func (s *Server) handleArgoCDApp(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/argocd/apps/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app id required"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetArgoCDApp(w, r, id)
		case http.MethodPut:
			s.handleUpdateArgoCDApp(w, r, id)
		case http.MethodDelete:
			s.handleDeleteArgoCDApp(w, r, id)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "sync":
		s.handleSyncArgoCDApp(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetArgoCDApp 处理 GET /api/v1/argocd/apps/{id}：获取详情。
func (s *Server) handleGetArgoCDApp(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "argocd:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	a, ok := s.store.GetApp(actx.TenantID, id)
	if !ok || a == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// handleUpdateArgoCDApp 处理 PUT /api/v1/argocd/apps/{id}：更新应用。
func (s *Server) handleUpdateArgoCDApp(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "argocd:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	var body store.ArgoCDApp
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	body.ID = id
	updated, ok := s.store.UpdateApp(actx.TenantID, &body)
	if !ok || updated == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "argocd_app_update", Target: id, Detail: sanitizeAuditDetail("name=" + updated.Name),
	})
	writeJSON(w, http.StatusOK, updated)
}

// handleDeleteArgoCDApp 处理 DELETE /api/v1/argocd/apps/{id}：删除应用。
func (s *Server) handleDeleteArgoCDApp(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "argocd:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	if !s.store.DeleteApp(actx.TenantID, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "argocd_app_delete", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleSyncArgoCDApp 处理 POST /api/v1/argocd/apps/{id}/sync：同步应用。
// 真实执行：调用 argocd CLI 执行 app sync，更新状态。
func (s *Server) handleSyncArgoCDApp(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "argocd:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	a, ok := s.store.GetApp(actx.TenantID, id)
	if !ok || a == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	// 真实同步：调用 argocd CLI。
	if err := syncArgoCDApp(a); err != nil {
		a.Status = "outofsync"
		a.HealthStatus = "unknown"
		now := time.Now()
		a.UpdatedAt = now
		s.store.UpdateApp(actx.TenantID, a)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("argocd sync failed: %v", err)})
		return
	}

	a, ok = s.store.SyncApp(actx.TenantID, id)
	if !ok || a == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "sync succeeded but failed to update status"})
		return
	}

	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "argocd_app_sync", Target: id, Detail: sanitizeAuditDetail("name=" + a.Name),
	})
	writeJSON(w, http.StatusOK, a)
}

// syncArgoCDApp 调用 argocd CLI 执行 app sync。
func syncArgoCDApp(a *store.ArgoCDApp) error {
	if a.Name == "" {
		return fmt.Errorf("app name is required")
	}
	args := []string{"app", "sync", a.Name}
	if a.Namespace != "" {
		args = append(args, "--namespace", a.Namespace)
	}
	if a.TargetRevision != "" {
		args = append(args, "--revision", a.TargetRevision)
	}
	ctx, cancel := context.WithTimeout(context.Background(), argoCDSyncTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "argocd", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("argocd sync %q failed: %v (output: %s)", a.Name, err, string(output))
	}
	return nil
}

