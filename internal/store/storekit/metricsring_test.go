// metricsring_test.go MetricsRing 环形缓冲边界（自 internal/store/store_extra_test.go 迁入，TD-61）。
//
// 迁入理由：这些用例断言缓冲内部状态（capacity 等私有字段）与 nil 接收者语义；
// MetricsRing 下沉 storekit 后父包无法再访问其私有字段，测试随类型走。
package storekit

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// ============================================================================
// MetricsRing 环形缓冲边界
// ============================================================================

// TestMetricsRing_NilReceiver 验证 MetricsRing nil 接收者不 panic。
func TestMetricsRing_NilReceiver(t *testing.T) {
	var r *MetricsRing
	r.Add(&proto.DeviceMetrics{DeviceID: "d1"}) // 不应 panic
	if r.Latest() != nil {
		t.Fatal("nil.Latest() 应返回 nil")
	}
	if r.Since(time.Time{}) != nil {
		t.Fatal("nil.Since() 应返回 nil")
	}
}

// TestMetricsRing_NilMetric 验证 Add nil metric 不 panic。
func TestMetricsRing_NilMetric(t *testing.T) {
	r := NewMetricsRing(3)
	r.Add(nil) // 不应 panic
	if r.Latest() != nil {
		t.Fatal("Add nil 后 Latest 应返回 nil")
	}
}

// TestMetricsRing_ZeroCapacity 验证 capacity<=0 时使用默认容量。
func TestMetricsRing_ZeroCapacity(t *testing.T) {
	r := NewMetricsRing(0)
	if r.capacity != MetricsRingDefaultCap {
		t.Fatalf("capacity = %d, want %d", r.capacity, MetricsRingDefaultCap)
	}
	r = NewMetricsRing(-1)
	if r.capacity != MetricsRingDefaultCap {
		t.Fatalf("capacity = %d, want %d", r.capacity, MetricsRingDefaultCap)
	}
}

// TestMetricsRing_Latest_AfterOne 验证 Add 一条后 Latest 返回该条。
func TestMetricsRing_Latest_AfterOne(t *testing.T) {
	r := NewMetricsRing(3)
	r.Add(&proto.DeviceMetrics{DeviceID: "d1", CPU: proto.CPUMetrics{Cores: 4}})
	got := r.Latest()
	if got == nil || got.CPU.Cores != 4 {
		t.Fatalf("Latest = %+v, want Cores=4", got)
	}
}

// TestMetricsRing_Latest_Empty 验证空缓冲 Latest 返回 nil。
func TestMetricsRing_Latest_Empty(t *testing.T) {
	r := NewMetricsRing(3)
	if r.Latest() != nil {
		t.Fatal("空缓冲 Latest 应返回 nil")
	}
}

// TestMetricsRing_Since_Empty 验证空缓冲 Since 返回 nil。
func TestMetricsRing_Since_Empty(t *testing.T) {
	r := NewMetricsRing(3)
	if r.Since(time.Time{}) != nil {
		t.Fatal("空缓冲 Since 应返回 nil")
	}
}

// TestMetricsRing_Since_Filter 验证 Since 时间过滤。
func TestMetricsRing_Since_Filter(t *testing.T) {
	r := NewMetricsRing(10)
	base := time.Now()
	for i := 0; i < 5; i++ {
		r.Add(&proto.DeviceMetrics{
			DeviceID:    "d1",
			CPU:         proto.CPUMetrics{Cores: i + 1},
			CollectedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	// Since = base + 2min，应返回 Cores=3,4,5
	got := r.Since(base.Add(2 * time.Minute))
	if len(got) != 3 {
		t.Fatalf("Since filter len = %d, want 3", len(got))
	}
	for i, s := range got {
		if s.CPU.Cores != i+3 {
			t.Fatalf("got[%d].Cores = %d, want %d", i, s.CPU.Cores, i+3)
		}
	}
}

// TestMetricsRing_Since_AllWhenZero 验证 Since 零值返回全部。
func TestMetricsRing_Since_AllWhenZero(t *testing.T) {
	r := NewMetricsRing(5)
	for i := 0; i < 3; i++ {
		r.Add(&proto.DeviceMetrics{DeviceID: "d1", CollectedAt: time.Now()})
	}
	got := r.Since(time.Time{})
	if len(got) != 3 {
		t.Fatalf("Since zero len = %d, want 3", len(got))
	}
}

// TestMetricsRing_Since_NoneMatch 验证 Since 全部不匹配返回 nil。
func TestMetricsRing_Since_NoneMatch(t *testing.T) {
	r := NewMetricsRing(5)
	r.Add(&proto.DeviceMetrics{DeviceID: "d1", CollectedAt: time.Now()})
	future := time.Now().Add(time.Hour)
	if got := r.Since(future); got != nil {
		t.Fatalf("Since future = %+v, want nil", got)
	}
}
