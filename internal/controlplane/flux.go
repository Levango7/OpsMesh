// flux.go 实现 Phase 2 Flux 应用管理 HTTP handler（GitOps 原生的第二大支柱）。
//
// API 端点：
//   - GET    /api/v1/flux/apps           列出 Flux 应用（Kustomization/HelmRelease）
//   - POST   /api/v1/flux/apps           创建 Flux 应用
//   - GET    /api/v1/flux/apps/{id}      获取应用详情
//   - PUT    /api/v1/flux/apps/{id}      更新应用
//   - DELETE /api/v1/flux/apps/{id}      删除应用
//   - POST   /api/v1/flux/apps/{id}/sync 同步应用（flux reconcile）
//
// 设计要点：
//   - 与 argocd.go 风格一致：租户上下文、统一错误格式、decodeJSONBody 解析、鉴权。
//   - 内存存储（sync.Map），重启后数据丢失，可后续迁移至 store 层。
//   - 鉴权：需 flux:read/flux:write 权限。
//   - 真实同步：调用 flux CLI 执行 flux reconcile source/kustomization/helmrelease。
package controlplane

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// FluxApp Flux 应用（Kustomization 或 HelmRelease）。
type FluxApp struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenantID"`
	Name           string    `json:"name"`
	Namespace      string    `json:"namespace"`
	Type           string    `json:"type"`           // "kustomization" | "helmrelease"
	SourceURL      string    `json:"sourceURL"`      // Git 仓库 URL
	SourcePath     string    `json:"sourcePath"`     // 仓库内路径
	TargetRevision string    `json:"targetRevision"` // "main"|"HEAD"|"1.0.0"
	ClusterURL     string    `json:"clusterURL"`     // K8s 集群 API 地址
	SyncPolicy     string    `json:"syncPolicy"`     // "manual"|"auto"
	Status         string    `json:"status"`         // "synced"|"outofsync"|"unknown"|"progressing"
	HealthStatus   string    `json:"healthStatus"`   // "healthy"|"degraded"|"missing"|"unknown"
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// fluxAppStore 内存 Flux 应用存储（按 tenantID + ID 索引）。
type fluxAppStore struct {
	mu   sync.RWMutex
	apps map[string]*FluxApp // key = tenantID + ":" + id
}

var fluxStore = &fluxAppStore{
	apps: make(map[string]*FluxApp),
}

func fluxKey(tenantID, id string) string {
	return tenantID + ":" + id
}

// nextFluxAppID 生成简单应用 ID。
func nextFluxAppID() string {
	return fmt.Sprintf("flux-%d", time.Now().UnixNano())
}

// fluxAppSyncTimeout flux reconcile 命令超时时间。
const fluxAppSyncTimeout = 60 * time.Second

// handleFluxApps 统一处理 /api/v1/flux/apps：
//   - GET：列出应用
//   - POST：创建应用
func (s *Server) handleFluxApps(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListFluxApps(w, r)
	case http.MethodPost:
		s.handleCreateFluxApp(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListFluxApps 处理 GET /api/v1/flux/apps：列出应用。
func (s *Server) handleListFluxApps(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "flux:read"); !ok {
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
	fluxStore.mu.RLock()
	apps := make([]*FluxApp, 0)
	for _, a := range fluxStore.apps {
		if a.TenantID == actx.TenantID {
			apps = append(apps, a)
		}
	}
	fluxStore.mu.RUnlock()
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"apps": apps})
}

// handleCreateFluxApp 处理 POST /api/v1/flux/apps：创建应用。
func (s *Server) handleCreateFluxApp(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "flux:write")
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
	var body FluxApp
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.Type == "" {
		body.Type = "kustomization"
	}
	if body.SyncPolicy == "" {
		body.SyncPolicy = "manual"
	}
	if body.Status == "" {
		body.Status = "unknown"
	}
	if body.HealthStatus == "" {
		body.HealthStatus = "unknown"
	}
	now := time.Now()
	app := &FluxApp{
		ID:             nextFluxAppID(),
		TenantID:       actx.TenantID,
		Name:           body.Name,
		Namespace:      body.Namespace,
		Type:           body.Type,
		SourceURL:      body.SourceURL,
		SourcePath:     body.SourcePath,
		TargetRevision: body.TargetRevision,
		ClusterURL:     body.ClusterURL,
		SyncPolicy:     body.SyncPolicy,
		Status:         body.Status,
		HealthStatus:   body.HealthStatus,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	fluxStore.mu.Lock()
	fluxStore.apps[fluxKey(actx.TenantID, app.ID)] = app
	fluxStore.mu.Unlock()
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "flux_app_create", Target: app.ID, Detail: sanitizeAuditDetail("name=" + app.Name),
	})
	paginate.WriteJSON(w, http.StatusCreated, app)
}

// handleFluxApp 分派 /api/v1/flux/apps/{id} 子路径：
//   - GET    /api/v1/flux/apps/{id}        获取应用详情
//   - PUT    /api/v1/flux/apps/{id}        更新应用
//   - DELETE /api/v1/flux/apps/{id}      删除应用
//   - POST   /api/v1/flux/apps/{id}/sync   同步应用
func (s *Server) handleFluxApp(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/flux/apps/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "app id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "app id required"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetFluxApp(w, r, id)
		case http.MethodPut:
			s.handleUpdateFluxApp(w, r, id)
		case http.MethodDelete:
			s.handleDeleteFluxApp(w, r, id)
		default:
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "sync":
		s.handleSyncFluxApp(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetFluxApp 处理 GET /api/v1/flux/apps/{id}：获取详情。
func (s *Server) handleGetFluxApp(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "flux:read"); !ok {
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
	app, ok := fluxStore.getApp(actx.TenantID, id)
	if !ok || app == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, app)
}

// handleUpdateFluxApp 处理 PUT /api/v1/flux/apps/{id}：更新应用。
func (s *Server) handleUpdateFluxApp(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "flux:write")
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
	var body FluxApp
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	body.ID = id
	now := time.Now()
	body.UpdatedAt = now
	fluxStore.mu.Lock()
	existing, ok := fluxStore.apps[fluxKey(actx.TenantID, id)]
	if !ok || existing == nil {
		fluxStore.mu.Unlock()
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	// 保留原有 CreatedAt 和 TenantID
	body.TenantID = actx.TenantID
	body.CreatedAt = existing.CreatedAt
	fluxStore.apps[fluxKey(actx.TenantID, id)] = &body
	fluxStore.mu.Unlock()
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "flux_app_update", Target: id, Detail: sanitizeAuditDetail("name=" + body.Name),
	})
	paginate.WriteJSON(w, http.StatusOK, body)
}

// handleDeleteFluxApp 处理 DELETE /api/v1/flux/apps/{id}：删除应用。
func (s *Server) handleDeleteFluxApp(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "flux:write")
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
	fluxStore.mu.Lock()
	existing, ok := fluxStore.apps[fluxKey(actx.TenantID, id)]
	if !ok || existing == nil {
		fluxStore.mu.Unlock()
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	delete(fluxStore.apps, fluxKey(actx.TenantID, id))
	fluxStore.mu.Unlock()
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "flux_app_delete", Target: id, Detail: sanitizeAuditDetail("name=" + existing.Name),
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleSyncFluxApp 处理 POST /api/v1/flux/apps/{id}/sync：同步应用。
// 真实执行：调用 flux CLI 执行 flux reconcile，更新状态。
func (s *Server) handleSyncFluxApp(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "flux:write")
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
	fluxStore.mu.Lock()
	app, ok := fluxStore.apps[fluxKey(actx.TenantID, id)]
	if !ok || app == nil {
		fluxStore.mu.Unlock()
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	appName := app.Name
	appNamespace := app.Namespace
	appType := app.Type
	fluxStore.mu.Unlock()

	// 真实同步：调用 flux CLI。
	if err := syncFluxApp(appName, appNamespace, appType); err != nil {
		fluxStore.mu.Lock()
		if a, ok := fluxStore.apps[fluxKey(actx.TenantID, id)]; ok {
			a.Status = "outofsync"
			a.HealthStatus = "unknown"
			a.UpdatedAt = time.Now()
		}
		fluxStore.mu.Unlock()
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("flux sync failed: %v", err)})
		return
	}

	fluxStore.mu.Lock()
	if a, ok := fluxStore.apps[fluxKey(actx.TenantID, id)]; ok {
		a.Status = "synced"
		a.HealthStatus = "healthy"
		a.UpdatedAt = time.Now()
		app = a
	}
	fluxStore.mu.Unlock()

	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "flux_app_sync", Target: id, Detail: sanitizeAuditDetail("name=" + app.Name),
	})
	paginate.WriteJSON(w, http.StatusOK, app)
}

// getApp 按 (tenantID, id) 获取应用。
func (s *fluxAppStore) getApp(tenantID, id string) (*FluxApp, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	app, ok := s.apps[fluxKey(tenantID, id)]
	return app, ok
}

// syncFluxApp 调用 flux CLI 执行资源协调。
func syncFluxApp(name, namespace, appType string) error {
	if name == "" {
		return fmt.Errorf("app name is required")
	}
	// G702 命令注入防护：Name/Namespace 会成为 flux CLI 的 argv，白名单校验。
	if !isSafeCLIArg(name) {
		return fmt.Errorf("invalid app name: %q", name)
	}
	if namespace != "" && !isSafeCLIArg(namespace) {
		return fmt.Errorf("invalid namespace: %q", namespace)
	}

	// Flux 子命令：flux reconcile source <type> <name> [--namespace <ns>]
	// type: kustomization 或 helmrelease
	subType := "kustomization"
	if appType == "helmrelease" {
		subType = "helmrelease"
	}
	args := []string{"reconcile", "source", subType, name}
	if namespace != "" {
		args = append(args, "--namespace", namespace)
	}
	ctx, cancel := context.WithTimeout(context.Background(), fluxAppSyncTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "flux", args...) // #nosec G702 -- args 已白名单校验
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("flux reconcile %s/%s failed: %v (output: %s)", subType, name, err, string(output))
	}
	return nil
}
