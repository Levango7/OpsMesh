// eval.go 设备侧 ABAC 评估器（M2）：与服务端 internal/controlplane.evaluateABAC 逐条对齐语义。
//
// # 从服务端复刻的算法（逐项一致，不可自行"改进"）
//
//  1. 策略顺序：服务端按内存插入序遍历；设备侧没有插入序，改为按策略 ID 字典序稳定排序，
//     使同一策略集合在任意输入顺序下都得到相同的 MatchedPolicyID。
//  2. 字段匹配（subject / resource / action，各自独立）：
//     - "" 或 "*"      ⇒ 恒匹配（空白 pattern 等价全通配，这是服务端既有行为）；
//     - "prefix*"      ⇒ 前缀匹配；
//     - 其他           ⇒ 精确相等（区分大小写；"*" 只认末尾，中间星号按字面字符处理）。
//  3. 条件匹配（Conditions 逐键等值，作用于 context JSON 对象）：
//     - Conditions 为空 ⇒ 恒真，**不解析 context**（因此坏 context 也能通过无条件策略）；
//     - context 为空串/纯空白 ⇒ 视作空对象（有条件则必然缺键 ⇒ 不匹配）；
//     - context 必须是 JSON 对象；解析失败或为数组/标量/null ⇒ 有条件一律不匹配；
//     - 每个条件键必须存在；字符串值直接比较；
//     非字符串值（数字/布尔/null/嵌套对象/数组）按 json.Marshal 文本比较，
//     嵌套对象键序由 encoding/json 排序，因此 {"n":1,"env":"prod"} 与
//     {"env":"prod","n":1} 比较等价，而数字 1 与字符串 "1" 只有在条件写成 "1" 时才相等；
//     - conditions 中有、context 中没有的键 ⇒ 不匹配（缺键不等于空值）。
//     - context 中多余的键不影响匹配（只做子集等值）。
//  4. Effect：只有精确等于 "deny" 才视为拒绝；**其他任何值（"allow"、""、"audit"、未知字符串）
//     都按"允许候选"处理**——这是服务端现状，设备侧必须同样处理以免两边决策分叉。
//  5. 决策序（deny-overrides + default-deny）：
//     a. 遍历策略：首个命中且 Effect=="deny" 的策略立即返回拒绝（DenyOverride=true）；
//     b. 记录首个命中且非 deny 的策略 ID（按排序后的顺序，即最小 ID）；
//     c. 遍历结束：有 allow 候选 ⇒ 允许；否则 ⇒ 拒绝且 MatchedPolicyID 为空。
//  6. 租户过滤：服务端按策略 TenantID 过滤（"" 为全局策略）。策略包在签发时已按租户收敛
//     （Bundle.TenantScope），Policy 结构不含 TenantID，因此设备侧不做二次租户过滤。
//  7. 过期：BundleExpired = now.After(expiresAt)，只作标记，不改变 Allowed/Reason。
//     失败开放/关闭由调用方（M4 请求闸门）按运行模式决定。
//
// # 与服务端保持一致的 reason 文案
//
//	"denied by policy <id>" / "allowed by policy <id>" / "no matching policy (default deny)"
package abacbundle

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Decision 单次 ABAC 评估结果。
type Decision struct {
	Allowed         bool
	MatchedPolicyID string
	Reason          string
	DenyOverride    bool
	BundleExpired   bool
}

// Evaluate 对单次请求做本地决策，语义与 controlplane.evaluateABAC 对齐。
//
// policies 不会被修改；now/expiresAt 只影响 BundleExpired。
func Evaluate(policies []Policy, subjectJSON, resourceJSON, actionAttr, contextJSON string, now time.Time, expiresAt time.Time) Decision {
	expired := now.After(expiresAt)

	sorted := append([]Policy(nil), policies...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	matchedAllow := ""
	for _, p := range sorted {
		if !evalPatternMatch(p.Subject, subjectJSON) ||
			!evalPatternMatch(p.Resource, resourceJSON) ||
			!evalPatternMatch(p.Action, actionAttr) {
			continue
		}
		if !evalConditionsMatch(p.Conditions, contextJSON) {
			continue
		}
		if p.Effect == "deny" {
			return Decision{
				Allowed:         false,
				MatchedPolicyID: p.ID,
				Reason:          "denied by policy " + p.ID,
				DenyOverride:    true,
				BundleExpired:   expired,
			}
		}
		if matchedAllow == "" {
			matchedAllow = p.ID
		}
	}
	if matchedAllow != "" {
		return Decision{
			Allowed:         true,
			MatchedPolicyID: matchedAllow,
			Reason:          "allowed by policy " + matchedAllow,
			BundleExpired:   expired,
		}
	}
	return Decision{
		Allowed:       false,
		Reason:        "no matching policy (default deny)",
		BundleExpired: expired,
	}
}

// EvaluateBundle 用已缓存的签名包做本地决策。
//
// 本函数**不验签**：调用方必须先通过 abacbundle.Verify/KeySet.Verify 或
// KeySet.VerifySignature 确认 Bundle 可信（agent 侧 EvaluateLocal 已内置复验）。
func EvaluateBundle(sb SignedBundle, subjectJSON, resourceJSON, actionAttr, contextJSON string, now time.Time) Decision {
	return Evaluate(sb.Bundle.Policies, subjectJSON, resourceJSON, actionAttr, contextJSON, now, sb.Bundle.ExpiresAt)
}

// evalPatternMatch 与服务端 abacPatternMatch 逐字对齐。
func evalPatternMatch(pattern, value string) bool {
	switch {
	case pattern == "" || pattern == "*":
		return true
	case strings.HasSuffix(pattern, "*"):
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	default:
		return pattern == value
	}
}

// evalConditionsMatch 与服务端 abacConditionsMatch 逐字对齐。
func evalConditionsMatch(cond map[string]string, contextJSON string) bool {
	if len(cond) == 0 {
		return true
	}
	ctx := map[string]any{}
	if strings.TrimSpace(contextJSON) != "" {
		if err := json.Unmarshal([]byte(contextJSON), &ctx); err != nil {
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
