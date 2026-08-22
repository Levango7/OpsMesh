// grpc.go 提供 OTel gRPC 客户端/服务端拦截器，实现 gRPC metadata 注入/提取 trace context。
// agent gRPC 埋点：agent→控制面 gRPC 调用自动注入 traceparent，控制面提取后接续 trace。
package otelx

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"

	"go.opentelemetry.io/otel/trace"
)

// metadataSupplier 实现 propagation.TextMapCarrier，从 gRPC metadata 读写 trace context。
// 用于在 gRPC metadata 中注入/提取 W3C traceparent（与 HTTP 头同一 propagator）。
type metadataSupplier struct {
	md *metadata.MD
}

func (s metadataSupplier) Get(key string) string {
	if s.md == nil {
		return ""
	}
	values := s.md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (s metadataSupplier) Set(key, value string) {
	if s.md == nil {
		return
	}
	s.md.Set(key, value)
}

// Keys 实现 propagation.TextMapCarrier 接口（OTel v1.21+ 新增方法）。
// 返回 metadata 中所有键（去小写后去重）。
func (s metadataSupplier) Keys() []string {
	if s.md == nil {
		return nil
	}
	seen := make(map[string]bool, len(*s.md))
	out := make([]string, 0, len(*s.md))
	for k := range *s.md {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// GRPCClientUnaryInterceptor 是 gRPC 一元客户端拦截器：为每次 RPC 创建 span 并注入 trace context 到 metadata。
//   - 从 ctx 创建 client span（SpanKindClient）。
//   - 将 span context 注入 outgoing metadata（W3C traceparent），使服务端能接续 trace。
//   - RPC 结束记录 status（错误时标记 Error）。
//
// 用法：
//
//	conn, _ := grpc.Dial(target, grpc.WithUnaryInterceptor(otelx.GRPCClientUnaryInterceptor("agent")))
func GRPCClientUnaryInterceptor(tracerName string) grpc.UnaryClientInterceptor {
	tracer := otel.Tracer(tracerName)
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// 创建 client span。
		ctx, span := tracer.Start(ctx, method,
			trace.WithSpanKind(trace.SpanKindClient),
		)
		defer span.End()

		// 将 span context 注入 outgoing metadata（W3C traceparent）。
		// 若 ctx 已有 incoming trace（从上游 HTTP/gRPC 提取），则接续该 trace。
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			md = metadata.New(nil)
		}
		carrier := metadataSupplier{md: &md}
		otel.GetTextMapPropagator().Inject(ctx, carrier)
		ctx = metadata.NewOutgoingContext(ctx, md)

		// 执行 RPC。
		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return err
	}
}

// GRPCServerUnaryInterceptor 是 gRPC 一元服务端拦截器：从 metadata 提取 trace context 并创建 server span。
//   - 从 incoming metadata 提取 W3C traceparent，接续客户端 trace。
//   - 创建 server span（SpanKindServer），记录 method。
//   - handler 错误时标记 span status 为 Error。
//
// 用法：
//
//	gs := grpc.NewServer(grpc.UnaryInterceptor(otelx.GRPCServerUnaryInterceptor("controlplane")))
func GRPCServerUnaryInterceptor(tracerName string) grpc.UnaryServerInterceptor {
	tracer := otel.Tracer(tracerName)
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		// 从 incoming metadata 提取 W3C trace context。
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			md = metadata.New(nil)
		}
		carrier := metadataSupplier{md: &md}
		ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)

		// 创建 server span。
		ctx, span := tracer.Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		// 执行 handler。
		resp, err = handler(ctx, req)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return resp, err
	}
}

// InjectGRPCMetadata 将当前 ctx 的 trace context 注入到 gRPC outgoing metadata。
// 供 agent 在不使用拦截器的场景（如手动构造 metadata 签名时）手动注入 trace context。
// 返回的 ctx 已附加 outgoing metadata，可直接用于 gRPC Invoke。
func InjectGRPCMetadata(ctx context.Context) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(nil)
	}
	carrier := metadataSupplier{md: &md}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return metadata.NewOutgoingContext(ctx, md)
}

// ExtractGRPCMetadata 从 gRPC incoming metadata 提取 trace context 到 ctx。
// 供服务端在不使用拦截器的场景手动提取 trace context。
func ExtractGRPCMetadata(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		md = metadata.New(nil)
	}
	carrier := metadataSupplier{md: &md}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}
