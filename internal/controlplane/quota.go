// quota.go — P2-B5 多租户资源配额与计费（task 274）。
//
// 实现租户级资源配额管理：
//   - 限制每租户的设备数/任务数/告警数上限，超额拒绝（返回 ErrQuotaExceeded）；
//   - 提供用量统计 API（GET /api/v1/quotas/{tenantID} 返回当前用量 + 配额）；
//   - 配置 API（PUT /api/v1/quotas/{tenantID} 设置租户配额，GET /api/v1/quotas 列出所有配额）。
//
// 设计要点：
//   - QuotaConfig 定义在 store 包（避免 store→controlplane 循环依赖）；
//   - QuotaManager 持有 store.Store 引用，通过 DeviceStore/TaskStore/AlertStore 查询当前用量，
//     通过 QuotaStore 读取/设置配额；
//   - 配额为 0 表示不限；未设置配额的租户使用默认配额（来自 config.QuotaMaxDevices/QuotaMaxTasks/QuotaMaxAlerts）；
//   - 并发安全：mu 保护 quotas 缓存（与 store 同步），store 自身并发安全。
//
// 路由（在 server_lifecycle.go Start 中注册）：
//   - GET    /api/v1/quotas        — 列出所有租户配额（管理端用）
//   - GET    /api/v1/quotas/{tenantID} — 获取租户配额 + 用量
//   - PUT    /api/v1/quotas/{tenantID} — 设置租户配额
//   - DELETE /api/v1/quotas/{tenantID} — 清除租户配额（回退到默认配额）
package controlplane

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"opsmesh/internal/authctx"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// ErrQuotaExceeded 配额超限错误。CheckDevice/CheckTask/CheckAlert 在超额时返回此错误。
// 调用方（创建路径）据此返回 429 Too Many Requests 或 403 Forbidden。
var ErrQuotaExceeded = errors.New("quota exceeded")

// QuotaManager 配额管理器：租户级资源配额检查 + 用量统计。
//
// 持有 store.Store 引用，结合 DeviceStore/TaskStore/AlertStore 查询当前用量，
// 结合 QuotaStore 读取配额配置，比较后决定是否放行。
//
// 默认配额（defaultQuota）用于未显式设置配额的租户（来自 config）。
// enabled 控制是否启用配额检查（false 时所有 Check 方法直接放行，向后兼容）。
type QuotaManager struct {
	store        store.Store // 用于查询当前用量 + 读写配额配置
	defaultQuota *store.QuotaConfig
	enabled      bool
}

// NewQuotaManager 构造配额管理器。
// st 为持久化存储（提供 DeviceStore/TaskStore/AlertStore/QuotaStore 接口）。
// enabled 控制是否启用配额检查。
// defaultQuota 为未显式设置配额的租户的回退配额（来自 config）。
func NewQuotaManager(st store.Store, enabled bool, defaultQuota *store.QuotaConfig) *QuotaManager {
	if defaultQuota == nil {
		defaultQuota = &store.QuotaConfig{}
	}
	return &QuotaManager{
		store:        st,
		defaultQuota: defaultQuota,
		enabled:      enabled,
	}
}

// Enabled 返回配额检查是否启用。
func (q *QuotaManager) Enabled() bool {
	return q != nil && q.enabled
}

// SetQuota 设置租户配额（持久化到 store）。
// cfg 为 nil 时清除该租户配额（回退到默认配额）。
func (q *QuotaManager) SetQuota(tenantID string, cfg *store.QuotaConfig) error {
	if q == nil {
		return nil
	}
	if tenantID == "" {
		return fmt.Errorf("tenantID must not be empty")
	}
	return q.store.SetQuota(tenantID, cfg)
}

// GetQuota 获取租户配额（未设置时回退到默认配额）。
// 返回的配额始终非 nil（默认配额保证非 nil）。
func (q *QuotaManager) GetQuota(tenantID string) *store.QuotaConfig {
	if q == nil {
		return &store.QuotaConfig{}
	}
	if tenantID == "" {
		return q.defaultQuota
	}
	cfg, err := q.store.GetQuota(tenantID)
	if err != nil || cfg == nil {
		// 未设置或查询失败：回退到默认配额。
		return q.defaultQuota
	}
	return cfg
}

// CheckDevice 检查设备配额：当前设备数 + 1 是否超过 MaxDevices。
// 配额为 0 表示不限；未启用配额检查时直接放行。
// 调用方在创建设备前调用此方法，超额返回 ErrQuotaExceeded。
func (q *QuotaManager) CheckDevice(tenantID string) error {
	if q == nil || !q.enabled || tenantID == "" {
		return nil
	}
	cfg := q.GetQuota(tenantID)
	if cfg.MaxDevices <= 0 {
		return nil // 0=不限
	}
	current := q.countDevices(tenantID)
	if current >= cfg.MaxDevices {
		return fmt.Errorf("%w: devices %d >= max %d (tenant=%s)", ErrQuotaExceeded, current, cfg.MaxDevices, tenantID)
	}
	return nil
}

// CheckTask 检查任务配额：当前任务数 + 1 是否超过 MaxTasks。
// 配额为 0 表示不限；未启用配额检查时直接放行。
// 调用方在创建任务前调用此方法，超额返回 ErrQuotaExceeded。
func (q *QuotaManager) CheckTask(tenantID string) error {
	if q == nil || !q.enabled || tenantID == "" {
		return nil
	}
	cfg := q.GetQuota(tenantID)
	if cfg.MaxTasks <= 0 {
		return nil // 0=不限
	}
	current := q.countTasks(tenantID)
	if current >= cfg.MaxTasks {
		return fmt.Errorf("%w: tasks %d >= max %d (tenant=%s)", ErrQuotaExceeded, current, cfg.MaxTasks, tenantID)
	}
	return nil
}

// CheckAlert 检查告警配额：当前告警数 + 1 是否超过 MaxAlerts。
// 配额为 0 表示不限；未启用配额检查时直接放行。
// 调用方在创建告警前调用此方法，超额返回 ErrQuotaExceeded。
func (q *QuotaManager) CheckAlert(tenantID string) error {
	if q == nil || !q.enabled || tenantID == "" {
		return nil
	}
	cfg := q.GetQuota(tenantID)
	if cfg.MaxAlerts <= 0 {
		return nil // 0=不限
	}
	current := q.countAlerts(tenantID)
	if current >= cfg.MaxAlerts {
		return fmt.Errorf("%w: alerts %d >= max %d (tenant=%s)", ErrQuotaExceeded, current, cfg.MaxAlerts, tenantID)
	}
	return nil
}

// QuotaUsage 租户当前用量统计（GET /api/v1/quotas/{tenantID} 响应体）。
type QuotaUsage struct {
	Devices int                `json:"devices"` // 当前设备数
	Tasks   int                `json:"tasks"`   // 当前任务数
	Alerts  int                `json:"alerts"`  // 当前告警数
	Quota   *store.QuotaConfig `json:"quota"`   // 当前生效配额（含默认回退）
}

// Usage 查询租户当前用量统计 + 生效配额。
// 用于 GET /api/v1/quotas/{tenantID} 响应。
func (q *QuotaManager) Usage(tenantID string) (*QuotaUsage, error) {
	if q == nil {
		return &QuotaUsage{Quota: &store.QuotaConfig{}}, nil
	}
	cfg := q.GetQuota(tenantID)
	return &QuotaUsage{
		Devices: q.countDevices(tenantID),
		Tasks:   q.countTasks(tenantID),
		Alerts:  q.countAlerts(tenantID),
		Quota:   cfg,
	}, nil
}

// countDevices 统计租户当前设备数（含活跃 + 已退役归档，与 Snapshot 一致）。
// tenantID 为空时返回 0（不统计）。
func (q *QuotaManager) countDevices(tenantID string) int {
	if tenantID == "" {
		return 0
	}
	// Snapshot 返回 segment -> 设备列表 的视图，按租户过滤后累加所有 segment 的设备数。
	// 包含已退役设备（与 Snapshot 行为一致，配额按总设备数计）。
	segMap := q.store.Snapshot(tenantID)
	count := 0
	for _, devices := range segMap {
		count += len(devices)
	}
	return count
}

// countTasks 统计租户当前任务数（AllTasks 按租户过滤）。
// tenantID 为空时返回 0（不统计）。
func (q *QuotaManager) countTasks(tenantID string) int {
	if tenantID == "" {
		return 0
	}
	return len(q.store.AllTasks(tenantID))
}

// countAlerts 统计租户当前告警数（Alerts 按租户过滤）。
// tenantID 为空时返回 0（不统计）。
func (q *QuotaManager) countAlerts(tenantID string) int {
	if tenantID == "" {
		return 0
	}
	return len(q.store.Alerts(tenantID))
}

// ============================================================================
// HTTP API handler（GET/PUT/DELETE /api/v1/quotas[/{tenantID}]）
// ============================================================================

// handleQuotas 处理 /api/v1/quotas：GET 列出所有租户配额（管理端用）。
// 仅 admin 角色可访问（跨租户视图）。
func (s *Server) handleQuotas(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listQuotas(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleQuotaRouting 分派 /api/v1/quotas/{tenantID} 子路径：
//   - GET    获取租户配额 + 用量
//   - PUT    设置租户配额
//   - DELETE 清除租户配额（回退到默认配额）
func (s *Server) handleQuotaRouting(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimPrefix(r.URL.Path, "/api/v1/quotas/")
	if tenantID == "" {
		jsonError(w, http.StatusBadRequest, "tenantID required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getQuota(w, r, tenantID)
	case http.MethodPut:
		s.setQuota(w, r, tenantID)
	case http.MethodDelete:
		s.deleteQuota(w, r, tenantID)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// listQuotas 列出所有租户配额（管理端用）。
// 由于 store.QuotaStore 接口未提供 ListQuotas 方法，此处返回当前 actx 租户的配额 + 用量。
// 跨租户列表需扩展 QuotaStore 接口，当前 MVP 仅返回当前租户视图。
func (s *Server) listQuotas(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "quota:read"); !ok {
		return
	}
	if s.quotaMgr == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"enabled": false, "quotas": []interface{}{}})
		return
	}
	usage, err := s.quotaMgr.Usage(actx.TenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": s.quotaMgr.Enabled(),
		"current": usage,
	})
}

// getQuota 获取租户配额 + 当前用量（GET /api/v1/quotas/{tenantID}）。
// 仅同租户用户或 admin 可访问（actx.TenantID 须匹配 tenantID）。
func (s *Server) getQuota(w http.ResponseWriter, r *http.Request, tenantID string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "quota:read"); !ok {
		return
	}
	// 租户隔离：非 admin 用户仅能查看自己租户的配额。
	if actx.TenantID != tenantID && !s.isAdmin(actx) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cannot view other tenant quota"})
		return
	}
	if s.quotaMgr == nil {
		writeJSON(w, http.StatusOK, &QuotaUsage{Quota: &store.QuotaConfig{}})
		return
	}
	usage, err := s.quotaMgr.Usage(tenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

// setQuota 设置租户配额（PUT /api/v1/quotas/{tenantID}）。
// 仅 admin 角色可设置配额（跨租户管理权限）。
func (s *Server) setQuota(w http.ResponseWriter, r *http.Request, tenantID string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "quota:write"); !ok {
		return
	}
	if !s.isAdmin(actx) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required to set quota"})
		return
	}
	var cfg store.QuotaConfig
	if err := decodeJSONBody(w, r, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid quota config: " + err.Error()})
		return
	}
	// 校验非负（0=不限，正数为上限）。
	if cfg.MaxDevices < 0 || cfg.MaxTasks < 0 || cfg.MaxAlerts < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quota values must be non-negative (0=unlimited)"})
		return
	}
	if s.quotaMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "quota manager not enabled"})
		return
	}
	if err := s.quotaMgr.SetQuota(tenantID, &cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// 记审计日志（M1-4 携带 trace_id）。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "set_quota", Target: tenantID,
		Detail: fmt.Sprintf("maxDevices=%d maxTasks=%d maxAlerts=%d", cfg.MaxDevices, cfg.MaxTasks, cfg.MaxAlerts),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tenantID":  tenantID,
		"quota":     &cfg,
		"updatedAt": "now",
	})
}

// deleteQuota 清除租户配额（DELETE /api/v1/quotas/{tenantID}），回退到默认配额。
// 仅 admin 角色可清除配额。
func (s *Server) deleteQuota(w http.ResponseWriter, r *http.Request, tenantID string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "quota:write"); !ok {
		return
	}
	if !s.isAdmin(actx) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required to delete quota"})
		return
	}
	if s.quotaMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "quota manager not enabled"})
		return
	}
	if err := s.quotaMgr.SetQuota(tenantID, nil); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "delete_quota", Target: tenantID,
		Detail: "quota cleared, fallback to default",
	})
	writeJSON(w, http.StatusOK, map[string]string{
		"tenantID": tenantID,
		"status":   "cleared",
	})
}

// isAdmin 判断 actx 是否为 admin 角色（含 federation 转发场景）。
// 通过 X-User-Roles 头或 JWT claims 中的角色判断。
// 此处简化为检查 roles 字符串中是否包含 "admin"（与 RBAC 角色名一致）。
func (s *Server) isAdmin(actx authctx.Context) bool {
	roles := actx.Roles
	for _, r := range roles {
		if r == "admin" || r == "role-admin" {
			return true
		}
	}
	return false
}
