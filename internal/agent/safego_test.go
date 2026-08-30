package agent

import (
	"context"
	"testing"
	"time"
)

// TestSafeGo_RecoversAndRestarts 验证 panic 循环被捕获并重启（CB-9）。
// fn 前两次调用 panic，第三次正常返回计数；断言 safeGo 至少执行到第三次
// （即 panic 后确有重启），且测试进程不崩溃（recover 生效的直接证据）。
func TestSafeGo_RecoversAndRestarts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	calls := make(chan int, 4)
	n := 0
	safeGo(ctx, "test-panic-loop", func(context.Context) {
		n++
		calls <- n
		if n <= 2 {
			panic("boom")
		}
		// 第三次：正常挂住直到 ctx 取消，模拟真实循环的 for-select。
		<-ctx.Done()
	})

	select {
	case <-calls: // 第一次调用已发生
	case <-time.After(2 * time.Second):
		t.Fatal("safeGo 未启动循环")
	}
	// 等待 panic→重启链路走完（两次 panic + 第三次正常进入）。
	deadline := time.Now().Add(3*safeGoRestartDelay + 2*time.Second)
	for n < 3 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n < 3 {
		t.Fatalf("panic 后未重启：期望 n>=3，实际 n=%d", n)
	}
}

// TestSafeGo_NoRestartOnCleanExit 验证正常退出（ctx 取消）不重启。
func TestSafeGo_NoRestartOnCleanExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	safeGo(ctx, "test-clean-exit", func(context.Context) {
		defer close(done)
		<-ctx.Done()
	})
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("循环未随 ctx 退出")
	}
}
