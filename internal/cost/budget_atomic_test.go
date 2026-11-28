package cost

import (
	"context"
	"errors"
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

// TestRecordUsagePersistsOverageAndBlocksNext 钉住真实付费后的记账语义：
// 最后一次响应可能已经越限，但必须记录真实用量，否则后续请求会永远看到限内快照。
func TestRecordUsagePersistsOverageAndBlocksNext(t *testing.T) {
	s := NewMemoryBudgetStore()
	bm := NewBudgetManager(s)
	const tenant = "tenant-actual-overage"
	bm.SetBudget(tenant, 50, 1000, 0.8)
	if ok, _ := bm.RecordUsage(tenant, 40); !ok {
		t.Fatal("限内用量应放行")
	}
	if ok, _ := bm.RecordUsage(tenant, 20); ok {
		t.Fatal("真实用量越限后应返回 false")
	}
	got, err := s.GetBudget(context.Background(), tenant)
	if err != nil || got == nil || got.UsedToday != 60 || got.UsedMonth != 60 {
		t.Fatalf("越限响应的真实用量必须落库: budget=%+v err=%v", got, err)
	}
	if !bm.IsOverBudget(tenant) {
		t.Fatal("越限入账后必须阻断下一次付费调用")
	}
}

// TestBudgetManagerConfigDoesNotRestoreStaleUsage 模拟两个控制面副本：
// 副本二持有旧快照时修改配置，不得把副本一已入账的用量覆盖掉。
func TestBudgetManagerConfigDoesNotRestoreStaleUsage(t *testing.T) {
	s := NewMemoryBudgetStore()
	bm1 := NewBudgetManager(s)
	bm2 := NewBudgetManager(s)
	const tenant = "tenant-stale-config"
	bm1.SetBudget(tenant, 50, 1000, 0.8)
	_ = bm1.GetBudget(tenant)
	_ = bm2.GetBudget(tenant)
	if ok, _ := bm1.RecordUsage(tenant, 30); !ok {
		t.Fatal("限内用量应放行")
	}
	bm2.SetBudget(tenant, 100, 2000, 0.9)
	got, err := s.GetBudget(context.Background(), tenant)
	if err != nil || got == nil || got.UsedToday != 30 || got.UsedMonth != 30 {
		t.Fatalf("旧配置快照回滚了用量: budget=%+v err=%v", got, err)
	}
}

func TestMemoryConsumeIfUnderLimitRejectsOverflow(t *testing.T) {
	s := NewMemoryBudgetStore()
	const tenant = "tenant-overflow"
	if err := s.UpsertBudget(context.Background(), &TokenBudget{
		TenantID: tenant, DailyLimit: 0, MonthlyLimit: 0, UsedToday: 1, UsedMonth: 1,
	}); err != nil {
		t.Fatal(err)
	}
	maxInt := int(^uint(0) >> 1)
	if ok, _, err := s.ConsumeIfUnderLimit(context.Background(), tenant, maxInt); err == nil || ok {
		t.Fatalf("整数溢出必须 fail-closed: ok=%v err=%v", ok, err)
	}
}

type failingGetBudgetStore struct {
	*MemoryBudgetStore
	fail atomic.Bool
}

func (s *failingGetBudgetStore) GetBudget(ctx context.Context, tenantID string) (*TokenBudget, error) {
	if s.fail.Load() {
		return nil, errors.New("budget store unavailable")
	}
	return s.MemoryBudgetStore.GetBudget(ctx, tenantID)
}

func TestIsOverBudgetFailsClosedWithWarmCache(t *testing.T) {
	s := &failingGetBudgetStore{MemoryBudgetStore: NewMemoryBudgetStore()}
	bm := NewBudgetManager(s)
	const tenant = "tenant-warm-cache"
	bm.SetBudget(tenant, 50, 1000, 0.8)
	_ = bm.GetBudget(tenant)
	s.fail.Store(true)
	if !bm.IsOverBudget(tenant) {
		t.Fatal("本地缓存不得掩盖 SQL 存储故障")
	}
}

func TestBudgetAlertDedupAcrossManagersAndReload(t *testing.T) {
	s := NewMemoryBudgetStore()
	bm1 := NewBudgetManager(s)
	bm2 := NewBudgetManager(s)
	const tenant = "tenant-alert-reload"
	bm1.SetBudget(tenant, 100, 100, 0.8)
	alertCount := 0
	for i := 0; i < 10; i++ {
		bm := bm1
		if i%2 == 1 {
			bm = bm2
		}
		_, alerts := bm.RecordUsage(tenant, 10)
		alertCount += len(alerts)
	}
	if alertCount != 2 {
		t.Fatalf("日/月告警应各恰好一次，实际=%d", alertCount)
	}
	bm3 := NewBudgetManager(s)
	if _, alerts := bm3.RecordUsage(tenant, 0); len(alerts) != 0 {
		t.Fatalf("重载后重复告警: %+v", alerts)
	}
}
