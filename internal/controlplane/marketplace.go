package controlplane

// marketplace.go 实现 Phase 6 插件市场 HTTP handler（CRUD + 安装/卸载 + 启停）。
//
// API 端点：
//   - GET    /api/v1/marketplace/plugins        列出插件
//   - POST   /api/v1/marketplace/plugins        注册插件
//   - GET    /api/v1/marketplace/plugins/{id}   插件详情
//   - DELETE /api/v1/marketplace/plugins/{id}   删除插件
//   - POST   /api/v1/marketplace/plugins/{id}/install    安装插件
//   - POST   /api/v1/marketplace/plugins/{id}/uninstall  卸载插件
//   - POST   /api/v1/marketplace/plugins/{id}/enable     启用插件
//   - POST   /api/v1/marketplace/plugins/{id}/disable    禁用插件

import (
	"net/http"
	"net/url"
	"strings"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// allowedPluginTypes 插件类型白名单（L1 输入校验）。
// 仅允许 data/logic/integration 三类，拒绝任意其他值防止下游分发逻辑误判。
var allowedPluginTypes = map[string]bool{
	"data":        true,
	"logic":       true,
	"integration": true,
}

// handleMarketplacePlugins 统一处理 /api/v1/marketplace/plugins。
func (s *Server) handleMarketplacePlugins(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListPlugins(w, r)
	case http.MethodPost:
		s.handleCreatePlugin(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListPlugins 处理 GET /api/v1/marketplace/plugins：列出插件。
func (s *Server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "plugin:read"); !ok {
		return
	}
	plugins := s.store.ListPlugins()
	writeJSON(w, http.StatusOK, map[string]interface{}{"plugins": plugins})
}

// handleCreatePlugin 处理 POST /api/v1/marketplace/plugins：注册插件。
func (s *Server) handleCreatePlugin(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "plugin:write")
	if !ok {
		return
	}
	var body store.Plugin
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.Version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "version is required"})
		return
	}
	// L1 输入校验：pluginType 白名单 {data,logic,integration}。
	if body.Type == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type is required"})
		return
	}
	if !allowedPluginTypes[body.Type] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid type: " + body.Type + " (want data|logic|integration)"})
		return
	}
	// L1 输入校验：downloadURL 仅允许 http/https scheme（防 file:// / ftp:// 等不安全协议）。
	// 空URL允许（插件可能内嵌无外链下载）；非空时强制 scheme 白名单。
	if body.DownloadURL != "" {
		u, err := url.Parse(body.DownloadURL)
		if err != nil || u.Scheme == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid downloadURL: parse failed"})
			return
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid downloadURL scheme: " + u.Scheme + " (want http|https)"})
			return
		}
	}
	created := s.store.CreatePlugin(&body)
	if created == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create plugin failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "plugin_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	writeJSON(w, http.StatusCreated, created)
}

// handleMarketplacePluginRouting 分派 /api/v1/marketplace/plugins/{id} 子路径。
func (s *Server) handleMarketplacePluginRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/marketplace/plugins/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "plugin id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "plugin id required"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetPlugin(w, r, id)
		case http.MethodDelete:
			s.handleDeletePlugin(w, r, id)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "install":
		s.handleInstallPlugin(w, r, id)
	case "uninstall":
		s.handleUninstallPlugin(w, r, id)
	case "enable":
		s.handleEnablePlugin(w, r, id)
	case "disable":
		s.handleDisablePlugin(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetPlugin 处理 GET /api/v1/marketplace/plugins/{id}。
func (s *Server) handleGetPlugin(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "plugin:read"); !ok {
		return
	}
	p, ok := s.store.GetPlugin(id)
	if !ok || p == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "plugin not found"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleDeletePlugin 处理 DELETE /api/v1/marketplace/plugins/{id}。
func (s *Server) handleDeletePlugin(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "plugin:write")
	if !ok {
		return
	}
	if !s.store.DeletePlugin(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "plugin not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "plugin_delete", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleInstallPlugin 处理 POST /api/v1/marketplace/plugins/{id}/install。
func (s *Server) handleInstallPlugin(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "plugin:write")
	if !ok {
		return
	}
	p, ok := s.store.GetPlugin(id)
	if !ok || p == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "plugin not found"})
		return
	}
	if p.Installed {
		writeJSON(w, http.StatusOK, p)
		return
	}
	p.Installed = true
	p.Enabled = true
	updated, ok := s.store.UpdatePlugin(p)
	if !ok || updated == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "install plugin failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "plugin_install", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, updated)
}

// handleUninstallPlugin 处理 POST /api/v1/marketplace/plugins/{id}/uninstall。
func (s *Server) handleUninstallPlugin(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "plugin:write")
	if !ok {
		return
	}
	p, ok := s.store.GetPlugin(id)
	if !ok || p == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "plugin not found"})
		return
	}
	if !p.Installed {
		writeJSON(w, http.StatusOK, p)
		return
	}
	p.Installed = false
	p.Enabled = false
	updated, ok := s.store.UpdatePlugin(p)
	if !ok || updated == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "uninstall plugin failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "plugin_uninstall", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, updated)
}

// handleEnablePlugin 处理 POST /api/v1/marketplace/plugins/{id}/enable。
func (s *Server) handleEnablePlugin(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "plugin:write")
	if !ok {
		return
	}
	p, ok := s.store.GetPlugin(id)
	if !ok || p == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "plugin not found"})
		return
	}
	p.Enabled = true
	updated, ok := s.store.UpdatePlugin(p)
	if !ok || updated == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "enable plugin failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "plugin_enable", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, updated)
}

// handleDisablePlugin 处理 POST /api/v1/marketplace/plugins/{id}/disable。
func (s *Server) handleDisablePlugin(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "plugin:write")
	if !ok {
		return
	}
	p, ok := s.store.GetPlugin(id)
	if !ok || p == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "plugin not found"})
		return
	}
	p.Enabled = false
	updated, ok := s.store.UpdatePlugin(p)
	if !ok || updated == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "disable plugin failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "plugin_disable", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, updated)
}
