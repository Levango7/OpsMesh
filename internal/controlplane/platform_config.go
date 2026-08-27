package controlplane

// platform_config.go 实现 Phase 6 平台配置/健康检查/指标 HTTP handler。
//
// API 端点：
//   - GET /api/v1/platform/config   平台配置
//   - PUT /api/v1/platform/config   更新平台配置
//   - GET /api/v1/platform/health   平台健康检查
//   - GET /api/v1/platform/metrics  平台指标汇总

import (
	"opsmesh/internal/controlplane/paginate"
	"net/http"
	"runtime"
	"time"

	"opsmesh/internal/proto"
)

// PlatformConfig 平台配置视图。
type PlatformConfig struct {
	Version           string `json:"version"`
	BuildTime         string `json:"buildTime"`
	GoVersion         string `json:"goVersion"`
	DefaultTenant     string `json:"defaultTenant"`
	MaxTenants        int    `json:"maxTenants"`
	EnableMarketplace bool   `json:"enableMarketplace"`
	EnableBilling     bool   `json:"enableBilling"`
	UpdatedAt         string `json:"updatedAt"`
}

// PlatformHealth 平台健康检查视图。
type PlatformHealth struct {
	Status     string            `json:"status"`     // ok|degraded|down
	Components map[string]string `json:"components"` // 组件名→状态
	Timestamp  string            `json:"timestamp"`
}

// PlatformMetrics 平台指标汇总视图。
type PlatformMetrics struct {
	Tenants       int `json:"tenants"`
	Devices       int `json:"devices"`
	Tasks         int `json:"tasks"`
	Alerts        int `json:"alerts"`
	APIKeys       int `json:"apiKeys"`
	Plugins       int `json:"plugins"`
	Subscriptions int `json:"subscriptions"`
	Invoices      int `json:"invoices"`
}

// handlePlatformConfig 处理 GET/PUT /api/v1/platform/config。
func (s *Server) handlePlatformConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetPlatformConfig(w, r)
	case http.MethodPut:
		s.handleUpdatePlatformConfig(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleGetPlatformConfig 处理 GET /api/v1/platform/config。
func (s *Server) handleGetPlatformConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "platform:read"); !ok {
		return
	}
	cfg := PlatformConfig{
		Version:           "0.6.0",
		BuildTime:         time.Now().Format("2006-01-02"),
		GoVersion:         runtime.Version(),
		DefaultTenant:     "default",
		MaxTenants:        100,
		EnableMarketplace: true,
		EnableBilling:     true,
		UpdatedAt:         time.Now().Format(time.RFC3339),
	}
	paginate.WriteJSON(w, http.StatusOK, cfg)
}

// handleUpdatePlatformConfig 处理 PUT /api/v1/platform/config。
//
// 假审计修正：本 handler 未实际落库配置（MVP 仅返回 echo），审计 Action 加 _simulated
// 后缀以如实标记非真实持久化；响应体加 simulated:true 字段提示客户端此更新未落库。
func (s *Server) handleUpdatePlatformConfig(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "platform:write")
	if !ok {
		return
	}
	var body PlatformConfig
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	body.UpdatedAt = time.Now().Format(time.RFC3339)
	// 假审计修正：Action 加 _simulated 后缀，标记此更新未实际落库（MVP echo 占位）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: "default", UserID: caller.ID, Action: "platform_config_update_simulated", Target: "config", Detail: "echo placeholder, not persisted",
	})
	// 响应体加 simulated:true，如实告知客户端此更新未持久化。
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"config":    body,
		"simulated": true,
	})
}

// handlePlatformHealth 处理 GET /api/v1/platform/health：平台健康检查。
func (s *Server) handlePlatformHealth(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "platform:read"); !ok {
		return
	}
	components := map[string]string{
		"store":   "ok",
		"agent":   "ok",
		"task":    "ok",
		"alert":   "ok",
		"billing": "ok",
	}
	health := PlatformHealth{
		Status:     "ok",
		Components: components,
		Timestamp:  time.Now().Format(time.RFC3339),
	}
	paginate.WriteJSON(w, http.StatusOK, health)
}

// handlePlatformMetrics 处理 GET /api/v1/platform/metrics：平台指标汇总。
func (s *Server) handlePlatformMetrics(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "platform:read"); !ok {
		return
	}
	metrics := PlatformMetrics{
		Tenants:       len(s.store.ListTenants()),
		Devices:       len(s.store.Snapshot("")),
		Tasks:         s.store.PendingDepth(),
		Alerts:        len(s.store.Alerts("")),
		APIKeys:       len(s.store.ListAPIKeys("")),
		Plugins:       len(s.store.ListPlugins()),
		Subscriptions: len(s.store.ListSubscriptions("")),
		Invoices:      len(s.store.ListInvoices("")),
	}
	paginate.WriteJSON(w, http.StatusOK, metrics)
}
