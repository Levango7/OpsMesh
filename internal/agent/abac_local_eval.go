// abac_local_eval.go 设备侧本地 ABAC 评估入口（M2）。
//
// 只消费已验签的 LKG 策略包，不发起网络请求、不触碰请求闸门（M4 才接线）。
// 安全语义：
//   - 无包 ⇒ ErrNoBundle（调用方可回退服务端评估或按模式 fail-closed）；
//   - 每次评估前用当前 KeySet 重新验签内存包（纵深防御：防缓存/内存被篡改或旧 keyId 残留）；
//   - 过期不阻止评估，只通过 Decision.BundleExpired 暴露，fail-open/closed 由调用方决定。
package agent

import (
	"errors"
	"fmt"
	"time"

	"github.com/Levango7/OpsMesh/pkg/abacbundle"
)

// ErrNoBundle 当前没有可用的已验签策略包。
var ErrNoBundle = errors.New("agent: no verified ABAC policy bundle cached")

// EvaluateLocal 用当前 LKG 策略包做本地 ABAC 决策。
//
// subject/resource/action 为匹配值；ctx 为 context JSON 对象文本（可为空）。
// 返回 error 仅表示「无法可信评估」（无包/复验失败），不代表拒绝决策。
func (c *ABACBundleClient) EvaluateLocal(subject, resource, action, ctx string) (abacbundle.Decision, error) {
	if c == nil {
		return abacbundle.Decision{}, ErrNoBundle
	}
	sb, _ := c.Current()
	if sb == nil {
		return abacbundle.Decision{}, ErrNoBundle
	}
	if c.keys == nil {
		return abacbundle.Decision{}, fmt.Errorf("%w: no verifier key set", ErrNoBundle)
	}
	if err := c.keys.VerifySignature(*sb); err != nil {
		return abacbundle.Decision{}, fmt.Errorf("agent: cached ABAC bundle failed re-verification: %w", err)
	}

	now := time.Now()
	if c.now != nil {
		now = c.now()
	}
	return abacbundle.EvaluateBundle(*sb, subject, resource, action, ctx, now.UTC()), nil
}
