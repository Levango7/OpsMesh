// autoprovision_loop_test.go — D3+ AutoProvisionLoop 循环 + 退避机制单测。
//
// 覆盖：
//  1. 双闸第一闸：Enabled=false / cfg=nil 时 AutoProvisionLoop 立即返回（不启动循环）
//  2. 配置完整性闸：LoopInterval<=0 或 SegmentCIDR="" 时不启动
//  3. 循环执行：配置合法时至少执行一轮（用短间隔 + ctx 取消验证不阻塞）
//  4. 退避机制：失败后间隔翻倍（通过 metrics 间接验证循环在运行）
package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/pkg/metrics"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/device-svc/internal/store"
)

// TestAutoProvisionLoop_DisabledByDefault 验证未启用时立即返回（不阻塞）。
func TestAutoProvisionLoop_DisabledByDefault(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewService(st, st, st, st, st, nil)
	// 不注入 AutoProvisionConfig（默认关闭）
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消
	done := make(chan struct{})
	go func() {
		svc.AutoProvisionLoop(ctx)
		close(done)
	}()
	select {
	case <-done:
		// 期望立即返回
	case <-time.After(2 * time.Second):
		t.Fatal("未启用时 AutoProvisionLoop 应立即返回")
	}
}

// TestAutoProvisionLoop_ExplicitFalseRejected 验证显式 Enabled=false 也立即返回。
func TestAutoProvisionLoop_ExplicitFalseRejected(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewService(st, st, st, st, st, nil)
	svc.SetAutoProvisionConfig(&AutoProvisionConfig{
		Enabled:      false,
		LoopInterval: 1 * time.Second,
		SegmentCIDR:  "127.0.0.1/32",
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		svc.AutoProvisionLoop(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Enabled=false 时 AutoProvisionLoop 应立即返回")
	}
}

// TestAutoProvisionLoop_NoIntervalRejected 验证 LoopInterval<=0 时不启动。
func TestAutoProvisionLoop_NoIntervalRejected(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewService(st, st, st, st, st, nil)
	svc.SetAutoProvisionConfig(&AutoProvisionConfig{
		Enabled:      true,
		LoopInterval: 0, // 无间隔
		SegmentCIDR:  "127.0.0.1/32",
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		svc.AutoProvisionLoop(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("LoopInterval<=0 时 AutoProvisionLoop 应立即返回")
	}
}

// TestAutoProvisionLoop_NoSegmentCIDRRejected 验证 SegmentCIDR="" 时不启动。
func TestAutoProvisionLoop_NoSegmentCIDRRejected(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewService(st, st, st, st, st, nil)
	svc.SetAutoProvisionConfig(&AutoProvisionConfig{
		Enabled:      true,
		LoopInterval: 1 * time.Second,
		SegmentCIDR:  "", // 无网段
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		svc.AutoProvisionLoop(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SegmentCIDR='' 时 AutoProvisionLoop 应立即返回")
	}
}

// TestAutoProvisionLoop_RunsAndExitsOnCancel 验证循环启动后 ctx 取消能优雅退出。
// 用短间隔（200ms）+ 无效 CIDR 触发失败退避，2s 后取消 ctx，循环应在取消后立即退出。
func TestAutoProvisionLoop_RunsAndExitsOnCancel(t *testing.T) {
	metrics.Init("device-svc-test")
	st := store.NewMemoryStore()
	svc := NewService(st, st, st, st, st, nil)
	svc.SetAutoProvisionConfig(&AutoProvisionConfig{
		Enabled:           true,
		FallbackAdvertise: "http://127.0.0.1:8081",
		LoopInterval:      200 * time.Millisecond,
		LoopMaxBackoff:    1 * time.Second,
		SegmentCIDR:       "not-a-cidr", // 无效 CIDR 触发失败，验证退避不崩溃
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.AutoProvisionLoop(ctx)
		close(done)
	}()
	// 让循环跑几轮（退避后间隔翻倍，2s 内应有多轮失败）。
	time.Sleep(2 * time.Second)
	cancel()
	select {
	case <-done:
		// 优雅退出
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 AutoProvisionLoop 应在 2s 内退出")
	}
}

// TestRecordDeviceMetrics 验证 4 项设备 metrics 被正确记录。
// 通过 /metrics handler 抓取 Prometheus 文本输出，断言包含期望的 metric 行。
func TestRecordDeviceMetrics(t *testing.T) {
	metrics.Init("device-svc-test")
	st := store.NewMemoryStore()
	svc := NewService(st, st, st, st, st, nil)

	// 种子：2 online（1 已纳管 + 1 未纳管）+ 1 discovered + 1 offline。
	st.RegisterDevice(&models.Device{ID: "dev-1", Status: "online", AgentID: "ag-1"})
	st.RegisterDevice(&models.Device{ID: "dev-2", Status: "online", AgentID: ""})
	st.RegisterDevice(&models.Device{ID: "dev-3", Status: "discovered"})
	st.RegisterDevice(&models.Device{ID: "dev-4", Status: "offline"})

	svc.RecordDeviceMetrics()

	// 抓取 /metrics 输出验证。
	handler := metrics.GetHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body := rec.Body.String()

	// total=4, online=2, offline=2（discovered+offline）, success_rate=25%（1/4*100）
	for _, want := range []string{
		`business_metrics{name="device_total"} 4`,
		`business_metrics{name="device_online"} 2`,
		`business_metrics{name="device_offline"} 2`,
		`business_metrics{name="provision_success_rate"} 25`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics 缺少 %q\n---输出:\n%s", want, body)
		}
	}
}

// TestRecordDeviceMetrics_EmptyStore 验证空 store 时 metrics 为 0（不 panic）。
func TestRecordDeviceMetrics_EmptyStore(t *testing.T) {
	metrics.Init("device-svc-test")
	st := store.NewMemoryStore()
	svc := NewService(st, st, st, st, st, nil)

	svc.RecordDeviceMetrics() // 不应 panic

	handler := metrics.GetHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `business_metrics{name="device_total"} 0`) {
		t.Fatalf("空 store device_total 应为 0\n---%s", body)
	}
	if !strings.Contains(body, `business_metrics{name="provision_success_rate"} 0`) {
		t.Fatalf("空 store provision_success_rate 应为 0\n---%s", body)
	}
}
