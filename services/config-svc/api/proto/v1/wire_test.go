// wire_test.go 证明 config-svc 的 gRPC 契约真能过线路编解码（#59 的防复发门禁）。
//
// 与 auth/device 同一形态：api/proto/v1 下原本是手写 struct，任何一条 RPC 都会在
// proto codec 的 marshal 阶段失败，而仓库里当时没有一条测试 dial 过它。
package configv1_test

import (
	"testing"

	"github.com/Levango7/OpsMesh/pkg/grpcwire"
)

func TestAllMethodsCrossCodec(t *testing.T) {
	if got := grpcwire.AssertUnaryMethodsCrossCodec(t, "opsmesh.config.v1"); got != 24 {
		t.Fatalf("本包应覆盖 24 条 unary 方法，实际检查到 %d 条——覆盖数与 .proto 不符，说明有方法没真过 codec（或包名/生成物漂移）", got)
	}
}

func TestGeneratedFilesAreProtocOutput(t *testing.T) {
	grpcwire.AssertGeneratedFilesLookGenerated(t, "config.pb.go", "config_grpc.pb.go")
}
