// authz_audit.go — 授权拒绝入审计链（A3，2026-10-12 模拟用户演练）。
//
// 为什么必须记：演练里两个"模拟用户"独立复现——被拒的越权尝试**零留痕**。审计链里只有
// user_login / user_create / user_change_password；6 项越权写操作（下发任务/建号/改角色/删设备/
// 建网关路由）与跨租户读全部无痕迹，审计 count 前后不动。对值班复盘与等保场景，**越权尝试
// 恰恰是最该留痕的一类**——正常操作有业务记录，异常尝试只有审计能回答"谁在什么时候试过什么"。
//
// 边界（写清楚，别让人以为 401 也记）：只记「**已带身份**但被判无权/不相符」的 403——
// 无权限、租户不符、用户不符三类；**无身份的 401 不记**（扫描器日常噪音，纳入只会淹真信号）。
// 这条边界是刻意的：审计的价值来自信噪比，不来自条数。
//
// 字段口径：actor/tenant 取身份头，缺失回退 JWT（不查库：拒绝路径不应因查库失败而记不上）；
// detail 记「方法 + 路径（**不含 query**，台账里不留参数值）+ 所需权限 + 拒绝原因」，
// 全部经 sanitizeAuditDetail 兜底（与既有审计同口径，防控制字符/超长串）。
package controlplane

import (
	"net/http"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// auditAuthzDenied 记录一次授权拒绝。required 为空表示"非权限类拒绝"（如租户/用户不符）。
func (s *Server) auditAuthzDenied(r *http.Request, required, reason string) {
	if s == nil || r == nil || s.store == nil {
		return
	}
	hdr := authctx.FromHTTPHeader(r.Header)
	actor, tenant := hdr.UserID, hdr.TenantID
	if actor == "" {
		actor = s.tokenUsername(r) // 只解声明、不查库（拒绝路径要能独立记上）
	}
	if tenant == "" {
		if tt, _, _ := s.tenantFromBearer(r); tt != "" {
			tenant = tt
		}
	}
	if tenant == "" {
		// 审计归属租户：拒绝发生在哪个租户域就归哪；全空时归 default（与其它审计同口径）。
		tenant = "default"
	}
	detail := r.Method + " " + r.URL.Path
	if required != "" {
		detail += " | required=" + required
	}
	if reason != "" {
		detail += " | " + reason
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant,
		UserID:   actor,
		Action:   "authz_denied",
		Target:   sanitizeAuditDetail(r.URL.Path), // 只有路径：query 一律不入台账
		Detail:   sanitizeAuditDetail(detail),
	})
}
