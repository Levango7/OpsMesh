package evaluator

import (
	"testing"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/models"
)

// validRule 造一条能通过 AddRule 校验的规则（租户可指定）。
func validRule(tenant, id string) *models.ScaleRule {
	return &models.ScaleRule{
		TenantID:           tenant,
		ID:                 id,
		Name:               "rule-" + id,
		Deployment:         "web-app",
		Namespace:          "default",
		Metric:             "cpu",
		ScaleUpThreshold:   80,
		ScaleDownThreshold: 20,
		MinReplicas:        1,
		MaxReplicas:        10,
		Enabled:            true,
	}
}

// TestEvaluatorTenantIsolation 是 D4（issue #27 裁决②）在 autoscaler **真实数据面**
// （Evaluator 是纯内存引擎，store 层已确认从未装配）上的钉子：
// 空租户拒写、跨租户读/列/改/删不可见、跨租户同 ID 不覆盖、决策与冷却按租户过滤。
func TestEvaluatorTenantIsolation(t *testing.T) {
	e := NewEvaluator(nil)

	// 空租户拒绝写入
	if err := e.AddRule(validRule("", "r-x")); err != ErrTenantRequired {
		t.Fatalf("空租户应返回 ErrTenantRequired，实得 %v", err)
	}

	// A 建规则 + 记一条决策
	if err := e.AddRule(validRule("A", "r-a")); err != nil {
		t.Fatalf("A 建规则失败: %v", err)
	}
	e.RecordDecision(&models.ScaleDecision{TenantID: "A", ID: "d-a", RuleID: "r-a", Action: "scale_up"})

	// B 视角：读/列/改/删全部不可见或不可写
	if _, err := e.GetRule("r-a", "B"); err == nil {
		t.Fatal("B 读到了 A 的规则")
	}
	if got := e.ListRules("B"); len(got) != 0 {
		t.Fatalf("B 列表看到 %d 条（应 0）", len(got))
	}
	if err := e.UpdateRule(validRule("B", "r-a"), "B"); err != ErrTenantMismatch {
		t.Fatalf("B 改 A 的规则应 ErrTenantMismatch，实得 %v", err)
	}
	if err := e.DeleteRule("r-a", "B"); err == nil {
		t.Fatal("B 删掉了 A 的规则")
	}
	// 跨租户同 ID 建规则：不覆盖
	if err := e.AddRule(validRule("B", "r-a")); err != ErrTenantMismatch {
		t.Fatalf("跨租户同 ID 应 ErrTenantMismatch，实得 %v", err)
	}
	// 决策与冷却按租户过滤
	if got := e.Decisions("B"); len(got) != 0 {
		t.Fatalf("B 看到 %d 条决策（应 0）", len(got))
	}
	if got := e.Cooldowns("B"); len(got) != 0 {
		t.Fatalf("B 看到 %d 条冷却（应 0）", len(got))
	}
	// Evaluate 只评估本租户规则（B 无规则 ⇒ 0 条决策）
	decisions, err := e.Evaluate(nil, nil, "", "B")
	if err != nil {
		t.Fatalf("Evaluate(B): %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("B 的评估产生了 %d 条决策（应 0）", len(decisions))
	}

	// A 自己一切正常
	if got, err := e.GetRule("r-a", "A"); err != nil || got.TenantID != "A" {
		t.Fatalf("A 读自己的规则失败: %v", err)
	}
	if got := e.ListRules("A"); len(got) != 1 {
		t.Fatalf("A 列表应 1 条，实得 %d", len(got))
	}
	if got := e.Decisions("A"); len(got) != 1 {
		t.Fatalf("A 决策应 1 条，实得 %d", len(got))
	}
	if got := e.Cooldowns("A"); len(got) != 1 {
		t.Fatalf("A 冷却应 1 条，实得 %d", len(got))
	}
}

// TestRecordDecisionRejectsEmptyTenant：无主决策不入历史（防止"匿名决策"污染别人视图）。
func TestRecordDecisionRejectsEmptyTenant(t *testing.T) {
	e := NewEvaluator(nil)
	e.RecordDecision(&models.ScaleDecision{ID: "d-x", RuleID: "r-x", Action: "manual"})
	if got := e.DecisionHistoryLen(); got != 0 {
		t.Fatalf("空租户决策应被拒收，历史仍有 %d 条", got)
	}
}
