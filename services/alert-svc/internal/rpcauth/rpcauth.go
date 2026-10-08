// Package rpcauth 给 alert-svc 的 gRPC 面提供可选的共享密钥鉴权。
//
// 为什么是"可选"而不是"默认开启"：出厂 compose 把该端口发布成 127.0.0.1:${ALERT_SVC_GRPC_PORT}
// （docker-compose.prod.yml 第 714-724 行），门禁第 18 节断言"出厂发布端口除白名单外只绑环回"，
// 且全仓没有任何组件 dial 这条 gRPC（2026-10-03 实测：NewAlertServiceClient 除生成桩外零调用点）。
// 空 token = 不鉴权 ⇒ 现有部署升级后行为不变。
//
// 为什么必须存在：`PAGERDUTY_ENABLED=true` 之后，ack/resolve **只有这条 gRPC 入口**
// （服务侧没有对应 REST 路由，也没有 grpc-gateway/reflection，见 docs/operations.md §4.6.0），
// 于是任何能连到该端口的进程都能改告警状态并触发值班外发。config.Validate() 已经把那种
// 组合拦成"启动即拒"，本包负责真的校验来路。
package rpcauth

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	// mdAuthorization 是 gRPC 生态约定的凭据头名（小写：HTTP/2 元数据键强制小写）。
	mdAuthorization = "authorization"
	// bearerPrefix 与仓库其它面一致：网关侧 REST 也按 "Bearer <token>" 收。
	bearerPrefix = "Bearer "
	// healthMethodPrefix 豁免 gRPC 标准健康服务。
	// 豁免的理由要说清：健康探测是"这个进程能不能服务"的存活信号，探针侧（kubelet /
	// grpc_health_probe）通常无法携带业务凭据；把它纳入鉴权会让"没配 token"变成"节点全红"，
	// 那是拿可用性换一个不存在的保护——健康响应里没有任何告警数据。
	// 本仓出厂探针其实走 HTTP /health（compose 第 740 行），所以豁免不是必需，只是不埋雷。
	healthMethodPrefix = "/grpc.health.v1.Health/"
)

// New 返回校验共享密钥的一元拦截器。token 为空时返回一个直接放行的拦截器——
// 调用方可以无条件挂进链里，"有没有开鉴权"只由 token 是否配置决定，不留两处判断。
func New(token string) grpc.UnaryServerInterceptor {
	if token == "" {
		return passthrough
	}
	// 把期望值预先算成完整头内容，比较时用常数时间，避免按字节提前返回造成的时序侧信道。
	want := []byte(bearerPrefix + token)
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if info != nil && strings.HasPrefix(info.FullMethod, healthMethodPrefix) {
			return handler(ctx, req)
		}
		if err := authorize(ctx, want); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

func passthrough(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	return handler(ctx, req)
}

// authorize 只回 "未认证"，不回"哪里错了"到能帮人枚举的程度：
// 缺头 / 多头 / 值不符 一律同一个码；细分原因只进服务端日志，不进响应。
func authorize(ctx context.Context, want []byte) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "未认证：请求缺少凭据")
	}
	got := md.Get(mdAuthorization)
	if len(got) != 1 {
		return status.Error(codes.Unauthenticated, "未认证：请求凭据不正确")
	}
	if subtle.ConstantTimeCompare([]byte(got[0]), want) != 1 {
		return status.Error(codes.Unauthenticated, "未认证：请求凭据不正确")
	}
	return nil
}
