// Package grpcx grpcx_extra_test.go：补测 codec/stub/service 的未覆盖路径，
// 把 internal/grpcx 整体语句覆盖率从 48.9% 提升到 70%+。
//
// 覆盖目标：
//   - service.go 中 7 个 _Registration_*_Handler（原 0%）：dec 失败 / 无拦截器 / 带拦截器 三条路径。
//   - stub.go 中 0% 函数（TaskLegacy/AgentInfoToProto/TaskResultToProto）及部分覆盖函数的 nil 路径。
//   - stub.go 中 StubAdapter 各方法的 inner 返回 error 路径。
//   - codec.go 中 injectVersion/checkVersion/Marshal 的边界与错误路径。
//
// 仅新增测试，不修改任何源代码。
package grpcx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "opsmesh/internal/grpcx/pb"
	"opsmesh/internal/proto"
)

// ===== helper：可注入 error 的 fakeLegacyServer 变体 =====
//
// stub_test.go 中的 fakeLegacyServer 不支持返回 error，这里定义一个 errLegacyServer
// 用于覆盖 StubAdapter 各方法的 inner 返回 error 分支（原 75%→100%）。

type errLegacyServer struct {
	err error // 每个方法都返回该 error
}

func (e *errLegacyServer) Register(ctx context.Context, info *proto.AgentInfo) (*RegisterResp, error) {
	return nil, e.err
}
func (e *errLegacyServer) Heartbeat(ctx context.Context, req *HeartbeatReq) (*Empty, error) {
	return nil, e.err
}
func (e *errLegacyServer) PullTasks(ctx context.Context, req *PullTasksReq) (*PullTasksResp, error) {
	return nil, e.err
}
func (e *errLegacyServer) ReportResult(ctx context.Context, res *proto.TaskResult) (*Empty, error) {
	return nil, e.err
}
func (e *errLegacyServer) CancelTask(ctx context.Context, req *CancelTaskReq) (*Empty, error) {
	return nil, e.err
}
func (e *errLegacyServer) PollCancels(ctx context.Context, req *PollCancelsReq) (*PollCancelsResp, error) {
	return nil, e.err
}
func (e *errLegacyServer) ReportLogs(ctx context.Context, req *ReportLogsReq) (*Empty, error) {
	return nil, e.err
}

// ===== StubAdapter 错误路径测试 =====

// TestStubAdapterRegisterError 覆盖 StubAdapter.Register 的 inner 返回 error 分支。
func TestStubAdapterRegisterError(t *testing.T) {
	sentinel := errors.New("register boom")
	adapter := NewStubAdapter(&errLegacyServer{err: sentinel})
	resp, err := adapter.Register(context.Background(), &pb.AgentInfo{AgentId: "a1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel error, got %v", err)
	}
	if resp != nil {
		t.Fatalf("resp should be nil on error, got %+v", resp)
	}
}

// TestStubAdapterHeartbeatError 覆盖 StubAdapter.Heartbeat 的 inner 返回 error 分支。
func TestStubAdapterHeartbeatError(t *testing.T) {
	sentinel := errors.New("heartbeat boom")
	adapter := NewStubAdapter(&errLegacyServer{err: sentinel})
	resp, err := adapter.Heartbeat(context.Background(), &pb.HeartbeatReq{AgentId: "a1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel error, got %v", err)
	}
	if resp != nil {
		t.Fatalf("resp should be nil on error, got %+v", resp)
	}
}

// TestStubAdapterPullTasksError 覆盖 StubAdapter.PullTasks 的 inner 返回 error 分支。
func TestStubAdapterPullTasksError(t *testing.T) {
	sentinel := errors.New("pull boom")
	adapter := NewStubAdapter(&errLegacyServer{err: sentinel})
	resp, err := adapter.PullTasks(context.Background(), &pb.PullTasksReq{AgentId: "a1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel error, got %v", err)
	}
	if resp != nil {
		t.Fatalf("resp should be nil on error, got %+v", resp)
	}
}

// TestStubAdapterReportResultError 覆盖 StubAdapter.ReportResult 的 inner 返回 error 分支。
func TestStubAdapterReportResultError(t *testing.T) {
	sentinel := errors.New("report boom")
	adapter := NewStubAdapter(&errLegacyServer{err: sentinel})
	resp, err := adapter.ReportResult(context.Background(), &pb.TaskResult{TaskId: "tk-1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel error, got %v", err)
	}
	if resp != nil {
		t.Fatalf("resp should be nil on error, got %+v", resp)
	}
}

// TestStubAdapterCancelTaskError 覆盖 StubAdapter.CancelTask 的 inner 返回 error 分支。
func TestStubAdapterCancelTaskError(t *testing.T) {
	sentinel := errors.New("cancel boom")
	adapter := NewStubAdapter(&errLegacyServer{err: sentinel})
	resp, err := adapter.CancelTask(context.Background(), &pb.CancelTaskReq{TaskId: "tk-1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel error, got %v", err)
	}
	if resp != nil {
		t.Fatalf("resp should be nil on error, got %+v", resp)
	}
}

// TestStubAdapterPollCancelsError 覆盖 StubAdapter.PollCancels 的 inner 返回 error 分支。
func TestStubAdapterPollCancelsError(t *testing.T) {
	sentinel := errors.New("poll boom")
	adapter := NewStubAdapter(&errLegacyServer{err: sentinel})
	resp, err := adapter.PollCancels(context.Background(), &pb.PollCancelsReq{AgentId: "a1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel error, got %v", err)
	}
	if resp != nil {
		t.Fatalf("resp should be nil on error, got %+v", resp)
	}
}

// ===== stub.go 互转函数 nil 路径与 0% 函数测试 =====

// TestStubConvertersNil 覆盖所有互转函数的 nil 入参分支（src==nil → nil）。
func TestStubConvertersNil(t *testing.T) {
	if got := AgentInfoLegacy(nil); got != nil {
		t.Fatalf("AgentInfoLegacy(nil) = %v, want nil", got)
	}
	if got := TaskLegacy(nil); got != nil {
		t.Fatalf("TaskLegacy(nil) = %v, want nil", got)
	}
	if got := TaskResultLegacy(nil); got != nil {
		t.Fatalf("TaskResultLegacy(nil) = %v, want nil", got)
	}
	if got := HeartbeatReqLegacy(nil); got != nil {
		t.Fatalf("HeartbeatReqLegacy(nil) = %v, want nil", got)
	}
	if got := CmdbReportLegacy(nil); got != nil {
		t.Fatalf("CmdbReportLegacy(nil) = %v, want nil", got)
	}
	if got := AgentInfoToProto(nil); got != nil {
		t.Fatalf("AgentInfoToProto(nil) = %v, want nil", got)
	}
	if got := TaskToProto(nil); got != nil {
		t.Fatalf("TaskToProto(nil) = %v, want nil", got)
	}
	if got := TaskResultToProto(nil); got != nil {
		t.Fatalf("TaskResultToProto(nil) = %v, want nil", got)
	}
	if got := RegisterRespToProto(nil); got != nil {
		t.Fatalf("RegisterRespToProto(nil) = %v, want nil", got)
	}
}

// TestTaskLegacy 覆盖 TaskLegacy 的非 nil 全字段互转（原 0%）。
func TestTaskLegacy(t *testing.T) {
	claimedAt := timestamppb.New(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC))
	createdAt := timestamppb.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	lastFiredAt := timestamppb.New(time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC))

	src := &pb.Task{
		TaskId:      "tk-1",
		AgentId:     "a1",
		TenantId:    "t1",
		Type:        "shell",
		Command:     "echo hi",
		Content:     "content-x",
		Path:        "/tmp/x",
		Status:      "pending",
		ClaimedBy:   "worker-1",
		ClaimedAt:   claimedAt,
		CreatedAt:   createdAt,
		RetryCount:  2,
		MaxRetries:  5,
		DeadLetter:  true,
		Schedule:    "*/5 * * * *",
		ParentId:    "tk-parent",
		LastFiredAt: lastFiredAt,
		DependsOn:   []string{"tk-dep1", "tk-dep2"},
	}
	got := TaskLegacy(src)
	if got.TaskID != "tk-1" || got.AgentID != "a1" || got.TenantID != "t1" {
		t.Fatalf("TaskLegacy 字段丢失: %+v", got)
	}
	if got.Type != "shell" || got.Command != "echo hi" || got.Content != "content-x" || got.Path != "/tmp/x" {
		t.Fatalf("TaskLegacy 命令字段丢失: %+v", got)
	}
	if got.Status != "pending" || got.ClaimedBy != "worker-1" {
		t.Fatalf("TaskLegacy 状态字段丢失: %+v", got)
	}
	if !got.ClaimedAt.Equal(claimedAt.AsTime()) || !got.CreatedAt.Equal(createdAt.AsTime()) {
		t.Fatalf("TaskLegacy 时间字段丢失: %+v", got)
	}
	if got.RetryCount != 2 || got.MaxRetries != 5 || !got.DeadLetter {
		t.Fatalf("TaskLegacy 重试字段丢失: %+v", got)
	}
	if got.Schedule != "*/5 * * * *" || got.ParentID != "tk-parent" {
		t.Fatalf("TaskLegacy 调度字段丢失: %+v", got)
	}
	if !got.LastFiredAt.Equal(lastFiredAt.AsTime()) {
		t.Fatalf("TaskLegacy LastFiredAt 丢失: %+v", got)
	}
	if len(got.DependsOn) != 2 || got.DependsOn[0] != "tk-dep1" || got.DependsOn[1] != "tk-dep2" {
		t.Fatalf("TaskLegacy DependsOn 丢失: %+v", got.DependsOn)
	}
}

// TestAgentInfoToProto 覆盖 AgentInfoToProto 的非 nil 全字段互转（原 0%）。
func TestAgentInfoToProto(t *testing.T) {
	lastSeen := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	src := &proto.AgentInfo{
		AgentID:         "a1",
		Hostname:        "host-1",
		Segment:         "seg-1",
		TenantID:        "t1",
		Addr:            "10.0.0.1",
		GRPCPort:        9090,
		MetricsPort:     9091,
		Status:          "online",
		Load:            42,
		LastSeen:        lastSeen,
		InstallToken:    "tok-abc",
		OnboardDeviceID: "dev-1",
	}
	got := AgentInfoToProto(src)
	if got.AgentId != "a1" || got.Hostname != "host-1" || got.Segment != "seg-1" {
		t.Fatalf("AgentInfoToProto 字段丢失: %+v", got)
	}
	if got.TenantId != "t1" || got.Addr != "10.0.0.1" || got.GrpcPort != 9090 || got.MetricsPort != 9091 {
		t.Fatalf("AgentInfoToProto 网络字段丢失: %+v", got)
	}
	if got.Status != "online" || got.Load != 42 {
		t.Fatalf("AgentInfoToProto 状态字段丢失: %+v", got)
	}
	if got.LastSeen == nil || !got.LastSeen.AsTime().Equal(lastSeen) {
		t.Fatalf("AgentInfoToProto LastSeen 丢失: %+v", got.LastSeen)
	}
	if got.InstallToken != "tok-abc" || got.OnboardDeviceId != "dev-1" {
		t.Fatalf("AgentInfoToProto token 字段丢失: %+v", got)
	}
}

// TestTaskResultToProto 覆盖 TaskResultToProto 的非 nil 全字段互转（原 0%）。
func TestTaskResultToProto(t *testing.T) {
	finishedAt := time.Date(2024, 4, 5, 6, 7, 8, 0, time.UTC)
	src := &proto.TaskResult{
		TaskID:     "tk-1",
		AgentID:    "a1",
		ExitCode:   0,
		Stdout:     "ok",
		Stderr:     "",
		DurationMs: 250,
		FinishedAt: finishedAt,
	}
	got := TaskResultToProto(src)
	if got.TaskId != "tk-1" || got.AgentId != "a1" || got.ExitCode != 0 {
		t.Fatalf("TaskResultToProto 字段丢失: %+v", got)
	}
	if got.Stdout != "ok" || got.Stderr != "" || got.DurationMs != 250 {
		t.Fatalf("TaskResultToProto 输出字段丢失: %+v", got)
	}
	if got.FinishedAt == nil || !got.FinishedAt.AsTime().Equal(finishedAt) {
		t.Fatalf("TaskResultToProto FinishedAt 丢失: %+v", got.FinishedAt)
	}
}

// TestRegisterRespToProtoNilAndEmpty 覆盖 RegisterRespToProto 的 nil 和空 ControlConfig 路径。
func TestRegisterRespToProtoNilAndEmpty(t *testing.T) {
	// 空 ControlConfig map。
	got := RegisterRespToProto(&RegisterResp{AgentID: "a1", ControlConfig: nil})
	if got.AgentId != "a1" {
		t.Fatalf("AgentId = %q, want a1", got.AgentId)
	}
	if len(got.ControlConfig) != 0 {
		t.Fatalf("ControlConfig 应为空, got %+v", got.ControlConfig)
	}
}

// TestHeartbeatReqLegacyNilCmdb 覆盖 HeartbeatReqLegacy 中 CmdbReport 为 nil 的路径。
func TestHeartbeatReqLegacyNilCmdb(t *testing.T) {
	src := &pb.HeartbeatReq{
		AgentId: "a1",
		Status:  "online",
		Load:    1,
		// CmdbReport 为 nil
	}
	got := HeartbeatReqLegacy(src)
	if got.AgentID != "a1" || got.Status != "online" || got.Load != 1 {
		t.Fatalf("HeartbeatReqLegacy 字段丢失: %+v", got)
	}
	if got.CmdbReport != nil {
		t.Fatalf("CmdbReport 应为 nil, got %+v", got.CmdbReport)
	}
}

// TestCmdbReportLegacyEmptyAttrs 覆盖 CmdbReportLegacy 中 Attrs 为空切片的路径。
func TestCmdbReportLegacyEmptyAttrs(t *testing.T) {
	src := &pb.CmdbReport{
		CiType: "machine",
		Seq:    1,
		Attrs:  nil, // 空 attrs
	}
	got := CmdbReportLegacy(src)
	if got.CiType != "machine" || got.Seq != 1 {
		t.Fatalf("CmdbReportLegacy 字段丢失: %+v", got)
	}
	if len(got.Attrs) != 0 {
		t.Fatalf("Attrs 应为空, got %+v", got.Attrs)
	}
}

// TestTaskToProtoZeroTime 覆盖 TaskToProto 中时间为零值时 tsToProto 返回 nil 的路径。
func TestTaskToProtoZeroTime(t *testing.T) {
	src := &proto.Task{
		TaskID:    "tk-1",
		ClaimedAt: time.Time{}, // 零值
		CreatedAt: time.Time{}, // 零值
	}
	got := TaskToProto(src)
	if got.TaskId != "tk-1" {
		t.Fatalf("TaskId = %q, want tk-1", got.TaskId)
	}
	if got.ClaimedAt != nil {
		t.Fatalf("ClaimedAt 应为 nil（零值时间）, got %+v", got.ClaimedAt)
	}
	if got.CreatedAt != nil {
		t.Fatalf("CreatedAt 应为 nil（零值时间）, got %+v", got.CreatedAt)
	}
}

// TestTsLegacyNil 覆盖 tsLegacy(nil) 返回零值 time.Time 的路径。
func TestTsLegacyNil(t *testing.T) {
	got := tsLegacy(nil)
	if !got.IsZero() {
		t.Fatalf("tsLegacy(nil) 应为零值时间, got %v", got)
	}
}

// TestTsToProtoZero 覆盖 tsToProto(零值时间) 返回 nil 的路径。
func TestTsToProtoZero(t *testing.T) {
	got := tsToProto(time.Time{})
	if got != nil {
		t.Fatalf("tsToProto(零值) 应为 nil, got %+v", got)
	}
}

// TestTsToProtoNonZero 覆盖 tsToProto 非零值路径，确保时间正确转换。
func TestTsToProtoNonZero(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	got := tsToProto(now)
	if got == nil {
		t.Fatal("tsToProto(非零) 不应返回 nil")
	}
	if !got.AsTime().Equal(now) {
		t.Fatalf("tsToProto 时间不匹配: got %v, want %v", got.AsTime(), now)
	}
}

// TestTsLegacyNonNil 覆盖 tsLegacy 非 nil 路径，确保时间正确转换。
func TestTsLegacyNonNil(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	ts := timestamppb.New(now)
	got := tsLegacy(ts)
	if !got.Equal(now) {
		t.Fatalf("tsLegacy 时间不匹配: got %v, want %v", got, now)
	}
}

// ===== service.go 7 个 Handler 测试 =====
//
// 每个 handler 测试三条路径：
//  1. dec 成功 + interceptor == nil → 直接调用 srv 方法。
//  2. dec 失败 → 返回 dec 的 error。
//  3. dec 成功 + interceptor != nil → 走拦截器路径。

// noOpInterceptor 是一个简单的拦截器，直接调用 handler 并返回其结果。
func noOpInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	return handler(ctx, req)
}

// recordingInterceptor 记录被调用时的 FullMethod，用于验证拦截器路径确实被走。
type recordingInterceptor struct {
	called   bool
	fullName string
}

func (r *recordingInterceptor) intercept(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	r.called = true
	r.fullName = info.FullMethod
	return handler(ctx, req)
}

// TestRegisterHandler 覆盖 _Registration_Register_Handler 的三条路径。
func TestRegisterHandler(t *testing.T) {
	fake := &fakeLegacyServer{}
	ctx := context.Background()

	// 路径 1：dec 成功 + 无拦截器。
	in := &proto.AgentInfo{AgentID: "a1", Hostname: "host-a"}
	dec := func(v interface{}) error {
		*(v.(*proto.AgentInfo)) = *in
		return nil
	}
	resp, err := _Registration_Register_Handler(fake, ctx, dec, nil)
	if err != nil {
		t.Fatalf("Register handler: %v", err)
	}
	if rr, ok := resp.(*RegisterResp); !ok || rr.AgentID != "agent-fake" {
		t.Fatalf("Register handler resp 错误: %+v", resp)
	}

	// 路径 2：dec 失败。
	decErr := errors.New("dec boom")
	badDec := func(v interface{}) error { return decErr }
	_, err = _Registration_Register_Handler(fake, ctx, badDec, nil)
	if !errors.Is(err, decErr) {
		t.Fatalf("want dec error, got %v", err)
	}

	// 路径 3：dec 成功 + 拦截器。
	rec := &recordingInterceptor{}
	resp, err = _Registration_Register_Handler(fake, ctx, dec, rec.intercept)
	if err != nil {
		t.Fatalf("Register handler with interceptor: %v", err)
	}
	if !rec.called {
		t.Fatal("拦截器未被调用")
	}
	if rec.fullName != "/opsmesh.v1.Registration/Register" {
		t.Fatalf("FullMethod = %q, want /opsmesh.v1.Registration/Register", rec.fullName)
	}
}

// TestHeartbeatHandler 覆盖 _Registration_Heartbeat_Handler 的三条路径。
func TestHeartbeatHandler(t *testing.T) {
	fake := &fakeLegacyServer{}
	ctx := context.Background()

	in := &HeartbeatReq{AgentID: "a1", Status: "online", Load: 1}
	dec := func(v interface{}) error {
		*(v.(*HeartbeatReq)) = *in
		return nil
	}
	resp, err := _Registration_Heartbeat_Handler(fake, ctx, dec, nil)
	if err != nil {
		t.Fatalf("Heartbeat handler: %v", err)
	}
	if _, ok := resp.(*Empty); !ok {
		t.Fatalf("Heartbeat handler resp 类型错误: %+v", resp)
	}

	decErr := errors.New("dec boom")
	badDec := func(v interface{}) error { return decErr }
	_, err = _Registration_Heartbeat_Handler(fake, ctx, badDec, nil)
	if !errors.Is(err, decErr) {
		t.Fatalf("want dec error, got %v", err)
	}

	rec := &recordingInterceptor{}
	resp, err = _Registration_Heartbeat_Handler(fake, ctx, dec, rec.intercept)
	if err != nil {
		t.Fatalf("Heartbeat handler with interceptor: %v", err)
	}
	if !rec.called {
		t.Fatal("拦截器未被调用")
	}
	if rec.fullName != "/opsmesh.v1.Registration/Heartbeat" {
		t.Fatalf("FullMethod = %q, want /opsmesh.v1.Registration/Heartbeat", rec.fullName)
	}
}

// TestPullTasksHandler 覆盖 _Registration_PullTasks_Handler 的三条路径。
func TestPullTasksHandler(t *testing.T) {
	fake := &fakeLegacyServer{
		pullTasks: []proto.Task{{TaskID: "tk-1", AgentID: "a1"}},
	}
	ctx := context.Background()

	in := &PullTasksReq{AgentID: "a1"}
	dec := func(v interface{}) error {
		*(v.(*PullTasksReq)) = *in
		return nil
	}
	resp, err := _Registration_PullTasks_Handler(fake, ctx, dec, nil)
	if err != nil {
		t.Fatalf("PullTasks handler: %v", err)
	}
	pr, ok := resp.(*PullTasksResp)
	if !ok || len(pr.Tasks) != 1 || pr.Tasks[0].TaskID != "tk-1" {
		t.Fatalf("PullTasks handler resp 错误: %+v", resp)
	}

	decErr := errors.New("dec boom")
	badDec := func(v interface{}) error { return decErr }
	_, err = _Registration_PullTasks_Handler(fake, ctx, badDec, nil)
	if !errors.Is(err, decErr) {
		t.Fatalf("want dec error, got %v", err)
	}

	rec := &recordingInterceptor{}
	_, err = _Registration_PullTasks_Handler(fake, ctx, dec, rec.intercept)
	if err != nil {
		t.Fatalf("PullTasks handler with interceptor: %v", err)
	}
	if !rec.called {
		t.Fatal("拦截器未被调用")
	}
	if rec.fullName != "/opsmesh.v1.Registration/PullTasks" {
		t.Fatalf("FullMethod = %q, want /opsmesh.v1.Registration/PullTasks", rec.fullName)
	}
}

// TestReportResultHandler 覆盖 _Registration_ReportResult_Handler 的三条路径。
func TestReportResultHandler(t *testing.T) {
	fake := &fakeLegacyServer{}
	ctx := context.Background()

	in := &proto.TaskResult{TaskID: "tk-1", AgentID: "a1", ExitCode: 0}
	dec := func(v interface{}) error {
		*(v.(*proto.TaskResult)) = *in
		return nil
	}
	resp, err := _Registration_ReportResult_Handler(fake, ctx, dec, nil)
	if err != nil {
		t.Fatalf("ReportResult handler: %v", err)
	}
	if _, ok := resp.(*Empty); !ok {
		t.Fatalf("ReportResult handler resp 类型错误: %+v", resp)
	}

	decErr := errors.New("dec boom")
	badDec := func(v interface{}) error { return decErr }
	_, err = _Registration_ReportResult_Handler(fake, ctx, badDec, nil)
	if !errors.Is(err, decErr) {
		t.Fatalf("want dec error, got %v", err)
	}

	rec := &recordingInterceptor{}
	_, err = _Registration_ReportResult_Handler(fake, ctx, dec, rec.intercept)
	if err != nil {
		t.Fatalf("ReportResult handler with interceptor: %v", err)
	}
	if !rec.called {
		t.Fatal("拦截器未被调用")
	}
	if rec.fullName != "/opsmesh.v1.Registration/ReportResult" {
		t.Fatalf("FullMethod = %q, want /opsmesh.v1.Registration/ReportResult", rec.fullName)
	}
}

// TestCancelTaskHandler 覆盖 _Registration_CancelTask_Handler 的三条路径。
func TestCancelTaskHandler(t *testing.T) {
	fake := &fakeLegacyServer{}
	ctx := context.Background()

	in := &CancelTaskReq{TaskID: "tk-1", TenantID: "t1"}
	dec := func(v interface{}) error {
		*(v.(*CancelTaskReq)) = *in
		return nil
	}
	resp, err := _Registration_CancelTask_Handler(fake, ctx, dec, nil)
	if err != nil {
		t.Fatalf("CancelTask handler: %v", err)
	}
	if _, ok := resp.(*Empty); !ok {
		t.Fatalf("CancelTask handler resp 类型错误: %+v", resp)
	}

	decErr := errors.New("dec boom")
	badDec := func(v interface{}) error { return decErr }
	_, err = _Registration_CancelTask_Handler(fake, ctx, badDec, nil)
	if !errors.Is(err, decErr) {
		t.Fatalf("want dec error, got %v", err)
	}

	rec := &recordingInterceptor{}
	_, err = _Registration_CancelTask_Handler(fake, ctx, dec, rec.intercept)
	if err != nil {
		t.Fatalf("CancelTask handler with interceptor: %v", err)
	}
	if !rec.called {
		t.Fatal("拦截器未被调用")
	}
	if rec.fullName != "/opsmesh.v1.Registration/CancelTask" {
		t.Fatalf("FullMethod = %q, want /opsmesh.v1.Registration/CancelTask", rec.fullName)
	}
}

// TestPollCancelsHandler 覆盖 _Registration_PollCancels_Handler 的三条路径。
func TestPollCancelsHandler(t *testing.T) {
	fake := &fakeLegacyServer{cancelledIDs: []string{"tk-2", "tk-3"}}
	ctx := context.Background()

	in := &PollCancelsReq{AgentID: "a1"}
	dec := func(v interface{}) error {
		*(v.(*PollCancelsReq)) = *in
		return nil
	}
	resp, err := _Registration_PollCancels_Handler(fake, ctx, dec, nil)
	if err != nil {
		t.Fatalf("PollCancels handler: %v", err)
	}
	pr, ok := resp.(*PollCancelsResp)
	if !ok || len(pr.CancelledTaskIDs) != 2 {
		t.Fatalf("PollCancels handler resp 错误: %+v", resp)
	}

	decErr := errors.New("dec boom")
	badDec := func(v interface{}) error { return decErr }
	_, err = _Registration_PollCancels_Handler(fake, ctx, badDec, nil)
	if !errors.Is(err, decErr) {
		t.Fatalf("want dec error, got %v", err)
	}

	rec := &recordingInterceptor{}
	_, err = _Registration_PollCancels_Handler(fake, ctx, dec, rec.intercept)
	if err != nil {
		t.Fatalf("PollCancels handler with interceptor: %v", err)
	}
	if !rec.called {
		t.Fatal("拦截器未被调用")
	}
	if rec.fullName != "/opsmesh.v1.Registration/PollCancels" {
		t.Fatalf("FullMethod = %q, want /opsmesh.v1.Registration/PollCancels", rec.fullName)
	}
}

// TestReportLogsHandler 覆盖 _Registration_ReportLogs_Handler 的三条路径。
func TestReportLogsHandler(t *testing.T) {
	fake := &fakeLegacyServer{}
	ctx := context.Background()

	in := &ReportLogsReq{Report: proto.LogReport{AgentID: "a1"}}
	dec := func(v interface{}) error {
		*(v.(*ReportLogsReq)) = *in
		return nil
	}
	resp, err := _Registration_ReportLogs_Handler(fake, ctx, dec, nil)
	if err != nil {
		t.Fatalf("ReportLogs handler: %v", err)
	}
	if _, ok := resp.(*Empty); !ok {
		t.Fatalf("ReportLogs handler resp 类型错误: %+v", resp)
	}
	if fake.lastReport == nil || fake.lastReport.Report.AgentID != "a1" {
		t.Fatalf("ReportLogs handler 未传递请求: %+v", fake.lastReport)
	}

	decErr := errors.New("dec boom")
	badDec := func(v interface{}) error { return decErr }
	_, err = _Registration_ReportLogs_Handler(fake, ctx, badDec, nil)
	if !errors.Is(err, decErr) {
		t.Fatalf("want dec error, got %v", err)
	}

	rec := &recordingInterceptor{}
	_, err = _Registration_ReportLogs_Handler(fake, ctx, dec, rec.intercept)
	if err != nil {
		t.Fatalf("ReportLogs handler with interceptor: %v", err)
	}
	if !rec.called {
		t.Fatal("拦截器未被调用")
	}
	if rec.fullName != "/opsmesh.v1.Registration/ReportLogs" {
		t.Fatalf("FullMethod = %q, want /opsmesh.v1.Registration/ReportLogs", rec.fullName)
	}
}

// ===== codec.go 边界与错误路径测试 =====

// TestInjectVersionNonObject 覆盖 injectVersion 中非 object JSON（数组/字符串/数字）路径。
// 非 object 时应原样返回（不注入 __v），由 Unmarshal 侧 checkVersion 拒绝。
func TestInjectVersionNonObject(t *testing.T) {
	cases := [][]byte{
		[]byte(`[1,2,3]`), // 数组
		[]byte(`"hello"`), // 字符串
		[]byte(`42`),      // 数字
		[]byte(`true`),    // 布尔
	}
	for i, c := range cases {
		out, err := injectVersion(c)
		if err != nil {
			t.Fatalf("case %d: injectVersion 非对象不应报错, got %v", i, err)
		}
		if string(out) != string(c) {
			t.Fatalf("case %d: 非对象应原样返回, got %s, want %s", i, out, c)
		}
	}
}

// TestInjectVersionEmpty 覆盖 injectVersion 中空字节输入路径（应返回 error）。
func TestInjectVersionEmpty(t *testing.T) {
	_, err := injectVersion([]byte(""))
	if err == nil {
		t.Fatal("空字节应返回 error")
	}
	_, err = injectVersion([]byte("   \t\n  "))
	if err == nil {
		t.Fatal("仅空白字符应返回 error")
	}
}

// TestInjectVersionEmptyObject 覆盖 injectVersion 中空对象 {} 路径（不插入逗号）。
func TestInjectVersionEmptyObject(t *testing.T) {
	out, err := injectVersion([]byte(`{}`))
	if err != nil {
		t.Fatalf("injectVersion({}) 不应报错: %v", err)
	}
	if string(out) != `{"__v":1}` {
		t.Fatalf("injectVersion({}) = %s, want {\"__v\":1}", out)
	}
}

// TestInjectVersionNull 覆盖 injectVersion 中 "null" 输入路径（视为空对象）。
func TestInjectVersionNull(t *testing.T) {
	out, err := injectVersion([]byte(`null`))
	if err != nil {
		t.Fatalf("injectVersion(null) 不应报错: %v", err)
	}
	if string(out) != `{"__v":1}` {
		t.Fatalf("injectVersion(null) = %s, want {\"__v\":1}", out)
	}
}

// TestCheckVersionEmpty 覆盖 checkVersion 中空 data 路径（返回 ErrCodecVersionMissing）。
func TestCheckVersionEmpty(t *testing.T) {
	err := checkVersion([]byte(""))
	if !errors.Is(err, ErrCodecVersionMissing) {
		t.Fatalf("空 data 应返回 ErrCodecVersionMissing, got %v", err)
	}
	err = checkVersion([]byte("   "))
	if !errors.Is(err, ErrCodecVersionMissing) {
		t.Fatalf("仅空白 data 应返回 ErrCodecVersionMissing, got %v", err)
	}
}

// TestCheckVersionNonObject 覆盖 checkVersion 中非 object 路径（应返回 error）。
func TestCheckVersionNonObject(t *testing.T) {
	cases := [][]byte{
		[]byte(`[1,2,3]`),
		[]byte(`"hello"`),
		[]byte(`42`),
	}
	for i, c := range cases {
		err := checkVersion(c)
		if err == nil {
			t.Fatalf("case %d: 非 object 应返回 error", i)
		}
		// 错误消息应包含 "expected json object"。
		if !strings.Contains(err.Error(), "expected json object") {
			t.Fatalf("case %d: 错误消息应包含 'expected json object', got %v", i, err)
		}
	}
}

// TestCheckVersionMalformedJSON 覆盖 checkVersion 中 JSON 解析失败路径。
func TestCheckVersionMalformedJSON(t *testing.T) {
	err := checkVersion([]byte(`{invalid json`))
	if err == nil {
		t.Fatal("畸形 JSON 应返回 error")
	}
	if !strings.Contains(err.Error(), "codec version check failed") {
		t.Fatalf("错误消息应包含 'codec version check failed', got %v", err)
	}
}

// TestCheckVersionFieldNotInt 覆盖 checkVersion 中 __v 字段非整数路径。
func TestCheckVersionFieldNotInt(t *testing.T) {
	err := checkVersion([]byte(`{"__v":"not-int"}`))
	if err == nil {
		t.Fatal("__v 非整数应返回 error")
	}
	if !strings.Contains(err.Error(), "not int") {
		t.Fatalf("错误消息应包含 'not int', got %v", err)
	}
}

// TestCheckVersionMismatch 覆盖 checkVersion 中 __v 不匹配路径（返回 ErrCodecVersionMismatch）。
func TestCheckVersionMismatch(t *testing.T) {
	err := checkVersion([]byte(`{"__v":99}`))
	if !errors.Is(err, ErrCodecVersionMismatch) {
		t.Fatalf("__v=99 应返回 ErrCodecVersionMismatch, got %v", err)
	}
}

// TestMarshalJSONError 覆盖 Marshal 中 json.Marshal 失败路径（传入不可序列化的值）。
func TestMarshalJSONError(t *testing.T) {
	// 传入不可序列化的值：循环引用的 map。
	type cyclic struct {
		Self *cyclic `json:"self"`
	}
	c := &cyclic{}
	c.Self = c // 自引用导致 json.Marshal 失败
	_, err := JSONCodec.Marshal(c)
	if err == nil {
		t.Fatal("循环引用应导致 Marshal 失败")
	}
}

// TestUnmarshalNonObject 覆盖 Unmarshal 中非 object 输入路径（checkVersion 拒绝）。
func TestUnmarshalNonObject(t *testing.T) {
	var hb HeartbeatReq
	err := JSONCodec.Unmarshal([]byte(`[1,2,3]`), &hb)
	if err == nil {
		t.Fatal("非 object 数组应被 Unmarshal 拒绝")
	}
}

// TestCodecName 确保 JSONCodec.Name() 返回常量 CodecName。
func TestCodecName(t *testing.T) {
	if got := JSONCodec.Name(); got != CodecName {
		t.Fatalf("Name() = %q, want %q", got, CodecName)
	}
}

// ===== StubAdapter ReportLogs 路径（StubAdapter 未实现 ReportLogs，跳过；pb 接口无此方法）=====
// 注：pb.RegistrationServer 接口仅含 6 方法（无 ReportLogs），StubAdapter 不需要适配 ReportLogs。
// ReportLogs 仅在手写 RegistrationServer 接口（含 7 方法）中存在，已在 TestReportLogsHandler 覆盖。
