// abac_shadow.go ABAC 影子模式（批次50）：只观察、不强制。
//
// 背景：ABAC 服务端引擎（abac_engine.go）已有确定性决策（deny-overrides +
// default-deny），但强制闸门尚未接线（D2 待 owner 决策）。直接把 ABAC allow/deny
// 接进请求放行会在策略镜像尚未被真实策略覆盖时误伤正常流量；shadow 模式先以
// 「评估 + 记录」积累 would-deny 证据，再据指标决定是否升级为 enforcement。
//
// 安全边界（硬约束）：
//   - 绝不修改请求/响应/状态码，绝不阻断：评估结果只落日志与计数；
//   - 评估调用包在 defer recover() 内：任何 panic（含策略数据异常）都不得外溢，
//     更不能把本中间件变成 500 来源；
//   - 只读取已建立的身份上下文（默认拒绝中间件在 production 已做过 token/租户
//     交叉校验）；租户缺失即跳过，不猜测、不回落；
//   - context 只取 remoteAddr/userAgent，绝不采集 Cookie/Authorization 等秘密头；
//   - 关闭时（cfg.ABACShadow=false）构造期直接返回 next，热路径零开销。
package controlplane

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/logx"
)

// shadow 决策标签取值（与 internal/metrics 的固定标签集合保持一致）。
const (
	abacShadowDecisionAllow = "allow"
	abacShadowDecisionDeny  = "deny"
	// abacShadowDecisionError 仅用于评估器 panic：把「shadow 自身坏了」与
	// 「策略判定 deny」分开，否则评估失效会被误读成大量 would-deny。
	abacShadowDecisionError = "error"
)

// 评估计数暴露给测试（Prometheus 序列由 internal/metrics 单独维护，
// 两者分离：测试不依赖指标渲染格式，运维面不受测试钩子影响）。
var (
	// ABACShadowEvalTotal 成功进入评估的请求数（不含跳过与 panic）。
	ABACShadowEvalTotal atomic.Int64
	// ABACShadowWouldDenyTotal 评估结果为 deny 的请求数（would-deny）。
	ABACShadowWouldDenyTotal atomic.Int64
	// ABACShadowPanicTotal 评估器 panic 次数（应为 0；非 0 视为 ABAC 数据缺陷）。
	ABACShadowPanicTotal atomic.Int64
)

// abacShadowEvalHook 仅测试用：替换评估器以注入固定决策或 panic。
// 生产恒为 nil；本包测试串行执行（无 t.Parallel），用例结束须复位。
var abacShadowEvalHook func(s *Server, tenantID, subject, resource, actionAttr, contextJSON string) (bool, string, string)

// abacShadowSubject / abacShadowResource / abacShadowContext 是喂给策略引擎的
// 属性 JSON 形状。字段顺序固定，便于按前缀模式（如 `{"userID":"alice"*`）配置策略。
type abacShadowSubject struct {
	UserID   string   `json:"userID"`
	Roles    []string `json:"roles"`
	TenantID string   `json:"tenantID"`
}

type abacShadowResource struct {
	Path   string `json:"path"`
	Method string `json:"method"`
}

type abacShadowContext struct {
	RemoteAddr string `json:"remoteAddr"`
	UserAgent  string `json:"userAgent"`
}

// abacShadowMiddleware 在鉴权上下文建立之后、业务 handler 之前观察变更请求。
//
// 插入层选择（server_lifecycle.go）：位于 defaultDenyMiddleware 之内、
// bodyLimitMiddleware/mux 之外。原因：
//  1. defaultDenyMiddleware 在 production 已对 /api/v1/* 完成身份校验/交叉校验，
//     到这里仍能通过的身份才是「已验签身份」；未认证请求不会污染 would-deny 统计；
//  2. 位于限流/CSRF 之后：被 429/403 提前拒绝的请求不消耗评估开销；
//  3. 位于 handler 之外：无论 handler 返回 200/202/4xx/5xx，都能观察到该请求，
//     且本中间件不读取响应，天然不可能改写状态码。
func (s *Server) abacShadowMiddleware(next http.Handler) http.Handler {
	// 关闭时零开销：不分配闭包、不解析路径。
	if s.cfg == nil || !s.cfg.ABACShadow {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if abacShadowApplies(r) {
			s.observeABACShadow(r)
		}
		next.ServeHTTP(w, r)
	})
}

// abacShadowApplies 判定请求是否进入 shadow 观察面。
// 只观察变更方法（读请求不改变授权决策面）；排除 ABAC 管理面自身（否则策略
// 管理请求会与「策略是否覆盖管理面」互相纠缠）、联邦入站（HMAC 认证、独立语义）、
// SSE 长连接与三个基础端点。
func abacShadowApplies(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
	default:
		return false
	}
	p := r.URL.Path
	switch p {
	case "/healthz", "/readyz", "/metrics", "/api/v1/abac", "/api/v1/events/stream":
		return false
	}
	if !strings.HasPrefix(p, "/api/v1/") {
		return false
	}
	if strings.HasPrefix(p, "/api/v1/abac/") || strings.HasPrefix(p, "/api/v1/federation/") {
		return false
	}
	return true
}

// observeABACShadow 执行一次「评估 + 记录」。任何 failure 都只影响观察面：
// 跳过（租户缺失/属性序列化失败）或记 error（panic），从不影响请求处理。
func (s *Server) observeABACShadow(r *http.Request) {
	actx := s.abacShadowIdentity(r)
	if actx.TenantID == "" {
		// 无已验签租户 ⇒ 无从限定策略范围；shadow 不猜测（default-deny 是引擎
		// 语义，把「身份缺失」混进 would-deny 会让指标失去可解释性）。
		logx.Debug(r.Context(), "abac shadow skipped: no verified tenant in identity context",
			"method", r.Method, "path", r.URL.Path)
		return
	}

	subjectJSON, err := json.Marshal(abacShadowSubject{
		UserID:   actx.UserID,
		Roles:    actx.Roles,
		TenantID: actx.TenantID,
	})
	if err != nil {
		logx.Warn(r.Context(), "abac shadow skipped: subject marshal failed", "err", err)
		return
	}
	resourceJSON, err := json.Marshal(abacShadowResource{Path: r.URL.Path, Method: r.Method})
	if err != nil {
		logx.Warn(r.Context(), "abac shadow skipped: resource marshal failed", "err", err)
		return
	}
	contextJSON, err := json.Marshal(abacShadowContext{RemoteAddr: r.RemoteAddr, UserAgent: r.UserAgent()})
	if err != nil {
		logx.Warn(r.Context(), "abac shadow skipped: context marshal failed", "err", err)
		return
	}

	evaluate := s.evaluateABAC
	if abacShadowEvalHook != nil {
		evaluate = func(tenantID, subject, resource, actionAttr, ctxJSON string) (bool, string, string) {
			return abacShadowEvalHook(s, tenantID, subject, resource, actionAttr, ctxJSON)
		}
	}

	start := time.Now()
	allowed, policyID, reason, panicked := s.safeABACShadowEvaluate(evaluate, actx.TenantID, string(subjectJSON), string(resourceJSON), r.Method, string(contextJSON))
	duration := time.Since(start)
	if panicked {
		ABACShadowPanicTotal.Add(1)
		if s.metrics != nil {
			s.metrics.IncABACShadowDecision(abacShadowDecisionError, r.Method)
		}
		// panic 也是必须落盘的观察结果：decision=error 与策略 deny 分开，
		// 否则「评估器坏了」会被误读成大量 would-deny。
		logx.Warn(r.Context(), "abac shadow decision",
			"shadow", true,
			"decision", abacShadowDecisionError,
			"matchedPolicyID", "",
			"reason", "evaluator panic (recovered; request unaffected)",
			"method", r.Method,
			"path", r.URL.Path,
			"tenantID", actx.TenantID,
			"userID", actx.UserID,
			"durationMs", duration.Milliseconds())
		return
	}

	decision := abacShadowDecisionDeny
	if allowed {
		decision = abacShadowDecisionAllow
	} else {
		ABACShadowWouldDenyTotal.Add(1)
	}
	ABACShadowEvalTotal.Add(1)
	if s.metrics != nil {
		s.metrics.IncABACShadowDecision(decision, r.Method)
	}
	// 结构化记录：一条决策一行，字段与运维/审计查询约定一致。
	// 注意：path 取原始 URL.Path（不含 query），避免 query 中的令牌/参数进入日志。
	logx.Info(r.Context(), "abac shadow decision",
		"shadow", true,
		"decision", decision,
		"matchedPolicyID", policyID,
		"reason", reason,
		"method", r.Method,
		"path", r.URL.Path,
		"tenantID", actx.TenantID,
		"userID", actx.UserID,
		"durationMs", duration.Milliseconds())
}

// safeABACShadowEvaluate 是唯一的评估调用点：defer recover 兜住评估器 panic。
// panic 时返回 panicked=true（调用方记 error 决策），请求照常进入 handler。
func (s *Server) safeABACShadowEvaluate(
	evaluate func(string, string, string, string, string) (bool, string, string),
	tenantID, subject, resource, actionAttr, contextJSON string,
) (allowed bool, policyID, reason string, panicked bool) {
	defer func() {
		if rec := recover(); rec != nil {
			allowed, policyID, reason, panicked = false, "", "", true
		}
	}()
	allowed, policyID, reason = evaluate(tenantID, subject, resource, actionAttr, contextJSON)
	return allowed, policyID, reason, false
}

// abacShadowIdentity 读取与业务 handler 同源的身份（网关注入头 + Bearer/Cookie
// JWT 回退），但不做写入/拒绝：本函数只读，失败由调用方跳过。
// production 下 defaultDenyMiddleware 已执行过 requireTenantContext 的交叉校验，
// 因此能走到这里的身份已通过「头 ↔ token」一致性检查。
func (s *Server) abacShadowIdentity(r *http.Request) authctx.Context {
	actx := authctx.FromHTTPHeader(r.Header)
	tokenTenant, tokenUser, tokenRoles := s.tenantFromBearer(r)
	if actx.TenantID == "" {
		actx.TenantID = tokenTenant
	}
	if actx.UserID == "" {
		actx.UserID = tokenUser
	}
	if len(actx.Roles) == 0 {
		actx.Roles = tokenRoles
	}
	return actx
}
