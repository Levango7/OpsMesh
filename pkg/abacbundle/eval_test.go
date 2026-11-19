package abacbundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// evalFixtureCase 与 testdata/eval_fixtures.json 的结构一一对应；
// controlplane 的 parity 测试读取同一文件，保证两侧使用同一份期望值。
type evalFixtureCase struct {
	Name     string   `json:"name"`
	Policies []Policy `json:"policies"`
	Subject  string   `json:"subject"`
	Resource string   `json:"resource"`
	Action   string   `json:"action"`
	Context  string   `json:"context"`
	Expected struct {
		Allowed         bool   `json:"allowed"`
		MatchedPolicyID string `json:"matchedPolicyID"`
		DenyOverride    bool   `json:"denyOverride"`
	} `json:"expected"`
}

func loadEvalFixtures(t *testing.T) []evalFixtureCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "eval_fixtures.json"))
	if err != nil {
		t.Fatalf("读取 eval_fixtures.json: %v", err)
	}
	var cases []evalFixtureCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("解析 eval_fixtures.json: %v", err)
	}
	if len(cases) < 20 {
		t.Fatalf("fixture 数量=%d，至少需要 20 条", len(cases))
	}
	return cases
}

func TestEvaluateFixtures(t *testing.T) {
	cases := loadEvalFixtures(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	expiresAt := now.Add(24 * time.Hour)

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got := Evaluate(tc.Policies, tc.Subject, tc.Resource, tc.Action, tc.Context, now, expiresAt)
			if got.Allowed != tc.Expected.Allowed {
				t.Fatalf("Allowed=%v, want %v (reason=%q id=%q)", got.Allowed, tc.Expected.Allowed, got.Reason, got.MatchedPolicyID)
			}
			if got.MatchedPolicyID != tc.Expected.MatchedPolicyID {
				t.Fatalf("MatchedPolicyID=%q, want %q (reason=%q)", got.MatchedPolicyID, tc.Expected.MatchedPolicyID, got.Reason)
			}
			if got.DenyOverride != tc.Expected.DenyOverride {
				t.Fatalf("DenyOverride=%v, want %v (reason=%q)", got.DenyOverride, tc.Expected.DenyOverride, got.Reason)
			}
			if got.BundleExpired {
				t.Fatalf("未过期包不应标记 BundleExpired (now=%s expiresAt=%s)", now, expiresAt)
			}
			if got.Reason == "" {
				t.Fatal("Decision.Reason 不得为空")
			}
		})
	}
}

func TestEvaluateBundleExpiredFlag(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	sb := SignedBundle{Bundle: Bundle{
		SchemaVersion: SchemaVersion,
		BundleID:      "b-expired",
		KeyID:         "k1",
		IssuedAt:      now.Add(-2 * time.Hour),
		ExpiresAt:     now.Add(-time.Hour),
		Policies: []Policy{
			{ID: "p1", Subject: "*", Resource: "*", Action: "*", Effect: "allow"},
		},
	}}

	got := EvaluateBundle(sb, "alice", "vm:1", "read", "", now)
	if !got.Allowed || got.MatchedPolicyID != "p1" {
		t.Fatalf("过期只做标记，不应改变决策: %+v", got)
	}
	if !got.BundleExpired {
		t.Fatal("now 晚于 ExpiresAt 时 BundleExpired 必须为 true")
	}

	boundary := EvaluateBundle(sb, "alice", "vm:1", "read", "", sb.Bundle.ExpiresAt)
	if boundary.BundleExpired {
		t.Fatal("now == ExpiresAt 时不算过期（严格 After）")
	}
}

func TestEvaluatePolicyOrderIsDeterministic(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	future := now.Add(time.Hour)
	policies := []Policy{
		{ID: "zeta-allow", Subject: "*", Resource: "*", Action: "*", Effect: "allow"},
		{ID: "alpha-allow", Subject: "*", Resource: "*", Action: "*", Effect: "allow"},
	}
	forward := Evaluate(policies, "u", "r", "a", "", now, future)
	reversed := Evaluate([]Policy{policies[1], policies[0]}, "u", "r", "a", "", now, future)
	if forward.MatchedPolicyID != "alpha-allow" || reversed.MatchedPolicyID != "alpha-allow" {
		t.Fatalf("多 allow 命中必须按 ID 字典序取最小: forward=%q reversed=%q", forward.MatchedPolicyID, reversed.MatchedPolicyID)
	}

	denies := []Policy{
		{ID: "z-deny", Subject: "*", Resource: "*", Action: "*", Effect: "deny"},
		{ID: "b-deny", Subject: "*", Resource: "*", Action: "*", Effect: "deny"},
	}
	got := Evaluate([]Policy{denies[0], denies[1]}, "u", "r", "a", "", now, future)
	if got.Allowed || got.MatchedPolicyID != "b-deny" || !got.DenyOverride {
		t.Fatalf("多 deny 命中必须按 ID 字典序取最小: %+v", got)
	}
}
