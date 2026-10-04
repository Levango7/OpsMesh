// wire_test.go 证明 log-svc 的 gRPC 契约真能过线路编解码（#59 的防复发门禁）。
//
// log-svc 原本就有 *_grpc.pb.go，但消息类型仍是手写 struct ⇒ 与其余服务同样卡在
// proto codec。这里除了逐方法过 codec，还用生成的 typed client 真打一次 AppendLog，
// 兜住"生成的 client/Register 签名仍然对得上"。
package logv1_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/Levango7/OpsMesh/pkg/grpcwire"

	logv1 "opsmesh.io/log-svc/api/proto/v1"
)

func TestAllMethodsCrossCodec(t *testing.T) {
	if got := grpcwire.AssertUnaryMethodsCrossCodec(t, "opsmesh.log.v1"); got != 4 {
		t.Fatalf("只覆盖到 %d 条 unary 方法，与本包 .proto 的条数不符——说明有方法没真过 codec，或包名/生成物漂移", got)
	}
}

func TestGeneratedFilesAreProtocOutput(t *testing.T) {
	grpcwire.AssertGeneratedFilesLookGenerated(t, "log.pb.go", "log_grpc.pb.go")
}

// stubServer 只实现一条代表方法，其余由生成的 Unimplemented* 兜底。
type stubServer struct {
	logv1.UnimplementedLogServiceServer
}

func (s *stubServer) AppendLog(_ context.Context, in *logv1.AppendLogRequest) (*logv1.LogEntry, error) {
	return &logv1.LogEntry{Message: in.GetMessage()}, nil
}

func TestTypedClientRoundTrip(t *testing.T) {
	conn, cleanup := grpcwire.ServeBufconn(t, func(srv *grpc.Server) {
		logv1.RegisterLogServiceServer(srv, &stubServer{})
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	got, err := logv1.NewLogServiceClient(conn).AppendLog(ctx, &logv1.AppendLogRequest{Message: "线路冒烟"})
	if err != nil {
		t.Fatalf("AppendLog 经线路失败（这就是 #59 的成因形态）：%v", err)
	}
	if got.GetMessage() != "线路冒烟" {
		t.Fatalf("服务端回的不是发过去的日志：%+v", got)
	}
}
