// wire_test.go 证明 device-svc 的 gRPC 契约真能过线路编解码（#59 的防复发门禁）。
//
// 背景：api/proto/v1 下的类型此前是手写 Go struct，没有 ProtoReflect()，不满足 grpc
// 默认 proto codec 对 proto.Message 的要求 ⇒ **每一条 RPC 都在编解码阶段失败**
// （proto: failed to marshal, message is *devicev1.RegisterDeviceRequest, want proto.Message）。
// 当时没有任何测试 dial 过这些服务（全是进程内直调方法），所以 CI 全绿而 API 100% 不可用。
//
// 两条线各管一件事：
//   - TestAllDeviceMethodsCrossCodec：本包 4 个 service 的全部 unary 方法逐条真过 codec；
//   - TestTypedClientsRoundTrip：生成的 NewXxxClient / RegisterXxxServer 名字与签名仍对得上
//     （手写版与生成版同名 ⇒ 调用点零改动，这个"同名"需要一条测试兜着）。
package devicev1_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/Levango7/OpsMesh/pkg/grpcwire"
	devicev1 "github.com/Levango7/OpsMesh/services/device-svc/api/proto/v1"
)

func TestAllDeviceMethodsCrossCodec(t *testing.T) {
	if got := grpcwire.AssertUnaryMethodsCrossCodec(t, "opsmesh.device.v1"); got != 22 {
		t.Fatalf("只覆盖到 %d 条 unary 方法，与本包 .proto 的条数不符——说明有方法没真过 codec，或包名/生成物漂移", got)
	}
}

func TestGeneratedFilesAreProtocOutput(t *testing.T) {
	grpcwire.AssertGeneratedFilesLookGenerated(t, "device.pb.go", "device_grpc.pb.go")
}

// stubServer 只实现每个 service 的一条代表方法，其余由生成的 Unimplemented* 兜底。
type stubServer struct {
	devicev1.UnimplementedDeviceServiceServer
	devicev1.UnimplementedAgentServiceServer
	devicev1.UnimplementedCMDBServiceServer
	devicev1.UnimplementedDiscoveryServiceServer
}

func (s *stubServer) RegisterDevice(_ context.Context, in *devicev1.RegisterDeviceRequest) (*devicev1.Device, error) {
	d := in.GetDevice()
	if d == nil {
		d = &devicev1.Device{Id: "fallback"}
	}
	return &devicev1.Device{Id: d.GetId(), Name: d.GetName(), Ip: d.GetIp()}, nil
}

func (s *stubServer) RegisterAgent(_ context.Context, in *devicev1.RegisterAgentRequest) (*devicev1.Agent, error) {
	return &devicev1.Agent{Id: in.GetAgent().GetId()}, nil
}

func (s *stubServer) CreateCI(_ context.Context, in *devicev1.CreateCIRequest) (*devicev1.CI, error) {
	return &devicev1.CI{Id: in.GetCi().GetId(), CiType: in.GetCi().GetCiType()}, nil
}

func (s *stubServer) StartDiscovery(_ context.Context, in *devicev1.StartDiscoveryRequest) (*devicev1.DiscoveryJob, error) {
	return &devicev1.DiscoveryJob{Id: "job-1", Cidr: in.GetCidr()}, nil
}

func TestTypedClientsRoundTrip(t *testing.T) {
	conn, cleanup := grpcwire.ServeBufconn(t, func(srv *grpc.Server) {
		stub := &stubServer{}
		devicev1.RegisterDeviceServiceServer(srv, stub)
		devicev1.RegisterAgentServiceServer(srv, stub)
		devicev1.RegisterCMDBServiceServer(srv, stub)
		devicev1.RegisterDiscoveryServiceServer(srv, stub)
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dev, err := devicev1.NewDeviceServiceClient(conn).RegisterDevice(ctx,
		&devicev1.RegisterDeviceRequest{Device: &devicev1.Device{Id: "dev-1", Name: "web-1", Ip: "10.0.0.1"}})
	if err != nil {
		t.Fatalf("RegisterDevice 经线路失败：%v", err)
	}
	if dev.GetId() != "dev-1" || dev.GetName() != "web-1" || dev.GetIp() != "10.0.0.1" {
		t.Fatalf("服务端回的不是发过去的设备：%+v", dev)
	}

	if _, err := devicev1.NewAgentServiceClient(conn).RegisterAgent(ctx,
		&devicev1.RegisterAgentRequest{Agent: &devicev1.Agent{Id: "ag-1"}}); err != nil {
		t.Fatalf("RegisterAgent 经线路失败：%v", err)
	}
	ci, err := devicev1.NewCMDBServiceClient(conn).CreateCI(ctx,
		&devicev1.CreateCIRequest{Ci: &devicev1.CI{Id: "ci-1", CiType: "server"}})
	if err != nil {
		t.Fatalf("CreateCI 经线路失败：%v", err)
	}
	if ci.GetCiType() != "server" || ci.GetId() != "ci-1" {
		t.Fatalf("CreateCI 往返丢了字段：%+v", ci)
	}
	job, err := devicev1.NewDiscoveryServiceClient(conn).StartDiscovery(ctx,
		&devicev1.StartDiscoveryRequest{Cidr: "10.0.0.0/24"})
	if err != nil {
		t.Fatalf("StartDiscovery 经线路失败：%v", err)
	}
	if job.GetCidr() != "10.0.0.0/24" {
		t.Fatalf("StartDiscovery 往返丢了 cidr：%+v", job)
	}
}
