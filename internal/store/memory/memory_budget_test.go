package memory

import (
	"context"
	"testing"

	"github.com/Levango7/OpsMesh/internal/cost"
)

func TestBudgetStoreRoundTrip(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	b := &cost.TokenBudget{TenantID: "t1", DailyLimit: 100, MonthlyLimit: 1000, AlertThreshold: 0.8, UsedToday: 10, UsedMonth: 20, Timezone: "UTC"}
	if err := s.UpsertBudget(ctx, b); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBudget(ctx, "t1")
	if err != nil || got == nil {
		t.Fatalf("GetBudget: %v %v", got, err)
	}
	if got.UsedToday != 10 || got.UsedMonth != 20 || got.DailyLimit != 100 {
		t.Fatalf("mismatch: %+v", got)
	}
	list, err := s.ListBudgets(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListBudgets: %v %d", err, len(list))
	}
	alert := cost.BudgetAlert{TenantID: "t1", Type: "daily", Threshold: 0.8, Used: 90, Limit: 100}
	if err := s.RecordAlert(ctx, alert); err != nil {
		t.Fatal(err)
	}
	alerts, err := s.ListAlerts(ctx, "t1", 10)
	if err != nil || len(alerts) != 1 {
		t.Fatalf("ListAlerts: %v %d", err, len(alerts))
	}
}
