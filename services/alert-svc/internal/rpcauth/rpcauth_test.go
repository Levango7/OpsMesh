package rpcauth

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// 用一个"必然不该被调用"的 handler 来证明拦截器真的挡在了业务之前。
func reachedHandler(ctx context.Context, req interface{}) (interface{}, error) {
	return "HANDLER_REACHED", nil
}

func call(t *testing.T, it grpc.UnaryServerInterceptor, md metadata.MD, method string) (interface{}, error, bool) {
	t.Helper()
	ctx := context.Background()
	if md != nil {
		ctx = metadata.NewIncomingContext(ctx, md)
	}
	reached := false
	h := func(ctx context.Context, req interface{}) (interface{}, error) {
		reached = true
		return reachedHandler(ctx, req)
	}
	info := &grpc.UnaryServerInfo{FullMethod: method}
	resp, err := it(ctx, nil, info, h)
	return resp, err, reached
}

const token = "s3cret-token"

func TestEmptyTokenPassesThrough(t *testing.T) {
	// 空 token = 不鉴权：这是"升级不改变现有部署"的前提，必须被测住而不是靠读代码相信。
	_, err, reached := call(t, New(""), nil, "/opsmesh.alert.v1.AlertService/AcknowledgeAlert")
	if err != nil || !reached {
		t.Fatalf("空 token 应放行到 handler：err=%v reached=%v", err, reached)
	}
}

func TestMissingMetadataRejected(t *testing.T) {
	_, err, reached := call(t, New(token), nil, "/opsmesh.alert.v1.AlertService/AcknowledgeAlert")
	if reached {
		t.Fatal("缺少凭据却进入了 handler")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("期望 Unauthenticated，实得 %v", status.Code(err))
	}
}

func TestWrongTokenRejected(t *testing.T) {
	md := metadata.New(map[string]string{"authorization": "Bearer wrong-token"})
	_, err, reached := call(t, New(token), md, "/opsmesh.alert.v1.AlertService/ResolveAlert")
	if reached {
		t.Fatal("错误凭据却进入了 handler")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("期望 Unauthenticated，实得 %v", status.Code(err))
	}
}

// 只带裸 token（没有 "Bearer " 前缀）也必须拒——否则等于悄悄支持两种格式，
// 而文档只承诺一种，将来排查"为什么我的调用 401"时两种都会出现。
func TestRawTokenWithoutSchemeRejected(t *testing.T) {
	md := metadata.New(map[string]string{"authorization": token})
	if _, err, reached := call(t, New(token), md, "/opsmesh.alert.v1.AlertService/ResolveAlert"); reached || err == nil {
		t.Fatal("裸 token（无 Bearer 前缀）应当被拒")
	}
}

// 多一条 authorization 也要拒：如果取 md.Get(...)[0] 就放行，攻击面变成"塞两条挑一条"。
func TestMultipleAuthorizationHeadersRejected(t *testing.T) {
	md := metadata.New(map[string]string{})
	md.Append("authorization", "Bearer "+token)
	md.Append("authorization", "Bearer other")
	if _, err, reached := call(t, New(token), md, "/opsmesh.alert.v1.AlertService/ResolveAlert"); reached || status.Code(err) != codes.Unauthenticated {
		t.Fatalf("重复 authorization 应当被拒，实得 err=%v reached=%v", err, reached)
	}
}

func TestCorrectTokenAllowed(t *testing.T) {
	md := metadata.New(map[string]string{"authorization": "Bearer " + token})
	resp, err, reached := call(t, New(token), md, "/opsmesh.alert.v1.AlertService/AcknowledgeAlert")
	if err != nil || !reached {
		t.Fatalf("正确凭据应当放行：err=%v reached=%v", err, reached)
	}
	if resp != "HANDLER_REACHED" {
		t.Fatalf("handler 返回值被吞：got=%v", resp)
	}
}

func TestHealthMethodExempt(t *testing.T) {
	// 豁免只适用于 health；告警方法仍要凭据。两条一起测才说明豁免范围不是"整条链空转"。
	if _, err, reached := call(t, New(token), nil, "/grpc.health.v1.Health/Check"); !reached || err != nil {
		t.Fatalf("health 应免鉴权：err=%v reached=%v", err, reached)
	}
	if _, err, reached := call(t, New(token), nil, "/opsmesh.alert.v1.AlertService/ListAlerts"); reached || err == nil {
		t.Fatalf("非 health 方法不应因豁免规则被放行：err=%v reached=%v", err, reached)
	}
}
