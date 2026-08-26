package controlplane

// network.go 实现 Phase 4 网络管理 HTTP handler（网络设备 CRUD + 拓扑 + 发现 + 指标 + 配置下发）。
//
// API 端点：
//   - GET    /api/v1/network/devices           列出网络设备
//   - POST   /api/v1/network/devices           添加网络设备
//   - GET    /api/v1/network/devices/{id}      设备详情
//   - DELETE /api/v1/network/devices/{id}      删除设备
//   - GET    /api/v1/network/devices/{id}/metrics 设备监控指标
//   - POST   /api/v1/network/devices/{id}/config  下发网络配置
//   - POST   /api/v1/network/discover          网络发现（请求体：{subnet}）
//
// 注意：/api/v1/network/topology 已由 server_network.go 注册，本文件不重复注册。
// 本文件聚焦"网络设备管理"（CRUD/指标/配置下发），与 server_network.go 的
// "网络拓扑发现/诊断/连通性"互补。
//
// 设计要点（与 traffic.go 风格一致）：
//   - 用 s.k8sTenantFromRequest(w, r) 提取租户；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 用 decodeJSONBody 解析请求体；
//   - 鉴权：需 network:read/network:write 权限。

import (
	"net/http"
	"strings"

	"opsmesh/internal/network"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// networkEngine 网络管理引擎（包级单例，无状态，线程安全）。
var networkEngine = network.NewEngine()

// handleNetworkDevices 统一处理 /api/v1/network/devices：
//   - GET：列出网络设备
//   - POST：添加网络设备
func (s *Server) handleNetworkDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListNetworkDevices(w, r)
	case http.MethodPost:
		s.handleCreateNetworkDevice(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListNetworkDevices 处理 GET /api/v1/network/devices：列出网络设备。
func (s *Server) handleListNetworkDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "network:read"); !ok {
		return
	}
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	devices := s.store.ListNetworkDevices(tenant)
	writeJSON(w, http.StatusOK, map[string]interface{}{"devices": devices})
}

// handleCreateNetworkDevice 处理 POST /api/v1/network/devices：添加网络设备。
func (s *Server) handleCreateNetworkDevice(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "network:write")
	if !ok {
		return
	}
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	var body store.NetworkDevice
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.Type != "" && !network.ValidDeviceType(network.DeviceType(body.Type)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid device type: " + body.Type})
		return
	}
	created := s.store.CreateNetworkDevice(tenant, &body)
	if created == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create network device failed"})
		return
	}
	// 审计：记录创建人（H9 写路径审计补齐，与 automation/webhook/slo 风格一致）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "network_device_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	writeJSON(w, http.StatusCreated, created)
}

// handleNetworkDeviceRouting 分派 /api/v1/network/devices/{id} 子路径：
//   - GET    /api/v1/network/devices/{id}           设备详情
//   - DELETE /api/v1/network/devices/{id}           删除设备
//   - GET    /api/v1/network/devices/{id}/metrics   设备监控指标
//   - POST   /api/v1/network/devices/{id}/config    下发网络配置
func (s *Server) handleNetworkDeviceRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/network/devices/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "device id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "device id required"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetNetworkDevice(w, r, id)
		case http.MethodDelete:
			s.handleDeleteNetworkDevice(w, r, id)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "metrics":
		s.handleNetworkDeviceMetrics(w, r, id)
	case "config":
		s.handleNetworkDeviceConfig(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetNetworkDevice 处理 GET /api/v1/network/devices/{id}：设备详情。
func (s *Server) handleGetNetworkDevice(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "network:read"); !ok {
		return
	}
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	d, ok := s.store.GetNetworkDevice(tenant, id)
	if !ok || d == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "network device not found"})
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleDeleteNetworkDevice 处理 DELETE /api/v1/network/devices/{id}：删除设备。
func (s *Server) handleDeleteNetworkDevice(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "network:write")
	if !ok {
		return
	}
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	if !s.store.DeleteNetworkDevice(tenant, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "network device not found"})
		return
	}
	// 审计：记录删除人（H9 写路径审计补齐，与 automation/webhook/slo 风格一致）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "network_device_delete", Target: id,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleNetworkDeviceMetrics 处理 GET /api/v1/network/devices/{id}/metrics：设备监控指标。
func (s *Server) handleNetworkDeviceMetrics(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "network:read"); !ok {
		return
	}
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	// 校验设备存在
	d, ok := s.store.GetNetworkDevice(tenant, id)
	if !ok || d == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "network device not found"})
		return
	}
	metrics := s.store.GetNetworkMetrics(id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"deviceID": id, "metrics": metrics})
}

// handleNetworkDeviceConfig 处理 POST /api/v1/network/devices/{id}/config：下发网络配置。
func (s *Server) handleNetworkDeviceConfig(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "network:write")
	if !ok {
		return
	}
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	var body network.ConfigRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if err := network.ValidateConfig(body.Config); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	updated, ok := s.store.UpdateNetworkConfig(tenant, id, body.Config)
	if !ok || updated == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "network device not found"})
		return
	}
	// 审计：记录配置下发人（H9 写路径审计补齐，与 automation/webhook/slo 风格一致）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "network_device_config", Target: id, Detail: sanitizeAuditDetail("config=" + body.Config),
	})
	writeJSON(w, http.StatusOK, updated)
}

// handleNetworkDiscover 处理 POST /api/v1/network/discover：网络发现。
func (s *Server) handleNetworkDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "network:write"); !ok {
		return
	}
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	var req network.DiscoverRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	result := networkEngine.Discover(req)
	if result.Error != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": result.Error})
		return
	}
	writeJSON(w, http.StatusOK, result)
}
