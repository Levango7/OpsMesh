// server_audits.go 审计相关 HTTP handler。
//
// 从 server.go 拆分而来（按路由域拆分巨型 server.go）。
// 仅包含审计检索端点 handleAudits，逻辑未做任何修改。
package controlplane

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/store"
)

// handleAudits 处理 GET /api/v1/audits：按租户/动作/时间窗检索审计事件（审计可查；等保三级留痕必须可检索）。
// 查询参数：tenant（仅平台租户可指定他租户）、action、from/to（RFC3339）、limit（默认 100，上限 1000）。
//
// 租户口径（§3.8 统一，2026-10-14）：与 /api/v1/audit/events 对齐——非平台租户调用者
// 一律只看本租户（actx.TenantID 由 requireTenantContext 从令牌/头解出）；平台租户
// （default）保留跨租户查询能力（运维排障需要，与 handleListUsers 同策）。
// 旧逻辑只在 requireAuth=true 时强制 ⇒ **requireAuth=false 形态（本仓默认部署）**
// 下客户端可用 ?tenant=<他租户> 定向读、不带参则返回全租户（演练人设实测 96 条
// 跨租户审计）——"隔离交给前置网关"的前提在本形态下不成立（控制面自身仍跑 RBAC）。
func (s *Server) handleAudits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
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
	// 非平台租户：强制本租户（客户端传什么都不看）；平台租户：允许按 tenant 指定。
	if tenantOrDefault(actx.TenantID) != store.DefaultTenantID {
		tenant = actx.TenantID
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
	paginate.WriteJSON(w, http.StatusOK, evs)
}
