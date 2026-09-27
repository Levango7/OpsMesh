package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestSafeGo_RecoversAndRestarts 验证 panic 循环被捕获并重启（CB-9）。
// fn 前两次调用 panic，第三次正常返回计数；断言 safeGo 至少执行到第三次
// （即 panic 后确有重启），且测试进程不崩溃（recover 生效的直接证据）。
// 计数走 atomic + channel 回传：裸 int 会被循环 goroutine 与测试协程并发读写，
// CI 带 -race 时会直接判失败（同 TestSafeGo_EarlyReturnRestarts 的处理）。
func TestSafeGo_RecoversAndRestarts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	calls := make(chan int, 4)
	var n atomic.Int32
	safeGo(ctx, "test-panic-loop", func(context.Context) {
		seq := int(n.Add(1))
		calls <- seq
		if seq <= 2 {
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
	// 等 panic→重启链路走完（两次 panic + 第三次正常进入）。只从 channel 取数，
	// 不去读循环侧持有的变量——那正是要修的 data race。
	got := 1
	timeout := time.After(3*safeGoRestartDelay + 2*time.Second)
	for got < 3 {
		select {
		case <-calls:
			got++
		case <-timeout:
			t.Fatalf("panic 后未重启：期望至少 3 次调用，实际 %d", got)
		}
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

// TestSafeGo_EarlyReturnRestarts 验证 fn 未 panic 但提前 return（ctx 未取消）时循环仍被重启。
// 真实场景：logCollectLoop 在未配置采集路径时立即 return。此时**不得**记为 panic
// （措辞区分见 safego.go），但重启本身必须发生——否则该能力永久静默缺失。
// 用带缓冲 channel 传递计数，避免测试侧与循环侧的 data race（CI 带 -race）。
func TestSafeGo_EarlyReturnRestarts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make(chan struct{}, 4)
	safeGo(ctx, "test-early-return", func(context.Context) {
		select {
		case calls <- struct{}{}:
		default:
		}
	})
	got := 0
	deadline := time.After(safeGoRestartDelay + 3*time.Second)
	for got < 2 {
		select {
		case <-calls:
			got++
		case <-deadline:
			t.Fatalf("提前 return 的循环未被重启：期望至少 2 次调用，实际 %d", got)
		}
	}
}
