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
// 字段口径（2026-10-14 演练 §3.5 修订）：
//   - **actor/tenant 只信两个可信源**：① JWT 的 user_id/tenant_id 声明（存储 ID，
//     与 user_login 等既有审计同口径）；② --trust-gateway-headers 注入模式下的
//     X-User-Id/X-Tenant-ID（由受信网关注入，不是客户端自称）。
//     客户端自称头一律只作**证据**进 detail（claimed_user=），绝不作 actor——演练
//     人设实测两条伪造署名入库（user mismatch 那类 403 的 actor 竟是被拒的头值
//     ⇒ 按 actor 追责会被带偏、真实 JWT 主体缺失）。两源都不可用 ⇒ unknown
//     （身份不适用就不编，也不采信自称）。
//   - UserID 统一为**存储 ID**（此前 authz_denied 记用户名、user_login 记存储 ID，
//     同字段两种含义 ⇒ 按 actor 单一键对账必漏）；用户名以 actor_name= 进 detail，
//     可读性不缺、语义不再分裂。
//   - detail 另记「方法 + 路径（**不含 query**）+ 所需权限 + 拒绝原因」，全部经
//     sanitizeAuditDetail 兜底（与既有审计同口径，防控制字符/超长串）。
//
// 拒绝路径**不查库**（actor/tenant 都从凭证解）：查库失败不该让越权尝试记不上。
package controlplane

import (
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// auditAuthzDenied 记录一次授权拒绝。required 为空表示"非权限类拒绝"（如租户/用户不符）。
func (s *Server) auditAuthzDenied(r *http.Request, required, reason string) {
	if s == nil || r == nil || s.store == nil {
		return
	}
	hdr := authctx.FromHTTPHeader(r.Header)
	trustHeaders := s.cfg != nil && s.cfg.TrustGatewayHeaders

	jwtTenant, jwtUser, _ := s.tenantFromBearer(r)
	claimed := strings.TrimSpace(hdr.UserID)

	// actor：JWT 存储 ID 优先；注入模式才认头；都不可用记 unknown。
	actor := jwtUser
	if actor == "" && trustHeaders {
		actor = claimed
	}
	if actor == "" {
		actor = "unknown"
	}
	// tenant：JWT 优先；注入模式才认头；全空归 default（与其它审计同口径）。
	tenant := jwtTenant
	if tenant == "" && trustHeaders {
		tenant = hdr.TenantID
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
	// 可读性/证据字段：用户名另置（不与 UserID 的存储 ID 语义混）；自称值与 actor
	// 不同才记（相同即注入模式，记了是噪音）。
	if name := s.tokenUsername(r); name != "" {
		detail += " | actor_name=" + name
	}
	if claimed != "" && claimed != actor {
		detail += " | claimed_user=" + claimed
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant,
		UserID:   actor,
		Action:   "authz_denied",
		Target:   sanitizeAuditDetail(r.URL.Path), // 只有路径：query 一律不入台账
		Detail:   sanitizeAuditDetail(detail),
	})
}
