// sse_redis.go — SSE 跨副本扇出（Redis Pub/Sub，ADR-2026-11-15 §ADR-2）。
//
// 设计：
//   - publishEvent 本地广播后 PUBLISH 到 `opsmesh:sse:<tenant>`；
//   - 每实例 PSUBSCRIBE `opsmesh:sse:*`，收到后仅向本实例订阅者 fan-out；
//   - 事件携带 Origin（实例 ID），订阅端跳过自己发布的事件，避免本副本重复投递；
//   - Redis 未配置 ⇒ 不启用（单实例本地广播，启动时 WARN 一次）。
package controlplane

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Levango7/OpsMesh/internal/logx"
)

const sseRedisChannelPrefix = "opsmesh:sse:"

// newSSERedisClient 构造 SSE 用的 Redis 客户端（只读发布/订阅用途）。
func newSSERedisClient(addr, password string) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: addr, Password: password})
}

// sseRedisPublish 发布事件到跨副本频道（失败仅告警，不影响主流程）。
func (s *Server) sseRedisPublish(tenantID string, ev SSEEvent) {
	if s.ssePub == nil || tenantID == "" {
		return
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.ssePub.Publish(ctx, sseRedisChannelPrefix+tenantID, payload).Err(); err != nil {
		// 限噪：Redis 故障时每 30s 最多一条告警。
		now := time.Now()
		s.eventMu.Lock()
		if now.Sub(s.lastSSEPubWarn) > 30*time.Second {
			s.lastSSEPubWarn = now
			logx.Warn(context.Background(), "SSE Redis 发布失败（跨副本扇出降级）", "tenant", tenantID, "err", err)
		}
		s.eventMu.Unlock()
	}
}

// sseRedisSubscribeLoop 订阅跨副本频道并把事件投递给本实例订阅者。
// 跳过 Origin==本实例 的事件（否则自己发布的事件会被二次投递）。
func (s *Server) sseRedisSubscribeLoop(ctx context.Context) {
	if s.ssePub == nil {
		return
	}
	sub := newSSERedisClient(s.cfg.RedisAddr, s.cfg.RedisPassword)
	defer func() { _ = sub.Close() }()

	ps := sub.PSubscribe(ctx, sseRedisChannelPrefix+"*")
	defer func() { _ = ps.Close() }()
	ch := ps.Channel()
	self := s.haInstanceID()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var ev SSEEvent
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				continue
			}
			if ev.Origin != "" && ev.Origin == self {
				continue
			}
			s.broadcastLocal(ev)
		}
	}
}
