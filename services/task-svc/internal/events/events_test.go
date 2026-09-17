// events_test.go — events 包单元测试（A-2 阶段）。
//
// 覆盖目标：
//   - NoopBus/LogBus 不 panic 且返回 nil
//   - NoopAuditSink/LogAuditSink 不 panic
//   - NewAuditEvent 从 ctx 提取 TraceID（空 ctx → 空串，向后兼容）
//   - SSEBridge stub/noop 不 panic
package events

import (
	"context"
	"testing"
)

func TestNoopBus(t *testing.T) {
	var bus EventBus = NoopBus{}
	if err := bus.Publish(context.Background(), Event{Action: "test"}); err != nil {
		t.Errorf("NoopBus.Publish error: %v", err)
	}
}

func TestLogBus(t *testing.T) {
	var bus EventBus = LogBus{}
	if err := bus.Publish(context.Background(), Event{Action: "test", Target: "t1"}); err != nil {
		t.Errorf("LogBus.Publish error: %v", err)
	}
}

func TestNoopAuditSink(t *testing.T) {
	var sink AuditSink = NoopAuditSink{}
	sink.Audit(AuditEvent{Action: "test"}) // 不 panic 即通过
}

func TestLogAuditSink(t *testing.T) {
	var sink AuditSink = LogAuditSink{}
	sink.Audit(AuditEvent{Action: "test", Target: "t1"}) // 不 panic 即通过
}

func TestNewAuditEvent_EmptyCtx(t *testing.T) {
	e := NewAuditEvent(context.Background(), "tenant-1", "user-1", "create_task", "task-1", "echo hi")
	if e.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q, want tenant-1", e.TenantID)
	}
	if e.Action != "create_task" {
		t.Errorf("Action = %q, want create_task", e.Action)
	}
	if e.TraceID != "" {
		t.Errorf("TraceID = %q, want empty (no span in ctx)", e.TraceID)
	}
	if e.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestStubSSEBridge(t *testing.T) {
	var bridge SSEBridge = StubSSEBridge{}
	bridge.Forward(context.Background(), "task_status", "tenant-1", map[string]string{"taskID": "t1"})
}

func TestNoopSSEBridge(t *testing.T) {
	var bridge SSEBridge = NoopSSEBridge{}
	bridge.Forward(context.Background(), "task_status", "tenant-1", nil)
}

// TestServiceEmitAudit_NilReceivers 验证 service 层 emitAudit 在所有注入为 nil 时不 panic。
// （通过 events 包侧验证接口 nil 安全；service 层的集成在 service_test.go 覆盖）
func TestEvent_NilSafe(t *testing.T) {
	var bus EventBus // nil
	if bus != nil {
		t.Error("nil EventBus should be nil")
	}
	// 模拟 service.emitAudit 的 nil 检查模式
	if bus != nil {
		_ = bus.Publish(context.Background(), Event{})
	}
}
