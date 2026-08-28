// gRPC 拦截器：服务间 JWT 传播与校验。
package auth

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// gRPC metadata key for service token.
const grpcTokenKey = "x-service-token"

// StreamServerInterceptor 服务端流式 gRPC 拦截器：从 metadata 提取并校验 JWT。
// 校验通过后把 ServiceClaims 注入 context。
func StreamServerInterceptor(secret string) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		claims, err := extractAndValidate(ss.Context(), secret)
		if err != nil {
			return err
		}
		ctx := context.WithValue(ss.Context(), authenticatedClaimsKey, claims)
		return handler(srv, &serverStreamWithContext{ServerStream: ss, ctx: ctx})
	}
}

// UnaryServerInterceptor 服务端一元 gRPC 拦截器：从 metadata 提取并校验 JWT。
func UnaryServerInterceptor(secret string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		claims, err := extractAndValidate(ctx, secret)
		if err != nil {
			return nil, err
		}
		newCtx := context.WithValue(ctx, authenticatedClaimsKey, claims)
		return handler(newCtx, req)
	}
}

// StreamClientInterceptor 客户端流式 gRPC 拦截器：向 metadata 注入 JWT token。
func StreamClientInterceptor(token string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			md = metadata.Pairs(grpcTokenKey, token)
		} else {
			md = md.Copy()
			md.Set(grpcTokenKey, token)
		}
		newCtx := metadata.NewOutgoingContext(ctx, md)
		return streamer(newCtx, desc, cc, method, opts...)
	}
}

// UnaryClientInterceptor 客户端一元 gRPC 拦截器：向 metadata 注入 JWT token。
func UnaryClientInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			md = metadata.Pairs(grpcTokenKey, token)
		} else {
			md = md.Copy()
			md.Set(grpcTokenKey, token)
		}
		newCtx := metadata.NewOutgoingContext(ctx, md)
		return invoker(newCtx, method, req, reply, cc, opts...)
	}
}

// ClaimsFromContext 从经过拦截器的 gRPC context 提取 ServiceClaims。
func ClaimsFromContext(ctx context.Context) (*ServiceClaims, bool) {
	claims, ok := ctx.Value(authenticatedClaimsKey).(*ServiceClaims)
	return claims, ok
}

// extractAndValidate 从 gRPC metadata 提取 token 并校验。
func extractAndValidate(ctx context.Context, secret string) (*ServiceClaims, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, errors.New("auth: gRPC metadata 缺失")
	}
	vals := md.Get(grpcTokenKey)
	if len(vals) == 0 {
		return nil, errors.New("auth: gRPC metadata 中无服务 token")
	}
	claims, err := ValidateServiceToken(vals[0], secret)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// serverStreamWithContext 包装 ServerStream 以替换 context。
type serverStreamWithContext struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *serverStreamWithContext) Context() context.Context {
	return s.ctx
}
