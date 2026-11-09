// sse.go — SSE 实时推送
//
// 设计目标：替代前端 5s 轮询，控制面主动推送任务状态变更/告警/设备上下线到浏览器。
// 端点：GET /api/v1/events/stream（text/event-stream）。
//
// 事件类型（event: <type>，全量 10 种与 docs/sse-protocol.md 枚举表对齐，
// 由 sse_contract_test.go 守护代码↔文档一致性）：
//   - task_status           任务状态变更（create / claim / cancel / report_result）
//   - alert_new             告警列表变更（新告警 / ack / silence，前端据此刷新告警面板）
//   - device_online         设备/agent 上线（Register）
//   - device_offline        设备/agent 下线（退役 / 离线归档）
//   - approval_status       作业审批通过/拒绝/取消
//   - schedule_status       定时任务触发/暂停/恢复
//   - os_template_changed   OS 优化模板增删改（data: templateID + action）
//   - mw_template_changed   中间件模板增删改（data: templateID + action）
//   - agent_logs            agent 日志上报到达
//   - hello                 连接建立握手（handler 首帧，不走 publishEvent）
//
// 信封格式（data 行为 SSEEvent JSON）：
//
//	event: task_status\n
//	data: {"type":"task_status","tenantID":"t1","data":{"taskID":"xxx","status":"running","agentID":"yyy"}}\n\n
//
// 慢消费者策略：每个订阅者 buffered chan(16)，publishEvent 非阻塞广播；
// 缓冲满则丢弃该事件（避免一个慢客户端拖垮广播，设计取舍）。
//
// 连接保活：每 15s 发送 SSE 注释帧 ": ping\n\n"（不触发客户端 message 事件，仅保活）。
// 客户端断开（ctx.Done）时 unsubscribe 并 close chan，防止泄漏。
//
// 安全（H6 SSE 租户隔离）：handler 入口复用 requireTenantContext 提取并**校验**身份
// （X-Tenant-ID 头 / Authorization Bearer / HttpOnly Cookie JWT），与其余业务端点同一口径：
// 头与令牌租户不一致 → 403，头非空但无凭证且 requireAuth → 401，两者均无身份时
// requireAuth → 401、demo → 填充 "default" 租户便于本地体验、其余 → 400。
// publishEvent 携带 tenantID 标记事件归属，handler 仅下发与本租户匹配的事件，
// 跨租户事件在 SSE 通道被丢弃（不广播给他人）。
package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Levango7/OpsMesh/internal/otelx"
)

// SSEEvent 是推送给前端的事件信封。
// Type 为事件类型（task_status / alert_new / device_online / device_offline），
// TenantID 为事件归属租户（空表示全局事件，如 hello；非空时仅下发到同租户订阅者），
// Data 为业务载荷（任意可 JSON 序列化结构）。
//
// 分布式可观测性：TraceID 字段携带 OTel trace_id，
// 使前端可关联后端链路追踪/日志/审计日志，端到端可观测。
// omitempty 保证旧客户端不感知新字段（向后兼容）。
type SSEEvent struct {
	Type     string      `json:"type"`
	TenantID string      `json:"tenantID,omitempty"`
	Data     interface{} `json:"data"`
	// TraceID 关联 OTel 链路追踪的 trace_id（32 字符 hex），空串表示无关联（向后兼容）。
	// 由 publishEvent 从 ctx 自动提取注入，调用方无需手动设置。
	TraceID string `json:"traceID,omitempty"`
}

// sseSubscriberBuf 每个订阅者的缓冲通道容量。
// 取 16：足够吸收短时突发（如批量下发），又不至于在慢客户端上积压过多内存。
const sseSubscriberBuf = 16

// sseHeartbeatInterval SSE 连接保活心跳间隔。
// 注释帧 ": ping\n\n" 不触发浏览器 message 事件，仅维持 TCP 连接活跃，
// 防止中间代理（nginx / envoy）因空闲超时关闭连接。
const sseHeartbeatInterval = 15 * time.Second

// handleEventsStream 处理 GET /api/v1/events/stream：SSE 长连接。
// 设置标准 SSE 响应头，订阅事件总线，循环写事件帧 + flush。
// 客户端断开（r.Context().Done()）时自动取消订阅并释放资源。
//
// 鉴权：复用 requireTenantContext（矩阵语义见 http_infra.go），与其余业务端点同一口径。
// 此前 handler 自行提取身份、**头非空即无条件采信**，只在头为空时才回退令牌 ⇒
// 匿名者伪造 X-Tenant-ID 即可订阅他租户事件流（边缘 nginx 不剥离该头）。
//   - 头与令牌租户不一致 → 403；X-User-Id 与令牌 user 不一致 → 403；
//   - 头非空但无任何凭证：requireAuth 且未开 --trust-gateway-headers → 401，否则放行；
//   - 头空 → 回退 Bearer / HttpOnly Cookie JWT（浏览器 EventSource 设不了自定义头，走这条）；
//   - 两者均空：requireAuth → 401；demo → 填 "default"；其余 → 400（G1：不允许空租户全量订阅）。
//
// 租户过滤：收到的事件若 TenantID 非空且与当前订阅者租户不匹配则丢弃，
// 不跨租户广播（防止 A 租户订阅者收到 B 租户的任务/告警事件）。
//
// 顺序约束（2026-11-07 修复）：**subscribeEvents 必须在写 hello 握手帧之前**。
// 客户端收到 hello 后会立刻 publish 事件；握手先于订阅时，那些事件落在
// 「尚未入册」的窗口里被 publishEvent 静默丢弃（首连丢第一条事件）。
func (s *Server) handleEventsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 身份提取 + 防伪校验统一走 requireTenantContext；ok=false 时响应已写入。
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	tenant := actx.TenantID

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	// SSE 标准响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx 透传不缓冲（否则 flush 无效）
	w.WriteHeader(http.StatusOK)

	// 订阅必须**先于握手帧**注册（顺序即语义）：
	// 客户端收到 hello 后通常立刻 publish 事件；若订阅在握手之后才注册，
	// 那个窗口内发布的事件会命中 publishEvent 的「无订阅者快速返回」而被静默丢弃
	// ⇒ 表现为「刚连上的客户端丢掉第一条事件」（整包测试下必现，见
	// TestSSE_SubscribeBeforeHello_NoFirstEventLoss）。订阅先注册则事件进有缓冲 chan，不会丢。
	ch := s.subscribeEvents()
	defer s.unsubscribeEvents(ch)

	// 握手帧：让客户端确认连接已建立（event: hello，data 为空对象）
	fmt.Fprint(w, "event: hello\ndata: {}\n\n")
	flusher.Flush()

	ctx := r.Context()
	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return // 客户端断开，unsubscribe 由 defer 执行
		case ev, open := <-ch:
			if !open {
				return // 通道被关闭（服务端主动关闭订阅）
			}
			// 租户隔离：事件归属租户非空且与当前订阅者不匹配则丢弃，
			// 不跨租户下发（防跨租户信息泄露）。
			// 注：订阅者租户必非空（G1 已在入口拒绝空租户订阅），空租户事件（全局，如 hello）
			// 仍会推给所有订阅者。
			if ev.TenantID != "" && ev.TenantID != tenant {
				continue
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue // 载荷不可序列化，跳过（不应发生）
			}
			// SSE 事件帧：event 行 + data 行 + 空行结束
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
			flusher.Flush()
		case <-heartbeat.C:
			// SSE 注释帧：保活，不触发客户端 message 事件
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// subscribeEvents 注册一个新的 SSE 订阅者，返回其事件接收通道。
// 通道带缓冲（sseSubscriberBuf），publishEvent 非阻塞写入。
func (s *Server) subscribeEvents() chan SSEEvent {
	ch := make(chan SSEEvent, sseSubscriberBuf)
	s.eventMu.Lock()
	s.eventSubs[ch] = struct{}{}
	s.eventMu.Unlock()
	return ch
}

// unsubscribeEvents 取消订阅并关闭通道，释放资源。
// 重复调用安全（delete 对不存在的 key 无副作用），但 close 对已关闭通道会 panic，
// 故仅在本函数内 close，且调用方应保证每个 ch 只 unsubscribe 一次（handler 用 defer 保证）。
func (s *Server) unsubscribeEvents(ch chan SSEEvent) {
	s.eventMu.Lock()
	delete(s.eventSubs, ch)
	s.eventMu.Unlock()
	close(ch)
}

// publishEvent 非阻塞广播一个事件到所有活跃订阅者。
// 慢消费者（缓冲满）丢弃该事件，避免一个慢客户端拖垮广播。
// typ 为事件类型（task_status / alert_new / device_online / device_offline），
// tenantID 为事件归属租户（空表示全局事件，所有订阅者均接收；
// 非空时由 handleEventsStream 按租户过滤，跨租户订阅者不会收到），
// data 为业务载荷（任意可 JSON 序列化结构）。
//
// 分布式可观测性：从 ctx 提取 OTel trace_id 注入 SSEEvent.TraceID，
// 使 SSE 事件与后端链路追踪/日志/审计日志关联。ctx 无有效 span 时 TraceID 为空（向后兼容）。
// PublishEvent 是 grpc.EventPublisher 接口的实现，将 SSE 事件发布委托给 publishEvent。
// 使 grpc 包可通过该接口注入事件发布能力，解除对 controlplane.Server 的直接依赖。
func (s *Server) PublishEvent(ctx context.Context, typ string, tenantID string, data interface{}) {
	s.publishEvent(ctx, typ, tenantID, data)
}

func (s *Server) publishEvent(ctx context.Context, typ string, tenantID string, data interface{}) {
	s.eventMu.RLock()
	defer s.eventMu.RUnlock()
	if len(s.eventSubs) == 0 {
		return // 无订阅者，快速返回（避免构造事件开销）
	}
	ev := SSEEvent{
		Type:     typ,
		TenantID: tenantID,
		Data:     data,
		TraceID:  otelx.TraceIDFromContext(ctx),
	}
	for ch := range s.eventSubs {
		select {
		case ch <- ev:
		default: // 慢消费者，丢弃（设计取舍：保广播延迟，弃个别事件）
		}
	}
}
