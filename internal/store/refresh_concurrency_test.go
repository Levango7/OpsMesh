// refresh_concurrency_test.go 是 ConsumeRefreshToken 的并发回归测试（内存侧）。
//
// 背景：internal/store/sqlstore/sql_refresh.go 的 ConsumeRefreshToken 曾存在并发双消费 bug
// （Get→Delete 两步非原子，多副本并发下同一 refresh token 可被消费多次）。
// 修复后 MemoryStore 用互斥锁（SQLStore 侧见 internal/store/sqlstore）。
// 本文件回归验证：N 个 goroutine 并发消费同一 token，
// 仅一个成功，其余返回 (nil, false)。
//
// 测试分层（TD-61 批次 3-sql 后本文件仅余内存侧）：
//  1. MemoryStore 并发（始终运行，无外部依赖）；

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
