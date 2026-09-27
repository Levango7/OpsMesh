package leader

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// newTestMySQLLeaseOps 打开测试用 MySQL（OPSMESH_TEST_MYSQL_DSN 门控，与根模块
// 集成测试同一约定）。表名固定，测试间用随机 holder/lease 名隔离。
func newTestMySQLLeaseOps(t *testing.T) (*MySQLLeaseOps, func()) {
	t.Helper()
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping leader MySQL integration test")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	ops := NewMySQLLeaseOps(db)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ops.EnsureTable(ctx); err != nil {
		db.Close()
		t.Fatalf("ensure table: %v", err)
	}
	return ops, func() { db.Close() }
}

// TestMySQLLeaseOps_AcquireRenewRelease 全生命周期：acquire → 持有校验 → renew → release。
func TestMySQLLeaseOps_AcquireRenewRelease(t *testing.T) {
	ops, cleanup := newTestMySQLLeaseOps(t)
	defer cleanup()
	ctx := context.Background()
	lease := "lease-test-full"
	holder := "pod-a-" + time.Now().Format("150405.000")

	if !ops.Acquire(ctx, lease, holder, 10*time.Second) {
		t.Fatal("空表上首次 Acquire 应成功")
	}
	if !ops.Acquire(ctx, lease, holder, 10*time.Second) {
		t.Fatal("持有者重复 Acquire 应成功（幂等续期）")
	}
	if !ops.Renew(ctx, lease, holder, 10*time.Second) {
		t.Fatal("持有者 Renew 应成功")
	}
	if err := ops.Release(ctx, lease, holder); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if ops.Renew(ctx, lease, holder, 10*time.Second) {
		t.Fatal("释放后 Renew 应失败（已不是持有者）")
	}
}

// TestMySQLLeaseOps_ContendedAcquire 单一赢家：两个持有者争同一租约，
// 后到者必须拿不到未过期的租约。
func TestMySQLLeaseOps_ContendedAcquire(t *testing.T) {
	ops, cleanup := newTestMySQLLeaseOps(t)
	defer cleanup()
	ctx := context.Background()
	lease := "lease-test-contend"
	a := "pod-a-" + time.Now().Format("150405.000")
	b := "pod-b-" + time.Now().Format("150405.000")

	if !ops.Acquire(ctx, lease, a, 60*time.Second) {
		t.Fatal("A 首次 Acquire 应成功")
	}
	if ops.Acquire(ctx, lease, b, 60*time.Second) {
		t.Fatal("B 在 A 持有未过期租约时 Acquire 必须失败")
	}
	if !ops.Renew(ctx, lease, a, 60*time.Second) {
		t.Fatal("A 的 Renew 不应被 B 的失败尝试影响")
	}
}

// TestMySQLLeaseOps_StateMachineMultiReplica 端到端走一遍 K8sLeaseElector 状态机：
// A 当选 → A 失联（不 Renew）→ 租约过期 → B 接管 → A 恢复后拿不回 leader。
// 用短 TTL 让"过期"发生在真实时钟上。
func TestMySQLLeaseOps_StateMachineMultiReplica(t *testing.T) {
	ops, cleanup := newTestMySQLLeaseOps(t)
	defer cleanup()
	ctx := context.Background()
	lease := "lease-test-failover"
	ttl := 1200 * time.Millisecond

	a := NewK8sLease(ops, lease, "pod-a")
	b := NewK8sLease(ops, lease, "pod-b")

	if !a.Renew(ctx, ttl) {
		t.Fatal("A 首次 Renew（acquire）应成功")
	}
	if !a.IsLeader() {
		t.Fatal("A 应持有 leader 缓存状态")
	}
	if b.Renew(ctx, ttl) {
		t.Fatal("A 未失联时 B 不得接管")
	}
	if b.IsLeader() {
		t.Fatal("B 不应缓存 leader 状态")
	}

	// A 失联：等 TTL 过期（不放 Renew）。
	time.Sleep(ttl + 400*time.Millisecond)

	if !b.Renew(ctx, ttl) {
		t.Fatal("租约过期后 B 应接管")
	}
	if !b.IsLeader() {
		t.Fatal("B 应缓存 leader 状态")
	}
	if a.Renew(ctx, ttl) {
		t.Fatal("B 持有期间 A 不得续租")
	}
	if a.IsLeader() {
		t.Fatal("A 的 leader 缓存应被 Renew 失败刷新为 false")
	}

	// 优雅退出：B 释放后 A 可立即接管（不必等 TTL）。
	if err := b.Close(); err != nil {
		t.Fatalf("B Close: %v", err)
	}
	if !a.Renew(ctx, ttl) {
		t.Fatal("B 释放后 A 应能立即接管")
	}
}

// TestMySQLLeaseOps_ConcurrentAcquire 并发抢锁：N 个 goroutine 争同一租约，
// 恰好一个赢家（InnoDB 行锁 + 条件更新的原子性回归）。
func TestMySQLLeaseOps_ConcurrentAcquire(t *testing.T) {
	ops, cleanup := newTestMySQLLeaseOps(t)
	defer cleanup()
	ctx := context.Background()
	lease := "lease-test-concurrent"

	const n = 8
	winners := make([]bool, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// 每个竞争者必须持唯一身份：相同 holder 的两次 Acquire 都会成功，
			// 那验证不了单一赢家语义。
			holder := fmt.Sprintf("pod-%s-%d", time.Now().Format("150405.000"), i)
			winners[i] = ops.Acquire(ctx, lease, holder, 30*time.Second)
		}(i)
	}
	wg.Wait()

	count := 0
	for _, w := range winners {
		if w {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("并发 Acquire 应恰好 1 个赢家，实际 %d", count)
	}
}
