// server_audits.go 审计相关 HTTP handler。
//
// 从 server.go 拆分而来（按路由域拆分巨型 server.go）。
// 仅包含审计检索端点 handleAudits，逻辑未做任何修改。
package controlplane

import (
	"net/http"
	"strconv"
	"time"
)

// handleAudits 处理 GET /api/v1/audits：按租户/动作/时间窗检索审计事件（审计可查；等保三级留痕必须可检索）。
// 查询参数：tenant（requireAuth 时强制取自身租户）、action、from/to（RFC3339）、limit（默认 100，上限 1000）。
func (s *Server) handleAudits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "audit:read"); !ok {
		return
	}
	q := r.URL.Query()
	tenant := q.Get("tenant")
	if s.requireAuth {
		tenant = actx.TenantID // 强制租户隔离，忽略客户端伪造
	}
	action := q.Get("action")
	var since, until time.Time
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			until = t
		}
	}
	limit := 100
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 1000 {
		limit = 1000
	}
	evs := s.store.QueryAudits(tenant, action, since, until, limit)
	writeJSON(w, http.StatusOK, evs)
}
