package cost

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestMemoryConsumeIfUnderLimitIsAtomic 钉并发硬限：
// 10 个并发各消费 10 token，日限 50，必须恰好 5 个放行、5 个拒绝，总量不突破。
func TestMemoryConsumeIfUnderLimitIsAtomic(t *testing.T) {
	s := NewMemoryBudgetStore()
	const tenant = "tenant-atomic"
	if err := s.UpsertBudget(context.Background(), &TokenBudget{
		TenantID: tenant, DailyLimit: 50, MonthlyLimit: 1000,
		AlertThreshold: 0.8, Timezone: "UTC", Alerted: make(map[string]bool),
	}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var allowed int64
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _, err := s.ConsumeIfUnderLimit(context.Background(), tenant, 10)
			if err != nil {
				t.Errorf("consume error: %v", err)
				return
			}
			if ok {
				atomic.AddInt64(&allowed, 1)
			}
		}()
	}
	wg.Wait()
	if allowed != 5 {
		t.Fatalf("放行数 = %d, want 5", allowed)
	}
	b, err := s.GetBudget(context.Background(), tenant)
	if err != nil || b == nil {
		t.Fatalf("读取预算失败: %v", err)
	}
	if b.UsedToday != 50 || b.UsedMonth != 50 {
		t.Fatalf("用量 = today=%d month=%d, want 50/50", b.UsedToday, b.UsedMonth)
	}
}

// TestMemoryConsumeIfUnderLimitWindowReset 验证日/月窗口重置在原子操作内完成。
func TestMemoryConsumeIfUnderLimitWindowReset(t *testing.T) {
	s := NewMemoryBudgetStore()
	const tenant = "tenant-window"
	yesterday := time.Now().AddDate(0, 0, -1)
	lastMonth := time.Now().AddDate(0, -1, 0)
	if err := s.UpsertBudget(context.Background(), &TokenBudget{
		TenantID: tenant, DailyLimit: 50, MonthlyLimit: 100,
		UsedToday: 49, UsedMonth: 99, LastReset: yesterday, MonthReset: lastMonth,
		AlertThreshold: 0.8, Timezone: "UTC", Alerted: make(map[string]bool),
	}); err != nil {
		t.Fatal(err)
	}
	ok, usage, err := s.ConsumeIfUnderLimit(context.Background(), tenant, 10)
	if err != nil || !ok {
		t.Fatalf("跨窗口后应重置并放行: ok=%v err=%v", ok, err)
	}
	if usage.UsedToday != 10 || usage.UsedMonth != 10 {
		t.Fatalf("窗口重置后用量 = today=%d month=%d, want 10/10", usage.UsedToday, usage.UsedMonth)
	}
}

// TestMemoryConsumeIfUnderLimitRejectsNegative 拒绝负 token，防回卷。
func TestMemoryConsumeIfUnderLimitRejectsNegative(t *testing.T) {
	s := NewMemoryBudgetStore()
	if ok, _, err := s.ConsumeIfUnderLimit(context.Background(), "tenant-negative", -1); err == nil || ok {
		t.Fatalf("负 token 应拒绝: ok=%v err=%v", ok, err)
	}
}
