// wire_test.go 证明 task-svc 的 gRPC 契约真能过线路编解码（#59 的防复发门禁）。
//
// 与 auth/config/device 同一形态：api/proto/v1 下原本是手写 struct，任何一条 RPC 都会
// 在 proto codec 的 marshal 阶段失败，而仓库里当时没有一条测试 dial 过这些服务。
// task-svc 尤其要紧：agent 的任务领取/上报在生产形态有 gRPC 通道，编解码失败等于全链路停摆。
package taskv1_test

import (
	"testing"

	"github.com/Levango7/OpsMesh/pkg/grpcwire"
)

func TestAllMethodsCrossCodec(t *testing.T) {
	if got := grpcwire.AssertUnaryMethodsCrossCodec(t, "opsmesh.task.v1"); got != 20 {
		t.Fatalf("只覆盖到 %d 条 unary 方法，与本包 .proto 的条数不符——说明有方法没真过 codec，或包名/生成物漂移", got)
	}
}

func TestGeneratedFilesAreProtocOutput(t *testing.T) {
	grpcwire.AssertGeneratedFilesLookGenerated(t, "task.pb.go", "task_grpc.pb.go")
}
