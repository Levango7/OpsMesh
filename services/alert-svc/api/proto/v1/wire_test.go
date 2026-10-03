// wire_test.go 证明"RPC 真的能经 gRPC 线路编解码"，而不是只在进程内被直接调用。
//
// 为什么必须有这个文件（2026-10-03 的 P0 复盘）：此前 api/proto 下的类型是**手写 Go struct**，
// 不满足 grpc 默认 proto codec 对 proto.Message 的要求，于是线上任何一次调用都会得到
// `proto: failed to marshal, message is *alertv1.CreateRuleRequest, want proto.Message`。
// 而仓库里所有测试都是 in-process 直接调 Service 方法 ⇒ 永远碰不到编解码路径，
// 所以 CI 全绿、健康检查全绿，API 却 100% 不可用。
//
// 本测试走 bufconn（真 gRPC server + 真 client + 真 codec，只是不经 TCP），
// 只要有人把手写类型塞回来、或生成物与 .proto 漂移导致类型不再实现 proto.Message，它就会红。
package alertv1_test

import (
	"context"
	"net"
	"testing"
	"time"

	alertv1 "github.com/Levango7/OpsMesh/services/alert-svc/api/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// stubServer 只实现本测试需要的一条方法，其余由生成的 Unimplemented* 兜底。
type stubServer struct {
	alertv1.UnimplementedAlertServiceServer
	got *alertv1.CreateRuleRequest
}

func (s *stubServer) CreateRule(_ context.Context, in *alertv1.CreateRuleRequest) (*alertv1.AlertRule, error) {
	s.got = in
	return in.Rule, nil
}

func TestAlertServiceRPCCrossesTheWire(t *testing.T) {
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	stub := &stubServer{}
	alertv1.RegisterAlertServiceServer(srv, stub)
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	// 必须用 passthrough：默认 dns resolver 会去解析 "bufnet" 这个名字并返回零地址
	// （实测报 "name resolver error: produced zero addresses"，与线路本身无关）。
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	want := &alertv1.AlertRule{
		Id: "wire-1", TenantId: "t1", Name: "线路冒烟", Metric: "cpu", Op: ">",
		Threshold: 90, Severity: "critical", Enabled: true,
	}
	got, err := alertv1.NewAlertServiceClient(conn).CreateRule(ctx, &alertv1.CreateRuleRequest{Rule: want})
	if err != nil {
		t.Fatalf("CreateRule 经线路失败（这条错误就是 #59 的成因形态）：%v", err)
	}
	if got.GetId() != want.Id || got.GetThreshold() != want.Threshold || !got.GetEnabled() {
		t.Fatalf("服务端收到的是另一条规则：%+v", got)
	}
	if stub.got == nil || stub.got.GetRule().GetTenantId() != "t1" {
		t.Fatalf("服务端未收到预期载荷：%+v", stub.got)
	}
}
