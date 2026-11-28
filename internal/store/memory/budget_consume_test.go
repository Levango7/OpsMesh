package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

// TestMemoryStoreConsumeIfUnderLimitIsAtomic 验证 dev/单副本存储的并发硬限。
func TestMemoryStoreConsumeIfUnderLimitIsAtomic(t *testing.T) {
	m := NewMemoryStore()
	const tenant = "tenant-mem-atomic"
	if err := m.UpsertBudget(context.Background(), &TokenBudget{
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
			ok, _, err := m.ConsumeIfUnderLimit(context.Background(), tenant, 10)
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
	b, err := m.GetBudget(context.Background(), tenant)
	if err != nil || b == nil {
		t.Fatalf("读取预算失败: %v", err)
	}
	if b.UsedToday != 50 || b.UsedMonth != 50 {
		t.Fatalf("用量 = today=%d month=%d, want 50/50", b.UsedToday, b.UsedMonth)
	}
}
