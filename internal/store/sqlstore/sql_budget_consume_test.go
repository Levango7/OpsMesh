package sqlstore

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Levango7/OpsMesh/internal/cost"
)

// TestSQLConsumeIfUnderLimitConcurrent 用真 MySQL 验证行锁硬限：
// 10 个并发各消费 10 token，日限 50，必须恰好 5 个放行、5 个拒绝，总量不突破。
// 未设置 OPSMESH_TEST_MYSQL_DSN 时按既有约定跳过。
func TestSQLConsumeIfUnderLimitConcurrent(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping SQL budget atomicity integration test")
	}
	st, err := NewSQLStore(dsn, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.DB().Close() }()
	const tenant = "tenant-sql-atomic"
	if _, err := st.DB().Exec(`DELETE FROM budgets WHERE tenant_id=?`, tenant); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertBudget(context.Background(), &TokenBudget{
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
			ok, _, err := st.ConsumeIfUnderLimit(context.Background(), tenant, 10)
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
	b, err := st.GetBudget(context.Background(), tenant)
	if err != nil || b == nil {
		t.Fatalf("读取预算失败: %v", err)
	}
	if b.UsedToday != 50 || b.UsedMonth != 50 {
		t.Fatalf("用量 = today=%d month=%d, want 50/50", b.UsedToday, b.UsedMonth)
	}
}

// TestSQLBudgetStoreErrorFailsClosed 验证存储故障不是「无限制」：
// 预检 IsOverBudget 与记账 RecordUsage 在 DB 断开时都必须拒绝，而非放行。
func TestSQLBudgetStoreErrorFailsClosed(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping SQL budget fail-closed integration test")
	}
	st, err := NewSQLStore(dsn, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = st.DB().Close() // 模拟存储故障。

	bm := cost.NewBudgetManager(st)
	if !bm.IsOverBudget("tenant-db-down") {
		t.Fatal("DB 断开时 IsOverBudget 应 fail-closed 返回 true")
	}
	if ok, _ := bm.RecordUsage("tenant-db-down", 1); ok {
		t.Fatal("DB 断开时 RecordUsage 应 fail-closed 返回 false")
	}
}

// TestSQLConsumeIfUnderLimitReloadAndReplica 验证重启读回与「两副本」共享 SQL 后
// 仍只有一份全局用量，不会被各自内存状态放大。
func TestSQLConsumeIfUnderLimitReloadAndReplica(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping SQL budget reload integration test")
	}
	st1, err := NewSQLStore(dsn, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st1.DB().Close() }()
	st2, err := NewSQLStore(dsn, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st2.DB().Close() }()
	const tenant = "tenant-sql-replica"
	if _, err := st1.DB().Exec(`DELETE FROM budgets WHERE tenant_id=?`, tenant); err != nil {
		t.Fatal(err)
	}
	if err := st1.UpsertBudget(context.Background(), &TokenBudget{
		TenantID: tenant, DailyLimit: 50, MonthlyLimit: 1000,
		AlertThreshold: 0.8, Timezone: "UTC", Alerted: make(map[string]bool),
	}); err != nil {
		t.Fatal(err)
	}

	ok, _, err := st1.ConsumeIfUnderLimit(context.Background(), tenant, 20)
	if err != nil || !ok {
		t.Fatalf("首副本消费应放行: ok=%v err=%v", ok, err)
	}
	// 模拟第二个控制面实例读取同一 SQL：用量必须立即可见且不能重复扣减。
	b, err := st2.GetBudget(context.Background(), tenant)
	if err != nil || b == nil {
		t.Fatalf("第二副本读取失败: %v", err)
	}
	if b.UsedToday != 20 || b.UsedMonth != 20 {
		t.Fatalf("第二副本用量 = today=%d month=%d, want 20/20", b.UsedToday, b.UsedMonth)
	}
	ok, _, err = st2.ConsumeIfUnderLimit(context.Background(), tenant, 35)
	if err != nil || ok {
		t.Fatalf("第二副本越过日限应拒绝: ok=%v err=%v", ok, err)
	}
}
