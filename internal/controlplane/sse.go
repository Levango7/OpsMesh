// sse.go — M3-2B SSE 实时推送
//
// 设计目标：替代前端 5s 轮询，控制面主动推送任务状态变更/告警/设备上下线到浏览器。
// 端点：GET /api/v1/events/stream（text/event-stream）。
//
// 事件类型（event: <type>）：
//   - task_status      任务状态变更（create / cancel / report_result / claim）
//   - alert_new        告警列表变更（新告警产生 / ack / silence，前端据此刷新告警面板）
//   - device_online    设备/agent 上线（Register）
//   - device_offline   设备/agent 下线（退役 / 离线归档）
//   - hello            连接建立握手（handler 首帧）
//
// 信封格式（data 行为 SSEEvent JSON）：
//
//	event: task_status\n
//	data: {"type":"task_status","data":{"taskID":"xxx","status":"running","agentID":"yyy"}}\n\n
//
// 慢消费者策略：每个订阅者 buffered chan(16)，publishEvent 非阻塞广播；
// 缓冲满则丢弃该事件（避免一个慢客户端拖垮广播，M3-2B 设计取舍）。
//
// 连接保活：每 15s 发送 SSE 注释帧 ": ping\n\n"（不触发客户端 message 事件，仅保活）。
// 客户端断开（ctx.Done）时 unsubscribe 并 close chan，防止泄漏。
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SSEEvent 是推送给前端的事件信封。
// Type 为事件类型（task_status / alert_new / device_online / device_offline），
// Data 为业务载荷（任意可 JSON 序列化结构）。
type SSEEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
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
func (s *Server) handleEventsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	// SSE 标准响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx 透传不缓冲（否则 flush 无效）
	w.WriteHeader(http.StatusOK)

	// 握手帧：让客户端确认连接已建立（event: hello，data 为空对象）
	fmt.Fprint(w, "event: hello\ndata: {}\n\n")
	flusher.Flush()

	ch := s.subscribeEvents()
	defer s.unsubscribeEvents(ch)

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
// data 为业务载荷（任意可 JSON 序列化结构）。
func (s *Server) publishEvent(typ string, data interface{}) {
	s.eventMu.RLock()
	defer s.eventMu.RUnlock()
	if len(s.eventSubs) == 0 {
		return // 无订阅者，快速返回（避免构造事件开销）
	}
	ev := SSEEvent{Type: typ, Data: data}
	for ch := range s.eventSubs {
		select {
		case ch <- ev:
		default: // 慢消费者，丢弃（M3-2B 设计取舍：保广播延迟，弃个别事件）
		}
	}
}