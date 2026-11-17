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
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/store/model"
)

// abacPolicy 控制面侧策略镜像。
type abacPolicy struct {
	ID        string            `json:"id"`
	TenantID  string            `json:"tenantID"`
	Subject   string            `json:"subject"`
	Resource  string            `json:"resource"`
	Action    string            `json:"action"`
	Effect    string            `json:"effect"` // allow / deny
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
	// 记录本副本见过的租户：刷新循环只回读这些租户（见 abac_persistence.go）。
	s.noteABACTenant(tenantID)
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
//
// 写穿（批次47）：store 非空时先落库、成功后再改缓存；落库失败返回 error 且**不改缓存**
// （handler 据此 500，避免"接口说创建成功、重启后策略消失"的静默假成功）。
// store 为空时保持原内存态语义（MemoryStore 未配置的测试/开发场景）。
func (s *Server) addABACPolicy(p abacPolicy) error {
	var version int64
	if s.store != nil {
		stored, ok := s.store.UpsertABACPolicyRecord(model.ABACPolicyRecord{
			TenantID: p.TenantID,
			ID:       p.ID,
			Subject:  p.Subject,
			Resource: p.Resource,
			Action:   p.Action,
			Effect:   p.Effect,
		})
		if !ok {
			err := fmt.Errorf("abac policy store upsert failed (tenant=%s id=%s)", p.TenantID, p.ID)
			logx.Error(context.Background(), "ABAC 策略持久化失败，缓存未更新", err,
				"tenant", p.TenantID, "policy", p.ID)
			return err
		}
		version = stored.Version
	}
	s.abacMu.Lock()
	defer s.abacMu.Unlock()
	s.abacRev++
	s.noteABACTenantLocked(p.TenantID)
	for i := range s.abacPolicies {
		if s.abacPolicies[i].ID == p.ID {
			s.abacPolicies[i] = p
			s.setABACVersionLocked(p.TenantID, p.ID, version)
			return nil
		}
	}
	s.abacPolicies = append(s.abacPolicies, p)
	s.setABACVersionLocked(p.TenantID, p.ID, version)
	return nil
}

// removeABACPolicy 删除策略镜像（policy-delete 时调用）并同步删除持久层记录。
//
// 签名保持单参（既有调用方与用例不动）；租户先查缓存，缓存未命中再回读 store 定位
// （删除可能发生在"本副本尚未加载该策略"的副本上）。
func (s *Server) removeABACPolicy(id string) error {
	var tenantID string
	s.abacMu.RLock()
	for _, p := range s.abacPolicies {
		if p.ID == id {
			tenantID = p.TenantID
			break
		}
	}
	s.abacMu.RUnlock()
	if s.store != nil {
		if tenantID == "" {
			for _, rec := range s.store.ListABACPolicyRecords("") {
				if rec.ID == id {
					tenantID = rec.TenantID
					break
				}
			}
		}
		s.store.DeleteABACPolicyRecord(tenantID, id)
	}
	s.abacMu.Lock()
	defer s.abacMu.Unlock()
	s.abacRev++
	out := s.abacPolicies[:0]
	for _, p := range s.abacPolicies {
		if p.ID != id {
			out = append(out, p)
		}
	}
	s.abacPolicies = out
	if s.abacVersions != nil {
		delete(s.abacVersions, abacVersionKey(tenantID, id))
	}
	return nil
}

// listABACPolicies 返回策略镜像副本（policy-list 时调用）。
func (s *Server) listABACPolicies(tenantID string) []abacPolicy {
	if tenantID != "" {
		s.noteABACTenant(tenantID)
	}
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
