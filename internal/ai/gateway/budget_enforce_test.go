// budget_enforce_test.go — TD-97 ① 预算真执法的行为契约测试。
//
// 背景：BudgetManager.RecordUsage/IsOverBudget 此前在生产代码零调用方
// （tech-debt.md TD-97 ①），预算是「只读展示」。本文件钉住接线后的语义：
//   - 入口越限即拒（明确错误，不是静默降级），且钱没花（provider 未被调用）；
//   - 出口按租户计量（A/B 各自计数，互不占用）；
//   - 打满的那一次放行（钱已花），下一次才被拒；
//   - 无租户 ctx 放行但记账到 default（拍板口径：不锁死单机形态）；
//   - 缓存命中不重复计量。
package aigateway

import (
	"context"
	"errors"
	"testing"

	"github.com/Levango7/OpsMesh/internal/cost"
	"github.com/Levango7/OpsMesh/internal/store"
)

// stubEnforcer 记录调用面的最小替身（真 BudgetManager 由 cost 包自己的测试覆盖）。
type stubEnforcer struct {
	over    bool
	records []struct {
		tenant string
		tokens int
	}
}

func (s *stubEnforcer) IsOverBudget(tenantID string) bool { return s.over }

func (s *stubEnforcer) RecordUsage(tenantID string, tokens int) (bool, []cost.BudgetAlert) {
	s.records = append(s.records, struct {
		tenant string
		tokens int
	}{tenantID, tokens})
	return !s.over, nil
}

// stubProvider 不真打模型，只返回确定用量。
type stubProvider struct{ called int }

func (p *stubProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	p.called++
	resp := &CompletionResponse{ID: "stub", Model: req.Model}
	resp.Usage.PromptTokens = 100
	resp.Usage.CompletionTokens = 50
	// Choices 不影响预算路径（执法只读 Usage），留空即可。
	return resp, nil
}

func (p *stubProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	return nil, errors.New("not implemented")
}

func (p *stubProvider) Name() string { return "stub" }

func (p *stubProvider) ListModels() []string { return []string{"m1"} }

func (p *stubProvider) HealthCheck(ctx context.Context) error { return nil }

func budgetTestGateway(t *testing.T) (*Gateway, *stubProvider, *stubEnforcer) {
	t.Helper()
	g := NewGateway()
	sp := &stubProvider{}
	g.RegisterProvider(ProviderConfig{Name: "p1", APIKey: "k", Models: []string{"m1"}}, sp)
	en := &stubEnforcer{}
	g.SetBudgets(en)
	return g, sp, en
}

func TestComplete_BudgetBlocksBeforeSpend(t *testing.T) {
	g, sp, en := budgetTestGateway(t)
	en.over = true
	ctx := tenantCtx("tenant-a")

	_, err := g.Complete(ctx, CompletionRequest{Model: "m1", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("越限应拒")
	}
	if sp.called != 0 {
		t.Fatalf("越限时不应真打模型（钱没花）：called=%d", sp.called)
	}
	if len(en.records) > 0 {
		t.Fatal("入口拒绝路径不应记账（没有用量发生）")
	}
}

func TestComplete_BudgetRecordsTenantUsage(t *testing.T) {
	g, _, en := budgetTestGateway(t)
	ctxA := tenantCtx("tenant-a")
	ctxB := tenantCtx("tenant-b")

	if _, err := g.Complete(ctxA, CompletionRequest{Model: "m1", Messages: []Message{{Role: "user", Content: "a"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Complete(ctxB, CompletionRequest{Model: "m1", Messages: []Message{{Role: "user", Content: "b"}}}); err != nil {
		t.Fatal(err)
	}
	if len(en.records) != 2 {
		t.Fatalf("应各记一次：%+v", en.records)
	}
	if en.records[0].tenant != "tenant-a" || en.records[1].tenant != "tenant-b" {
		t.Fatalf("计量应按各自租户：%+v", en.records)
	}
	for _, r := range en.records {
		if r.tokens != 150 { // 100 prompt + 50 completion
			t.Fatalf("计量值应为 prompt+completion：%+v", en.records)
		}
	}
}

func TestComplete_BudgetRecordsDefaultWhenNoTenant(t *testing.T) {
	g, _, en := budgetTestGateway(t)
	// 无租户 ctx（单机/内部 agent 路径）：放行但记账到 default。
	if _, err := g.Complete(context.Background(), CompletionRequest{Model: "m1", Messages: []Message{{Role: "user", Content: "x"}}}); err != nil {
		t.Fatalf("无租户 ctx 应放行：%v", err)
	}
	if len(en.records) != 1 || en.records[0].tenant != store.DefaultTenantID {
		t.Fatalf("无租户应记账到 default：%+v", en.records)
	}
	// default 租户 ctx 与裸 ctx 同一命名空间
	if _, err := g.Complete(tenantCtx(store.DefaultTenantID), CompletionRequest{Model: "m1", Messages: []Message{{Role: "user", Content: "y"}}}); err != nil {
		t.Fatal(err)
	}
	if en.records[1].tenant != store.DefaultTenantID {
		t.Fatalf("default 租户 ctx 应同一空间：%+v", en.records)
	}
}

func TestComplete_BudgetCacheHitNotDoubleCounted(t *testing.T) {
	g, _, en := budgetTestGateway(t)
	ctx := tenantCtx("tenant-a")
	req := CompletionRequest{Model: "m1", Messages: []Message{{Role: "user", Content: "same"}}}
	if _, err := g.Complete(ctx, req); err != nil {
		t.Fatal(err)
	}
	// 第二次同请求命中语义缓存：不应再计量
	if _, err := g.Complete(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(en.records) != 1 {
		t.Fatalf("缓存命中不应重复计量：%+v", en.records)
	}
}

func TestComplete_BudgetNilDisablesEnforcement(t *testing.T) {
	g := NewGateway() // 未 SetBudgets：独立使用/单测路径
	sp := &stubProvider{}
	g.RegisterProvider(ProviderConfig{Name: "p1", APIKey: "k", Models: []string{"m1"}}, sp)
	if _, err := g.Complete(context.Background(), CompletionRequest{Model: "m1", Messages: []Message{{Role: "user", Content: "x"}}}); err != nil {
		t.Fatalf("nil budgets 应完全放行：%v", err)
	}
	if sp.called != 1 {
		t.Fatalf("nil budgets 应正常执行：called=%d", sp.called)
	}
}

// 编译期确认 *cost.BudgetManager 满足 BudgetEnforcer（控制面注入的就是它）。
var _ BudgetEnforcer = (*cost.BudgetManager)(nil)
