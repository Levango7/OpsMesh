// abac_enforce.go ABAC 定向强制（批次53）：只对显式白名单中的高风险路由强制。
//
// 产品方向（与批次50 shadow 的差异）：
//   - 不做全局闸门：默认 --abac-enforce 为空 = kill switch，中间件构造期直接返回 next；
//   - 只对白名单条目 "METHOD /path/pattern" 命中且身份/租户已建立的请求调用
//     evaluateABAC 做真实放行/拒绝；
//   - 命中但无匹配策略（default-deny）⇒ 403 + 审计 abac_enforce_denied + 指标；
//   - 未命中的路由完全不受影响（RBAC 仍是唯一强制层）。
//
// 安全边界：
//   - 评估器 panic 由 defer recover 兜住，按 fail-closed 返回受控 403（绝不 500）；
//   - 只在 method+path 命中时读写响应；allow 分支不改写任何响应字节；
//   - 白名单匹配用分段比较（字面量 + `{name}` 单段占位），不使用正则，不存在注入面；
//   - ABAC 管理面 / 联邦入站 / 健康与指标端点 / SSE 即使被误配进白名单也绝不强制。
package controlplane

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// 定向强制决策标签取值（与 internal/metrics 的固定标签集合保持一致）。
const (
	abacEnforceDecisionAllow = "allow"
	abacEnforceDecisionDeny  = "deny"
	// abacEnforceDecisionError 仅用于评估器 panic：panic 按 fail-closed 拒绝，
	// 但必须与「策略判定 deny」分开计数，否则评估器故障会被误读成策略收紧。
	abacEnforceDecisionError = "error"
)

// 评估计数暴露给测试（与 shadow 相同：指标渲染与测试钩子分离）。
var (
	// ABACEnforceEvalTotal 成功进入评估并得到确定决策的请求数（不含 panic）。
	ABACEnforceEvalTotal atomic.Int64
	// ABACEnforceDeniedTotal 被定向强制拒绝的请求数（含 default-deny）。
	ABACEnforceDeniedTotal atomic.Int64
	// ABACEnforcePanicTotal 评估器 panic 次数（应为 0）。
	ABACEnforcePanicTotal atomic.Int64
)

// abacEnforceEvalHook 仅测试用：替换评估器以注入固定决策或 panic。
// 生产恒为 nil；本包测试串行执行（无 t.Parallel），用例结束须复位。
var abacEnforceEvalHook func(s *Server, tenantID, subject, resource, actionAttr, contextJSON string) (bool, string, string)

// abacEnforceRule 是编译后的白名单条目：method + 已拆分的路径分段。
type abacEnforceRule struct {
	method   string
	path     string
	segments []string
}

// abacEnforceDeniedResponse 是 403 响应体（字段与需求约定一致）。
type abacEnforceDeniedResponse struct {
	Error           string `json:"error"`
	Reason          string `json:"reason"`
	MatchedPolicyID string `json:"matchedPolicyID"`
	Enforced        bool   `json:"enforced"`
}

// abacEnforceMiddleware 在鉴权上下文建立之后、业务 handler 之前做定向强制。
//
// 插入层选择（server_lifecycle.go）：位于 defaultDenyMiddleware 之内、
// abacShadowMiddleware 之外、bodyLimitMiddleware/mux 之前。原因：
//  1. defaultDenyMiddleware 在 production 已对 /api/v1/* 完成身份校验/交叉校验，
//     到这里仍能通过的身份才是「已验签身份」；强制判定不会基于可伪造的头单独生效；
//  2. 位于限流/CSRF 之后：被 429/403 提前拒绝的请求不消耗评估开销；
//  3. 位于 shadow 之外：命中白名单时强制先决策；shadow 在构造期编译同一份白名单并
//     对命中路由直接跳过，避免同一次请求被评估两遍（见 abacShadowMiddleware）；
//  4. 位于 bodyLimit/mux 之前：deny 在读取请求体之前完成；allow 直接交给 next，
//     本中间件不写任何响应字节，不可能改写业务响应。
func (s *Server) abacEnforceMiddleware(next http.Handler) http.Handler {
	// kill switch：空白名单 = 不强制（构造期零开销）。
	if s.cfg == nil || len(s.cfg.ABACEnforce) == 0 {
		return next
	}
	rules := compileABACEnforceRules(s.cfg)
	if len(rules) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !abacEnforceCovers(rules, r) {
			next.ServeHTTP(w, r)
			return
		}
		s.enforceABACRequest(w, r, next)
	})
}

// compileABACEnforceRules 把配置条目编译为分段匹配规则。
// 非法条目在 Validate 阶段已拒绝启动；这里仍做防御性跳过（程序化构造 Config 的
// 测试/嵌入场景不会因一条坏条目把整个进程变成 403 来源）。
func compileABACEnforceRules(cfg *config.Config) []abacEnforceRule {
	if cfg == nil || len(cfg.ABACEnforce) == 0 {
		return nil
	}
	rules := make([]abacEnforceRule, 0, len(cfg.ABACEnforce))
	seen := make(map[string]struct{}, len(cfg.ABACEnforce))
	for _, entry := range cfg.ABACEnforce {
		method, path, err := config.ParseABACEnforceEntry(entry)
		if err != nil {
			logx.Warn(nil, "abac enforce: ignoring invalid allowlist entry", "entry", entry, "err", err)
			continue
		}
		if abacEnforceExcludedPath(path) {
			logx.Warn(nil, "abac enforce: ignoring excluded allowlist entry", "entry", entry)
			continue
		}
		key := method + " " + path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		rules = append(rules, abacEnforceRule{
			method:   method,
			path:     path,
			segments: strings.Split(strings.TrimPrefix(path, "/"), "/"),
		})
	}
	return rules
}

// abacEnforceCovers 报告请求是否被定向强制白名单覆盖。
// 供本中间件与 shadow 共用：命中时 shadow 必须让位，避免双重评估。
func abacEnforceCovers(rules []abacEnforceRule, r *http.Request) bool {
	if len(rules) == 0 || r == nil {
		return false
	}
	if abacEnforceExcludedPath(r.URL.Path) {
		return false
	}
	for _, rule := range rules {
		if abacEnforceRuleMatches(rule, r.Method, r.URL.Path) {
			return true
		}
	}
	return false
}

// abacEnforceRuleMatches：方法精确 + 路径逐段比较。
// `{name}` 分段匹配任意一个非空分段；其余分段按字面量精确比较。
func abacEnforceRuleMatches(rule abacEnforceRule, method, path string) bool {
	if rule.method != method {
		return false
	}
	actual := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(actual) != len(rule.segments) {
		return false
	}
	for i, want := range rule.segments {
		if isABACEnforcePlaceholder(want) {
			if actual[i] == "" {
				return false
			}
			continue
		}
		if actual[i] != want {
			return false
		}
	}
	return true
}

func isABACEnforcePlaceholder(seg string) bool {
	return len(seg) > 2 && strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")
}

// abacEnforceExcludedPath 是强制面的硬排除清单：即使白名单误配也绝不强制。
func abacEnforceExcludedPath(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/metrics", "/api/v1/events/stream":
		return true
	}
	if strings.HasPrefix(path, "/api/v1/abac") {
		return true
	}
	return strings.HasPrefix(path, "/api/v1/federation/")
}

// enforceABACRequest 执行一次强制决策。
// 只允许两处写响应：deny（403）或把控制权交给 next（allow）；allow 路径不写字节。
func (s *Server) enforceABACRequest(w http.ResponseWriter, r *http.Request, next http.Handler) {
	actx := s.abacShadowIdentity(r)
	if actx.TenantID == "" {
		// 身份缺失：production 下 defaultDenyMiddleware 已拒绝；这里只记录并放行，
		// 不把「无身份」当成 ABAC deny（否则开发模式会因缺头全部 403）。
		logx.Warn(r.Context(), "abac enforce skipped: no verified tenant in identity context",
			"method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
		return
	}

	subjectJSON, resourceJSON, actionAttr, contextJSON, err := buildABACPolicyInputs(r, actx)
	if err != nil {
		// 输入序列化理论上不可失败；按 fail-closed 处理，绝不把坏输入当 allow。
		s.denyABACEnforce(w, r, actx, "abac input build failed: "+err.Error(), "", abacEnforceDecisionError)
		return
	}

	evaluate := s.evaluateABAC
	if abacEnforceEvalHook != nil {
		evaluate = func(tenantID, subject, resource, actionAttr, ctxJSON string) (bool, string, string) {
			return abacEnforceEvalHook(s, tenantID, subject, resource, actionAttr, ctxJSON)
		}
	}

	start := time.Now()
	allowed, policyID, reason, panicked := s.safeABACEnforceEvaluate(evaluate, actx.TenantID, subjectJSON, resourceJSON, actionAttr, contextJSON)
	duration := time.Since(start)
	if panicked {
		ABACEnforcePanicTotal.Add(1)
		s.denyABACEnforce(w, r, actx, "evaluator panic (recovered; fail-closed)", "", abacEnforceDecisionError)
		return
	}

	if allowed {
		ABACEnforceEvalTotal.Add(1)
		if s.metrics != nil {
			s.metrics.IncABACEnforceDecision(abacEnforceDecisionAllow, r.Method)
		}
		logx.Info(r.Context(), "abac enforce decision",
			"enforced", true,
			"decision", abacEnforceDecisionAllow,
			"matchedPolicyID", policyID,
			"reason", reason,
			"method", r.Method,
			"path", r.URL.Path,
			"tenantID", actx.TenantID,
			"userID", actx.UserID,
			"durationMs", duration.Milliseconds())
		next.ServeHTTP(w, r)
		return
	}

	s.denyABACEnforce(w, r, actx, reason, policyID, abacEnforceDecisionDeny)
	ABACEnforceEvalTotal.Add(1)
}

// denyABACEnforce 写 403 + 审计 + 指标 + 结构化日志。
func (s *Server) denyABACEnforce(w http.ResponseWriter, r *http.Request, actx authctx.Context, reason, policyID, decision string) {
	ABACEnforceDeniedTotal.Add(1)
	if s.metrics != nil {
		s.metrics.IncABACEnforceDecision(decision, r.Method)
	}
	// 审计：target 取原始 URL.Path（不含 query），detail 只放决策原因（已脱敏）。
	if s.store != nil {
		s.audit(r.Context(), &proto.AuditEvent{
			TenantID: actx.TenantID,
			UserID:   actx.UserID,
			Action:   "abac_enforce_denied",
			Target:   r.URL.Path,
			Detail:   sanitizeAuditDetail(reason),
		})
	}
	logx.Warn(r.Context(), "abac enforce denied",
		"enforced", true,
		"decision", decision,
		"matchedPolicyID", policyID,
		"reason", reason,
		"method", r.Method,
		"path", r.URL.Path,
		"tenantID", actx.TenantID,
		"userID", actx.UserID)
	paginate.WriteJSON(w, http.StatusForbidden, abacEnforceDeniedResponse{
		Error:           "abac denied",
		Reason:          reason,
		MatchedPolicyID: policyID,
		Enforced:        true,
	})
}

// safeABACEnforceEvaluate 是唯一的评估调用点：defer recover 兜住评估器 panic。
// panic 时返回 panicked=true（调用方按 fail-closed 返回 403），绝不外溢成 500。
func (s *Server) safeABACEnforceEvaluate(
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

// buildABACPolicyInputs 生成与 shadow 完全一致的策略输入形状：
// subject {"userID","roles","tenantID"}、resource {"path","method"}、
// actionAttr=method、context {"remoteAddr","userAgent"}（不采集秘密头）。
// 抽成共享函数是为了让 shadow 与 enforce 不会因各自序列化而语义漂移。
func buildABACPolicyInputs(r *http.Request, actx authctx.Context) (subjectJSON, resourceJSON, actionAttr, contextJSON string, err error) {
	subject, err := json.Marshal(abacShadowSubject{
		UserID:   actx.UserID,
		Roles:    actx.Roles,
		TenantID: actx.TenantID,
	})
	if err != nil {
		return "", "", "", "", err
	}
	resource, err := json.Marshal(abacShadowResource{Path: r.URL.Path, Method: r.Method})
	if err != nil {
		return "", "", "", "", err
	}
	ctxJSON, err := json.Marshal(abacShadowContext{RemoteAddr: r.RemoteAddr, UserAgent: r.UserAgent()})
	if err != nil {
		return "", "", "", "", err
	}
	return string(subject), string(resource), r.Method, string(ctxJSON), nil
}
