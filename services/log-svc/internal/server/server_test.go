package server

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	logv1 "opsmesh.io/log-svc/api/proto/v1"
	"opsmesh.io/log-svc/internal/service"
	"opsmesh.io/log-svc/pkg/logstore"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return NewServer(service.NewService(logstore.NewMemory(100)))
}

func TestTenantIDValidationMapsToInvalidArgument(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"SearchLogs", func() error {
			_, err := srv.SearchLogs(ctx, &logv1.SearchLogsRequest{})
			return err
		}},
		{"AppendLog", func() error {
			_, err := srv.AppendLog(ctx, &logv1.AppendLogRequest{})
			return err
		}},
		{"GetLogStats", func() error {
			_, err := srv.GetLogStats(ctx, &logv1.GetLogStatsRequest{})
			return err
		}},
		{"ListLogSources", func() error {
			_, err := srv.ListLogSources(ctx, &logv1.ListLogSourcesRequest{})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("%s 缺 tenant_id 应返回 InvalidArgument, got %v", tc.name, err)
			}
			if !strings.Contains(status.Convert(err).Message(), "tenant_id is required") {
				t.Fatalf("错误文案 = %q", status.Convert(err).Message())
			}
		})
	}
}

func TestAppendLogRequiresMessage(t *testing.T) {
	srv := newTestServer(t)
	_, err := srv.AppendLog(context.Background(), &logv1.AppendLogRequest{TenantId: "t1"})
	if status.Code(err) != codes.InvalidArgument || !strings.Contains(status.Convert(err).Message(), "message is required") {
		t.Fatalf("缺 message 应返回 InvalidArgument, got %v", err)
	}
}

// 校验必须先于委派：svc 为 nil 时，缺 tenant_id 的请求仍应拿到 InvalidArgument 而不是 panic。
func TestValidationRunsBeforeDelegation(t *testing.T) {
	srv := NewServer(nil)
	_, err := srv.SearchLogs(context.Background(), &logv1.SearchLogsRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("got %v, want InvalidArgument", err)
	}
}

func TestAppendThenSearchStatsAndSources(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	entry, err := srv.AppendLog(ctx, &logv1.AppendLogRequest{
		TenantId: "t1",
		DeviceId: "dev-1",
		AgentId:  "agent-1",
		Level:    "error",
		Source:   "agent",
		Message:  "boom",
	})
	if err != nil {
		t.Fatalf("AppendLog: %v", err)
	}
	if entry.GetTenantId() != "t1" || entry.GetMessage() != "boom" || entry.GetLevel() != "error" {
		t.Fatalf("AppendLog 响应字段失真: %+v", entry)
	}

	resp, err := srv.SearchLogs(ctx, &logv1.SearchLogsRequest{TenantId: "t1"})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}
	if resp.GetTotal() != 1 || len(resp.GetEntries()) != 1 {
		t.Fatalf("SearchLogs total = %d, entries = %d, want 1", resp.GetTotal(), len(resp.GetEntries()))
	}
	if resp.GetEntries()[0].GetMessage() != "boom" {
		t.Fatalf("SearchLogs 条目 = %+v", resp.GetEntries()[0])
	}

	stats, err := srv.GetLogStats(ctx, &logv1.GetLogStatsRequest{TenantId: "t1"})
	if err != nil {
		t.Fatalf("GetLogStats: %v", err)
	}
	if stats.GetTotalEntries() != 1 {
		t.Fatalf("GetLogStats total = %d, want 1", stats.GetTotalEntries())
	}

	if _, err := srv.ListLogSources(ctx, &logv1.ListLogSourcesRequest{TenantId: "t1"}); err != nil {
		t.Fatalf("ListLogSources: %v", err)
	}
}

func TestJSONCodecRoundTrip(t *testing.T) {
	codec := jsonCodec{}
	in := map[string]string{"tenant": "t1", "msg": "hello"}

	raw, err := codec.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out map[string]string
	if err := codec.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out["tenant"] != "t1" || out["msg"] != "hello" {
		t.Fatalf("往返失真: %v", out)
	}
	if codec.Name() != "json" || codec.String() != "json" {
		t.Fatalf("codec 名称 = %q/%q, want json", codec.Name(), codec.String())
	}

	if err := codec.Unmarshal([]byte("{"), &out); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

// 端到端：真实 gRPC 栈 + JSON codec 的注册/服务链路必须可用（main 就用这条路径）。
func TestGRPCServerEndToEndWithJSONCodec(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	gs := NewGRPCServer(service.NewService(logstore.NewMemory(10)))
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(jsonCodec{})),
	)
	if err != nil {
		t.Fatalf("拨号: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := logv1.NewLogServiceClient(conn)

	if _, err := client.SearchLogs(ctx, &logv1.SearchLogsRequest{TenantId: "t1"}); err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}
}

// 注意：main.go:81-84 把 service.Service 直接交给 NewGRPCServer，没有经过
// NewServer 的校验包装器（仓内 NewServer 零调用）。因此上述 E2E 只验证真实
// 生产接线（原始 service + JSON codec + gRPC 栈）能工作；tenant_id 校验是
// 包装器契约，见 TestTenantIDValidationMapsToInvalidArgument，已在板面登记
// 「校验层未接线」待 owner 处置，测试不把缺失校验写成正确行为。
