package controlplane

// audit_query.go 实现 Phase 3 审计日志查询 HTTP handler。
//
// API 端点：
//   - GET /api/v1/audit/events  查询审计事件（支持 action/user/from/to/limit 查询参数过滤）
//   - GET /api/v1/audit/export  导出审计日志（JSON 格式）
//
// 设计要点：
//   - 用 s.requireTenantContext(w, r) 提取租户；
//   - 用现有 AuditStore（s.store.QueryAudits）查询审计事件；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 鉴权：需 audit:read 权限。
//   - from/to 用 RFC3339 解析；limit 默认 100，上限 1000 防滥用。

import (
	"net/http"
	"strconv"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/proto"
)

// 审计查询限制常量（L7 魔法数提取）。
const (
	auditDefaultLimit  = 100   // 默认返回条数
	auditMaxLimit      = 1000  // 最大返回条数（防滥用）
	exportDefaultLimit = 1000  // 导出默认条数
	exportMaxLimit     = 10000 // 导出最大条数
)

// handleAuditEvents 处理 GET /api/v1/audit/events：查询审计事件。
//
// 查询参数：
//   - action：按动作过滤（空=不限）
//   - user：按用户过滤（空=不限，匹配 Actor 字段）
//   - from：起始时间（RFC3339，空=不限）
//   - to：结束时间（RFC3339，空=不限）
//   - limit：返回上限（默认 100，上限 1000）
func (s *Server) handleAuditEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "audit:read"); !ok {
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
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	q := r.URL.Query()
	action := q.Get("action")
	user := q.Get("user")
	from := q.Get("from")
	to := q.Get("to")
	limit := auditDefaultLimit
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > auditMaxLimit {
		limit = auditMaxLimit
	}
	var since, until time.Time
	if from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid 'from' time (use RFC3339): " + err.Error()})
			return
		}
		since = t
	}
	if to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid 'to' time (use RFC3339): " + err.Error()})
			return
		}
		until = t
	}
	events := s.store.QueryAudits(actx.TenantID, action, since, until, limit)
	// 按 user 过滤（QueryAudits 不支持 user 维度，内存过滤）。
	if user != "" {
		filtered := make([]*proto.AuditEvent, 0, len(events))
		for _, e := range events {
			if e.UserID == user {
				filtered = append(filtered, e)
			}
		}
		events = filtered
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
}

// handleAuditExport 处理 GET /api/v1/audit/export：导出审计日志（JSON 格式）。
//
// 查询参数同 /api/v1/audit/events，但返回纯 JSON 数组（非 {events:[]} 包装），
// 便于外部工具直接消费。limit 默认 1000，上限 10000。
func (s *Server) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "audit:read"); !ok {
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
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	q := r.URL.Query()
	action := q.Get("action")
	from := q.Get("from")
	to := q.Get("to")
	limit := exportDefaultLimit
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > exportMaxLimit {
		limit = exportMaxLimit
	}
	var since, until time.Time
	if from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid 'from' time (use RFC3339): " + err.Error()})
			return
		}
		since = t
	}
	if to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid 'to' time (use RFC3339): " + err.Error()})
			return
		}
		until = t
	}
	events := s.store.QueryAudits(actx.TenantID, action, since, until, limit)
	paginate.WriteJSON(w, http.StatusOK, events)
}
