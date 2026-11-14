// abac_engine.go 实现服务端 ABAC 策略注册与评估（审计整改批次17）。
//
// 背景：此前 `evaluate` 恒 501（防假 ALLOWED 的安全兜底），策略文件只下发到
// agent 侧、控制面无法做访问决策。本文件在控制面维护一份**已创建策略**的镜像，
// 提供确定性的评估语义：
//
//   - 匹配字段：tenant / subject / resource / action（支持 `*` 通配与 `prefix*` 前缀）
//   - 条件：context 为 JSON 对象，逐键等值匹配（字符串化比较）
//   - 决策：deny-overrides；无匹配策略 ⇒ 默认拒绝（default-deny）
//
// 说明：设备侧脚本下发路径保持不变（policy-create/list/delete 仍走
// buildABACScript）；evaluate 不再下发脚本，改为本地评估返回 decision。
package controlplane

import (
	"encoding/json"
	"strings"
)

// abacPolicy 控制面侧策略镜像。
type abacPolicy struct {
	ID       string            `json:"id"`
	TenantID string            `json:"tenantID"`
	Subject  string            `json:"subject"`
	Resource string            `json:"resource"`
	Action   string            `json:"action"`
	Effect   string            `json:"effect"` // allow / deny
	Condition map[string]string `json:"condition,omitempty"`
}

// abacPatternMatch 支持三种形态：精确、`*`（全通配）、`prefix*` 前缀。
func abacPatternMatch(pattern, value string) bool {
	switch {
	case pattern == "" || pattern == "*":
		return true
	case strings.HasSuffix(pattern, "*"):
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	default:
		return pattern == value
	}
}

// abacConditionsMatch 逐键等值匹配 context（context 为 JSON 对象；空条件恒真）。
func abacConditionsMatch(cond map[string]string, contextJSON string) bool {
	if len(cond) == 0 {
		return true
	}
	ctx := map[string]any{}
	if strings.TrimSpace(contextJSON) != "" {
		if err := json.Unmarshal([]byte(contextJSON), &ctx); err != nil {
			// 条件无法解析 ⇒ 不匹配（安全侧：不把坏输入当满足条件）。
			return false
		}
	}
	for k, want := range cond {
		raw, ok := ctx[k]
		if !ok {
			return false
		}
		got, ok := raw.(string)
		if !ok {
			// 非字符串值按 JSON 文本比较（保持确定性）。
			b, err := json.Marshal(raw)
			if err != nil {
				return false
			}
			got = string(b)
		}
		if got != want {
			return false
		}
	}
	return true
}

// evaluateABAC 对单次请求做决策：deny-overrides + default-deny。
// 返回 (allowed, matchedPolicyID, reason)。
func (s *Server) evaluateABAC(tenantID, subject, resource, actionAttr, contextJSON string) (bool, string, string) {
	s.abacMu.RLock()
	policies := append([]abacPolicy(nil), s.abacPolicies...)
	s.abacMu.RUnlock()

	matchedAllow := ""
	for _, p := range policies {
		if p.TenantID != "" && p.TenantID != tenantID {
			continue
		}
		if !abacPatternMatch(p.Subject, subject) || !abacPatternMatch(p.Resource, resource) || !abacPatternMatch(p.Action, actionAttr) {
			continue
		}
		if !abacConditionsMatch(p.Condition, contextJSON) {
			continue
		}
		if p.Effect == "deny" {
			return false, p.ID, "denied by policy " + p.ID
		}
		if matchedAllow == "" {
			matchedAllow = p.ID
		}
	}
	if matchedAllow != "" {
		return true, matchedAllow, "allowed by policy " + matchedAllow
	}
	return false, "", "no matching policy (default deny)"
}

// addABACPolicy 注册/更新策略镜像（policy-create 时调用）。
func (s *Server) addABACPolicy(p abacPolicy) {
	s.abacMu.Lock()
	defer s.abacMu.Unlock()
	for i := range s.abacPolicies {
		if s.abacPolicies[i].ID == p.ID {
			s.abacPolicies[i] = p
			return
		}
	}
	s.abacPolicies = append(s.abacPolicies, p)
}

// removeABACPolicy 删除策略镜像（policy-delete 时调用）。
func (s *Server) removeABACPolicy(id string) {
	s.abacMu.Lock()
	defer s.abacMu.Unlock()
	out := s.abacPolicies[:0]
	for _, p := range s.abacPolicies {
		if p.ID != id {
			out = append(out, p)
		}
	}
	s.abacPolicies = out
}

// listABACPolicies 返回策略镜像副本（policy-list 时调用）。
func (s *Server) listABACPolicies(tenantID string) []abacPolicy {
	s.abacMu.RLock()
	defer s.abacMu.RUnlock()
	out := make([]abacPolicy, 0, len(s.abacPolicies))
	for _, p := range s.abacPolicies {
		if p.TenantID == "" || p.TenantID == tenantID {
			out = append(out, p)
		}
	}
	return out
}
