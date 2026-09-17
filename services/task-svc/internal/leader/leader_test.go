// leader_test.go — leader 包单元测试（A-2 阶段）。
//
// 覆盖目标：
//   - StubLeaderElector 永真（IsLeader/Renew 返回 true，Close 无错）
//   - K8sLeaseElector 状态机：acquire → renew → lose → re-acquire
//   - K8sLeaseElector nil 安全（ops=nil 时 Renew 返回 false）
package leader

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestStubLeaderElector(t *testing.T) {
	e := NewStub()
	if !e.IsLeader() {
		t.Error("StubLeaderElector.IsLeader() = false, want true")
	}
	if !e.Renew(context.Background(), 15*time.Second) {
		t.Error("StubLeaderElector.Renew() = false, want true")
	}
	if err := e.Close(); err != nil {
		t.Errorf("StubLeaderElector.Close() error: %v", err)
	}
}

// mockLeaseOps 模拟 K8s Lease 操作（测试用）。
type mockLeaseOps struct {
	acquireCalls int32
	renewCalls   int32
	releaseCalls int32
	acquireOK    bool
	renewOK      bool
}

func (m *mockLeaseOps) Acquire(context.Context, string, string, time.Duration) bool {
	atomic.AddInt32(&m.acquireCalls, 1)
	return m.acquireOK
}

func (m *mockLeaseOps) Renew(context.Context, string, string, time.Duration) bool {
	atomic.AddInt32(&m.renewCalls, 1)
	return m.renewOK
}

func (m *mockLeaseOps) Release(context.Context, string, string) error {
	atomic.AddInt32(&m.releaseCalls, 1)
	return nil
}

func TestK8sLeaseElector_AcquireSuccess(t *testing.T) {
	ops := &mockLeaseOps{acquireOK: true, renewOK: true}
	e := NewK8sLease(ops, "test-lease", "pod-1")

	// 初始非 leader
	if e.IsLeader() {
		t.Error("初始状态应为非 leader")
	}

	// Renew → acquire 成功 → 晋升为 leader
	if !e.Renew(context.Background(), 15*time.Second) {
		t.Error("Renew() = false, want true (acquire success)")
	}
	if !e.IsLeader() {
		t.Error("acquire 成功后应为 leader")
	}
	if got := atomic.LoadInt32(&ops.acquireCalls); got != 1 {
		t.Errorf("acquireCalls = %d, want 1", got)
	}
}

func TestK8sLeaseElector_AcquireFail(t *testing.T) {
	ops := &mockLeaseOps{acquireOK: false, renewOK: false}
	e := NewK8sLease(ops, "test-lease", "pod-1")

	// Renew → acquire 失败 → 仍非 leader
	if e.Renew(context.Background(), 15*time.Second) {
		t.Error("Renew() = true, want false (acquire fail)")
	}
	if e.IsLeader() {
		t.Error("acquire 失败后应仍为非 leader")
	}
}

func TestK8sLeaseElector_RenewAfterAcquire(t *testing.T) {
	ops := &mockLeaseOps{acquireOK: true, renewOK: true}
	e := NewK8sLease(ops, "test-lease", "pod-1")

	// 第一次 Renew → acquire
	if !e.Renew(context.Background(), 15*time.Second) {
		t.Fatal("首次 Renew(acquire) 失败")
	}
	// 第二次 Renew → renew（已是 leader）
	if !e.Renew(context.Background(), 15*time.Second) {
		t.Fatal("二次 Renew(renew) 失败")
	}
	if got := atomic.LoadInt32(&ops.renewCalls); got != 1 {
		t.Errorf("renewCalls = %d, want 1", got)
	}
}

func TestK8sLeaseElector_LoseLeadership(t *testing.T) {
	ops := &mockLeaseOps{acquireOK: true, renewOK: true}
	e := NewK8sLease(ops, "test-lease", "pod-1")

	// acquire 成功
	if !e.Renew(context.Background(), 15*time.Second) {
		t.Fatal("acquire 失败")
	}

	// renew 失败 → 失去 leader
	ops.renewOK = false
	if e.Renew(context.Background(), 15*time.Second) {
		t.Error("renew 失败时应返回 false")
	}
	if e.IsLeader() {
		t.Error("renew 失败后应为非 leader")
	}

	// 再次 renew → 尝试 acquire（非 leader 走 acquire 分支）
	ops.acquireOK = true
	if !e.Renew(context.Background(), 15*time.Second) {
		t.Fatal("re-acquire 失败")
	}
	if !e.IsLeader() {
		t.Error("re-acquire 成功后应为 leader")
	}
}

func TestK8sLeaseElector_NilOps(t *testing.T) {
	e := NewK8sLease(nil, "test-lease", "pod-1")
	if e.Renew(context.Background(), 15*time.Second) {
		t.Error("nil ops 时 Renew 应返回 false")
	}
	if err := e.Close(); err != nil {
		t.Errorf("nil ops 时 Close 应无错: %v", err)
	}
}

func TestK8sLeaseElector_Close(t *testing.T) {
	ops := &mockLeaseOps{acquireOK: true, renewOK: true}
	e := NewK8sLease(ops, "test-lease", "pod-1")

	// acquire 成为 leader
	if !e.Renew(context.Background(), 15*time.Second) {
		t.Fatal("acquire 失败")
	}

	// Close → 释放租约
	if err := e.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}
	if got := atomic.LoadInt32(&ops.releaseCalls); got != 1 {
		t.Errorf("releaseCalls = %d, want 1", got)
	}
	if e.IsLeader() {
		t.Error("Close 后应为非 leader")
	}
}
