// ssebridge.go — task-svc → controlplane SSE 通道桥接接口。
//
// 设计目标（TD-60 A-2 阶段：SSE 桥接）：
//   - task-svc 产生的任务状态变更事件需要能桥接到 controlplane 的 SSE 通道，
//     使前端订阅 /api/v1/events 的客户端能实时收到 task-svc 的事件。
//   - **不改动 controlplane 侧代码**：只在 task-svc 侧准备桥接能力。
//   - **接口注入模式**：SSEBridge 为接口，stub 为默认（日志记录），
//     生产环境由 main 注入真实 SSE client（HTTP POST 到 controlplane /api/v1/events/ingest
//     或 gRPC EventPublisher.PublishEvent）。
//   - **慢消费者不阻塞**：Forward 非阻塞，stub 实现直接返回，真实实现内部应做缓冲/超时。
//
// 事件类型对齐 controlplane publishEvent 的字面量事件名（sse_contract_test.go 锚定）：
//   - "task_status"：任务状态变更（创建/分配/完成/取消/失败）

package events

import (
	"context"
	"log"
)

// SSEBridge 桥接接口：将 task-svc 事件转发到 controlplane SSE 通道。
//
// 语义对齐 controlplane Server.PublishEvent(ctx, typ, tenantID, data)，
// 但 task-svc 内部独立声明——保持服务独立性，生产环境由 main 注入适配器。
type SSEBridge interface {
	// Forward 转发一个事件到 SSE 通道。
	// typ 为事件类型（如 "task_status"），tenantID 为事件归属租户，
	// data 为业务载荷（任意可 JSON 序列化结构）。
	// 非阻塞：慢消费者/无订阅者时快速返回，不阻塞调用方。
	Forward(ctx context.Context, typ string, tenantID string, data interface{})
}

// StubSSEBridge stub 实现：仅日志记录（默认/测试/单机用）。
//
// 生产环境由 main 注入真实 SSE client（如 HTTP SSE client 或 gRPC EventPublisher）。
type StubSSEBridge struct{}

// Forward 记日志后返回（不实际转发）。
func (StubSSEBridge) Forward(ctx context.Context, typ string, tenantID string, data interface{}) {
	log.Printf("[sse-bridge-stub] type=%s tenant=%s data=%v", typ, tenantID, data)
}

// NoopSSEBridge 空实现：完全静默（测试用，避免日志噪声）。
type NoopSSEBridge struct{}

func (NoopSSEBridge) Forward(context.Context, string, string, interface{}) {}
