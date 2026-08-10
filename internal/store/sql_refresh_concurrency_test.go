// sql_refresh_concurrency_test.go 是 ConsumeRefreshToken 的并发回归测试。
//
// 背景：internal/store/sql_refresh.go 的 ConsumeRefreshToken 曾存在并发双消费 bug
// （Get→Delete 两步非原子，多副本并发下同一 refresh token 可被消费多次）。
// 修复后 MemoryStore 用互斥锁、SQLStore 用事务内 SELECT ... FOR UPDATE + DELETE
// 并校验 RowsAffected。本文件回归验证：N 个 goroutine 并发消费同一 token，
// 仅一个成功，其余返回 (nil, false)。
//
// 测试分层：
//  1. MemoryStore 并发（始终运行，无外部依赖）；
//  2. SQLStore 并发（需 OPSMESH_TEST_MYSQL_DSN，未设置时 t.Skip）。
package store

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// MemoryStore：ConsumeRefreshToken 并发回归
// ============================================================================

// TestConsumeRefreshToken_MemoryStoreConcurrent 启动 N 个 goroutine 同时消费同一
// refresh token，验证仅一个成功，其余返回 (nil, false)。
//
// 回归目标：MemoryStore.ConsumeRefreshToken 在互斥锁保护下完成 Get+Delete 原子操作，
// 并发下 successCount 必须恰好为 1。
func TestConsumeRefreshToken_MemoryStoreConcurrent(t *testing.T) {
	m := NewMemoryStore()
	rt := &RefreshToken{
		TokenHash: "test-hash-concurrent",
		UserID:    "user-1",
		TenantID:  "default",
		DeviceFP:  "fp-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := m.SaveRefreshToken(rt); err != nil {
		t.Fatalf("SaveRefreshToken: %v", err)
	}

	const N = 100
	var successCount int32
	var failCount int32
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			got, ok := m.ConsumeRefreshToken("test-hash-concurrent")
			if ok {
				if got == nil {
					t.Error("ok=true 但返回 nil token")
					return
				}
				if got.TokenHash != "test-hash-concurrent" {
					t.Errorf("返回 token hash 错误: got=%q", got.TokenHash)
					return
				}
				atomic.AddInt32(&successCount, 1)
			} else {
				if got != nil {
					t.Error("ok=false 但返回非 nil token")
					return
				}
				atomic.AddInt32(&failCount, 1)
			}
		}()
	}
	wg.Wait()

	if successCount != 1 {
		t.Errorf("expected exactly 1 success, got %d", successCount)
	}
	if failCount != N-1 {
		t.Errorf("expected exactly %d failures, got %d", N-1, failCount)
	}
	// 消费后 token 应已从 store 中删除。
	if got := m.GetRefreshToken("test-hash-concurrent"); got != nil {
		t.Errorf("消费后 GetRefreshToken 应返回 nil; got=%+v", got)
	}
}

// TestConsumeRefreshToken_MemoryStoreNonExistent 消费不存在的 token 应返回 (nil, false)。
func TestConsumeRefreshToken_MemoryStoreNonExistent(t *testing.T) {
	m := NewMemoryStore()
	got, ok := m.ConsumeRefreshToken("non-existent-hash")
	if ok {
		t.Fatal("不存在的 token 不应消费成功")
	}
	if got != nil {
		t.Fatalf("不存在的 token 应返回 nil; got=%+v", got)
	}
}

// TestConsumeRefreshToken_MemoryStoreEmptyHash 消费空 hash 应返回 (nil, false)。
func TestConsumeRefreshToken_MemoryStoreEmptyHash(t *testing.T) {
	m := NewMemoryStore()
	// 先存一个 token，确保空 hash 失败不是因为 store 为空。
	rt := &RefreshToken{
		TokenHash: "some-hash",
		UserID:    "user-1",
		TenantID:  "default",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := m.SaveRefreshToken(rt); err != nil {
		t.Fatalf("SaveRefreshToken: %v", err)
	}

	got, ok := m.ConsumeRefreshToken("")
	if ok {
		t.Fatal("空 hash 不应消费成功")
	}
	if got != nil {
		t.Fatalf("空 hash 应返回 nil; got=%+v", got)
	}
	// 空 hash 不应误删已有 token。
	if got := m.GetRefreshToken("some-hash"); got == nil {
		t.Fatal("空 hash 消费不应影响已有 token")
	}
}

// TestConsumeRefreshToken_MemoryStoreDoubleConsume 先消费一次成功，
// 再消费同一 token 应失败（防重放）。
func TestConsumeRefreshToken_MemoryStoreDoubleConsume(t *testing.T) {
	m := NewMemoryStore()
	rt := &RefreshToken{
		TokenHash: "test-hash-double",
		UserID:    "user-2",
		TenantID:  "default",
		DeviceFP:  "fp-2",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := m.SaveRefreshToken(rt); err != nil {
		t.Fatalf("SaveRefreshToken: %v", err)
	}

	// 第一次消费：应成功。
	got1, ok1 := m.ConsumeRefreshToken("test-hash-double")
	if !ok1 {
		t.Fatal("第一次消费应成功")
	}
	if got1 == nil || got1.TokenHash != "test-hash-double" {
		t.Fatalf("第一次消费返回的 token 错误: %+v", got1)
	}

	// 第二次消费同一 token：应失败（已被删除）。
	got2, ok2 := m.ConsumeRefreshToken("test-hash-double")
	if ok2 {
		t.Fatal("第二次消费同一 token 不应成功")
	}
	if got2 != nil {
		t.Fatalf("第二次消费应返回 nil; got=%+v", got2)
	}
}

// ============================================================================
// SQLStore：ConsumeRefreshToken 并发回归（需真实 MySQL，默认跳过）
// ============================================================================

// TestConsumeRefreshToken_SQLStoreConcurrent 用真实 MySQL 验证 SQLStore 的并发消费。
//
// 运行：
//
//	OPSMESH_TEST_MYSQL_DSN="user:pass@tcp(127.0.0.1:3306)/opsmesh?parseTime=true" \
//	go test ./internal/store/ -run TestConsumeRefreshToken_SQLStoreConcurrent -v
//
// 回归目标：SQLStore.ConsumeRefreshToken 在事务内 SELECT ... FOR UPDATE + DELETE
// 并校验 RowsAffected，并发下 successCount 必须恰好为 1。
func TestConsumeRefreshToken_SQLStoreConcurrent(t *testing.T) {
	// 复用 migration_test.go 中的 newTestSQLStore：创建临时库 + NewSQLStore + cleanup。
	// 未设置 OPSMESH_TEST_MYSQL_DSN 时内部会 t.Skip。
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	rt := &RefreshToken{
		TokenHash: "sql-test-hash-concurrent",
		UserID:    "user-sql-1",
		TenantID:  "default",
		DeviceFP:  "fp-sql-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := s.SaveRefreshToken(rt); err != nil {
		t.Fatalf("SaveRefreshToken: %v", err)
	}

	const N = 50
	var successCount int32
	var failCount int32
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			got, ok := s.ConsumeRefreshToken("sql-test-hash-concurrent")
			if ok {
				if got == nil {
					t.Error("ok=true 但返回 nil token")
					return
				}
				if got.TokenHash != "sql-test-hash-concurrent" {
					t.Errorf("返回 token hash 错误: got=%q", got.TokenHash)
					return
				}
				atomic.AddInt32(&successCount, 1)
			} else {
				if got != nil {
					t.Error("ok=false 但返回非 nil token")
					return
				}
				atomic.AddInt32(&failCount, 1)
			}
		}()
	}
	wg.Wait()

	if successCount != 1 {
		t.Errorf("expected exactly 1 success, got %d", successCount)
	}
	if failCount != N-1 {
		t.Errorf("expected exactly %d failures, got %d", N-1, failCount)
	}
	// 消费后 token 应已从 DB 中删除。
	if got := s.GetRefreshToken("sql-test-hash-concurrent"); got != nil {
		t.Errorf("消费后 GetRefreshToken 应返回 nil; got=%+v", got)
	}
}

// TestConsumeRefreshToken_SQLStoreDoubleConsume 用真实 MySQL 验证 SQLStore 双消费失败。
func TestConsumeRefreshToken_SQLStoreDoubleConsume(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	rt := &RefreshToken{
		TokenHash: "sql-test-hash-double",
		UserID:    "user-sql-2",
		TenantID:  "default",
		DeviceFP:  "fp-sql-2",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := s.SaveRefreshToken(rt); err != nil {
		t.Fatalf("SaveRefreshToken: %v", err)
	}

	// 第一次消费：应成功。
	got1, ok1 := s.ConsumeRefreshToken("sql-test-hash-double")
	if !ok1 {
		t.Fatal("第一次消费应成功")
	}
	if got1 == nil || got1.TokenHash != "sql-test-hash-double" {
		t.Fatalf("第一次消费返回的 token 错误: %+v", got1)
	}

	// 第二次消费同一 token：应失败（已被删除）。
	got2, ok2 := s.ConsumeRefreshToken("sql-test-hash-double")
	if ok2 {
		t.Fatal("第二次消费同一 token 不应成功")
	}
	if got2 != nil {
		t.Fatalf("第二次消费应返回 nil; got=%+v", got2)
	}
}

// TestConsumeRefreshToken_SQLStoreNonExistent 用真实 MySQL 验证消费不存在的 token 返回 (nil, false)。
func TestConsumeRefreshToken_SQLStoreNonExistent(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	got, ok := s.ConsumeRefreshToken("sql-non-existent-hash")
	if ok {
		t.Fatal("不存在的 token 不应消费成功")
	}
	if got != nil {
		t.Fatalf("不存在的 token 应返回 nil; got=%+v", got)
	}
}

// TestConsumeRefreshToken_SQLStoreEmptyHash 用真实 MySQL 验证消费空 hash 返回 (nil, false)。
func TestConsumeRefreshToken_SQLStoreEmptyHash(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	// 先存一个 token，确保空 hash 失败不是因为 store 为空。
	rt := &RefreshToken{
		TokenHash: "sql-some-hash",
		UserID:    "user-sql-3",
		TenantID:  "default",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := s.SaveRefreshToken(rt); err != nil {
		t.Fatalf("SaveRefreshToken: %v", err)
	}

	got, ok := s.ConsumeRefreshToken("")
	if ok {
		t.Fatal("空 hash 不应消费成功")
	}
	if got != nil {
		t.Fatalf("空 hash 应返回 nil; got=%+v", got)
	}
	// 空 hash 不应误删已有 token。
	if got := s.GetRefreshToken("sql-some-hash"); got == nil {
		t.Fatal("空 hash 消费不应影响已有 token")
	}
}
