// Package grpcx stub.go：生成 stub 适配层。
//
// StubAdapter 桥接「生成 stub 的 pb.RegistrationServer 接口」与「手写路径的 grpcx.RegistrationServer 接口」。
// 兼容期 controlplane 仍实现 grpcx.RegistrationServer（手写路径，默认）；
// 切换到生成 stub 时用 NewStubAdapter(impl) 包装，得到 pb.RegistrationServer，
// 用 pb.Registration_ServiceDesc 注册到 gRPC server（走 protobuf codec）。
//
// 互转函数（AgentInfoToProto/Legacy 等）在 pb 消息与 internal/proto 消息之间转换，
// 是传输层防腐层（pb 是 IDL 契约，internal/proto 是 JSON 友好的领域传输模型）。
package grpcx

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "opsmesh/internal/grpcx/pb" // 包名为 pbv1，用 pb 别名引用（兼容性修复）
	"opsmesh/internal/proto"
)

// ===== StubAdapter =====

// StubAdapter 把现有 grpcx.RegistrationServer 实现适配为 pb.RegistrationServer。
// 它在 pb 消息 <-> internal/proto 消息 之间转换，把业务逻辑委托给 inner。
// 兼容期：inner 是 controlplane.grpcServerImpl（实现 grpcx.RegistrationServer）。
type StubAdapter struct {
	inner RegistrationServer
}

// NewStubAdapter 构造 StubAdapter。传入实现 grpcx.RegistrationServer 的对象。
// 用法：gs.RegisterService(&pb.Registration_ServiceDesc, NewStubAdapter(impl))
func NewStubAdapter(inner RegistrationServer) *StubAdapter {
	return &StubAdapter{inner: inner}
}

// 编译期断言：StubAdapter 实现 pb.RegistrationServer 全部 7 方法。
var _ pb.RegistrationServer = (*StubAdapter)(nil)

// Register 实现 pb.RegistrationServer.Register。
func (a *StubAdapter) Register(ctx context.Context, req *pb.AgentInfo) (*pb.RegisterResp, error) {
	resp, err := a.inner.Register(ctx, AgentInfoLegacy(req))
	if err != nil {
		return nil, err
	}
	return RegisterRespToProto(resp), nil
}

// Heartbeat 实现 pb.RegistrationServer.Heartbeat。
func (a *StubAdapter) Heartbeat(ctx context.Context, req *pb.HeartbeatReq) (*pb.Empty, error) {
	_, err := a.inner.Heartbeat(ctx, HeartbeatReqLegacy(req))
	if err != nil {
		return nil, err
	}
	return &pb.Empty{}, nil
}

// PullTasks 实现 pb.RegistrationServer.PullTasks。
func (a *StubAdapter) PullTasks(ctx context.Context, req *pb.PullTasksReq) (*pb.PullTasksResp, error) {
	resp, err := a.inner.PullTasks(ctx, &PullTasksReq{AgentID: req.AgentId})
	if err != nil {
		return nil, err
	}
	tasks := make([]*pb.Task, 0, len(resp.Tasks))
	for i := range resp.Tasks {
		tasks = append(tasks, TaskToProto(&resp.Tasks[i]))
	}
	return &pb.PullTasksResp{Tasks: tasks}, nil
}

// ReportResult 实现 pb.RegistrationServer.ReportResult。
func (a *StubAdapter) ReportResult(ctx context.Context, req *pb.TaskResult) (*pb.Empty, error) {
	_, err := a.inner.ReportResult(ctx, TaskResultLegacy(req))
	if err != nil {
		return nil, err
	}
	return &pb.Empty{}, nil
}

// CancelTask 实现 pb.RegistrationServer.CancelTask。
func (a *StubAdapter) CancelTask(ctx context.Context, req *pb.CancelTaskReq) (*pb.Empty, error) {
	_, err := a.inner.CancelTask(ctx, &CancelTaskReq{TaskID: req.TaskId, TenantID: req.TenantId})
	if err != nil {
		return nil, err
	}
	return &pb.Empty{}, nil
}

// PollCancels 实现 pb.RegistrationServer.PollCancels。
func (a *StubAdapter) PollCancels(ctx context.Context, req *pb.PollCancelsReq) (*pb.PollCancelsResp, error) {
	resp, err := a.inner.PollCancels(ctx, &PollCancelsReq{AgentID: req.AgentId})
	if err != nil {
		return nil, err
	}
	return &pb.PollCancelsResp{CancelledTaskIds: resp.CancelledTaskIDs}, nil
}

// ReportLogs 实现 pb.RegistrationServer.ReportLogs。
func (a *StubAdapter) ReportLogs(ctx context.Context, req *pb.ReportLogsReq) (*pb.Empty, error) {
	_, err := a.inner.ReportLogs(ctx, &ReportLogsReq{Report: *LogReportLegacy(req.Report)})
	if err != nil {
		return nil, err
	}
	return &pb.Empty{}, nil
}

// ===== 互转函数：pb -> internal/proto（Legacy 后缀，表示"转回手写路径消息"）=====

// AgentInfoLegacy 把生成 stub 的 AgentInfo 转为手写路径的 proto.AgentInfo。
func AgentInfoLegacy(src *pb.AgentInfo) *proto.AgentInfo {
	if src == nil {
		return nil
	}
	return &proto.AgentInfo{
		AgentID:         src.AgentId,
		Hostname:        src.Hostname,
		Segment:         src.Segment,
		TenantID:        src.TenantId,
		Addr:            src.Addr,
		GRPCPort:        int(src.GrpcPort),
		MetricsPort:     int(src.MetricsPort),
		Status:          src.Status,
		Load:            int(src.Load),
		LastSeen:        tsLegacy(src.LastSeen),
		InstallToken:    src.InstallToken,
		OnboardDeviceID: src.OnboardDeviceId,
	}
}

// TaskLegacy 把生成 stub 的 Task 转为手写路径的 proto.Task。
func TaskLegacy(src *pb.Task) *proto.Task {
	if src == nil {
		return nil
	}
	return &proto.Task{
		TaskID:      src.TaskId,
		AgentID:     src.AgentId,
		TenantID:    src.TenantId,
		Type:        src.Type,
		Command:     src.Command,
		Content:     src.Content,
		Path:        src.Path,
		Status:      src.Status,
		ClaimedBy:   src.ClaimedBy,
		ClaimedAt:   tsLegacy(src.ClaimedAt),
		CreatedAt:   tsLegacy(src.CreatedAt),
		RetryCount:  int(src.RetryCount),
		MaxRetries:  int(src.MaxRetries),
		DeadLetter:  src.DeadLetter,
		Schedule:    src.Schedule,
		ParentID:    src.ParentId,
		LastFiredAt: tsLegacy(src.LastFiredAt),
		DependsOn:   src.DependsOn,
	}
}

// TaskResultLegacy 把生成 stub 的 TaskResult 转为手写路径的 proto.TaskResult。
func TaskResultLegacy(src *pb.TaskResult) *proto.TaskResult {
	if src == nil {
		return nil
	}
	return &proto.TaskResult{
		TaskID:     src.TaskId,
		AgentID:    src.AgentId,
		ExitCode:   int(src.ExitCode),
		Stdout:     src.Stdout,
		Stderr:     src.Stderr,
		DurationMs: src.DurationMs,
		FinishedAt: tsLegacy(src.FinishedAt),
	}
}

// HeartbeatReqLegacy 把生成 stub 的 HeartbeatReq 转为手写路径的 grpcx.HeartbeatReq。
func HeartbeatReqLegacy(src *pb.HeartbeatReq) *HeartbeatReq {
	if src == nil {
		return nil
	}
	return &HeartbeatReq{
		AgentID:    src.AgentId,
		Status:     src.Status,
		Load:       int(src.Load),
		CmdbReport: CmdbReportLegacy(src.CmdbReport),
	}
}

// CmdbReportLegacy 把生成 stub 的 CmdbReport 转为手写路径的 proto.CmdbReport。
func CmdbReportLegacy(src *pb.CmdbReport) *proto.CmdbReport {
	if src == nil {
		return nil
	}
	attrs := make([]proto.CmdbAttr, 0, len(src.Attrs))
	for _, a := range src.Attrs {
		attrs = append(attrs, proto.CmdbAttr{
			Key:   a.Key,
			Value: a.Value,
			Type:  a.Type,
		})
	}
	return &proto.CmdbReport{
		CiType: src.CiType,
		Seq:    src.Seq,
		Attrs:  attrs,
	}
}

// LogLineLegacy 把生成 stub 的 LogLine 转为手写路径的 proto.LogLine。
func LogLineLegacy(src *pb.LogLine) proto.LogLine {
	if src == nil {
		return proto.LogLine{}
	}
	return proto.LogLine{
		Timestamp: tsLegacy(src.Timestamp),
		Level:     src.Level,
		Message:   src.Message,
	}
}

// LogReportLegacy 把生成 stub 的 LogReport 转为手写路径的 proto.LogReport。
func LogReportLegacy(src *pb.LogReport) *proto.LogReport {
	if src == nil {
		return nil
	}
	lines := make([]proto.LogLine, 0, len(src.Lines))
	for _, l := range src.Lines {
		lines = append(lines, LogLineLegacy(l))
	}
	return &proto.LogReport{
		AgentID:     src.AgentId,
		TenantID:    src.TenantId,
		Hostname:    src.Hostname,
		LogName:     src.LogName,
		Lines:       lines,
		CollectedAt: tsLegacy(src.CollectedAt),
	}
}

// ===== 互转函数：internal/proto/grpcx -> pb（ToProto 后缀）=====

// AgentInfoToProto 把手写路径的 proto.AgentInfo 转为生成 stub 的 pb.AgentInfo。
func AgentInfoToProto(src *proto.AgentInfo) *pb.AgentInfo {
	if src == nil {
		return nil
	}
	return &pb.AgentInfo{
		AgentId:         src.AgentID,
		Hostname:        src.Hostname,
		Segment:         src.Segment,
		TenantId:        src.TenantID,
		Addr:            src.Addr,
		GrpcPort:        int32(src.GRPCPort),
		MetricsPort:     int32(src.MetricsPort),
		Status:          src.Status,
		Load:            int32(src.Load),
		LastSeen:        tsToProto(src.LastSeen),
		InstallToken:    src.InstallToken,
		OnboardDeviceId: src.OnboardDeviceID,
	}
}

// TaskToProto 把手写路径的 proto.Task 转为生成 stub 的 pb.Task。
func TaskToProto(src *proto.Task) *pb.Task {
	if src == nil {
		return nil
	}
	return &pb.Task{
		TaskId:      src.TaskID,
		AgentId:     src.AgentID,
		TenantId:    src.TenantID,
		Type:        src.Type,
		Command:     src.Command,
		Content:     src.Content,
		Path:        src.Path,
		Status:      src.Status,
		ClaimedBy:   src.ClaimedBy,
		ClaimedAt:   tsToProto(src.ClaimedAt),
		CreatedAt:   tsToProto(src.CreatedAt),
		RetryCount:  int32(src.RetryCount),
		MaxRetries:  int32(src.MaxRetries),
		DeadLetter:  src.DeadLetter,
		Schedule:    src.Schedule,
		ParentId:    src.ParentID,
		LastFiredAt: tsToProto(src.LastFiredAt),
		DependsOn:   src.DependsOn,
	}
}

// TaskResultToProto 把手写路径的 proto.TaskResult 转为生成 stub 的 pb.TaskResult。
func TaskResultToProto(src *proto.TaskResult) *pb.TaskResult {
	if src == nil {
		return nil
	}
	return &pb.TaskResult{
		TaskId:     src.TaskID,
		AgentId:    src.AgentID,
		ExitCode:   int32(src.ExitCode),
		Stdout:     src.Stdout,
		Stderr:     src.Stderr,
		DurationMs: src.DurationMs,
		FinishedAt: tsToProto(src.FinishedAt),
	}
}

// RegisterRespToProto 把手写路径的 grpcx.RegisterResp 转为生成 stub 的 pb.RegisterResp。
func RegisterRespToProto(src *RegisterResp) *pb.RegisterResp {
	if src == nil {
		return nil
	}
	cfg := make(map[string]int32, len(src.ControlConfig))
	for k, v := range src.ControlConfig {
		cfg[k] = int32(v)
	}
	return &pb.RegisterResp{
		AgentId:       src.AgentID,
		ControlConfig: cfg,
	}
}

// LogLineToProto 把手写路径的 proto.LogLine 转为生成 stub 的 pb.LogLine。
func LogLineToProto(src *proto.LogLine) *pb.LogLine {
	if src == nil {
		return nil
	}
	return &pb.LogLine{
		Timestamp: tsToProto(src.Timestamp),
		Level:     src.Level,
		Message:   src.Message,
	}
}

// LogReportToProto 把手写路径的 proto.LogReport 转为生成 stub 的 pb.LogReport。
func LogReportToProto(src *proto.LogReport) *pb.LogReport {
	if src == nil {
		return nil
	}
	lines := make([]*pb.LogLine, 0, len(src.Lines))
	for i := range src.Lines {
		lines = append(lines, LogLineToProto(&src.Lines[i]))
	}
	return &pb.LogReport{
		AgentId:     src.AgentID,
		TenantId:    src.TenantID,
		Hostname:    src.Hostname,
		LogName:     src.LogName,
		Lines:       lines,
		CollectedAt: tsToProto(src.CollectedAt),
	}
}

// ===== Timestamp 互转 helper =====

// tsToProto 把 time.Time 转为 *timestamppb.Timestamp（零值返回 nil）。
func tsToProto(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// tsLegacy 把 *timestamppb.Timestamp 转为 time.Time（nil 返回零值）。
func tsLegacy(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}
