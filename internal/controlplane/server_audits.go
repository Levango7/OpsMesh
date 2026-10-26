// server_audits.go 审计相关 HTTP handler。
//
// 从 server.go 拆分而来（按路由域拆分巨型 server.go）。
// 仅包含审计检索端点 handleAudits，逻辑未做任何修改。
package controlplane

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// highRiskActions 是 Wave 5-C 审计链加固中认定的高风险操作动作集合。
// 命中此集合的动作产出的审计事件会被置 HighRisk=true，并可通过
// GET /api/v1/audit/high-risk 专项检索，哈希链校验也会将 HighRisk 计入摘要。
var highRiskActions = map[string]bool{
	"user_delete":     true,
	"group_delete":    true,
	"firewall_add":    true,
	"firewall_del":    true,
	"service_stop":    true,
	"service_start":   true,
	"service_restart": true,
	"pkg_install":     true,
	"pkg_remove":      true,
	"pkg_upgrade":     true,
	"task_create":     true,
	"approve_task":    true,
	"reject_task":     true,
	"secret_rotate":   true,
	"secret_delete":   true,
}

// isHighRiskAction 判定给定动作名是否为高风险操作。
func isHighRiskAction(action string) bool {
	return highRiskActions[action]
}

// AuditHighRisk 记录一条标记为高风险的审计事件。它是高风险操作（用户/组删除、防火墙变更、
// 服务启停重启、包安装卸载升级、任务创建与审批、密钥轮换/删除等）的统一审计入口，
// 除了落库审计事件外，还将 HighRisk 置 true，供高风险审计专项查询与链式防篡改校验使用。
func (s *Server) AuditHighRisk(ctx context.Context, tenantID, userID, action, target, detail, traceID string) {
	s.audit(ctx, &proto.AuditEvent{
		TenantID: tenantID,
		UserID:   userID,
		Action:   action,
		Target:   target,
		Detail:   detail,
		TraceID:  traceID,
		HighRisk: true,
	})
}

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
