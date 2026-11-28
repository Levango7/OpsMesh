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

// TestSQLBudgetManagerOverageAndConfigSafety 走真实 BudgetManager 调用链，
// 防止存储单测绿、上层又用陈旧快照把原子用量覆盖回去。
func TestSQLBudgetManagerOverageAndConfigSafety(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping SQL budget manager integration test")
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
	const tenant = "tenant-sql-manager-safety"
	if _, err := st1.DB().Exec(`DELETE FROM budgets WHERE tenant_id=?`, tenant); err != nil {
		t.Fatal(err)
	}
	bm1 := cost.NewBudgetManager(st1)
	bm2 := cost.NewBudgetManager(st2)
	bm1.SetBudget(tenant, 50, 1000, 0.8)
	_ = bm1.GetBudget(tenant)
	_ = bm2.GetBudget(tenant)
	if ok, _ := bm1.RecordUsage(tenant, 30); !ok {
		t.Fatal("首副本限内用量应放行")
	}
	bm2.SetBudget(tenant, 100, 2000, 0.9)
	got, err := st1.GetBudget(context.Background(), tenant)
	if err != nil || got == nil || got.UsedToday != 30 || got.UsedMonth != 30 {
		t.Fatalf("第二副本配置更新回滚了用量: budget=%+v err=%v", got, err)
	}
	bm2.SetBudget(tenant, 50, 1000, 0.8)
	if ok, _ := bm1.RecordUsage(tenant, 40); ok {
		t.Fatal("实际用量越限后 RecordUsage 应返回 false")
	}
	got, err = st2.GetBudget(context.Background(), tenant)
	if err != nil || got == nil || got.UsedToday != 70 || got.UsedMonth != 70 {
		t.Fatalf("越限响应的真实用量未落库: budget=%+v err=%v", got, err)
	}
	if !bm1.IsOverBudget(tenant) || !bm2.IsOverBudget(tenant) {
		t.Fatal("两个副本都必须在下一次付费调用前看到越限")
	}
}

func TestSQLBudgetAlertDedupAcrossManagers(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping SQL budget alert integration test")
	}
	st, err := NewSQLStore(dsn, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.DB().Close() }()
	const tenant = "tenant-sql-alert-dedup"
	if _, err := st.DB().Exec(`DELETE FROM budgets WHERE tenant_id=?`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`DELETE FROM budget_alerts WHERE tenant_id=?`, tenant); err != nil {
		t.Fatal(err)
	}
	bm1 := cost.NewBudgetManager(st)
	bm2 := cost.NewBudgetManager(st)
	bm1.SetBudget(tenant, 100, 100, 0.8)
	var wg sync.WaitGroup
	var alertCount int64
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			bm := bm1
			if i%2 == 1 {
				bm = bm2
			}
			_, alerts := bm.RecordUsage(tenant, 10)
			atomic.AddInt64(&alertCount, int64(len(alerts)))
		}(i)
	}
	wg.Wait()
	if alertCount != 2 {
		t.Fatalf("日/月告警应各恰好一次，实际=%d", alertCount)
	}
	got, err := st.GetBudget(context.Background(), tenant)
	if err != nil || got == nil || got.UsedToday != 100 || got.UsedMonth != 100 {
		t.Fatalf("并发实际用量 = %+v, want 100/100 (err=%v)", got, err)
	}
	// 新管理器模拟重启：已告警状态必须从存储恢复，不能再次发同类告警。
	bm3 := cost.NewBudgetManager(st)
	if _, alerts := bm3.RecordUsage(tenant, 0); len(alerts) != 0 {
		t.Fatalf("重启后重复告警: %+v", alerts)
	}
}

func TestSQLInsertDefaultBudgetPreservesExistingRow(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping SQL budget insert integration test")
	}
	st, err := NewSQLStore(dsn, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.DB().Close() }()
	const tenant = "tenant-sql-default-insert"
	if err := st.UpsertBudget(context.Background(), &TokenBudget{
		TenantID: tenant, DailyLimit: 77, MonthlyLimit: 777, UsedToday: 17, UsedMonth: 27,
		AlertThreshold: 0.6, Timezone: "UTC", Alerted: map[string]bool{"daily:0.6": true},
	}); err != nil {
		t.Fatal(err)
	}
	tx, err := st.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.insertDefaultBudget(context.Background(), tx, &TokenBudget{
		TenantID: tenant, DailyLimit: 100000, MonthlyLimit: 1000000, AlertThreshold: 0.8, Timezone: "UTC",
	}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetBudget(context.Background(), tenant)
	if err != nil || got == nil || got.DailyLimit != 77 || got.UsedToday != 17 || got.UsedMonth != 27 || !got.Alerted["daily:0.6"] {
		t.Fatalf("默认首插覆盖了既有预算行: %+v (err=%v)", got, err)
	}
}
