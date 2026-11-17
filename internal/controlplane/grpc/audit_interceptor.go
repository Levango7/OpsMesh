package grpc

// audit_interceptor.go — 统一的 gRPC 调用审计拦截器（issue #103）。
//
// 背景：第三方报告担忧「鉴权/校验/限流/审计四件事每个 handler 自己写 ⇒
// compliance/auditsafe 形同虚设」。核验后发现：
//   - 鉴权：本链路是 HMAC agent 签名（领域特化，比通用 user 鉴权更强），在 handler 内完成；
//   - 审计：handler 有 g.Audit helper（带 trace_id），但**只有主动调用的 handler 才有留痕**；
//   - 限流：pkg/ratelimit.GRPCInterceptor 已在三个业务微服务装配（本 agent 链路签名制不适用）；
//   - 校验：proto 生成层 + handler 内。
//
// 唯一确实缺失的统一维度是：**每次 RPC 调用本身的可审计留痕**——
// 方法、结果码、耗时、对端。本拦截器在入口统一补齐，不碰鉴权/限流/校验语义，
// 因此零误伤风险（尤其不会干扰 agent 高频心跳）。
//
// 日志级别：OK 用 Debug（避免心跳/拉任务刷屏），非 OK 用 Warn（异常调用需可见）。

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/Levango7/OpsMesh/internal/logx"
)

// logFunc 与 logx.Info/Warn 同签名，便于测试注入。
type logFunc func(ctx context.Context, msg string, args ...any)

// AuditUnaryInterceptor 返回统一调用审计拦截器。
// peerAddr 为 true 时记录对端地址（默认开；排障有用，但注意基数——地址是连续值，仅进日志不进指标）。
func AuditUnaryInterceptor() grpc.UnaryServerInterceptor {
	return auditInterceptor(logx.Debug, logx.Warn, true)
}

func auditInterceptor(debug, warn logFunc, withPeer bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		elapsed := time.Since(start)

		code := status.Code(err)
		args := []any{
			"grpc_method", info.FullMethod,
			"grpc_code", code.String(),
			"duration_ms", elapsed.Milliseconds(),
		}
		if withPeer {
			if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
				args = append(args, "peer", p.Addr.String())
			}
		}
		if code == codes.OK {
			debug(ctx, "grpc_call", args...)
		} else {
			args = append(args, "grpc_error", err.Error())
			warn(ctx, "grpc_call_failed", args...)
		}
		return resp, err
	}
}
