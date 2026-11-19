package cost

// memory_budget_store_test.go — issue #98：BudgetStore 接线门禁。

import (
	"context"
	"testing"
)

func TestMemoryBudgetStoreCRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryBudgetStore()

	if got, err := s.GetBudget(ctx, "t1"); err != nil || got != nil {
		t.Fatalf("empty get: got=%v err=%v", got, err)
	}

	want := &TokenBudget{TenantID: "t1", DailyLimit: 100, UsedToday: 30}
	if err := s.UpsertBudget(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBudget(ctx, "t1")
	if err != nil || got == nil || got.DailyLimit != 100 || got.UsedToday != 30 {
		t.Fatalf("get after upsert: got=%+v err=%v", got, err)
	}
	// 深拷贝：外部修改返回值不应污染内部状态。
	got.UsedToday = 999
	again, _ := s.GetBudget(ctx, "t1")
	if again.UsedToday != 30 {
		t.Errorf("store must deep-copy on read, got UsedToday=%d", again.UsedToday)
	}

	list, err := s.ListBudgets(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: len=%d err=%v", len(list), err)
	}
}

func TestMemoryBudgetStoreAlerts(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryBudgetStore()
	for i := 0; i < 5; i++ {
		if err := s.RecordAlert(ctx, BudgetAlert{TenantID: "t1", Type: "daily", Used: i}); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := s.ListAlerts(ctx, "t1", 0)
	if len(all) != 5 {
		t.Errorf("want 5 alerts, got %d", len(all))
	}
	recent, _ := s.ListAlerts(ctx, "t1", 2)
	if len(recent) != 2 || recent[0].Used != 3 || recent[1].Used != 4 {
		t.Errorf("recent 2 alerts wrong: %+v", recent)
	}
}
