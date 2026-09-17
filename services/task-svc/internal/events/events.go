// Package events 提供 task-svc 的事件总线与审计集成能力。
//
// 设计目标（TD-60 A-2 阶段：恢复完整生产路径）：
//   - **接口注入模式**：EventBus / AuditSink / SSEBridge 均为接口，
//     service 层持有接口指针，nil 时跳过发射（向后兼容，不破坏现有 API/测试）。
//   - **不硬绑定具体实现**：task-svc 不直接依赖 controlplane 的事件实现，
//     生产环境由 main 注入真实实现（Kafka/SQL/SSE client），测试/单机用 Noop/Log。
//   - **参考 controlplane 审计模式**：Event 结构对齐 internal/events.Event
//     （TenantID/UserID/Action/Target/Detail/Level），AuditEvent 对齐
//     internal/proto.AuditEvent（含 TraceID 关联 OTel 链路追踪）。
//   - **分布式可观测性**：发射时从 ctx 提取 trace_id 注入 AuditEvent.TraceID，
//     使审计日志与链路追踪/SSE 事件跨域关联检索。
package events

import (
	"context"
	"log"
	"time"

	"github.com/Levango7/OpsMesh/pkg/trace"
)

// Level 事件级别（对齐 internal/events.Level）。
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelAlert Level = "alert"
)

// SchemaVersion 是当前事件契约（信封 JSON 结构）的语义版本。
const SchemaVersion = "1.0.0"

// Event 是 task-svc 产出的统一事件（审计/告警共用信封）。
// 字段对齐 internal/events.Event，保证与 controlplane 事件总线消费者兼容。
type Event struct {
	TenantID string `json:"tenantID"`
	UserID   string `json:"userID"`
	Action   string `json:"action"`
	Target   string `json:"target"`
	Detail   string `json:"detail"`
	Level    Level  `json:"level"`
	Version  string `json:"version"`
}

// stamp 在发布前为事件加盖当前契约版本；若调用方已显式指定则保留。
func stamp(e Event) Event {
	if e.Version == "" {
		e.Version = SchemaVersion
	}
	return e
}

// EventBus 事件总线接口（可选注入：nil 时 service 层跳过发射）。
type EventBus interface {
	Publish(ctx context.Context, e Event) error
}

// NoopBus 丢弃所有事件（默认/测试用）。
type NoopBus struct{}

func (NoopBus) Publish(context.Context, Event) error { return nil }

// LogBus 把事件结构化输出到日志（开发/单机可见，替代 Kafka）。
type LogBus struct{}

func (LogBus) Publish(ctx context.Context, e Event) error {
	e = stamp(e)
	log.Printf("[event] action=%s target=%s tenant=%s user=%s level=%s detail=%s version=%s",
		e.Action, e.Target, e.TenantID, e.UserID, e.Level, e.Detail, e.Version)
	return nil
}

// AuditEvent 审计事件（等保三级：操作 100% 留痕）。
// 字段对齐 internal/proto.AuditEvent，保证审计检索端兼容。
type AuditEvent struct {
	TenantID  string    `json:"tenantID"`
	UserID    string    `json:"userID"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"createdAt"`
	TraceID   string    `json:"traceID,omitempty"`
}

// AuditSink 审计日志写入接口（可选注入：nil 时 service 层跳过写入）。
type AuditSink interface {
	Audit(e AuditEvent)
}

// NoopAuditSink 丢弃所有审计事件（默认/测试用）。
type NoopAuditSink struct{}

func (NoopAuditSink) Audit(AuditEvent) {}

// LogAuditSink 把审计事件结构化输出到日志（开发/单机可见）。
type LogAuditSink struct{}

func (LogAuditSink) Audit(e AuditEvent) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	log.Printf("[audit] tenant=%s user=%s action=%s target=%s detail=%s trace=%s",
		e.TenantID, e.UserID, e.Action, e.Target, e.Detail, e.TraceID)
}

// NewAuditEvent 从 ctx 提取 trace_id 构造 AuditEvent（对齐 controlplane s.audit helper）。
// ctx 无有效 span 时 TraceID 为空串（向后兼容，不破坏无 OTel 场景）。
func NewAuditEvent(ctx context.Context, tenantID, userID, action, target, detail string) AuditEvent {
	return AuditEvent{
		TenantID:  tenantID,
		UserID:    userID,
		Action:    action,
		Target:    target,
		Detail:    detail,
		CreatedAt: time.Now(),
		TraceID:   trace.TraceIDFromContext(ctx),
	}
}
