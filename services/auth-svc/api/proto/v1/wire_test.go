// wire_test.go 证明 auth-svc 的 gRPC 契约真能过线路编解码（#59 的防复发门禁）。
//
// 背景：api/proto/v1 下的类型此前是手写 Go struct，没有 ProtoReflect()，不满足 grpc
// 默认 proto codec 对 proto.Message 的要求 ⇒ 每一条 RPC 都在编解码阶段失败
// （proto: failed to marshal, message is *authv1.LoginRequest, want proto.Message）。
// 当时没有任何测试 dial 过这些服务（全是进程内直调方法），于是
// "CI 全绿 + gRPC API 100% 不可用"长期共存。
//
// 两条检查各管一件事：
//   - TestAllMethodsCrossCodec：本包全部 unary 方法逐条走真 codec（请求与响应两个方向）；
//   - TestGeneratedFilesAreProtocOutput：文件必须是 protoc 产物——手写版同名文件也过编译，
//     光看文件名判不出来，所以查生成标记。
package authv1_test

import (
	"testing"

	"github.com/Levango7/OpsMesh/pkg/grpcwire"
)

func TestAllMethodsCrossCodec(t *testing.T) {
	if got := grpcwire.AssertUnaryMethodsCrossCodec(t, "opsmesh.auth.v1"); got != 19 {
		t.Fatalf("本包应覆盖 19 条 unary 方法，实际检查到 %d 条——覆盖数与 .proto 不符，说明有方法没真过 codec（或包名/生成物漂移）", got)
	}
}

func TestGeneratedFilesAreProtocOutput(t *testing.T) {
	grpcwire.AssertGeneratedFilesLookGenerated(t, "auth.pb.go", "auth_grpc.pb.go")
}
