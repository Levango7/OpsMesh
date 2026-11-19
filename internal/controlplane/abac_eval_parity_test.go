package controlplane

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/Levango7/OpsMesh/pkg/abacbundle"
)

// TestABACEvalParityDeviceFixtures 是服务端 ↔ 设备评估器的一致性守卫。
//
// 两侧读取同一份 pkg/abacbundle/testdata/eval_fixtures.json：
//   - 设备侧 pkg/abacbundle.TestEvaluateFixtures 直接断言 Evaluate；
//   - 本测试把同一批 Policy 映射成 controlplane.abacPolicy（TenantID 为空 = 全局策略，
//     与策略包按租户签发后的设备视图一致），按 ID 字典序插入内存镜像
//     （设备侧 Evaluate 的确定性顺序），再调用 s.evaluateABAC。
//
// 任何一侧改动语义而另一侧未同步，都会在 CI 中立即失败。
func TestABACEvalParityDeviceFixtures(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "pkg", "abacbundle", "testdata", "eval_fixtures.json"))
	if err != nil {
		t.Fatalf("读取 eval_fixtures.json: %v", err)
	}
	var cases []struct {
		Name     string              `json:"name"`
		Policies []abacbundle.Policy `json:"policies"`
		Subject  string              `json:"subject"`
		Resource string              `json:"resource"`
		Action   string              `json:"action"`
		Context  string              `json:"context"`
		Expected struct {
			Allowed         bool   `json:"allowed"`
			MatchedPolicyID string `json:"matchedPolicyID"`
			DenyOverride    bool   `json:"denyOverride"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("解析 eval_fixtures.json: %v", err)
	}
	if len(cases) < 20 {
		t.Fatalf("fixture 数量=%d，至少需要 20 条", len(cases))
	}

	const tenantID = "t-parity"
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			s := &Server{}

			policies := append([]abacbundle.Policy(nil), tc.Policies...)
			sort.SliceStable(policies, func(i, j int) bool { return policies[i].ID < policies[j].ID })
			for _, p := range policies {
				if err := s.addABACPolicy(abacPolicy{
					ID:        p.ID,
					TenantID:  "", // 空 = 全局策略，与设备侧策略包视图一致
					Subject:   p.Subject,
					Resource:  p.Resource,
					Action:    p.Action,
					Effect:    p.Effect,
					Condition: p.Conditions,
				}); err != nil {
					t.Fatalf("addABACPolicy(%s): %v", p.ID, err)
				}
			}

			allowed, id, reason := s.evaluateABAC(tenantID, tc.Subject, tc.Resource, tc.Action, tc.Context)
			denyOverride := !allowed && id != ""

			if allowed != tc.Expected.Allowed {
				t.Fatalf("Allowed=%v, want %v (id=%q reason=%q)", allowed, tc.Expected.Allowed, id, reason)
			}
			if id != tc.Expected.MatchedPolicyID {
				t.Fatalf("MatchedPolicyID=%q, want %q (reason=%q)", id, tc.Expected.MatchedPolicyID, reason)
			}
			if denyOverride != tc.Expected.DenyOverride {
				t.Fatalf("DenyOverride=%v, want %v (id=%q reason=%q)", denyOverride, tc.Expected.DenyOverride, id, reason)
			}
			if reason == "" {
				t.Fatal("evaluateABAC 必须返回 reason")
			}
		})
	}
}
