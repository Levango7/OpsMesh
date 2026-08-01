package grpcx

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	pb "opsmesh/internal/grpcx/pb" // 包名为 pbv1，用 pb 别名引用（M4-4C 兼容性修复）
	"opsmesh/internal/proto"
)

// fakeLegacyServer 是 grpcx.RegistrationServer 的测试假实现，
// 记录收到的请求，返回固定响应，用于验证 StubAdapter 的互转正确性。
type fakeLegacyServer struct {
	lastAgent    *proto.AgentInfo
	lastHeart    *HeartbeatReq
	lastPullAid  string
	lastResult   *proto.TaskResult
	lastCancel   *CancelTaskReq
	lastPollAid  string
	registerResp *RegisterResp
	pullTasks    []proto.Task
	cancelledIDs []string
}

func (f *fakeLegacyServer) Register(ctx context.Context, info *proto.AgentInfo) (*RegisterResp, error) {
	f.lastAgent = info
	if f.registerResp != nil {
		return f.registerResp, nil
	}
	return &RegisterResp{AgentID: "agent-fake", ControlConfig: map[string]int{"heartbeatInterval": 10}}, nil
}
func (f *fakeLegacyServer) Heartbeat(ctx context.Context, req *HeartbeatReq) (*Empty, error) {
	f.lastHeart = req
	return &Empty{}, nil
}
func (f *fakeLegacyServer) PullTasks(ctx context.Context, req *PullTasksReq) (*PullTasksResp, error) {
	f.lastPullAid = req.AgentID
	return &PullTasksResp{Tasks: f.pullTasks}, nil
}
func (f *fakeLegacyServer) ReportResult(ctx context.Context, res *proto.TaskResult) (*Empty, error) {
	f.lastResult = res
	return &Empty{}, nil
}
func (f *fakeLegacyServer) CancelTask(ctx context.Context, req *CancelTaskReq) (*Empty, error) {
	f.lastCancel = req
	return &Empty{}, nil
}
func (f *fakeLegacyServer) PollCancels(ctx context.Context, req *PollCancelsReq) (*PollCancelsResp, error) {
	f.lastPollAid = req.AgentID
	return &PollCancelsResp{CancelledTaskIDs: f.cancelledIDs}, nil
}

// TestStubAdapterImplementsPbServer 编译期断言：StubAdapter 满足 pb.RegistrationServer 接口。
// 若 StubAdapter 缺方法或方法签名不匹配，本测试编译失败（无需运行）。
func TestStubAdapterImplementsPbServer(t *testing.T) {
	var _ pb.RegistrationServer = (*StubAdapter)(nil)
}

// TestStubAdapterRegister 验证 Register 路径：pb.AgentInfo -> proto.AgentInfo -> 业务 -> pb.RegisterResp。
func TestStubAdapterRegister(t *testing.T) {
	fake := &fakeLegacyServer{}
	adapter := NewStubAdapter(fake)

	in := &pb.AgentInfo{
		AgentId:    "a1",
		Hostname:   "host-a",
		Segment:    "seg-a",
		TenantId:   "t1",
		GrpcPort:   9090,
		Load:       7,
		Status:     "online",
		InstallToken: "tok-123",
	}
	resp, err := adapter.Register(context.Background(), in)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp.AgentId != "agent-fake" {
		t.Fatalf("AgentId = %q, want agent-fake", resp.AgentId)
	}
	if v := resp.ControlConfig["heartbeatInterval"]; v != 10 {
		t.Fatalf("ControlConfig[heartbeatInterval] = %d, want 10", v)
	}
	// 验证互转：pb.AgentInfo -> proto.AgentInfo 字段逐一对齐。
	got := fake.lastAgent
	if got.AgentID != "a1" || got.Hostname != "host-a" || got.Segment != "seg-a" ||
		got.TenantID != "t1" || got.GRPCPort != 9090 || got.Load != 7 ||
		got.Status != "online" || got.InstallToken != "tok-123" {
		t.Fatalf("AgentInfo 互转丢失字段: %+v", got)
	}
}

// TestStubAdapterHeartbeat 验证 Heartbeat 路径含 CmdbReport 嵌套互转。
func TestStubAdapterHeartbeat(t *testing.T) {
	fake := &fakeLegacyServer{}
	adapter := NewStubAdapter(fake)

	in := &pb.HeartbeatReq{
		AgentId: "a1",
		Status:  "online",
		Load:    3,
		CmdbReport: &pb.CmdbReport{
			CiType: "machine",
			Seq:    42,
			Attrs: []*pb.CmdbAttr{
				{Key: "os.version", Value: "ubuntu22.04", Type: "string"},
			},
		},
	}
	if _, err := adapter.Heartbeat(context.Background(), in); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	got := fake.lastHeart
	if got.AgentID != "a1" || got.Status != "online" || got.Load != 3 {
		t.Fatalf("HeartbeatReq 互转丢失字段: %+v", got)
	}
	if got.CmdbReport == nil || got.CmdbReport.CiType != "machine" || got.CmdbReport.Seq != 42 {
		t.Fatalf("CmdbReport 互转丢失: %+v", got.CmdbReport)
	}
	if len(got.CmdbReport.Attrs) != 1 || got.CmdbReport.Attrs[0].Key != "os.version" {
		t.Fatalf("CmdbAttr 互转丢失: %+v", got.CmdbReport.Attrs)
	}
}

// TestStubAdapterPullTasks 验证 PullTasks 路径：返回的 proto.Task 列表转为 pb.Task 列表。
func TestStubAdapterPullTasks(t *testing.T) {
	fake := &fakeLegacyServer{
		pullTasks: []proto.Task{
			{TaskID: "tk-1", AgentID: "a1", Type: "shell", Command: "echo hi", Status: "pending"},
		},
	}
	adapter := NewStubAdapter(fake)

	resp, err := adapter.PullTasks(context.Background(), &pb.PullTasksReq{AgentId: "a1"})
	if err != nil {
		t.Fatalf("PullTasks: %v", err)
	}
	if fake.lastPullAid != "a1" {
		t.Fatalf("PullTasksReq.AgentID 互转丢失: %q", fake.lastPullAid)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].TaskId != "tk-1" || resp.Tasks[0].Command != "echo hi" {
		t.Fatalf("Task 互转丢失: %+v", resp.Tasks)
	}
}

// TestStubAdapterReportResult 验证 ReportResult 路径。
func TestStubAdapterReportResult(t *testing.T) {
	fake := &fakeLegacyServer{}
	adapter := NewStubAdapter(fake)

	in := &pb.TaskResult{TaskId: "tk-1", AgentId: "a1", ExitCode: 0, Stdout: "ok", DurationMs: 120}
	if _, err := adapter.ReportResult(context.Background(), in); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
	got := fake.lastResult
	if got.TaskID != "tk-1" || got.AgentID != "a1" || got.ExitCode != 0 || got.Stdout != "ok" || got.DurationMs != 120 {
		t.Fatalf("TaskResult 互转丢失字段: %+v", got)
	}
}

// TestStubAdapterCancelTask 验证 CancelTask 路径。
func TestStubAdapterCancelTask(t *testing.T) {
	fake := &fakeLegacyServer{}
	adapter := NewStubAdapter(fake)

	if _, err := adapter.CancelTask(context.Background(), &pb.CancelTaskReq{TaskId: "tk-1", TenantId: "t1"}); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if fake.lastCancel.TaskID != "tk-1" || fake.lastCancel.TenantID != "t1" {
		t.Fatalf("CancelTaskReq 互转丢失: %+v", fake.lastCancel)
	}
}

// TestStubAdapterPollCancels 验证 PollCancels 路径。
func TestStubAdapterPollCancels(t *testing.T) {
	fake := &fakeLegacyServer{cancelledIDs: []string{"tk-2", "tk-3"}}
	adapter := NewStubAdapter(fake)

	resp, err := adapter.PollCancels(context.Background(), &pb.PollCancelsReq{AgentId: "a1"})
	if err != nil {
		t.Fatalf("PollCancels: %v", err)
	}
	if fake.lastPollAid != "a1" {
		t.Fatalf("PollCancelsReq.AgentID 互转丢失: %q", fake.lastPollAid)
	}
	if len(resp.CancelledTaskIds) != 2 || resp.CancelledTaskIds[0] != "tk-2" {
		t.Fatalf("PollCancelsResp 互转丢失: %+v", resp.CancelledTaskIds)
	}
}

// TestPbServiceDescConsistency 验证生成 stub 的 ServiceDesc 与手写 ServiceDesc 同名同方法集。
// 兼容期两条路径必须保持契约一致，否则切换会破坏客户端。
func TestPbServiceDescConsistency(t *testing.T) {
	pbDesc := pb.Registration_ServiceDesc
	legacyDesc := Registration_ServiceDesc

	if pbDesc.ServiceName != legacyDesc.ServiceName {
		t.Fatalf("ServiceName 不一致: pb=%q legacy=%q", pbDesc.ServiceName, legacyDesc.ServiceName)
	}
	if len(pbDesc.Methods) != len(legacyDesc.Methods) {
		t.Fatalf("方法数不一致: pb=%d legacy=%d", len(pbDesc.Methods), len(legacyDesc.Methods))
	}
	// 方法名集合应完全相同（顺序也应对齐，因 .proto 与 service.go 同序定义）。
	for i, m := range pbDesc.Methods {
		if m.MethodName != legacyDesc.Methods[i].MethodName {
			t.Fatalf("方法[%d] 不一致: pb=%q legacy=%q", i, m.MethodName, legacyDesc.Methods[i].MethodName)
		}
	}
	if len(pbDesc.Streams) != 0 || len(legacyDesc.Streams) != 0 {
		t.Fatal("两条路径都不应有流式方法")
	}
	// 编译期类型断言：pbDesc 是 grpc.ServiceDesc。
	var _ grpc.ServiceDesc = pbDesc
}